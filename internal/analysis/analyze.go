// Package analysis turns the per-frame rows collected by a task into
// diagnostics: what is wrong with the stream, not just what it contains.
package analysis

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"streamanalyzer/internal/models"
)

// Frame is one collected media frame, as read back from a task's workbook.
type Frame struct {
	IsVideo   bool
	DTS       int64
	PTS       int64
	CTS       int64
	Len       int64
	FrameType string
	IsKey     bool
	NALUTypes string
	// ArrivalMs is when the analyzer received the frame (unix millis, 0 unknown).
	ArrivalMs int64
}

// Thresholds for the diagnostics below. They are deliberately loose: this tool
// reports on live streams from many encoders, so only clear defects should fire.
const (
	dtsGapFactor       = 4    // gap vs. median frame interval
	dtsGapMinMs        = 1000 // never flag gaps below this
	stallMs            = 3000 // no frame arrived for this long
	avSyncWarnMs       = 200  // |video DTS - audio DTS|
	avSyncErrorMs      = 500
	gopJitterRatioWarn = 0.25 // stddev/mean of I-frame intervals
	keyframeGapWarnMs  = 10000
	bitratePeakWarn    = 3.0 // peak second / average second
	jitterWarnMs       = 100
)

// Result carries everything the API adds on top of the raw chart arrays.
type Result struct {
	Health    *models.StreamHealth
	AVSync    []models.AVSyncPoint
	Jitter    []models.JitterPoint
	FrameKind []models.FrameKindStat
	NALU      []models.NALUStat
}

// Analyze inspects the frames of one task. secondStats supplies the per-second
// byte/FPS aggregates already computed by the caller.
func Analyze(frames []Frame, secondStats []*models.SecondStat) *Result {
	res := &Result{Health: &models.StreamHealth{}}
	if len(frames) == 0 {
		res.Health.Level = "warn"
		res.Health.Summary = "尚未采集到音视频数据"
		return res
	}

	var video, audio []Frame
	for _, f := range frames {
		if f.IsVideo {
			video = append(video, f)
		} else {
			audio = append(audio, f)
		}
	}

	alerts := make([]models.Alert, 0, 8)
	h := res.Health

	// Realtime-ness: a stream pulled from a static file arrives far faster than
	// its own timeline, which would make jitter/stall checks meaningless.
	realtime := isRealtimePaced(video)

	alerts = append(alerts, checkTimestamps(video, "视频")...)
	alerts = append(alerts, checkTimestamps(audio, "音频")...)
	alerts = append(alerts, checkPTSvsDTS(video)...)
	alerts = append(alerts, checkPresence(video, audio)...)

	res.AVSync = buildAVSync(video, audio)
	if a, maxDiff := checkAVSync(res.AVSync); a != nil {
		alerts = append(alerts, *a)
		h.AVSyncMaxMs = maxDiff
	} else {
		h.AVSyncMaxMs = maxAbsDiff(res.AVSync)
	}

	if realtime {
		res.Jitter = buildJitter(video)
		h.JitterAvgMs, h.JitterMaxMs = jitterSummary(res.Jitter)
		if h.JitterAvgMs > jitterWarnMs {
			alerts = append(alerts, models.Alert{
				Severity: "warn", Code: "jitter_high", Title: "帧到达抖动偏大",
				Detail: fmt.Sprintf("平均抖动 %.0f ms（峰值 %.0f ms），超过 %d ms，播放端可能需要更大缓冲。",
					h.JitterAvgMs, h.JitterMaxMs, jitterWarnMs),
			})
		}
		if a := checkStall(video); a != nil {
			alerts = append(alerts, *a)
		}
	} else {
		alerts = append(alerts, models.Alert{
			Severity: "info", Code: "not_realtime", Title: "非实时速率拉流",
			Detail: "数据到达速度明显快于流自身时间轴（常见于点播文件/秒开回放），因此跳过抖动与卡顿判定。",
		})
	}

	// Encoding-layer stats.
	res.FrameKind = buildFrameKinds(video)
	res.NALU = buildNALUStats(video)
	if a := checkGOP(video, h); a != nil {
		alerts = append(alerts, *a)
	}
	if a := checkBitrate(secondStats, h); a != nil {
		alerts = append(alerts, *a)
	}
	fillAverages(h, secondStats, video)

	if hasB := hasBFrames(res.FrameKind); hasB {
		alerts = append(alerts, models.Alert{
			Severity: "info", Code: "b_frames", Title: "存在 B 帧",
			Detail: "码流包含 B 帧（由 slice_type 判定）。B 帧会引入重排序延迟，低延迟直播场景通常建议关闭。",
		})
	}

	sortAlerts(alerts)
	h.Alerts = alerts
	h.Score, h.Level, h.Summary = score(alerts)
	return res
}

