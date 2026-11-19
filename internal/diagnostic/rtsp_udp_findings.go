package diagnostic

import "fmt"

func addRTSPUDPFindings(report *Report) {
	metrics := report.RTSP
	add := func(code, severity, message, next string) {
		report.Findings = append(report.Findings, Finding{ID: fmt.Sprintf("F%03d", len(report.Findings)+1), Code: code, Severity: severity, Message: message, NextCheck: next, StartMS: 0, EndMS: report.Manifest.Observation.ElapsedMS, Occurrences: 1})
	}
	played := false
	for _, request := range metrics.Requests {
		if request.Method == "PLAY" && request.Complete && request.StatusCode == 200 {
			played = true
		}
	}
	if played && metrics.RTPPackets == 0 && !metrics.AnalysisLimited {
		add("rtsp_udp_no_rtp", "warn", "RTSP 控制协商已完成 PLAY，但没有观测到来自协商源的可识别 UDP RTP 媒体包。", "核对协商的 RTP/RTCP 端口、防火墙、NAT 与源端发送；RTCP 到达也不能证明 RTP 端口可达，仍需对照现场抓包。")
	}
	if metrics.FilteredDatagrams > 0 {
		add("rtsp_udp_source_filtered", "info", fmt.Sprintf("接收了 %d 个来自非协商来源的 UDP 数据报；原文已保留，未计入媒体统计。", metrics.FilteredDatagrams), "对照协商地址与来源端口，核对 NAT 或设备是否使用了不同发送端口。")
	}
	if metrics.UDPOversizeDatagrams > 0 {
		add("rtsp_udp_oversize", "info", fmt.Sprintf("%d 个 UDP 数据报超过 SDK 接收缓冲区；原始完整数据报已保留并纳入原始包分析。", metrics.UDPOversizeDatagrams), "SDK 消费路径未接收这些超长数据报，后续预览结果需与原始包统计分开判断。")
	}
}
