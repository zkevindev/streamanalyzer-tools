package hlsorigin

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	liveWindow         = 3
	segmentDuration    = 2 * time.Second
	maxPlaylistSamples = 48
)

var ErrLiveUnavailable = errors.New("请先启动 HLS 直播源")
var ErrFaultPending = errors.New("当前故障仍在等待触发或重试")

type PlaylistSample struct {
	At       time.Time `json:"at"`
	Sequence uint64    `json:"sequence"`
	Frozen   bool      `json:"frozen"`
}

type liveClock struct {
	started time.Time
	last    uint64
	frozen  bool
}

func (c liveClock) sequence(now time.Time) uint64 {
	if c.frozen {
		return c.last
	}
	return c.last + uint64(now.Sub(c.started)/segmentDuration)
}

func (m *Manager) Freeze(frozen bool) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.status.Active || m.status.Mode != "live" {
		return Status{}, ErrLiveUnavailable
	}
	if m.live.frozen != frozen {
		now := time.Now()
		last := m.live.sequence(now)
		if !frozen {
			last++
		}
		m.live = liveClock{started: now, last: last, frozen: frozen}
	}
	return m.snapshot(), nil
}

// Live reuses the VOD transport stream segments in a rolling list. Each
// repetition starts a new timestamp period and receives unique media URLs.
func livePlaylist(last uint64) []byte {
	first := last - liveWindow + 1
	var playlist bytes.Buffer
	fmt.Fprintf(&playlist, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-DISCONTINUITY-SEQUENCE:%d\n",
		first, (first-1)/SegmentCount)
	for seq := first; seq <= last; seq++ {
		if seq > first && (seq-1)%SegmentCount == 0 {
			playlist.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		fmt.Fprintf(&playlist, "#EXTINF:2.000,\nlive-%d.ts\n", seq)
	}
	return playlist.Bytes()
}

func liveResourceSequence(name string, last uint64) (uint64, bool) {
	n, suffix, ok := strings.Cut(strings.TrimPrefix(name, "live-"), ".")
	if !strings.HasPrefix(name, "live-") || !ok || suffix != "ts" {
		return 0, false
	}
	seq, err := strconv.ParseUint(n, 10, 64)
	if err != nil || seq == 0 || seq > last || last-seq >= 2*liveWindow {
		return 0, false
	}
	return seq, true
}

func (m *Manager) recordPlaylist(now time.Time, seq uint64) {
	if len(m.status.PlaylistSamples) == maxPlaylistSamples {
		copy(m.status.PlaylistSamples, m.status.PlaylistSamples[1:])
		m.status.PlaylistSamples = m.status.PlaylistSamples[:maxPlaylistSamples-1]
	}
	m.status.PlaylistSamples = append(m.status.PlaylistSamples, PlaylistSample{now, seq, m.live.frozen})
}
