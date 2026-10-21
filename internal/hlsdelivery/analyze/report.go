package analyze

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

// ReportValue 返回与 JSON 输出相同的结构，供 HTTP API 直接编码。
func ReportValue(rep *Report) any {
	return compatibleReport(rep)
}

// CompareValue 返回两路对比的 JSON 结构。
func CompareValue(rep *CompareReport) any {
	if rep == nil {
		return nil
	}
	return map[string]any{
		"tool":      rep.Tool,
		"version":   rep.Version,
		"a":         ReportValue(rep.A),
		"b":         ReportValue(rep.B),
		"delta":     rep.Delta,
		"notes":     rep.Notes,
		"exit_code": rep.ExitCode,
	}
}

// WriteReport 按 console 或 json 写出报告。
func WriteReport(w io.Writer, rep *Report, format string) error {
	if strings.EqualFold(format, "json") {
		return writeJSON(w, compatibleReport(rep))
	}
	WriteConsole(w, rep)
	return nil
}

// WriteCompare 写出对比结果。
func WriteCompare(w io.Writer, rep *CompareReport, format string) error {
	if strings.EqualFold(format, "json") {
		return writeJSON(w, rep)
	}
	fmt.Fprintf(w, "流 A  %s\n", rep.A.URL)
	writeBrief(w, primary(rep.A))
	fmt.Fprintf(w, "\n流 B  %s\n", rep.B.URL)
	writeBrief(w, primary(rep.B))
	fmt.Fprintf(w, "\n对比\n")
	for _, note := range rep.Notes {
		fmt.Fprintf(w, "  %s\n", note)
	}
	if rep.ExitCode != 0 {
		fmt.Fprintf(w, "\n退出码: %d %s\n", rep.ExitCode, exitReason(rep.ExitCode))
	}
	return nil
}

