package diagnostic

import (
	"fmt"
	"math"
	"reflect"

	"github.com/pion/rtp"
)

const maxRTSPPacketSamples = 200

type RTSPTrackHeaderSummary struct {
	TrackID           int     `json:"track_id"`
	Kind              string  `json:"kind"`
	Codec             string  `json:"codec,omitempty"`
	SSRC              *uint32 `json:"ssrc,omitempty"`
	MarkerPackets     int     `json:"marker_packets,omitempty"`
	FirstSequence     *uint16 `json:"first_sequence,omitempty"`
	LastSequence      *uint16 `json:"last_sequence,omitempty"`
	FirstRTPTimestamp *uint32 `json:"first_rtp_timestamp,omitempty"`
	LastRTPTimestamp  *uint32 `json:"last_rtp_timestamp,omitempty"`
}

type RTSPPacketSample struct {
	Index            int      `json:"index"`
	TrackID          int      `json:"track_id"`
	Kind             string   `json:"kind"`
	Codec            string   `json:"codec,omitempty"`
	SSRC             uint32   `json:"ssrc"`
	PayloadType      uint8    `json:"payload_type"`
	Sequence         uint16   `json:"sequence"`
	SequenceDelta    *int     `json:"sequence_delta,omitempty"`
	Timestamp        uint32   `json:"timestamp"`
	TimestampDelta   *int64   `json:"timestamp_delta,omitempty"`
	TimestampDeltaMS *float64 `json:"timestamp_delta_ms,omitempty"`
	Marker           bool     `json:"marker,omitempty"`
	ArrivalMS        int64    `json:"arrival_ms"`
	PayloadBytes     int      `json:"payload_bytes"`
	PacketBytes      int      `json:"packet_bytes"`
	Anomaly          string   `json:"anomaly,omitempty"`
}

type RTSPPacketReport struct {
	Version   string                   `json:"version"`
	Tracks    []RTSPTrackHeaderSummary `json:"tracks,omitempty"`
	Samples   []RTSPPacketSample       `json:"samples,omitempty"`
	Truncated int                      `json:"truncated,omitempty"`
}

