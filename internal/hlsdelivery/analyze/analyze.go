package analyze

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"streamanalyzer/internal/hlsdelivery/m3u8"
	"streamanalyzer/internal/hlsdelivery/media"
	"streamanalyzer/internal/hlsdelivery/metrics"
)

const (
	ToolName = "hlsstats"
	Version  = "1.0.0"
)

// Options 控制一次分析。阈值为 0 时不启用对应的退出码。
type Options struct {
	Timeout             time.Duration
	Variant             int
	VariantSet          bool
	AllVariants         bool
	Segments            int
	ParseSegments       bool
	Duration            time.Duration
	TraceIPs            bool
	Headers             http.Header
	Insecure            bool
	ErrorThreshold      float64
	LatencyThreshold    float64
	ThroughputThreshold float64
	OnSegment           func(SegmentResult)
	Log                 io.Writer
}

func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.Segments <= 0 {
		o.Segments = 10
	}
	if o.Duration <= 0 {
		o.Duration = 30 * time.Second
	}
	return o
}

// ExitError 是带进程退出码的失败。
type ExitError struct {
	Code int
	Msg  string
	Err  error
}

func (e *ExitError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("退出码 %d", e.Code)
}

func (e *ExitError) Unwrap() error { return e.Err }

// Report 是一次播放列表或分片分析的结果。
type Report struct {
	Tool            string                `json:"tool"`
	Version         string                `json:"version"`
	Command         string                `json:"command"`
	SchemaVersion   string                `json:"schema_version,omitempty"`
	URL             string                `json:"url"`
	StreamType      string                `json:"type,omitempty"`
	Started         string                `json:"started"`
	DurationS       float64               `json:"duration_s"`
	Live            bool                  `json:"live"`
	Playlist        PlaylistInfo          `json:"playlist"`
	PlaylistRefresh *PlaylistRefreshStats `json:"playlist_refresh,omitempty"`
	Variant         *VariantInfo          `json:"variant,omitempty"`
	Variants        []VariantInfo         `json:"variants,omitempty"`
	Summary         Summary               `json:"summary"`
	Infrastructure  Infrastructure        `json:"infrastructure"`
	ABR             *ABRReport            `json:"abr,omitempty"`
	Recommendations []string              `json:"recommendations,omitempty"`
	Segments        []SegmentResult       `json:"segments,omitempty"`
	Results         []Report              `json:"results,omitempty"`
	LiveLatencyMS   *float64              `json:"live_latency_ms,omitempty"`
	EdgeLagMS       *float64              `json:"edge_lag_ms,omitempty"`
	Stale           bool                  `json:"stale,omitempty"`
	ExitCode        int                   `json:"exit_code"`
}

