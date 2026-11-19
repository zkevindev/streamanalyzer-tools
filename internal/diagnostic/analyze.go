package diagnostic

import (
	"fmt"
	"slices"

	"streamanalyzer/internal/codec"
)

const (
	firstFrameWarnMS = 3000
	videoGapWarnMS   = 3000
	dtsGapWarnMS     = 2000
	gopWarnMS        = 10000
)

func analyze(manifest Manifest, events []Event) Report {
	report := Report{RuleVersion: manifest.Protocol + "/2", Manifest: manifest, Findings: []Finding{}}
	obs := manifest.Observation
	metrics := &report.Metrics
	var firstVideo, lastVideo *Event
	var longestGapStart, longestGapEnd *Event
	var lastAudio, lastKey, audioGapStart, audioGapEnd *Event
	seen := make(map[string]int)
	addOnce := func(code, severity, message string, start, end int64, evidence ...Event) {
		if index, ok := seen[code]; ok {
			finding := &report.Findings[index]
			finding.Occurrences++
			finding.EndMS = max(finding.EndMS, end)
			return
		}
		seen[code] = len(report.Findings)
		report.Findings = append(report.Findings, Finding{Code: code, Severity: severity, Message: message, StartMS: start, EndMS: end, Evidence: evidence, Occurrences: 1})
	}

	checkDTS := func(previous *Event, current Event) {
		if previous == nil {
			return
		}
		delta := current.DTS - previous.DTS
		track := "视频"
		if current.Kind == "audio" {
			track = "音频"
		}
		switch {
		case delta < 0:
			addOnce(current.Kind+"_dts_backwards", "warn", fmt.Sprintf("%s DTS 回退 %d ms", track, -delta), previous.ArrivalMS, current.ArrivalMS, *previous, current)
		case delta > dtsGapWarnMS:
			addOnce(current.Kind+"_dts_gap", "warn", fmt.Sprintf("%s DTS 间隔 %d ms，超过 %d ms", track, delta, dtsGapWarnMS), previous.ArrivalMS, current.ArrivalMS, *previous, current)
		}
	}

	for i := range events {
		event := &events[i]
		switch event.Kind {
		case "video", "video_aux":
			metrics.VideoCodecs = addCodec(metrics.VideoCodecs, codec.VideoCodecName(event.Codec))
		case "audio", "audio_aux":
			name := codec.AudioCodecName(event.Codec)
			if name == "mpeg4-audio" {
				// Enhanced FLV/RTMP assigns mp4a specifically to AAC.
				name = "aac"
			}
			metrics.AudioCodecs = addCodec(metrics.AudioCodecs, name)
		}
		switch event.Kind {
		case "video":
			metrics.VideoFrames++
			checkDTS(lastVideo, *event)
			if firstVideo == nil {
				firstVideo = event
				metrics.FirstVideoMS = &event.ArrivalMS
			}
			if lastVideo != nil && event.ArrivalMS-lastVideo.ArrivalMS > metrics.MaxVideoGapMS {
				metrics.MaxVideoGapMS = event.ArrivalMS - lastVideo.ArrivalMS
				longestGapStart, longestGapEnd = lastVideo, event
			}
			lastVideo = event
			if event.Keyframe {
				if metrics.FirstKeyframeMS == nil {
					metrics.FirstKeyframeMS = &event.ArrivalMS
				}
				if lastKey != nil {
					gop := event.DTS - lastKey.DTS
					metrics.MaxGOPMS = max(metrics.MaxGOPMS, gop)
					if gop > gopWarnMS {
						addOnce("long_gop", "warn", fmt.Sprintf("视频关键帧间隔 %d ms，超过 %d ms", gop, gopWarnMS), lastKey.ArrivalMS, event.ArrivalMS, *lastKey, *event)
					}
				}
				lastKey = event
			}
		case "audio":
			metrics.AudioFrames++
			checkDTS(lastAudio, *event)
			if metrics.FirstAudioMS == nil {
				metrics.FirstAudioMS = &event.ArrivalMS
			}
			if lastAudio != nil && event.ArrivalMS-lastAudio.ArrivalMS > metrics.MaxAudioGapMS {
				metrics.MaxAudioGapMS = event.ArrivalMS - lastAudio.ArrivalMS
				audioGapStart, audioGapEnd = lastAudio, event
			}
			lastAudio = event
		case "invalid_audio", "invalid_video", "invalid_tag_size":
			message := "FLV 标签结构异常，参见原始流的字节位置"
			if manifest.Protocol == ProtocolRTMP {
				message = "RTMP 媒体消息结构异常，参见原始流的字节位置"
			}
			addOnce(event.Kind, "warn", message, event.ArrivalMS, event.ArrivalMS, *event)
		}
	}
	slices.Sort(metrics.VideoCodecs)
	slices.Sort(metrics.AudioCodecs)

	if firstVideo != nil && firstVideo.ArrivalMS > firstFrameWarnMS {
		addOnce("slow_first_video", "warn", fmt.Sprintf("首个视频帧在 %d ms 到达，超过 %d ms", firstVideo.ArrivalMS, firstFrameWarnMS), 0, firstVideo.ArrivalMS, *firstVideo)
	}
	if metrics.MaxVideoGapMS > videoGapWarnMS {
		addOnce("video_arrival_gap", "warn", fmt.Sprintf("采样点连续 %d ms 未收到新视频帧", metrics.MaxVideoGapMS), longestGapStart.ArrivalMS, longestGapEnd.ArrivalMS, *longestGapStart, *longestGapEnd)
	}
	if metrics.MaxAudioGapMS > videoGapWarnMS {
		addOnce("audio_arrival_gap", "warn", fmt.Sprintf("采样点连续 %d ms 未收到新音频帧", metrics.MaxAudioGapMS), audioGapStart.ArrivalMS, audioGapEnd.ArrivalMS, *audioGapStart, *audioGapEnd)
	}
	if lastAudio != nil && obs.EndReason == "duration_limit" && obs.ElapsedMS-lastAudio.ArrivalMS > videoGapWarnMS {
		addOnce("audio_tail_gap", "warn", fmt.Sprintf("采样结束前 %d ms 未收到新音频帧", obs.ElapsedMS-lastAudio.ArrivalMS), lastAudio.ArrivalMS, obs.ElapsedMS, *lastAudio)
	}
	if firstVideo != nil && lastKey == nil && obs.ElapsedMS-firstVideo.ArrivalMS > firstFrameWarnMS {
		addOnce("no_video_keyframe", "warn", "已收到视频帧，采样窗口内尚未观察到关键帧", firstVideo.ArrivalMS, obs.ElapsedMS, *firstVideo)
	}
	if lastVideo != nil && obs.EndReason == "duration_limit" && obs.ElapsedMS-lastVideo.ArrivalMS > videoGapWarnMS {
		addOnce("video_tail_gap", "warn", fmt.Sprintf("采样结束前 %d ms 未收到新视频帧", obs.ElapsedMS-lastVideo.ArrivalMS), lastVideo.ArrivalMS, obs.ElapsedMS, *lastVideo)
	}
	if firstVideo == nil && lastAudio == nil {
		addOnce("no_media", "warn", "采样窗口内没有读到完整的音视频帧", 0, obs.ElapsedMS)
	}

	reason := endReasonText(obs.EndReason)
	switch obs.EndReason {
	case "http_status":
		addOnce("http_status", "error", fmt.Sprintf("播放请求收到 HTTP %d", obs.HTTPStatus), 0, obs.ElapsedMS)
	case "request_error", "invalid_flv", "truncated_tag", "body_error", "rtmp_error":
		addOnce(obs.EndReason, "error", reason, obs.ElapsedMS, obs.ElapsedMS)
	case "size_limit", "tag_limit", "canceled":
		addOnce(obs.EndReason, "info", reason, obs.ElapsedMS, obs.ElapsedMS)
	}
	addObservationFindings(&report)

	slices.SortStableFunc(report.Findings, func(left, right Finding) int {
		switch {
		case left.StartMS < right.StartMS:
			return -1
		case left.StartMS > right.StartMS:
			return 1
		default:
			return 0
		}
	})
	for i := range report.Findings {
		report.Findings[i].ID = fmt.Sprintf("F%03d", i+1)
	}
	enrichFindings(&report)
	return report
}

