package diagnostic

import "slices"

const maxPlayerSchedulingIntervals = 256

// Raw intervals use the player's clock. Derived intervals use capture offsets.
type PlayerSchedulingInterval struct {
	StartMS int64 `json:"start_ms"`
	EndMS   int64 `json:"end_ms"`
}

type PlayerSchedulingRecording struct {
	LongTaskSupport    string                     `json:"longtask_support"`
	ObservationStartMS int64                      `json:"observation_start_ms"`
	ObservationEndMS   int64                      `json:"observation_end_ms"`
	LongTasks          []PlayerSchedulingInterval `json:"long_tasks"`
	TimerDelays        []PlayerSchedulingInterval `json:"timer_delays"`
	DroppedLongTasks   int                        `json:"dropped_long_tasks"`
	DroppedTimerDelays int                        `json:"dropped_timer_delays"`
}

// Metrics count the observed active fragments after clipping. A raw task that
// crosses an excluded span can contribute multiple fragments; no stalls are
// synthesized from task or timer observations.
type PlayerSchedulingMetrics struct {
	Count     int                        `json:"count"`
	TotalMS   int64                      `json:"total_ms"`
	MaxMS     int64                      `json:"max_ms"`
	Intervals []PlayerSchedulingInterval `json:"intervals"`
}

type PlayerSchedulingReport struct {
	LongTaskSupport    string                     `json:"longtask_support"`
	ObservationStartMS int64                      `json:"observation_start_ms"`
	ObservationEndMS   int64                      `json:"observation_end_ms"`
	ActiveCoverageMS   int64                      `json:"active_coverage_ms"`
	ActiveIntervals    []PlayerSchedulingInterval `json:"active_intervals"`
	LongTasks          *PlayerSchedulingMetrics   `json:"long_tasks,omitempty"`
	TimerDelays        *PlayerSchedulingMetrics   `json:"timer_delays,omitempty"`
	DroppedLongTasks   int                        `json:"dropped_long_tasks"`
	DroppedTimerDelays int                        `json:"dropped_timer_delays"`
}

func validPlayerScheduling(value *PlayerSchedulingRecording, elapsed int64) bool {
	if value == nil {
		return true
	}
	if (value.LongTaskSupport != "supported" && value.LongTaskSupport != "unsupported") || value.ObservationStartMS < 0 || value.ObservationEndMS < value.ObservationStartMS || value.ObservationEndMS > elapsed || len(value.LongTasks) > maxPlayerSchedulingIntervals || len(value.TimerDelays) > maxPlayerSchedulingIntervals || value.DroppedLongTasks < 0 || value.DroppedLongTasks > 1_000_000_000 || value.DroppedTimerDelays < 0 || value.DroppedTimerDelays > 1_000_000_000 {
		return false
	}
	if value.LongTaskSupport == "unsupported" && len(value.LongTasks) > 0 {
		return false
	}
	for index, intervals := range [][]PlayerSchedulingInterval{value.LongTasks, value.TimerDelays} {
		minimum := int64(50)
		if index == 1 {
			minimum = 100
		}
		previous := value.ObservationStartMS
		for _, interval := range intervals {
			if interval.StartMS < previous || interval.EndMS < interval.StartMS || interval.EndMS > value.ObservationEndMS || interval.EndMS-interval.StartMS < minimum {
				return false
			}
			previous = interval.EndMS
		}
	}
	return true
}

func playerSchedulingExtends(previous, next *PlayerSchedulingRecording) bool {
	if previous == nil {
		return true
	}
	if next == nil || next.LongTaskSupport != previous.LongTaskSupport || next.ObservationStartMS != previous.ObservationStartMS || next.ObservationEndMS < previous.ObservationEndMS || next.DroppedLongTasks < previous.DroppedLongTasks || next.DroppedTimerDelays < previous.DroppedTimerDelays {
		return false
	}
	return len(next.LongTasks) >= len(previous.LongTasks) && len(next.TimerDelays) >= len(previous.TimerDelays) && slices.Equal(previous.LongTasks, next.LongTasks[:len(previous.LongTasks)]) && slices.Equal(previous.TimerDelays, next.TimerDelays[:len(previous.TimerDelays)])
}

func playerSchedulingMetrics(intervals []PlayerSchedulingInterval, active []PlayerSchedulingInterval, offset int64) *PlayerSchedulingMetrics {
	result := &PlayerSchedulingMetrics{Intervals: []PlayerSchedulingInterval{}}
	for _, interval := range intervals {
		for _, span := range active {
			start, end := max(offset+interval.StartMS, span.StartMS), min(offset+interval.EndMS, span.EndMS)
			if end <= start {
				continue
			}
			result.Intervals = append(result.Intervals, PlayerSchedulingInterval{StartMS: start, EndMS: end})
			result.TotalMS += end - start
			result.MaxMS = max(result.MaxMS, end-start)
		}
	}
	result.Count = len(result.Intervals)
	return result
}

func analyzePlayerScheduling(record PlayerRecording, spans []playerActiveSpan, observation Observation) *PlayerSchedulingReport {
	value := record.Scheduling
	if value == nil {
		return nil
	}
	offset := record.StartedAt.Sub(observation.StartedAt).Milliseconds()
	result := &PlayerSchedulingReport{LongTaskSupport: value.LongTaskSupport, ObservationStartMS: offset + value.ObservationStartMS, ObservationEndMS: offset + value.ObservationEndMS, ActiveIntervals: []PlayerSchedulingInterval{}, DroppedLongTasks: value.DroppedLongTasks, DroppedTimerDelays: value.DroppedTimerDelays}
	for _, span := range spans {
		start, end := max(span.start, value.ObservationStartMS), min(span.end, value.ObservationEndMS)
		if observation.EndReason != "" {
			end = min(end, observation.ElapsedMS-offset)
		}
		if end <= start {
			continue
		}
		result.ActiveIntervals = append(result.ActiveIntervals, PlayerSchedulingInterval{StartMS: offset + start, EndMS: offset + end})
		result.ActiveCoverageMS += end - start
	}
	if result.ActiveCoverageMS > 0 {
		if value.LongTaskSupport == "supported" {
			result.LongTasks = playerSchedulingMetrics(value.LongTasks, result.ActiveIntervals, offset)
		}
		result.TimerDelays = playerSchedulingMetrics(value.TimerDelays, result.ActiveIntervals, offset)
	}
	return result
}
