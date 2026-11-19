package hlsorigin

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

//go:generate sh generate-ll.sh
//go:embed llmedia/*
var llMedia embed.FS

const (
	llPartDuration    = 400 * time.Millisecond
	llPartsPerSegment = 5
	llInitialParts    = liveWindow * llPartsPerSegment
	llFixtureSegments = 6
)

type PartDelay struct {
	Sequence    uint64    `json:"sequence"`
	Part        uint64    `json:"part"`
	DelayMS     int       `json:"delay_ms"`
	AvailableAt time.Time `json:"available_at"`
	ReleaseAt   time.Time `json:"release_at"`
	RequestedAt time.Time `json:"requested_at,omitzero"`
	UnblockedAt time.Time `json:"unblocked_at,omitzero"`
}

type LowLatencyStatus struct {
	BlockedReloads   uint64     `json:"blocked_reloads"`
	LastReloadWaitMS *int64     `json:"last_reload_wait_ms,omitempty"`
	NextSequence     uint64     `json:"next_sequence"`
	NextPart         uint64     `json:"next_part"`
	Fault            *PartDelay `json:"fault,omitempty"`
}

func (s *LowLatencyStatus) setEdge(published uint64) {
	s.NextSequence, s.NextPart = published/llPartsPerSegment+1, published%llPartsPerSegment
}

