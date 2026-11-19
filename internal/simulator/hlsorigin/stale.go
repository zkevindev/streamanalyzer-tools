package hlsorigin

import (
	"net/http"
	"strconv"
	"time"
)

type stalePlaylist struct {
	previous uint64
	served   bool
}

// Track the workbench's player and its independent sampler separately so the
// sampler cannot consume the player's one stale response.
func (m *Manager) serveStalePlaylist(w http.ResponseWriter, r *http.Request, now time.Time, origin uint64) uint64 {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		return origin
	}
	state := &m.stale[0]
	player := r.UserAgent() == PlayerUserAgent
	if player {
		state = &m.stale[1]
	}
	served := origin
	sample := PlaylistSample{At: now, Player: player, OriginSequence: origin - liveWindow + 1}
	if !state.served && state.previous > liveWindow && origin > state.previous && origin-state.previous < liveWindow {
		served = state.previous - 1
		sample.CacheAge = int(origin-served) * int(segmentDuration/time.Second)
		w.Header().Set("Age", strconv.Itoa(sample.CacheAge))
		state.served = true
		m.status.LastFault = &now
		m.status.FaultHits++
	}
	state.previous = served
	sample.Sequence = served - liveWindow + 1
	m.addPlaylistSample(sample)
	return served
}
