package hls

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/asticode/go-astits"
)

type SegmentStats struct {
	StreamID    string
	Seq         uint64
	URI         string
	DurationSec float64
	SizeBytes   int

	PATCount int
	PMTCount int
	PESCount int

	VideoPES int
	AudioPES int

	// 90 kHz 时间基（与 MPEG-TS PES 一致）；无有效值时为 -1。
	VideoPTSFirst90k int64
	VideoPTSLast90k  int64
	VideoDTSFirst90k int64
	VideoDTSLast90k  int64
	AudioPTSFirst90k int64
	AudioPTSLast90k  int64
	AudioDTSFirst90k int64
	AudioDTSLast90k  int64
	// 本切片内：首帧视频 PTS − 首帧音频 PTS（90k tick）；仅当两者均有首 PTS 时有效。
	AVDiffPTS90k int64
	AVDiffValid  bool
	// 首帧视频 DTS − 首帧音频 DTS（90k tick）；仅当两者均有有效 DTS（含 PTS 兜底）时有效。
	AVDiffDTS90k   int64
	AVDiffDTSValid bool
	// 切片内相邻 I/IDR 帧之间的间隔（毫秒），由 H.264 IDR(5) / H.265 IDR 类型 NALU 的 PTS 差换算。
	IFrameIntervalsMs []int64
}

type Client struct {
	PlaylistURL string
	HTTPClient  *http.Client
	UserAgent   string
	OnSegment   func(SegmentStats)
}

func (c *Client) Run(ctx context.Context) error {
	if c == nil || c.PlaylistURL == "" {
		return fmt.Errorf("missing playlist url")
	}
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	mediaURLs, err := c.discoverMediaPlaylists(ctx, hc, c.PlaylistURL, make(map[string]bool), 0)
	if err != nil {
		return err
	}
	if len(mediaURLs) == 0 {
		return fmt.Errorf("no media playlist found from %s", c.PlaylistURL)
	}

	errCh := make(chan error, len(mediaURLs))
	var wg sync.WaitGroup
	for _, mu := range mediaURLs {
		mediaURL := mu
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := c.runMediaPlaylist(ctx, hc, mediaURL); e != nil &&
				e != context.Canceled && e != context.DeadlineExceeded {
				select {
				case errCh <- e:
				default:
				}
				cancel()
			}
		}()
	}
	wg.Wait()

	select {
	case e := <-errCh:
		return e
	default:
		return ctx.Err()
	}
}

