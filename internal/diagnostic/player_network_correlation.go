package diagnostic

import (
	"cmp"
	"fmt"
	"slices"
)

// The request trace belongs to the actual local HLS playback proxy, while
// capture requests still describe the independent diagnostic sampler.
func correlatePlayerRequests(result *StallCorrelation, report Report, stall PlayerStall) {
	if report.PlayerNetwork == nil {
		return // Keep legacy correlation byte-for-byte reproducible.
	}
	result.PlayerNetworkRecorded = true
	for _, request := range report.PlayerNetwork.Requests {
		if request.EndMS >= result.WindowStartMS && request.StartMS <= result.WindowEndMS {
			result.PlayerRequests = append(result.PlayerRequests, request)
		}
	}
	if len(result.PlayerRequests) > maxStallRequests {
		priority := func(request PlayerNetworkRequest) int64 {
			score := playerRequestPriority(request)
			if request.EndMS >= stall.StartMS && request.StartMS <= stall.EndMS {
				score += 2_000_000
			}
			return score
		}
		slices.SortStableFunc(result.PlayerRequests, func(a, b PlayerNetworkRequest) int {
			return cmp.Compare(priority(b), priority(a))
		})
		result.PlayerRequests = result.PlayerRequests[:maxStallRequests]
		slices.SortStableFunc(result.PlayerRequests, func(a, b PlayerNetworkRequest) int { return cmp.Compare(a.Number, b.Number) })
		result.Notes = append(result.Notes, "实际 HLS 播放请求较多，仅保留 16 条，优先保留失败或耗时较长的请求。")
	}
	if report.PlayerNetwork.DroppedRequests > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("播放请求台账达到上限，另有 %d 次请求未保留；未出现请求记录不能证明没有发起请求。", report.PlayerNetwork.DroppedRequests))
	}
	result.Notes = append(result.Notes, "实际播放请求在本地 HLS 代理观测：首字节表示代理读到源响应正文，结束表示代理处理结束，不能证明浏览器已接收、追加缓冲或呈帧。")
}

func playerRequestPriority(request PlayerNetworkRequest) int64 {
	if request.Outcome == "http_error" || request.Outcome == "transport_error" || request.Outcome == "timeout" {
		return 1_000_000 + request.EndMS - request.StartMS
	}
	return request.EndMS - request.StartMS
}

func playerRequestFindingCode(request PlayerNetworkRequest) string {
	if request.Method != "GET" || (request.Kind != "media" && request.Kind != "init" && request.Kind != "key") {
		return ""
	}
	if request.Outcome == "http_error" || request.Outcome == "transport_error" || request.Outcome == "timeout" {
		return "stall_player_request_failed"
	}
	if request.Outcome == "complete" && request.EndMS-request.StartMS >= 500 {
		return "stall_player_request_slow"
	}
	return ""
}

func assessStallV2(item StallInvestigation) *StallAssessment {
	result := assessStallV1(item)
	result.Version = "stall-assessment/2"
	if !item.Correlation.PlayerNetworkRecorded && len(item.Correlation.PlayerRequests) == 0 {
		return result
	}
	if result.Conclusion == "播放器缓冲接近耗尽，需要补采实际播放连接的分片请求。" {
		result.Conclusion = "已观测到缓冲接近耗尽，当前实际播放请求尚不足以解释；继续核对媒体供给、清单更新和浏览器追加缓冲。"
	}
	var ids []string
	inactive := slices.ContainsFunc(item.Correlation.BufferSamples, func(sample PlayerBufferSample) bool {
		return sample.OffsetMS >= item.Stall.StartMS && sample.OffsetMS <= item.Stall.EndMS && (sample.Paused || sample.Seeking || sample.Hidden)
	})
	for _, request := range item.Correlation.PlayerRequests {
		if inactive || playerRequestFindingCode(request) == "" || request.EndMS < item.Stall.StartMS || request.StartMS > item.Stall.EndMS {
			continue
		}
		nearUnderflow := slices.ContainsFunc(item.Correlation.BufferSamples, func(sample PlayerBufferSample) bool {
			return sample.OffsetMS >= item.Stall.StartMS && sample.OffsetMS <= item.Stall.EndMS &&
				!sample.Paused && !sample.Seeking && !sample.Hidden && sample.BufferMS <= 250 &&
				sample.OffsetMS >= request.StartMS-500 && sample.OffsetMS <= request.EndMS+500
		})
		if !nearUnderflow {
			continue
		}
		for _, finding := range item.Correlation.Findings {
			if finding.Code == playerRequestFindingCode(request) && finding.StartMS == request.StartMS && finding.EndMS == request.EndMS &&
				slices.ContainsFunc(finding.Evidence, func(event Event) bool {
					return event.Kind == "player_request" && event.Sequence != nil && *event.Sequence == request.Number
				}) {
				ids = append(ids, finding.ID)
			}
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > 0 {
		result.Directions = slices.DeleteFunc(result.Directions, func(direction StallDirection) bool { return direction.Code == "insufficient_evidence" })
		result.Directions = append(result.Directions, StallDirection{
			Code: "actual_playback_request", Status: "candidate", Title: "优先核对实际 HLS 播放请求",
			Message:     "实际播放代理的媒体、初始化或密钥请求变慢或失败，附近观测到低缓冲（样本容差 500 ms）；需要核对上游响应与本地代理处理，不能据此认定浏览器解码故障。",
			EvidenceIDs: ids, NextChecks: []string{"对照该请求的响应头、首正文时刻、HTTP 状态和代理处理结束时间。", "在源端与另一接收点检查同一资源，并复测节点或网络；结合播放器追加缓冲日志核对代理之后的处理。"},
		})
		result.Conclusion = "实际 HLS 播放请求异常与低缓冲相邻，优先核对这条播放路径的响应与处理。"
	}
	result.Limitations = append(result.Limitations, "播放请求记录来自本地代理的上游侧，包含代理处理与写出影响；它不能证明浏览器已经接收或消费这些字节。")
	return result
}

func recomputeStallAssessment(item StallInvestigation, version string) (*StallAssessment, error) {
	switch version {
	case "stall-assessment/1":
		if item.Correlation.PlayerNetworkRecorded || len(item.Correlation.PlayerRequests) > 0 || item.Correlation.Scheduling != nil {
			return nil, fmt.Errorf("%w: 排查分层版本与证据类型不一致", ErrInvalid)
		}
		return assessStallV1(item), nil
	case "stall-assessment/2":
		if item.Correlation.Scheduling != nil {
			return nil, fmt.Errorf("%w: 排查分层版本与证据类型不一致", ErrInvalid)
		}
		return assessStallV2(item), nil
	case stallAssessmentVersion:
		return assessStall(item), nil
	default:
		return nil, fmt.Errorf("%w: 不支持的排查分层规则版本", ErrInvalid)
	}
}
