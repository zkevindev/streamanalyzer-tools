package mediasource

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/yutopp/go-rtmp"
	"github.com/yutopp/go-rtmp/message"
)

type RTMPConfig struct {
	StallMS         int    `json:"stall_ms,omitempty"`
	DelayKeyframeMS int    `json:"delay_keyframe_ms,omitempty"`
	Fault           string `json:"fault"`
	Port            int    `json:"port"`
}

type RTMPStatus struct {
	Ingest *RTMPIngestStatus `json:"ingest,omitempty"`
	RTMPConfig
	Active      bool   `json:"active"`
	URL         string `json:"url,omitempty"`
	Connections uint64 `json:"connections"`
	Plays       uint64 `json:"plays"`
	FaultHits   uint64 `json:"fault_hits"`
	LastStage   string `json:"last_stage,omitempty"`
}

type RTMPManager struct {
	mu     sync.Mutex
	source *rtmpSource
	closed bool
}

type rtmpSource struct {
	relay    *rtmpRelay
	status   RTMPStatus
	ctx      context.Context
	cancel   context.CancelFunc
	listener net.Listener
	server   *rtmp.Server
	served   chan struct{}
	plan     []chunk
	mu       sync.Mutex
	clients  map[*rtmpWire]bool
}

func (m *RTMPManager) Start(cfg RTMPConfig) (RTMPStatus, error) {
	switch cfg.Fault {
	case "":
		cfg.Fault = "normal"
	case "normal", "tls_untrusted", "handshake_stall", "app_reject", "stream_not_found", "no_media", "publish_relay", "publish_failover":
	default:
		return RTMPStatus{}, ErrInvalid
	}
	if cfg.Fault == "publish_failover" && cfg.StallMS == 0 {
		cfg.StallMS = 1500
	}
	if cfg.Fault == "publish_failover" && (cfg.StallMS < 500 || cfg.StallMS > 10000) ||
		cfg.Fault != "publish_failover" && cfg.StallMS != 0 {
		return RTMPStatus{}, ErrInvalid
	}
	if cfg.Port < 0 || cfg.Port > 65535 || cfg.DelayKeyframeMS < 0 || cfg.DelayKeyframeMS > 15000 ||
		cfg.Fault != "publish_relay" && cfg.DelayKeyframeMS != 0 {
		return RTMPStatus{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return RTMPStatus{}, ErrClosed
	}
	if m.source != nil && m.source.ctx.Err() == nil {
		return RTMPStatus{}, ErrActive
	}

	data, err := fixtures.ReadFile("fixtures/regular.flv")
	if err != nil {
		return RTMPStatus{}, err
	}
	plan, err := planFLV(data, "normal")
	if err != nil {
		return RTMPStatus{}, err
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: cfg.Port})
	if err != nil {
		return RTMPStatus{}, err
	}
	var listen net.Listener = listener
	scheme := "rtmp"
	if cfg.Fault == "tls_untrusted" {
		cert, err := fixtureRTMPCertificate()
		if err != nil {
			listener.Close()
			return RTMPStatus{}, err
		}
		listen = tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		scheme = "rtmps"
	}
	ctx, cancel := context.WithCancel(context.Background())
	target := url.URL{Scheme: scheme, Host: listener.Addr().String(), Path: "/live/test"}
	source := &rtmpSource{
		status: RTMPStatus{RTMPConfig: cfg, Active: true, URL: target.String()},
		ctx:    ctx, cancel: cancel, listener: listen, served: make(chan struct{}), plan: plan,
		clients: make(map[*rtmpWire]bool),
	}
	if cfg.Fault == "publish_relay" || cfg.Fault == "publish_failover" {
		source.relay = &rtmpRelay{start: time.Now(), viewers: make(map[*rtmpWire]*relayViewer)}
	}
	if cfg.Fault == "publish_failover" {
		backup := target
		backup.Path = "/live/backup"
		source.relay.failover = &rtmpFailover{report: RTMPFailoverStatus{StallMS: cfg.StallMS, BackupURL: backup.String()}}
	}
	source.server = rtmp.NewServer(&rtmp.ServerConfig{OnConnect: source.accept})
	m.source = source
	go func() {
		defer close(source.served)
		_ = source.server.Serve(listen)
	}()
	return source.snapshot(), nil
}

