package gb28181

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

var (
	ErrPlatformConfig = errors.New("平台配置无效")
	ErrPlatformPeer   = errors.New("设备尚未注册或注册已过期")
)

type PlatformConfig struct {
	SIPTransport string `json:"sip_transport,omitempty"`
	IP           string `json:"ip"`
	Port         int    `json:"port"`
	PlatformID   string `json:"platform_id"`
	DeviceID     string `json:"device_id"`
}

type PreviewConfig struct {
	MediaTransport mediaTransport `json:"media_transport,omitempty"`
	SkipConnect    bool           `json:"skip_connect,omitempty"`
	ChannelID      string         `json:"channel_id"`
	WindowMS       int            `json:"window_ms"`
	SkipACK        bool           `json:"skip_ack"`
}

type PlatformMedia struct {
	Packets      int `json:"packets"`
	PSUnits      int `json:"ps_units"`
	PartialUnits int `json:"partial_units"`
	VideoUnits   int `json:"video_units"`
	Keyframes    int `json:"keyframes"`
}

type PlatformPreview struct {
	MediaTransport mediaTransport `json:"media_transport"`
	ChannelID      string         `json:"channel_id"`
	CallID         string         `json:"call_id"`
	SSRC           uint32         `json:"ssrc"`
	State          string         `json:"state"`
	SIPCode        int            `json:"sip_code,omitempty"`
	Start          time.Time      `json:"start"`
	WindowMS       int            `json:"window_ms"`
	RTPAddress     string         `json:"rtp_address"`
	Media          PlatformMedia  `json:"media"`
	Timeline       []SessionMark  `json:"timeline"`
	Error          string         `json:"error,omitempty"`
}

type PlatformStatus struct {
	SIPTransport  string           `json:"sip_transport,omitempty"`
	Busy          bool             `json:"busy"`
	Active        bool             `json:"active"`
	Address       string           `json:"address,omitempty"`
	PlatformID    string           `json:"platform_id,omitempty"`
	DeviceID      string           `json:"device_id,omitempty"`
	Peer          string           `json:"peer,omitempty"`
	Preview       *PlatformPreview `json:"preview,omitempty"`
	DeviceSession *SessionReport   `json:"device_session,omitempty"`
}

type PlatformManager struct {
	mu     sync.Mutex
	run    *platform
	closed bool
}

type platform struct {
	cfg     PlatformConfig
	local   netip.AddrPort
	client  *sipgo.Client
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	peer    netip.AddrPort
	expires time.Time
	preview *PlatformPreview
	busy    bool
	workers sync.WaitGroup
}

