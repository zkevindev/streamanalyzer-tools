package diagnostic

import (
	"strings"
)

func presentationWindow(source *PlayerPresentationReport, start, end int64) *PlayerPresentationReport {
	if source == nil {
		return nil
	}
	result := *source
	result.ObservationStartMS = min(source.ObservationEndMS, max(source.ObservationStartMS, start))
	result.ObservationEndMS = max(result.ObservationStartMS, min(source.ObservationEndMS, end))
	result.ActiveCoverageMS = 0
	result.ActiveIntervals = []PlayerSchedulingInterval{}
	result.Gaps = []PlayerPresentationGapReport{}
	result.Count, result.TotalMS, result.MaxMS = nil, nil, nil
	for _, span := range source.ActiveIntervals {
		if clipped, ok := schedulingIntersection(span, start, end); ok {
			result.ActiveIntervals = append(result.ActiveIntervals, clipped)
			result.ActiveCoverageMS += clipped.EndMS - clipped.StartMS
		}
	}
	if source.Count == nil || result.ActiveCoverageMS == 0 {
		return &result
	}
	count, total, largest := 0, int64(0), int64(0)
	for _, gap := range source.Gaps {
		overlap := max(0, min(gap.EndMS, end)-max(gap.StartMS, start))
		if overlap == 0 {
			continue
		}
		result.Gaps = append(result.Gaps, gap)
		count++
		total += overlap
		largest = max(largest, overlap)
	}
	result.Count, result.TotalMS, result.MaxMS = &count, &total, &largest
	return &result
}

func presentationBufferingOverlapEvidence(report Report, selected PlayerStall) (int64, []Event) {
	if report.Player == nil || report.Player.Presentation == nil {
		return 0, nil
	}
	var overlap int64
	var evidence []Event
	for _, gap := range report.Player.Presentation.Gaps {
		if min(gap.EndMS, selected.EndMS) <= max(gap.StartMS, selected.StartMS) {
			continue
		}
		used := false
		for _, buffer := range report.Player.Stalls {
			start, end := max(gap.StartMS, buffer.StartMS, selected.StartMS), min(gap.EndMS, buffer.EndMS, selected.EndMS)
			if end <= start {
				continue
			}
			overlap += end - start
			used = true
			if len(evidence) < 3 {
				evidence = append(evidence, Event{Kind: "player_stall", Artifact: playerName, ArrivalMS: buffer.StartMS, DurationMS: buffer.DurationMS})
			}
		}
		if used && len(evidence) < 4 {
			evidence = append(evidence, Event{Kind: "player_presentation_gap", Artifact: playerName, ArrivalMS: gap.StartMS, DurationMS: gap.DurationMS})
		}
	}
	return overlap, evidence
}

func assessStall(item StallInvestigation) *StallAssessment {
	result := assessStallV4(item)
	result.Version = stallAssessmentVersion
	if item.IntervalKind == IntervalPresentationGap {
		convert := func(text string) string { return strings.ReplaceAll(text, "卡顿", "呈帧回调间隔") }
		result.Conclusion = convert(result.Conclusion)
		for i := range result.Directions {
			direction := &result.Directions[i]
			direction.Title = convert(direction.Title)
			direction.Message = convert(direction.Message)
			for j := range direction.NextChecks {
				direction.NextChecks[j] = convert(direction.NextChecks[j])
			}
		}
		var ids []string
		for _, finding := range item.Correlation.Findings {
			if finding.Code == "presentation_gap_selected" {
				ids = append(ids, finding.ID)
			}
		}
		if len(ids) > 0 {
			result.Directions = append(result.Directions, StallDirection{Code: "presentation_callback_gap", Status: "observed", Title: "观测到连续前台呈帧回调间隔", Message: "该区间两端均有视频帧回调，间隔超过基于先前回调媒体时间节奏的阈值；浏览器未必发出等待事件，也不能据此确认画面冻结。", EvidenceIDs: ids, NextChecks: []string{"核对源内容帧率及帧率切换，并对照视频、媒体线程与页面调度记录。"}})
		}
		if result.Conclusion == "现有证据尚不能将呈帧回调间隔缩小到单一环节。" {
			result.Conclusion = "已观测到呈帧回调间隔，需结合内容帧率、页面调度与媒体线程继续验证。"
		}
	}
	if item.Correlation.Presentation != nil {
		var overlapIDs []string
		for _, finding := range item.Correlation.Findings {
			if finding.Code == "presentation_buffering_overlap" {
				overlapIDs = append(overlapIDs, finding.ID)
			}
		}
		if len(overlapIDs) > 0 {
			result.Directions = append(result.Directions, StallDirection{Code: "presentation_buffering_overlap", Status: "observed", Title: "回调间隔与缓冲卡顿有重合", Message: "同时间轴可见两类观测的交集；它们使用各自的统计口径，不能相加为新的卡顿次数。", EvidenceIDs: overlapIDs, NextChecks: []string{"对照缓冲、收流、调度与呈帧回调，核对它们是同一次等待的不同表现还是独立问题。"}})
		}
		result.Limitations = append(result.Limitations, "呈帧回调间隔独立于缓冲卡顿次数，只记录达到估算阈值且已恢复回调的完整区间；未恢复的末尾不补推。与缓冲卡顿重合时不能重复累计为两次卡顿。")
	}
	return result
}
