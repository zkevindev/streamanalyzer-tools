package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"regexp"
	"slices"
	"sync"
	"time"
)

const (
	playerNetworkSchemaVersion = 5
	playerNetworkName          = "player-network.json"
	maxPlayerNetworkBytes      = 512 << 10
	maxPlayerNetworkRequests   = 512
	maxPlayerNetworkOffsetMS   = 3_600_000
)

// PlayerNetworkRequest describes the local proxy's upstream observation, never
// delivery to the browser, SourceBuffer append, or decoder consumption.
type PlayerNetworkRequest struct {
	Number      int    `json:"number"`
	ResourceID  string `json:"resource_id"`
	Kind        string `json:"kind"`
	Method      string `json:"method"`
	StartMS     int64  `json:"start_ms"`
	HeaderMS    *int64 `json:"header_ms,omitempty"`
	FirstBodyMS *int64 `json:"first_body_ms,omitempty"`
	EndMS       int64  `json:"end_ms"`
	HTTPStatus  int    `json:"http_status,omitempty"`
	Bytes       int64  `json:"bytes"`
	Outcome     string `json:"outcome"`
}

type PlayerNetworkReport struct {
	Version         int                    `json:"version"`
	Boundary        string                 `json:"boundary"`
	PlayerRevision  int                    `json:"player_revision"`
	Complete        bool                   `json:"complete"`
	DroppedRequests int                    `json:"dropped_requests"`
	Requests        []PlayerNetworkRequest `json:"requests"`
}

type playerNetworkEntry struct {
	request PlayerNetworkRequest
	done    bool
}

type playerNetwork struct {
	mu              sync.Mutex
	origin          time.Time
	initialOffsetMS int64
	entries         []*playerNetworkEntry
	dropped         int
	stopped         bool
	frozen          bool
	last            *PlayerNetworkReport
}

func newPlayerNetwork() *playerNetwork { return &playerNetwork{origin: time.Now()} }

func (n *playerNetwork) setOrigin(at time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.entries) == 0 {
		now := time.Now()
		n.origin, n.initialOffsetMS = now, now.Sub(at).Milliseconds()
	}
}

func (n *playerNetwork) offset() int64 {
	return max(0, n.initialOffsetMS+time.Since(n.origin).Milliseconds())
}

func (n *playerNetwork) begin(resource, kind, method string) *playerNetworkEntry {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.frozen || n.stopped {
		return nil
	}
	if len(n.entries) >= maxPlayerNetworkRequests || n.offset() > maxPlayerNetworkOffsetMS {
		n.dropped = min(1_000_000_000, n.dropped+1)
		return nil
	}
	e := &playerNetworkEntry{request: PlayerNetworkRequest{Number: len(n.entries) + 1, ResourceID: resource, Kind: kind, Method: method, StartMS: n.offset(), Outcome: "incomplete"}}
	n.entries = append(n.entries, e)
	return e
}

func (n *playerNetwork) header(e *playerNetworkEntry, status int) {
	if e == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.frozen || n.stopped {
		return
	}
	at := min(int64(maxPlayerNetworkOffsetMS), n.offset())
	e.request.HeaderMS, e.request.HTTPStatus = &at, status
}

func (n *playerNetwork) body(e *playerNetworkEntry, bytes int) {
	if e == nil || bytes <= 0 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.frozen || n.stopped {
		return
	}
	if e.request.FirstBodyMS == nil {
		at := min(int64(maxPlayerNetworkOffsetMS), n.offset())
		e.request.FirstBodyMS = &at
	}
	e.request.Bytes += int64(bytes)
}

func (n *playerNetwork) finish(e *playerNetworkEntry, outcome string) {
	if e == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.frozen || n.stopped {
		return
	}
	e.request.EndMS, e.request.Outcome, e.done = min(int64(maxPlayerNetworkOffsetMS), n.offset()), outcome, true
}

func clonePlayerNetwork(value *PlayerNetworkReport) *PlayerNetworkReport {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Requests = slices.Clone(value.Requests)
	for i := range copy.Requests {
		if p := copy.Requests[i].HeaderMS; p != nil {
			v := *p
			copy.Requests[i].HeaderMS = &v
		}
		if p := copy.Requests[i].FirstBodyMS; p != nil {
			v := *p
			copy.Requests[i].FirstBodyMS = &v
		}
	}
	return &copy
}

