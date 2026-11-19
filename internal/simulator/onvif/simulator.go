package onvif

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	devicePath   = "/onvif/device_service"
	mediaPath    = "/onvif/media_service"
	ptzPath      = "/onvif/ptz_service"
	snapshotPath = "/onvif/snapshot/"
)

var (
	ErrActive = errors.New("ONVIF 模拟器正在运行")
	ErrClosed = errors.New("ONVIF 模拟器服务已关闭")
	ErrConfig = errors.New("ONVIF 配置无效")
)

type Config struct {
	UDPSourceFaultMS       int    `json:"udp_source_fault_ms,omitempty"`
	RecordingGapSeconds    int    `json:"recording_gap_seconds,omitempty"`
	RTSPResolutionSwitchMS int    `json:"rtsp_resolution_switch_ms,omitempty"`
	RTCPClockStepMS        int    `json:"rtcp_clock_step_ms,omitempty"`
	RecordingFixture       bool   `json:"recording_fixture,omitempty"`
	RTSPDisconnectMS       int    `json:"rtsp_disconnect_ms,omitempty"`
	VideoGapMS             int    `json:"video_gap_ms,omitempty"`
	SOAPUsername           string `json:"soap_username,omitempty"`
	SOAPPassword           string `json:"soap_password,omitempty"`
	ClockOffset            int    `json:"clock_offset_seconds,omitempty"`
	SilentUDP              bool   `json:"silent_udp,omitempty"`
	RTSPUsername           string `json:"rtsp_username,omitempty"`
	RTSPPassword           string `json:"rtsp_password,omitempty"`
	BindIP                 string `json:"bind_ip"`
	Name                   string `json:"name"`
	HTTPPort               int    `json:"http_port"`
	RTSPPort               int    `json:"rtsp_port"`
}

type Address struct {
	IP        string `json:"ip"`
	Interface string `json:"interface"`
}

func Addresses() ([]Address, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	result := []Address{}
	for _, nic := range interfaces {
		if nic.Flags&net.FlagUp == 0 || nic.Flags&(net.FlagLoopback|net.FlagMulticast) == 0 {
			continue
		}
		addresses, err := nic.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addresses {
			ip, _, _ := strings.Cut(a.String(), "/")
			if parsed := net.ParseIP(ip); parsed.To4() != nil && (parsed.IsLoopback() || parsed.IsGlobalUnicast() || parsed.IsLinkLocalUnicast()) {
				result = append(result, Address{IP: ip, Interface: nic.Name})
			}
		}
	}
	return result, nil
}

