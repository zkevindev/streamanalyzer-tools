package diagnostic

import "fmt"

func addPlayerPresentationFindings(report *Report) {
	if report.Player == nil || report.Player.Presentation == nil {
		return
	}
	value := report.Player.Presentation
	if value.Count == nil || *value.Count == 0 || len(value.Gaps) == 0 {
		return
	}
	finding := Finding{Code: "player_presentation_gaps_observed", Severity: "warn", Message: fmt.Sprintf("连续前台播放时段观测到 %d 次呈帧回调长间隔，累计 %d ms，最长 %d ms", *value.Count, *value.TotalMS, *value.MaxMS), NextCheck: "对照真实缓冲卡顿、源流帧率、播放器解码呈现和主线程调度；回调间隔不证明画面内容静止或解码失败。", StartMS: max(0, value.Gaps[0].StartMS), EndMS: max(0, value.Gaps[len(value.Gaps)-1].EndMS), Occurrences: *value.Count}
	for _, gap := range value.Gaps[:min(4, len(value.Gaps))] {
		finding.Evidence = append(finding.Evidence, Event{Kind: "player_presentation_gap", Artifact: playerName, ArrivalMS: gap.StartMS, DurationMS: gap.DurationMS})
	}
	report.Findings = append(report.Findings, finding)
}
