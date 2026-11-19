package sipphone

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

var (
	ErrConfig   = errors.New("SIP 配置无效")
	ErrConflict = errors.New("当前 SIP 状态无法执行此操作")
	ErrClosed   = errors.New("SIP 模拟器服务已关闭")
	ErrNoAudio  = errors.New("暂无收到的 RTP 音频")
	userPattern = regexp.MustCompile(`^[a-zA-Z0-9_.+*!~%-]{1,96}$`)
	hostPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}$`)
)

type Config struct {
	Preset           string `json:"preset,omitempty"`
	PresetDelayMS    int    `json:"preset_delay_ms,omitempty"`
	PresetDurationMS int    `json:"preset_duration_ms,omitempty"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	User             string `json:"user"`
	Domain           string `json:"domain"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	LocalIP          string `json:"local_ip"`
	LocalPort        int    `json:"local_port"`
}

func isUnicast(ip netip.Addr) bool {
	return ip.Is4() && (ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}

func (c *Config) validate() error {
	c.Host, c.Domain, c.User = strings.TrimSpace(c.Host), strings.TrimSpace(c.Domain), strings.TrimSpace(c.User)
	c.Username, c.LocalIP = strings.TrimSpace(c.Username), strings.TrimSpace(c.LocalIP)
	if c.Port == 0 {
		c.Port = 5060
	}
	if c.Domain == "" {
		c.Domain = c.Host
	}
	if c.Username == "" {
		c.Username = c.User
	}
	if !hostPattern.MatchString(c.Host) || !hostPattern.MatchString(c.Domain) || c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("%w，服务器和 SIP 域需要 IPv4 地址或主机名", ErrConfig)
	}
	if !userPattern.MatchString(c.User) {
		return fmt.Errorf("%w，分机号需要 1–96 个 SIP 用户名字符", ErrConfig)
	}
	if len(c.Username) > 128 || len(c.Password) > 1024 || strings.ContainsAny(c.Username+c.Password, "\r\n\x00") {
		return fmt.Errorf("%w，Digest 凭据格式无效", ErrConfig)
	}
	ip, _ := netip.ParseAddr(c.LocalIP)
	if c.LocalPort < 0 || c.LocalPort > 65535 || c.LocalIP != "" && !isUnicast(ip) {
		return fmt.Errorf("%w，本机 SIP 地址需要单播 IPv4，端口范围为 0–65535", ErrConfig)
	}
	return c.validatePreset()
}

type Status struct {
	Preset       string      `json:"preset,omitempty"`
	Active       bool        `json:"active"`
	State        string      `json:"state"`
	User         string      `json:"user,omitempty"`
	Registrar    string      `json:"registrar,omitempty"`
	LocalAddress string      `json:"local_address,omitempty"`
	Error        string      `json:"error,omitempty"`
	Call         *CallStatus `json:"call,omitempty"`
	Events       []Event     `json:"events,omitempty"`
}

type Event struct {
	Time      time.Time `json:"time"`
	Direction string    `json:"direction"`
	Message   string    `json:"message"`
}

type Manager struct {
	mu     sync.Mutex
	agent  *agent
	closed bool
}

func (m *Manager) Start(cfg Config) (Status, error) {
	if err := cfg.validate(); err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Status{}, ErrClosed
	}
	if m.agent != nil && m.agent.running() {
		return Status{}, fmt.Errorf("%w，设备已启动", ErrConflict)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &agent{cfg: cfg, ctx: ctx, cancel: cancel, done: make(chan struct{}), registered: make(chan struct{}),
		status: Status{Active: true, State: "registering", Preset: cfg.Preset, User: cfg.User, Registrar: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))}}
	m.agent = a
	go a.run()
	return a.snapshot(), nil
}

func (m *Manager) current() *agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.agent
}

func (m *Manager) Status() Status {
	if a := m.current(); a != nil {
		return a.snapshot()
	}
	return Status{State: "idle"}
}

func (m *Manager) Stop() Status {
	if a := m.current(); a != nil {
		a.stop()
		return a.snapshot()
	}
	return Status{State: "idle"}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	a := m.agent
	m.mu.Unlock()
	if a != nil {
		a.stop()
		<-a.done
	}
}

func (m *Manager) Dial(target string) error {
	if a := m.current(); a != nil {
		return a.dial(target)
	}
	return ErrConflict
}

func (m *Manager) Answer() error {
	if a := m.current(); a != nil {
		return a.callAction(true)
	}
	return ErrConflict
}

func (m *Manager) Hangup() error {
	if a := m.current(); a != nil {
		return a.callAction(false)
	}
	return ErrConflict
}

func (m *Manager) SetAudioFault(direction string) error {
	var mode audioFault
	switch direction {
	case "off":
		mode = audioFaultOff
	case "send":
		mode = audioFaultSend
	case "receive":
		mode = audioFaultReceive
	default:
		return fmt.Errorf("%w，未知的 RTP 故障方向", ErrConfig)
	}
	a := m.current()
	if a == nil {
		return ErrConflict
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx.Err() != nil || a.call == nil || a.call.status.State != "incall" || a.call.status.Scenario.running() {
		return ErrConflict
	}
	a.call.audio.fault.Store(uint32(mode))
	return nil
}

func (m *Manager) ReceivedAudio() ([]byte, error) {
	if a := m.current(); a != nil {
		a.mu.Lock()
		c := a.call
		a.mu.Unlock()
		if c != nil {
			if wav := c.audio.wav(); wav != nil {
				return wav, nil
			}
		}
	}
	return nil, ErrNoAudio
}

type agent struct {
	cfg        Config
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	registered chan struct{} // The first successful REGISTER.
	workers    sync.WaitGroup
	mu         sync.Mutex
	status     Status
	call       *phoneCall

	client    *sipgo.Client
	dialogs   sipgo.DialogUA
	local     netip.AddrPort
	registrar netip.AddrPort
	// REGISTER transactions are serialized by the agent's run loop.
	registerID  sip.CallIDHeader
	registerTag string
	registerSeq uint32
}

func (a *agent) running() bool {
	select {
	case <-a.done:
		return false
	default:
		return true
	}
}

func (a *agent) stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status.Active && a.ctx.Err() == nil {
		a.status.State = "stopping"
		a.cancel()
	}
}

func (a *agent) snapshot() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	status := a.status
	status.Events = slices.Clone(status.Events)
	if a.call != nil {
		call := a.call.status
		call.SendRTP = a.call.audio.sent.snapshot()
		call.ReceiveRTP = a.call.audio.received.snapshot()
		call.PacketsSent = call.SendRTP.Packets
		call.PacketsReceived = call.ReceiveRTP.Packets
		call.AudioFault = audioFault(a.call.audio.fault.Load()).String()
		if call.Scenario != nil {
			report := *call.Scenario
			call.Scenario = &report
		}
		status.Call = &call
	}
	return status
}

func (a *agent) event(direction, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	const limit = 48
	if len(a.status.Events) == limit {
		copy(a.status.Events, a.status.Events[1:])
		a.status.Events = a.status.Events[:limit-1]
	}
	a.status.Events = append(a.status.Events, Event{time.Now(), direction, message})
}

func (a *agent) state(state string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx.Err() != nil {
		return
	}
	a.status.State, a.status.Error = state, ""
	if err != nil {
		a.status.Error = err.Error()
	}
}

func (a *agent) run() {
	defer close(a.done)
	defer a.cancel()
	err := a.serve()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status.Active = false
	if a.status.State == "stopping" {
		a.status.State, a.status.Error = "stopped", ""
	} else {
		a.status.State = "failed"
		if err != nil {
			a.status.Error = err.Error()
		}
	}
}
