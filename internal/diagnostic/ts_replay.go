package diagnostic

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
)

const (
	tsPacketBytes = 188
	tsNullPID     = 0x1fff
	tsReportPIDs  = 64
)

type MPEGTSMetrics struct {
	Packets          int                `json:"packets"`
	PacketSize       int                `json:"packet_size,omitempty"`
	TransportErrors  int                `json:"transport_errors"`
	SyncLossBytes    int64              `json:"sync_loss_bytes"`
	TrailingBytes    int64              `json:"trailing_bytes,omitempty"`
	ContinuityErrors int                `json:"continuity_errors"`
	Duplicates       int                `json:"duplicates"`
	MalformedPackets int                `json:"malformed_packets"`
	PATSections      int                `json:"pat_sections"`
	PMTSections      int                `json:"pmt_sections"`
	PCRPIDs          int                `json:"pcr_pids"`
	MaxPCRGapMS      int64              `json:"max_pcr_gap_ms,omitempty"`
	PSIErrors        int                `json:"psi_errors,omitempty"`
	PIDCount         int                `json:"pid_count"`
	RTPDatagrams     int                `json:"rtp_datagrams"`
	RTPMissing       int                `json:"rtp_missing"`
	RTPOutOfOrder    int                `json:"rtp_out_of_order"`
	RTPDuplicates    int                `json:"rtp_duplicates"`
	RTPInvalid       int                `json:"rtp_invalid"`
	PIDs             []MPEGTSPIDMetrics `json:"pids"`
}

type MPEGTSPIDMetrics struct {
	PID              uint16 `json:"pid"`
	Packets          int    `json:"packets"`
	TransportErrors  int    `json:"transport_errors"`
	ContinuityErrors int    `json:"continuity_errors"`
	Duplicates       int    `json:"duplicates"`
	PESStarts        int    `json:"pes_starts"`
	PCRCount         int    `json:"pcr_count,omitempty"`
	PTSCount         int    `json:"pts_count,omitempty"`
	DTSCount         int    `json:"dts_count,omitempty"`
	StreamType       *uint8 `json:"stream_type,omitempty"`
	Codec            string `json:"codec,omitempty"`
}

type tsAnalyzer struct {
	onMedia    func(Event)
	report     Report
	seen       map[string]int
	pids       map[uint16]*tsPID
	pmt        map[uint16]uint16
	patID      uint16
	patVersion byte
	patSeen    bool
	pcrPIDs    map[uint16]bool
	rtp        map[uint32]*tsRTPSource
	lastSSRC   uint32
	hasSSRC    bool
	rtpGap     []Event
	generation uint64
}

type tsPID struct {
	mediaKind       string
	mediaGeneration uint64
	generation      uint64
	metrics         MPEGTSPIDMetrics
	last            Event
	prev            [tsPacketBytes]byte
	havePrev        bool
	prevPayload     bool
	prevPCR         bool
	duplicateUsed   bool
	cc              byte
	psi             []byte
	pcr             uint64
	havePCR         bool
	pcrEvent        Event
	dts             uint64
	haveDTS         bool
	dtsEvent        Event
}

func newTSAnalyzer(manifest Manifest) *tsAnalyzer {
	return &tsAnalyzer{
		report: Report{
			RuleVersion: manifest.Protocol + "/2", Manifest: manifest,
			MPEGTS: &MPEGTSMetrics{PIDs: []MPEGTSPIDMetrics{}}, Findings: []Finding{},
			Metrics: Metrics{MediaTimingUnit: "pes"},
		},
		seen: make(map[string]int), pids: make(map[uint16]*tsPID), pmt: make(map[uint16]uint16),
		pcrPIDs: make(map[uint16]bool), rtp: make(map[uint32]*tsRTPSource),
	}
}