// buildRTSPPacketReport extracts per-track RTP header ranges (SSRC, sequence,
// timestamp, marker count) and a bounded sample of RTP packet headers from the
// archived RTSP stream without modifying legacy RTSPMetrics or Findings schemas.
func buildRTSPPacketReport(dir string, report Report) (*RTSPPacketReport, error) {
	if !isRTSPProtocol(report.Manifest.Protocol) || report.RTSP == nil || !report.RTSP.SDPAvailable || report.RTSP.RTPPackets == 0 {
		return nil, nil
	}

	trackByID := map[int]RTSPTrackMetrics{}
	summaries := make([]RTSPTrackHeaderSummary, 0, len(report.RTSP.Tracks))
	summaryIdx := map[int]int{}
	for _, t := range report.RTSP.Tracks {
		trackByID[t.ID] = t
		summaryIdx[t.ID] = len(summaries)
		summaries = append(summaries, RTSPTrackHeaderSummary{
			TrackID: t.ID,
			Kind:    t.Kind,
			Codec:   t.Codec,
		})
	}

	type trackSeqTracker struct {
		hasSSRC  bool
		lastSSRC uint32
		ssrcs    map[uint32]*rtspSequenceState
		scratch  RTSPTrackMetrics
	}
	trackers := map[int]*trackSeqTracker{}

	out := &RTSPPacketReport{
		Version: "rtsp-packets/1",
		Tracks:  summaries,
	}
	pktIndex := 0

	export := &rtspWireExport{
		packet: func(_ int, data []byte, event Event) error {
			var pkt rtp.Packet
			if pkt.Unmarshal(data) != nil {
				return nil
			}
			pktIndex++
			tid := int(event.Track)
			tm, ok := trackByID[tid]
			if !ok {
				return nil
			}
			sum := &out.Tracks[summaryIdx[tid]]
			ssrc := pkt.SSRC
			seq := pkt.SequenceNumber
			ts := pkt.Timestamp

			sum.SSRC = &ssrc
			if pkt.Marker {
				sum.MarkerPackets++
			}
			if sum.FirstSequence == nil {
				v := seq
				sum.FirstSequence = &v
			}
			lastSeq := seq
			sum.LastSequence = &lastSeq
			if sum.FirstRTPTimestamp == nil {
				v := ts
				sum.FirstRTPTimestamp = &v
			}
			lastTS := ts
			sum.LastRTPTimestamp = &lastTS

			tr := trackers[tid]
			if tr == nil {
				tr = &trackSeqTracker{ssrcs: map[uint32]*rtspSequenceState{}}
				trackers[tid] = tr
			}
			anomaly := ""
			if tr.hasSSRC && tr.lastSSRC != ssrc {
				anomaly = "ssrc_change"
			}
			tr.hasSSRC = true
			tr.lastSSRC = ssrc

			seqState := tr.ssrcs[ssrc]
			if seqState == nil && len(tr.ssrcs) < maxRTSPSSRCs {
				seqState = &rtspSequenceState{}
				tr.ssrcs[ssrc] = seqState
			}

			var seqDelta *int
			var tsDelta *int64
			var tsDeltaMS *float64
			if seqState != nil {
				if seqState.initialized {
					dSeq := int(int16(seq - uint16(seqState.highest)))
					seqDelta = &dSeq
					dTS := int64(int32(ts - seqState.timestamp))
					tsDelta = &dTS
					if tm.ClockRate > 0 {
						ms := math.Round((float64(dTS)*1000.0/float64(tm.ClockRate))*100) / 100
						tsDeltaMS = &ms
					}
				}
				prevGaps := tr.scratch.SequenceGaps
				prevOOO := tr.scratch.OutOfOrderPackets
				prevDup := tr.scratch.DuplicatePackets
				prevReset := tr.scratch.SequenceResets
				prevTSReg := tr.scratch.TimestampRegressions
				seqState.observe(seq, ts, &tr.scratch)
				switch {
				case tr.scratch.SequenceResets > prevReset:
					anomaly = "seq_reset"
				case tr.scratch.SequenceGaps > prevGaps:
					anomaly = "seq_gap"
				case tr.scratch.DuplicatePackets > prevDup:
					anomaly = "duplicate"
				case tr.scratch.OutOfOrderPackets > prevOOO:
					anomaly = "out_of_order"
				case tr.scratch.TimestampRegressions > prevTSReg:
					anomaly = "ts_regression"
				}
			}

			if len(out.Samples) < 120 || (anomaly != "" && len(out.Samples) < maxRTSPPacketSamples) {
				out.Samples = append(out.Samples, RTSPPacketSample{
					Index:            pktIndex,
					TrackID:          tid,
					Kind:             tm.Kind,
					Codec:            tm.Codec,
					SSRC:             ssrc,
					PayloadType:      pkt.PayloadType,
					Sequence:         seq,
					SequenceDelta:    seqDelta,
					Timestamp:        ts,
					TimestampDelta:   tsDelta,
					TimestampDeltaMS: tsDeltaMS,
					Marker:           pkt.Marker,
					ArrivalMS:        event.ArrivalMS,
					PayloadBytes:     len(pkt.Payload),
					PacketBytes:      len(data),
					Anomaly:          anomaly,
				})
			} else {
				out.Truncated++
			}
			return nil
		},
	}

	if _, err := readRTSPWire(dir, report.Manifest, nil, export); err != nil {
		return nil, err
	}
	if len(out.Samples) == 0 {
		return nil, nil
	}
	return out, nil
}

func validateStoredRTSPPackets(dir string, stored, replayed Report) (*RTSPPacketReport, error) {
	if stored.RTSPPackets == nil {
		return nil, nil
	}
	expected, err := buildRTSPPacketReport(dir, replayed)
	if err != nil || expected == nil || !reflect.DeepEqual(stored.RTSPPackets, expected) {
		return nil, fmt.Errorf("%w: RTSP 报文头明细与原始流不一致", ErrInvalid)
	}
	return stored.RTSPPackets, nil
}
