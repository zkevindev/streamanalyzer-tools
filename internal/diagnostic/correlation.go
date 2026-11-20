package diagnostic

import "strings"

func applyContentCorrelations(report Report, quality *ContentQualityReport) {
	if quality == nil || quality.State == "unavailable" {
		return
	}
	offset := int64(0)
	if report.Metrics.FirstVideoMS != nil && report.Metrics.FirstAudioMS != nil {
		offset = min(*report.Metrics.FirstVideoMS, *report.Metrics.FirstAudioMS)
	} else if report.Metrics.FirstVideoMS != nil {
		offset = *report.Metrics.FirstVideoMS
	} else if report.Metrics.FirstAudioMS != nil {
		offset = *report.Metrics.FirstAudioMS
	} else if report.Manifest.Observation.FirstPayloadMS != nil {
		offset = *report.Manifest.Observation.FirstPayloadMS
	}
	quality.CorrelationAlignment = "first_media_estimate"
	quality.Correlations = []LayerCorrelation{}
	for _, group := range []struct {
		kind      string
		intervals []ContentQualityInterval
	}{{"black", quality.BlackIntervals}, {"freeze", quality.FreezeIntervals}, {"silence", quality.SilenceIntervals}} {
		for _, interval := range group.intervals {
			correlation := correlateLayerInterval(report, group.kind, interval.StartMS, interval.EndMS, interval.StartMS+offset, interval.EndMS+offset)
			correlation.Message = "源内容异常与列出的网络/播放器证据同期，时序关联不能单独确定因果。"
			quality.Correlations = append(quality.Correlations, correlation)
		}
	}
	if len(quality.Correlations) == 0 && report.Player != nil {
		for index, stall := range report.Player.Stalls {
			correlation := correlateLayerInterval(report, "none_detected", stall.StartMS, stall.EndMS, stall.StartMS, stall.EndMS)
			correlation.PlayerStalls = []int{index + 1}
			correlation.Message = "源内容检测未报告黑帧、冻结或静音；播放异常优先结合同期供给和本机播放证据验证。"
			quality.Correlations = append(quality.Correlations, correlation)
		}
	}
	if len(quality.Correlations) > 16 {
		quality.Correlations = quality.Correlations[:16]
	}
}

func correlateLayerInterval(report Report, kind string, contentStart, contentEnd, alignedStart, alignedEnd int64) LayerCorrelation {
	result := LayerCorrelation{ContentKind: kind, ContentStartMS: contentStart, ContentEndMS: contentEnd,
		AlignedStartMS: alignedStart, AlignedEndMS: alignedEnd, Status: "candidate",
		NetworkFindingIDs: []string{}, PlayerStalls: []int{}, RTMPControls: []int{}, LowThroughputSecs: []int{}}
	overlap := func(start, end int64) bool { return min(end, alignedEnd+500) >= max(start, alignedStart-500) }
	for _, finding := range report.Findings {
		if !networkCorrelationFinding(finding.Code) || !overlap(finding.StartMS, finding.EndMS) {
			continue
		}
		result.NetworkFindingIDs = append(result.NetworkFindingIDs, finding.ID)
	}
	if report.Player != nil {
		for index, stall := range report.Player.Stalls {
			if overlap(stall.StartMS, stall.EndMS) {
				result.PlayerStalls = append(result.PlayerStalls, index+1)
			}
		}
	}
	if report.RTMPControl != nil {
		for index, message := range report.RTMPControl.Messages {
			if overlap(message.ArrivalMS, message.ArrivalMS) && (message.Level == "error" || message.Name == "stream_eof" || message.Name == "stream_dry") {
				result.RTMPControls = append(result.RTMPControls, index+1)
			}
		}
	}
	if report.Throughput != nil && report.Throughput.AverageMediaKbps > 0 {
		for _, sample := range report.Throughput.Samples {
			if sample.NetworkKbps < report.Throughput.AverageMediaKbps*0.5 && overlap(int64(sample.Second*1000), int64(sample.Second*1000)+sample.DurationMS) {
				result.LowThroughputSecs = append(result.LowThroughputSecs, sample.Second)
			}
		}
	}
	return result
}

func networkCorrelationFinding(code string) bool {
	return strings.Contains(code, "arrival_gap") || strings.Contains(code, "tail_gap") || strings.Contains(code, "segment_failed") ||
		strings.Contains(code, "playlist_failure") || code == "http_status" || code == "request_error" || code == "rtmp_error" || strings.HasPrefix(code, "connection_")
}
