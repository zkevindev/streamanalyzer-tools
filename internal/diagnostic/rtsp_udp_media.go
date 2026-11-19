package diagnostic

import "github.com/pion/rtp"

// Both TCP interleaved payloads and exact UDP datagrams pass through the same
// packet observer. Only the archive location and read-arrival clock differ.
func observeRTSPRTP(metrics *RTSPMetrics, states []rtspTrackState, mediaID int, data []byte, artifact string, offset, at int64, events *[]Event, visit func(Event)) {
	var packet rtp.Packet
	if packet.Unmarshal(data) != nil {
		metrics.MalformedPackets++
		if len(*events) < 2 {
			*events = append(*events, Event{Artifact: artifact, Offset: offset, Size: len(data), ArrivalMS: at, Kind: "rtp_invalid"})
		}
		return
	}
	metrics.RTPPackets++
	metrics.RTPBytes += int64(len(data))
	if metrics.FirstRTPMS == nil {
		first := at
		metrics.FirstRTPMS = &first
	}
	trackIndex := -1
	for index := range metrics.Tracks {
		if metrics.Tracks[index].MediaID == mediaID && metrics.Tracks[index].PayloadType == int(packet.PayloadType) {
			trackIndex = index
			break
		}
	}
	if trackIndex < 0 {
		metrics.UnknownPayloadPackets++
		return
	}
	track, state := &metrics.Tracks[trackIndex], &states[trackIndex]
	seq := packet.SequenceNumber
	event := Event{Artifact: artifact, Offset: offset, Size: len(data), ArrivalMS: at, Kind: "rtp_packet", Codec: track.Codec, Track: uint8(track.ID), RTPSequence: &seq}
	if visit != nil && (track.Kind == "video" || track.Kind == "audio") {
		mediaEvent := event
		mediaEvent.Kind = track.Kind + "_rtp"
		visit(mediaEvent)
	}
	if track.Kind == "video" && metrics.FirstVideoRTPMS == nil {
		first := at
		metrics.FirstVideoRTPMS = &first
	}
	if track.Kind == "audio" && metrics.FirstAudioRTPMS == nil {
		first := at
		metrics.FirstAudioRTPMS = &first
	}
	track.Received, track.RTPPackets = true, track.RTPPackets+1
	track.RTPBytes += int64(len(data))
	if track.FirstRTPMS == nil {
		first := at
		track.FirstRTPMS = &first
	}
	if track.LastRTPMS != nil && at-*track.LastRTPMS > track.MaxArrivalGapMS {
		track.MaxArrivalGapMS = at - *track.LastRTPMS
		state.gapStart, state.gapEnd = *track.LastRTPMS, at
		if state.lastEvent != nil {
			state.gapEvidence = []Event{*state.lastEvent, event}
		}
	}
	last := at
	track.LastRTPMS = &last
	if state.received && state.lastSSRC != packet.SSRC {
		track.SSRCChanges++
	}
	state.lastSSRC, state.received = packet.SSRC, true
	sequence := state.ssrcs[packet.SSRC]
	if sequence == nil {
		if len(state.ssrcs) == maxRTSPSSRCs {
			track.AnalysisLimited = true
		} else {
			sequence = &rtspSequenceState{}
			state.ssrcs[packet.SSRC] = sequence
		}
	}
	if sequence != nil {
		previousGaps := track.SequenceGaps
		sequence.observe(packet.SequenceNumber, packet.Timestamp, track)
		if track.SequenceGaps > previousGaps && len(state.sequenceEvidence) < 4 {
			if state.lastEvent != nil {
				state.sequenceEvidence = append(state.sequenceEvidence, *state.lastEvent)
			}
			state.sequenceEvidence = append(state.sequenceEvidence, event)
		}
	}
	state.lastEvent = &event
}
