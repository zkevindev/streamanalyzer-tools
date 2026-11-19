package onvifdiscovery

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type scanState struct {
	result    Discovery
	cancel    context.CancelFunc
	expires   time.Time
	addresses map[string][]string
}
type selection struct {
	device, media target
	profiles      map[string]bool
	offset        time.Duration
	expires       time.Time
}

type Manager struct {
	mu         sync.Mutex
	jobs       map[string]*scanState
	selections map[string]selection
	operations map[string]context.CancelFunc
	closed     bool
	workers    sync.WaitGroup
}

func (m *Manager) pruneLocked() {
	now := time.Now()
	for id, job := range m.jobs {
		if job.result.State != "running" && now.After(job.expires) {
			delete(m.jobs, id)
		}
	}
	for id, value := range m.selections {
		if now.After(value.expires) {
			delete(m.selections, id)
		}
	}
}

func (m *Manager) Start(req DiscoverRequest) (Discovery, error) {
	nic, err := bindInterface(req.BindIP)
	if err != nil {
		return Discovery{}, err
	}
	if req.Seconds == 0 {
		req.Seconds = 3
	}
	if req.Seconds < 1 || req.Seconds > 10 {
		return Discovery{}, invalid("发现时长须为 1 至 10 秒")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Discovery{}, &Error{Code: "closed", Message: "ONVIF 发现服务已关闭"}
	}
	if m.jobs == nil {
		m.jobs = make(map[string]*scanState)
	}
	m.pruneLocked()
	for _, job := range m.jobs {
		if job.result.State == "running" {
			return Discovery{}, &Error{Code: "busy", Message: "已有发现任务运行中，请先停止或等待完成"}
		}
	}
	if len(m.jobs) >= maxJobs {
		var oldest string
		var at time.Time
		for id, job := range m.jobs {
			if oldest == "" || job.result.StartedAt.Before(at) {
				oldest, at = id, job.result.StartedAt
			}
		}
		delete(m.jobs, oldest)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.Seconds)*time.Second)
	value := Discovery{ID: identifier(), State: "running", Interface: nic, Seconds: req.Seconds, StartedAt: time.Now().UTC(), Devices: []Device{}}
	state := &scanState{result: value, cancel: cancel, expires: time.Now().Add(stateTTL), addresses: make(map[string][]string)}
	m.jobs[value.ID] = state
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		defer cancel()
		err := m.discover(ctx, state)
		m.mu.Lock()
		defer m.mu.Unlock()
		if ctx.Err() == context.Canceled {
			state.result.State = "canceled"
		} else if err != nil {
			state.result.State = "failed"
			if known, ok := err.(*Error); ok {
				state.result.Error = known
			} else {
				state.result.Error = &Error{Code: "discovery_error", Message: "无法完成所选网卡的 WS-Discovery 搜索"}
			}
		} else {
			state.result.State = "complete"
		}
		state.expires = time.Now().Add(stateTTL)
	}()
	return value, nil
}

func copyDiscovery(value Discovery) Discovery {
	value.Devices = append([]Device{}, value.Devices...)
	for i := range value.Devices {
		value.Devices[i].Types = append([]string{}, value.Devices[i].Types...)
	}
	return value
}

func (m *Manager) Get(id string) (Discovery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	if value := m.jobs[id]; value != nil {
		return copyDiscovery(value.result), nil
	}
	return Discovery{}, &Error{Code: "not_found", Message: "发现任务已过期，请重新搜索"}
}
func (m *Manager) Stop(id string) (Discovery, error) {
	m.mu.Lock()
	value := m.jobs[id]
	if value != nil {
		value.cancel()
	}
	m.mu.Unlock()
	return m.Get(id)
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	for _, job := range m.jobs {
		job.cancel()
	}
	for _, cancel := range m.operations {
		cancel()
	}
	m.selections = nil
	m.mu.Unlock()
	m.workers.Wait()
}

func (m *Manager) operation(parent context.Context) (context.Context, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, nil, &Error{Code: "closed", Message: "ONVIF 发现服务已关闭"}
	}
	if len(m.operations) >= 4 {
		return nil, nil, &Error{Code: "busy", Message: "ONVIF 查询较多，请稍后重试"}
	}
	if m.operations == nil {
		m.operations = make(map[string]context.CancelFunc)
	}
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	id := identifier()
	m.operations[id] = cancel
	m.workers.Add(1)
	return ctx, func() { cancel(); m.mu.Lock(); delete(m.operations, id); m.mu.Unlock(); m.workers.Done() }, nil
}

func validateCredentials(value Credentials) error {
	if len(value.Username) > 128 || len(value.Password) > 256 || !utf8.ValidString(value.Username+value.Password) || strings.ContainsAny(value.Username+value.Password, "\r\n\x00") || (value.Username == "") != (value.Password == "") {
		return invalid("请同时填写有效的 ONVIF 用户名和密码，或将两项同时留空")
	}
	return nil
}

func (m *Manager) selectedTarget(ctx context.Context, req ProfilesRequest) (target, error) {
	if req.ServiceURL != "" {
		if req.DiscoveryID != "" || req.DeviceID != "" {
			return target{}, invalid("请选择发现的设备，或单独填写手动服务地址")
		}
		nic, err := bindInterface(req.BindIP)
		if err != nil {
			return target{}, err
		}
		return resolveTarget(ctx, req.ServiceURL, nic, nil)
	}
	m.mu.Lock()
	m.pruneLocked()
	job := m.jobs[req.DiscoveryID]
	var urls []string
	var peer net.IP
	var nic Interface
	if job != nil {
		for _, device := range job.result.Devices {
			if device.ID == req.DeviceID {
				urls = append([]string{}, job.addresses[device.ID]...)
				peer = net.ParseIP(device.PeerIP)
				nic = job.result.Interface
				break
			}
		}
	}
	m.mu.Unlock()
	if len(urls) == 0 {
		return target{}, &Error{Code: "not_found", Message: "设备记录已过期，请重新搜索或使用手动地址"}
	}
	if req.BindIP != "" && req.BindIP != nic.IP {
		return target{}, invalid("所选设备与当前网卡不一致，请重新搜索")
	}
	for _, address := range urls {
		value, err := resolveTarget(ctx, address, nic, peer)
		if err == nil {
			return value, nil
		}
	}
	return target{}, &Error{Code: "endpoint_scope", Message: "发现的服务地址未指向回复设备，未发送凭据；可核对后使用手动地址"}
}
