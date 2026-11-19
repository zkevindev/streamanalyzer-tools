package hlsorigin

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

//go:generate ffmpeg -hide_banner -loglevel error -y -f lavfi -i testsrc2=size=320x180:rate=15 -f lavfi -i sine=frequency=440:sample_rate=48000 -t 24 -c:v libx264 -threads 2 -preset fast -crf 35 -pix_fmt yuv420p -profile:v baseline -g 30 -sc_threshold 0 -c:a aac -b:a 32k -hls_time 2 -hls_list_size 0 -hls_playlist_type vod -start_number 1 -hls_segment_filename media/segment-%02d.ts media/playlist.m3u8
//go:embed media/*
var media embed.FS

const SegmentCount = 12

var (
	ErrActive = errors.New("HLS 模拟器正在运行")
	ErrClosed = errors.New("HLS 模拟器已关闭")
	ErrConfig = errors.New("HLS 场景参数无效")
)

type Config struct {
	Mode    string `json:"mode"`
	Segment int    `json:"segment,omitempty"`
	DelayMS int    `json:"delay_ms,omitempty"`
}

type Status struct {
	LowLatency *LowLatencyStatus `json:"low_latency,omitempty"`
	Config
	LiveFault       *LiveFaultObservation `json:"live_fault,omitempty"`
	Frozen          bool                  `json:"frozen"`
	MediaSequence   uint64                `json:"media_sequence,omitempty"`
	PlaylistSamples []PlaylistSample      `json:"playlist_samples,omitempty"`
	Active          bool                  `json:"active"`
	ID              string                `json:"id,omitempty"`
	Playlist        string                `json:"playlist,omitempty"`
	SegmentRequests uint64                `json:"segment_requests"`
	FaultHits       uint64                `json:"fault_hits"`
	LastFault       *time.Time            `json:"last_fault,omitempty"`
}

type Manager struct {
	live   liveClock
	mu     sync.Mutex
	status Status
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
}

func (m *Manager) Start(cfg Config) (Status, error) {
	switch cfg.Mode {
	case "normal", "live", "abr", "audio", "ll":
		cfg.Segment, cfg.DelayMS = 0, 0
	case "delay", "not_found":
		if cfg.Segment < 1 || cfg.Segment > SegmentCount {
			return Status{}, fmt.Errorf("%w，分片序号需为 1–%d", ErrConfig, SegmentCount)
		}
		if cfg.Mode == "delay" && (cfg.DelayMS < 500 || cfg.DelayMS > 15000) {
			return Status{}, fmt.Errorf("%w，延迟需为 500–15000 ms", ErrConfig)
		}
		if cfg.Mode == "not_found" {
			cfg.DelayMS = 0
		}
	default:
		return Status{}, ErrConfig
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
	m.status = Status{Config: cfg, Active: true, ID: id, Playlist: "/api/simulators/hls/stream/" + id + "/playlist.m3u8"}
	if cfg.Mode == "ll" {
		m.status.LowLatency = &LowLatencyStatus{}
	}
	m.live = liveClock{started: time.Now(), last: liveWindow}
	return m.snapshot(), nil
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshot()
}

func (m *Manager) snapshot() Status {
	result := m.status
	result.PlaylistSamples = slices.Clone(result.PlaylistSamples)
	if result.LowLatency != nil {
		low := *result.LowLatency
		if low.Fault != nil {
			fault := *low.Fault
			low.Fault = &fault
		}
		if result.Active {
			low.setEdge(llPublished(m.live.started, time.Now()))
		}
		result.MediaSequence = low.NextSequence - liveWindow
		result.LowLatency = &low
	}
	if result.Mode == "live" || result.Mode == "abr" || result.Mode == "audio" {
		result.Frozen = m.live.frozen
		last := m.live.sequence(time.Now())
		result.MediaSequence = last - liveWindow + 1
		if result.LiveFault != nil {
			fault := *result.LiveFault
			fault.Expired = fault.RetryAt == nil && (!result.Active || last >= fault.Sequence+2*liveWindow)
			result.LiveFault = &fault
		}
	}
	return result
}

func (m *Manager) stopLocked() Status {
	if m.status.Active {
		if m.status.LowLatency != nil {
			m.status.LowLatency.setEdge(llPublished(m.live.started, time.Now()))
		}
		if (m.status.Mode == "live" || m.status.Mode == "abr" || m.status.Mode == "audio") && !m.live.frozen {
			m.live.last = m.live.sequence(time.Now())
			m.live.frozen = true
		}
		m.cancel()
		m.status.Active = false
	}
	return m.snapshot()
}

func (m *Manager) Stop() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked()
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.stopLocked()
}

func (m *Manager) Serve(w http.ResponseWriter, r *http.Request, id, name string) {
	m.mu.Lock()
	if !m.status.Active || id != m.status.ID {
		m.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	if m.status.Mode == "ll" {
		m.serveLowLatency(w, r, name)
		return
	}
	if m.status.Mode == "abr" || m.status.Mode == "audio" {
		m.serveFragmented(w, r, name)
		return
	}
	playlist := name == "playlist.m3u8"
	kind := "application/vnd.apple.mpegurl"
	segment := 0
	liveFault := ""
	if m.status.Mode == "live" {
		now := time.Now()
		last := m.live.sequence(now)
		if playlist {
			if r.Method == http.MethodGet {
				m.recordPlaylist(now, last-liveWindow+1)
			}
			m.mu.Unlock()
			w.Header().Set("Content-Type", kind)
			http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(livePlaylist(last)))
			return
		}
		if seq, ok := liveResourceSequence(name, last); ok {
			segment = int((seq-1)%SegmentCount) + 1
			name = fmt.Sprintf("segment-%02d.ts", segment)
			liveFault = m.checkLiveFault(r, seq, now)
		}
	} else if !playlist {
		for i := 1; i <= SegmentCount; i++ {
			if name == fmt.Sprintf("segment-%02d.ts", i) {
				segment = i
				break
			}
		}
	}
	if !playlist {
		if segment == 0 {
			m.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		kind = "video/mp2t"
	}
	cfg, ctx := m.status.Config, m.ctx
	fault := false
	if r.Method == http.MethodGet && segment != 0 {
		m.status.SegmentRequests++
		if segment == cfg.Segment {
			m.status.FaultHits++
			now := time.Now()
			m.status.LastFault = &now
			fault = true
		}
	}
	m.mu.Unlock()
	if liveFault == "not_found" {
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}
	if fault {
		if cfg.Mode == "not_found" {
			http.NotFound(w, r)
			return
		}
		timer := time.NewTimer(time.Duration(cfg.DelayMS) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.Context().Done():
			return
		case <-ctx.Done():
			w.WriteHeader(http.StatusGone)
			return
		}
	}
	data, err := media.ReadFile("media/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "no-store")
	if liveFault == "truncate" {
		serveTruncatedSegment(w, data)
		return
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