func (c *Client) discoverMediaPlaylists(ctx context.Context, hc *http.Client, playlistURL string, visited map[string]bool, depth int) ([]string, error) {
	if depth > 8 {
		return nil, fmt.Errorf("playlist nesting too deep: %s", playlistURL)
	}
	if visited[playlistURL] {
		return nil, nil
	}
	visited[playlistURL] = true

	pl, err := c.fetchPlaylist(ctx, hc, playlistURL)
	if err != nil {
		return nil, err
	}
	if pl.HasMap {
		return nil, fmt.Errorf("fmp4 playlist not supported yet (#EXT-X-MAP): %s", playlistURL)
	}
	if pl.IsMaster {
		out := make([]string, 0, len(pl.Variants))
		for _, v := range pl.Variants {
			nextURL, err := resolveURL(playlistURL, v.URI)
			if err != nil {
				return nil, err
			}
			sub, err := c.discoverMediaPlaylists(ctx, hc, nextURL, visited, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		}
		return dedupStrings(out), nil
	}

	childPlaylists := make([]string, 0)
	for _, seg := range pl.Segments {
		if looksLikeM3U8(seg.URI) {
			nextURL, err := resolveURL(playlistURL, seg.URI)
			if err != nil {
				return nil, err
			}
			childPlaylists = append(childPlaylists, nextURL)
		}
	}
	if len(childPlaylists) > 0 {
		out := make([]string, 0, len(childPlaylists))
		for _, cp := range dedupStrings(childPlaylists) {
			sub, err := c.discoverMediaPlaylists(ctx, hc, cp, visited, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
		}
		return dedupStrings(out), nil
	}
	return []string{playlistURL}, nil
}

func (c *Client) runMediaPlaylist(ctx context.Context, hc *http.Client, playlistURL string) error {
	lastSeq := uint64(0)
	hasLastSeq := false

	for {
		pl, err := c.fetchPlaylist(ctx, hc, playlistURL)
		if err != nil {
			return err
		}
		if pl.IsMaster {
			sub, err := c.discoverMediaPlaylists(ctx, hc, playlistURL, make(map[string]bool), 0)
			if err != nil {
				return err
			}
			if len(sub) == 0 {
				return fmt.Errorf("master playlist has no media children: %s", playlistURL)
			}
			playlistURL = sub[0]
			lastSeq = 0
			hasLastSeq = false
			continue
		}
		if pl.HasMap {
			return fmt.Errorf("fmp4 playlist not supported yet (#EXT-X-MAP): %s", playlistURL)
		}

		startIdx := 0
		if hasLastSeq {
			for i, seg := range pl.Segments {
				if seg.Seq > lastSeq {
					startIdx = i
					break
				}
				startIdx = i + 1
			}
		}

		for i := startIdx; i < len(pl.Segments); i++ {
			seg := pl.Segments[i]
			if looksLikeM3U8(seg.URI) {
				nextURL, err := resolveURL(playlistURL, seg.URI)
				if err != nil {
					return err
				}
				playlistURL = nextURL
				lastSeq = 0
				hasLastSeq = false
				goto nextRound
			}
			if !looksLikeTS(seg.URI) {
				return fmt.Errorf("non-ts segment is not supported yet: %s", seg.URI)
			}
			segURL, err := resolveURL(playlistURL, seg.URI)
			if err != nil {
				return err
			}
			raw, err := c.fetchBinary(ctx, hc, segURL)
			if err != nil {
				return fmt.Errorf("fetch segment %d failed: %w", seg.Seq, err)
			}
			stats, err := parseTSSegment(raw)
			if err != nil {
				return fmt.Errorf("parse ts segment %d failed: %w", seg.Seq, err)
			}
			stats.StreamID = playlistURL
			stats.Seq = seg.Seq
			stats.URI = segURL
			stats.DurationSec = seg.Duration
			stats.SizeBytes = len(raw)
			if c.OnSegment != nil {
				c.OnSegment(stats)
			}
			lastSeq = seg.Seq
			hasLastSeq = true
		}

		if pl.Endlist {
			return nil
		}
	nextRound:
		wait := 2 * time.Second
		if pl.TargetDuration > 0 {
			wait = time.Duration(pl.TargetDuration*500) * time.Millisecond
			if wait < time.Second {
				wait = time.Second
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (c *Client) fetchPlaylist(ctx context.Context, hc *http.Client, playlistURL string) (*playlist, error) {
	body, err := c.fetchText(ctx, hc, playlistURL)
	if err != nil {
		return nil, err
	}
	pl, err := parsePlaylist(body)
	if err != nil {
		return nil, fmt.Errorf("parse playlist %s failed: %w", playlistURL, err)
	}
	return pl, nil
}

func (c *Client) fetchText(ctx context.Context, hc *http.Client, u string) (string, error) {
	b, err := c.fetchBinary(ctx, hc, u)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *Client) fetchBinary(ctx context.Context, hc *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	ua := c.UserAgent
	if ua == "" {
		ua = "StreamAnalyzer-HLSClient/1.0"
	}
	req.Header.Set("User-Agent", ua)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

type playlist struct {
	IsMaster       bool
	Variants       []variantStream
	Segments       []mediaSegment
	TargetDuration int
	Endlist        bool
	HasMap         bool
}

type variantStream struct {
	URI       string
	Bandwidth int
}

type mediaSegment struct {
	Seq      uint64
	URI      string
	Duration float64
}

func parsePlaylist(content string) (*playlist, error) {
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	if len(lines) == 0 || lines[0] != "#EXTM3U" {
		return nil, fmt.Errorf("invalid m3u8 header")
	}

	pl := &playlist{}
	seq := uint64(0)
	nextDur := 0.0
	expectVariant := false
	pendingVariantBandwidth := 0

	for _, ln := range lines {
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "#EXT-X-STREAM-INF:") {
			pl.IsMaster = true
			attrs := strings.TrimPrefix(ln, "#EXT-X-STREAM-INF:")
			pendingVariantBandwidth = parseBandwidth(attrs)
			expectVariant = true
			continue
		}
		if strings.HasPrefix(ln, "#EXT-X-TARGETDURATION:") {
			v := strings.TrimPrefix(ln, "#EXT-X-TARGETDURATION:")
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				pl.TargetDuration = n
			}
			continue
		}
		if strings.HasPrefix(ln, "#EXT-X-MEDIA-SEQUENCE:") {
			v := strings.TrimPrefix(ln, "#EXT-X-MEDIA-SEQUENCE:")
			if n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64); err == nil {
				seq = n
			}
			continue
		}
		if strings.HasPrefix(ln, "#EXTINF:") {
			v := strings.TrimPrefix(ln, "#EXTINF:")
			if idx := strings.IndexByte(v, ','); idx >= 0 {
				v = v[:idx]
			}
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f >= 0 {
				nextDur = f
			} else {
				nextDur = 0
			}
			continue
		}
		if strings.HasPrefix(ln, "#EXT-X-MAP:") {
			pl.HasMap = true
			continue
		}
		if ln == "#EXT-X-ENDLIST" {
			pl.Endlist = true
			continue
		}
		if strings.HasPrefix(ln, "#") {
			continue
		}
		// URI line
		if expectVariant {
			pl.Variants = append(pl.Variants, variantStream{
				URI:       ln,
				Bandwidth: pendingVariantBandwidth,
			})
			pendingVariantBandwidth = 0
			expectVariant = false
			continue
		}
		pl.Segments = append(pl.Segments, mediaSegment{
			Seq:      seq,
			URI:      ln,
			Duration: nextDur,
		})
		seq++
		nextDur = 0
	}

	if pl.IsMaster {
		return pl, nil
	}
	if len(pl.Segments) == 0 {
		return nil, fmt.Errorf("media playlist has no segments")
	}
	return pl, nil
}

func resolveURL(baseURL, ref string) (string, error) {
	bu, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	ru, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return bu.ResolveReference(ru).String(), nil
}

func looksLikeTS(u string) bool {
	pu, err := url.Parse(u)
	if err != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(pu.Path))
	return ext == ".ts"
}

func looksLikeM3U8(u string) bool {
	pu, err := url.Parse(u)
	if err != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(pu.Path))
	return ext == ".m3u8"
}

func parseBandwidth(attrs string) int {
	for _, part := range strings.Split(attrs, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "BANDWIDTH=") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(part, "BANDWIDTH="))
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	return 0
}

func pickBestVariant(vs []variantStream) variantStream {
	best := vs[0]
	for i := 1; i < len(vs); i++ {
		if vs[i].Bandwidth > best.Bandwidth {
			best = vs[i]
		}
	}
	return best
}

func dedupStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func parseTSSegment(b []byte) (SegmentStats, error) {
	var out SegmentStats
	out.VideoPTSFirst90k = -1
	out.VideoPTSLast90k = -1
	out.VideoDTSFirst90k = -1
	out.VideoDTSLast90k = -1
	out.AudioPTSFirst90k = -1
	out.AudioPTSLast90k = -1
	out.AudioDTSFirst90k = -1
	out.AudioDTSLast90k = -1
	if len(b) == 0 {
		return out, fmt.Errorf("empty segment")
	}
	dmx := astits.NewDemuxer(context.Background(), bytes.NewReader(b))
	videoPID := make(map[uint16]bool)
	audioPID := make(map[uint16]bool)
	videoPIDStreamType := make(map[uint16]astits.StreamType)
	var keyframePTS []int64

	for {
		data, err := dmx.NextData()
		if errors.Is(err, io.EOF) || errors.Is(err, astits.ErrNoMorePackets) {
			break
		}
		if err != nil {
			return out, err
		}
		if data == nil {
			continue
		}
		if data.PAT != nil {
			out.PATCount++
		}
		if data.PMT != nil {
			out.PMTCount++
			for _, es := range data.PMT.ElementaryStreams {
				cat := tsStreamCategory(es.StreamType)
				if cat == "video" {
					videoPID[es.ElementaryPID] = true
					videoPIDStreamType[es.ElementaryPID] = es.StreamType
				} else if cat == "audio" {
					audioPID[es.ElementaryPID] = true
				}
			}
		}
		if data.PES != nil {
			out.PESCount++
			isVideo := videoPID[data.PID]
			isAudio := audioPID[data.PID]
			switch {
			case isVideo:
				out.VideoPES++
			case isAudio:
				out.AudioPES++
			}
			if isVideo || isAudio {
				var ptsBase, dtsBase int64
				var hasPTS, hasDTS bool
				if data.PES.Header != nil {
					if oh := data.PES.Header.OptionalHeader; oh != nil {
						if oh.PTS != nil {
							ptsBase = oh.PTS.Base
							hasPTS = true
						}
						if oh.DTS != nil {
							dtsBase = oh.DTS.Base
							hasDTS = true
						}
					}
				}
				// 多数 HLS 切片里音视频 PES 仅带 PTS（无 DTS 字段）；无 DTS 时解码时间戳按与 PTS 相同处理（音视频一致）。
				var ed90k int64
				hasED := false
				switch {
				case hasDTS:
					ed90k, hasED = dtsBase, true
				case hasPTS:
					ed90k, hasED = ptsBase, true
				}
				if isVideo {
					if hasPTS {
						if out.VideoPTSFirst90k < 0 || ptsBase < out.VideoPTSFirst90k {
							out.VideoPTSFirst90k = ptsBase
						}
						if out.VideoPTSLast90k < 0 || ptsBase > out.VideoPTSLast90k {
							out.VideoPTSLast90k = ptsBase
						}
					}
					if hasED {
						if out.VideoDTSFirst90k < 0 || ed90k < out.VideoDTSFirst90k {
							out.VideoDTSFirst90k = ed90k
						}
						if out.VideoDTSLast90k < 0 || ed90k > out.VideoDTSLast90k {
							out.VideoDTSLast90k = ed90k
						}
					}
				}
				if isAudio {
					if hasPTS {
						if out.AudioPTSFirst90k < 0 || ptsBase < out.AudioPTSFirst90k {
							out.AudioPTSFirst90k = ptsBase
						}
						if out.AudioPTSLast90k < 0 || ptsBase > out.AudioPTSLast90k {
							out.AudioPTSLast90k = ptsBase
						}
					}
					if hasED {
						if out.AudioDTSFirst90k < 0 || ed90k < out.AudioDTSFirst90k {
							out.AudioDTSFirst90k = ed90k
						}
						if out.AudioDTSLast90k < 0 || ed90k > out.AudioDTSLast90k {
							out.AudioDTSLast90k = ed90k
						}
					}
				}
				if isVideo && hasPTS && len(data.PES.Data) > 0 {
					st := videoPIDStreamType[data.PID]
					if st != 0 && isKeyframeAccessUnit(data.PES.Data, st) {
						keyframePTS = append(keyframePTS, ptsBase)
					}
				}
			}
		}
	}
	if out.VideoPTSFirst90k >= 0 && out.AudioPTSFirst90k >= 0 {
		out.AVDiffPTS90k = out.VideoPTSFirst90k - out.AudioPTSFirst90k
		out.AVDiffValid = true
	}
	if out.VideoDTSFirst90k >= 0 && out.AudioDTSFirst90k >= 0 {
		out.AVDiffDTS90k = out.VideoDTSFirst90k - out.AudioDTSFirst90k
		out.AVDiffDTSValid = true
	}
	if len(keyframePTS) > 1 {
		sort.Slice(keyframePTS, func(i, j int) bool { return keyframePTS[i] < keyframePTS[j] })
		uniq := make([]int64, 0, len(keyframePTS))
		for _, k := range keyframePTS {
			if len(uniq) == 0 || uniq[len(uniq)-1] != k {
				uniq = append(uniq, k)
			}
		}
		for i := 1; i < len(uniq); i++ {
			d := (uniq[i] - uniq[i-1]) / 90
			if d > 0 {
				out.IFrameIntervalsMs = append(out.IFrameIntervalsMs, d)
			}
		}
	}
	return out, nil
}

func tsStreamCategory(st astits.StreamType) string {
	switch st {
	case astits.StreamTypeH264Video, astits.StreamTypeH265Video, astits.StreamTypeMPEG1Video, astits.StreamTypeMPEG2Video:
		return "video"
	case astits.StreamTypeAACAudio, astits.StreamTypeMPEG1Audio, astits.StreamTypeMPEG2Audio:
		return "audio"
	default:
		return ""
	}
}
