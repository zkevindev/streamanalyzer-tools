package sipphone

import (
	"math"
	"sync"
	"time"

	"github.com/pion/rtp"
)

// RTPStats describes the packets that passed this endpoint's media path.
type RTPStats struct {
	Packets      uint64    `json:"packets"`
	FirstAt      time.Time `json:"first_at,omitzero"`
	LastAt       time.Time `json:"last_at,omitzero"`
	SequenceGaps uint64    `json:"sequence_gaps"`
	JitterMS     float64   `json:"jitter_ms"`
	FaultDrops   uint64    `json:"fault_drops"`
}

type rtpStats struct {
	mu        sync.Mutex
	stats     RTPStats
	ssrc      uint32
	timestamp uint32
	top       uint16
	window    uint64
	span      int
}

func (s *rtpStats) observe(header rtp.Header, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stats.Packets == 0 {
		s.stats.FirstAt = now
	}
	if s.stats.Packets == 0 || s.ssrc != header.SSRC {
		s.ssrc, s.top, s.window, s.span = header.SSRC, header.SequenceNumber, 1, 1
		s.stats.JitterMS = 0
	} else {
		delta := int(int16(header.SequenceNumber - s.top))
		if delta > 0 {
			s.stats.SequenceGaps += uint64(delta - 1)
			s.window = s.window<<delta | 1
			s.span = min(64, s.span+delta)
			s.top = header.SequenceNumber
		} else if back := -delta; back < s.span {
			mask := uint64(1) << back
			if s.window&mask == 0 {
				s.window |= mask
				s.stats.SequenceGaps--
			}
		}
		arrival := float64(now.Sub(s.stats.LastAt)) / float64(time.Millisecond)
		clock := float64(int32(header.Timestamp-s.timestamp)) / (sampleRate / 1000.0)
		s.stats.JitterMS += (math.Abs(arrival-clock) - s.stats.JitterMS) / 16
	}
	s.stats.LastAt = now
	s.timestamp = header.Timestamp
	s.stats.Packets++
}

func (s *rtpStats) drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.FaultDrops++
}

func (s *rtpStats) snapshot() RTPStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

type audioFault uint32

const (
	audioFaultOff audioFault = iota
	audioFaultSend
	audioFaultReceive
)

func (f audioFault) String() string {
	switch f {
	case audioFaultSend:
		return "send"
	case audioFaultReceive:
		return "receive"
	default:
		return "off"
	}
}
