package mediasource

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"time"

	"github.com/yutopp/go-rtmp"
	"github.com/yutopp/go-rtmp/message"
)

const relayMaxPendingBytes = 4 << 20

type RTMPPublication struct {
	Number            uint64    `json:"number"`
	PublishedAt       time.Time `json:"published_at"`
	FirstMediaAt      time.Time `json:"first_media_at,omitzero"`
	FirstKeyframeAt   time.Time `json:"first_keyframe_at,omitzero"`
	RelayKeyframeAt   time.Time `json:"relay_keyframe_at,omitzero"`
	DisconnectedAt    time.Time `json:"disconnected_at,omitzero"`
	WithheldKeyframes uint64    `json:"withheld_keyframes"`
}

type RTMPIngestStatus struct {
	Failover   *RTMPFailoverStatus `json:"failover,omitempty"`
	Publishing bool                `json:"publishing"`
	Viewers    int                 `json:"viewers"`
	Sessions   []RTMPPublication   `json:"sessions"`
}

type relayPacket struct {
	media     byte
	timestamp uint32
	payload   []byte
}

func (p relayPacket) message() (int, message.Message) {
	if p.media == relayAudio {
		return 5, &message.AudioMessage{Payload: bytes.NewReader(p.payload)}
	}
	return 6, &message.VideoMessage{Payload: bytes.NewReader(p.payload)}
}

const (
	relayAudio = 8
	relayVideo = 9
)

type relayViewer struct {
	frames  chan relayPacket
	done    chan struct{}
	pending int
}

// Relay state is guarded by rtmpSource.mu.
type rtmpRelay struct {
	failover             *rtmpFailover
	start                time.Time
	publisher            *rtmpWire
	viewers              map[*rtmpWire]*relayViewer
	sessions             []RTMPPublication
	publications         uint64
	initAudio, initVideo *relayPacket
	firstStamp           *uint32
	bridgeStamp          uint32
	lastRelayStamp       uint32
}

func (r *rtmpRelay) snapshot() *RTMPIngestStatus {
	status := &RTMPIngestStatus{Publishing: r.publisher != nil && r.currentPublication() != nil, Viewers: len(r.viewers), Sessions: slices.Clone(r.sessions)}
	if f := r.failover; f != nil {
		report := f.report
		report.PrimaryConnected = f.demoted != nil || (report.SwitchedAt.IsZero() && r.publisher != nil)
		status.Failover = &report
	}
	return status
}

func (r *rtmpRelay) currentPublication() *RTMPPublication {
	if len(r.sessions) == 0 || !r.sessions[len(r.sessions)-1].DisconnectedAt.IsZero() {
		return nil
	}
	return &r.sessions[len(r.sessions)-1]
}

func (s *rtmpSource) reservePublisher(wire *rtmpWire, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.relay
	if name == "backup" {
		if f := r.failover; f != nil && f.standby == nil && f.report.SwitchedAt.IsZero() && s.ctx.Err() == nil {
			f.standby = wire
			return nil
		}
		return errors.New("the relay does not have an available standby slot")
	}
	if r.publisher != nil || s.ctx.Err() != nil {
		return errors.New("the relay already has a publisher or has stopped")
	}
	r.publisher = wire
	return nil
}

func (s *rtmpSource) publisherStarted(wire *rtmpWire) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.relay
	if f := r.failover; f != nil && f.standby == wire && s.ctx.Err() == nil {
		f.report.BackupPublishedAt = time.Now()
		return
	}
	if r.publisher != wire || s.ctx.Err() != nil {
		return
	}
	r.appendPublication(time.Now())
	s.status.LastStage = "publish"
}

func (r *rtmpRelay) appendPublication(at time.Time) {
	if len(r.sessions) == 8 {
		r.sessions = slices.Delete(r.sessions, 0, 1)
	}
	r.publications++
	r.sessions = append(r.sessions, RTMPPublication{Number: r.publications, PublishedAt: at})
}

func (s *rtmpSource) releasePublisher(wire *rtmpWire) {
	r := s.relay
	if f := r.failover; f != nil {
		switch wire {
		case f.standby:
			f.standby, f.backupAudio, f.backupVideo = nil, nil, nil
			f.report.BackupPublishedAt = time.Time{}
		case f.demoted:
			f.demoted = nil
			for i := range r.sessions {
				if r.sessions[i].Number == f.oldNumber {
					r.sessions[i].DisconnectedAt = time.Now()
					break
				}
			}
		}
	}
	if r.publisher == wire {
		if p := r.currentPublication(); p != nil {
			p.DisconnectedAt = time.Now()
		}
		if f := r.failover; f != nil && f.report.SwitchedAt.IsZero() {
			f.report.PrimaryLastMediaAt, f.report.StallDetectedAt = time.Time{}, time.Time{}
		}
		r.publisher, r.firstStamp, r.initAudio, r.initVideo = nil, nil, nil, nil
		if s.status.Active {
			s.status.LastStage = "publisher_disconnected"
		}
	}
}

