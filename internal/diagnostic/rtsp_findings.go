package diagnostic

import "fmt"

func addRTSPFindings(report *Report, events []Event, states []rtspTrackState) {
	add := func(code, severity, message, next string, start, end int64, evidence []Event) {
		report.Findings = append(report.Findings, Finding{ID: fmt.Sprintf("F%03d", len(report.Findings)+1), Code: code, Severity: severity, Message: message, NextCheck: next, StartMS: start, EndMS: end, Occurrences: 1, Evidence: evidence})
	}
	metrics := report.RTSP
	observation := report.Manifest.Observation
	if observation.FailureCode != "" {
		add("rtsp_capture_failed", "error", "RTSP 采样阶段未完成（"+observation.FailureStage+" / "+observation.FailureCode+"）", "核对所选协议、鉴权、证书与对应服务端日志。", 0, observation.ElapsedMS, nil)
	}
	for _, request := range metrics.Requests {
		if request.StatusCode >= 300 && request.StatusCode < 400 {
			add("rtsp_redirect_stopped", "warn", "RTSP 返回重定向；已保留响应并停止自动跟随。", "手动确认重定向目标后重新采样。", request.StartMS, request.EndMS, nil)
		}
	}
	if metrics.RTPPackets == 0 {
		add("rtsp_no_rtp", "error", "本次没有观测到可识别的 RTP 媒体包。", "对照 DESCRIBE、SETUP、PLAY 的状态及 TCP 交错传输配置。", 0, observation.ElapsedMS, nil)
	}
	for index, track := range metrics.Tracks {
		if track.MaxArrivalGapMS >= 1000 {
			add("rtsp_receive_gap", "warn", fmt.Sprintf("轨道 %d 的 RTP 到达间隔最长 %d ms。", track.ID, track.MaxArrivalGapMS), "对比源端发送与另一个接收点；TCP 到达间隔不等于解码帧间隔或网络丢包。", states[index].gapStart, states[index].gapEnd, states[index].gapEvidence)
		}
		if track.SequenceGaps > 0 {
			add("rtsp_sequence_gap", "warn", fmt.Sprintf("轨道 %d 观测到 %d 次前向序号缺口，256 包回补窗口后仍有 %d 个序号未观测。", track.ID, track.SequenceGaps, track.MissingSequencePackets), "对照源端 RTP 序号与发送记录；该数字不是 TCP 网络丢包证明。", 0, observation.ElapsedMS, states[index].sequenceEvidence)
		}
		if track.DuplicatePackets > 0 || track.OutOfOrderPackets > 0 {
			add("rtsp_rtp_reordered", "info", fmt.Sprintf("轨道 %d 观测到 %d 个重复包、%d 个乱序包。", track.ID, track.DuplicatePackets, track.OutOfOrderPackets), "核对源端打包顺序与 SSRC；不将乱序包计成额外媒体帧。", 0, observation.ElapsedMS, nil)
		}
	}
	if metrics.MalformedPackets > 0 || metrics.TrailingBytes > 0 {
		add("rtsp_wire_incomplete", "warn", fmt.Sprintf("原始应用流含 %d 个无法解析的包与 %d 字节未完成尾部。", metrics.MalformedPackets, metrics.TrailingBytes), "结合采样停止时刻与服务端关闭记录；原始字节已保留在证据包中。", 0, observation.ElapsedMS, events[:min(2, len(events))])
	}
	if metrics.AnalysisLimited || metrics.UnknownChannelPackets > 0 || metrics.UnknownPayloadPackets > 0 {
		add("rtsp_analysis_limited", "info", "部分原始 RTP/交错通道未纳入受支持的轨道统计。", "核对 SDP 与 SETUP 协商通道，原始数据仍可下载复查。", 0, observation.ElapsedMS, nil)
	}
}