// isRealtimePaced reports whether frames arrived at roughly the pace of their
// own timestamps, which is what a live pull looks like.
func isRealtimePaced(video []Frame) bool {
	if len(video) < 10 {
		return false
	}
	first, last := video[0], video[len(video)-1]
	if first.ArrivalMs == 0 || last.ArrivalMs == 0 {
		return false
	}
	dtsSpan := last.DTS - first.DTS
	arrSpan := last.ArrivalMs - first.ArrivalMs
	if dtsSpan <= 0 {
		return false
	}
	return float64(arrSpan) >= 0.5*float64(dtsSpan)
}

func checkTimestamps(frames []Frame, label string) []models.Alert {
	if len(frames) < 2 {
		return nil
	}

	median := medianDelta(frames)
	gapLimit := int64(dtsGapFactor) * median
	if gapLimit < dtsGapMinMs {
		gapLimit = dtsGapMinMs
	}

	var backwards, dup, gaps int
	var firstBackDTS, firstGapDTS, maxGap int64
	for i := 1; i < len(frames); i++ {
		d := frames[i].DTS - frames[i-1].DTS
		switch {
		case d < 0:
			backwards++
			if firstBackDTS == 0 {
				firstBackDTS = frames[i].DTS
			}
		case d == 0:
			dup++
		case d > gapLimit:
			gaps++
			if firstGapDTS == 0 {
				firstGapDTS = frames[i].DTS
			}
			if d > maxGap {
				maxGap = d
			}
		}
	}

	var out []models.Alert
	if backwards > 0 {
		out = append(out, models.Alert{
			Severity: "error", Code: "dts_backwards", Title: label + " DTS 回退",
			Detail: fmt.Sprintf("检测到 %d 次 DTS 比上一帧更小，播放器可能丢帧或卡顿。", backwards),
			Count:  backwards, AtDTS: firstBackDTS,
		})
	}
	if gaps > 0 {
		out = append(out, models.Alert{
			Severity: "warn", Code: "dts_gap", Title: label + " 时间戳跳变",
			Detail: fmt.Sprintf("检测到 %d 次异常间隔（最大 %d ms，正常约 %d ms），可能存在丢帧或推流端卡顿。",
				gaps, maxGap, median),
			Count: gaps, AtDTS: firstGapDTS,
		})
	}
	if dup > len(frames)/10 && dup > 5 {
		out = append(out, models.Alert{
			Severity: "warn", Code: "dts_duplicate", Title: label + " 时间戳重复",
			Detail: fmt.Sprintf("%d 帧与前一帧 DTS 相同，时间戳精度或推流端打时间戳可能有问题。", dup),
			Count:  dup,
		})
	}
	return out
}

func checkPTSvsDTS(video []Frame) []models.Alert {
	var bad int
	var firstDTS int64
	for _, f := range video {
		if f.PTS < f.DTS {
			bad++
			if firstDTS == 0 {
				firstDTS = f.DTS
			}
		}
	}
	if bad == 0 {
		return nil
	}
	return []models.Alert{{
		Severity: "error", Code: "pts_lt_dts", Title: "PTS 小于 DTS",
		Detail: fmt.Sprintf("%d 帧的 PTS 小于 DTS（CTS 为负），时间戳计算有误，解码端可能异常。", bad),
		Count:  bad, AtDTS: firstDTS,
	}}
}

func checkPresence(video, audio []Frame) []models.Alert {
	var out []models.Alert
	if len(video) == 0 {
		out = append(out, models.Alert{
			Severity: "warn", Code: "no_video", Title: "无视频数据",
			Detail: "整个采集期间未收到视频帧。",
		})
	}
	if len(audio) == 0 {
		out = append(out, models.Alert{
			Severity: "warn", Code: "no_audio", Title: "无音频数据",
			Detail: "整个采集期间未收到音频帧，可能是纯视频流或推流端未推音频。",
		})
	}
	return out
}