func (m *Manager) ArmPartDelay(delay int) (Status, error) {
	if delay < 400 || delay > 3000 {
		return Status{}, fmt.Errorf("%w，Part 延迟需为 400–3000 毫秒", ErrConfig)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.status.Active || m.status.Mode != "ll" {
		return Status{}, ErrLiveUnavailable
	}
	now := time.Now()
	state := m.status.LowLatency
	if state.Fault != nil && state.Fault.UnblockedAt.IsZero() && now.Before(state.Fault.ReleaseAt.Add(3*segmentDuration)) {
		return Status{}, ErrFaultPending
	}
	// Leave one part between the current preload request and the delayed
	// part so an in-flight prefetch cannot miss an arming operation.
	position := llPublished(m.live.started, now) + 1
	available := llPublishAt(m.live.started, position)
	state.Fault = &PartDelay{Sequence: position/llPartsPerSegment + 1, Part: position % llPartsPerSegment, DelayMS: delay,
		AvailableAt: available, ReleaseAt: available.Add(time.Duration(delay) * time.Millisecond)}
	return m.snapshot(), nil
}

func llPublished(start, now time.Time) uint64 {
	return llInitialParts + uint64(max(0, now.Sub(start))/llPartDuration)
}

func llPublishAt(start time.Time, position uint64) time.Time {
	if position < llInitialParts {
		return start
	}
	return start.Add(time.Duration(position+1-llInitialParts) * llPartDuration)
}

func llNumber(raw string) (uint64, bool) {
	value, err := strconv.ParseUint(raw, 10, 32)
	return value, err == nil && strconv.FormatUint(value, 10) == raw
}

func llReloadDeadline(query url.Values, published uint64) (position uint64, requested, valid bool) {
	if !query.Has("_HLS_msn") {
		return 0, false, !query.Has("_HLS_part")
	}
	seq, ok := llNumber(query.Get("_HLS_msn"))
	if !ok || seq == 0 || seq > published/llPartsPerSegment+2 {
		return 0, false, false
	}
	if !query.Has("_HLS_part") {
		return seq*llPartsPerSegment - 1, true, true
	}
	part, ok := llNumber(query.Get("_HLS_part"))
	if !ok || part > llPartsPerSegment {
		return 0, false, false
	}
	return (seq-1)*llPartsPerSegment + part, true, true
}

func llPlaylist(published uint64) []byte {
	completed := published / llPartsPerSegment
	first := completed - liveWindow + 1
	var text bytes.Buffer
	fmt.Fprintf(&text, "#EXTM3U\n#EXT-X-VERSION:9\n#EXT-X-TARGETDURATION:2\n#EXT-X-SERVER-CONTROL:CAN-BLOCK-RELOAD=YES,PART-HOLD-BACK=1.200\n#EXT-X-PART-INF:PART-TARGET=0.400\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-DISCONTINUITY-SEQUENCE:%d\n",
		first, (first-1)/llFixtureSegments)
	for seq := first; seq <= completed+1; seq++ {
		if seq == completed+1 && published%llPartsPerSegment == 0 {
			break
		}
		boundary := (seq-1)%llFixtureSegments == 0
		if boundary && seq > first {
			text.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		if seq == first || boundary {
			fmt.Fprintf(&text, "#EXT-X-MAP:URI=\"ll-init-%d.mp4\"\n", (seq-1)/llFixtureSegments)
		}
		parts := uint64(llPartsPerSegment)
		if seq > completed {
			parts = published % llPartsPerSegment
		}
		for part := uint64(0); part < parts; part++ {
			independent := ""
			if part == 0 {
				independent = ",INDEPENDENT=YES"
			}
			fmt.Fprintf(&text, "#EXT-X-PART:DURATION=0.400,URI=\"ll-%d-%d.m4s\"%s\n", seq, part, independent)
		}
		if seq <= completed {
			fmt.Fprintf(&text, "#EXTINF:2.000,\nll-%d.m4s\n", seq)
		}
	}
	fmt.Fprintf(&text, "#EXT-X-PRELOAD-HINT:TYPE=PART,URI=\"ll-%d-%d.m4s\"\n", completed+1, published%llPartsPerSegment)
	return text.Bytes()
}

// serveLowLatency owns the mutex on entry. No playlist or part wait holds it.
func (m *Manager) serveLowLatency(w http.ResponseWriter, r *http.Request, name string) {
	start, id, scope := m.live.started, m.status.ID, m.ctx
	now := time.Now()
	requestAt := now
	published := llPublished(start, now)
	w.Header().Set("Cache-Control", "no-store")
	if name == "playlist.m3u8" {
		position, requested, valid := llReloadDeadline(r.URL.Query(), published)
		blocked := requested && position >= published
		if blocked {
			m.status.LowLatency.BlockedReloads++
		}
		m.mu.Unlock()
		if !valid {
			http.Error(w, "Invalid HLS delivery directive", http.StatusBadRequest)
			return
		}
		if blocked && !llWait(w, r, scope, llPublishAt(start, position)) {
			return
		}
		now = time.Now()
		published = llPublished(start, now)
		m.mu.Lock()
		if !m.status.Active || m.status.ID != id {
			m.mu.Unlock()
			w.WriteHeader(http.StatusGone)
			return
		}
		if blocked {
			wait := now.Sub(requestAt).Milliseconds()
			m.status.LowLatency.LastReloadWaitMS = &wait
		}
		if r.Method == http.MethodGet {
			m.recordPlaylist(now, published/llPartsPerSegment-liveWindow+1)
		}
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(llPlaylist(published)))
		return
	}

	minSequence := published/llPartsPerSegment - min(published/llPartsPerSegment-1, 2*liveWindow-1)
	stem, ok := strings.CutPrefix(name, "ll-")
	if !ok {
		m.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	if init, ok := strings.CutPrefix(stem, "init-"); ok {
		epoch, valid := llNumber(strings.TrimSuffix(init, ".mp4"))
		m.mu.Unlock()
		if !valid || !strings.HasSuffix(init, ".mp4") || epoch < (minSequence-1)/llFixtureSegments ||
			epoch > (published/llPartsPerSegment)/llFixtureSegments {
			http.NotFound(w, r)
			return
		}
		data, _ := llMedia.ReadFile("llmedia/init.mp4")
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
		return
	}
	stem, extOK := strings.CutSuffix(stem, ".m4s")
	sequenceText, partText, partial := strings.Cut(stem, "-")
	sequence, sequenceOK := llNumber(sequenceText)
	part, partOK := llNumber(partText)
	if !extOK || !sequenceOK || sequence < minSequence || sequence > published/llPartsPerSegment+1 ||
		partial && (!partOK || part >= llPartsPerSegment) || !partial && sequence > published/llPartsPerSegment {
		m.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	position := (sequence-1)*llPartsPerSegment + part
	if partial && position > published {
		m.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	available := llPublishAt(start, position)
	var fault *PartDelay
	if partial && r.Method == http.MethodGet {
		if configured := m.status.LowLatency.Fault; configured != nil && configured.Sequence == sequence && configured.Part == part && !now.After(configured.ReleaseAt) {
			fault = configured
			if fault.RequestedAt.IsZero() {
				fault.RequestedAt = now
				m.status.FaultHits++
				at := now
				m.status.LastFault = &at
			}
			available = configured.ReleaseAt
		}
	}
	if r.Method == http.MethodGet {
		m.status.SegmentRequests++
	}
	m.mu.Unlock()
	if !llWait(w, r, scope, available) {
		return
	}
	if fault != nil {
		m.mu.Lock()
		if m.status.ID == id && m.status.LowLatency.Fault == fault && fault.UnblockedAt.IsZero() {
			fault.UnblockedAt = time.Now()
		}
		m.mu.Unlock()
	}
	parts := uint64(1)
	if !partial {
		parts = llPartsPerSegment
	}
	var data bytes.Buffer
	for index := position; index < position+parts; index++ {
		path := fmt.Sprintf("llmedia/part-%02d.m4s", index%(llFixtureSegments*llPartsPerSegment)+1)
		file, err := llMedia.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		data.Write(file)
	}
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data.Bytes()))
}

func llWait(w http.ResponseWriter, r *http.Request, source context.Context, available time.Time) bool {
	timer := time.NewTimer(max(0, time.Until(available)))
	defer timer.Stop()
	select {
	case <-r.Context().Done():
		return false
	case <-source.Done():
		w.WriteHeader(http.StatusGone)
		return false
	case <-timer.C:
		return true
	}
}
