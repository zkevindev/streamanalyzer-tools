package diagnostic

import (
	"cmp"
	"slices"
)

type modelPlayerHop struct {
	Number             int            `json:"number"`
	StartMS            int64          `json:"start_ms"`
	EndMS              int64          `json:"end_ms"`
	HTTPStatus         int            `json:"http_status,omitempty"`
	TTFBMS             *int64         `json:"ttfb_ms,omitempty"`
	ConnectionReused   *bool          `json:"connection_reused,omitempty"`
	Stages             []CaptureStage `json:"stages,omitempty"`
	FailureStage       string         `json:"failure_stage,omitempty"`
	FailureCode        string         `json:"failure_code,omitempty"`
	ModelOmittedStages int            `json:"model_omitted_stages,omitempty"`
}

type modelPlayerRequest struct {
	PlaylistSequence *uint64          `json:"playlist_sequence,omitempty"`
	Number           int              `json:"number"`
	ResourceID       string           `json:"resource_id"`
	Kind             string           `json:"kind"`
	Method           string           `json:"method"`
	StartMS          int64            `json:"start_ms"`
	HeaderMS         *int64           `json:"header_ms,omitempty"`
	FirstBodyMS      *int64           `json:"first_body_ms,omitempty"`
	EndMS            int64            `json:"end_ms"`
	HTTPStatus       int              `json:"http_status,omitempty"`
	Bytes            int64            `json:"bytes"`
	Outcome          string           `json:"outcome"`
	Hops             []modelPlayerHop `json:"hops,omitempty"`
	DroppedHops      int              `json:"dropped_hops,omitempty"`
	FailureStage     string           `json:"failure_stage,omitempty"`
	FailureCode      string           `json:"failure_code,omitempty"`
	ModelOmittedHops int              `json:"model_omitted_hops,omitempty"`
}

type modelPhaseBudget struct{ hops, stages int }

func modelPlaybackRequests(source []PlayerNetworkRequest, budget *modelPhaseBudget) []modelPlayerRequest {
	requests := slices.Clone(source)
	slices.SortStableFunc(requests, func(a, b PlayerNetworkRequest) int {
		return cmp.Compare(playerRequestPriority(b), playerRequestPriority(a))
	})
	result := make([]modelPlayerRequest, 0, len(requests))
	for _, request := range requests {
		value := modelPlayerRequest{PlaylistSequence: request.PlaylistSequence, Number: request.Number, ResourceID: request.ResourceID, Kind: request.Kind, Method: request.Method, StartMS: request.StartMS, HeaderMS: request.HeaderMS, FirstBodyMS: request.FirstBodyMS, EndMS: request.EndMS, HTTPStatus: request.HTTPStatus, Bytes: request.Bytes, Outcome: request.Outcome, DroppedHops: request.DroppedHops, FailureStage: request.FailureStage, FailureCode: request.FailureCode}
		hops := slices.Clone(request.Hops)
		slices.SortStableFunc(hops, func(a, b PlayerNetworkHop) int {
			priority := func(hop PlayerNetworkHop) int64 {
				score := hop.EndMS - hop.StartMS
				if hop.FailureCode != "" {
					score += 1_000_000
				}
				if hop.Number == 1 || hop.Number == len(request.Hops) {
					score += 100_000
				}
				return score
			}
			return cmp.Compare(priority(b), priority(a))
		})
		hops = hops[:min(3, len(hops), budget.hops)]
		value.ModelOmittedHops = len(request.Hops) - len(hops)
		for _, hop := range hops {
			stages := slices.Clone(hop.Stages)
			slices.SortStableFunc(stages, func(a, b CaptureStage) int { return cmp.Compare(b.EndMS-b.StartMS, a.EndMS-a.StartMS) })
			stages = stages[:min(6, len(stages), budget.stages)]
			budget.stages -= len(stages)
			budget.hops--
			slices.SortStableFunc(stages, func(a, b CaptureStage) int { return cmp.Compare(a.StartMS, b.StartMS) })
			value.Hops = append(value.Hops, modelPlayerHop{Number: hop.Number, StartMS: hop.StartMS, EndMS: hop.EndMS, HTTPStatus: hop.HTTPStatus, TTFBMS: hop.TTFBMS, ConnectionReused: hop.ConnectionReused, Stages: stages, FailureStage: hop.FailureStage, FailureCode: hop.FailureCode, ModelOmittedStages: len(hop.Stages) - len(stages)})
		}
		slices.SortStableFunc(value.Hops, func(a, b modelPlayerHop) int { return cmp.Compare(a.Number, b.Number) })
		result = append(result, value)
	}
	slices.SortStableFunc(result, func(a, b modelPlayerRequest) int { return cmp.Compare(a.Number, b.Number) })
	return result
}
