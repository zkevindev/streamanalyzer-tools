package diagnostic

import (
	"cmp"
	"slices"
)

type modelPlayerPresentation struct {
	Support            string                        `json:"support"`
	ObservationStartMS int64                         `json:"observation_start_ms"`
	ObservationEndMS   int64                         `json:"observation_end_ms"`
	ReadyAtMS          *int64                        `json:"ready_at_ms,omitempty"`
	ActiveCoverageMS   int64                         `json:"active_coverage_ms"`
	Count              *int                          `json:"count,omitempty"`
	TotalMS            *int64                        `json:"total_ms,omitempty"`
	MaxMS              *int64                        `json:"max_ms,omitempty"`
	Gaps               []PlayerPresentationGapReport `json:"gaps"`
	DroppedGaps        int                           `json:"dropped_gaps"`
	ModelOmittedGaps   int                           `json:"model_omitted_gaps,omitempty"`
}

func modelPresentation(source *PlayerPresentationReport) *modelPlayerPresentation {
	if source == nil {
		return nil
	}
	gaps := append([]PlayerPresentationGapReport{}, source.Gaps...)
	if len(gaps) > 16 {
		slices.SortStableFunc(gaps, func(a, b PlayerPresentationGapReport) int { return cmp.Compare(b.DurationMS, a.DurationMS) })
		gaps = gaps[:16]
		slices.SortStableFunc(gaps, func(a, b PlayerPresentationGapReport) int { return cmp.Compare(a.StartMS, b.StartMS) })
	}
	return &modelPlayerPresentation{Support: source.Support, ObservationStartMS: source.ObservationStartMS, ObservationEndMS: source.ObservationEndMS, ReadyAtMS: source.ReadyAtMS, ActiveCoverageMS: source.ActiveCoverageMS, Count: source.Count, TotalMS: source.TotalMS, MaxMS: source.MaxMS, Gaps: gaps, DroppedGaps: source.DroppedGaps, ModelOmittedGaps: len(source.Gaps) - len(gaps)}
}
