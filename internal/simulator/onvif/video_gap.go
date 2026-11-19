package onvif

import (
	"context"
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"github.com/zaf/g711"
)

const videoGapDelay = 2 * time.Second

type RTPGapStatus struct {
	Profile string    `json:"profile"`
	Begin   time.Time `json:"begin,omitzero"`
	End     time.Time `json:"end,omitzero"`
}

type videoGap struct {
	duration time.Duration
	armed    atomic.Int64
	begin    atomic.Int64
	end      atomic.Int64
}

func (g *videoGap) skip(now time.Time) bool {
	at := g.armed.Load()
	if at == 0 || now.UnixNano() < at || now.UnixNano() >= at+int64(g.duration) {
		return false
	}
	g.begin.CompareAndSwap(0, now.UnixNano())
	return true
}

func (g *videoGap) snapshot(profile string) RTPGapStatus {
	result := RTPGapStatus{Profile: profile}
	if at := g.begin.Load(); at != 0 {
		result.Begin = time.Unix(0, at)
	}
	if at := g.end.Load(); at != 0 {
		result.End = time.Unix(0, at)
	}
	return result
}

func (v *videoTrack) playAudio(ctx context.Context) {
	const frame = 160
	var tone [5 * frame]byte
	for i := range tone {
		tone[i] = g711.EncodeAlawFrame(int16(5000 * math.Sin(2*math.Pi*440*float64(i)/8000)))
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var clock senderClock
	packet := rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 8, SSRC: rand.Uint32(), SequenceNumber: uint16(rand.Uint32()), Timestamp: rand.Uint32()}}
	for i := 0; ; i = (i + 1) % 5 {
		packet.Payload = tone[i*frame : (i+1)*frame]
		// WritePacketRTP retains the serialized bytes, not this packet.
		if err := v.stream.WritePacketRTP(v.audio, &packet); err != nil {
			return
		}
		if v.clockStep != 0 {
			now := time.Now()
			var offset time.Duration
			if armed := v.clockArmed.Load(); armed != 0 && now.UnixNano() >= armed {
				offset = v.clockStep
			}
			clock.send(v, v.audio, &packet, 8000, now, offset, 1, frame)
		}
		packet.Timestamp += frame
		packet.SequenceNumber++
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}
