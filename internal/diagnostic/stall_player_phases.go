package diagnostic

import (
	"fmt"
	"slices"
)

var playerPhaseLabels = map[string]string{
	"dns": "DNS 解析", "connect": "TCP 建联", "tls": "TLS 握手", "request_send": "请求写出", "response_first_byte": "等待响应首字节", "response_headers": "响应头接收", "receive": "上游响应体阶段", "proxy_write": "本地代理写出", "proxy_limit": "本地代理限制", "playlist": "播放清单处理",
}

var playerPhaseChecks = map[string]string{
	"dns":                 "对照本机解析结果与另一 DNS 服务，核对地址族和缓存变化。",
	"connect":             "对照同节点 TCP 连通性、建联重试与网络重传记录。",
	"tls":                 "核对目标节点证书、TLS 协议协商及握手日志。",
	"request_send":        "核对连接等待、请求写出与对端读取情况。",
	"response_first_byte": "对照源端处理、缓存节点响应和重定向后的请求耗时。",
	"response_headers":    "核对 HTTP 状态和响应头处理，并对照源端请求日志。",
	"receive":             "对照媒体码率、持续供给和代理写出背压；上游字节不代表浏览器已消费。",
	"proxy_write":         "核对本地浏览器连接、代理写出和客户端关闭时刻。",
	"proxy_limit":         "核对资源长度与代理资源限制，保留原始 dump 进行复盘。",
	"playlist":            "核对清单格式、资源引用和播放器支持能力。",
}

func hasPlayerPhaseEvidence(item StallInvestigation) bool {
	return slices.ContainsFunc(item.Correlation.PlayerRequests, func(request PlayerNetworkRequest) bool {
		return len(request.Hops) > 0 || request.DroppedHops > 0 || request.FailureStage != "" || request.FailureCode != ""
	})
}

func failedPlayerPhaseWindow(request PlayerNetworkRequest) (int64, int64, bool) {
	if len(request.Hops) == 0 {
		return 0, 0, false
	}
	hop := request.Hops[len(request.Hops)-1]
	if hop.Number != len(request.Hops)+request.DroppedHops {
		return 0, 0, false
	}
	if request.FailureStage == "proxy_write" || request.FailureStage == "proxy_limit" || request.FailureStage == "playlist" {
		return hop.EndMS, request.EndMS, true
	}
	proven := request.FailureStage == "receive" || (hop.FailureStage == request.FailureStage && hop.FailureCode != "") || (request.FailureStage == "response_headers" && request.FailureCode == "http_error" && hop.HTTPStatus != 0 && hop.HTTPStatus != 200 && hop.HTTPStatus != 206)
	if !proven {
		return 0, 0, false
	}
	for j := len(hop.Stages) - 1; j >= 0; j-- {
		stage := hop.Stages[j]
		if stage.Name == request.FailureStage {
			return stage.StartMS, stage.EndMS, true
		}
	}
	// The failed RoundTrip completed at this instant, but missing callbacks
	// do not establish the start/duration of a particular failed phase.
	return hop.EndMS, hop.EndMS, true
}

func playerPhaseFindings(requests []PlayerNetworkRequest, stall PlayerStall) []Finding {
	var result []Finding
	const maximum = 6
	add := func(request PlayerNetworkRequest, code, stage, message string, start, end int64, status int) {
		if len(result) >= maximum || end < stall.StartMS || start > stall.EndMS {
			return
		}
		number := request.Number
		result = append(result, Finding{Code: code, Severity: "warn", StartMS: start, EndMS: end, Occurrences: 1, Message: message,
			NextCheck: playerPhaseChecks[stage], Evidence: []Event{{Kind: "player_phase_" + stage, Artifact: playerNetworkName, Sequence: &number, ArrivalMS: start, DurationMS: end - start, HTTPStatus: status}}})
	}
	// Failed stages take precedence over slower successful phases in the
	// bounded selected-case evidence list.
	for _, request := range requests {
		if request.Outcome == "canceled" || request.Outcome == "incomplete" || request.EndMS < stall.StartMS || request.StartMS > stall.EndMS {
			continue
		}
		if label := playerPhaseLabels[request.FailureStage]; label != "" && request.FailureCode != "" {
			if start, end, known := failedPlayerPhaseWindow(request); known {
				add(request, "stall_player_phase_failed", request.FailureStage, fmt.Sprintf("实际播放代理请求 #%d 在%s报告失败（%s）；阶段为代理观测。", request.Number, label, request.FailureCode), start, end, request.HTTPStatus)
			}
		}
	}
	for _, request := range requests {
		if request.Outcome == "canceled" || request.Outcome == "incomplete" {
			continue
		}
		for _, hop := range request.Hops {
			for _, stage := range hop.Stages {
				if request.FailureCode != "" && stage.Name == request.FailureStage {
					continue
				}
				label := playerPhaseLabels[stage.Name]
				if label == "" || stage.EndMS-stage.StartMS < 500 {
					continue
				}
				add(request, "stall_player_phase_delay", stage.Name, fmt.Sprintf("实际播放代理请求 #%d 第 %d 跳（HTTP %d）的%s观测耗时 %d ms。", request.Number, hop.Number, hop.HTTPStatus, label, stage.EndMS-stage.StartMS), stage.StartMS, stage.EndMS, hop.HTTPStatus)
			}
		}
	}
	return result
}

func assessStallV4(item StallInvestigation) *StallAssessment {
	result := assessStallV3(item)
	result.Version = "stall-assessment/4"
	if !hasPlayerPhaseEvidence(item) {
		return result
	}
	var ids, checks []string
	for _, finding := range item.Correlation.Findings {
		if finding.Code != "stall_player_phase_delay" && finding.Code != "stall_player_phase_failed" {
			continue
		}
		ids = append(ids, finding.ID)
		if finding.NextCheck != "" && !slices.Contains(checks, finding.NextCheck) {
			checks = append(checks, finding.NextCheck)
		}
	}
	if len(ids) > 0 {
		result.Directions = append(result.Directions, StallDirection{Code: "playback_request_phases", Status: "observed", Title: "实际播放请求的逐跳阶段证据",
			Message: "代理记录到与所选卡顿同期的较长阶段或失败阶段，可以据此安排验证；慢阶段并不单独确定网络、源站或浏览器根因。", EvidenceIDs: ids, NextChecks: checks})
		for i := range result.Directions {
			if result.Directions[i].Code == "actual_playback_request" {
				for _, check := range checks {
					if !slices.Contains(result.Directions[i].NextChecks, check) {
						result.Directions[i].NextChecks = append(result.Directions[i].NextChecks, check)
					}
				}
			}
		}
	}
	result.Limitations = append(result.Limitations, "逐跳阶段来自实际播放代理的上游 HTTP 请求；连接复用或接口未提供时，DNS/TCP/TLS 等缺项表示未观测，不按零处理。响应体阶段可能与代理写出重叠。")
	if slices.ContainsFunc(item.Correlation.PlayerRequests, func(request PlayerNetworkRequest) bool { return request.DroppedHops > 0 }) {
		result.Limitations = append(result.Limitations, "逐跳记录达到条数或元数据预算，部分跳数未保留，缺失不能证明未发生重定向。")
	}
	return result
}
