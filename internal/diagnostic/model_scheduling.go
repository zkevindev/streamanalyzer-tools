package diagnostic

import (
	"cmp"
	"slices"
)

type modelSchedulingMetrics struct {
	Count            int                        `json:"count"`
	TotalMS          int64                      `json:"total_ms"`
	MaxMS            int64                      `json:"max_ms"`
	Intervals        []PlayerSchedulingInterval `json:"intervals"`
	OmittedIntervals int                        `json:"omitted_intervals"`
}

type modelPlayerScheduling struct {
	LongTaskSupport    string                  `json:"longtask_support"`
	ObservationStartMS int64                   `json:"observation_start_ms"`
	ObservationEndMS   int64                   `json:"observation_end_ms"`
	ActiveCoverageMS   int64                   `json:"active_coverage_ms"`
	LongTasks          *modelSchedulingMetrics `json:"long_tasks,omitempty"`
	TimerDelays        *modelSchedulingMetrics `json:"timer_delays,omitempty"`
	DroppedLongTasks   int                     `json:"dropped_long_tasks"`
	DroppedTimerDelays int                     `json:"dropped_timer_delays"`
}

func modelSchedulingGroup(source *PlayerSchedulingMetrics) *modelSchedulingMetrics {
	if source == nil {
		return nil
	}
	intervals := append([]PlayerSchedulingInterval{}, source.Intervals...)
	const maximum = 16
	if len(intervals) > maximum {
		slices.SortStableFunc(intervals, func(a, b PlayerSchedulingInterval) int { return cmp.Compare(b.EndMS-b.StartMS, a.EndMS-a.StartMS) })
		intervals = intervals[:maximum]
		slices.SortStableFunc(intervals, func(a, b PlayerSchedulingInterval) int { return cmp.Compare(a.StartMS, b.StartMS) })
	}
	return &modelSchedulingMetrics{Count: source.Count, TotalMS: source.TotalMS, MaxMS: source.MaxMS, Intervals: intervals, OmittedIntervals: len(source.Intervals) - len(intervals)}
}

func modelScheduling(source *PlayerSchedulingReport) *modelPlayerScheduling {
	if source == nil {
		return nil
	}
	return &modelPlayerScheduling{LongTaskSupport: source.LongTaskSupport, ObservationStartMS: source.ObservationStartMS, ObservationEndMS: source.ObservationEndMS,
		ActiveCoverageMS: source.ActiveCoverageMS, LongTasks: modelSchedulingGroup(source.LongTasks), TimerDelays: modelSchedulingGroup(source.TimerDelays), DroppedLongTasks: source.DroppedLongTasks, DroppedTimerDelays: source.DroppedTimerDelays}
}
