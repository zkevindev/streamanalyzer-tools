package gb28181

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxDevices           = 16
	maxChannelsPerDevice = 16
	maxConcurrentMedia   = 32
	maxFleetCaptureBytes = 64 << 20
)

type deviceSpec struct {
	config   Config
	channels []string
}

func deviceSpecs(cfg Config) ([]deviceSpec, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if cfg.DeviceCount == 0 {
		cfg.DeviceCount = 1
	}
	if cfg.ChannelsPerDevice == 0 {
		cfg.ChannelsPerDevice = 1
	}
	if cfg.DeviceCount < 1 || cfg.DeviceCount > maxDevices || cfg.ChannelsPerDevice < 1 || cfg.ChannelsPerDevice > maxChannelsPerDevice {
		return nil, fmt.Errorf("设备数和每台通道数应分别为 1–%d 和 1–%d", maxDevices, maxChannelsPerDevice)
	}
	if cfg.RegisterIntervalMS < 0 || cfg.RegisterIntervalMS > 5000 {
		return nil, errors.New("批量启动间隔应为 0–5000 毫秒")
	}
	if cfg.LocalPort != 0 && cfg.LocalPort+cfg.DeviceCount-1 > 65535 {
		return nil, errors.New("批量设备所需的连续 SIP 端口超出 65535")
	}
	used := map[string]bool{cfg.PlatformID: true}
	allocate := func(base string, offset int) (string, error) {
		// Keep the administrative and device-type prefix of the GB identifier.
		prefix, serial := base[:13], base[13:]
		value, _ := strconv.Atoi(serial)
		value += offset
		if value > 9999999 {
			return "", fmt.Errorf("编号 %s 的七位序号无法容纳批量配置", base)
		}
		id := fmt.Sprintf("%s%07d", prefix, value)
		if used[id] {
			return "", fmt.Errorf("批量生成的国标编号重复：%s，请调整起始编号", id)
		}
		used[id] = true
		return id, nil
	}
	specs := make([]deviceSpec, cfg.DeviceCount)
	for i := range specs {
		spec := &specs[i]
		spec.config = cfg
		offset := i * (cfg.ChannelsPerDevice + 1)
		id, err := allocate(cfg.DeviceID, offset)
		if err != nil {
			return nil, err
		}
		spec.config.DeviceID = id
		if cfg.Username == "" {
			spec.config.Username = id
		}
		if cfg.LocalPort != 0 {
			spec.config.LocalPort += i
		}
		spec.channels = make([]string, cfg.ChannelsPerDevice)
		for j := range spec.channels {
			channel, err := allocate(cfg.ChannelID, offset+j)
			if err != nil {
				return nil, err
			}
			spec.channels[j] = channel
		}
		spec.config.ChannelID = spec.channels[0]
	}
	return specs, nil
}

type fleet struct {
	devices      []*device
	done         chan struct{}
	mediaSlots   chan struct{}
	captureBytes atomic.Int64
}

func (f *fleet) running() bool {
	select {
	case <-f.done:
		return false
	default:
		return true
	}
}

func (f *fleet) stop() {
	for _, d := range f.devices {
		d.stop()
	}
}

type DeviceSummary struct {
	DeviceID     string `json:"device_id"`
	State        string `json:"state"`
	LocalAddress string `json:"local_address,omitempty"`
	Error        string `json:"error,omitempty"`
	ChannelCount int    `json:"channel_count"`
	ActiveMedia  int    `json:"active_media"`
}

type ChannelStatus struct {
	Online    bool   `json:"online"`
	ChannelID string `json:"channel_id"`
	MediaStatus
}

// Manager owns the device batch shared by the workbench and the HTTP API.
type Manager struct {
	mu        sync.Mutex
	fleet     *fleet
	preparing bool
	closed    bool
}

func (m *Manager) Start(cfg Config) error {
	specs, err := deviceSpecs(cfg)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.available(); err != nil {
		return err
	}
	m.start(specs, nil)
	return nil
}

