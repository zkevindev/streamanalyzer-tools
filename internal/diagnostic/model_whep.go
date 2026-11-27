package diagnostic

import (
	"cmp"
	"slices"
)

type modelWHEPMetrics struct {
	WHEPMetrics
	OmittedTracks int `json:"omitted_tracks,omitempty"`
}

// modelWHEP projects bounded, credential-free WHEP diagnostics for LLM summaries.
func modelWHEP(source *WHEPMetrics) *modelWHEPMetrics {
	if source == nil {
		return nil
	}
	result := &modelWHEPMetrics{WHEPMetrics: *source}
	result.Boundary = "native_whep_webrtc_receive"
	result.LocationPath = "" // avoid leaking stream path tokens into model prompt
	result.ETag = ""
	if len(source.LinkICEServers) > 4 {
		result.LinkICEServers = append([]string(nil), source.LinkICEServers[:4]...)
	} else if len(source.LinkICEServers) > 0 {
		result.LinkICEServers = append([]string(nil), source.LinkICEServers...)
	}
	if source.SelectedPair != nil {
		cp := *source.SelectedPair
		result.SelectedPair = &cp
	}
	if source.OfferSDP != nil {
		cp := *source.OfferSDP
		result.OfferSDP = &cp
	}
	if source.AnswerSDP != nil {
		cp := *source.AnswerSDP
		result.AnswerSDP = &cp
	}
	result.Tracks = append([]WHEPTrackMetrics(nil), source.Tracks...)
	const trackLimit = 16
	if len(result.Tracks) > trackLimit {
		slices.SortStableFunc(result.Tracks, func(a, b WHEPTrackMetrics) int {
			score := func(v WHEPTrackMetrics) int {
				if v.MissingSequencePackets > 0 || v.OutOfOrderPackets > 0 || v.MaxArrivalGapMS >= 3000 || v.PLICount > 0 {
					return 1
				}
				return 0
			}
			return cmp.Compare(score(b), score(a))
		})
		result.OmittedTracks = len(result.Tracks) - trackLimit
		result.Tracks = result.Tracks[:trackLimit]
		slices.SortFunc(result.Tracks, func(a, b WHEPTrackMetrics) int { return cmp.Compare(a.ID, b.ID) })
	}
	return result
}
