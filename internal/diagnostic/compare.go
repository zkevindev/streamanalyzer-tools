package diagnostic

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type ComparisonRun struct {
	ID        string    `json:"id"`
	Protocol  string    `json:"protocol"`
	Origin    string    `json:"origin,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type ComparisonMetric struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Unit     string   `json:"unit"`
	Baseline *float64 `json:"baseline,omitempty"`
	Current  *float64 `json:"current,omitempty"`
	Delta    *float64 `json:"delta,omitempty"`
	State    string   `json:"state"`
}

type ReportComparison struct {
	Version          string             `json:"version"`
	Baseline         ComparisonRun      `json:"baseline"`
	Current          ComparisonRun      `json:"current"`
	Warnings         []string           `json:"warnings"`
	Metrics          []ComparisonMetric `json:"metrics"`
	NewFindings      []string           `json:"new_findings"`
	ResolvedFindings []string           `json:"resolved_findings"`
}

func comparisonRun(detail Detail) ComparisonRun {
	return ComparisonRun{ID: detail.ID, Protocol: detail.Protocol, Origin: detail.Origin, Status: detail.Status, CreatedAt: detail.CreatedAt}
}

func comparisonNumber(value int64) *float64 {
	result := float64(value)
	return &result
}

func comparisonOptional(value *int64) *float64 {
	if value == nil {
		return nil
	}
	return comparisonNumber(*value)
}

func comparisonMetric(key, label, unit string, baseline, current *float64, lowerBetter bool) ComparisonMetric {
	metric := ComparisonMetric{Key: key, Label: label, Unit: unit, Baseline: baseline, Current: current, State: "unobserved"}
	if baseline == nil || current == nil {
		return metric
	}
	delta := *current - *baseline
	metric.Delta = &delta
	switch {
	case delta == 0:
		metric.State = "unchanged"
	case !lowerBetter:
		metric.State = "changed"
	case delta < 0:
		metric.State = "improved"
	default:
		metric.State = "worse"
	}
	return metric
}

func (s *Service) Compare(id, baselineID string) (ReportComparison, error) {
	if id == baselineID {
		return ReportComparison{}, fmt.Errorf("%w: 请选择另一条基线记录", ErrInvalid)
	}
	current, err := s.Detail(id)
	if err != nil {
		return ReportComparison{}, err
	}
	baseline, err := s.Detail(baselineID)
	if err != nil {
		return ReportComparison{}, err
	}
	if current.Report == nil || baseline.Report == nil || current.Status == "running" || baseline.Status == "running" {
		return ReportComparison{}, ErrPending
	}
	if current.Report.Manifest.Protocol != baseline.Report.Manifest.Protocol {
		return ReportComparison{}, fmt.Errorf("%w: 只能对比相同协议的报告", ErrInvalid)
	}
	return compareReports(current, baseline), nil
}

func compareReports(current, baseline Detail) ReportComparison {
	a, b := *current.Report, *baseline.Report
	result := ReportComparison{Version: "report-comparison/1", Baseline: comparisonRun(baseline), Current: comparisonRun(current),
		Warnings: []string{}, Metrics: []ComparisonMetric{}, NewFindings: []string{}, ResolvedFindings: []string{}}
	if a.Manifest.Observation.Origin != b.Manifest.Observation.Origin {
		result.Warnings = append(result.Warnings, "两次记录来源不同，结果属于 A/B 对照，不能解释为同一链路的时间前后变化。")
	}
	if a.RuleVersion != b.RuleVersion {
		result.Warnings = append(result.Warnings, "规则版本不同，新增和消失的规则不直接代表现场改善或恶化。")
	}
	if !slices.Equal(a.Metrics.VideoCodecs, b.Metrics.VideoCodecs) || !slices.Equal(a.Metrics.AudioCodecs, b.Metrics.AudioCodecs) {
		result.Warnings = append(result.Warnings, "音视频编码集合不同，帧数、时间戳和播放器指标可能不具备直接可比性。")
	}
	aDuration, bDuration := a.Manifest.Observation.ElapsedMS, b.Manifest.Observation.ElapsedMS
	if min(aDuration, bDuration) <= 0 || float64(max(aDuration, bDuration))/float64(max(1, min(aDuration, bDuration))) > 1.1 {
		result.Warnings = append(result.Warnings, "采样时长相差超过 10%，帧数、包数和累计异常次数不应直接作为质量结论。")
	}

	add := func(key, label, unit string, before, after *float64, lowerBetter bool) {
		result.Metrics = append(result.Metrics, comparisonMetric(key, label, unit, before, after, lowerBetter))
	}
	add("ttfb_ms", "首字节 / 握手首字节", "ms", comparisonOptional(b.Manifest.Observation.TTFBMS), comparisonOptional(a.Manifest.Observation.TTFBMS), true)
	add("first_payload_ms", "首个载荷到达", "ms", comparisonOptional(b.Manifest.Observation.FirstPayloadMS), comparisonOptional(a.Manifest.Observation.FirstPayloadMS), true)
	add("first_video_ms", "首个视频媒体", "ms", comparisonOptional(b.Metrics.FirstVideoMS), comparisonOptional(a.Metrics.FirstVideoMS), true)
	add("first_audio_ms", "首个音频媒体", "ms", comparisonOptional(b.Metrics.FirstAudioMS), comparisonOptional(a.Metrics.FirstAudioMS), true)
	add("first_keyframe_ms", "首个关键帧", "ms", comparisonOptional(b.Metrics.FirstKeyframeMS), comparisonOptional(a.Metrics.FirstKeyframeMS), true)
	var beforeGap, afterGap *float64
	if b.Metrics.VideoFrames > 1 {
		beforeGap = comparisonNumber(b.Metrics.MaxVideoGapMS)
	}
	if a.Metrics.VideoFrames > 1 {
		afterGap = comparisonNumber(a.Metrics.MaxVideoGapMS)
	}
	add("max_video_gap_ms", "视频最大到达间隔", "ms", beforeGap, afterGap, true)
	add("finding_count", "规则证据数", "count", comparisonNumber(int64(len(b.Findings))), comparisonNumber(int64(len(a.Findings))), true)

	if b.Player != nil || a.Player != nil {
		var before, after *PlayerReport = b.Player, a.Player
		var beforeFirst, afterFirst, beforeStalls, afterStalls, beforeTotal, afterTotal *float64
		if before != nil {
			beforeFirst, beforeStalls, beforeTotal = comparisonOptional(before.FirstScreenMS), comparisonNumber(int64(before.StallCount)), comparisonNumber(before.TotalStallMS)
		}
		if after != nil {
			afterFirst, afterStalls, afterTotal = comparisonOptional(after.FirstScreenMS), comparisonNumber(int64(after.StallCount)), comparisonNumber(after.TotalStallMS)
		}
		add("first_screen_ms", "播放器首屏", "ms", beforeFirst, afterFirst, true)
		add("stall_count", "缓冲卡顿次数", "count", beforeStalls, afterStalls, true)
		add("total_stall_ms", "累计缓冲卡顿", "ms", beforeTotal, afterTotal, true)
	}
	if b.HLS != nil || a.HLS != nil {
		add("hls_segment_failures", "HLS 分片失败", "count", hlsComparisonNumber(b.HLS, func(v *HLSMetrics) int64 { return int64(v.SegmentsFailed) }), hlsComparisonNumber(a.HLS, func(v *HLSMetrics) int64 { return int64(v.SegmentsFailed) }), true)
		add("hls_playlist_failures", "HLS 清单失败", "count", hlsComparisonNumber(b.HLS, func(v *HLSMetrics) int64 { return int64(v.PlaylistFailures) }), hlsComparisonNumber(a.HLS, func(v *HLSMetrics) int64 { return int64(v.PlaylistFailures) }), true)
	}
	if b.RTSP != nil || a.RTSP != nil {
		add("rtsp_missing_packets", "RTSP 估计缺失包", "count", rtspComparisonNumber(b.RTSP), rtspComparisonNumber(a.RTSP), true)
		add("rtsp_out_of_order", "RTSP 乱序包", "count", rtspOutOfOrderComparisonNumber(b.RTSP), rtspOutOfOrderComparisonNumber(a.RTSP), true)
	}
	if b.MPEGTS != nil || a.MPEGTS != nil {
		add("ts_continuity_errors", "TS 连续计数异常", "count", tsComparisonNumber(b.MPEGTS, func(v *MPEGTSMetrics) int64 { return int64(v.ContinuityErrors) }), tsComparisonNumber(a.MPEGTS, func(v *MPEGTSMetrics) int64 { return int64(v.ContinuityErrors) }), true)
		add("ts_transport_errors", "TS 传输错误标记", "count", tsComparisonNumber(b.MPEGTS, func(v *MPEGTSMetrics) int64 { return int64(v.TransportErrors) }), tsComparisonNumber(a.MPEGTS, func(v *MPEGTSMetrics) int64 { return int64(v.TransportErrors) }), true)
		add("ts_sync_loss_bytes", "TS 同步丢失字节", "bytes", tsComparisonNumber(b.MPEGTS, func(v *MPEGTSMetrics) int64 { return v.SyncLossBytes }), tsComparisonNumber(a.MPEGTS, func(v *MPEGTSMetrics) int64 { return v.SyncLossBytes }), true)
	}

	beforeCodes := findingCodeSet(b.Findings)
	afterCodes := findingCodeSet(a.Findings)
	for code := range afterCodes {
		if !beforeCodes[code] {
			result.NewFindings = append(result.NewFindings, code)
		}
	}
	for code := range beforeCodes {
		if !afterCodes[code] {
			result.ResolvedFindings = append(result.ResolvedFindings, code)
		}
	}
	slices.Sort(result.NewFindings)
	slices.Sort(result.ResolvedFindings)
	return result
}

func hlsComparisonNumber(value *HLSMetrics, get func(*HLSMetrics) int64) *float64 {
	if value == nil {
		return nil
	}
	return comparisonNumber(get(value))
}

func tsComparisonNumber(value *MPEGTSMetrics, get func(*MPEGTSMetrics) int64) *float64 {
	if value == nil {
		return nil
	}
	return comparisonNumber(get(value))
}

func rtspComparisonNumber(value *RTSPMetrics) *float64 {
	if value == nil {
		return nil
	}
	var total int64
	for _, track := range value.Tracks {
		total += int64(track.MissingSequencePackets)
	}
	return comparisonNumber(total)
}

func rtspOutOfOrderComparisonNumber(value *RTSPMetrics) *float64 {
	if value == nil {
		return nil
	}
	var total int64
	for _, track := range value.Tracks {
		total += int64(track.OutOfOrderPackets)
	}
	return comparisonNumber(total)
}

func findingCodeSet(findings []Finding) map[string]bool {
	result := make(map[string]bool, len(findings))
	for _, finding := range findings {
		if strings.TrimSpace(finding.Code) != "" {
			result[finding.Code] = true
		}
	}
	return result
}