func addCodec(names []string, name string) []string {
	if name == "" || slices.Contains(names, name) {
		return names
	}
	return append(names, name)
}

func endReasonText(reason string) string {
	switch reason {
	case "eof":
		return "响应结束"
	case "duration_limit":
		return "达到采样时长"
	case "canceled":
		return "用户停止采样，已保留采样前缀"
	case "size_limit":
		return "达到原始流大小上限，已保留采样前缀"
	case "tag_limit":
		return "达到标签数量上限，已保留采样前缀"
	case "timeline_limit":
		return "达到接收时间线大小上限，已保留完整的数据报前缀"
	case "udp_bind_error":
		return "无法绑定 UDP 接收端口或加入组播"
	case "udp_read_error":
		return "读取 UDP 数据报失败"
	case "http_status":
		return "播放请求返回非 200 状态"
	case "request_error":
		return "播放请求未获得 HTTP 响应"
	case "invalid_flv":
		return "响应头部不是受支持的 FLV 结构"
	case "truncated_tag":
		return "响应在 FLV 标签结束前中断"
	case "body_error":
		return "读取响应体时连接中断"
	case "rtmp_error":
		return "RTMP 建连、播放或接收消息失败"
	case "hls_error":
		return "HLS 播放列表不可用"
	case "rtsp_error":
		return "RTSP 协商或媒体接收失败"
	default:
		return ""
	}
}