func checkStall(video []Frame) *models.Alert {
	var stalls int
	var maxGap, firstDTS int64
	for i := 1; i < len(video); i++ {
		if video[i].ArrivalMs == 0 || video[i-1].ArrivalMs == 0 {
			continue
		}
		gap := video[i].ArrivalMs - video[i-1].ArrivalMs
		if gap > stallMs {
			stalls++
			if firstDTS == 0 {
				firstDTS = video[i].DTS
			}
			if gap > maxGap {
				maxGap = gap
			}
		}
	}
	if stalls == 0 {
		return nil
	}
	return &models.Alert{
		Severity: "error", Code: "stream_stall", Title: "拉流中断/卡顿",
		Detail: fmt.Sprintf("%d 次超过 %d ms 没有收到视频帧（最长 %d ms），观众端会明显卡顿。",
			stalls, stallMs, maxGap),
		Count: stalls, AtDTS: firstDTS,
	}
}

// buildAVSync tracks video-minus-audio DTS per arrival second, showing whether
// the two tracks drift apart over time.
func buildAVSync(video, audio []Frame) []models.AVSyncPoint {
	if len(video) == 0 || len(audio) == 0 {
		return nil
	}

	lastV := map[int]int64{}
	lastA := map[int]int64{}
	secs := map[int]bool{}
	base := baseSecond(video, audio)
	if base == 0 {
		return nil
	}

	for _, f := range video {
		if f.ArrivalMs == 0 {
			continue
		}
		s := int(f.ArrivalMs/1000 - base)
		lastV[s] = f.DTS
		secs[s] = true
	}
	for _, f := range audio {
		if f.ArrivalMs == 0 {
			continue
		}
		s := int(f.ArrivalMs/1000 - base)
		lastA[s] = f.DTS
		secs[s] = true
	}

	out := make([]models.AVSyncPoint, 0, len(secs))
	for s := range secs {
		v, okV := lastV[s]
		a, okA := lastA[s]
		if !okV || !okA {
			continue
		}
		out = append(out, models.AVSyncPoint{Second: s, DiffMs: v - a})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Second < out[j].Second })
	return out
}

func baseSecond(video, audio []Frame) int64 {
	var base int64
	for _, set := range [][]Frame{video, audio} {
		for _, f := range set {
			if f.ArrivalMs == 0 {
				continue
			}
			s := f.ArrivalMs / 1000
			if base == 0 || s < base {
				base = s
			}
			break
		}
	}
	return base
}

func checkAVSync(points []models.AVSyncPoint) (*models.Alert, int64) {
	if len(points) == 0 {
		return nil, 0
	}
	maxAbs := maxAbsDiff(points)
	switch {
	case maxAbs >= avSyncErrorMs:
		return &models.Alert{
			Severity: "error", Code: "av_desync", Title: "音视频不同步",
			Detail: fmt.Sprintf("音视频时间戳最大相差 %d ms（阈值 %d ms），观众会察觉到唇音不同步。",
				maxAbs, avSyncErrorMs),
		}, maxAbs
	case maxAbs >= avSyncWarnMs:
		return &models.Alert{
			Severity: "warn", Code: "av_drift", Title: "音视频偏差偏大",
			Detail: fmt.Sprintf("音视频时间戳最大相差 %d ms（超过 %d ms），建议关注是否持续扩大。",
				maxAbs, avSyncWarnMs),
		}, maxAbs
	}
	return nil, maxAbs
}

func maxAbsDiff(points []models.AVSyncPoint) int64 {
	var m int64
	for _, p := range points {
		d := p.DiffMs
		if d < 0 {
			d = -d
		}
		if d > m {
			m = d
		}
	}
	return m
}