// WriteConsole 打印给人看的结果。
func WriteConsole(w io.Writer, rep *Report) {
	if rep.Command == "manifest" {
		writeManifest(w, rep)
		return
	}
	if len(rep.Results) > 1 {
		for i := range rep.Results {
			fmt.Fprintf(w, "\n======== 变体 %d ========\n", i)
			writeOne(w, &rep.Results[i])
		}
		if rep.ExitCode != 0 {
			fmt.Fprintf(w, "\n退出码: %d %s\n", rep.ExitCode, exitReason(rep.ExitCode))
		}
		return
	}
	writeOne(w, rep)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func writeManifest(w io.Writer, rep *Report) {
	fmt.Fprintf(w, "播放列表  %s\n", rep.URL)
	kind := "点播"
	if rep.Live {
		kind = "直播"
	}
	if len(rep.Variants) > 0 && rep.Playlist.SegmentCount == 0 {
		kind = "主播放列表"
	}
	fmt.Fprintf(w, "类型      %s", kind)
	if rep.Playlist.Version > 0 {
		fmt.Fprintf(w, "   HLS version %d", rep.Playlist.Version)
	}
	fmt.Fprintln(w)
	if rep.Playlist.TargetDurationS > 0 {
		fmt.Fprintf(w, "目标时长  %.0f s    媒体序号 %d    分片数 %d\n",
			rep.Playlist.TargetDurationS, rep.Playlist.MediaSequence, rep.Playlist.SegmentCount)
	}
	if rep.Playlist.Encrypted {
		fmt.Fprintln(w, "加密      是")
	}
	if rep.Infrastructure.CDNDetected != "" {
		fmt.Fprintf(w, "CDN       %s\n", rep.Infrastructure.CDNDetected)
	}
	if len(rep.Variants) > 0 {
		fmt.Fprintln(w, "\n变体")
		fmt.Fprintf(w, "  %-12s %-16s %-8s %s\n", "码率", "分辨率", "帧率", "编码")
		for _, v := range rep.Variants {
			rate := "-"
			if v.Bandwidth > 0 {
				rate = fmt.Sprintf("%d kbps", v.Bandwidth/1000)
			}
			res := v.Resolution
			if res == "" {
				res = "-"
			}
			fps := "-"
			if v.FrameRate > 0 {
				fps = fmt.Sprintf("%.0f", v.FrameRate)
			}
			fmt.Fprintf(w, "  %-12s %-16s %-8s %s\n", rate, res, fps, v.Codecs)
			fmt.Fprintf(w, "    %s\n", v.URI)
		}
	}
}

func writeOne(w io.Writer, rep *Report) {
	fmt.Fprintln(w, "======== HLS 分析结果 ========")
	fmt.Fprintf(w, "地址    %s\n", rep.URL)
	if rep.Started != "" {
		fmt.Fprintf(w, "开始    %s    耗时 %.1fs\n", rep.Started, rep.DurationS)
	}
	if rep.Live {
		fmt.Fprintln(w, "类型    直播")
	}
	if rep.Variant != nil {
		fmt.Fprintf(w, "变体    %s\n", describeVariant(*rep.Variant))
	}
	if rep.Playlist.TargetDurationS > 0 {
		fmt.Fprintf(w, "播放列表  目标 %.0fs  分片 %d  序号从 %d 起\n",
			rep.Playlist.TargetDurationS, rep.Playlist.SegmentCount, rep.Playlist.MediaSequence)
	}
	fmt.Fprintln(w, "\n分片")
	fmt.Fprintf(w, "  成功            %d\n", rep.Summary.SegmentsFetched)
	fmt.Fprintf(w, "  失败            %d\n", rep.Summary.SegmentsFailed)
	fmt.Fprintf(w, "  错误率          %.2f%%\n", rep.Summary.ErrorRatePct)
	fmt.Fprintf(w, "  平均 TTFB       %s\n", formatMS(rep.Summary.AvgTTFBMS))
	fmt.Fprintf(w, "  平均吞吐        %s\n", formatRate(rep.Summary.AvgThroughputKbps))
	fmt.Fprintf(w, "  P50 吞吐        %s\n", formatRate(rep.Summary.P50ThroughputKbps))
	fmt.Fprintf(w, "  最差 5%% 吞吐   %s\n", formatRate(rep.Summary.P95ThroughputKbps))
	fmt.Fprintf(w, "  最低吞吐        %s\n", formatRate(rep.Summary.MinThroughputKbps))
	fmt.Fprintf(w, "  最高吞吐        %s\n", formatRate(rep.Summary.MaxThroughputKbps))
	if rep.Summary.CacheHitRatePct != nil {
		fmt.Fprintf(w, "  缓存命中率      %.0f%%（命中 %d / 未命中 %d）\n", *rep.Summary.CacheHitRatePct, rep.Infrastructure.CacheHits, rep.Infrastructure.CacheMisses)
	} else {
		fmt.Fprintln(w, "  缓存命中率      未知")
	}
	if rep.Summary.PTSContinuity != "" {
		fmt.Fprintf(w, "  PTS 连续性      %s    不连续标记 %d\n", rep.Summary.PTSContinuity, rep.Summary.Discontinuities)
	}
	if bd := rep.Summary.ErrorBreakdown; bd != (ErrorBreakdown{}) {
		fmt.Fprintln(w, "\n失败原因")
		if bd.HTTP4xx > 0 {
			fmt.Fprintf(w, "  HTTP 4xx        %d\n", bd.HTTP4xx)
		}
		if bd.HTTP5xx > 0 {
			fmt.Fprintf(w, "  HTTP 5xx        %d\n", bd.HTTP5xx)
		}
		if bd.Timeouts > 0 {
			fmt.Fprintf(w, "  超时            %d\n", bd.Timeouts)
		}
		if bd.ShortReads > 0 {
			fmt.Fprintf(w, "  连接中断        %d\n", bd.ShortReads)
		}
		if bd.Other > 0 {
			fmt.Fprintf(w, "  其他            %d\n", bd.Other)
		}
	}
	if rep.Infrastructure.CDNDetected != "" || rep.Infrastructure.IPFlips > 0 || len(rep.Infrastructure.IPs) > 0 {
		fmt.Fprintln(w, "\n链路")
		if rep.Infrastructure.CDNDetected != "" {
			line := rep.Infrastructure.CDNDetected
			if rep.Infrastructure.EdgeLocation != "" {
				line += " (" + rep.Infrastructure.EdgeLocation + ")"
			}
			fmt.Fprintf(w, "  CDN             %s\n", line)
		}
		fmt.Fprintf(w, "  IP 变化         %d\n", rep.Infrastructure.IPFlips)
		if len(rep.Infrastructure.IPs) > 0 {
			fmt.Fprintf(w, "  IP              %s\n", strings.Join(rep.Infrastructure.IPs, ", "))
		}
	}
	if rep.LiveLatencyMS != nil {
		fmt.Fprintf(w, "\n估计观看延迟  %s\n", formatMS(*rep.LiveLatencyMS))
		if rep.EdgeLagMS != nil {
			fmt.Fprintf(w, "直播边缘滞后  %s\n", formatMS(*rep.EdgeLagMS))
		}
	}
	if rep.Stale {
		fmt.Fprintln(w, "播放列表      停更")
	}
	if rep.ABR != nil && len(rep.ABR.Variants) > 0 {
		fmt.Fprintln(w, "\n多码率")
		fmt.Fprintf(w, "  实测平均带宽  %s\n", formatRate(rep.ABR.MeasuredBandwidthKbps))
		fmt.Fprintf(w, "  最差 5%% 带宽  %s\n", formatRate(rep.ABR.P95BandwidthKbps))
		if rep.ABR.Recommended != "" {
			fmt.Fprintf(w, "  推荐档        %s\n", rep.ABR.Recommended)
		}
		if rep.ABR.SafeFallback != "" {
			fmt.Fprintf(w, "  保守档        %s\n", rep.ABR.SafeFallback)
		}
		fmt.Fprintf(w, "  %-16s %10s %10s %10s %s\n", "档位", "码率", "平均余量", "最差余量", "风险")
		for _, v := range rep.ABR.Variants {
			fmt.Fprintf(w, "  %-16s %10s %10s %10s %s\n",
				v.Label, fmt.Sprintf("%d kbps", v.BandwidthKbps), formatX(v.Headroom), formatX(v.P95Headroom), v.Risk)
		}
	}
	if len(rep.Recommendations) > 0 {
		fmt.Fprintln(w, "\n建议")
		for _, item := range rep.Recommendations {
			fmt.Fprintf(w, "  - %s\n", item)
		}
	}
	if rep.ExitCode != 0 {
		fmt.Fprintf(w, "\n退出码: %d %s\n", rep.ExitCode, exitReason(rep.ExitCode))
	}
}

func writeBrief(w io.Writer, rep *Report) {
	fmt.Fprintf(w, "  成功 %d  失败 %d  错误率 %.2f%%\n", rep.Summary.SegmentsFetched, rep.Summary.SegmentsFailed, rep.Summary.ErrorRatePct)
	fmt.Fprintf(w, "  TTFB %s  吞吐 %s  最差 5%% %s\n",
		formatMS(rep.Summary.AvgTTFBMS), formatRate(rep.Summary.AvgThroughputKbps), formatRate(rep.Summary.P95ThroughputKbps))
	if rep.Summary.CacheHitRatePct != nil {
		fmt.Fprintf(w, "  缓存命中率 %.0f%%\n", *rep.Summary.CacheHitRatePct)
	}
	if rep.Infrastructure.CDNDetected != "" {
		fmt.Fprintf(w, "  CDN %s\n", rep.Infrastructure.CDNDetected)
	}
}

func describeVariant(v VariantInfo) string {
	var parts []string
	if v.Resolution != "" {
		parts = append(parts, v.Resolution)
	}
	if v.Name != "" {
		parts = append(parts, v.Name)
	}
	if v.Bandwidth > 0 {
		parts = append(parts, fmt.Sprintf("%d kbps", v.Bandwidth/1000))
	}
	if v.Codecs != "" {
		parts = append(parts, v.Codecs)
	}
	if len(parts) == 0 {
		return v.URI
	}
	return strings.Join(parts, "  ")
}

func formatMS(ms float64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.2f s", ms/1000)
	}
	return fmt.Sprintf("%.0f ms", ms)
}