func (m *Manager) StartFile(ctx context.Context, cfg Config, name string, input io.Reader) error {
	specs, err := deviceSpecs(cfg)
	if err != nil {
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

	media, err := prepareLocalMedia(ctx, name, specs[0].config.AudioCodec, input)
	if err != nil {
		return err
	}
	if err := media.setRecording(specs[0].config.RecordingStart, time.Now()); err != nil {
		media.remove()
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
	m.start(specs, media)
	return nil
}

// available and start are called with m.mu held.
func (m *Manager) available() error {
	if m.closed {
		return ErrClosed
	}
	if m.preparing || m.fleet != nil && m.fleet.running() {
		return ErrActive
	}
	return nil
}

func (m *Manager) start(specs []deviceSpec, file *localMedia) {
	f := &fleet{done: make(chan struct{}), mediaSlots: make(chan struct{}, maxConcurrentMedia)}
	source := SourceInfo{VideoMode: "builtin", VideoCodec: "h264", OutputCodec: "h264", AudioCodec: "none"}
	if file != nil {
		source = file.info
	}
	for i, spec := range specs {
		cfg := spec.config
		ctx, cancel := context.WithCancel(context.Background())
		state := "registering"
		if i > 0 && cfg.RegisterIntervalMS != 0 {
			state = "queued"
		}
		d := &device{
			cfg: cfg, ctx: ctx, cancel: cancel, done: make(chan struct{}), file: file, fleet: f,
			channels: spec.channels, offline: make(map[string]bool), media: make(map[string]*mediaSession), channelStatus: make(map[string]*MediaStatus, len(spec.channels)),
			status: Status{State: state, Platform: net.JoinHostPort(cfg.PlatformHost, strconv.Itoa(cfg.PlatformPort)),
				SIPTransport: cfg.SIPTransport, DeviceID: cfg.DeviceID, MediaSource: source},
		}
		if file != nil {
			d.status.Recording = file.recording
		}
		for _, id := range spec.channels {
			d.channelStatus[id] = &MediaStatus{MediaState: "idle"}
		}
		f.devices = append(f.devices, d)
	}
	m.fleet = f
	for i, d := range f.devices {
		go d.run(time.Duration(i*d.cfg.RegisterIntervalMS) * time.Millisecond)
	}
	go func() {
		for _, d := range f.devices {
			<-d.done
		}
		if file != nil {
			file.remove()
		}
		close(f.done)
	}()
}

func (m *Manager) currentFleet() *fleet {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fleet
}

func (m *Manager) Status() Status {
	result, _ := m.StatusFor("", "")
	return result
}

func (m *Manager) StatusFor(deviceID, channelID string) (Status, bool) {
	f := m.currentFleet()
	if f == nil {
		return Status{State: "idle", MediaStatus: MediaStatus{MediaState: "idle"}, Events: []Event{}}, deviceID == "" && channelID == ""
	}
	selected := f.devices[0]
	for _, d := range f.devices {
		if d.cfg.DeviceID == deviceID {
			selected = d
			break
		}
	}
	if deviceID != "" && selected.cfg.DeviceID != deviceID || channelID != "" && !selected.hasChannel(channelID) {
		return Status{}, false
	}
	if channelID == "" {
		channelID = selected.cfg.ChannelID
	}
	var result Status
	devices := make([]DeviceSummary, 0, len(f.devices))
	for _, d := range f.devices {
		d.mu.Lock()
		playing := 0
		for _, s := range d.media {
			if s.status.MediaState != "playback_done" {
				playing++
			}
		}
		devices = append(devices, DeviceSummary{DeviceID: d.cfg.DeviceID, State: d.status.State,
			LocalAddress: d.status.LocalAddress, Error: d.status.Error, ChannelCount: len(d.channels), ActiveMedia: playing})
		if d == selected {
			result = d.status
			result.ChannelID, result.MediaStatus = channelID, *d.channelStatus[channelID]
			result.ChannelOnline = !d.offline[channelID]
			if d.flow != nil && d.flow.ctx.Err() == nil {
				d.flow.pruneCatalog(time.Now())
				result.CatalogSubscriptions = len(d.flow.catalog)
			}
			result.Events = append([]Event{}, d.status.Events...)
			for _, id := range d.channels {
				result.Channels = append(result.Channels, ChannelStatus{ChannelID: id, Online: !d.offline[id], MediaStatus: *d.channelStatus[id]})
			}
			for i := len(d.evidence) - 1; i >= 0; i-- {
				if channelID == d.evidence[i].ChannelID {
					report := d.evidence[i].SessionReport
					report.Timeline = slices.Clone(report.Timeline)
					result.Sessions = append(result.Sessions, report)
				}
			}
		}
		d.mu.Unlock()
	}
	result.Active = f.running()
	result.Devices = devices
	return result, true
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f := m.fleet; f != nil && f.running() {
		f.stop()
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	f := m.fleet
	m.mu.Unlock()
	if f != nil {
		f.stop()
		<-f.done
	}
}

func (d *device) hasChannel(id string) bool {
	return slices.Contains(d.channels, id)
}

// The device ID is also accepted as an alias for its first channel.
func (d *device) channelFor(target string) string {
	if target == d.cfg.DeviceID {
		return d.cfg.ChannelID
	}
	if d.hasChannel(target) {
		return target
	}
	return ""
}
