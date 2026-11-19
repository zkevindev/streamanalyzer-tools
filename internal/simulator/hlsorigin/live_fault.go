package hlsorigin

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const PlayerUserAgent = "StreamAnalyzer-Diagnostic-Player/1.0"

type LiveFaultConfig struct {
	Kind     string `json:"kind"`
	Sequence uint64 `json:"sequence"`
	Audience string `json:"audience"`
}

type LiveFaultObservation struct {
	LiveFaultConfig
	Rendition string     `json:"rendition,omitempty"`
	ArmedAt   time.Time  `json:"armed_at"`
	HitAt     *time.Time `json:"hit_at,omitempty"`
	RetryAt   *time.Time `json:"retry_at,omitempty"`
	Attempts  int        `json:"attempts"`
	Expired   bool       `json:"expired"`
}

func (m *Manager) ArmLiveFault(cfg LiveFaultConfig) (Status, error) {
	if cfg.Kind != "not_found" && cfg.Kind != "truncate" {
		return Status{}, fmt.Errorf("%w，选择首次 404 或首次正文中断", ErrConfig)
	}
	if cfg.Audience == "" {
		cfg.Audience = "player"
	}
	if cfg.Audience != "player" && cfg.Audience != "all" {
		return Status{}, fmt.Errorf("%w，选择实际播放请求或全部请求", ErrConfig)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.status.Active || (m.status.Mode != "live" && m.status.Mode != "audio") {
		return Status{}, ErrLiveUnavailable
	}
	now := time.Now()
	next := m.live.sequence(now) + 1
	if cfg.Sequence == 0 {
		cfg.Sequence = next
	}
	if cfg.Sequence < next || cfg.Sequence > next+120 {
		return Status{}, fmt.Errorf("%w，分片序号需在 %d–%d 内", ErrConfig, next, next+120)
	}
	if current := m.status.LiveFault; current != nil && current.RetryAt == nil && m.live.sequence(now) < current.Sequence+2*liveWindow {
		return Status{}, ErrFaultPending
	}
	m.status.LiveFault = &LiveFaultObservation{LiveFaultConfig: cfg, ArmedAt: now}
	if m.status.Mode == "audio" {
		m.status.LiveFault.Rendition = "audio"
	}
	return m.snapshot(), nil
}

func (m *Manager) checkLiveFault(r *http.Request, seq uint64, now time.Time) string {
	fault := m.status.LiveFault
	if fault == nil || fault.Sequence != seq || r.Method != http.MethodGet ||
		fault.Audience == "player" && r.UserAgent() != PlayerUserAgent {
		return ""
	}
	fault.Attempts++
	if fault.HitAt != nil {
		if fault.RetryAt == nil {
			fault.RetryAt = &now
		}
		return ""
	}
	fault.HitAt = &now
	m.status.LastFault = &now
	m.status.FaultHits++
	return fault.Kind
}

func serveTruncatedSegment(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	// Keep a valid prefix while making the body shorter than Content-Length.
	_, _ = w.Write(data[:len(data)/2])
}
