package mediasource

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

//go:generate sh generate.sh
//go:embed fixtures/*
var fixtures embed.FS

var (
	ErrInvalid = errors.New("请选择可用的流和故障组合")
	ErrActive  = errors.New("媒体模拟源正在运行")
	ErrClosed  = errors.New("媒体模拟源已关闭")
)

type Config struct {
	TargetIP   string `json:"target_ip,omitempty"`
	TargetPort int    `json:"target_port,omitempty"`
	Interface  string `json:"interface,omitempty"`
	Protocol   string `json:"protocol"`
	Fault      string `json:"fault"`
}

type Status struct {
	Sending       bool   `json:"sending"`
	SentDatagrams uint64 `json:"sent_datagrams"`
	SendError     string `json:"send_error,omitempty"`
	Config
	Active     bool       `json:"active"`
	ID         string     `json:"id,omitempty"`
	URL        string     `json:"url,omitempty"`
	DurationMS int64      `json:"duration_ms,omitempty"`
	Requests   uint64     `json:"requests"`
	FaultHits  uint64     `json:"fault_hits"`
	LastFault  *time.Time `json:"last_fault,omitempty"`
}

type chunk struct {
	at    time.Duration
	data  []byte
	fault bool
}

type stream struct {
	chunks []chunk
	mime   string
	size   int
}

type Manager struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	status Status
	stream stream
	closed bool
}

func (m *Manager) Start(cfg Config) (Status, error) {
	if cfg.Fault == "" {
		cfg.Fault = "normal"
	}
	var filename, mime, captureURL string
	latePSI := cfg.Fault == "late_pat" || cfg.Fault == "late_pmt"
	if latePSI && !cfg.datagram() {
		return Status{}, ErrInvalid
	}
	if cfg.datagram() {
		if latePSI && cfg.TargetPort == 0 && net.ParseIP(cfg.TargetIP).IsMulticast() {
			reserved, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
			if err != nil {
				return Status{}, fmt.Errorf("分配组播测试端口：%w", err)
			}
			cfg.TargetPort = reserved.LocalAddr().(*net.UDPAddr).Port
			reserved.Close()
		}
		dest, _, err := cfg.destination()
		if err != nil {
			return Status{}, err
		}
		captureURL = cfg.captureURL(dest)
	}
	switch cfg.Protocol {
	case "http-flv":
		mime = "video/x-flv"
		switch cfg.Fault {
		case "normal", "dts_backwards":
			filename = "regular.flv"
		case "long_gop":
			filename = "long-gop.flv"
		default:
			return Status{}, ErrInvalid
		}
	case "http-ts", "udp-ts", "rtp-ts":
		mime, filename = "video/mp2t", "regular.ts"
		rtpFault := cfg.Protocol == "rtp-ts" && (cfg.Fault == "rtp_drop" || cfg.Fault == "rtp_reorder" || cfg.Fault == "rtp_duplicate")
		if cfg.Fault != "normal" && cfg.Fault != "continuity" && cfg.Fault != "pcr_jump" && cfg.Fault != "pmt_pid_switch" && cfg.Fault != "program_outage" && cfg.Fault != "program_clock_drift" && !rtpFault && !latePSI {
			return Status{}, ErrInvalid
		}
	default:
		return Status{}, ErrInvalid
	}

	data, err := fixtures.ReadFile("fixtures/" + filename)
	if err != nil {
		return Status{}, err
	}
	plan, err := prepareStream(cfg, data, mime)
	if err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Status{}, ErrClosed
	}
	if m.status.Active {
		return Status{}, ErrActive
	}
	id := uuid.NewString()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.stream = plan
	if captureURL == "" {
		captureURL = "/api/simulators/media/stream/" + id
	}
	m.status = Status{Config: cfg, Active: true, ID: id, URL: captureURL,
		DurationMS: plan.chunks[len(plan.chunks)-1].at.Milliseconds()}
	return m.status, nil
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) Stop() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
	return m.status
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	if m.status.Active {
		m.cancel()
		m.status.Active = false
		m.stream = stream{}
	}
}

func (m *Manager) Serve(w http.ResponseWriter, r *http.Request, id string) {
	m.mu.Lock()
	if !m.status.Active || id != m.status.ID || m.status.Config.datagram() {
		m.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	plan, ctx := m.stream, m.ctx
	if r.Method == http.MethodGet {
		m.status.Requests++
	}
	m.mu.Unlock()

	w.Header().Set("Content-Type", plan.mime)
	w.Header().Set("Content-Length", strconv.Itoa(plan.size))
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	w.WriteHeader(http.StatusOK)
	flush := http.NewResponseController(w)
	begin := time.Now()
	for _, piece := range plan.chunks {
		if !waitForChunk(r.Context(), ctx, piece.at-time.Since(begin)) {
			return
		}
		if _, err := w.Write(piece.data); err != nil {
			return
		}
		if err := flush.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return
		}
		if piece.fault {
			m.mu.Lock()
			if m.status.ID == id && m.status.Active {
				at := time.Now()
				m.status.FaultHits++
				m.status.LastFault = &at
			}
			m.mu.Unlock()
		}
	}
}

func waitForChunk(request, source context.Context, delay time.Duration) bool {
	if request.Err() != nil || source.Err() != nil {
		return false
	}
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-request.Done():
		return false
	case <-source.Done():
		return false
	}
}

func prepareStream(cfg Config, data []byte, mime string) (stream, error) {
	plan := stream{mime: mime}
	var err error
	if cfg.Protocol == "http-flv" {
		plan.chunks, err = planFLV(data, cfg.Fault)
	} else {
		fault := cfg.Fault
		if strings.HasPrefix(fault, "rtp_") {
			fault = "normal"
		}
		plan.chunks, err = planTS(data, fault)
	}
	if err != nil {
		return stream{}, fmt.Errorf("媒体模拟源准备失败：%w", err)
	}
	for _, piece := range plan.chunks {
		plan.size += len(piece.data)
	}
	return plan, nil
}
