package diagnostic

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/yutopp/go-flv/tag"
	"github.com/yutopp/go-rtmp"
	"github.com/yutopp/go-rtmp/message"
)

const (
	playerChunkLimit     = 64 << 10
	playerBootstrapLimit = 8 << 20
	playerBootstrapParts = 4096
	playerQueueSize      = 128
)

// PlayerSession relays one diagnostic sample to one local playback client.
// Its bounded queues never wait for a player to read media.
type PlayerSession struct {
	ctx       context.Context
	cancel    context.CancelFunc
	transport string
	hls       *playerHLS
	ready     chan struct{}

	mu             sync.Mutex
	bootstrap      [][]byte
	bootBytes      int
	live           chan []byte
	subscribed     bool
	ended          bool
	dropped        bool
	previewFailure string
}

func newPlayerSession(ctx context.Context, protocol string, target *url.URL, client *http.Client) *PlayerSession {
	ctx, cancel := context.WithCancel(ctx)
	session := &PlayerSession{ctx: ctx, cancel: cancel, ready: make(chan struct{})}
	switch protocol {
	case ProtocolHTTPFLV, ProtocolRTMP, ProtocolRTSP, ProtocolRTSPS:
		session.transport = "flv"
	case ProtocolHTTPTS, ProtocolUDPTS, ProtocolRTPTS:
		session.transport = "mpegts"
	case ProtocolHLS:
		session.transport = "hls"
		session.hls = newPlayerHLS(target, client)
	}
	return session
}

type playerSessionContextKey struct{}

func withPlayerSession(ctx context.Context, session *PlayerSession) context.Context {
	return context.WithValue(ctx, playerSessionContextKey{}, session)
}

func capturePlayer(ctx context.Context) *PlayerSession {
	session, _ := ctx.Value(playerSessionContextKey{}).(*PlayerSession)
	return session
}

func (p *PlayerSession) Transport() string { return p.transport }

func (p *PlayerSession) Source() string {
	if p.hls != nil {
		return "independent_pull"
	}
	return "same_capture"
}

func (p *PlayerSession) Done() <-chan struct{} { return p.ctx.Done() }

func (p *PlayerSession) WaitForSubscriber(ctx context.Context, timeout time.Duration) {
	if p.hls != nil {
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.ready:
	case <-p.ctx.Done():
	case <-ctx.Done():
	case <-timer.C:
	}
}

// Subscribe transfers the captured prefix and then the live queue to the
// player. A second attachment could start mid-stream and skew a measurement.
func (p *PlayerSession) Subscribe() ([][]byte, <-chan []byte, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hls != nil || p.ended || p.ctx.Err() != nil || p.dropped {
		return nil, nil, nil, ErrNotFound
	}
	if p.subscribed {
		return nil, nil, nil, ErrBusy
	}
	p.subscribed = true
	p.live = make(chan []byte, playerQueueSize)
	bootstrap, live := p.bootstrap, p.live
	p.bootstrap, p.bootBytes = nil, 0
	close(p.ready)
	return bootstrap, live, p.EndMedia, nil
}

func (p *PlayerSession) EndMedia() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.endMedia()
}

func (p *PlayerSession) endMedia() {
	p.ended = true
	p.bootstrap, p.bootBytes = nil, 0
	if p.live != nil {
		close(p.live)
		p.live = nil
	}
}

func (p *PlayerSession) Close() {
	p.cancel()
	p.EndMedia()
	if p.hls != nil {
		p.hls.close()
	}
}

func (p *PlayerSession) StreamDropped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dropped
}

func (p *PlayerSession) acceptsMedia() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.ended && p.ctx.Err() == nil
}

func (p *PlayerSession) dropMedia() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.ended {
		p.dropped = true
		p.endMedia()
	}
}

func (p *PlayerSession) publish(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ended || p.ctx.Err() != nil {
		return
	}
	if p.live == nil && (len(data) > playerBootstrapLimit-p.bootBytes || len(p.bootstrap)+(len(data)+playerChunkLimit-1)/playerChunkLimit > playerBootstrapParts) {
		p.dropped = true
		p.endMedia()
		return
	}
	for len(data) > 0 {
		n := min(len(data), playerChunkLimit)
		if p.live == nil {
			p.bootstrap = append(p.bootstrap, bytes.Clone(data[:n]))
			p.bootBytes += n
		} else if len(p.live) < cap(p.live) {
			// The only sender holds mu. A receiver can only free capacity.
			p.live <- bytes.Clone(data[:n])
		} else {
			p.dropped = true
			p.endMedia()
			return
		}
		data = data[n:]
	}
}