// buildJitter follows RFC 3550's interarrival jitter: it compares how far apart
// frames arrived against how far apart their timestamps say they should be.
func buildJitter(video []Frame) []models.JitterPoint {
	if len(video) < 2 {
		return nil
	}
	out := make([]models.JitterPoint, 0, len(video))
	var j float64
	for i := 1; i < len(video); i++ {
		prev, cur := video[i-1], video[i]
		if prev.ArrivalMs == 0 || cur.ArrivalMs == 0 {
			continue
		}
		arrDelta := float64(cur.ArrivalMs - prev.ArrivalMs)
		d := arrDelta - float64(cur.DTS-prev.DTS)
		j += (math.Abs(d) - j) / 16
		out = append(out, models.JitterPoint{
			Index: i, DTS: cur.DTS, JitterMs: round2(j), ArrivalDeltaMs: round2(arrDelta),
		})
	}
	return out
}

func jitterSummary(points []models.JitterPoint) (avg, max float64) {
	if len(points) == 0 {
		return 0, 0
	}
	var sum float64
	for _, p := range points {
		sum += p.JitterMs
		if p.JitterMs > max {
			max = p.JitterMs
		}
	}
	return round2(sum / float64(len(points))), round2(max)
}

func buildFrameKinds(video []Frame) []models.FrameKindStat {
	order := []string{"I", "P", "B"}
	stats := map[string]*models.FrameKindStat{}
	for _, f := range video {
		kind := f.FrameType
		if kind == "" || kind == "H" {
			continue
		}
		if _, ok := stats[kind]; !ok {
			stats[kind] = &models.FrameKindStat{Kind: kind}
			if kind != "I" && kind != "P" && kind != "B" {
				order = append(order, kind)
			}
		}
		s := stats[kind]
		s.Count++
		s.TotalBytes += f.Len
		if f.Len > s.MaxBytes {
			s.MaxBytes = f.Len
		}
	}

	out := make([]models.FrameKindStat, 0, len(stats))
	for _, kind := range order {
		s, ok := stats[kind]
		if !ok {
			continue
		}
		if s.Count > 0 {
			s.AvgBytes = round2(float64(s.TotalBytes) / float64(s.Count))
		}
		out = append(out, *s)
	}
	return out
}

func hasBFrames(kinds []models.FrameKindStat) bool {
	for _, k := range kinds {
		if k.Kind == "B" && k.Count > 0 {
			return true
		}
	}
	return false
}

