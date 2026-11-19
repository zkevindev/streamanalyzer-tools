package diagnostic

import (
	"fmt"
	"slices"
	"sort"
)

type TSProgramPID struct {
	PID           uint16 `json:"pid"`
	StreamType    byte   `json:"stream_type"`
	FirstPacketMS *int64 `json:"first_packet_ms,omitempty"`
}

type TSProgramMap struct {
	Version byte           `json:"version"`
	PCRPID  uint16         `json:"pcr_pid"`
	Streams []TSProgramPID `json:"streams"`
}

type TSProgramMapChange struct {
	Program uint16       `json:"program"`
	PMTPID  uint16       `json:"pmt_pid"`
	AtMS    int64        `json:"at_ms"`
	Before  TSProgramMap `json:"before"`
	After   TSProgramMap `json:"after"`
}

func sameProgramStreams(a, b []TSProgramPID) bool {
	return slices.EqualFunc(a, b, func(x, y TSProgramPID) bool {
		return x.PID == y.PID && x.StreamType == y.StreamType
	})
}

func (a *tsAnalyzer) programMap(state *tsPID, section []byte, pcr uint16, streams []tsPMTStream, observed Event) {
	// Track single-section maps. Multi-section PSI still goes through the
	// regular parser, but requires assembly before a reliable map comparison.
	if section[6] != 0 || section[7] != 0 {
		return
	}
	next := TSProgramMap{Version: (section[5] >> 1) & 31, PCRPID: pcr, Streams: make([]TSProgramPID, 0, len(streams))}
	for _, stream := range streams {
		next.Streams = append(next.Streams, TSProgramPID{PID: stream.pid, StreamType: stream.kind})
	}
	sort.Slice(next.Streams, func(i, j int) bool { return next.Streams[i].PID < next.Streams[j].PID })
	if state.lastMap != nil && state.lastMap.Version == next.Version {
		return
	}
	previous := state.lastMap
	state.lastMap = &next
	if previous == nil || previous.PCRPID == next.PCRPID && sameProgramStreams(previous.Streams, next.Streams) {
		return
	}
	if len(a.report.MPEGTS.ProgramMaps) >= 16 {
		return
	}
	change := TSProgramMapChange{Program: a.pmt[state.metrics.PID], PMTPID: state.metrics.PID, AtMS: observed.ArrivalMS,
		Before: TSProgramMap{Version: previous.Version, PCRPID: previous.PCRPID, Streams: slices.Clone(previous.Streams)},
		After:  TSProgramMap{Version: next.Version, PCRPID: next.PCRPID, Streams: slices.Clone(next.Streams)}}
	a.report.MPEGTS.ProgramMaps = append(a.report.MPEGTS.ProgramMaps, change)
	for i := range change.After.Streams {
		pid := &change.After.Streams[i]
		a.pendingPMTPackets[pid.PID] = append(a.pendingPMTPackets[pid.PID], pid)
	}
	a.note("ts_program_map_change", "info", fmt.Sprintf("节目 %d 的 PMT 版本由 %d 切换为 %d，媒体或 PCR PID 已变化",
		change.Program, previous.Version, next.Version), observed)
}

func (a *tsAnalyzer) firstAfterProgramMap(pid uint16, at int64) {
	if pending := a.pendingPMTPackets[pid]; len(pending) != 0 {
		for _, value := range pending {
			value.FirstPacketMS = &at
		}
		delete(a.pendingPMTPackets, pid)
	}
}
