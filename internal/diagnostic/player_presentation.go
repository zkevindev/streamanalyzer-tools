package diagnostic

import "slices"

const maxPlayerPresentationGaps = 128

type PlayerPresentationGap struct {
	StartMS         int64 `json:"start_ms"`
	EndMS           int64 `json:"end_ms"`
	ExpectedFrameMS int64 `json:"expected_frame_ms"`
}

type PlayerPresentationRecording struct {
	Support            string                  `json:"support"`
	ObservationStartMS int64                   `json:"observation_start_ms"`
	ObservationEndMS   int64                   `json:"observation_end_ms"`
	ReadyAtMS          *int64                  `json:"ready_at_ms,omitempty"`
	Gaps               []PlayerPresentationGap `json:"gaps"`
	DroppedGaps        int                     `json:"dropped_gaps"`
}

type PlayerPresentationGapReport struct {
	StartMS            int64 `json:"start_ms"`
	EndMS              int64 `json:"end_ms"`
	DurationMS         int64 `json:"duration_ms"`
	ExpectedFrameMS    int64 `json:"expected_frame_ms"`
	BufferingOverlapMS int64 `json:"buffering_overlap_ms"`
}

type PlayerPresentationReport struct {
	Support            string                        `json:"support"`
	ObservationStartMS int64                         `json:"observation_start_ms"`
	ObservationEndMS   int64                         `json:"observation_end_ms"`
	ReadyAtMS          *int64                        `json:"ready_at_ms,omitempty"`
	ActiveCoverageMS   int64                         `json:"active_coverage_ms"`
	ActiveIntervals    []PlayerSchedulingInterval    `json:"active_intervals"`
	Count              *int                          `json:"count,omitempty"`
	TotalMS            *int64                        `json:"total_ms,omitempty"`
	MaxMS              *int64                        `json:"max_ms,omitempty"`
	Gaps               []PlayerPresentationGapReport `json:"gaps"`
	DroppedGaps        int                           `json:"dropped_gaps"`
}

func validPlayerPresentation(value *PlayerPresentationRecording, elapsed int64) bool {
	if value == nil {
		return true
	}
	if (value.Support != "supported" && value.Support != "unsupported") || value.ObservationStartMS < 0 || value.ObservationEndMS < value.ObservationStartMS || value.ObservationEndMS > elapsed || len(value.Gaps) > maxPlayerPresentationGaps || value.DroppedGaps < 0 || value.DroppedGaps > 1_000_000_000 {
		return false
	}
	if value.ReadyAtMS == nil {
		return len(value.Gaps) == 0
	}
	if value.Support != "supported" || *value.ReadyAtMS < value.ObservationStartMS || *value.ReadyAtMS > value.ObservationEndMS {
		return false
	}
	previous := *value.ReadyAtMS
	for _, gap := range value.Gaps {
		if gap.ExpectedFrameMS <= 0 || gap.ExpectedFrameMS > 5000 || gap.StartMS < previous || gap.EndMS < gap.StartMS || gap.EndMS > value.ObservationEndMS || gap.EndMS-gap.StartMS < max(1000, 3*gap.ExpectedFrameMS) {
			return false
		}
		previous = gap.EndMS
	}
	return true
}

func playerPresentationExtends(previous, next *PlayerPresentationRecording) bool {
	if previous == nil {
		return true
	}
	if next == nil || next.Support != previous.Support || next.ObservationStartMS != previous.ObservationStartMS || next.ObservationEndMS < previous.ObservationEndMS || next.DroppedGaps < previous.DroppedGaps || len(next.Gaps) < len(previous.Gaps) {
		return false
	}
	if previous.ReadyAtMS != nil && (next.ReadyAtMS == nil || *previous.ReadyAtMS != *next.ReadyAtMS) {
		return false
	}
	return slices.Equal(previous.Gaps, next.Gaps[:len(previous.Gaps)])
}

// Closed callback gaps are retained whole, never clipped across an inactive
// period or EOF. Callback timing is not image-content or decoder proof.
func analyzePlayerPresentation(record PlayerRecording, spans []playerActiveSpan, observation Observation, stalls []PlayerStall) *PlayerPresentationReport {
	value := record.Presentation
	if value == nil {
		return nil
	}
	offset := record.StartedAt.Sub(observation.StartedAt).Milliseconds()
	result := &PlayerPresentationReport{Support: value.Support, ObservationStartMS: offset + value.ObservationStartMS, ObservationEndMS: offset + value.ObservationEndMS, ActiveIntervals: []PlayerSchedulingInterval{}, Gaps: []PlayerPresentationGapReport{}, DroppedGaps: value.DroppedGaps}
	if value.ReadyAtMS == nil {
		return result
	}
	ready := offset + *value.ReadyAtMS
	result.ReadyAtMS = &ready
	if value.Support != "supported" {
		return result
	}
	for _, span := range spans {
		start, end := max(span.start, value.ObservationStartMS, *value.ReadyAtMS), min(span.end, value.ObservationEndMS)
		if observation.EndReason != "" {
			end = min(end, observation.ElapsedMS-offset)
		}
		if end <= start {
			continue
		}
		result.ActiveIntervals = append(result.ActiveIntervals, PlayerSchedulingInterval{StartMS: offset + start, EndMS: offset + end})
		result.ActiveCoverageMS += end - start
	}
	if result.ActiveCoverageMS == 0 {
		return result
	}
	count, total, largest := 0, int64(0), int64(0)
	result.Count, result.TotalMS, result.MaxMS = &count, &total, &largest
	for _, gap := range value.Gaps {
		start, end := offset+gap.StartMS, offset+gap.EndMS
		if !slices.ContainsFunc(result.ActiveIntervals, func(span PlayerSchedulingInterval) bool { return start >= span.StartMS && end <= span.EndMS }) {
			continue
		}
		item := PlayerPresentationGapReport{StartMS: start, EndMS: end, DurationMS: end - start, ExpectedFrameMS: gap.ExpectedFrameMS}
		for _, stall := range stalls {
			item.BufferingOverlapMS += max(0, min(end, stall.EndMS)-max(start, stall.StartMS))
		}
		result.Gaps = append(result.Gaps, item)
		count++
		total += item.DurationMS
		largest = max(largest, item.DurationMS)
	}
	return result
}
