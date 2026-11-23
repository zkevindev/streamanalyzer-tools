package mediasource

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"streamanalyzer/internal/simulator/testpattern"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/auth"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
	"github.com/pion/rtp"
)

const (
	rtspPrimaryPath  = "/live/test"
	rtspRedirectPath = "/live/redirected"
)

type RTSPConfig struct {
	Fault     string `json:"fault"`
	Port      int    `json:"port"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	DropEvery int    `json:"drop_every,omitempty"`
}

type RTSPStatus struct {
	RTSPConfig
	Active         bool   `json:"active"`
	URL            string `json:"url,omitempty"`
	RedirectTarget string `json:"redirect_target,omitempty"`
	Connections    uint64 `json:"connections"`
	Describes      uint64 `json:"describes"`
	TCPPlays       uint64 `json:"tcp_plays"`
	UDPPlays       uint64 `json:"udp_plays"`
	FaultHits      uint64 `json:"fault_hits"`
	DroppedPackets uint64 `json:"dropped_packets"`
	Publishing     bool   `json:"publishing,omitempty"`
	Viewers        int    `json:"viewers,omitempty"`
	LastStage      string `json:"last_stage,omitempty"`
}

type RTSPManager struct {
	mu     sync.Mutex
	source *rtspSource
	closed bool
}

type rtspSource struct {
	mu             sync.Mutex
	status         RTSPStatus
	ctx            context.Context
	cancel         context.CancelFunc
	server         *gortsplib.Server
	stream         *gortsplib.ServerStream
	videoMedia     *description.Media
	audioMedia     *description.Media
	encoder        *rtph264.Encoder
	frames         [][][]byte
	publisher      *gortsplib.ServerSession
	relayStream    *gortsplib.ServerStream
	viewers        map[*gortsplib.ServerSession]bool
	ready          chan struct{}
	done           chan struct{}
	sentPackets    atomic.Uint64
	droppedPackets atomic.Uint64
}

func (m *RTSPManager) Start(cfg RTSPConfig) (RTSPStatus, error) {
	switch cfg.Fault {
	case "":
		cfg.Fault = "normal"
	case "normal", "tls_untrusted", "auth_required", "redirect", "not_found", "service_unavailable", "no_media", "rtp_drop", "publish_relay":
	default:
		return RTSPStatus{}, ErrInvalid
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return RTSPStatus{}, ErrInvalid
	}
	if cfg.Fault == "rtp_drop" {
		if cfg.DropEvery == 0 {
			cfg.DropEvery = 10
		}
		if cfg.DropEvery < 2 || cfg.DropEvery > 200 {
			return RTSPStatus{}, ErrInvalid
		}
	} else if cfg.DropEvery != 0 {
		return RTSPStatus{}, ErrInvalid
	}
	if cfg.Fault == "auth_required" && (cfg.Username == "" || cfg.Password == "") {
		return RTSPStatus{}, ErrInvalid
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return RTSPStatus{}, ErrClosed
	}
	if m.source != nil && m.source.ctx.Err() == nil {
		return RTSPStatus{}, ErrActive
	}

	frames, sps, pps, err := buildRTSPPatternFrames()
	if err != nil {
		return RTSPStatus{}, err
	}
	avc := &format.H264{
		PayloadTyp:        96,
		PacketizationMode: 1,
		SPS:               sps,
		PPS:               pps,
	}
	encoder, err := avc.CreateEncoder()
	if err != nil {
		return RTSPStatus{}, err
	}

	tcp, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: cfg.Port})
	if err != nil {
		return RTSPStatus{}, err
	}

	var tlsCfg *tls.Config
	scheme := "rtsp"
	var rtpConn, rtcpConn *net.UDPConn
	if cfg.Fault == "tls_untrusted" {
		cert, err := fixtureRTMPCertificate()
		if err != nil {
			_ = tcp.Close()
			return RTSPStatus{}, err
		}
		tlsCfg = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		scheme = "rtsps"
	} else {
		rtpConn, rtcpConn, err = listenRTSPUDP(net.IPv4(127, 0, 0, 1))
		if err != nil {
			_ = tcp.Close()
			return RTSPStatus{}, err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	primaryURL := url.URL{Scheme: scheme, Host: tcp.Addr().String(), Path: rtspPrimaryPath}
	redirectTarget := ""
	if cfg.Fault == "redirect" {
		redirectURL := primaryURL
		redirectURL.Path = rtspRedirectPath
		redirectTarget = redirectURL.String()
	}

	src := &rtspSource{
		status: RTSPStatus{
			RTSPConfig:     cfg,
			Active:         true,
			URL:            primaryURL.String(),
			RedirectTarget: redirectTarget,
		},
		ctx:        ctx,
		cancel:     cancel,
		videoMedia: &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{avc}},
		audioMedia: &description.Media{Type: description.MediaTypeAudio, Formats: []format.Format{&format.G711{PayloadTyp: 8, SampleRate: 8000, ChannelCount: 1}}},
		encoder:    encoder,
		frames:     frames,
		viewers:    make(map[*gortsplib.ServerSession]bool),
		ready:      make(chan struct{}),
		done:       make(chan struct{}),
	}

	srv := &gortsplib.Server{
		Handler:     src,
		AuthMethods: []auth.VerifyMethod{auth.VerifyMethodDigestMD5, auth.VerifyMethodBasic},
		RTSPAddress: tcp.Addr().String(),
		TLSConfig:   tlsCfg,
		Listen:      func(_, _ string) (net.Listener, error) { return tcp, nil },
	}
	if rtpConn != nil && rtcpConn != nil {
		srv.UDPRTPAddress = rtpConn.LocalAddr().String()
		srv.UDPRTCPAddress = rtcpConn.LocalAddr().String()
		srv.ListenPacket = func(_, address string) (net.PacketConn, error) {
			if address == rtpConn.LocalAddr().String() {
				return rtpConn, nil
			}
			return rtcpConn, nil
		}
	}
	src.server = srv

	if err := srv.Start(); err != nil {
		cancel()
		_ = tcp.Close()
		if rtpConn != nil {
			_ = rtpConn.Close()
		}
		if rtcpConn != nil {
			_ = rtcpConn.Close()
		}
		return RTSPStatus{}, err
	}

	if cfg.Fault != "publish_relay" {
		stream := &gortsplib.ServerStream{
			Server: srv,
			Desc:   &description.Session{Medias: []*description.Media{src.videoMedia, src.audioMedia}},
		}
		if err := stream.Initialize(); err != nil {
			cancel()
			close(src.ready)
			srv.Close()
			return RTSPStatus{}, err
		}
		src.stream = stream
	}

	close(src.ready)
	m.source = src

	if cfg.Fault != "publish_relay" && cfg.Fault != "no_media" {
		go src.serveMediaLoop()
	} else {
		close(src.done)
	}

	return src.snapshot(), nil
}

func (m *RTSPManager) Status() RTSPStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.source == nil {
		return RTSPStatus{}
	}
	return m.source.snapshot()
}

func (m *RTSPManager) Stop() RTSPStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked()
}

func (m *RTSPManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.stopLocked()
}

func (m *RTSPManager) stopLocked() RTSPStatus {
	s := m.source
	if s == nil {
		return RTSPStatus{}
	}
	s.mu.Lock()
	if !s.status.Active {
		s.mu.Unlock()
		return s.snapshot()
	}
	s.status.Active = false
	relay := s.relayStream
	s.relayStream = nil
	s.publisher = nil
	s.mu.Unlock()

	s.cancel()
	<-s.done
	if relay != nil {
		relay.Close()
	}
	if s.stream != nil {
		s.stream.Close()
	}
	if s.server != nil {
		s.server.Close()
	}
	return s.snapshot()
}

func (s *rtspSource) snapshot() RTSPStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.DroppedPackets = s.droppedPackets.Load()
	st.Publishing = s.publisher != nil
	st.Viewers = len(s.viewers)
	return st
}

func (s *rtspSource) recordStage(stage string, faultHit bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.status.Active {
		return
	}
	s.status.LastStage = stage
	if faultHit {
		s.status.FaultHits++
	}
}

func (s *rtspSource) OnConnOpen(_ *gortsplib.ServerHandlerOnConnOpenCtx) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.status.Active {
		return
	}
	s.status.Connections++
	if s.status.Fault == "tls_untrusted" {
		s.status.LastStage = "tls"
		s.status.FaultHits++
	}
}

func (s *rtspSource) verifyAuth(conn *gortsplib.ServerConn, req *base.Request) bool {
	if s.status.Fault != "auth_required" {
		return true
	}
	if conn.VerifyCredentials(req, s.status.Username, s.status.Password) {
		return true
	}
	s.recordStage("auth", true)
	return false
}

func (s *rtspSource) resolveStream(path string) (*base.Response, *gortsplib.ServerStream) {
	switch s.status.Fault {
	case "not_found":
		s.recordStage("describe", true)
		return &base.Response{StatusCode: base.StatusNotFound}, nil
	case "service_unavailable":
		s.recordStage("describe", true)
		return &base.Response{StatusCode: base.StatusServiceUnavailable}, nil
	case "redirect":
		if path == rtspPrimaryPath {
			s.recordStage("redirect", true)
			return &base.Response{
				StatusCode: base.StatusCode(302),
				Header:     base.Header{"Location": base.HeaderValue{s.status.RedirectTarget}},
			}, nil
		}
		if path != rtspRedirectPath {
			return &base.Response{StatusCode: base.StatusNotFound}, nil
		}
	case "publish_relay":
		if path != rtspPrimaryPath {
			return &base.Response{StatusCode: base.StatusNotFound}, nil
		}
		s.mu.Lock()
		relay := s.relayStream
		s.mu.Unlock()
		if relay == nil {
			s.recordStage("describe", true)
			return &base.Response{StatusCode: base.StatusNotFound}, nil
		}
		return &base.Response{StatusCode: base.StatusOK}, relay
	default:
		if path != rtspPrimaryPath {
			return &base.Response{StatusCode: base.StatusNotFound}, nil
		}
	}
	return &base.Response{StatusCode: base.StatusOK}, s.stream
}

func (s *rtspSource) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	<-s.ready
	s.mu.Lock()
	if s.status.Active {
		s.status.Describes++
		s.status.LastStage = "describe"
	}
	s.mu.Unlock()

	if !s.verifyAuth(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	res, stream := s.resolveStream(ctx.Path)
	return res, stream, nil
}

func (s *rtspSource) OnAnnounce(ctx *gortsplib.ServerHandlerOnAnnounceCtx) (*base.Response, error) {
	<-s.ready
	if !s.verifyAuth(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, liberrors.ErrServerAuth{}
	}
	if s.status.Fault != "publish_relay" || ctx.Path != rtspPrimaryPath {
		return &base.Response{StatusCode: base.StatusForbidden}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publisher != nil && s.publisher != ctx.Session {
		return &base.Response{StatusCode: base.StatusCode(409)}, nil
	}
	if s.relayStream != nil {
		s.relayStream.Close()
		s.relayStream = nil
	}
	stream := &gortsplib.ServerStream{
		Server: s.server,
		Desc:   ctx.Description,
	}
	if err := stream.Initialize(); err != nil {
		return &base.Response{StatusCode: base.StatusInternalServerError}, err
	}
	s.publisher = ctx.Session
	s.relayStream = stream
	s.status.LastStage = "announce"
	return &base.Response{StatusCode: base.StatusOK}, nil
}

func (s *rtspSource) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	<-s.ready
	if !s.verifyAuth(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	s.recordStage("setup", false)
	if s.status.Fault == "publish_relay" {
		s.mu.Lock()
		isPub := s.publisher == ctx.Session
		relay := s.relayStream
		s.mu.Unlock()
		if isPub {
			return &base.Response{StatusCode: base.StatusOK}, nil, nil
		}
		if relay == nil {
			return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
		}
		return &base.Response{StatusCode: base.StatusOK}, relay, nil
	}
	res, stream := s.resolveStream(ctx.Path)
	return res, stream, nil
}

func (s *rtspSource) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	<-s.ready
	if !s.verifyAuth(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, liberrors.ErrServerAuth{}
	}
	s.mu.Lock()
	if s.status.Active {
		s.status.LastStage = "play"
		if s.status.Fault == "no_media" {
			s.status.FaultHits++
		}
		if ctx.Session.Transport() != nil && ctx.Session.Transport().Protocol == gortsplib.ProtocolTCP {
			s.status.TCPPlays++
		} else {
			s.status.UDPPlays++
		}
		s.viewers[ctx.Session] = true
	}
	s.mu.Unlock()
	return &base.Response{StatusCode: base.StatusOK}, nil
}

func (s *rtspSource) OnRecord(ctx *gortsplib.ServerHandlerOnRecordCtx) (*base.Response, error) {
	<-s.ready
	if !s.verifyAuth(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, liberrors.ErrServerAuth{}
	}
	s.recordStage("record", false)
	ctx.Session.OnPacketRTPAny(func(medi *description.Media, _ format.Format, pkt *rtp.Packet) {
		s.mu.Lock()
		relay := s.relayStream
		s.mu.Unlock()
		if relay != nil {
			_ = relay.WritePacketRTP(medi, pkt)
		}
	})
	return &base.Response{StatusCode: base.StatusOK}, nil
}

func (s *rtspSource) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	<-s.ready
	s.mu.Lock()
	delete(s.viewers, ctx.Session)
	var oldRelay *gortsplib.ServerStream
	if s.publisher == ctx.Session {
		s.publisher = nil
		oldRelay = s.relayStream
		s.relayStream = nil
	}
	s.mu.Unlock()
	if oldRelay != nil {
		oldRelay.Close()
	}
}

func (s *rtspSource) serveMediaLoop() {
	defer close(s.done)
	ticker := time.NewTicker(time.Second / time.Duration(testpattern.FPS))
	defer ticker.Stop()

	var videoTS uint32
	var audioTS uint32
	var audioSeq uint16
	audioPayload := bytes.Repeat([]byte{0xd5}, 533)

	for idx := 0; ; idx = (idx + 1) % len(s.frames) {
		packets, err := s.encoder.Encode(s.frames[idx])
		if err != nil {
			return
		}
		for _, pkt := range packets {
			pkt.Timestamp = videoTS
			n := s.sentPackets.Add(1)
			if s.status.Fault == "rtp_drop" && s.status.DropEvery > 0 && n%uint64(s.status.DropEvery) == 0 {
				s.droppedPackets.Add(1)
				s.recordStage("receive", true)
				continue
			}
			if err := s.stream.WritePacketRTP(s.videoMedia, pkt); err != nil {
				return
			}
		}
		audioSeq++
		audioPkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    8,
				SequenceNumber: audioSeq,
				Timestamp:      audioTS,
				SSRC:           0x52545350,
			},
			Payload: audioPayload,
		}
		_ = s.stream.WritePacketRTP(s.audioMedia, audioPkt)

		videoTS += uint32(90000 / testpattern.FPS)
		audioTS += uint32(8000 / testpattern.FPS)

		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func buildRTSPPatternFrames() ([][][]byte, []byte, []byte, error) {
	total := testpattern.Len()
	if total == 0 {
		return nil, nil, nil, fmt.Errorf("testpattern 帧数据为空")
	}
	frames := make([][][]byte, 0, total)
	var sps, pps []byte
	for i := 0; i < total; i++ {
		nalus := splitAnnexBNALUs(testpattern.AccessUnit(i))
		filtered := make([][]byte, 0, len(nalus))
		for _, nalu := range nalus {
			if len(nalu) == 0 {
				continue
			}
			nt := nalu[0] & 0x1f
			if nt == 9 {
				continue
			}
			if nt == 7 && len(sps) == 0 {
				sps = append([]byte(nil), nalu...)
			}
			if nt == 8 && len(pps) == 0 {
				pps = append([]byte(nil), nalu...)
			}
			filtered = append(filtered, nalu)
		}
		if len(filtered) > 0 {
			frames = append(frames, filtered)
		}
	}
	if len(frames) == 0 || len(sps) == 0 || len(pps) == 0 {
		return nil, nil, nil, fmt.Errorf("无法从 testpattern 提取 H.264 SPS/PPS")
	}
	return frames, sps, pps, nil
}

func splitAnnexBNALUs(data []byte) [][]byte {
	var out [][]byte
	start := -1
	for i := 0; i < len(data); {
		scLen := 0
		if i+3 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			scLen = 4
		} else if i+2 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			scLen = 3
		}
		if scLen > 0 {
			if start >= 0 && i > start {
				out = append(out, data[start:i])
			}
			i += scLen
			start = i
			continue
		}
		i++
	}
	if start >= 0 && start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

func listenRTSPUDP(ip net.IP) (*net.UDPConn, *net.UDPConn, error) {
	for range 32 {
		first, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
		if err != nil {
			return nil, nil, err
		}
		port := first.LocalAddr().(*net.UDPAddr).Port
		other := port + 1
		if port%2 != 0 {
			other = port - 1
		}
		second, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: other})
		if err != nil {
			_ = first.Close()
			continue
		}
		if port%2 == 0 {
			return first, second, nil
		}
		return second, first, nil
	}
	return nil, nil, fmt.Errorf("无法分配连续的 RTSP RTP 和 RTCP 端口")
}
