package diagnostic

import (
	"fmt"

	"github.com/bluenviron/gortsplib/v5/pkg/rtptime"
	"github.com/pion/rtp"
)

// The live GlobalDecoder reads time.Now. An offline pass must instead use the
// saved receive clock; processing speed must never change reconstructed PTS.
type investigationRTSPClock struct {
	leading            int
	arrival, pts, rate int64
	tracks             map[int]*investigationRTSPClockTrack
}
type investigationRTSPClockTrack struct {
	pts      int64
	previous uint32
	ssrc     uint32
}

func (c *investigationRTSPClock) decode(id int, codec rtptime.GlobalDecoderTrack, packet *rtp.Packet, arrival int64) (int64, bool, error) {
	rate := int64(codec.ClockRate())
	if rate <= 0 || rate > 1<<31-1 || arrival < 0 || arrival > maxPlayerElapsedMS {
		return 0, false, fmt.Errorf("RTP 时钟超出离线重建范围。")
	}
	if c.tracks == nil {
		c.tracks = map[int]*investigationRTSPClockTrack{}
	}
	track := c.tracks[id]
	eligible := codec.PTSEqualsDTS(packet)
	if track == nil {
		if !eligible {
			return 0, false, nil
		}
		if c.leading == 0 {
			c.leading = id
			c.arrival = arrival
			c.pts = 0
			c.rate = rate
		}
		pts := c.pts*rate/c.rate + (arrival-c.arrival)*rate/1000
		c.tracks[id] = &investigationRTSPClockTrack{pts: pts, previous: packet.Timestamp, ssrc: packet.SSRC}
		return pts, true, nil
	}
	if track.ssrc != packet.SSRC {
		return 0, false, fmt.Errorf("选中轨道的 SSRC 改变，无法可靠关联播放器媒体时钟。")
	}
	track.pts += int64(int32(packet.Timestamp - track.previous))
	track.previous = packet.Timestamp
	if track.pts < -2*rate || track.pts > maxPlayerElapsedMS*rate/1000 {
		return 0, false, fmt.Errorf("RTP 时间戳不连续，未跨越不明时钟生成片段。")
	}
	if c.leading == id && eligible {
		c.pts = track.pts
		c.arrival = arrival
		c.rate = rate
	}
	return track.pts, true, nil
}

// Reordering is bounded by the native diagnostic sequence window. A sequence
// hole is not converted into a supposedly complete access unit.
type investigationRTSPReorder struct {
	initialized           bool
	next                  uint16
	ssrc                  uint32
	pending               map[uint16]investigationRTSPPacket
	duplicates, reordered int
	onGap                 func() error
	consume               func(*rtp.Packet, Event) error
}
type investigationRTSPPacket struct {
	packet *rtp.Packet
	event  Event
}

func (r *investigationRTSPReorder) push(packet *rtp.Packet, event Event, consume func(*rtp.Packet, Event) error) error {
	r.consume = consume
	if !r.initialized {
		r.initialized = true
		r.next = packet.SequenceNumber
		r.ssrc = packet.SSRC
		r.pending = map[uint16]investigationRTSPPacket{}
	}
	if packet.SSRC != r.ssrc {
		return fmt.Errorf("选中轨道的 SSRC 改变，未拼接不同时钟的媒体。")
	}
	delta := int16(packet.SequenceNumber - r.next)
	if delta < -rtspSequenceWindow {
		return fmt.Errorf("RTP 序号跳变超出有界重排范围。")
	}
	if delta < 0 {
		r.duplicates++
		return nil
	}
	for delta >= rtspSequenceWindow {
		if len(r.pending) > 0 {
			if err := r.advance(); err != nil {
				return err
			}
		} else {
			if r.onGap != nil {
				if err := r.onGap(); err != nil {
					return err
				}
			}
			r.next = packet.SequenceNumber
		}
		delta = int16(packet.SequenceNumber - r.next)
	}
	if _, exists := r.pending[packet.SequenceNumber]; exists {
		r.duplicates++
		return nil
	}
	if delta > 0 {
		r.pending[packet.SequenceNumber] = investigationRTSPPacket{packet: packet.Clone(), event: event}
		r.reordered++
		return nil
	}
	// UDP replay reuses its read buffer. Depayloaders retain fragments and
	// aggregate NALUs until marker, so even in-order packets need ownership.
	if err := consume(packet.Clone(), event); err != nil {
		return err
	}
	r.next++
	for {
		value, exists := r.pending[r.next]
		if !exists {
			return nil
		}
		delete(r.pending, r.next)
		if err := consume(value.packet, value.event); err != nil {
			return err
		}
		r.next++
	}
}
func (r *investigationRTSPReorder) advance() error {
	var next uint16
	distance := int32(1 << 16)
	for seq := range r.pending {
		if delta := int32(uint16(seq - r.next)); delta < distance {
			next = seq
			distance = delta
		}
	}
	if distance == 1<<16 {
		return nil
	}
	if distance > 0 && r.onGap != nil {
		if err := r.onGap(); err != nil {
			return err
		}
	}
	r.next = next
	for {
		value, exists := r.pending[r.next]
		if !exists {
			return nil
		}
		delete(r.pending, r.next)
		if err := r.consume(value.packet, value.event); err != nil {
			return err
		}
		r.next++
	}
}
func (r *investigationRTSPReorder) finish() error {
	for len(r.pending) > 0 {
		if err := r.advance(); err != nil {
			return err
		}
	}
	return nil
}
