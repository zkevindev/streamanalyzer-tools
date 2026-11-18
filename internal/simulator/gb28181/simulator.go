package gb28181

import (
	"context"
	"errors"
	"io"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrActive = errors.New("模拟设备正在运行")
	ErrClosed = errors.New("模拟器服务已关闭")
	digits20  = regexp.MustCompile(`^[0-9]{20}$`)
	hostName  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}$`)
	eventID   atomic.Uint64
)

type Config struct {
	PlatformHost string       `json:"platform_host"`
	PlatformPort int          `json:"platform_port"`
	SIPTransport string       `json:"sip_transport,omitempty"`
	PlatformID   string       `json:"platform_id"`
	Domain       string       `json:"domain"`
	DeviceID     string       `json:"device_id"`
	ChannelID    string       `json:"channel_id"`
	Username     string       `json:"username"`
	Password     string       `json:"password"`
	LocalIP      string       `json:"local_ip"`
	LocalPort    int          `json:"local_port"`
	Heartbeat    int          `json:"heartbeat_seconds"`
	Faults       *FaultConfig `json:"faults,omitempty"`
	CaptureRTP   bool         `json:"capture_rtp,omitempty"`
	AudioCodec   string       `json:"audio_codec,omitempty"`
}

func (c *Config) validate() error {
	c.PlatformHost = strings.TrimSpace(c.PlatformHost)
	c.PlatformID = strings.TrimSpace(c.PlatformID)
	c.DeviceID = strings.TrimSpace(c.DeviceID)
	c.ChannelID = strings.TrimSpace(c.ChannelID)
	c.Domain = strings.TrimSpace(c.Domain)
	c.LocalIP = strings.TrimSpace(c.LocalIP)
	c.Username = strings.TrimSpace(c.Username)
	c.SIPTransport = strings.ToLower(strings.TrimSpace(c.SIPTransport))
	switch c.SIPTransport {
	case "":
		c.SIPTransport = "udp"
	case "udp", "tcp":
	default:
		return errors.New("SIP 信令传输请选择 UDP 或 TCP")
	}
	if !hostName.MatchString(c.PlatformHost) || c.PlatformPort < 1 || c.PlatformPort > 65535 {
		return errors.New("请输入有效的平台 IPv4 地址或主机名及 SIP 端口")
	}
	if !digits20.MatchString(c.PlatformID) || !digits20.MatchString(c.DeviceID) || !digits20.MatchString(c.ChannelID) {
		return errors.New("平台、设备与通道国标编号均需为 20 位数字")
	}
	if c.PlatformID == c.DeviceID || c.PlatformID == c.ChannelID || c.DeviceID == c.ChannelID {
		return errors.New("平台、设备和通道编号应各不相同")
	}
	if c.Domain == "" {
		c.Domain = c.PlatformID[:10]
	}
	if !hostName.MatchString(c.Domain) {
		return errors.New("SIP 域应为数字域或有效的域名")
	}
	if c.LocalIP != "" && !unicastIP(net.ParseIP(c.LocalIP)) {
		return errors.New("本机 SIP 地址需为单播 IPv4 地址")
	}
	if c.LocalPort < 0 || c.LocalPort > 65535 {
		return errors.New("本机 SIP 端口应为 0–65535")
	}
	if c.Username == "" {
		c.Username = c.DeviceID
	}
	if len(c.Username) > 128 || strings.ContainsAny(c.Username, "\r\n\x00") || len(c.Password) > 1024 {
		return errors.New("SIP 鉴权用户名或密码格式无效")
	}
	if c.Faults != nil {
		faults := *c.Faults
		c.Faults = &faults
		if err := c.Faults.validate(); err != nil {
			return err
		}
	}
	if c.Heartbeat == 0 {
		c.Heartbeat = 60
	}
	if c.Heartbeat < 5 || c.Heartbeat > 300 {
		return errors.New("心跳间隔应为 5–300 秒")
	}
	return nil
}

func unicastIP(ip net.IP) bool {
	return ip.To4() != nil && (ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}

type Event struct {
	ID        uint64    `json:"id"`
	Time      time.Time `json:"time"`
	Direction string    `json:"direction"`
	Summary   string    `json:"summary"`
	CallID    string    `json:"call_id,omitempty"`
	method    string
	Packet    string `json:"packet,omitempty"`
}

type Status struct {
	State          string          `json:"state"`
	Active         bool            `json:"active"`
	Platform       string          `json:"platform,omitempty"`
	SIPTransport   string          `json:"sip_transport,omitempty"`
	DeviceID       string          `json:"device_id,omitempty"`
	ChannelID      string          `json:"channel_id,omitempty"`
	LocalAddress   string          `json:"local_address,omitempty"`
	Error          string          `json:"error,omitempty"`
	MediaState     string          `json:"media_state"`
	MediaTransport mediaTransport  `json:"media_transport,omitempty"`
	MediaLocal     string          `json:"media_local,omitempty"`
	MediaError     string          `json:"media_error,omitempty"`
	MediaTarget    string          `json:"media_target,omitempty"`
	MediaSource    SourceInfo      `json:"media_source"`
	RTPPackets     uint64          `json:"rtp_packets"`
	VideoFrames    uint64          `json:"video_frames"`
	AudioPackets   uint64          `json:"audio_packets"`
	Sessions       []SessionReport `json:"sessions,omitempty"`
	Events         []Event         `json:"events"`
}

// Manager owns the one simulator instance shared by the workbench and the HTTP API.
type Manager struct {
	mu        sync.Mutex
	device    *device
	preparing bool
	closed    bool
}

func (m *Manager) Start(cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.available(); err != nil {
		return err
	}
	m.start(cfg, nil)
	return nil
}

func (m *Manager) StartFile(ctx context.Context, cfg Config, name string, input io.Reader) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	if err := m.available(); err != nil {
		m.mu.Unlock()
		return err
	}
	m.preparing = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.preparing = false
		m.mu.Unlock()
	}()

	media, err := prepareLocalMedia(ctx, name, cfg.AudioCodec, input)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		media.remove()
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		media.remove()
		return err
	}
	m.start(cfg, media)
	return nil
}

// available and start are called with m.mu held.
func (m *Manager) available() error {
	if m.closed {
		return ErrClosed
	}
	if m.preparing || m.device != nil && m.device.running() {
		return ErrActive
	}
	return nil
}

func (m *Manager) start(cfg Config, media *localMedia) {
	ctx, cancel := context.WithCancel(context.Background())
	source := SourceInfo{VideoMode: "builtin", VideoCodec: "h264", OutputCodec: "h264", AudioCodec: "none"}
	if media != nil {
		source = media.info
	}
	d := &device{
		cfg: cfg, ctx: ctx, cancel: cancel, done: make(chan struct{}),
		file: media,
		status: Status{State: "registering", MediaState: "idle",
			Platform: net.JoinHostPort(cfg.PlatformHost, strconv.Itoa(cfg.PlatformPort)), SIPTransport: cfg.SIPTransport,
			DeviceID: cfg.DeviceID, ChannelID: cfg.ChannelID, MediaSource: source},
	}
	m.device = d
	go d.run()
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	d := m.device
	m.mu.Unlock()
	if d == nil {
		return Status{State: "idle", MediaState: "idle", Events: []Event{}}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	result := d.status
	result.Active = d.running()
	result.Events = slices.Clone(d.status.Events)
	for i := len(d.evidence) - 1; i >= 0; i-- {
		report := d.evidence[i].SessionReport
		report.Timeline = slices.Clone(report.Timeline)
		result.Sessions = append(result.Sessions, report)
	}
	if result.Events == nil {
		result.Events = []Event{}
	}
	return result
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.device; d != nil && d.running() {
		d.stop()
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	d := m.device
	m.mu.Unlock()
	if d != nil {
		d.stop()
		<-d.done
	}
}

func (d *device) running() bool {
	select {
	case <-d.done:
		return false
	default:
		return true
	}
}

func (d *device) stop() {
	d.mu.Lock()
	if d.ctx.Err() == nil && d.running() {
		if d.status.State != "failed" && d.status.State != "stopped" {
			d.status.State = "stopping"
		}
		d.cancel()
	}
	d.mu.Unlock()
}

func (d *device) state(state, detail string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctx.Err() != nil && state != "stopped" {
		return
	}
	d.status.State, d.status.Error = state, detail
}

func (d *device) log(direction, summary, packet string) {
	d.addEvent(Event{Direction: direction, Summary: summary, Packet: packet})
}

func (d *device) addEvent(event Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	const maxEvents = 100
	if len(d.status.Events) == maxEvents {
		copy(d.status.Events, d.status.Events[1:])
		d.status.Events = d.status.Events[:maxEvents-1]
	}
	event.ID, event.Time = eventID.Add(1), time.Now()
	d.status.Events = append(d.status.Events, event)
	for i := len(d.evidence) - 1; event.CallID != "" && i >= 0; i-- {
		if d.evidence[i].CallID == event.CallID {
			d.evidence[i].addSignal(event)
			break
		}
	}
}

// udpSIPWire records datagrams from the configured platform.
type udpSIPWire struct {
	*net.UDPConn
	device   *device
	platform net.IP
	ready    chan struct{}
	once     sync.Once
}

func (w *udpSIPWire) ReadFrom(p []byte) (int, net.Addr, error) {
	w.once.Do(func() { close(w.ready) })
	for {
		n, addr, err := w.ReadFromUDP(p)
		if err != nil {
			return n, addr, err
		}
		if addr.IP.Equal(w.platform) {
			w.device.logPacket("in", p[:n])
			return n, addr, nil
		}
	}
}

func (w *udpSIPWire) WriteTo(p []byte, addr net.Addr) (int, error) {
	n, err := w.UDPConn.WriteTo(p, addr)
	if err == nil {
		w.device.logPacket("out", p)
	}
	return n, err
}

func (d *device) logPacket(direction string, p []byte) {
	// Redact headers before applying the display limit, including folded values.
	head, body, _ := strings.Cut(strings.ReplaceAll(string(p), "\r\n", "\n"), "\n\n")
	lines := strings.Split(head, "\n")
	var packet strings.Builder
	var callID, method string
	redacting := false
	for i, line := range lines {
		if i > 0 && len(line) > 0 && (line[0] == ' ' || line[0] == '\t') && redacting {
			continue
		}
		name, value, _ := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		switch strings.ToLower(name) {
		case "call-id", "i":
			callID = truncate(strings.TrimSpace(value), 240)
		case "cseq":
			if fields := strings.Fields(value); len(fields) == 2 {
				method = fields[1]
			}
		}
		redacting = strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Proxy-Authorization")
		if redacting {
			line = name + ": [redacted]"
		}
		packet.WriteString(line)
		packet.WriteByte('\n')
	}
	if body != "" {
		packet.WriteByte('\n')
		packet.WriteString(body)
	}
	d.addEvent(Event{Direction: direction, Summary: truncate(lines[0], 240), Packet: truncate(packet.String(), 4096), CallID: callID, method: method})
}

func truncate(s string, maxBytes int) string {
	if len(s) > maxBytes {
		return s[:maxBytes] + "…"
	}
	return s
}
