package diagnostic

const maxPlayerVideoCounter = 1_000_000_000

// PlayerVideoSample contains browser counters. FrameAgeMS and MaxFrameGapMS
// describe video-frame callbacks, not image-content freeze detection.
type PlayerVideoSample struct {
	QualitySource   string `json:"quality_source"`
	TotalFrames     *int64 `json:"total_frames,omitempty"`
	DroppedFrames   *int64 `json:"dropped_frames,omitempty"`
	PresentedFrames *int64 `json:"presented_frames,omitempty"`
	FrameAgeMS      *int64 `json:"frame_age_ms,omitempty"`
	MaxFrameGapMS   *int64 `json:"max_frame_gap_ms,omitempty"`
}

// Counts are observed increments between adjacent samples within a continuous
// foreground playback window; unsupported counters remain unknown.
type PlayerVideoReport struct {
	QualitySource   string   `json:"quality_source"`
	TotalFrames     *int64   `json:"total_frames,omitempty"`
	DroppedFrames   *int64   `json:"dropped_frames,omitempty"`
	PresentedFrames *int64   `json:"presented_frames,omitempty"`
	DroppedRatio    *float64 `json:"dropped_ratio,omitempty"`
	MaxFrameGapMS   *int64   `json:"max_frame_gap_ms,omitempty"`
	CounterResets   int      `json:"counter_resets"`
	SampleCount     int      `json:"sample_count"`
}

type playerActiveSpan struct{ start, end int64 }

func validPlayerVideoSample(video PlayerVideoSample, at int64) bool {
	switch video.QualitySource {
	case "playback_quality", "webkit":
		if video.TotalFrames == nil || video.DroppedFrames == nil {
			return false
		}
	case "unavailable":
		if video.TotalFrames != nil || video.DroppedFrames != nil {
			return false
		}
	default:
		return false
	}
	for _, counter := range []*int64{video.TotalFrames, video.DroppedFrames, video.PresentedFrames} {
		if counter != nil && (*counter < 0 || *counter > maxPlayerVideoCounter) {
			return false
		}
	}
	if video.TotalFrames != nil && video.DroppedFrames != nil && *video.DroppedFrames > *video.TotalFrames {
		return false
	}
	for _, ms := range []*int64{video.FrameAgeMS, video.MaxFrameGapMS} {
		if ms != nil && (*ms < 0 || *ms > maxPlayerElapsedMS || *ms > at) {
			return false
		}
	}
	return true
}

func cloneVideoNumber(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func clonePlayerVideoSample(video *PlayerVideoSample) *PlayerVideoSample {
	if video == nil {
		return nil
	}
	return &PlayerVideoSample{QualitySource: video.QualitySource, TotalFrames: cloneVideoNumber(video.TotalFrames), DroppedFrames: cloneVideoNumber(video.DroppedFrames), PresentedFrames: cloneVideoNumber(video.PresentedFrames), FrameAgeMS: cloneVideoNumber(video.FrameAgeMS), MaxFrameGapMS: cloneVideoNumber(video.MaxFrameGapMS)}
}

func addObservedVideoCount(value **int64, delta int64) {
	if *value == nil {
		zero := int64(0)
		*value = &zero
	}
	**value += delta
}

func analyzePlayerVideo(samples []PlayerSample, spans []playerActiveSpan) *PlayerVideoReport {
	var result *PlayerVideoReport
	for _, sample := range samples {
		if sample.Video != nil {
			if result == nil {
				result = &PlayerVideoReport{QualitySource: "unavailable"}
			}
			if source := sample.Video.QualitySource; source != "unavailable" {
				if result.QualitySource == "unavailable" {
					result.QualitySource = source
				} else if result.QualitySource != source {
					result.QualitySource = "mixed"
				}
			}
		}
	}
	if result == nil {
		return nil
	}
	var previous *PlayerVideoSample
	previousSpan := -1
	spanIndex := 0
	for _, sample := range samples {
		for spanIndex < len(spans) && sample.AtMS >= spans[spanIndex].end {
			spanIndex++
		}
		active := spanIndex < len(spans) && sample.AtMS >= spans[spanIndex].start && sample.AtMS < spans[spanIndex].end && !sample.Paused && !sample.Seeking && !sample.Hidden
		if !active || sample.Video == nil {
			previous = nil
			previousSpan = -1
			continue
		}
		video := sample.Video
		result.SampleCount++
		if video.MaxFrameGapMS != nil && (result.MaxFrameGapMS == nil || *video.MaxFrameGapMS > *result.MaxFrameGapMS) {
			result.MaxFrameGapMS = cloneVideoNumber(video.MaxFrameGapMS)
		}
		if previous != nil && previousSpan == spanIndex {
			reset := false
			if video.QualitySource == previous.QualitySource && video.TotalFrames != nil && video.DroppedFrames != nil && previous.TotalFrames != nil && previous.DroppedFrames != nil {
				totalDelta, droppedDelta := *video.TotalFrames-*previous.TotalFrames, *video.DroppedFrames-*previous.DroppedFrames
				if totalDelta < 0 || droppedDelta < 0 {
					reset = true
				} else if droppedDelta <= totalDelta {
					addObservedVideoCount(&result.TotalFrames, totalDelta)
					addObservedVideoCount(&result.DroppedFrames, droppedDelta)
				}
			}
			if video.PresentedFrames != nil && previous.PresentedFrames != nil {
				if delta := *video.PresentedFrames - *previous.PresentedFrames; delta < 0 {
					reset = true
				} else {
					addObservedVideoCount(&result.PresentedFrames, delta)
				}
			}
			if reset {
				result.CounterResets++
			}
		}
		previous, previousSpan = video, spanIndex
	}
	if result.TotalFrames != nil && result.DroppedFrames != nil && *result.TotalFrames > 0 {
		ratio := float64(*result.DroppedFrames) / float64(*result.TotalFrames)
		result.DroppedRatio = &ratio
	}
	return result
}

func equalPlayerSamples(left, right []PlayerSample) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		a, b := left[i], right[i]
		av, bv := a.Video, b.Video
		a.Video, b.Video = nil, nil
		if a != b || !equalPlayerVideoSamples(av, bv) {
			return false
		}
	}
	return true
}

func equalVideoNumber(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func equalPlayerVideoSamples(a, b *PlayerVideoSample) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.QualitySource == b.QualitySource && equalVideoNumber(a.TotalFrames, b.TotalFrames) && equalVideoNumber(a.DroppedFrames, b.DroppedFrames) && equalVideoNumber(a.PresentedFrames, b.PresentedFrames) && equalVideoNumber(a.FrameAgeMS, b.FrameAgeMS) && equalVideoNumber(a.MaxFrameGapMS, b.MaxFrameGapMS)
}
