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
	DeviceIDs    []string `json:"device_ids,omitempty"`
	SIPTransport string   `json:"sip_transport,omitempty"`
	IP           string   `json:"ip"`
	Port         int      `json:"port"`
	PlatformID   string   `json:"platform_id"`
	DeviceID     string   `json:"device_id"`
}

type PreviewConfig struct {
	DeviceID       string `json:"device_id,omitempty"`
	watchMedia     bool
	Recording      *PlaybackRange `json:"recording,omitempty"`
	Recovery       string         `json:"recovery,omitempty"`
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
	Recording      *PlatformRecording `json:"recording,omitempty"`
	MediaTransport mediaTransport     `json:"media_transport"`
	ChannelID      string             `json:"channel_id"`
	CallID         string             `json:"call_id"`
	SSRC           uint32             `json:"ssrc"`
	State          string             `json:"state"`
	SIPCode        int                `json:"sip_code,omitempty"`
	Start          time.Time          `json:"start"`
	WindowMS       int                `json:"window_ms"`
	RTPAddress     string             `json:"rtp_address"`
	Media          PlatformMedia      `json:"media"`
	Timeline       []SessionMark      `json:"timeline"`
	Error          string             `json:"error,omitempty"`
}

type PlatformStatus struct {
	PlatformDeviceStatus
	Devices      []PlatformDeviceStatus `json:"devices,omitempty"`
	SIPTransport string                 `json:"sip_transport,omitempty"`
	Active       bool                   `json:"active"`
	Address      string                 `json:"address,omitempty"`
	PlatformID   string                 `json:"platform_id,omitempty"`
}

type PlatformDeviceStatus struct {
	DeviceID      string            `json:"device_id,omitempty"`
	Peer          string            `json:"peer,omitempty"`
	Busy          bool              `json:"busy"`
	Preview       *PlatformPreview  `json:"preview,omitempty"`
	Recovery      *PlatformRecovery `json:"recovery,omitempty"`
	DeviceSession *SessionReport    `json:"device_session,omitempty"`
}

type PlatformManager struct {
	mu     sync.Mutex
	run    *platform
	closed bool
}

type platform struct {
	// The listener and workers are shared; all device/dialog state is per peer.
	devices       map[string]*platform
	peers         map[netip.AddrPort]*platform
	registrations uint64
	playback      *platformPlayback
	interrupt     func(netip.AddrPort) bool
	recovery      *PlatformRecovery
	catalog       *recoveryCatalog
	ignoredSN     map[uint32]struct{}
	cfg           PlatformConfig
	local         netip.AddrPort
	client        *sipgo.Client
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	mu            *sync.Mutex
	peer          netip.AddrPort
	expires       time.Time
	preview       *PlatformPreview
	busy          bool
	workers       *sync.WaitGroup
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
		!digits20.MatchString(cfg.PlatformID) {
		return fmt.Errorf("%w，填写本机 IPv4、端口及不同的 20 位平台和设备编号", ErrPlatformConfig)
	}
	ids := slices.Clone(cfg.DeviceIDs)
	for i := range ids {
		ids[i] = strings.TrimSpace(ids[i])
	}
	if cfg.DeviceID != "" {
		if index := slices.Index(ids, cfg.DeviceID); index > 0 {
			ids = append([]string{cfg.DeviceID}, slices.Delete(ids, index, index+1)...)
		} else if index < 0 {
			ids = append([]string{cfg.DeviceID}, ids...)
		}
	}
	if len(ids) < 1 || len(ids) > maxDevices {
		return fmt.Errorf("%w，需要 1–%d 个设备编号", ErrPlatformConfig, maxDevices)
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] || !digits20.MatchString(id) || id == cfg.PlatformID {
			return fmt.Errorf("%w，设备编号必须为不同的 20 位数字", ErrPlatformConfig)
		}
		seen[id] = true
	}
	cfg.DeviceID, cfg.DeviceIDs = ids[0], ids
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
	p := &platform{cfg: cfg, ctx: ctx, cancel: cancel, done: make(chan struct{}), mu: &sync.Mutex{}, workers: &sync.WaitGroup{}, peers: make(map[netip.AddrPort]*platform)}
	listener, addr, err := p.listen()
	if err != nil {
		cancel()
		return fmt.Errorf("无法监听平台 SIP %s 端口：%w", strings.ToUpper(cfg.SIPTransport), err)
	}
	p.local, p.interrupt = addr, listener.interrupt
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
	p.devices = make(map[string]*platform, len(ids))
	p.devices[cfg.DeviceID] = p
	for _, id := range ids[1:] {
		child := cfg
		child.DeviceID = id
		p.devices[id] = &platform{cfg: child, local: p.local, client: p.client, ctx: p.ctx, interrupt: p.interrupt, mu: p.mu, workers: p.workers, peers: p.peers}
	}
	server.OnRegister(p.registerDevice)
	server.OnMessage(p.requestDevice)
	server.OnBye(p.requestDevice)
	server.OnOptions(p.requestDevice)
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

