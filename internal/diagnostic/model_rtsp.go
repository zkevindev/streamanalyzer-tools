package diagnostic

import (
	"cmp"
	"slices"
)

type modelRTSPMetrics struct {
	RTSPMetrics
	OmittedRequests int `json:"omitted_requests"`
	OmittedTracks   int `json:"omitted_tracks"`
}

// RTSP's local archive contains raw SDP and authentication challenges. Only
// bounded, normalized protocol statistics enter the model request.
func modelRTSP(source *RTSPMetrics) *modelRTSPMetrics {
	if source == nil {
		return nil
	}
	result := &modelRTSPMetrics{RTSPMetrics: *source}
	result.Boundary = "native_rtsp_tcp_receive"
	result.Transport = "tcp_interleaved"
	result.Requests = append([]RTSPRequestTiming(nil), source.Requests...)
	const requestLimit = 32
	if len(result.Requests) > requestLimit {
		slices.SortStableFunc(result.Requests, func(a, b RTSPRequestTiming) int {
			priority := func(value RTSPRequestTiming) int {
				if value.FailureCode != "" || !value.Complete || value.StatusCode >= 300 {
					return 1
				}
				return 0
			}
			return cmp.Compare(priority(b), priority(a))
		})
		result.OmittedRequests = len(result.Requests) - requestLimit
		result.Requests = result.Requests[:requestLimit]
		slices.SortFunc(result.Requests, func(a, b RTSPRequestTiming) int { return cmp.Compare(a.Number, b.Number) })
	}
	for i := range result.Requests {
		request := &result.Requests[i]
		switch request.Method {
		case "OPTIONS", "DESCRIBE", "SETUP", "PLAY", "GET_PARAMETER", "SET_PARAMETER", "TEARDOWN":
		default:
			request.Method = "OTHER"
		}
		if !validCaptureFailure(request.FailureCode) {
			request.FailureCode = "rtsp_error"
		}
	}
	result.Tracks = append([]RTSPTrackMetrics(nil), source.Tracks...)
	const trackLimit = 16
	if len(result.Tracks) > trackLimit {
		slices.SortStableFunc(result.Tracks, func(a, b RTSPTrackMetrics) int {
			priority := func(value RTSPTrackMetrics) int {
				if !value.Received || value.SequenceGaps > 0 || value.MissingSequencePackets > 0 || value.MaxArrivalGapMS >= 3000 || value.AnalysisLimited {
					return 1
				}
				return 0
			}
			return cmp.Compare(priority(b), priority(a))
		})
		result.OmittedTracks = len(result.Tracks) - trackLimit
		result.Tracks = result.Tracks[:trackLimit]
		slices.SortFunc(result.Tracks, func(a, b RTSPTrackMetrics) int { return cmp.Compare(a.ID, b.ID) })
	}
	for i := range result.Tracks {
		track := &result.Tracks[i]
		if track.Kind != "video" && track.Kind != "audio" {
			track.Kind = "other"
		}
		switch track.Codec {
		case "h264", "h265", "av1", "vp8", "vp9", "aac", "opus", "g711", "pcma", "pcmu", "g722", "lpcm", "mpeg2video", "mpeg4video", "mpeg1audio", "jpeg", "ac3", "speex":
		default:
			track.Codec = "unknown"
		}
	}
	return result
}
