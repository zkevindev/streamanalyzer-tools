package diagnostic

import (
	"fmt"
	"slices"
)

const tsClockMedianSize = 5

type TSProgramClockSummary struct {
	PCRPID      uint16 `json:"pcr_pid"`
	Samples     int    `json:"samples"`
	StartMS     int64  `json:"start_ms"`
	EndMS       int64  `json:"end_ms"`
	FirstLeadMS int64  `json:"first_lead_ms"`
	LastLeadMS  int64  `json:"last_lead_ms"`
	DriftMS     int64  `json:"drift_ms"`
}

type tsClockKey struct{ program, pid uint16 }

type tsProgramClock struct {
	pcrPID               uint16
	pcrEpoch, mediaEpoch uint64
	count                int
	early, recent        [tsClockMedianSize]int64
	first, last          Event
}

func (a *tsAnalyzer) observeProgramClock(media *tsPID, pts uint64, event Event) {
	for program, state := range a.programs {
		if a.pmt[state.pmt] != program || !slices.ContainsFunc(state.mapping.Streams, func(stream TSProgramPID) bool {
			return media.metrics.PID == stream.PID
		}) {
			continue
		}
		pcrPID := state.mapping.PCRPID
		source := a.pids[pcrPID]
		if source == nil || !source.havePCR || source.generation != a.generation ||
			event.ArrivalMS < source.pcrEvent.ArrivalMS || event.ArrivalMS-source.pcrEvent.ArrivalMS > 300 {
			continue
		}
		key := tsClockKey{program: program, pid: media.metrics.PID}
		clock := a.programClocks[key]
		if clock == nil || clock.pcrPID != pcrPID || clock.pcrEpoch != source.clockEpoch || clock.mediaEpoch != media.clockEpoch {
			if clock == nil && len(a.programClocks) >= 128 {
				continue
			}
			if a.programClocks == nil {
				a.programClocks = make(map[tsClockKey]*tsProgramClock)
			}
			clock = &tsProgramClock{pcrPID: pcrPID, pcrEpoch: source.clockEpoch, mediaEpoch: media.clockEpoch, first: event}
			a.programClocks[key] = clock
		}
		offset := unwrapTSClock(pts*300, source.pcr, (1<<33)*300) / 27000
		if clock.count < tsClockMedianSize {
			clock.early[clock.count] = offset
		}
		clock.recent[clock.count%tsClockMedianSize] = offset
		clock.last = event
		clock.count++
	}
}

func (c *tsProgramClock) summary() *TSProgramClockSummary {
	if c.count < 2*tsClockMedianSize || c.last.ArrivalMS-c.first.ArrivalMS < 1500 {
		return nil
	}
	early, recent := c.early, c.recent
	slices.Sort(early[:])
	slices.Sort(recent[:])
	first, last := early[tsClockMedianSize/2], recent[tsClockMedianSize/2]
	return &TSProgramClockSummary{PCRPID: c.pcrPID, Samples: c.count, StartMS: c.first.ArrivalMS, EndMS: c.last.ArrivalMS,
		FirstLeadMS: first, LastLeadMS: last, DriftMS: last - first}
}

func (c *TSProgramClockSummary) String() string {
	return fmt.Sprintf("%d → %d ms（%+d ms）", c.FirstLeadMS, c.LastLeadMS, c.DriftMS)
}