func (p *platform) device(id string) *platform {
	if id == "" {
		return p
	}
	return p.devices[id]
}

func (p *platform) forget(peer netip.AddrPort) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if device := p.peers[peer]; device != nil {
		device.peer, device.expires = netip.AddrPort{}, time.Time{}
		delete(p.peers, peer)
	}
}

func (p *platform) registerDevice(req *sip.Request, tx sip.ServerTransaction) {
	if to := req.To(); to != nil && to.Address.User != "" {
		if device := p.device(to.Address.User); device != nil {
			device.register(req, tx)
			return
		}
	}
	_ = tx.Respond(sip.NewResponseFromRequest(req, 404, "Not Found", nil))
}

func (p *platform) requestDevice(req *sip.Request, tx sip.ServerTransaction) {
	peer, _ := netip.ParseAddrPort(req.Source())
	p.mu.Lock()
	device := p.peers[peer]
	p.mu.Unlock()
	if device != nil {
		device.peerRequest(req, tx)
		return
	}
	_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil))
}

func (p *platform) register(req *sip.Request, tx sip.ServerTransaction) {
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
	owner := p.peers[peer]
	now := time.Now()
	if (owner != nil && owner != p && now.Before(owner.expires)) ||
		(p.peer.IsValid() && now.Before(p.expires) && p.peer != peer) {
		p.mu.Unlock()
		_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil))
		return
	}
	registeredAt := now
	if p.peers[p.peer] == p {
		delete(p.peers, p.peer)
	}
	p.peer, p.expires = netip.AddrPort{}, time.Time{}
	if expires > 0 {
		p.peer, p.expires = peer, registeredAt.Add(time.Duration(expires)*time.Second)
		p.peers[peer] = p
	}
	p.mu.Unlock()
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	res.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	if contact := req.Contact(); contact != nil {
		res.AppendHeader(sip.HeaderClone(contact))
	}
	if tx.Respond(res) == nil && expires > 0 {
		p.mu.Lock()
		p.registrations++
		if r := p.recovery; r != nil && r.Mode != recoveryMedia && r.State == "interrupted" && r.RegisteredAt.IsZero() &&
			!registeredAt.Before(r.InterruptedAt) && (r.Mode != recoveryUDP || r.IgnoredKeepalives >= 3) {
			r.RegisteredAt = time.Now()
			r.State = "registered"
		}
		p.mu.Unlock()
	}
}

func (p *platform) peerRequest(req *sip.Request, tx sip.ServerTransaction) {
	p.mu.Lock()
	valid := p.peer.String() == req.Source() && time.Now().Before(p.expires)
	var ignored bool
	var catalog *recoveryCatalog
	var heartbeat *PlatformRecovery
	if valid {
		ignored, catalog, heartbeat = p.recoveryPeerMessage(req)
		p.recordingPeerMessage(req)
	}
	p.mu.Unlock()
	if !valid {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil))
		return
	}
	if ignored {
		return
	}
	if tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)) != nil {
		return
	}
	if heartbeat != nil {
		p.mu.Lock()
		if heartbeat.State == "waiting_media" {
			heartbeat.KeepaliveAt = time.Now()
		}
		p.mu.Unlock()
	}
	if catalog != nil {
		select {
		case catalog.done <- struct{}{}:
		default:
		}
	}
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
	result := PlatformStatus{SIPTransport: p.cfg.SIPTransport, Active: p.ctx.Err() == nil, Address: p.local.String(), PlatformID: p.cfg.PlatformID}
	now := time.Now()
	for _, id := range p.cfg.DeviceIDs {
		result.Devices = append(result.Devices, p.devices[id].snapshotDevice(now, result.Active))
	}
	result.PlatformDeviceStatus = result.Devices[0]
	return result
}

func (p *platform) snapshotDevice(now time.Time, active bool) PlatformDeviceStatus {
	device := PlatformDeviceStatus{DeviceID: p.cfg.DeviceID, Busy: p.busy}
	if active && now.Before(p.expires) {
		device.Peer = p.peer.String()
	}
	if p.preview != nil {
		device.Preview = clonePlatformPreview(p.preview)
	}
	if p.recovery != nil {
		device.Recovery = p.recovery.snapshot()
	}
	return device
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
