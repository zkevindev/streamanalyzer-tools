package analyze

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"streamanalyzer/internal/hlsdelivery/m3u8"
)

// Monitor 在 duration 内反复读取直播播放列表，并拉取新分片。
func Monitor(ctx context.Context, rawURL string, opt Options) (*Report, error) {
	opt = opt.withDefaults()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, opt.Duration)
	defer cancel()
	started := time.Now()
	r := newRunner(opt)
	rep, err := r.monitor(ctx, rawURL, opt, started)
	if rep != nil {
		rep.DurationS = time.Since(started).Seconds()
		if rep.Started == "" {
			rep.Started = started.UTC().Format(time.RFC3339)
		}
	}
	return rep, err
}

func (r *runner) monitor(ctx context.Context, rawURL string, opt Options, started time.Time) (*Report, error) {
	var (
		lastSeq     = -1
		quietSince  time.Time
		stale       bool
		loggedStale bool
		obs         []observation
		lastPL      *m3u8.Playlist
		variants    []m3u8.Variant
		playlistH   http.Header
	)
	inits := map[string][]byte{}
	keys := map[string][]byte{}

	for {
		if ctx.Err() != nil {
			break
		}
		final, pl, res, err := r.loadPlaylist(ctx, rawURL)
		if err != nil {
			if lastPL == nil {
				return nil, manifestErr(err)
			}
			r.logf("播放列表刷新失败: %v\n", err)
			if !wait(ctx, time.Second) {
				break
			}
			continue
		}
		var master []m3u8.Variant
		if pl.IsMaster() {
			master = pl.Variants
			one := opt
			one.AllVariants = false
			picked, err := pickVariants(pl.Variants, one)
			if err != nil {
				return nil, err
			}
			mediaURL, err := resolveURL(final, picked[0].URI)
			if err != nil {
				return nil, manifestErr(err)
			}
			final, pl, res, err = r.loadPlaylist(ctx, mediaURL)
			if err != nil {
				if lastPL == nil {
					return nil, manifestErr(err)
				}
				r.logf("媒体播放列表刷新失败: %v\n", err)
				if !wait(ctx, time.Second) {
					break
				}
				continue
			}
		}
		if len(pl.Segments) == 0 {
			if lastPL == nil {
				return nil, &ExitError{Code: 6, Msg: "媒体播放列表里没有分片"}
			}
			if !wait(ctx, pollInterval(pl)) {
				break
			}
			continue
		}
		lastPL = pl
		variants = master
		if res != nil {
			playlistH = res.Header
		}

		var fresh []m3u8.Segment
		for _, seg := range pl.Segments {
			if seg.Sequence > lastSeq {
				fresh = append(fresh, seg)
			}
		}
		if len(fresh) > 100 {
			fresh = fresh[len(fresh)-100:]
		}
		if len(fresh) == 0 {
			if quietSince.IsZero() {
				quietSince = time.Now()
			}
			limit := 2 * time.Duration(pl.TargetDuration*float64(time.Second))
			if limit < time.Second {
				limit = time.Second
			}
			if time.Since(quietSince) > limit {
				stale = true
				if !loggedStale {
					r.logf("播放列表已 %.0f 秒没有新分片\n", time.Since(quietSince).Seconds())
					loggedStale = true
				}
			}
		} else {
			quietSince = time.Time{}
			loggedStale = false
			stale = false
			for _, seg := range fresh {
				if ctx.Err() != nil {
					break
				}
				item := r.pullOne(ctx, final, seg, opt, inits, keys)
				obs = append(obs, item)
				r.logf("%s  %s\n", formatWall(time.Now()), FormatSegmentLine(item.Result))
				if opt.OnSegment != nil {
					opt.OnSegment(item.Result)
				}
			}
			lastSeq = fresh[len(fresh)-1].Sequence
		}
		viewer, edge, known := estimateLatency(pl, time.Now())
		if known {
			r.logf("估计观看延迟 %.1fs（边缘滞后 %.1fs，播放器通常再留 3 个分片）\n", viewer.Seconds(), edge.Seconds())
		} else {
			r.logf("直播末尾分片没有可用的节目时间，观看延迟未知\n")
		}
		if !wait(ctx, pollInterval(pl)) {
			break
		}
	}
	if lastPL == nil {
		if ctx.Err() != nil {
			return nil, &ExitError{Code: 1, Msg: "在 --duration 内没有读到播放列表", Err: ctx.Err()}
		}
		return nil, &ExitError{Code: 5, Msg: "无法获取或解析播放列表"}
	}
	if len(obs) == 0 {
		return nil, &ExitError{Code: 1, Msg: "在 --duration 内没有拉到分片", Err: ctx.Err()}
	}
	rep := summarize("monitor", rawURL, opt, lastPL, obs, playlistH, variants)
	rep.Started = started.UTC().Format(time.RFC3339)
	rep.Stale = stale
	viewer, edge, known := estimateLatency(lastPL, time.Now())
	if known {
		ms := float64(viewer / time.Millisecond)
		rep.LiveLatencyMS = &ms
		em := float64(edge / time.Millisecond)
		rep.EdgeLagMS = &em
	}
	if stale {
		rep.Recommendations = append(rep.Recommendations, "播放列表长时间没有新分片，直播可能已经停了，或者打包端没有继续切片")
		rep.Infrastructure.PlaylistWarnings = append(rep.Infrastructure.PlaylistWarnings, rep.Recommendations[len(rep.Recommendations)-1])
	}
	if rep.Variant == nil && len(variants) > 0 {
		one := opt
		one.AllVariants = false
		picked, err := pickVariants(variants, one)
		if err == nil && len(picked) == 1 {
			info := toInfo(picked[0])
			rep.Variant = &info
			rep.Variants = variantInfos(variants)
		}
	}
	applyThresholds(rep, opt)
	return rep, nil
}

func estimateLatency(pl *m3u8.Playlist, at time.Time) (viewer, edge time.Duration, edgeKnown bool) {
	target := 2.0
	if pl != nil && pl.TargetDuration > 0 {
		target = pl.TargetDuration
	}
	hold := time.Duration(target * 3 * float64(time.Second))
	if pl == nil || len(pl.Segments) == 0 {
		return 0, 0, false
	}
	last := &pl.Segments[len(pl.Segments)-1]
	if !last.HasTime || last.ProgramDateTime.IsZero() {
		return 0, 0, false
	}
	edgeEnd := last.ProgramDateTime.Add(time.Duration(last.Duration * float64(time.Second)))
	lag := at.Sub(edgeEnd)
	if lag < 0 || lag > 5*time.Minute {
		return 0, 0, false
	}
	return lag + hold, lag, true
}

func pollInterval(pl *m3u8.Playlist) time.Duration {
	d := time.Second
	if pl != nil && pl.TargetDuration > 0 {
		d = time.Duration(pl.TargetDuration * float64(time.Second) / 2)
	}
	if d < 500*time.Millisecond {
		d = 500 * time.Millisecond
	}
	return d
}

func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *runner) logf(format string, args ...any) {
	if r.opt.Log == nil {
		return
	}
	fmt.Fprintf(r.opt.Log, format, args...)
}
