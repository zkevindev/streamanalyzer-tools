package diagnostic

import "fmt"

func whepFindings(report Report) []Finding {
	var findings []Finding
	obs := report.Manifest.Observation
	w := report.WHEP
	if w == nil {
		return findings
	}
	nextID := 1
	add := func(code, severity, message string, startMS, endMS int64, occurrences int, nextCheck string) {
		findings = append(findings, Finding{
			ID:          fmt.Sprintf("F%03d", nextID),
			Code:        code,
			Severity:    severity,
			Message:     message,
			StartMS:     max(0, startMS),
			EndMS:       max(startMS, endMS),
			Occurrences: occurrences,
			NextCheck:   nextCheck,
		})
		nextID++
	}

	// 1. HTTP Signaling failure
	if obs.HTTPStatus >= 400 || obs.FailureStage == "whep_signaling" {
		msg := fmt.Sprintf("WHEP 信令请求失败（HTTP %d）", obs.HTTPStatus)
		next := "检查 WHEP 地址、鉴权 Header（Authorization）、服务端流是否存在以及 SDP 编解码兼容性。"
		if obs.HTTPStatus == 401 || obs.HTTPStatus == 403 {
			msg = fmt.Sprintf("WHEP 信令鉴权失败（HTTP %d）", obs.HTTPStatus)
			next = "核对自定义请求头中的 Authorization Bearer Token 或签名参数是否过期。"
		} else if obs.HTTPStatus == 404 {
			msg = "WHEP 流或端点不存在（HTTP 404）"
			next = "确认推流端已上线且 WHEP 路径与流名配置一致。"
		} else if obs.FailureCode == "whep_sdp_error" {
			msg = "WHEP 信令返回了无效或无法协商的 Answer SDP"
			next = "检查服务端响应的 Content-Type 是否为 application/sdp 及音视频编解码能力是否匹配。"
		} else if obs.HTTPStatus == 0 {
			msg = "WHEP 信令连接失败：" + firstNonEmptyString(captureFailureText(obs), obs.FailureCode, "网络不可达")
		}
		add("whep_signaling_failed", "error", msg, 0, obs.ElapsedMS, 1, next)
		return findings
	}

	// 2. ICE / DTLS connection failure or slow connect
	if obs.FailureStage == "whep_ice" || obs.FailureCode == "whep_ice_failed" || w.ICEState == "failed" {
		add("whep_ice_failed", "error",
			fmt.Sprintf("WebRTC ICE 建连失败（最终状态 %s，本端候选 %d / 远端候选 %d）", w.ICEState, w.LocalCandidates, w.RemoteCandidates),
			0, obs.ElapsedMS, 1,
			"检查媒体服务器 UDP 端口放行、Answer SDP 中外网 Candidate IP 是否正确，或尝试配置 TURN 并使用 relay 策略复测。")
		return findings
	}
	if obs.FailureStage == "whep_dtls" || obs.FailureCode == "whep_dtls_failed" || w.DTLSState == "failed" {
		add("whep_dtls_failed", "error",
			fmt.Sprintf("ICE 已连通但 DTLS 握手失败（DTLS 状态 %s）", w.DTLSState),
			0, obs.ElapsedMS, 1,
			"检查路径 MTU 分片丢包、服务端 DTLS 证书指纹及 SDP a=setup 角色（actpass/active/passive）协商。")
		return findings
	}

	if w.ICEConnectMS != nil && *w.ICEConnectMS > 2000 {
		add("whep_ice_slow", "warn",
			fmt.Sprintf("ICE 建连耗时较长（%d ms），影响首屏起播速度", *w.ICEConnectMS),
			0, *w.ICEConnectMS, 1,
			"排查 STUN/TURN 响应延迟、无效内网 Candidate 过多或跨运营商连通性。")
	}

	// 3. Connected but no RTP media received
	if w.TotalPackets == 0 {
		add("whep_no_media", "error",
			"ICE / DTLS 已建连成功，但采样窗口内未收到任何 RTP 媒体包",
			0, obs.ElapsedMS, 1,
			"确认上游推流是否持续推送数据、SFU 转发路由是否已绑定该订阅者，以及是否因缺少关键帧/PLI 未下发首帧。")
		return findings
	}

	// 4. Mid-stream media stop
	if w.MediaStoppedGapMS >= 2000 && obs.EndReason == "duration_limit" {
		startMS := max(0, obs.ElapsedMS-w.MediaStoppedGapMS)
		add("whep_media_stopped", "error",
			fmt.Sprintf("采样后半段发生持续断流，最后 %d ms 未收到任何 RTP 媒体包", w.MediaStoppedGapMS),
			startMS, obs.ElapsedMS, 1,
			"排查推流端是否中途掉线、SFU 节点切换/主动踢流，或中间 NAT 会话超时导致 UDP 映射回收。")
	}

	// 5. Per-track RTP loss, jitter, out-of-order, keyframe & PLI
	thresholds := report.Manifest.thresholds()
	hasVideo, hasAudio := false, false
	for _, tr := range w.Tracks {
		if tr.Kind == "video" && tr.Received {
			hasVideo = true
		}
		if tr.Kind == "audio" && tr.Received {
			hasAudio = true
		}
		if !tr.Received {
			continue
		}

		if tr.MissingSequencePackets > 0 && tr.LossRatePct >= 0.5 {
			sev := "warn"
			if tr.LossRatePct >= 3.0 {
				sev = "error"
			}
			add("whep_rtp_packet_loss", sev,
				fmt.Sprintf("%s 轨（%s / SSRC 0x%08X）检测到 RTP 丢包 %d 包（丢包率 %.2f%%，%d 次序列空洞）",
					tr.Kind, firstNonEmptyString(tr.Codec, "unknown"), tr.SSRC, tr.MissingSequencePackets, tr.LossRatePct, tr.SequenceGaps),
				ptrVal(tr.FirstRTPMS), ptrVal(tr.LastRTPMS), tr.MissingSequencePackets,
				"核查公网弱网丢包、服务端 NACK 缓存窗口与 RTX 重传配置，或开启 TURN/TCP 对比验证。")
		}

		if tr.OutOfOrderPackets >= 5 {
			add("whep_rtp_out_of_order", "warn",
				fmt.Sprintf("%s 轨（SSRC 0x%08X）出现 %d 次 RTP 乱序到达", tr.Kind, tr.SSRC, tr.OutOfOrderPackets),
				ptrVal(tr.FirstRTPMS), ptrVal(tr.LastRTPMS), tr.OutOfOrderPackets,
				"检查多路径路由 ECMP 负载均衡导致的 UDP 乱序及播放器抖动缓冲策略。")
		}

		if tr.JitterMS >= 30.0 {
			sev := "warn"
			if tr.JitterMS >= 60.0 {
				sev = "error"
			}
			add("whep_rtp_jitter_high", sev,
				fmt.Sprintf("%s 轨（SSRC 0x%08X）RFC 3550 到达抖动偏高（均值 %.1f ms，峰值 %.1f ms）",
					tr.Kind, tr.SSRC, tr.JitterMS, tr.MaxJitterMS),
				ptrVal(tr.FirstRTPMS), ptrVal(tr.LastRTPMS), 1,
				"排查链路排队拥塞、Wi-Fi/蜂窝波动或推流端发包 pacing 不均匀。")
		}

		if tr.Kind == "video" {
			if tr.Keyframes == 0 {
				msg := fmt.Sprintf("视频轨（%s）收到 %d 包但全程未检测到关键帧（已发送 %d 次 PLI）",
					firstNonEmptyString(tr.Codec, "unknown"), tr.Packets, tr.PLICount)
				add("whep_pli_no_response", "error", msg, ptrVal(tr.FirstRTPMS), ptrVal(tr.LastRTPMS), max(1, tr.PLICount),
					"检查推流端/SFU 是否响应 RTCP PLI 请求并及时下发 IDR 关键帧，以及 SDP 中是否开启 rtcp-fb nack pli。")
			} else if tr.FirstKeyframeMS != nil && *tr.FirstKeyframeMS > thresholds.FirstFrameWarnMS {
				add("whep_keyframe_slow", "warn",
					fmt.Sprintf("视频首关键帧到达偏慢（%d ms，阈值 %d ms；PLI 次数 %d）",
						*tr.FirstKeyframeMS, thresholds.FirstFrameWarnMS, tr.PLICount),
					0, *tr.FirstKeyframeMS, 1,
					"检查 SFU 是否缓存最近 GOP 关键帧立即下发，或优化推流端响应 PLI 的速度。")
			}
			if tr.MaxFrameGapMS > thresholds.VideoGapWarnMS {
				add("whep_video_gap", "warn",
					fmt.Sprintf("视频轨最大帧间隔达到 %d ms（阈值 %d ms）", tr.MaxFrameGapMS, thresholds.VideoGapWarnMS),
					ptrVal(tr.FirstRTPMS), ptrVal(tr.LastRTPMS), 1,
					"排查源端卡顿、码率突发丢包或编码器掉帧。")
			}
		}
	}

	if w.AnswerSDP != nil {
		if w.AnswerSDP.HasVideo && !hasVideo {
			add("whep_no_video_track", "warn", "Answer SDP 声明了视频轨，但未收到任何视频 RTP 包", 0, obs.ElapsedMS, 1,
				"检查推流端视频采集/编码状态及 SFU 视频转发开关。")
		}
		if !w.AnswerSDP.HasAudio && !hasAudio {
			add("whep_no_audio_track", "info", "该 WHEP 流未协商音频轨（仅视频流）", 0, obs.ElapsedMS, 1,
				"如预期应含音频，请核对推流端音轨及 SFU Opus/G.711 转码配置。")
		}
	}

	return findings
}

func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func ptrVal(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