func (m *RTMPManager) Status() RTMPStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.source == nil {
		return RTMPStatus{}
	}
	return m.source.snapshot()
}

func (m *RTMPManager) Stop() RTMPStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked()
}

func (m *RTMPManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.stopLocked()
}

func (m *RTMPManager) stopLocked() RTMPStatus {
	s := m.source
	if s == nil {
		return RTMPStatus{}
	}
	s.mu.Lock()
	if !s.status.Active {
		s.mu.Unlock()
		return s.snapshot()
	}
	s.status.Active = false
	s.cancel()
	clients := make([]*rtmpWire, 0, len(s.clients))
	for client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.Unlock()
	_ = s.server.Close()
	_ = s.listener.Close()
	for _, client := range clients {
		client.Close()
	}
	<-s.served
	return s.snapshot()
}

func (s *rtmpSource) snapshot() RTMPStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status
	if s.relay != nil {
		status.Ingest = s.relay.snapshot()
	}
	return status
}

func (s *rtmpSource) accept(conn net.Conn) (io.ReadWriteCloser, *rtmp.ConnConfig) {
	client := &rtmpWire{Conn: conn, source: s}
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	s.mu.Lock()
	if s.ctx.Err() != nil || len(s.clients) >= 8 {
		_ = conn.Close()
	} else {
		s.clients[client] = true
		s.status.Connections++
		s.status.LastStage = "handshake"
		if s.status.Fault == "tls_untrusted" {
			s.status.LastStage = "tls"
			s.status.FaultHits++
		}
	}
	s.mu.Unlock()
	client.stall = s.status.Fault == "handshake_stall"
	return client, &rtmp.ConnConfig{Handler: &rtmpFixtureHandler{source: s, wire: client}}
}

type rtmpWire struct {
	net.Conn
	source *rtmpSource
	stall  bool
	once   sync.Once
}

func (w *rtmpWire) Write(data []byte) (int, error) {
	if w.stall {
		w.once.Do(func() { w.source.record("handshake", true, false) })
		timer := time.NewTimer(8 * time.Second)
		defer timer.Stop()
		select {
		case <-w.source.ctx.Done():
			return 0, net.ErrClosed
		case <-timer.C:
			return 0, context.DeadlineExceeded
		}
	}
	return w.Conn.Write(data)
}

func (w *rtmpWire) Close() error {
	w.source.mu.Lock()
	delete(w.source.clients, w)
	w.source.dropRelayWire(w)
	w.source.mu.Unlock()
	return w.Conn.Close()
}

func (s *rtmpSource) record(stage string, fault, played bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.status.Active {
		return
	}
	s.status.LastStage = stage
	if fault {
		s.status.FaultHits++
	}
	if played {
		s.status.Plays++
	}
}

type rtmpFixtureHandler struct {
	publishID uint32
	playID    uint32
	rtmp.DefaultHandler
	source *rtmpSource
	wire   *rtmpWire
	conn   *rtmp.Conn
}

func (h *rtmpFixtureHandler) OnServe(conn *rtmp.Conn) { h.conn = conn }

func (h *rtmpFixtureHandler) OnClose() {
	h.source.mu.Lock()
	defer h.source.mu.Unlock()
	h.source.dropRelayWire(h.wire)
}

