package diagnostic

import (
	"fmt"
	"slices"
)

type tsLayout struct {
	stride int
	prefix int
}

var tsLayouts = [...]tsLayout{
	{stride: tsPacketBytes},
	{stride: 192, prefix: 4}, // M2TS arrival timestamp prefix
	{stride: 204},            // Reed-Solomon trailer
}

type tsChunkMark struct {
	end       int64
	arrivalMS int64
}

type tsFramer struct {
	analyzer *tsAnalyzer
	layout   tsLayout
	pending  []byte
	offset   int64
	marks    []tsChunkMark
}

func (f *tsFramer) feed(span tsSpan, data []byte) {
	f.pending = append(f.pending, data...)
	f.marks = append(f.marks, tsChunkMark{end: span.Offset + int64(len(data)), arrivalMS: span.ArrivalMS})
	consumed := f.decode(f.pending, f.offset, f.arrivalAt, nil, true, false, false)
	f.offset += int64(consumed)
	f.pending = f.pending[:copy(f.pending, f.pending[consumed:])]
	discard := 0
	for discard < len(f.marks) && f.marks[discard].end <= f.offset {
		discard++
	}
	f.marks = slices.Delete(f.marks, 0, discard)
}

func (f *tsFramer) arrivalAt(end int64) int64 {
	for _, mark := range f.marks {
		if end <= mark.end {
			return mark.arrivalMS
		}
	}
	return 0
}

func (f *tsFramer) finishHTTP() {
	if len(f.pending) > 0 {
		f.decode(f.pending, f.offset, f.arrivalAt, nil, true, true, false)
	}
}

func (f *tsFramer) datagram(span tsSpan, data []byte, prefix int, sequence *uint16, ordered bool) {
	f.decode(data, span.Offset+int64(prefix), func(int64) int64 { return span.ArrivalMS }, sequence, ordered, true, true)
}

func (f *tsFramer) decode(data []byte, offset int64, arrivalAt func(int64) int64, sequence *uint16, ordered, final, datagram bool) int {
	layouts := tsLayouts[:]
	if sequence != nil {
		layouts = layouts[:1]
		f.layout = tsLayouts[0]
	}
	consumed := 0
	for len(data) > 0 {
		layout := f.layout
		if layout.stride != 0 && len(data) < layout.stride && !final {
			break
		}
		if layout.stride == 0 || len(data) < layout.stride || data[layout.prefix] != 0x47 {
			index, found := findTSLayout(data, layouts, final)
			if index < 0 {
				if final {
					event := Event{Offset: offset, Size: len(data), ArrivalMS: arrivalAt(offset + int64(len(data))), RTPSequence: sequence}
					f.analyzer.unframed(event, datagram, plausiblePartialTS(data, layouts))
					consumed += len(data)
					break
				}
				// Keep any candidate that would require more bytes to confirm.
				const lookahead = 2*204 + 4 + 4
				discard := max(0, len(data)-lookahead)
				if discard > 0 {
					event := Event{Offset: offset, Size: discard, ArrivalMS: arrivalAt(offset + int64(discard))}
					f.analyzer.unframed(event, datagram, false)
					consumed += discard
				}
				break
			}
			if index > 0 {
				event := Event{Offset: offset, Size: index, ArrivalMS: arrivalAt(offset + int64(index)), RTPSequence: sequence}
				f.analyzer.unframed(event, datagram, false)
				consumed += index
				offset += int64(index)
				data = data[index:]
			}
			layout = found
			f.layout = layout
		}
		metrics := f.analyzer.report.MPEGTS
		event := Event{Offset: offset, Size: layout.stride, ArrivalMS: arrivalAt(offset + int64(layout.stride)), RTPSequence: sequence}
		switch {
		case metrics.PacketSize == 0:
			metrics.PacketSize = layout.stride
		case metrics.PacketSize != layout.stride:
			f.analyzer.note("ts_packet_size_changed", "warn", "采样中检测到不同长度的 MPEG-TS 包", event)
		}
		packet := data[layout.prefix : layout.prefix+tsPacketBytes]
		f.analyzer.inspectPacket(packet, event, ordered)
		consumed += layout.stride
		offset += int64(layout.stride)
		data = data[layout.stride:]
	}
	return consumed
}

func findTSLayout(data []byte, layouts []tsLayout, final bool) (int, tsLayout) {
	// Keep earlier valid packets even if a later run provides more syncs.
	// Prefer a stronger match only when competing layouts start together.
	for i := 0; i+tsPacketBytes <= len(data); i++ {
		if data[i] != 0x47 && data[i+4] != 0x47 {
			continue
		}
		for confidence := 3; confidence >= 2; confidence-- {
			for _, layout := range layouts {
				if i+layout.stride > len(data) || !validTSHeader(data, i+layout.prefix+(confidence-1)*layout.stride) {
					continue
				}
				matches := true
				for j := 0; j < confidence-1; j++ {
					if !validTSHeader(data, i+layout.prefix+j*layout.stride) {
						matches = false
						break
					}
				}
				if matches {
					return i, layout
				}
			}
		}
	}
	if final {
		// A complete singleton at the end is also useful. Prefer an exact
		// packet length so a one-packet M2TS/204 datagram keeps its envelope.
		for i := max(0, len(data)-2*204+1); i+tsPacketBytes <= len(data); i++ {
			var partial tsLayout
			for _, layout := range layouts {
				remainder := len(data) - i
				if remainder < layout.stride || remainder >= 2*layout.stride || !validTSHeader(data, i+layout.prefix) {
					continue
				}
				if remainder == layout.stride {
					return i, layout
				}
				if partial.stride == 0 {
					partial = layout
				}
			}
			if partial.stride != 0 {
				return i, partial
			}
		}
	}
	return -1, tsLayout{}
}

func validTSHeader(data []byte, offset int) bool {
	return offset+4 <= len(data) && data[offset] == 0x47 && data[offset+3]&0x30 != 0
}

func plausiblePartialTS(data []byte, layouts []tsLayout) bool {
	for _, layout := range layouts {
		if len(data) > layout.prefix && len(data) < layout.stride && data[layout.prefix] == 0x47 {
			return true
		}
	}
	return false
}

func (a *tsAnalyzer) unframed(event Event, datagram, partial bool) {
	event.Kind = "ts_sync_loss"
	metrics := a.report.MPEGTS
	if partial {
		metrics.TrailingBytes += int64(event.Size)
		reason := a.report.Manifest.Observation.EndReason
		if datagram || reason == "eof" || reason == "body_error" {
			a.note("ts_incomplete_packet", "warn", "采样数据末尾包含不完整的 MPEG-TS 包", event)
		}
		return
	}
	metrics.SyncLossBytes += int64(event.Size)
	a.note("ts_sync_loss", "error", fmt.Sprintf("同步字无法对齐，跳过 %d 字节", event.Size), event)
}