func buildNALUStats(video []Frame) []models.NALUStat {
	counts := map[string]int{}
	for _, f := range video {
		if f.NALUTypes == "" {
			continue
		}
		for _, part := range strings.Split(f.NALUTypes, ";") {
			name, n, ok := parseNALUPart(part)
			if !ok {
				continue
			}
			counts[name] += n
		}
	}
	if len(counts) == 0 {
		return nil
	}

	out := make([]models.NALUStat, 0, len(counts))
	for name, c := range counts {
		out = append(out, models.NALUStat{Name: name, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func parseNALUPart(part string) (string, int, bool) {
	part = strings.TrimSpace(part)
	idx := strings.LastIndex(part, ":")
	if idx <= 0 || idx == len(part)-1 {
		return "", 0, false
	}
	name := part[:idx]
	var n int
	if _, err := fmt.Sscanf(part[idx+1:], "%d", &n); err != nil {
		return "", 0, false
	}
	return name, n, true
}

func checkGOP(video []Frame, h *models.StreamHealth) *models.Alert {
	var intervals []float64
	var lastKey int64 = -1
	for _, f := range video {
		if !f.IsKey {
			continue
		}
		if lastKey >= 0 {
			intervals = append(intervals, float64(f.DTS-lastKey))
		}
		lastKey = f.DTS
	}
	if len(intervals) == 0 {
		if len(video) > 0 {
			span := video[len(video)-1].DTS - video[0].DTS
			if span > keyframeGapWarnMs {
				return &models.Alert{
					Severity: "warn", Code: "no_keyframe", Title: "长时间无关键帧",
					Detail: fmt.Sprintf("%d ms 内没有出现关键帧，新观众将无法快速起播。", span),
				}
			}
		}
		return nil
	}

	mean, std := meanStd(intervals)
	h.GOPAvgMs = round2(mean)
	h.GOPJitterMs = round2(std)

	var maxIv float64
	for _, v := range intervals {
		if v > maxIv {
			maxIv = v
		}
	}
	if maxIv > keyframeGapWarnMs {
		return &models.Alert{
			Severity: "warn", Code: "keyframe_gap", Title: "关键帧间隔过大",
			Detail: fmt.Sprintf("最大 I 帧间隔 %.0f ms（平均 %.0f ms），超过 %d ms 会拖慢起播与拉流秒开。",
				maxIv, mean, keyframeGapWarnMs),
		}
	}
	if mean > 0 && std/mean > gopJitterRatioWarn {
		return &models.Alert{
			Severity: "warn", Code: "gop_unstable", Title: "GOP 不稳定",
			Detail: fmt.Sprintf("I 帧间隔波动较大（平均 %.0f ms，标准差 %.0f ms），编码器 GOP 设置可能不固定。",
				mean, std),
		}
	}
	return nil
}

func checkBitrate(stats []*models.SecondStat, h *models.StreamHealth) *models.Alert {
	if len(stats) < 3 {
		return nil
	}
	var sum, peak float64
	for _, s := range stats {
		total := float64(s.VideoBytes+s.AudioBytes) * 8 / 1000 // kbps
		sum += total
		if total > peak {
			peak = total
		}
	}
	avg := sum / float64(len(stats))
	if avg <= 0 {
		return nil
	}
	ratio := peak / avg
	h.BitratePeakRatio = round2(ratio)
	if ratio > bitratePeakWarn {
		return &models.Alert{
			Severity: "warn", Code: "bitrate_spike", Title: "码率波动剧烈",
			Detail: fmt.Sprintf("峰值码率 %.0f kbps 是平均值 %.0f kbps 的 %.1f 倍，可能造成网络拥塞与卡顿。",
				peak, avg, ratio),
		}
	}
	return nil
}

func fillAverages(h *models.StreamHealth, stats []*models.SecondStat, video []Frame) {
	if len(stats) == 0 {
		return
	}
	var vBytes, aBytes, vFrames int64
	for _, s := range stats {
		vBytes += s.VideoBytes
		aBytes += s.AudioBytes
		vFrames += int64(s.VideoFPS)
	}
	n := float64(len(stats))
	h.VideoBitrateAvg = round2(float64(vBytes) * 8 / 1000 / n)
	h.AudioBitrateAvg = round2(float64(aBytes) * 8 / 1000 / n)
	h.VideoFPSAvg = round2(float64(vFrames) / n)
}

func score(alerts []models.Alert) (int, string, string) {
	s := 100
	var errs, warns int
	for _, a := range alerts {
		switch a.Severity {
		case "error":
			s -= 25
			errs++
		case "warn":
			s -= 10
			warns++
		}
	}
	if s < 0 {
		s = 0
	}

	var level, summary string
	switch {
	case errs > 0:
		level = "bad"
		summary = fmt.Sprintf("发现 %d 个严重问题、%d 个警告，建议优先排查严重项。", errs, warns)
	case warns > 0:
		level = "warn"
		summary = fmt.Sprintf("未发现严重问题，但有 %d 个警告值得关注。", warns)
	default:
		level = "good"
		summary = "未发现明显异常，时间戳、GOP、码率与音视频同步均在正常范围。"
	}
	return s, level, summary
}

func sortAlerts(alerts []models.Alert) {
	rank := map[string]int{"error": 0, "warn": 1, "info": 2}
	sort.SliceStable(alerts, func(i, j int) bool {
		return rank[alerts[i].Severity] < rank[alerts[j].Severity]
	})
}

func medianDelta(frames []Frame) int64 {
	if len(frames) < 2 {
		return 0
	}
	deltas := make([]float64, 0, len(frames)-1)
	for i := 1; i < len(frames); i++ {
		d := frames[i].DTS - frames[i-1].DTS
		if d > 0 {
			deltas = append(deltas, float64(d))
		}
	}
	if len(deltas) == 0 {
		return 0
	}
	sort.Float64s(deltas)
	return int64(deltas[len(deltas)/2])
}

func meanStd(v []float64) (float64, float64) {
	if len(v) == 0 {
		return 0, 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	mean := sum / float64(len(v))
	var sq float64
	for _, x := range v {
		sq += (x - mean) * (x - mean)
	}
	return mean, math.Sqrt(sq / float64(len(v)))
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