func (h *rtmpFixtureHandler) OnConnect(_ uint32, cmd *message.NetConnectionConnect) error {
	reject := h.source.status.Fault == "app_reject" || cmd.Command.App != "live"
	h.source.record("app_connect", reject, false)
	if reject {
		return errors.New("fixture app rejected")
	}
	if h.source.relay != nil {
		_ = h.wire.SetDeadline(time.Time{})
	}
	return nil
}

func (h *rtmpFixtureHandler) OnPublish(stream *rtmp.StreamContext, _ uint32, command *message.NetStreamPublish) error {
	if h.source.relay == nil || (command.PublishingName != "test" && command.PublishingName != "backup") || h.playID != 0 || h.publishID != 0 {
		return errors.New("RTMP publish is unavailable for this stream")
	}
	if err := h.source.reservePublisher(h.wire, command.PublishingName); err != nil {
		return err
	}
	h.publishID = stream.StreamID
	return nil
}

func (h *rtmpFixtureHandler) OnPublishStarted(_ *rtmp.StreamContext) {
	h.source.publisherStarted(h.wire)
}

func (h *rtmpFixtureHandler) OnAudio(timestamp uint32, payload io.Reader) error {
	return h.relayMedia(relayAudio, timestamp, payload)
}

func (h *rtmpFixtureHandler) OnVideo(timestamp uint32, payload io.Reader) error {
	return h.relayMedia(relayVideo, timestamp, payload)
}

func (h *rtmpFixtureHandler) OnDeleteStream(_ uint32, command *message.NetStreamDeleteStream) error {
	if h.source.relay != nil {
		h.source.mu.Lock()
		defer h.source.mu.Unlock()
		if command.StreamID == h.publishID {
			h.source.releasePublisher(h.wire)
			h.publishID = 0
		}
		if command.StreamID == h.playID {
			h.source.releaseViewer(h.wire)
			h.playID = 0
		}
	}
	return nil
}

func (h *rtmpFixtureHandler) OnFCUnpublish(_ uint32, command *message.NetStreamFCUnpublish) error {
	if h.source.relay != nil && (command.StreamName == "test" || command.StreamName == "backup") {
		h.source.mu.Lock()
		h.source.releasePublisher(h.wire)
		h.source.mu.Unlock()
		h.publishID = 0
	}
	return nil
}

func (h *rtmpFixtureHandler) OnPlay(_ *rtmp.StreamContext, _ uint32, play *message.NetStreamPlay) error {
	reject := h.source.status.Fault == "stream_not_found" || play.StreamName != "test" || h.source.relay != nil && (h.playID != 0 || h.publishID != 0)
	h.source.record("play", reject, false)
	if reject {
		return rtmp.ErrStreamNotFound
	}
	return nil
}

func (h *rtmpFixtureHandler) OnPlayStarted(stream *rtmp.StreamContext) {
	s := h.source
	stall := s.status.Fault == "no_media"
	s.record("receive", stall, true)
	if stall {
		return
	}
	id := stream.StreamID
	if s.relay != nil {
		h.playID = id
		h.playRelay(id)
		return
	}
	go func() {
		defer h.wire.Close()
		start := time.Now()
		for _, ch := range s.plan {
			if len(ch.data) < 16 {
				continue
			}
			var payload message.Message
			var channel int
			switch ch.data[0] {
			case 8:
				payload = &message.AudioMessage{Payload: bytes.NewReader(ch.data[11 : len(ch.data)-4])}
				channel = 5
			case 9:
				payload = &message.VideoMessage{Payload: bytes.NewReader(ch.data[11 : len(ch.data)-4])}
				channel = 6
			default:
				continue
			}
			if !waitForChunk(s.ctx, s.ctx, ch.at-time.Since(start)) {
				return
			}
			if h.conn.Write(s.ctx, channel, uint32(ch.at.Milliseconds()), &rtmp.ChunkMessage{StreamID: id, Message: payload}) != nil {
				return
			}
		}
	}()
}

func fixtureRTMPCertificate() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	template := x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("生成本地 RTMPS 测试证书：%w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