func (n *playerNetwork) snapshot(revision int, complete bool) *PlayerNetworkReport {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.last != nil && (n.frozen || n.last.PlayerRevision == revision) {
		return clonePlayerNetwork(n.last)
	}
	result := &PlayerNetworkReport{Version: 1, Boundary: "upstream_proxy", PlayerRevision: revision, Complete: complete, DroppedRequests: n.dropped, Requests: []PlayerNetworkRequest{}}
	cutoff := min(int64(maxPlayerNetworkOffsetMS), n.offset())
	for _, entry := range n.entries {
		request := entry.request
		if !entry.done {
			request.EndMS, request.Outcome = cutoff, "incomplete"
		}
		result.Requests = append(result.Requests, request)
	}
	n.last, n.frozen = clonePlayerNetwork(result), complete
	return clonePlayerNetwork(result)
}

func (n *playerNetwork) stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		return
	}
	cutoff := min(int64(maxPlayerNetworkOffsetMS), n.offset())
	for _, entry := range n.entries {
		if !entry.done {
			entry.request.EndMS, entry.request.Outcome, entry.done = cutoff, "incomplete", true
		}
	}
	n.stopped = true
}

type playerNetworkReader struct {
	io.ReadCloser
	network *playerNetwork
	entry   *playerNetworkEntry
	err     error
	eof     bool
}

func (r *playerNetworkReader) Read(data []byte) (int, error) {
	n, err := r.ReadCloser.Read(data)
	r.network.body(r.entry, n)
	if err != nil && err != io.EOF {
		r.err = err
	}
	if err == io.EOF {
		r.eof = true
	}
	return n, err
}

func playerNetworkOutcome(err error, contextErr error) string {
	if errors.Is(contextErr, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(contextErr, context.Canceled) {
		return "canceled"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	return "transport_error"
}

var playerNetworkResourcePattern = regexp.MustCompile(`^(index\.m3u8|r[1-9][0-9]{0,3}\.(m3u8|ts|m4s|mp4|aac|mp3|vtt|bin))$`)

func validatePlayerNetwork(report *PlayerNetworkReport, player *PlayerReport, protocol string, complete bool) error {
	if report == nil || player == nil || protocol != ProtocolHLS || report.Version != 1 || report.Boundary != "upstream_proxy" || report.PlayerRevision != player.Revision || report.Complete != complete || report.DroppedRequests < 0 || report.DroppedRequests > 1_000_000_000 || len(report.Requests) > maxPlayerNetworkRequests || report.Requests == nil {
		return ErrInvalid
	}
	previous := int64(-60000)
	for index, request := range report.Requests {
		if request.Number != index+1 || !playerNetworkResourcePattern.MatchString(request.ResourceID) || (request.Method != "GET" && request.Method != "HEAD") || request.StartMS < previous || request.StartMS < -60000 || request.EndMS < request.StartMS || request.EndMS > maxPlayerNetworkOffsetMS || request.Bytes < 0 || request.Bytes > hlsSegmentLimit || (request.HTTPStatus != 0 && (request.HTTPStatus < 100 || request.HTTPStatus > 599)) {
			return ErrInvalid
		}
		previous = request.StartMS
		switch request.Kind {
		case "playlist", "media", "init", "key", "auxiliary":
		default:
			return ErrInvalid
		}
		switch request.Outcome {
		case "complete", "http_error", "transport_error", "canceled", "timeout", "incomplete":
		default:
			return ErrInvalid
		}
		for _, at := range []*int64{request.HeaderMS, request.FirstBodyMS} {
			if at != nil && (*at < request.StartMS || *at > request.EndMS) {
				return ErrInvalid
			}
		}
		if (request.HeaderMS == nil) != (request.HTTPStatus == 0) || (request.FirstBodyMS != nil && (request.HeaderMS == nil || *request.FirstBodyMS < *request.HeaderMS)) || (request.Bytes > 0 && request.FirstBodyMS == nil) || (request.FirstBodyMS != nil && request.Bytes == 0) || (request.Method == "HEAD" && (request.Bytes != 0 || request.FirstBodyMS != nil)) {
			return ErrInvalid
		}
		if request.Outcome == "complete" && (request.HTTPStatus != 200 && request.HTTPStatus != 206) {
			return ErrInvalid
		}
		if request.Outcome == "http_error" && (request.HTTPStatus == 0 || request.HTTPStatus == 200 || request.HTTPStatus == 206) {
			return ErrInvalid
		}
	}
	_, err := json.Marshal(report)
	return err
}