func (c *Config) normalize() (net.IP, *net.Interface, error) {
	c.BindIP = strings.TrimSpace(c.BindIP)
	c.Name = strings.TrimSpace(c.Name)
	if c.BindIP == "" {
		c.BindIP = "127.0.0.1"
	}
	if c.Name == "" {
		c.Name = "Stream Analyzer Camera"
	}
	if !utf8.ValidString(c.Name) || utf8.RuneCountInString(c.Name) > 64 || strings.ContainsFunc(c.Name, func(r rune) bool { return r < 0x20 }) {
		return nil, nil, fmt.Errorf("%w：设备名称应为 1–64 个可显示字符", ErrConfig)
	}
	if c.HTTPPort < 0 || c.HTTPPort > 65535 || c.RTSPPort < 0 || c.RTSPPort > 65535 {
		return nil, nil, fmt.Errorf("%w：端口应为 0–65535", ErrConfig)
	}
	if c.HTTPPort != 0 && c.HTTPPort == c.RTSPPort {
		return nil, nil, fmt.Errorf("%w：ONVIF HTTP 与 RTSP 需要不同的 TCP 端口", ErrConfig)
	}
	c.SOAPUsername = strings.TrimSpace(c.SOAPUsername)
	if (c.SOAPUsername == "") != (c.SOAPPassword == "") || len(c.SOAPUsername) > 128 || len(c.SOAPPassword) > 256 ||
		strings.ContainsAny(c.SOAPUsername+c.SOAPPassword, "\r\n\x00") {
		return nil, nil, fmt.Errorf("%w，请同时填写有效的 SOAP UsernameToken 用户名和密码", ErrConfig)
	}
	if c.RTSPDisconnectMS != 0 && (c.RTSPDisconnectMS < 1000 || c.RTSPDisconnectMS > 10000 || c.VideoGapMS != 0) {
		return nil, nil, fmt.Errorf("%w，RTSP 断线窗口需为 1000–10000 毫秒并单独启用", ErrConfig)
	}
	if c.RTCPClockStepMS < -2000 || c.RTCPClockStepMS > 2000 || c.RTCPClockStepMS != 0 && (c.VideoGapMS != 0 || c.RTSPDisconnectMS != 0) {
		return nil, nil, fmt.Errorf("%w，RTCP 时钟跳变需在 ±2000 ms 内并单独启用", ErrConfig)
	}
	if c.UDPSourceFaultMS != 0 && (c.UDPSourceFaultMS < 1000 || c.UDPSourceFaultMS > 5000 ||
		c.SilentUDP || c.VideoGapMS != 0 || c.RTSPDisconnectMS != 0 || c.RTSPResolutionSwitchMS != 0 || c.RTCPClockStepMS != 0) {
		return nil, nil, fmt.Errorf("%w，UDP 来源端口切换需在 1000–5000 ms 内并单独启用", ErrConfig)
	}
	if c.RecordingGapSeconds != 0 && (!c.RecordingFixture || c.RecordingGapSeconds < 2 || c.RecordingGapSeconds > 60) {
		return nil, nil, fmt.Errorf("%w，录像缺口需为 2–60 秒并启用录像", ErrConfig)
	}
	if c.RTSPResolutionSwitchMS != 0 && (c.RTSPResolutionSwitchMS < 2000 || c.RTSPResolutionSwitchMS > 10000 ||
		c.RTSPDisconnectMS != 0 || c.VideoGapMS != 0 || c.RTCPClockStepMS != 0 || c.SilentUDP) {
		return nil, nil, fmt.Errorf("%w，分辨率切换需在 2000–10000 ms 内并单独启用", ErrConfig)
	}
	if c.VideoGapMS != 0 && (c.VideoGapMS < 1000 || c.VideoGapMS > 10000) {
		return nil, nil, fmt.Errorf("%w，视频停包时长需为 1000–10000 ms", ErrConfig)
	}
	if c.ClockOffset < -3600 || c.ClockOffset > 3600 {
		return nil, nil, fmt.Errorf("%w，设备时钟偏差需在 ±3600 秒以内", ErrConfig)
	}
	c.RTSPUsername = strings.TrimSpace(c.RTSPUsername)
	if (c.RTSPUsername == "") != (c.RTSPPassword == "") || len(c.RTSPUsername) > 128 || len(c.RTSPPassword) > 256 ||
		strings.ContainsAny(c.RTSPUsername+c.RTSPPassword, "\r\n\x00") {
		return nil, nil, fmt.Errorf("%w，请同时填写有效的 RTSP Digest 用户名和密码", ErrConfig)
	}
	ip := net.ParseIP(c.BindIP).To4()
	if ip == nil {
		return nil, nil, fmt.Errorf("%w：请选择本机 IPv4 地址", ErrConfig)
	}
	c.BindIP = ip.String()
	addresses, err := Addresses()
	if err != nil {
		return nil, nil, fmt.Errorf("读取本机网卡失败：%w", err)
	}
	for _, addr := range addresses {
		if addr.IP == c.BindIP {
			nic, err := net.InterfaceByName(addr.Interface)
			if err != nil {
				return nil, nil, fmt.Errorf("读取 ONVIF 发现网卡失败：%w", err)
			}
			return ip, nic, nil
		}
	}
	return nil, nil, fmt.Errorf("%w：所选 IPv4 地址未绑定到可用网卡", ErrConfig)
}

