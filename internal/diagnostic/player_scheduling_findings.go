package diagnostic

import "fmt"

func addPlayerSchedulingFindings(report *Report) {
	if report.Player == nil || report.Player.Scheduling == nil {
		return
	}
	value := report.Player.Scheduling
	for _, item := range []struct {
		code, kind, label string
		metrics           *PlayerSchedulingMetrics
	}{
		{"player_long_tasks_observed", "player_long_task", "主线程长任务", value.LongTasks},
		{"player_timer_delays_observed", "player_timer_delay", "定时器回调延迟", value.TimerDelays},
	} {
		metrics := item.metrics
		if metrics == nil || metrics.Count == 0 || len(metrics.Intervals) == 0 {
			continue
		}
		finding := Finding{Code: item.code, Severity: "info", Message: fmt.Sprintf("播放器前台播放时段观测到%s：%d 个活动区间片段，累计 %d ms，最长 %d ms", item.label, metrics.Count, metrics.TotalMS, metrics.MaxMS), NextCheck: "对照卡顿区间、可播放缓冲和呈帧回调；结合设备负载与主线程性能记录验证，不能单凭调度观测确定媒体或网络根因。", StartMS: max(0, metrics.Intervals[0].StartMS), EndMS: max(0, metrics.Intervals[len(metrics.Intervals)-1].EndMS), Occurrences: metrics.Count}
		for _, interval := range metrics.Intervals[:min(4, len(metrics.Intervals))] {
			finding.Evidence = append(finding.Evidence, Event{Kind: item.kind, Artifact: playerName, ArrivalMS: interval.StartMS, DurationMS: interval.EndMS - interval.StartMS})
		}
		report.Findings = append(report.Findings, finding)
	}
}