// PlaylistRefreshStats 单独记录监控期间的播放列表刷新故障，不计入分片错误率。
type PlaylistRefreshStats struct {
	Failures            int    `json:"failures"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	LastError           string `json:"last_error"`
}

// PlaylistInfo 描述播放列表本身。
type PlaylistInfo struct {
	Version             int     `json:"version,omitempty"`
	Type                string  `json:"type,omitempty"`
	Live                bool    `json:"live"`
	TargetDurationS     float64 `json:"target_duration_s,omitempty"`
	MediaSequence       int     `json:"media_sequence,omitempty"`
	SegmentCount        int     `json:"segment_count"`
	IndependentSegments bool    `json:"independent_segments,omitempty"`
	Encrypted           bool    `json:"encrypted,omitempty"`
}

// VariantInfo 是主播放列表中的一路。
type VariantInfo struct {
	URI              string  `json:"uri"`
	Bandwidth        int     `json:"bandwidth,omitempty"`
	AverageBandwidth int     `json:"average_bandwidth,omitempty"`
	Resolution       string  `json:"resolution,omitempty"`
	Codecs           string  `json:"codecs,omitempty"`
	FrameRate        float64 `json:"frame_rate,omitempty"`
	Name             string  `json:"name,omitempty"`
}

// Summary 是分片请求的汇总。P95 吞吐是最差的 5%，不是最高的 5%。
type Summary struct {
	SegmentsFetched   int            `json:"segments_fetched"`
	SegmentsFailed    int            `json:"segments_failed"`
	SegmentsSkipped   int            `json:"segments_skipped,omitempty"`
	ErrorRatePct      float64        `json:"error_rate_pct"`
	AvgTTFBMS         float64        `json:"avg_ttfb_ms"`
	AvgDownloadMS     float64        `json:"avg_download_ms"`
	AvgThroughputKbps float64        `json:"avg_throughput_kbps"`
	P50ThroughputKbps float64        `json:"p50_throughput_kbps"`
	P95ThroughputKbps float64        `json:"p95_throughput_kbps"`
	MinThroughputKbps float64        `json:"min_throughput_kbps"`
	MaxThroughputKbps float64        `json:"max_throughput_kbps"`
	CacheHitRatePct   *float64       `json:"cache_hit_rate_pct,omitempty"`
	PTSContinuity     string         `json:"pts_continuity,omitempty"`
	Discontinuities   int            `json:"discontinuities"`
	ErrorBreakdown    ErrorBreakdown `json:"error_breakdown,omitempty"`
}

// ErrorBreakdown 按失败原因计数。
type ErrorBreakdown struct {
	HTTP4xx    int `json:"http_4xx,omitempty"`
	HTTP5xx    int `json:"http_5xx,omitempty"`
	Timeouts   int `json:"timeouts,omitempty"`
	ShortReads int `json:"short_reads,omitempty"`
	Other      int `json:"other,omitempty"`
}

// Infrastructure 是 CDN、缓存和解析 IP。
type Infrastructure struct {
	CDNDetected          string            `json:"cdn_detected,omitempty"`
	CDNHeaders           map[string]string `json:"cdn_headers,omitempty"`
	EdgeLocation         string            `json:"edge_location,omitempty"`
	LoadBalancerDetected bool              `json:"load_balancer_detected"`
	CacheHits            int               `json:"-"`
	CacheMisses          int               `json:"-"`
	CacheWarnings        []string          `json:"cache_warnings,omitempty"`
	IPFlips              int               `json:"ip_flips"`
	IPs                  []string          `json:"ips,omitempty"`
	PlaylistWarnings     []string          `json:"playlist_warnings,omitempty"`
}

// ABRReport 用实测吞吐评估各档位的余量。
type ABRReport struct {
	MeasuredBandwidthKbps float64      `json:"measured_bandwidth_kbps"`
	P95BandwidthKbps      float64      `json:"p95_bandwidth_kbps"`
	Recommended           string       `json:"recommended,omitempty"`
	SafeFallback          string       `json:"safe_fallback,omitempty"`
	Variants              []ABRVariant `json:"variants"`
}

// ABRVariant 是单个档位的余量和风险。
type ABRVariant struct {
	Label         string  `json:"label"`
	BandwidthKbps int     `json:"bandwidth_kbps"`
	Headroom      float64 `json:"headroom"`
	P95Headroom   float64 `json:"p95_headroom"`
	Risk          string  `json:"risk"`
}

// SegmentResult 是一个分片的测量。
type SegmentResult struct {
	Sequence           int      `json:"sequence"`
	Discontinuity      bool     `json:"discontinuity,omitempty"`
	Gap                bool     `json:"gap,omitempty"`
	URL                string   `json:"url"`
	DeclaredDurationS  float64  `json:"declared_duration_s"`
	HTTPStatus         int      `json:"http_status,omitempty"`
	Bytes              int      `json:"bytes,omitempty"`
	RemoteAddr         string   `json:"remote_addr,omitempty"`
	TTFBMS             float64  `json:"ttfb_ms"`
	DownloadMS         float64  `json:"download_ms"`
	ThroughputKbps     float64  `json:"throughput_kbps,omitempty"`
	CacheStatus        string   `json:"cache_status,omitempty"`
	CacheAge           *int     `json:"cache_age,omitempty"`
	IP                 string   `json:"ip,omitempty"`
	ActualDurationS    *float64 `json:"actual_duration_s,omitempty"`
	StartsWithKeyframe *bool    `json:"starts_with_keyframe,omitempty"`
	DurationDriftS     *float64 `json:"duration_drift_s,omitempty"`
	PTSStart           *int64   `json:"pts_start,omitempty"`
	PTSEnd             *int64   `json:"pts_end,omitempty"`
	Encryption         string   `json:"encryption,omitempty"`
	Error              string   `json:"error,omitempty"`
	ErrorKind          string   `json:"error_kind,omitempty"`
}

// CompareReport 是两路流的对比。
type CompareReport struct {
	Tool     string   `json:"tool"`
	Version  string   `json:"version"`
	A        *Report  `json:"a"`
	B        *Report  `json:"b"`
	Delta    Delta    `json:"delta"`
	Notes    []string `json:"notes"`
	ExitCode int      `json:"exit_code"`
}

// Delta 是 B 减 A。
type Delta struct {
	TTFBMS          float64  `json:"ttfb_ms"`
	ThroughputKbps  float64  `json:"throughput_kbps"`
	CacheHitRatePct *float64 `json:"cache_hit_rate_pct,omitempty"`
}

type observation struct {
	Result SegmentResult
	IP     string
	Header http.Header
	OK     bool
}

// Analyze 拉取播放列表和分片，返回质量报告。
func Analyze(ctx context.Context, rawURL string, opt Options) (*Report, error) {
	opt = opt.withDefaults()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, opt.Duration)
	defer cancel()
	started := time.Now()
	rep, err := newRunner(opt).analyze(ctx, rawURL, opt, started)
	if rep != nil {
		rep.DurationS = time.Since(started).Seconds()
	}
	return rep, err
}

// Manifest 只读取播放列表，不下载分片。
func Manifest(ctx context.Context, rawURL string, opt Options) (*Report, error) {
	opt = opt.withDefaults()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()
	started := time.Now()
	r := newRunner(opt)
	final, pl, res, err := r.loadPlaylist(ctx, rawURL)
	if err != nil {
		return nil, manifestErr(err)
	}
	rep := &Report{
		Tool:      ToolName,
		Version:   Version,
		Command:   "manifest",
		URL:       rawURL,
		Started:   started.UTC().Format(time.RFC3339),
		DurationS: time.Since(started).Seconds(),
		Playlist:  playlistInfo(pl),
		Variants:  absoluteVariantInfos(final, pl.Variants),
	}
	rep.Live = isLive(pl)
	rep.Playlist.Live = rep.Live
	rep.SchemaVersion = "1.0"
	rep.StreamType = streamType(pl)
	if res != nil {
		noteInfra(&rep.Infrastructure, res.Header)
	}
	return rep, nil
}

// Compare 用相同选项分析两路地址并比较汇总指标。
func Compare(ctx context.Context, left, right string, opt Options) (*CompareReport, error) {
	opt = opt.withDefaults()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, opt.Duration)
	defer cancel()
	a, err := Analyze(ctx, left, opt)
	if err != nil {
		return nil, err
	}
	b, err := Analyze(ctx, right, opt)
	if err != nil {
		return nil, err
	}
	notes, delta := compareNotes(a, b)
	return &CompareReport{
		Tool:     ToolName,
		Version:  Version,
		A:        a,
		B:        b,
		Delta:    delta,
		Notes:    notes,
		ExitCode: preferCode(a.ExitCode, b.ExitCode),
	}, nil
}

func (r *runner) analyze(ctx context.Context, rawURL string, opt Options, started time.Time) (*Report, error) {
	final, pl, res, err := r.loadPlaylist(ctx, rawURL)
	if err != nil {
		return nil, manifestErr(err)
	}
	if !pl.IsMaster() {
		if len(pl.Segments) == 0 {
			return nil, &ExitError{Code: 6, Msg: "媒体播放列表里没有分片"}
		}
		header := http.Header(nil)
		if res != nil {
			header = res.Header
		}
		return r.analyzeMedia(ctx, rawURL, final, pl, nil, header, opt, started)
	}
	picked, err := pickVariants(pl.Variants, opt)
	if err != nil {
		return nil, err
	}
	infos := absoluteVariantInfos(final, pl.Variants)
	var header http.Header
	if res != nil {
		header = res.Header
	}
	var results []Report
	for _, v := range picked {
		if ctx.Err() != nil {
			break
		}
		mediaURL, err := resolveURL(final, v.URI)
		if err != nil {
			return nil, manifestErr(err)
		}
		mediaFinal, mediaPL, _, err := r.loadPlaylist(ctx, mediaURL)
		if err != nil {
			return nil, manifestErr(err)
		}
		if len(mediaPL.Segments) == 0 {
			return nil, &ExitError{Code: 6, Msg: "媒体播放列表里没有分片"}
		}
		rep, err := r.analyzeMedia(ctx, rawURL, mediaFinal, mediaPL, pl.Variants, header, opt, time.Now())
		if err != nil {
			return nil, err
		}
		info := toInfo(v)
		info.URI = mediaURL
		rep.Variant = &info
		rep.Variants = infos
		results = append(results, *rep)
	}
	if len(results) == 0 {
		return nil, &ExitError{Code: 1, Msg: "在 --duration 内没有完成变体分析", Err: ctx.Err()}
	}
	if len(results) == 1 {
		results[0].Started = started.UTC().Format(time.RFC3339)
		return &results[0], nil
	}
	outer := &Report{
		Tool:          ToolName,
		Version:       Version,
		SchemaVersion: "1.0",
		Command:       "analyze",
		URL:           rawURL,
		Started:       started.UTC().Format(time.RFC3339),
		Variants:      infos,
		Results:       results,
	}
	for i := range results {
		outer.ExitCode = preferCode(outer.ExitCode, results[i].ExitCode)
	}
	return outer, nil
}

func (r *runner) analyzeMedia(ctx context.Context, pageURL, mediaURL string, pl *m3u8.Playlist, variants []m3u8.Variant, playlistHeader http.Header, opt Options, started time.Time) (*Report, error) {
	chosen := chooseSegments(pl, opt.Segments)
	inits := map[string][]byte{}
	keys := map[string][]byte{}
	var obs []observation
	for _, seg := range chosen {
		if ctx.Err() != nil {
			break
		}
		item := r.pullOne(ctx, mediaURL, seg, opt, inits, keys)
		obs = append(obs, item)
		if opt.OnSegment != nil {
			opt.OnSegment(item.Result)
		}
	}
	if len(obs) == 0 {
		return nil, &ExitError{Code: 1, Msg: "在 --duration 内没有拉到分片", Err: ctx.Err()}
	}
	rep := summarize("analyze", pageURL, opt, pl, obs, playlistHeader, variants)
	rep.Started = started.UTC().Format(time.RFC3339)
	rep.DurationS = time.Since(started).Seconds()
	applyThresholds(rep, opt)
	return rep, nil
}

func (r *runner) loadPlaylist(ctx context.Context, rawURL string) (string, *m3u8.Playlist, *fetched, error) {
	res := r.get(ctx, rawURL, maxPlaylist, hlsAccept, nil)
	if res.Err != nil || res.Status >= 400 {
		if res.Err == nil {
			res.Err = fmt.Errorf("HTTP %d", res.Status)
		}
		return rawURL, nil, res, res.Err
	}
	pl, err := m3u8.Parse(string(res.Body))
	if err != nil {
		return res.URL, nil, res, err
	}
	base := res.URL
	if base == "" {
		base = rawURL
	}
	return base, pl, res, nil
}

func (r *runner) pullOne(ctx context.Context, mediaURL string, seg m3u8.Segment, opt Options, inits map[string][]byte, keys map[string][]byte) observation {
	out := observation{Result: SegmentResult{Sequence: seg.Sequence, Discontinuity: seg.Discontinuity, Gap: seg.Gap, DeclaredDurationS: seg.Duration}}
	u, err := resolveURL(mediaURL, seg.URI)
	out.Result.URL = u
	if seg.Gap {
		return out
	}
	if err != nil {
		out.Result.Error = err.Error()
		out.Result.ErrorKind = "other"
		return out
	}
	res := r.get(ctx, u, maxSegment, "", seg.ByteRange)
	out.IP = res.IP
	out.Header = res.Header
	out.Result.HTTPStatus = res.Status
	out.Result.Bytes = len(res.Body)
	out.Result.RemoteAddr = res.Addr
	out.Result.TTFBMS = milliseconds(res.TTFB)
	out.Result.DownloadMS = milliseconds(res.Total)
	if opt.TraceIPs {
		out.Result.IP = res.IP
	}
	status, age := CacheInfo(res.Header)
	out.Result.CacheStatus = status
	out.Result.CacheAge = age
	if res.Err != nil {
		out.Result.Error = res.Err.Error()
		out.Result.ErrorKind = failureKind(res)
		return out
	}
	out.OK = true
	out.Result.ThroughputKbps = throughputKbps(len(res.Body), res.Total)
	if !opt.ParseSegments {
		return out
	}
	body := res.Body
	if seg.Key != nil && seg.Key.Method != "" && seg.Key.Method != "NONE" {
		out.Result.Encryption = seg.Key.Method
		if !strings.EqualFold(seg.Key.Method, "AES-128") {
			return out
		}
		key, err := r.loadKey(ctx, mediaURL, seg.Key, keys)
		if err != nil {
			out.OK = false
			out.Result.Error = "解密失败: " + err.Error()
			out.Result.ErrorKind = "other"
			out.Result.ThroughputKbps = 0
			return out
		}
		iv := seg.Key.IV
		if len(iv) == 0 {
			iv = media.IVForSequence(seg.Sequence)
		}
		plain, err := media.DecryptAES128(body, key, iv)
		if err != nil {
			out.OK = false
			out.Result.Error = "解密失败: " + err.Error()
			out.Result.ErrorKind = "other"
			out.Result.ThroughputKbps = 0
			return out
		}
		body = plain
	}
	init := r.loadInit(ctx, mediaURL, seg.Map, inits)
	info := media.Inspect(body, init)
	if info.Keyframe != nil {
		v := *info.Keyframe
		out.Result.StartsWithKeyframe = &v
	}
	if info.DurationKnown && seg.Duration > 0 {
		actual := info.Duration
		out.Result.ActualDurationS = &actual
		drift := seg.Duration - actual
		out.Result.DurationDriftS = &drift
	}
	if opt.ParseSegments {
		start, end := int64(-1), int64(-1)
		if info.PTSCount > 0 {
			start = int64(info.PTSStart)
			end = int64(info.PTSEnd)
		}
		out.Result.PTSStart = &start
		out.Result.PTSEnd = &end
	}
	return out
}

func (r *runner) loadInit(ctx context.Context, mediaURL string, m *m3u8.Map, cache map[string][]byte) []byte {
	if m == nil || m.URI == "" {
		return nil
	}
	u, err := resolveURL(mediaURL, m.URI)
	if err != nil {
		return nil
	}
	key := initCacheKey(u, m.ByteRange)
	if b, ok := cache[key]; ok {
		return b
	}
	res := r.get(ctx, u, maxSegment, "", m.ByteRange)
	if res.Err != nil || res.Status >= 400 {
		cache[key] = nil
		return nil
	}
	cache[key] = res.Body
	return res.Body
}

func initCacheKey(rawURL string, br *m3u8.ByteRange) string {
	if br == nil {
		return rawURL
	}
	return fmt.Sprintf("%s#%d+%d", rawURL, br.Offset, br.Length)
}

func (r *runner) loadKey(ctx context.Context, mediaURL string, key *m3u8.Key, cache map[string][]byte) ([]byte, error) {
	if key.URI == "" {
		return nil, errors.New("播放列表没有给出密钥地址")
	}
	u, err := resolveURL(mediaURL, key.URI)
	if err != nil {
		return nil, err
	}
	if b, ok := cache[u]; ok {
		if len(b) != 16 {
			return nil, errors.New("密钥不可用")
		}
		return b, nil
	}
	res := r.get(ctx, u, 4096, "", nil)
	if res.Err != nil {
		cache[u] = nil
		return nil, res.Err
	}
	if len(res.Body) < 16 {
		cache[u] = nil
		return nil, errors.New("密钥长度不是 16 字节")
	}
	cache[u] = append([]byte(nil), res.Body[:16]...)
	return cache[u], nil
}

func summarize(command, pageURL string, opt Options, pl *m3u8.Playlist, obs []observation, playlistHeader http.Header, variants []m3u8.Variant) *Report {
	rep := &Report{
		Tool:     ToolName,
		Version:  Version,
		Command:  command,
		URL:      pageURL,
		Live:     isLive(pl),
		Playlist: playlistInfo(pl),
	}
	var ttfbSum, downloadSum float64
	var ttfbN, downloadN int
	var rates []float64
	var hits, classified int
	var flips int
	var prevIP string
	seenIP := map[string]struct{}{}
	var badKey, unknownKey int
	var driftBad bool
	var query bool
	var vary string
	var blocked bool
	var sawEncryption bool

	for _, item := range obs {
		seg := item.Result
		if seg.Gap {
			rep.Summary.SegmentsSkipped++
			rep.Segments = append(rep.Segments, seg)
			continue
		}
		if strings.Contains(seg.URL, "?") {
			query = true
		}
		if item.IP != "" {
			if _, ok := seenIP[item.IP]; !ok {
				seenIP[item.IP] = struct{}{}
				rep.Infrastructure.IPs = append(rep.Infrastructure.IPs, item.IP)
			}
			if prevIP != "" && prevIP != item.IP {
				flips++
			}
			prevIP = item.IP
		}
		if item.Header != nil {
			noteInfra(&rep.Infrastructure, item.Header)
			if v := item.Header.Get("Vary"); interestingVary(v) && vary == "" {
				vary = v
			}
			if blocksCache(item.Header.Get("Cache-Control")) {
				blocked = true
			}
		}
		if item.OK {
			if seg.TTFBMS > 0 || seg.HTTPStatus > 0 {
				ttfbSum += seg.TTFBMS
				ttfbN++
				downloadSum += seg.DownloadMS
				downloadN++
			}
		}
		if seg.CacheStatus != "" {
			classified++
			if cacheHit(seg.CacheStatus) {
				hits++
			}
		}
		if seg.Encryption != "" {
			sawEncryption = true
		}
		if item.OK {
			rep.Summary.SegmentsFetched++
			if seg.ThroughputKbps > 0 {
				rates = append(rates, seg.ThroughputKbps)
			}
			if opt.ParseSegments {
				if seg.StartsWithKeyframe == nil {
					unknownKey++
				} else if !*seg.StartsWithKeyframe {
					badKey++
				}
				if seg.DurationDriftS != nil && abs(*seg.DurationDriftS) > 0.5 {
					driftBad = true
				}
			}
		} else {
			rep.Summary.SegmentsFailed++
			addBreakdown(&rep.Summary.ErrorBreakdown, seg.ErrorKind, seg.HTTPStatus)
		}
		rep.Segments = append(rep.Segments, seg)
	}
	total := rep.Summary.SegmentsFetched + rep.Summary.SegmentsFailed
	if total > 0 {
		rep.Summary.ErrorRatePct = 100 * float64(rep.Summary.SegmentsFailed) / float64(total)
	}
	if ttfbN > 0 {
		rep.Summary.AvgTTFBMS = ttfbSum / float64(ttfbN)
	}
	if downloadN > 0 {
		rep.Summary.AvgDownloadMS = downloadSum / float64(downloadN)
	}
	if len(rates) > 0 {
		var sum float64
		minRate, maxRate := rates[0], rates[0]
		for _, n := range rates {
			sum += n
			if n < minRate {
				minRate = n
			}
			if n > maxRate {
				maxRate = n
			}
		}
		rep.Summary.AvgThroughputKbps = sum / float64(len(rates))
		rep.Summary.MinThroughputKbps = minRate
		rep.Summary.MaxThroughputKbps = maxRate
		rep.Summary.P50ThroughputKbps = metrics.Quantile(rates, 0.50)
		rep.Summary.P95ThroughputKbps = metrics.Quantile(rates, 0.05)
	}
	if classified > 0 {
		rate := 100 * float64(hits) / float64(classified)
		rep.Summary.CacheHitRatePct = &rate
	}
	rep.Infrastructure.CacheHits = hits
	rep.Infrastructure.CacheMisses = classified - hits
	rep.Infrastructure.IPFlips = flips
	rep.Infrastructure.LoadBalancerDetected = len(rep.Infrastructure.IPs) > 1
	if rep.Infrastructure.CDNDetected == "" {
		noteInfra(&rep.Infrastructure, playlistHeader)
	}
	rep.SchemaVersion = "1.0"
	rep.StreamType = streamType(pl)
	rep.Summary.Discontinuities = countDiscontinuities(pl)
	rep.Summary.PTSContinuity = ptsContinuity(rep.Segments)

	var cacheW, playW, rec []string
	addCache := func(s string) {
		cacheW = append(cacheW, s)
		rec = append(rec, s)
	}
	addPlay := func(s string) {
		playW = append(playW, s)
		rec = append(rec, s)
	}
	if classified == 0 {
		addCache("响应里没有缓存状态头（例如 X-Cache、CF-Cache-Status），没法确认 CDN 是否命中")
	} else if hits == 0 {
		addCache("缓存命中率是 0%。检查源站 Cache-Control、分片 TTL，以及查询串和 Cookie 是否进了缓存键")
	} else if *rep.Summary.CacheHitRatePct < 80 {
		addCache(fmt.Sprintf("缓存命中率只有 %.0f%%，低于 80%%。检查缓存键和 TTL 是否把本可复用的分片打散了", *rep.Summary.CacheHitRatePct))
	}
	if blocked {
		addCache("分片带了 no-store、no-cache 或 max-age=0，CDN 可能无法缓存")
	}
	if vary != "" {
		addCache("响应 Vary 含有 " + vary + "，不同请求头会被分成不同缓存")
	}
	if query {
		addCache("分片 URL 带查询串。确认这些参数确实需要参与缓存键，否则命中率会被打散")
	}
	if opt.ParseSegments && badKey > 0 {
		msg := fmt.Sprintf("%d 个分片开头不是关键帧，切过去时可能先黑一会儿。把关键帧间隔调成和分片时长一致", badKey)
		if unknownKey > 0 {
			msg += fmt.Sprintf("；另有 %d 个分片没法判断", unknownKey)
		}
		addPlay(msg)
	} else if opt.ParseSegments && unknownKey > 0 && sawEncryption {
		addPlay("有分片使用了未能解析的加密方式，没有判断关键帧")
	}
	if driftBad {
		addPlay("有分片的实际时长和 EXTINF 相差超过 0.5 秒，播放器时间轴可能对不齐")
	}
	if pl != nil && pl.TargetDuration > 0 {
		for _, seg := range pl.Segments {
			if seg.Duration > pl.TargetDuration+0.01 {
				addPlay(fmt.Sprintf("有分片时长 %.2fs，超过了 EXT-X-TARGETDURATION %.0fs", seg.Duration, pl.TargetDuration))
				break
			}
		}
	}
	if flips >= 3 {
		rec = append(rec, fmt.Sprintf("解析到的 IP 变了 %d 次，DNS TTL 或负载均衡可能不稳定", flips))
	}
	if len(variants) > 0 && rep.Summary.AvgThroughputKbps > 0 {
		rep.ABR = buildABR(variants, rep.Summary.AvgThroughputKbps, rep.Summary.P95ThroughputKbps)
		if rep.ABR != nil && rep.ABR.Recommended == "" {
			rec = append(rec, "按最差 5% 的吞吐看，现有档位余量都不够，播放时容易重新缓冲")
		}
	}
	rep.Infrastructure.CacheWarnings = cacheW
	rep.Infrastructure.PlaylistWarnings = playW
	rep.Recommendations = rec
	return rep
}

func applyThresholds(rep *Report, opt Options) {
	if rep.Summary.SegmentsFetched+rep.Summary.SegmentsFailed == 0 {
		return
	}
	code := 0
	if opt.ErrorThreshold > 0 && rep.Summary.ErrorRatePct > opt.ErrorThreshold {
		code = preferCode(code, 2)
	}
	if opt.LatencyThreshold > 0 && rep.Summary.AvgTTFBMS > opt.LatencyThreshold {
		code = preferCode(code, 3)
	}
	if opt.ThroughputThreshold > 0 && rep.Summary.SegmentsFetched > 0 && rep.Summary.AvgThroughputKbps < opt.ThroughputThreshold {
		code = preferCode(code, 4)
	}
	rep.ExitCode = preferCode(rep.ExitCode, code)
}

func buildABR(variants []m3u8.Variant, avgKbps, p95Kbps float64) *ABRReport {
	type item struct {
		v    m3u8.Variant
		kbps int
	}
	var items []item
	for _, v := range variants {
		if v.IFrame || v.Bandwidth <= 0 {
			continue
		}
		items = append(items, item{v: v, kbps: v.Bandwidth / 1000})
	}
	if len(items) == 0 || avgKbps <= 0 {
		return nil
	}
	rep := &ABRReport{MeasuredBandwidthKbps: avgKbps, P95BandwidthKbps: p95Kbps}
	var recKbps, safeKbps int
	for _, it := range items {
		if it.kbps <= 0 {
			continue
		}
		head := avgKbps / float64(it.kbps)
		p95h := 0.0
		if p95Kbps > 0 {
			p95h = p95Kbps / float64(it.kbps)
		}
		risk := riskOf(p95h)
		label := variantLabel(it.v)
		rep.Variants = append(rep.Variants, ABRVariant{
			Label:         label,
			BandwidthKbps: it.kbps,
			Headroom:      head,
			P95Headroom:   p95h,
			Risk:          risk,
		})
		line := fmt.Sprintf("%s @ %d kbps", label, it.kbps)
		if risk != "HIGH" && it.kbps >= recKbps {
			rep.Recommended = line
			recKbps = it.kbps
		}
		if risk == "NONE" && it.kbps >= safeKbps {
			rep.SafeFallback = line
			safeKbps = it.kbps
		}
	}
	if len(rep.Variants) == 0 {
		return nil
	}
	return rep
}

func riskOf(p95Headroom float64) string {
	switch {
	case p95Headroom >= 3:
		return "NONE"
	case p95Headroom >= 2:
		return "LOW"
	case p95Headroom >= 1.2:
		return "MEDIUM"
	default:
		return "HIGH"
	}
}

func pickVariants(vs []m3u8.Variant, opt Options) ([]m3u8.Variant, error) {
	var list []m3u8.Variant
	for _, v := range vs {
		if !v.IFrame {
			list = append(list, v)
		}
	}
	if len(list) == 0 {
		return nil, &ExitError{Code: 5, Msg: "主播放列表里没有可播放的变体"}
	}
	if opt.AllVariants {
		return list, nil
	}
	idx := -1
	if opt.VariantSet {
		idx = opt.Variant
	}
	sorted := append([]m3u8.Variant(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Bandwidth < sorted[j].Bandwidth })
	if idx < 0 {
		return []m3u8.Variant{sorted[len(sorted)-1]}, nil
	}
	if idx >= len(sorted) {
		return nil, &ExitError{Code: 1, Msg: fmt.Sprintf("变体序号 %d 超出范围（共 %d 个，按码率从低到高，-1 表示最高）", idx, len(sorted))}
	}
	return []m3u8.Variant{sorted[idx]}, nil
}

func chooseSegments(pl *m3u8.Playlist, n int) []m3u8.Segment {
	if n <= 0 || len(pl.Segments) <= n {
		return append([]m3u8.Segment(nil), pl.Segments...)
	}
	if isLive(pl) {
		return append([]m3u8.Segment(nil), pl.Segments[len(pl.Segments)-n:]...)
	}
	return append([]m3u8.Segment(nil), pl.Segments[:n]...)
}

func isLive(pl *m3u8.Playlist) bool {
	if pl == nil || pl.IsMaster() {
		return false
	}
	return !pl.EndList && !strings.EqualFold(pl.Type, "VOD")
}

func playlistInfo(pl *m3u8.Playlist) PlaylistInfo {
	info := PlaylistInfo{Live: isLive(pl)}
	if pl == nil {
		return info
	}
	info.Version = pl.Version
	info.Type = pl.Type
	info.TargetDurationS = pl.TargetDuration
	info.MediaSequence = pl.MediaSequence
	info.SegmentCount = len(pl.Segments)
	info.IndependentSegments = pl.IndependentSegments
	for _, seg := range pl.Segments {
		if seg.Key != nil && seg.Key.Method != "" && !strings.EqualFold(seg.Key.Method, "NONE") {
			info.Encrypted = true
			break
		}
	}
	return info
}

func variantInfos(vs []m3u8.Variant) []VariantInfo {
	return absoluteVariantInfos("", vs)
}

func absoluteVariantInfos(base string, vs []m3u8.Variant) []VariantInfo {
	out := make([]VariantInfo, 0, len(vs))
	for _, v := range vs {
		info := toInfo(v)
		if base != "" {
			if abs, err := resolveURL(base, v.URI); err == nil {
				info.URI = abs
			}
		}
		out = append(out, info)
	}
	return out
}

func toInfo(v m3u8.Variant) VariantInfo {
	return VariantInfo{
		URI:              v.URI,
		Bandwidth:        v.Bandwidth,
		AverageBandwidth: v.AverageBandwidth,
		Resolution:       v.Resolution,
		Codecs:           v.Codecs,
		FrameRate:        v.FrameRate,
		Name:             v.Name,
	}
}

func variantLabel(v m3u8.Variant) string {
	if v.Resolution != "" {
		return v.Resolution
	}
	if v.Name != "" {
		return v.Name
	}
	if v.Codecs != "" {
		return v.Codecs
	}
	return v.URI
}

func compareNotes(a, b *Report) ([]string, Delta) {
	pa, pb := primary(a), primary(b)
	delta := Delta{
		TTFBMS:         pb.Summary.AvgTTFBMS - pa.Summary.AvgTTFBMS,
		ThroughputKbps: pb.Summary.AvgThroughputKbps - pa.Summary.AvgThroughputKbps,
	}
	notes := []string{
		fmt.Sprintf("流 B 的 TTFB 比流 A %s", signedMS(delta.TTFBMS)),
		fmt.Sprintf("流 B 的吞吐比流 A %s", signedRate(delta.ThroughputKbps)),
	}
	if pa.Summary.CacheHitRatePct != nil && pb.Summary.CacheHitRatePct != nil {
		diff := *pb.Summary.CacheHitRatePct - *pa.Summary.CacheHitRatePct
		delta.CacheHitRatePct = &diff
		if abs(diff) >= 0.5 {
			notes = append(notes, fmt.Sprintf("流 B 的缓存命中率比流 A %s", signedPct(diff)))
		}
	}
	return notes, delta
}

func primary(rep *Report) *Report {
	if rep == nil {
		return &Report{}
	}
	if rep.Summary.SegmentsFetched+rep.Summary.SegmentsFailed == 0 && len(rep.Results) > 0 {
		return &rep.Results[0]
	}
	return rep
}

func streamType(pl *m3u8.Playlist) string {
	if pl == nil {
		return ""
	}
	if pl.Type != "" {
		return strings.ToUpper(pl.Type)
	}
	if pl.EndList || !isLive(pl) {
		return "VOD"
	}
	return "LIVE"
}

func countDiscontinuities(pl *m3u8.Playlist) int {
	if pl == nil {
		return 0
	}
	n := 0
	for _, seg := range pl.Segments {
		if seg.Discontinuity {
			n++
		}
	}
	return n
}

// ptsContinuitySlack 是相邻分片 PTS 允许的偏差，单位是 90 kHz tick。
// 上一片的 PTSEnd 是最后一帧的时间戳，下一片的 PTSStart 通常再晚一帧
// （30fps 大约 3000 tick）。B 帧重排还会再偏几帧，所以用 0.5 秒作为断开阈值。
const ptsContinuitySlack int64 = 45000

func ptsContinuity(segs []SegmentResult) string {
	var prev int64
	var have bool
	checked := 0
	for _, seg := range segs {
		if seg.Discontinuity {
			have = false
		}
		if seg.PTSStart == nil || *seg.PTSStart < 0 || seg.PTSEnd == nil || *seg.PTSEnd < 0 {
			have = false
			continue
		}
		if have {
			if !ptsAdjacent(prev, *seg.PTSStart) {
				return "ISSUES"
			}
			checked++
		}
		prev = *seg.PTSEnd
		have = true
	}
	if checked == 0 {
		return ""
	}
	return "OK"
}

func ptsAdjacent(prevEnd, nextStart int64) bool {
	const mod int64 = 1 << 33
	gap := nextStart - prevEnd
	if gap > mod/2 {
		gap -= mod
	} else if gap < -mod/2 {
		gap += mod
	}
	return gap >= -ptsContinuitySlack && gap <= ptsContinuitySlack
}

func noteInfra(infra *Infrastructure, h http.Header) {
	if infra == nil || h == nil {
		return
	}
	if infra.CDNDetected == "" {
		infra.CDNDetected = DetectCDN(h)
	}
	if infra.EdgeLocation == "" {
		infra.EdgeLocation = edgeLocation(h)
	}
	if infra.CDNHeaders == nil {
		infra.CDNHeaders = map[string]string{}
	}
	for _, key := range []string{"X-Served-By", "Cf-Ray", "X-Amz-Cf-Pop", "X-Timer"} {
		if v := h.Get(key); v != "" && infra.CDNHeaders[key] == "" {
			infra.CDNHeaders[key] = v
		}
	}
}

func edgeLocation(h http.Header) string {
	if pop := h.Get("X-Amz-Cf-Pop"); len(pop) >= 3 {
		return strings.ToLower(pop[:3])
	}
	served := strings.ToLower(h.Get("X-Served-By"))
	if rest, ok := strings.CutPrefix(served, "cache-"); ok {
		pop, _, _ := strings.Cut(rest, "-")
		return pop
	}
	return ""
}

func manifestErr(err error) error {
	var ee *ExitError
	if errors.As(err, &ee) {
		return err
	}
	return &ExitError{Code: 5, Msg: "无法获取或解析播放列表: " + err.Error(), Err: err}
}

func preferCode(a, b int) int {
	if rank(b) > rank(a) {
		return b
	}
	return a
}

func rank(code int) int {
	switch code {
	case 0:
		return 0
	case 4:
		return 1
	case 3:
		return 2
	case 2:
		return 3
	case 6:
		return 4
	case 1:
		return 5
	case 5:
		return 6
	default:
		return 7
	}
}

func failureKind(res *fetched) string {
	if res.Status >= 400 {
		return "http"
	}
	if res.Kind != "" {
		return res.Kind
	}
	return "other"
}

func addBreakdown(b *ErrorBreakdown, kind string, status int) {
	switch {
	case status >= 500:
		b.HTTP5xx++
	case status >= 400:
		b.HTTP4xx++
	case kind == "timeout":
		b.Timeouts++
	case kind == "eof":
		b.ShortReads++
	default:
		b.Other++
	}
}

func resolveURL(base, ref string) (string, error) {
	bu, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	ru, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return "", err
	}
	return bu.ResolveReference(ru).String(), nil
}

func interestingVary(v string) bool {
	if strings.TrimSpace(v) == "" {
		return false
	}
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" || strings.EqualFold(part, "Accept-Encoding") || part == "*" {
			continue
		}
		return true
	}
	return false
}

func blocksCache(cc string) bool {
	cc = strings.ToLower(cc)
	return strings.Contains(cc, "no-store") || strings.Contains(cc, "no-cache") || strings.Contains(cc, "max-age=0")
}

func throughputKbps(n int, d time.Duration) float64 {
	if n <= 0 || d <= 0 {
		return 0
	}
	return float64(n) * 8 / d.Seconds() / 1000
}

func milliseconds(d time.Duration) float64 {
	return float64(d.Round(time.Millisecond) / time.Millisecond)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func signedMS(v float64) string {
	switch {
	case v > 0.5:
		return fmt.Sprintf("高 %.0f ms", v)
	case v < -0.5:
		return fmt.Sprintf("低 %.0f ms", -v)
	default:
		return "基本相同"
	}
}

func signedRate(kbps float64) string {
	switch {
	case kbps > 1:
		return "高 " + formatRate(kbps)
	case kbps < -1:
		return "低 " + formatRate(-kbps)
	default:
		return "基本相同"
	}
}

func signedPct(v float64) string {
	switch {
	case v > 0.05:
		return fmt.Sprintf("高 %.0f 个百分点", v)
	case v < -0.05:
		return fmt.Sprintf("低 %.0f 个百分点", -v)
	default:
		return "基本相同"
	}
}
