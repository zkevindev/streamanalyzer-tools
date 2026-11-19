package diagnostic

type rtspSequenceState struct {
	initialized bool
	highest     int64
	seen        map[int64]bool
	missing     map[int64]bool
	timestamp   uint32
}

func (state *rtspSequenceState) observe(sequence uint16, timestamp uint32, metrics *RTSPTrackMetrics) {
	if !state.initialized {
		state.initialized, state.highest, state.timestamp = true, int64(sequence), timestamp
		state.seen, state.missing = map[int64]bool{int64(sequence): true}, map[int64]bool{}
		return
	}
	delta := int64(int16(sequence - uint16(state.highest)))
	extended := state.highest + delta
	if delta > 3000 || delta < -3000 {
		metrics.SequenceResets++
		state.initialized = false
		state.observe(sequence, timestamp, metrics)
		return
	}
	if state.seen[extended] {
		metrics.DuplicatePackets++
		return
	}
	if extended < state.highest-rtspSequenceWindow {
		metrics.OutOfOrderPackets++
		return
	}
	state.seen[extended] = true
	if delta <= 0 {
		metrics.OutOfOrderPackets++
		if state.missing[extended] {
			delete(state.missing, extended)
			metrics.MissingSequencePackets--
		}
		return
	}
	if delta > 1 {
		metrics.SequenceGaps++
		metrics.MissingSequencePackets += int(delta - 1)
		for missing := max(state.highest+1, extended-rtspSequenceWindow); missing < extended; missing++ {
			state.missing[missing] = true
		}
	}
	if int32(timestamp-state.timestamp) < 0 {
		metrics.TimestampRegressions++
	}
	state.timestamp, state.highest = timestamp, extended
	for value := range state.seen {
		if value < extended-rtspSequenceWindow {
			delete(state.seen, value)
		}
	}
	for value := range state.missing {
		if value < extended-rtspSequenceWindow {
			delete(state.missing, value)
		}
	}
}
