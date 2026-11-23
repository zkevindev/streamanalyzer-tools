package whep

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"streamanalyzer/internal/simulator/testpattern"

	"github.com/pion/ice/v4"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

const h264Params = "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"
const signalWindow = 18 * time.Second
const mediaStopAfter = 2500 * time.Millisecond

type SessionStatus struct {
	ICE               string    `json:"ice"`
	DTLS              string    `json:"dtls"`
	Peer              string    `json:"peer"`
	ConnectedAt       time.Time `json:"connected_at,omitzero"`
	FirstRTPAt        time.Time `json:"first_rtp_at,omitzero"`
	LastRTPAt         time.Time `json:"last_rtp_at,omitzero"`
	RTPPackets        uint64    `json:"rtp_packets"`
	RTPPacketsDropped uint64    `json:"rtp_packets_dropped,omitempty"`
	MediaStoppedAt    time.Time `json:"media_stopped_at,omitzero"`
	ICEPacketsDropped uint64    `json:"ice_packets_dropped,omitempty"`
}

type session struct {
	mode       string
	pc         *webrtc.PeerConnection
	track      *webrtc.TrackLocalStaticRTP
	sender     *webrtc.RTPSender
	closeWire  func() error
	ctx        context.Context
	cancel     context.CancelFunc
	connected  chan struct{}
	done       chan struct{}
	mu         sync.Mutex
	status     SessionStatus
	dropped    atomic.Uint64
	rtpDropped atomic.Uint64
}

func newSession(mode string) *session {
	ctx, cancel := context.WithCancel(context.Background())
	return &session{mode: mode, ctx: ctx, cancel: cancel, connected: make(chan struct{}), done: make(chan struct{}),
		status: SessionStatus{ICE: "new", DTLS: "new", Peer: "new"}}
}

func (p *session) snapshot() SessionStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	status := p.status
	status.ICEPacketsDropped = p.dropped.Load()
	status.RTPPacketsDropped = p.rtpDropped.Load()
	return status
}

func (p *session) close() {
	p.cancel()
	if p.pc != nil {
		_ = p.pc.Close()
	}
	if p.closeWire != nil {
		_ = p.closeWire()
	}
}

type silentPacketConn struct {
	net.PacketConn
	dropped *atomic.Uint64
}

func (c silentPacketConn) ReadFrom(data []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(data)
		if err != nil {
			return n, addr, err
		}
		c.dropped.Add(1)
	}
}

func validOffer(raw string) bool {
	var description sdp.SessionDescription
	if description.UnmarshalString(raw) != nil || len(description.MediaDescriptions) != 1 {
		return false
	}
	media := description.MediaDescriptions[0]
	_, wantsVideo := media.Attribute("recvonly")
	return wantsVideo && media.MediaName.Media == "video" && media.MediaName.Port.Value != 0
}

