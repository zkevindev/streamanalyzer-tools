package diagnostic

import "strings"

func addObservationFindings(report *Report) {
	observation := report.Manifest.Observation
	message := captureFailureText(observation)
	if message == "" || observation.FailureCode == "canceled" {
		return
	}
	report.Findings = append(report.Findings, Finding{
		Code: "connection_" + observation.FailureCode, Severity: "error", Message: message,
		StartMS: observation.ElapsedMS, EndMS: observation.ElapsedMS, Occurrences: 1,
	})
}

func enrichFindings(report *Report) {
	for index := range report.Findings {
		finding := &report.Findings[index]
		if finding.NextCheck != "" {
			continue
		}
		switch {
		case strings.Contains(finding.Code, "dts"):
			finding.NextCheck = "对比异常前后的媒体时间戳，核对编码器时间基、断流重连和源站切换记录。"
		case strings.Contains(finding.Code, "gop"), finding.Code == "no_video_keyframe", finding.Code == "slow_first_video":
			finding.NextCheck = "检查关键帧发送周期及视频配置头，比较源端与当前采样点的首帧到达时间。"
		case strings.Contains(finding.Code, "arrival_gap"), strings.Contains(finding.Code, "tail_gap"):
			finding.NextCheck = "对照异常时间段的源端推流和网络带宽，比较另一采样点是否同时出现接收间隔。"
		case strings.Contains(finding.Code, "continuity"), finding.Code == "rtp_sequence_gap":
			finding.NextCheck = "对比发送端和接收端数据，检查网卡丢包、接收缓冲区和中间网络设备的丢包计数。"
		case strings.Contains(finding.Code, "pat"), strings.Contains(finding.Code, "pmt"):
			finding.NextCheck = "检查复用器是否周期发送 PAT/PMT，核对节目与音视频 PID 映射。"
		case finding.Code == "http_status":
			finding.NextCheck = "核对播放鉴权、源站可用性及返回该状态码的服务日志。"
		case finding.Code == "no_media", finding.Code == "ts_no_media":
			finding.NextCheck = "延长采样并确认发送端已有媒体输出，检查协议与封装是否和接收配置一致。"
		case strings.HasPrefix(finding.Code, "connection_"):
			finding.NextCheck = "从同一台设备核对连接阶段、目标服务状态及网络配置后重新采样。"
		}
	}
}