type Status struct {
	UDPSourceFault     *UDPSourceFaultStatus   `json:"udp_source_fault,omitempty"`
	ResolutionSwitchAt time.Time               `json:"resolution_switch_at,omitzero"`
	RTCPClockStepMS    int                     `json:"rtcp_clock_step_ms,omitempty"`
	Recording          *RecordingFixtureStatus `json:"recording,omitempty"`
	RTSPOutages        []RTSPOutageStatus      `json:"rtsp_outages,omitempty"`
	EventURL           string                  `json:"event_url,omitempty"`
	Events             EventStatus             `json:"events"`
	VideoGaps          []RTPGapStatus          `json:"video_gaps,omitempty"`
	SOAPDigest         bool                    `json:"soap_digest"`
	SOAPAuthRejected   uint64                  `json:"soap_auth_rejected"`
	SOAPClockRejected  uint64                  `json:"soap_clock_rejected"`
	ClockOffset        int                     `json:"clock_offset_seconds"`
	SilentUDP          bool                    `json:"silent_udp"`
	UDPPlays           uint64                  `json:"udp_plays"`
	TCPPlays           uint64                  `json:"tcp_plays"`
	Profiles           []Profile               `json:"profiles,omitempty"`
	RTSPDigest         bool                    `json:"rtsp_digest"`
	Active             bool                    `json:"active"`
	Name               string                  `json:"name,omitempty"`
	Interface          string                  `json:"interface,omitempty"`
	DiscoveryAddress   string                  `json:"discovery_address,omitempty"`
	EndpointID         string                  `json:"endpoint_id,omitempty"`
	DeviceURL          string                  `json:"device_url,omitempty"`
	PTZURL             string                  `json:"ptz_url,omitempty"`
	MediaURL           string                  `json:"media_url,omitempty"`
	StreamURL          string                  `json:"stream_url,omitempty"`
	DiscoveryReplies   uint64                  `json:"discovery_replies"`
	SOAPRequests       uint64                  `json:"soap_requests"`
	RTSPPlays          uint64                  `json:"rtsp_plays"`
}

type camera struct {
	recording     *recordingFixture
	events        eventBroker
	auth          soapAuth
	authRejected  atomic.Uint64
	clockRejected atomic.Uint64
	ptzMu         sync.Mutex
	profiles      []*profileVideo
	status        Status
	listener      net.Listener
	http          *http.Server
	discovery     *discoveryServer
	video         *videoServer
	cancel        context.CancelFunc
	workers       sync.WaitGroup

	discoveryReplies atomic.Uint64
	soapRequests     atomic.Uint64
}

type Manager struct {
	prepare               sync.Mutex
	mu                    sync.Mutex
	camera                *camera
	last                  Status
	lastDiscoveryInstance uint32
	closed                bool
}

func (m *Manager) Start(cfg Config) (Status, error) {
	return m.StartFiles(context.Background(), cfg, [2]LocalVideo{})
}

