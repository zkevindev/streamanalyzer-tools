package onvif

import (
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

type senderClock struct {
	base     time.Time
	last     time.Time
	previous uint32
	ticks    uint64
	count    uint32
	octets   uint32
}

func (s *senderClock) send(track *videoTrack, media *description.Media, packet *rtp.Packet, rate int, now time.Time, offset time.Duration, count, octets uint32) {
	if track.clockStep == 0 {
		return
	}
	if s.base.IsZero() {
		s.base, s.previous = now, packet.Timestamp
	}
	s.ticks += uint64(packet.Timestamp - s.previous)
	s.previous = packet.Timestamp
	s.count += count
	s.octets += octets
	if now.Sub(s.last) < time.Second {
		return
	}
	s.last = now
	seconds := time.Duration(s.ticks/uint64(rate)) * time.Second
	fraction := time.Duration(s.ticks%uint64(rate)) * time.Second / time.Duration(rate)
	timestamp := s.base.Add(seconds + fraction + offset)
	ntp := uint64(timestamp.Unix()+2208988800)<<32 | (uint64(timestamp.Nanosecond())<<32)/1000000000
	_ = track.stream.WritePacketRTCP(media, &rtcp.SenderReport{
		SSRC: packet.SSRC, NTPTime: ntp, RTPTime: packet.Timestamp, PacketCount: s.count, OctetCount: s.octets,
	})
}