func (s *rtmpSource) relayViewer(wire *rtmpWire) *relayViewer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil || !s.clients[wire] {
		return nil
	}
	viewer := &relayViewer{frames: make(chan relayPacket, 64), done: make(chan struct{})}
	for _, packet := range []*relayPacket{s.relay.initAudio, s.relay.initVideo} {
		if packet != nil {
			viewer.frames <- *packet
			viewer.pending += len(packet.payload)
		}
	}
	s.relay.viewers[wire] = viewer
	return viewer
}

func (s *rtmpSource) releaseViewer(wire *rtmpWire) {
	if viewer := s.relay.viewers[wire]; viewer != nil {
		delete(s.relay.viewers, wire)
		close(viewer.done)
	}
}

func (s *rtmpSource) dropRelayWire(wire *rtmpWire) {
	if s.relay != nil {
		s.releasePublisher(wire)
		s.releaseViewer(wire)
	}
}

func (h *rtmpFixtureHandler) playRelay(streamID uint32) {
	s := h.source
	viewer := s.relayViewer(h.wire)
	if viewer == nil {
		return
	}
	go func() {
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-viewer.done:
				return
			case packet := <-viewer.frames:
				s.mu.Lock()
				viewer.pending -= len(packet.payload)
				s.mu.Unlock()
				channel, data := packet.message()
				ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
				err := h.conn.Write(ctx, channel, packet.timestamp, &rtmp.ChunkMessage{StreamID: streamID, Message: data})
				cancel()
				if err != nil {
					_ = h.wire.Close()
					return
				}
			}
		}
	}()
}

func (h *rtmpFixtureHandler) relayMedia(kind byte, timestamp uint32, payload io.Reader) error {
	if h.source.relay == nil || h.publishID == 0 {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(payload, relayMaxPendingBytes/2+1))
	if err != nil {
		return err
	}
	if len(data) > relayMaxPendingBytes/2 {
		return errors.New("RTMP relay frame exceeds 2 MiB")
	}
	if len(data) < 2 {
		return nil
	}
	return h.source.forwardMedia(h.wire, kind, timestamp, data)
}

func (s *rtmpSource) forwardMedia(wire *rtmpWire, kind byte, stamp uint32, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.relay
	now := time.Now()
	config, key := relayHeader(kind, data)
	if f := r.failover; f != nil {
		if f.demoted == wire {
			return nil
		}
		if f.standby == wire && !s.standbyMedia(wire, kind, stamp, data, now, config, key) {
			return nil
		}
		if f.report.SwitchedAt.IsZero() && r.publisher == wire && !config {
			f.report.PrimaryLastMediaAt = now
			f.report.StallDetectedAt = time.Time{}
		}
	}
	p := r.currentPublication()
	if r.publisher != wire || p == nil {
		return errors.New("RTMP publisher is unavailable")
	}
	if !config && p.FirstMediaAt.IsZero() {
		p.FirstMediaAt = now
		s.status.LastStage = "publisher_media"
	}
	if key && p.FirstKeyframeAt.IsZero() {
		p.FirstKeyframeAt = now
	}
	if s.status.DelayKeyframeMS > 0 && kind == relayVideo && !config && p.RelayKeyframeAt.IsZero() &&
		(!key || now.Sub(p.PublishedAt) < time.Duration(s.status.DelayKeyframeMS)*time.Millisecond) {
		if key {
			p.WithheldKeyframes++
			s.status.FaultHits++
		}
		return nil
	}
	if key && p.RelayKeyframeAt.IsZero() {
		p.RelayKeyframeAt = now
		s.status.LastStage = "publisher_keyframe"
	}
	s.forwardPacket(kind, stamp, data, now, config)
	return nil
}

func (s *rtmpSource) forwardPacket(kind byte, stamp uint32, data []byte, now time.Time, config bool) {
	r := s.relay
	if r.firstStamp == nil {
		r.firstStamp = &stamp
		r.bridgeStamp = max(r.lastRelayStamp+1, uint32(now.Sub(r.start).Milliseconds()))
	}
	delta := stamp - *r.firstStamp
	if delta > uint32((24 * time.Hour).Milliseconds()) {
		delta = 0 // A backwards timestamp around the first configuration packet.
	}
	packet := relayPacket{media: kind, timestamp: r.bridgeStamp + delta, payload: data}
	r.lastRelayStamp = max(r.lastRelayStamp, packet.timestamp)
	if config {
		if kind == relayAudio {
			r.initAudio = &packet
		} else {
			r.initVideo = &packet
		}
	}
	for client, viewer := range r.viewers {
		if len(viewer.frames) == cap(viewer.frames) || viewer.pending+len(data) > relayMaxPendingBytes {
			s.releaseViewer(client)
			go client.Close()
			continue
		}
		viewer.pending += len(data)
		viewer.frames <- packet
	}
}

func relayHeader(kind byte, data []byte) (config, key bool) {
	if kind == relayAudio {
		return data[0]>>4 == 10 && data[1] == 0, false
	}
	if data[0]&0x80 != 0 && len(data) >= 5 {
		packetType := data[0] & 15
		return packetType == 0, (packetType == 1 || packetType == 3) && (data[0]>>4)&7 == 1
	}
	if codec := data[0] & 15; codec == 7 || codec == 12 {
		return data[1] == 0, data[1] == 1 && data[0]>>4 == 1
	}
	return false, data[0]>>4 == 1
}