func formatRate(kbps float64) string {
	if kbps >= 1000 {
		return fmt.Sprintf("%.1f Mbps", kbps/1000)
	}
	if kbps >= 10 {
		return fmt.Sprintf("%.1f kbps", kbps)
	}
	return fmt.Sprintf("%.0f kbps", kbps)
}

func formatX(v float64) string {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return "-"
	}
	return fmt.Sprintf("%.1fx", v)
}

func exitReason(code int) string {
	switch code {
	case 2:
		return "错误率超过 --error-threshold"
	case 3:
		return "TTFB 超过 --latency-threshold"
	case 4:
		return "吞吐低于 --throughput-threshold"
	case 5:
		return "播放列表获取或解析失败"
	case 6:
		return "媒体播放列表没有分片"
	case 1:
		return "执行失败"
	default:
		return ""
	}
}

// FormatSegmentLine 是分析过程中的一行进度。
func FormatSegmentLine(seg SegmentResult) string {
	state := seg.CacheStatus
	if seg.Error != "" {
		state = seg.Error
	}
	if state == "" {
		state = "OK"
	}
	line := fmt.Sprintf("seq=%-6d ttfb=%-8s 吞吐=%-12s %s",
		seg.Sequence, formatMS(seg.TTFBMS), formatRate(seg.ThroughputKbps), state)
	if seg.IP != "" {
		line += "  " + seg.IP
	}
	if seg.StartsWithKeyframe != nil && !*seg.StartsWithKeyframe {
		line += "  非关键帧开头"
	}
	return line
}

