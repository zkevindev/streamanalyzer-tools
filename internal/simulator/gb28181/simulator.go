package gb28181

import (
	"errors"
	"net"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo/sip"
)

var (
	ErrActive = errors.New("模拟设备正在运行")
	ErrClosed = errors.New("模拟器服务已关闭")
	digits20  = regexp.MustCompile(`^[0-9]{20}$`)
	hostName  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}$`)
	eventID   atomic.Uint64
)

type Config struct {
	Voice              bool         `json:"voice,omitempty"`
	PlatformHost       string       `json:"platform_host"`
	PlatformPort       int          `json:"platform_port"`
	SIPTransport       string       `json:"sip_transport,omitempty"`
	PlatformID         string       `json:"platform_id"`
	Domain             string       `json:"domain"`
	DeviceID           string       `json:"device_id"`
	DeviceCount        int          `json:"device_count,omitempty"`
	ChannelsPerDevice  int          `json:"channels_per_device,omitempty"`
	RegisterIntervalMS int          `json:"register_interval_ms,omitempty"`
	ChannelID          string       `json:"channel_id"`
	Username           string       `json:"username"`
	Password           string       `json:"password"`
	LocalIP            string       `json:"local_ip"`
	LocalPort          int          `json:"local_port"`
	Heartbeat          int          `json:"heartbeat_seconds"`
	Faults             *FaultConfig `json:"faults,omitempty"`
	CaptureRTP         bool         `json:"capture_rtp,omitempty"`
	RecordingStart     string       `json:"recording_start,omitempty"`
	AudioCodec         string       `json:"audio_codec,omitempty"`
}

func (c *Config) validate() error {
	c.PlatformHost = strings.TrimSpace(c.PlatformHost)
	c.PlatformID = strings.TrimSpace(c.PlatformID)
	c.DeviceID = strings.TrimSpace(c.DeviceID)
	c.ChannelID = strings.TrimSpace(c.ChannelID)
	c.Domain = strings.TrimSpace(c.Domain)
	c.LocalIP = strings.TrimSpace(c.LocalIP)
	c.Username = strings.TrimSpace(c.Username)
	c.RecordingStart = strings.TrimSpace(c.RecordingStart)
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
	channelID string
	toTag     string
	Packet    string `json:"packet,omitempty"`
}

type MediaStatus struct {
	MediaState     string         `json:"media_state"`
	MediaTransport mediaTransport `json:"media_transport,omitempty"`
	MediaLocal     string         `json:"media_local,omitempty"`
	MediaError     string         `json:"media_error,omitempty"`
	MediaTarget    string         `json:"media_target,omitempty"`
	RTPPackets     uint64         `json:"rtp_packets"`
	VideoFrames    uint64         `json:"video_frames"`
	AudioPackets   uint64         `json:"audio_packets"`
}

type Status struct {
	Broadcast            *BroadcastStatus `json:"broadcast,omitempty"`
	ChannelOnline        bool             `json:"channel_online"`
	CatalogSubscriptions int              `json:"catalog_subscriptions"`
	MediaStatus
	State        string          `json:"state"`
	Active       bool            `json:"active"`
	Platform     string          `json:"platform,omitempty"`
	SIPTransport string          `json:"sip_transport,omitempty"`
	DeviceID     string          `json:"device_id,omitempty"`
	ChannelID    string          `json:"channel_id,omitempty"`
	LocalAddress string          `json:"local_address,omitempty"`
	Error        string          `json:"error,omitempty"`
	Recording    *RecordingInfo  `json:"recording,omitempty"`
	MediaSource  SourceInfo      `json:"media_source"`
	Sessions     []SessionReport `json:"sessions,omitempty"`
	Events       []Event         `json:"events"`
	Devices      []DeviceSummary `json:"devices,omitempty"`
	Channels     []ChannelStatus `json:"channels,omitempty"`
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
	d.addSignalToEvidence(event)
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
	var callID, method, to string
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
		case "to", "t":
			to = strings.TrimSpace(value)
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
	event := Event{Direction: direction, Summary: truncate(lines[0], 240), Packet: truncate(packet.String(), 4096), CallID: callID, method: method}
	switch method {
	case "INVITE", "ACK", "BYE", "INFO":
		var uri sip.Uri
		params := sip.NewParams()
		if _, err := sip.ParseAddressValue(to, &uri, &params); err == nil {
			event.channelID = d.channelFor(uri.User)
			event.toTag, _ = params.Get("tag")
		}
		if direction == "in" && method == "INVITE" {
			if fields := strings.Fields(lines[0]); len(fields) == 3 && fields[0] == "INVITE" && sip.ParseUri(fields[1], &uri) == nil {
				event.channelID = d.channelFor(uri.User)
			}
		}
	}
	d.addEvent(event)
}

func truncate(s string, maxBytes int) string {
	if len(s) > maxBytes {
		return s[:maxBytes] + "…"
	}
	return s
}