func (m *Manager) StartFiles(ctx context.Context, cfg Config, files [2]LocalVideo) (Status, error) {
	if !m.prepare.TryLock() {
		return Status{}, ErrActive
	}
	defer m.prepare.Unlock()
	ip, nic, err := cfg.normalize()
	if err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	active, closed := m.camera != nil, m.closed
	m.mu.Unlock()
	switch {
	case closed:
		return Status{}, ErrClosed
	case active:
		return Status{}, ErrActive
	}
	profiles, err := prepareProfiles(ctx, files)
	if err != nil {
		return Status{}, err
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Status{}, ErrClosed
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort(cfg.BindIP, strconv.Itoa(cfg.HTTPPort)))
	if err != nil {
		return Status{}, fmt.Errorf("无法监听 ONVIF HTTP 端口：%w", err)
	}
	discovery, err := openDiscovery(ip, nic)
	if err != nil {
		listener.Close()
		return Status{}, fmt.Errorf("无法监听 WS-Discovery：%w", err)
	}
	c := &camera{listener: listener, discovery: discovery, profiles: profiles}
	origin := "http://" + listener.Addr().String()
	if cfg.RecordingFixture {
		c.recording = newRecordingFixture(cfg.ClockOffset, cfg.RecordingGapSeconds)
		info := &c.recording.info
		info.RecordingURL, info.SearchURL, info.ReplayURL = origin+recordingPath, origin+searchPath, origin+replayPath
	}
	video, err := startVideo(ip, cfg, profiles, c.recording)
	if err != nil {
		discovery.close()
		listener.Close()
		return Status{}, fmt.Errorf("无法启动 RTSP：%w", err)
	}
	c.video = video
	if cfg.SOAPUsername != "" {
		c.auth = soapAuth{username: cfg.SOAPUsername, password: cfg.SOAPPassword, nonces: make(map[string]time.Time)}
	}
	discovery.instance = max(uint32(time.Now().Unix()), m.lastDiscoveryInstance+1)
	m.lastDiscoveryInstance = discovery.instance
	c.status = Status{
		RTCPClockStepMS: cfg.RTCPClockStepMS, Active: true, SOAPDigest: cfg.SOAPUsername != "", ClockOffset: cfg.ClockOffset, SilentUDP: cfg.SilentUDP, Name: cfg.Name, Interface: nic.Name, DiscoveryAddress: discovery.group.String(),
		EndpointID: "urn:uuid:" + uuid.NewSHA1(uuid.NameSpaceURL, []byte("streamanalyzer:onvif:"+cfg.BindIP+"/"+cfg.Name)).String(),
		DeviceURL:  origin + devicePath, MediaURL: origin + mediaPath, PTZURL: origin + ptzPath, EventURL: origin + eventPath, RTSPDigest: cfg.RTSPUsername != "",
	}
	for _, profile := range profiles {
		profile.SnapshotURL = origin + snapshotPath + profile.Token
		profile.StreamURL = "rtsp://" + video.server.RTSPAddress + profile.path
		c.status.Profiles = append(c.status.Profiles, profile.Profile)
	}
	c.status.StreamURL = profiles[0].StreamURL
	mux := http.NewServeMux()
	mux.HandleFunc(devicePath, c.serveSOAP(deviceNS, c.deviceReply))
	mux.HandleFunc(mediaPath, c.serveSOAP(mediaNS, c.mediaReply))
	mux.HandleFunc(ptzPath, c.serveSOAP(ptzNS, c.ptzReply))
	mux.HandleFunc(eventPath, c.serveSOAPRequest(eventNS, c.eventReply))
	mux.HandleFunc(pullPath+"{subscription}", c.serveSOAPRequest(eventNS, c.eventReply))
	if c.recording != nil {
		mux.HandleFunc(recordingPath, c.serveSOAP(recordingNS, c.recordingReply))
		mux.HandleFunc(searchPath, c.serveSOAP(searchNS, c.searchReply))
		mux.HandleFunc(replayPath, c.serveSOAP(replayNS, c.replayReply))
	}
	mux.HandleFunc("GET "+snapshotPath+"{profile}", c.serveSnapshot)
	c.http = &http.Server{
		Handler: mux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	runCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.workers.Go(func() { _ = c.http.Serve(c.listener) })
	c.workers.Go(func() { c.discovery.serve(c) })
	for _, track := range c.video.tracks {
		c.workers.Go(func() { track.play(runCtx) })
		if track.audio != nil {
			c.workers.Go(func() { track.playAudio(runCtx) })
		}
	}
	m.camera = c
	return c.snapshot(), nil
}

func (c *camera) snapshot() Status {
	status := c.status
	status.Events = c.events.snapshot()
	if c.video.sourceFault != nil {
		fault := c.video.sourceFault.snapshot()
		status.UDPSourceFault = &fault
	}
	if c.recording != nil {
		status.Recording = c.recording.snapshot()
	}
	status.DiscoveryReplies = c.discoveryReplies.Load()
	status.SOAPRequests = c.soapRequests.Load()
	status.SOAPAuthRejected, status.SOAPClockRejected = c.authRejected.Load(), c.clockRejected.Load()
	status.UDPPlays, status.TCPPlays = c.video.udpPlays.Load(), c.video.tcpPlays.Load()
	status.RTSPPlays = status.UDPPlays + status.TCPPlays
	for _, profile := range c.profiles {
		track := c.video.tracks[profile.path]
		if track.outage.duration > 0 {
			status.RTSPOutages = append(status.RTSPOutages, track.outage.snapshot(profile.Token))
		}
		if at := track.switchedAt.Load(); at != 0 {
			status.ResolutionSwitchAt = time.Unix(0, at)
		}
		if track.gap.duration > 0 {
			status.VideoGaps = append(status.VideoGaps, track.gap.snapshot(profile.Token))
		}
	}
	return status
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.camera != nil {
		return m.camera.snapshot()
	}
	return m.last
}

func (m *Manager) stop() {
	if c := m.camera; c != nil {
		c.cancel()
		// Close the prebound listener too, in case Stop runs before Serve.
		c.http.Close()
		c.listener.Close()
		c.discovery.close()
		c.workers.Wait()
		c.video.close()
		m.last = c.snapshot()
		m.last.Active = false
		m.last.Events.Subscriptions = 0
		m.camera = nil
	}
}

func (m *Manager) Stop() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stop()
	return m.last
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.stop()
}