func formatWall(t time.Time) string {
	return t.Format("15:04:05")
}

func compatibleReport(rep *Report) any {
	if rep == nil {
		return nil
	}
	if rep.Command == "manifest" {
		return manifestJSON(rep)
	}
	if len(rep.Results) > 0 && rep.Summary.SegmentsFetched+rep.Summary.SegmentsFailed == 0 {
		out := make([]any, 0, len(rep.Results))
		for i := range rep.Results {
			out = append(out, analyzeJSON(&rep.Results[i]))
		}
		return map[string]any{
			"tool":    rep.Tool,
			"version": rep.Version,
			"command": rep.Command,
			"url":     rep.URL,
			"results": out,
		}
	}
	return analyzeJSON(rep)
}

func manifestJSON(rep *Report) map[string]any {
	return map[string]any{
		"tool":     rep.Tool,
		"version":  rep.Version,
		"url":      rep.URL,
		"variants": variantViews(rep.Variants),
	}
}

func analyzeJSON(rep *Report) map[string]any {
	out := map[string]any{
		"tool":           rep.Tool,
		"version":        rep.Version,
		"schema_version": "1.0",
		"url":            rep.URL,
		"type":           rep.StreamType,
		"duration_s":     rep.DurationS,
		"timestamp":      rep.Started,
		"segments":       segmentViews(rep.Segments),
		"summary":        summaryView(rep.Summary),
		"infrastructure": infraView(rep.Infrastructure),
		"_units": map[string]string{
			"duration_s":      "seconds",
			"bandwidth_kbps":  "kilobits per second",
			"throughput_kbps": "kilobits per second",
			"ttfb_ms":         "milliseconds",
			"download_ms":     "milliseconds",
			"latency_s":       "seconds",
		},
	}
	if len(rep.Variants) > 0 {
		out["manifest"] = map[string]any{
			"master_url":        rep.URL,
			"target_duration_s": rep.Playlist.TargetDurationS,
			"playlist_type":     rep.StreamType,
			"variants":          variantViews(rep.Variants),
		}
	}
	if rep.Variant != nil {
		out["tested_variant"] = map[string]any{
			"bandwidth_kbps": rep.Variant.Bandwidth / 1000,
			"resolution":     rep.Variant.Resolution,
		}
	}
	if rep.Command != "" && rep.Command != "analyze" {
		out["command"] = rep.Command
	}
	if rep.LiveLatencyMS != nil {
		out["live_latency_ms"] = *rep.LiveLatencyMS
	}
	if rep.EdgeLagMS != nil {
		out["edge_lag_ms"] = *rep.EdgeLagMS
	}
	if rep.Stale {
		out["stale"] = true
	}
	if len(rep.Recommendations) > 0 {
		out["recommendations"] = rep.Recommendations
	}
	if rep.ABR != nil {
		out["abr"] = rep.ABR
	}
	return out
}

