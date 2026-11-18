package diagnostic

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

func addPlayerFindings(report *Report) {
	report.Findings = slices.DeleteFunc(report.Findings, func(f Finding) bool { return strings.HasPrefix(f.Code, "player_") })
	player := report.Player
	if player == nil {
		return
	}
	add := func(code, severity, message, next string, start, end int64) {
		report.Findings = append(report.Findings, Finding{
			Code: code, Severity: severity, Message: message, NextCheck: next,
			StartMS: max(0, start), EndMS: max(0, end), Occurrences: 1,
		})
	}
	if player.StallCount > 0 {
		start, end := player.Stalls[0].StartMS, player.Stalls[len(player.Stalls)-1].EndMS
		add("player_stall", "warn", fmt.Sprintf("播放器观测到 %d 次卡顿，合计 %d ms", player.StallCount, player.TotalStallMS),
			"对照同一时间轴的缓冲区、收流间隔和请求耗时，结合客户端解码日志核对原因。", start, end)
		finding := &report.Findings[len(report.Findings)-1]
		finding.Occurrences = player.StallCount
		for _, stall := range player.Stalls[:min(4, len(player.Stalls))] {
			finding.Evidence = append(finding.Evidence, Event{Kind: "player_stall", Artifact: playerName, ArrivalMS: stall.StartMS, DurationMS: stall.DurationMS})
		}
	}
	if player.FirstScreenMS != nil && *player.FirstScreenMS > 3000 {
		add("player_slow_first_screen", "warn", fmt.Sprintf("播放器首帧在启动后 %d ms 到达", *player.FirstScreenMS),
			"对照播放器启动偏移、请求阶段和关键帧到达时间，再检查解码与渲染日志。",
			player.StartOffsetMS, player.StartOffsetMS+*player.FirstScreenMS)
	}
	switch player.State {
	case "autoplay_blocked":
		add("player_autoplay_blocked", "info", "浏览器阻止了自动播放，需要用户手动启动播放。", "点击播放器播放按钮后重新采样。", player.StartOffsetMS, player.StartOffsetMS+player.ElapsedMS)
	case "unsupported":
		add("player_unsupported", "info", "当前播放器未能使用该协议、编码或播放环境。", "核对浏览器解码能力和播放器错误事件。", player.StartOffsetMS, player.StartOffsetMS+player.ElapsedMS)
	case "error":
		add("player_error", "warn", "播放器发生播放错误，记录已保留。", "查看播放器事件并对照同一时间的拉流报告。", player.StartOffsetMS, player.StartOffsetMS+player.ElapsedMS)
	}
	slices.SortStableFunc(report.Findings, func(a, b Finding) int { return cmp.Compare(a.StartMS, b.StartMS) })
	for i := range report.Findings {
		report.Findings[i].ID = fmt.Sprintf("F%03d", i+1)
	}
}