type rtmpPlayerHandler struct {
	rtmp.DefaultHandler
	player *PlayerSession
}

func (h *rtmpPlayerHandler) OnAudio(timestamp uint32, payload io.Reader) error {
	h.tag(tag.TagTypeAudio, timestamp, payload)
	return nil
}

func (h *rtmpPlayerHandler) OnVideo(timestamp uint32, payload io.Reader) error {
	h.tag(tag.TagTypeVideo, timestamp, payload)
	return nil
}

func (h *rtmpPlayerHandler) OnSetDataFrame(timestamp uint32, data *message.NetStreamSetDataFrame) error {
	if !h.player.acceptsMedia() {
		return nil
	}
	if len(data.Payload) != 0 {
		h.tag(tag.TagTypeScriptData, timestamp, bytes.NewReader(data.Payload))
		return nil
	}
	values, _ := data.AmfData.([]interface{})
	if len(values) != 0 {
		var metadata bytes.Buffer
		encoder := message.NewAMFEncoder(&metadata, message.EncodingTypeAMF0)
		if encoder.Encode("onMetaData") == nil && encoder.Encode(values[0]) == nil {
			h.tag(tag.TagTypeScriptData, timestamp, &metadata)
		}
	}
	return nil
}

// RTMP audio/video messages are already FLV tag bodies. Preserving those
// bytes also preserves Enhanced FLV headers and codec configuration records.
func (h *rtmpPlayerHandler) tag(kind tag.TagType, timestamp uint32, reader io.Reader) {
	if !h.player.acceptsMedia() {
		return
	}
	const maxPayload = playerBootstrapLimit - 15
	payload, err := io.ReadAll(io.LimitReader(reader, maxPayload+1))
	if err != nil || len(payload) > maxPayload {
		h.player.dropMedia()
		return
	}
	packet := make([]byte, 11+len(payload)+4)
	packet[0] = byte(kind)
	putFLVUint24(packet[1:4], uint32(len(payload)))
	putFLVTimestamp(packet[4:8], timestamp)
	copy(packet[11:], payload)
	binary.BigEndian.PutUint32(packet[11+len(payload):], uint32(11+len(payload)))
	h.player.publish(packet)
}

func (h *rtmpPlayerHandler) OnUnknownMessage(timestamp uint32, msg message.Message) error {
	aggregate, ok := msg.(*message.AggregateMessage)
	if !ok {
		return nil
	}
	// Aggregate messages contain complete FLV tags and need their media
	// timestamps rebased to the timestamp in the outer RTMP message.
	var firstTimestamp uint32
	for first := true; h.player.acceptsMedia(); first = false {
		var header [11]byte
		if _, err := io.ReadFull(aggregate.Payload, header[:]); err != nil {
			if err != io.EOF {
				h.player.dropMedia()
			}
			return nil
		}
		size := int(readUint24(header[1:4]))
		if size > playerBootstrapLimit-15 {
			h.player.dropMedia()
			return nil
		}
		packet := make([]byte, len(header)+size+4)
		copy(packet, header[:])
		if _, err := io.ReadFull(aggregate.Payload, packet[len(header):]); err != nil ||
			binary.BigEndian.Uint32(packet[len(header)+size:]) != uint32(len(header)+size) {
			h.player.dropMedia()
			return nil
		}
		at := readUint24(header[4:7]) | uint32(header[7])<<24
		if first {
			firstTimestamp = at
		}
		putFLVTimestamp(packet[4:8], timestamp+at-firstTimestamp)
		if k := tag.TagType(header[0]); k == tag.TagTypeAudio || k == tag.TagTypeVideo || k == tag.TagTypeScriptData {
			h.player.publish(packet)
		}
	}
	return nil
}

func putFLVUint24(dst []byte, value uint32) {
	dst[0], dst[1], dst[2] = byte(value>>16), byte(value>>8), byte(value)
}

func putFLVTimestamp(dst []byte, value uint32) {
	putFLVUint24(dst, value)
	dst[3] = byte(value >> 24)
}

type playerMediaWriter struct {
	writer io.Writer
	player *PlayerSession
}

func (w playerMediaWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	w.player.publish(data[:n])
	return n, err
}

// PreviewFailure is a controlled local pipeline code, never upstream error text.
func (p *PlayerSession) PreviewFailure() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.previewFailure
}
func (p *PlayerSession) failPreview(code string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.previewFailure == "" {
		p.previewFailure = code
	}
	p.endMedia()
}