func variantViews(in []VariantInfo) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, v := range in {
		item := map[string]any{
			"bandwidth_kbps": v.Bandwidth / 1000,
			"resolution":     v.Resolution,
			"codecs":         v.Codecs,
			"url":            v.URI,
		}
		out = append(out, item)
	}
	return out
}

func segmentViews(in []SegmentResult) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, seg := range in {
		item := map[string]any{
			"sequence":            seg.Sequence,
			"url":                 seg.URL,
			"declared_duration_s": seg.DeclaredDurationS,
			"size_bytes":          seg.Bytes,
			"ttfb_ms":             seg.TTFBMS,
			"download_ms":         seg.DownloadMS,
			"throughput_kbps":     seg.ThroughputKbps,
		}
		if seg.HTTPStatus > 0 {
			item["http_status"] = seg.HTTPStatus
		}
		if seg.CacheStatus != "" {
			item["cache_status"] = seg.CacheStatus
		}
		if seg.CacheAge != nil {
			item["cache_age"] = *seg.CacheAge
		}
		if seg.RemoteAddr != "" {
			item["remote_addr"] = seg.RemoteAddr
		}
		if seg.ActualDurationS != nil {
			item["actual_duration_s"] = *seg.ActualDurationS
		}
		if seg.DurationDriftS != nil {
			item["duration_drift_s"] = *seg.DurationDriftS
		}
		if seg.PTSStart != nil {
			item["pts_start"] = *seg.PTSStart
		}
		if seg.PTSEnd != nil {
			item["pts_end"] = *seg.PTSEnd
		}
		if seg.StartsWithKeyframe != nil {
			item["starts_with_keyframe"] = *seg.StartsWithKeyframe
		}
		if seg.Error != "" {
			item["error"] = seg.Error
		}
		out = append(out, item)
	}
	return out
}

func summaryView(s Summary) map[string]any {
	out := map[string]any{
		"segments_fetched":    s.SegmentsFetched,
		"segments_failed":     s.SegmentsFailed,
		"error_rate_pct":      s.ErrorRatePct,
		"avg_ttfb_ms":         s.AvgTTFBMS,
		"avg_download_ms":     s.AvgDownloadMS,
		"avg_throughput_kbps": s.AvgThroughputKbps,
		"p50_throughput_kbps": s.P50ThroughputKbps,
		"p95_throughput_kbps": s.P95ThroughputKbps,
		"min_throughput_kbps": s.MinThroughputKbps,
		"max_throughput_kbps": s.MaxThroughputKbps,
		"discontinuities":     s.Discontinuities,
	}
	if s.CacheHitRatePct != nil {
		out["cache_hit_rate_pct"] = *s.CacheHitRatePct
	}
	if s.PTSContinuity != "" {
		out["pts_continuity"] = s.PTSContinuity
	}
	if bd := breakdownView(s.ErrorBreakdown); len(bd) > 0 {
		out["error_breakdown"] = bd
	}
	return out
}

func breakdownView(bd ErrorBreakdown) map[string]int {
	out := map[string]int{}
	if bd.HTTP4xx > 0 {
		out["http_4xx_errors"] = bd.HTTP4xx
	}
	if bd.HTTP5xx > 0 {
		out["http_5xx_errors"] = bd.HTTP5xx
	}
	if bd.Timeouts > 0 {
		out["timeouts"] = bd.Timeouts
	}
	if bd.ShortReads > 0 {
		out["short_reads"] = bd.ShortReads
	}
	if bd.Other > 0 {
		out["other"] = bd.Other
	}
	return out
}

func infraView(in Infrastructure) map[string]any {
	out := map[string]any{
		"load_balancer_detected": in.LoadBalancerDetected,
	}
	if in.CDNDetected != "" {
		out["cdn_detected"] = in.CDNDetected
	}
	if len(in.CDNHeaders) > 0 {
		out["cdn_headers"] = in.CDNHeaders
	}
	if in.EdgeLocation != "" {
		out["edge_location"] = in.EdgeLocation
	}
	return out
}