func replayTS(dir string, manifest Manifest) (Report, error) {
	analyzer := newTSAnalyzer(manifest)
	framer := tsFramer{analyzer: analyzer}
	err := walkTSChunks(dir, manifest, func(span tsSpan, data []byte) error {
		switch manifest.Protocol {
		case ProtocolHTTPTS:
			framer.feed(span, data)
		case ProtocolUDPTS:
			framer.datagram(span, data, 0, nil, true)
		case ProtocolRTPTS:
			analyzer.readRTP(&framer, span, data)
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	framer.finishHTTP()
	return analyzer.finish(), nil
}

func (a *tsAnalyzer) add(code, severity, message string, start, end int64, evidence ...Event) {
	if index, exists := a.seen[code]; exists {
		finding := &a.report.Findings[index]
		finding.Occurrences++
		finding.EndMS = max(finding.EndMS, end)
		return
	}
	a.seen[code] = len(a.report.Findings)
	a.report.Findings = append(a.report.Findings, Finding{
		Code: code, Severity: severity, Message: message, StartMS: start, EndMS: end, Evidence: evidence, Occurrences: 1,
	})
}

func (a *tsAnalyzer) note(code, severity, message string, evidence ...Event) {
	a.add(code, severity, message, evidence[0].ArrivalMS, evidence[len(evidence)-1].ArrivalMS, evidence...)
}

func (a *tsAnalyzer) pid(id uint16) *tsPID {
	state := a.pids[id]
	if state == nil {
		state = &tsPID{metrics: MPEGTSPIDMetrics{PID: id}}
		a.pids[id] = state
	}
	return state
}

func (s *tsPID) resetTimeline() {
	s.psi = s.psi[:0]
	s.havePCR, s.haveDTS = false, false
}

type tsPacket struct {
	payload       []byte
	discontinuity bool
	hasPCR        bool
	pcr           uint64
}

func decodeTSPacket(p []byte) (tsPacket, bool) {
	var out tsPacket
	switch p[3] >> 4 & 3 {
	case 0:
		return out, false
	case 1:
		out.payload = p[4:]
	case 2, 3:
		length := int(p[4])
		adaptationOnly := p[3]&0x10 == 0
		if length > 183 || (adaptationOnly && length != 183) || (!adaptationOnly && length == 183) {
			return out, false
		}
		if length > 0 {
			out.discontinuity = p[5]&0x80 != 0
			if p[5]&0x10 != 0 {
				if length < 7 || p[10]&0x7e != 0x7e {
					return out, false
				}
				extension := uint64(p[10]&1)<<8 | uint64(p[11])
				if extension >= 300 {
					return out, false
				}
				base := uint64(p[6])<<25 | uint64(p[7])<<17 | uint64(p[8])<<9 | uint64(p[9])<<1 | uint64(p[10]>>7)
				out.pcr, out.hasPCR = base*300+extension, true
			}
		}
		if !adaptationOnly {
			out.payload = p[5+length:]
		}
	}
	return out, true
}

func (a *tsAnalyzer) inspectPacket(p []byte, event Event, ordered bool) {
	metrics := a.report.MPEGTS
	metrics.Packets++
	id := uint16(p[1]&0x1f)<<8 | uint16(p[2])
	event.Kind, event.PID = "ts_packet", &id
	state := a.pid(id)
	state.metrics.Packets++
	if state.generation != a.generation && ordered {
		state.havePrev = false
		state.resetTimeline()
		state.generation = a.generation
	}
	if p[1]&0x80 != 0 {
		metrics.TransportErrors++
		state.metrics.TransportErrors++
		a.note("ts_transport_error", "error", fmt.Sprintf("PID %d 检测到传输错误指示（TEI）", id), event)
		if ordered {
			state.havePrev = false
			state.resetTimeline()
		}
		return
	}
	packet, ok := decodeTSPacket(p)
	if !ok {
		metrics.MalformedPackets++
		a.note("ts_malformed_packet", "warn", fmt.Sprintf("PID %d 包头或适配字段无效", id), event)
		if ordered {
			state.havePrev = false
			state.resetTimeline()
		}
		return
	}
	if id == tsNullPID || !ordered {
		return
	}

	cc := p[3] & 15
	payload := len(packet.payload) != 0
	identical := state.havePrev && state.prevPayload && payload && cc == state.cc &&
		sameTSPacket(state.prev[:], p, state.prevPCR && packet.hasPCR)
	if identical && !state.duplicateUsed {
		state.duplicateUsed = true
		state.metrics.Duplicates++
		metrics.Duplicates++
		return
	}

	if packet.discontinuity && !identical {
		state.resetTimeline()
	} else if state.havePrev {
		expected := state.cc
		if payload {
			expected = (expected + 1) & 15
		}
		if cc != expected {
			state.metrics.ContinuityErrors++
			metrics.ContinuityErrors++
			a.note("ts_continuity_error", "error", fmt.Sprintf("PID %d 连续计数预期 %d，实际 %d", id, expected, cc), state.last, event)
			state.resetTimeline()
		}
	}

	state.cc, state.last, state.havePrev = cc, event, true
	state.prevPayload, state.prevPCR, state.duplicateUsed = payload, packet.hasPCR, identical
	copy(state.prev[:], p)
	if identical {
		return
	}
	if packet.hasPCR {
		a.observePCR(state, packet.pcr, event)
	}
	if !payload || p[3]&0xc0 != 0 {
		return
	}
	start := p[1]&0x40 != 0
	if _, isPMT := a.pmt[id]; id == 0 || isPMT {
		a.consumePSI(state, packet.payload, start, event)
		return
	}
	if start {
		a.observePES(state, packet.payload, event)
	}
}

func sameTSPacket(before, after []byte, pcrInBoth bool) bool {
	if pcrInBoth {
		return bytes.Equal(before[:6], after[:6]) && bytes.Equal(before[12:], after[12:])
	}
	return bytes.Equal(before, after)
}

func unwrapTSClock(current, previous, modulo uint64) int64 {
	distance := int64(current) - int64(previous)
	if distance < -int64(modulo/2) {
		return distance + int64(modulo)
	}
	if distance > int64(modulo/2) {
		return distance - int64(modulo)
	}
	return distance
}

func (a *tsAnalyzer) observePCR(s *tsPID, pcr uint64, event Event) {
	a.pcrPIDs[s.metrics.PID] = true
	s.metrics.PCRCount++
	if s.havePCR {
		delta := unwrapTSClock(pcr, s.pcr, (1<<33)*300)
		gap := delta / 27_000
		a.report.MPEGTS.MaxPCRGapMS = max(a.report.MPEGTS.MaxPCRGapMS, gap)
		if delta < 0 {
			a.note("ts_pcr_backwards", "warn", fmt.Sprintf("PID %d PCR 出现回退", s.metrics.PID), s.pcrEvent, event)
		} else if gap > 100 {
			a.note("ts_pcr_gap", "warn", fmt.Sprintf("PID %d PCR 间隔 %d ms，超过 100 ms", s.metrics.PID, gap), s.pcrEvent, event)
		}
	}
	s.pcr, s.pcrEvent, s.havePCR = pcr, event, true
}

func (a *tsAnalyzer) observePES(s *tsPID, payload []byte, event Event) {
	if len(payload) < 6 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
		return
	}
	s.metrics.PESStarts++
	if s.mediaGeneration == a.generation {
		markMediaArrival(&a.report.Metrics, s.mediaKind, event.ArrivalMS)
		if a.onMedia != nil && (s.mediaKind == "audio" || s.mediaKind == "video") {
			event.Kind, event.Codec = s.mediaKind, s.metrics.Codec
			defer func() { a.onMedia(event) }()
		}
	}
	if len(payload) < 9 || payload[6]&0xc0 != 0x80 {
		return
	}
	flags := payload[7] >> 6
	if flags != 2 && flags != 3 || payload[8] < 5 || len(payload) < 14 {
		return
	}
	if pts, ok := decodeTSPESClock(payload[9:14], flags); ok {
		s.metrics.PTSCount++
		event.PTS, event.DTS = int64(pts/90), int64(pts/90)
	}
	if flags != 3 || payload[8] < 10 || len(payload) < 19 {
		return
	}
	dts, ok := decodeTSPESClock(payload[14:19], 1)
	if !ok {
		return
	}
	s.metrics.DTSCount++
	event.DTS = int64(dts / 90)
	if s.haveDTS && unwrapTSClock(dts, s.dts, 1<<33) < 0 {
		a.note("ts_dts_backwards", "warn", fmt.Sprintf("PID %d PES DTS 出现回退", s.metrics.PID), s.dtsEvent, event)
	}
	s.dts, s.dtsEvent, s.haveDTS = dts, event, true
}

func decodeTSPESClock(b []byte, prefix byte) (uint64, bool) {
	if b[0]>>4 != prefix || b[0]&1 == 0 || b[2]&1 == 0 || b[4]&1 == 0 {
		return 0, false
	}
	value := uint64(b[0]&0x0e)<<29 | uint64(b[1])<<22 | uint64(b[2]&0xfe)<<14 | uint64(b[3])<<7 | uint64(b[4]>>1)
	return value, true
}

func (a *tsAnalyzer) finish() Report {
	obs := a.report.Manifest.Observation
	m := a.report.MPEGTS
	m.PCRPIDs = len(a.pcrPIDs)
	if m.Packets == 0 {
		a.add("ts_no_media", "warn", "采样窗口内未识别到完整 MPEG-TS 包", 0, obs.ElapsedMS)
	} else {
		if m.PATSections == 0 {
			a.add("ts_pat_missing", "warn", "采样窗口内未识别到有效 PAT", 0, obs.ElapsedMS)
		}
		if m.PMTSections == 0 {
			a.add("ts_pmt_missing", "warn", "采样窗口内未识别到有效 PMT", 0, obs.ElapsedMS)
		}
	}
	if m.RTPMissing > 0 && len(a.rtpGap) > 0 {
		a.note("rtp_sequence_gap", "warn", fmt.Sprintf("按 RTP 序列估算有 %d 个数据报未在采样窗口内收到", m.RTPMissing), a.rtpGap...)
	}
	switch obs.EndReason {
	case "http_status":
		a.add("http_status", "error", fmt.Sprintf("播放请求收到 HTTP %d", obs.HTTPStatus), 0, obs.ElapsedMS)
	case "request_error", "body_error", "udp_bind_error", "udp_read_error":
		a.add(obs.EndReason, "error", endReasonText(obs.EndReason), 0, obs.ElapsedMS)
	case "size_limit", "tag_limit", "timeline_limit", "canceled":
		a.add(obs.EndReason, "info", endReasonText(obs.EndReason), obs.ElapsedMS, obs.ElapsedMS)
	}

	m.PIDCount = len(a.pids)
	for _, state := range a.pids {
		m.PIDs = append(m.PIDs, state.metrics)
	}
	// Keep the table bounded even for streams with many conflicting PIDs.
	slices.SortFunc(m.PIDs, func(left, right MPEGTSPIDMetrics) int {
		priority := func(p MPEGTSPIDMetrics) int {
			_, isPMT := a.pmt[p.PID]
			switch {
			case p.PID == 0 || isPMT || p.StreamType != nil:
				return 0
			case p.TransportErrors+p.ContinuityErrors > 0:
				return 1
			default:
				return 2
			}
		}
		if rank := cmp.Compare(priority(left), priority(right)); rank != 0 {
			return rank
		}
		if count := cmp.Compare(right.Packets, left.Packets); count != 0 {
			return count
		}
		return cmp.Compare(left.PID, right.PID)
	})
	m.PIDs = m.PIDs[:min(tsReportPIDs, len(m.PIDs))]
	slices.SortFunc(m.PIDs, func(left, right MPEGTSPIDMetrics) int { return cmp.Compare(left.PID, right.PID) })
	slices.Sort(a.report.Metrics.VideoCodecs)
	slices.Sort(a.report.Metrics.AudioCodecs)
	addObservationFindings(&a.report)
	slices.SortStableFunc(a.report.Findings, func(left, right Finding) int { return cmp.Compare(left.StartMS, right.StartMS) })
	for i := range a.report.Findings {
		a.report.Findings[i].ID = fmt.Sprintf("F%03d", i+1)
	}
	enrichFindings(&a.report)
	return a.report
}
