package diagnostic

import (
	"encoding/binary"
	"fmt"

	"streamanalyzer/internal/hlsdelivery/media"
)

const maxTSSection = 1024

func (a *tsAnalyzer) consumePSI(state *tsPID, payload []byte, start bool, event Event) {
	if !start {
		a.appendPSI(state, payload, false, event)
		return
	}
	pointer := int(payload[0])
	if pointer >= len(payload) {
		a.badPSI(state, "指针超出 MPEG-TS 负载", event)
		state.psi = state.psi[:0]
		return
	}
	if len(state.psi) != 0 {
		a.appendPSI(state, payload[1:1+pointer], false, event)
		if len(state.psi) != 0 {
			a.badPSI(state, "新分段开始前未读到完整 PSI 分段", event)
			state.psi = state.psi[:0]
		}
	}
	a.appendPSI(state, payload[1+pointer:], true, event)
}

func (a *tsAnalyzer) appendPSI(state *tsPID, payload []byte, allowNew bool, event Event) {
	for len(payload) > 0 {
		if len(state.psi) == 0 && (!allowNew || payload[0] == 0xff) {
			return
		}
		if len(state.psi) < 3 {
			size := min(3-len(state.psi), len(payload))
			state.psi = append(state.psi, payload[:size]...)
			payload = payload[size:]
		}
		if len(state.psi) < 3 {
			return
		}
		expectedType := byte(0)
		if state.metrics.PID != 0 {
			expectedType = 2
		}
		length := 3 + (int(state.psi[1]&0x0f) << 8) + int(state.psi[2])
		if state.psi[0] != expectedType || state.psi[1]&0x80 == 0 || length < 12 || length > maxTSSection {
			a.badPSI(state, "类型或长度无效", event)
			state.psi = state.psi[:0]
			return
		}
		size := min(length-len(state.psi), len(payload))
		state.psi = append(state.psi, payload[:size]...)
		payload = payload[size:]
		if len(state.psi) != length {
			return
		}

		if tsPSICRC(state.psi) != 0 {
			a.badPSI(state, "CRC 校验失败", event)
		} else if state.psi[5]&1 != 0 {
			if expectedType == 0 {
				a.observePAT(state.psi, event)
			} else {
				a.observePMT(state, state.psi, event)
			}
		}
		state.psi = state.psi[:0]
		if !allowNew {
			return
		}
	}
}

func (a *tsAnalyzer) badPSI(state *tsPID, detail string, event Event) {
	a.report.MPEGTS.PSIErrors++
	a.note("ts_psi_error", "warn", fmt.Sprintf("PID %d 的 PSI 分段%s", state.metrics.PID, detail), event)
}

var tsPSICRCTable = func() [256]uint32 {
	var table [256]uint32
	for i := range table {
		crc := uint32(i) << 24
		for range 8 {
			if crc&(1<<31) != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
	return table
}()

func tsPSICRC(bytes []byte) uint32 {
	crc := ^uint32(0)
	for _, value := range bytes {
		crc = crc<<8 ^ tsPSICRCTable[byte(crc>>24)^value]
	}
	return crc
}

func (a *tsAnalyzer) observePAT(section []byte, event Event) {
	end := len(section) - 4
	if (end-8)%4 != 0 || section[6] > section[7] {
		a.badPSI(a.pid(0), "节目列表无效", event)
		return
	}
	a.report.MPEGTS.PATSections++
	if a.report.MPEGTS.FirstPATMS == nil {
		at := event.ArrivalMS
		a.report.MPEGTS.FirstPATMS = &at
	}
	id := binary.BigEndian.Uint16(section[3:])
	version := section[5] >> 1 & 31
	if !a.patSeen || id != a.patID {
		clear(a.programs)
		clear(a.programClocks)
		clear(a.pendingPMTPackets)
		a.programSwitchBaseline = nil
	}
	if !a.patSeen || id != a.patID || version != a.patVersion {
		clear(a.pmt)
		a.patID, a.patVersion, a.patSeen = id, version, true
	}
	for i := 8; i+4 <= end; i += 4 {
		program := binary.BigEndian.Uint16(section[i:])
		pid := binary.BigEndian.Uint16(section[i+2:]) & tsNullPID
		if program != 0 && pid > 0 && pid < tsNullPID {
			a.pmt[pid] = program
		}
	}
}

type tsPMTStream struct {
	kind        byte
	pid         uint16
	descriptors []byte
}

func (a *tsAnalyzer) observePMT(state *tsPID, section []byte, event Event) {
	end := len(section) - 4
	if end < 12 || binary.BigEndian.Uint16(section[3:]) != a.pmt[state.metrics.PID] || section[6] > section[7] {
		a.badPSI(state, "节目号或长度无效", event)
		return
	}
	start := 12 + int(binary.BigEndian.Uint16(section[10:])&0x0fff)
	if start > end {
		a.badPSI(state, "节目描述符长度无效", event)
		return
	}
	var streams []tsPMTStream
	for i := start; i < end; {
		if i+5 > end {
			a.badPSI(state, "媒体流描述头不完整", event)
			return
		}
		next := i + 5 + int(binary.BigEndian.Uint16(section[i+3:])&0x0fff)
		if next > end {
			a.badPSI(state, "媒体流描述符长度无效", event)
			return
		}
		pid := binary.BigEndian.Uint16(section[i+1:]) & tsNullPID
		if pid > 0 && pid < tsNullPID {
			streams = append(streams, tsPMTStream{kind: section[i], pid: pid, descriptors: section[i+5 : next]})
		}
		i = next
	}
	a.report.MPEGTS.PMTSections++
	pcrPID := binary.BigEndian.Uint16(section[8:]) & tsNullPID
	a.programMap(state, section, pcrPID, streams, event)
	if pcrPID != tsNullPID {
		a.pcrPIDs[pcrPID] = true
	}
	for _, stream := range streams {
		video, audio := media.TSStreamCodecs(stream.kind, stream.descriptors)
		a.report.Metrics.VideoCodecs = addCodec(a.report.Metrics.VideoCodecs, video)
		a.report.Metrics.AudioCodecs = addCodec(a.report.Metrics.AudioCodecs, audio)
		pid := a.pid(stream.pid)
		pid.mediaKind = ""
		pid.mediaGeneration = a.generation
		metrics := &pid.metrics
		kind := stream.kind
		metrics.StreamType = &kind
		metrics.Codec = video
		if video != "" {
			pid.mediaKind = "video"
		} else if audio != "" {
			metrics.Codec = audio
			pid.mediaKind = "audio"
		}
	}
}
