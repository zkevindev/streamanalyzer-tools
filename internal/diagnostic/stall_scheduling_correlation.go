package diagnostic

import (
	"fmt"
	"slices"
)

func schedulingIntersection(interval PlayerSchedulingInterval, start, end int64) (PlayerSchedulingInterval, bool) {
	value := PlayerSchedulingInterval{StartMS: max(interval.StartMS, start), EndMS: min(interval.EndMS, end)}
	return value, value.EndMS > value.StartMS
}

func schedulingWindowMetrics(metrics *PlayerSchedulingMetrics, start, end int64) *PlayerSchedulingMetrics {
	if metrics == nil {
		return nil
	}
	result := &PlayerSchedulingMetrics{Intervals: []PlayerSchedulingInterval{}}
	for _, interval := range metrics.Intervals {
		if value, ok := schedulingIntersection(interval, start, end); ok {
			result.Intervals = append(result.Intervals, value)
			result.Count++
			result.TotalMS += value.EndMS - value.StartMS
			result.MaxMS = max(result.MaxMS, value.EndMS-value.StartMS)
		}
	}
	return result
}

// This filters already-observed active intervals; it never fills a gap in
// buffer samples or reconstructs unobserved scheduling events.
func schedulingWindow(source *PlayerSchedulingReport, start, end int64) *PlayerSchedulingReport {
	if source == nil {
		return nil
	}
	result := *source
	result.ObservationStartMS = min(source.ObservationEndMS, max(source.ObservationStartMS, start))
	result.ObservationEndMS = max(result.ObservationStartMS, min(source.ObservationEndMS, end))
	result.ActiveCoverageMS = 0
	result.ActiveIntervals = []PlayerSchedulingInterval{}
	for _, interval := range source.ActiveIntervals {
		if value, ok := schedulingIntersection(interval, start, end); ok {
			result.ActiveIntervals = append(result.ActiveIntervals, value)
			result.ActiveCoverageMS += value.EndMS - value.StartMS
		}
	}
	result.LongTasks, result.TimerDelays = nil, nil
	if result.ActiveCoverageMS > 0 {
		result.LongTasks = schedulingWindowMetrics(source.LongTasks, start, end)
		result.TimerDelays = schedulingWindowMetrics(source.TimerDelays, start, end)
	}
	return &result
}

func schedulingCaseFindings(scheduling *PlayerSchedulingReport, stall PlayerStall) []Finding {
	if scheduling == nil {
		return nil
	}
	var result []Finding
	for _, group := range []struct {
		metrics           *PlayerSchedulingMetrics
		code, label, kind string
	}{
		{scheduling.LongTasks, "stall_page_long_tasks", "浏览器长任务", "player_long_task"},
		{scheduling.TimerDelays, "stall_page_timer_delays", "定时器延后", "player_timer_delay"},
	} {
		selected := schedulingWindowMetrics(group.metrics, stall.StartMS, stall.EndMS)
		if selected == nil || selected.TotalMS == 0 {
			continue
		}
		finding := Finding{Code: group.code, Severity: "warn", StartMS: selected.Intervals[0].StartMS, EndMS: selected.Intervals[len(selected.Intervals)-1].EndMS, Occurrences: selected.Count,
			Message: fmt.Sprintf("选中卡顿内观测到%s重合区间 %d 个，合计 %d ms，最长 %d ms；按前台播放覆盖裁剪，不推断缺测期间的缓冲。", group.label, selected.Count, selected.TotalMS, selected.MaxMS)}
		for _, interval := range selected.Intervals[:min(4, len(selected.Intervals))] {
			finding.Evidence = append(finding.Evidence, Event{Kind: group.kind, Artifact: playerName, ArrivalMS: interval.StartMS, DurationMS: interval.EndMS - interval.StartMS})
		}
		result = append(result, finding)
	}
	return result
}

func assessStall(item StallInvestigation) *StallAssessment {
	result := assessStallV2(item)
	result.Version = stallAssessmentVersion
	scheduling := item.Correlation.Scheduling
	if scheduling == nil {
		return result
	}
	var candidateIDs []string
	for _, group := range []struct {
		metrics                *PlayerSchedulingMetrics
		code, direction, title string
	}{
		{scheduling.LongTasks, "stall_page_long_tasks", "page_long_tasks", "卡顿期间观测到浏览器长任务"},
		{scheduling.TimerDelays, "stall_page_timer_delays", "page_timer_delays", "卡顿期间观测到定时器延后"},
	} {
		selected := schedulingWindowMetrics(group.metrics, item.Stall.StartMS, item.Stall.EndMS)
		if selected == nil || selected.TotalMS == 0 {
			continue
		}
		var ids []string
		for _, finding := range item.Correlation.Findings {
			if finding.Code == group.code {
				ids = append(ids, finding.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		result.Directions = append(result.Directions, StallDirection{Code: group.direction, Status: "observed", Title: group.title,
			Message:     fmt.Sprintf("前台播放覆盖内与卡顿重合 %d ms，最长连续重合 %d ms；这是页面调度观测，不能单独确定媒体播放根因。", selected.TotalMS, selected.MaxMS),
			EvidenceIDs: ids, NextChecks: []string{"对照浏览器性能录制与媒体线程日志，检查长任务来源、事件回调和播放器追加缓冲。"}})
		if selected.MaxMS >= 500 {
			candidateIDs = append(candidateIDs, ids...)
		}
	}
	if len(candidateIDs) > 0 {
		result.Directions = slices.DeleteFunc(result.Directions, func(direction StallDirection) bool { return direction.Code == "insufficient_evidence" })
		result.Directions = append(result.Directions, StallDirection{Code: "page_scheduling", Status: "candidate", Title: "核对页面调度对卡顿观测的影响",
			Message:     "页面调度延后与卡顿至少连续重合 500 ms，可能影响播放器事件、采样和呈帧回调；仍需结合实际播放请求和媒体线程记录验证。",
			EvidenceIDs: candidateIDs, NextChecks: []string{"在同网络下关闭其他页面和扩展，比较页面性能录制。", "对照媒体线程与浏览器原生播放结果，区分页面回调延后和媒体消费中断。"}})
		if assessmentDirectionHas(result, "actual_playback_request") || assessmentDirectionHas(result, "source_or_transport") {
			result.Conclusion += " 同时存在页面调度延后，需要分别验证播放供给和页面处理。"
		} else {
			result.Conclusion = "页面调度延后与卡顿重合，优先对照页面性能和媒体线程记录。"
		}
	}
	result.Limitations = append(result.Limitations, "长任务与定时器延后分别统计，时间重合不能证明 CPU 占用、解码故障或画面冻结；缺测缓冲不作插值。")
	if scheduling.LongTaskSupport == "unsupported" {
		result.Limitations = append(result.Limitations, "当前浏览器不支持 Long Tasks API；定时器延后是独立观测，不能作为原生长任务替代计数。")
	}
	if scheduling.DroppedLongTasks > 0 || scheduling.DroppedTimerDelays > 0 {
		result.Limitations = append(result.Limitations, "页面调度记录存在采集丢弃，未列出事件不能证明对应时段没有调度延后。")
	}
	return result
}

func assessmentDirectionHas(result *StallAssessment, code string) bool {
	return slices.ContainsFunc(result.Directions, func(direction StallDirection) bool { return direction.Code == code })
}