func (m *PlatformManager) Start(cfg PlatformConfig) error {
	cfg.IP, cfg.PlatformID, cfg.DeviceID = strings.TrimSpace(cfg.IP), strings.TrimSpace(cfg.PlatformID), strings.TrimSpace(cfg.DeviceID)
	cfg.SIPTransport = strings.ToLower(strings.TrimSpace(cfg.SIPTransport))
	if cfg.SIPTransport == "" {
		cfg.SIPTransport = "udp"
	}
	if cfg.SIPTransport != "udp" && cfg.SIPTransport != "tcp" {
		return fmt.Errorf("%w，SIP 传输需为 UDP 或 TCP", ErrPlatformConfig)
	}
	if cfg.IP == "" {
		cfg.IP = "127.0.0.1"
	}
	if !unicastIP(net.ParseIP(cfg.IP)) || cfg.Port < 0 || cfg.Port > 65535 ||
		!digits20.MatchString(cfg.PlatformID) || !digits20.MatchString(cfg.DeviceID) || cfg.PlatformID == cfg.DeviceID {
		return fmt.Errorf("%w，填写本机 IPv4、端口及不同的 20 位平台和设备编号", ErrPlatformConfig)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.run != nil && m.run.ctx.Err() == nil {
		return ErrActive
	}
	if m.run != nil {
		select {
		case <-m.run.done:
		default:
			return ErrActive
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &platform{cfg: cfg, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	listener, addr, err := p.listen()
	if err != nil {
		cancel()
		return fmt.Errorf("无法监听平台 SIP %s 端口：%w", strings.ToUpper(cfg.SIPTransport), err)
	}
	p.local = addr
	var opts []sipgo.UserAgentOption
	if cfg.SIPTransport == "tcp" {
		opts = append(opts, sipgo.WithUserAgentTransactionLayerOptions(sip.WithTransactionLayerTerminateOnConnClose()),
			sipgo.WithUserAgentTransportLayerOptions(sip.WithTransportLayerTransports(sip.TransportsConfig{TCP: &sip.TransportTCP{
				WriteTimeout: 3 * time.Second,
				DialerCreate: func(net.Addr) net.Dialer { return net.Dialer{Control: refuseSIPRedial} },
			}})))
	}
	ua, err := sipgo.NewUA(opts...)
	if err != nil {
		cancel()
		listener.Close()
		return err
	}
	p.client, err = sipgo.NewClient(ua, sipgo.WithClientAddr(p.local.String()), sipgo.WithClientConnectionAddr(p.local.String()), sipgo.WithClientNAT())
	if err != nil {
		cancel()
		listener.Close()
		ua.Close()
		return err
	}
	server, err := sipgo.NewServer(ua)
	if err != nil {
		cancel()
		listener.Close()
		ua.Close()
		return err
	}
	server.OnRegister(p.register)
	server.OnMessage(p.peerRequest)
	server.OnBye(p.peerRequest)
	server.OnOptions(p.peerRequest)
	m.run = p
	served := make(chan struct{})
	go func() {
		_ = listener.serve(server)
		close(served)
		p.cancel()
	}()
	go func() {
		defer close(p.done)
		<-p.ctx.Done()
		p.mu.Lock()
		p.mu.Unlock()
		p.workers.Wait()
		listener.Close()
		<-served
		ua.Close()
	}()
	return nil
}

func (p *platform) forget(peer netip.AddrPort) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.peer == peer {
		p.peer, p.expires = netip.AddrPort{}, time.Time{}
	}
}

func (p *platform) register(req *sip.Request, tx sip.ServerTransaction) {
	if req.To() == nil || req.To().Address.User != p.cfg.DeviceID {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 404, "Not Found", nil))
		return
	}
	peer, err := netip.ParseAddrPort(req.Source())
	if err != nil {
		return
	}
	expires := defaultExpires
	raw := ""
	if h := req.GetHeader("Expires"); h != nil {
		raw = h.Value()
	}
	if contact := req.Contact(); contact != nil {
		if value, ok := contact.Params.Get("expires"); ok {
			raw = value
		}
	}
	if raw != "" {
		expires, err = strconv.Atoi(raw)
		if err != nil || expires < 0 {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 400, "Bad Request", nil))
			return
		}
	}
	expires = min(expires, defaultExpires)
	p.mu.Lock()
	if p.peer.IsValid() && time.Now().Before(p.expires) && p.peer != peer {
		p.mu.Unlock()
		_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil))
		return
	}
	p.peer, p.expires = peer, time.Now().Add(time.Duration(expires)*time.Second)
	p.mu.Unlock()
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	res.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	if contact := req.Contact(); contact != nil {
		res.AppendHeader(sip.HeaderClone(contact))
	}
	_ = tx.Respond(res)
}

func (p *platform) peerRequest(req *sip.Request, tx sip.ServerTransaction) {
	p.mu.Lock()
	valid := p.peer.String() == req.Source() && time.Now().Before(p.expires)
	p.mu.Unlock()
	if !valid {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil))
		return
	}
	_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
}

func (m *PlatformManager) Status() PlatformStatus {
	m.mu.Lock()
	p := m.run
	m.mu.Unlock()
	if p == nil {
		return PlatformStatus{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	result := PlatformStatus{SIPTransport: p.cfg.SIPTransport, Busy: p.busy, Active: p.ctx.Err() == nil, Address: p.local.String(), PlatformID: p.cfg.PlatformID, DeviceID: p.cfg.DeviceID}
	if result.Active && time.Now().Before(p.expires) {
		result.Peer = p.peer.String()
	}
	if p.preview != nil {
		preview := *p.preview
		preview.Timeline = slices.Clone(preview.Timeline)
		result.Preview = &preview
	}
	return result
}

func (m *PlatformManager) stop(closeManager bool) {
	m.mu.Lock()
	if closeManager {
		m.closed = true
	}
	p := m.run
	if p != nil {
		p.cancel()
	}
	m.mu.Unlock()
	if closeManager && p != nil {
		<-p.done
	}
}

func (m *PlatformManager) Stop()  { m.stop(false) }
func (m *PlatformManager) Close() { m.stop(true) }
