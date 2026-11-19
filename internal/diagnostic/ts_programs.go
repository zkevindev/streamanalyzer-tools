package diagnostic

import (
	"cmp"
	"slices"
)

type tsProgramState struct {
	pmt     uint16
	mapping TSProgramMap
}

type TSProgramSummary struct {
	Number  uint16                   `json:"number"`
	PMTPID  uint16                   `json:"pmt_pid"`
	Version byte                     `json:"version"`
	Streams []TSProgramStreamSummary `json:"streams"`
}

type TSProgramStreamSummary struct {
	PID                uint16 `json:"pid"`
	Packets            int    `json:"packets"`
	ContinuityErrors   int    `json:"continuity_errors"`
	AfterSwitchPackets *int   `json:"after_switch_packets,omitempty"`
	AfterSwitchErrors  *int   `json:"after_switch_errors,omitempty"`
}

func (a *tsAnalyzer) recordProgramBaselines() {
	a.programSwitchBaseline = make(map[uint16]MPEGTSPIDMetrics, len(a.pids))
	for id, state := range a.pids {
		a.programSwitchBaseline[id] = state.metrics
	}
}

func (a *tsAnalyzer) summarizeTSPrograms() {
	metrics := a.report.MPEGTS
	for number, state := range a.programs {
		if a.pmt[state.pmt] != number {
			continue
		}
		program := TSProgramSummary{Number: number, PMTPID: state.pmt, Version: state.mapping.Version,
			Streams: make([]TSProgramStreamSummary, 0, len(state.mapping.Streams))}
		for _, stream := range state.mapping.Streams {
			current := a.pids[stream.PID]
			entry := TSProgramStreamSummary{PID: stream.PID}
			if current != nil {
				entry.Packets, entry.ContinuityErrors = current.metrics.Packets, current.metrics.ContinuityErrors
			}
			if a.programSwitchBaseline != nil {
				baseline := a.programSwitchBaseline[stream.PID]
				packets, errors := entry.Packets-baseline.Packets, entry.ContinuityErrors-baseline.ContinuityErrors
				entry.AfterSwitchPackets, entry.AfterSwitchErrors = &packets, &errors
			}
			program.Streams = append(program.Streams, entry)
		}
		metrics.Programs = append(metrics.Programs, program)
	}
	slices.SortFunc(metrics.Programs, func(a, b TSProgramSummary) int { return cmp.Compare(a.Number, b.Number) })
}
