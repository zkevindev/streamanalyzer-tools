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
	if w := player.WebRTC; w != nil {
		if w.FreezeCount > 0 {
			add("player_webrtc_freeze", "warn",
				fmt.Sprintf("浏览器原生 WebRTC 播放观测到 %d 次画面冻结，累计冻结 %d ms", w.FreezeCount, w.TotalFreezesDurationMS),
				"对照服务端 RTP 丢包/抖动、PLI 次数与浏览器抖动缓冲延迟，定位网络波动还是端侧解码卡顿。",
				player.StartOffsetMS, player.StartOffsetMS+player.ElapsedMS)
		}
		if w.JitterBufferAvgMS >= 150 {
			add("player_webrtc_jitter_buffer_high", "warn",
				fmt.Sprintf("浏览器 WebRTC 抖动缓冲平均延迟偏高（%.1f ms，Jitter %.1f ms）", w.JitterBufferAvgMS, w.JitterMS),
				"检查网络抖动或丢包重传（NACK）导致的抖动缓冲膨胀。",
				player.StartOffsetMS, player.StartOffsetMS+player.ElapsedMS)
		}
		if w.PacketsLost > 0 && w.PacketsReceived > 0 {
			lossPct := float64(w.PacketsLost) * 100.0 / float64(w.PacketsLost+w.PacketsReceived)
			if lossPct >= 1.0 {
				add("player_webrtc_packet_loss", "warn",
					fmt.Sprintf("浏览器 WebRTC 接收侧报告丢包 %d 包（%.2f%%），已发 NACK %d / PLI %d", w.PacketsLost, lossPct, w.NACKCount, w.PLICount),
					"结合服务端采样丢包对比，判断丢包发生在公网下发还是本机无线网卡。",
					player.StartOffsetMS, player.StartOffsetMS+player.ElapsedMS)
			}
		}
	}
	addPlayerSchedulingFindings(report)
	addPlayerPresentationFindings(report)
	slices.SortStableFunc(report.Findings, func(a, b Finding) int { return cmp.Compare(a.StartMS, b.StartMS) })
	for i := range report.Findings {
		report.Findings[i].ID = fmt.Sprintf("F%03d", i+1)
	}
}
