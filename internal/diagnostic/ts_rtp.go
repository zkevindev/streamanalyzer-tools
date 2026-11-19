package diagnostic

import (
	"encoding/binary"
	"fmt"
)

const (
	rtpHistory       = 128
	rtpSourceLimit   = 32
	rtpMaxForwardGap = 3_000
)

type tsRTPSource struct {
	high  int64
	first int64
	seen  [rtpHistory]int64 // stores extended sequence + 1 + rtpHistory
	last  Event
}

// Late arrivals may precede the first observed wrap by up to one history
// window, so the offset keeps both the index and the stored value positive.
func rtpSlot(sequence int64) int { return int((sequence + rtpHistory) % rtpHistory) }

func rtpTSHeader(data []byte) (header int, end int, sequence uint16, ssrc uint32, result string) {
	if len(data) < 12 || data[0]>>6 != 2 {
		return 0, 0, 0, 0, "rtp_invalid_header"
	}
	header = 12 + 4*int(data[0]&15)
	if header > len(data) {
		return 0, 0, 0, 0, "rtp_invalid_header"
	}
	if data[0]&0x10 != 0 {
		if len(data)-header < 4 {
			return 0, 0, 0, 0, "rtp_invalid_header"
		}
		header += 4 + 4*int(binary.BigEndian.Uint16(data[header+2:]))
	}
	end = len(data)
	if data[0]&0x20 != 0 {
		padding := int(data[end-1])
		if padding == 0 {
			return 0, 0, 0, 0, "rtp_invalid_header"
		}
		end -= padding
	}
	if header >= end {
		return 0, 0, 0, 0, "rtp_invalid_header"
	}
	if data[1]&127 != 33 {
		return 0, 0, 0, 0, "rtp_payload_type"
	}
	return header, end, binary.BigEndian.Uint16(data[2:]), binary.BigEndian.Uint32(data[8:]), ""
}

func (a *tsAnalyzer) readRTP(f *tsFramer, span tsSpan, data []byte) {
	m := a.report.MPEGTS
	m.RTPDatagrams++
	event := Event{Offset: span.Offset, Size: span.Size, ArrivalMS: span.ArrivalMS, Kind: "rtp_datagram"}
	header, end, sequence, ssrc, result := rtpTSHeader(data)
	switch result {
	case "rtp_invalid_header":
		m.RTPInvalid++
		a.note(result, "error", "数据报的 RTP 头部、扩展或填充不完整", event)
		return
	case "rtp_payload_type":
		m.RTPInvalid++
		a.note(result, "error", "RTP 负载类型不是 MPEG-TS 静态类型 33", event)
		return
	}
	event.RTPSequence = &sequence
	if (end-header)%tsPacketBytes != 0 {
		m.RTPInvalid++
		a.note("rtp_payload_size", "warn", "RTP MPEG-TS 负载长度不是 188 字节的整数倍", event)
	}
	state := a.rtp[ssrc]
	if state == nil {
		if len(a.rtp) == rtpSourceLimit {
			a.note("rtp_multiple_sources_limit", "warn", "RTP 源数量超过可跟踪上限，超出部分保留原始数据", event)
			return
		}
		state = &tsRTPSource{first: int64(sequence), high: int64(sequence), last: event}
		state.seen[rtpSlot(int64(sequence))] = int64(sequence) + 1 + rtpHistory
		a.rtp[ssrc] = state
		a.switchRTPSender(ssrc, event)
		f.datagram(span, data[header:end], header, &sequence, true)
		return
	}

	distance := int64(int16(sequence - uint16(state.high)))
	extended := state.high + distance
	ordered := distance > 0
	switch {
	case distance == 0 || (distance < 0 && distance > -rtpHistory && state.seen[rtpSlot(extended)] == extended+1+rtpHistory):
		m.RTPDuplicates++
		a.note("rtp_duplicate", "info", fmt.Sprintf("RTP 序列 %d 重复到达", sequence), event)
		return
	case distance < 0:
		m.RTPOutOfOrder++
		if distance > -rtpHistory {
			if extended >= state.first {
				m.RTPMissing = max(0, m.RTPMissing-1)
			}
			state.seen[rtpSlot(extended)] = extended + 1 + rtpHistory
		}
		a.note("rtp_out_of_order", "warn", fmt.Sprintf("RTP 序列 %d 晚于后续数据报到达", sequence), event)
	case distance >= rtpMaxForwardGap:
		a.note("rtp_sequence_jump", "warn", fmt.Sprintf("RTP 序列跳变 %d，已重置该源的顺序跟踪", distance), state.last, event)
		*state = tsRTPSource{first: extended, high: extended, last: event}
		state.seen[rtpSlot(extended)] = extended + 1 + rtpHistory
		a.resetTSContext()
	default:
		if distance > 1 {
			m.RTPMissing += int(distance - 1)
			a.rtpGap = []Event{state.last, event}
		}
		state.high, state.last = extended, event
		state.seen[rtpSlot(extended)] = extended + 1 + rtpHistory
	}
	if ordered {
		a.switchRTPSender(ssrc, event)
	}
	f.datagram(span, data[header:end], header, &sequence, ordered)
}

func (a *tsAnalyzer) switchRTPSender(ssrc uint32, event Event) {
	if a.hasSSRC && a.lastSSRC != ssrc {
		a.note("rtp_source_changed", "info", "采样中收到多个 RTP 源，按源变化重置 MPEG-TS 连续性基线", event)
		a.resetTSContext()
	}
	a.lastSSRC, a.hasSSRC = ssrc, true
}

func (a *tsAnalyzer) resetTSContext() {
	a.generation++
	clear(a.pmt)
	clear(a.programClocks)
	a.patSeen = false
}