func (p *session) negotiate(requestCtx context.Context, offer string) (string, error) {
	if !validOffer(offer) {
		return "", ErrOffer
	}
	if p.ctx.Err() != nil || requestCtx.Err() != nil {
		return "", ErrMissing
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return "", ErrNegotiation
	}
	var wire net.PacketConn = conn
	if p.mode == "ice_timeout" {
		wire = silentPacketConn{PacketConn: conn, dropped: &p.dropped}
	}
	mux := webrtc.NewICEUDPMux(nil, wire)
	p.closeWire = mux.Close
	var settings webrtc.SettingEngine
	settings.SetICEUDPMux(mux)
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	settings.SetLite(true)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	settings.SetIncludeLoopbackCandidate(true)
	settings.SetIPFilter(net.IP.IsLoopback)
	var media webrtc.MediaEngine
	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: h264Params}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: codec, PayloadType: 102}, webrtc.RTPCodecTypeVideo); err != nil {
		return "", ErrNegotiation
	}
	api := webrtc.NewAPI(webrtc.WithSettingEngine(settings), webrtc.WithMediaEngine(&media))
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return "", ErrNegotiation
	}
	p.pc = pc
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		p.mu.Lock()
		p.status.ICE = state.String()
		p.mu.Unlock()
	})
	pc.OnConnectionStateChange(p.connectionState)
	if pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}) != nil {
		return "", ErrOffer
	}
	p.track, err = webrtc.NewTrackLocalStaticRTP(codec, "video", "streamanalyzer")
	if err != nil {
		return "", ErrNegotiation
	}
	p.sender, err = pc.AddTrack(p.track)
	if err != nil {
		return "", ErrOffer
	}
	transport := p.sender.Transport()
	transport.OnStateChange(func(state webrtc.DTLSTransportState) {
		p.mu.Lock()
		p.status.DTLS = state.String()
		p.mu.Unlock()
	})
	answer, err := pc.CreateAnswer(nil)
	if err != nil || !strings.Contains(answer.SDP, "a=sendonly") || !strings.Contains(answer.SDP, "H264/90000") {
		return "", ErrOffer
	}
	complete := webrtc.GatheringCompletePromise(pc)
	if pc.SetLocalDescription(answer) != nil {
		return "", ErrNegotiation
	}
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	select {
	case <-complete:
		local := pc.LocalDescription()
		if local == nil || !strings.Contains(local.SDP, "a=candidate:") {
			return "", ErrNegotiation
		}
		if p.ctx.Err() != nil || requestCtx.Err() != nil {
			return "", ErrMissing
		}
		return local.SDP, nil
	case <-p.ctx.Done():
		return "", ErrMissing
	case <-requestCtx.Done():
		return "", ErrMissing
	case <-timer.C:
		return "", ErrNegotiation
	}
}

func (p *session) connectionState(state webrtc.PeerConnectionState) {
	p.mu.Lock()
	p.status.Peer = state.String()
	if state == webrtc.PeerConnectionStateConnected && p.status.ConnectedAt.IsZero() {
		p.status.ConnectedAt = time.Now()
		close(p.connected)
	}
	p.mu.Unlock()
	if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
		p.cancel()
	}
}

func (p *session) run() {
	timer := time.NewTimer(signalWindow)
	defer timer.Stop()
	select {
	case <-p.ctx.Done():
		return
	case <-timer.C:
		return
	case <-p.connected:
	}
	if p.mode == "no_media" {
		<-p.ctx.Done()
		return
	}
	rtcpDone := make(chan struct{})
	go func() {
		defer close(rtcpDone)
		buf := make([]byte, 2048)
		for {
			if _, _, err := p.sender.Read(buf); err != nil {
				return
			}
		}
	}()
	defer func() {
		_ = p.sender.Stop()
		<-rtcpDone
	}()
	p.sendVideo()
}

func (p *session) sendVideo() {
	packetizer := rtp.NewPacketizer(1200, 102, 0, &codecs.H264Payloader{}, rtp.NewRandomSequencer(), 90000)
	const interval = time.Second / testpattern.FPS
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	begin := time.Now()
	var pktSeq uint64
	for frame := 0; ; frame++ {
		if p.mode == "media_stop" && time.Since(begin) >= mediaStopAfter {
			p.mu.Lock()
			p.status.MediaStoppedAt = time.Now()
			p.mu.Unlock()
			<-p.ctx.Done()
			return
		}
		packets := packetizer.Packetize(testpattern.BaselineAccessUnit(frame%testpattern.BaselineLen()), 90000/testpattern.FPS)
		var count uint64
		for _, packet := range packets {
			if p.ctx.Err() != nil {
				return
			}
			pktSeq++
			if p.mode == "rtp_drop" && pktSeq > 1 && pktSeq%6 == 0 {
				p.rtpDropped.Add(1)
				continue
			}
			if p.track.WriteRTP(packet) == nil {
				count++
			}
		}
		if count > 0 {
			p.mu.Lock()
			now := time.Now()
			if p.status.FirstRTPAt.IsZero() {
				p.status.FirstRTPAt = now
			}
			p.status.LastRTPAt = now
			p.status.RTPPackets += count
			p.mu.Unlock()
		}
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
