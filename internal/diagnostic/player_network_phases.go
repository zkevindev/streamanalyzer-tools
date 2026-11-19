package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"
)

const maxPlayerNetworkHops = 8
const maxPlayerNetworkHopStages = 16
const maxPlayerNetworkHopBytes = 256 << 10

// Hop stages contain controlled phase names and numeric timings only. TTFBMS
// is a duration; all other times are offsets in the request's capture clock.
type PlayerNetworkHop struct {
	Number           int            `json:"number"`
	StartMS          int64          `json:"start_ms"`
	EndMS            int64          `json:"end_ms"`
	HTTPStatus       int            `json:"http_status,omitempty"`
	TTFBMS           *int64         `json:"ttfb_ms,omitempty"`
	ConnectionReused *bool          `json:"connection_reused,omitempty"`
	Stages           []CaptureStage `json:"stages,omitempty"`
	FailureStage     string         `json:"failure_stage,omitempty"`
	FailureCode      string         `json:"failure_code,omitempty"`
}

func clonePlayerNetworkHops(hops []PlayerNetworkHop) []PlayerNetworkHop {
	result := slices.Clone(hops)
	for i := range result {
		result[i].Stages = slices.Clone(result[i].Stages)
		if p := result[i].TTFBMS; p != nil {
			value := *p
			result[i].TTFBMS = &value
		}
		if p := result[i].ConnectionReused; p != nil {
			value := *p
			result[i].ConnectionReused = &value
		}
	}
	return result
}

func playerPhaseFailure(stage, code string) (string, string) {
	if code == "network_error" || code == "body_error" {
		code = "transport_error"
	}
	if !validPlayerPhaseFailure(stage, code, true) {
		return "request_send", "transport_error"
	}
	return stage, code
}

func validPlayerPhaseFailure(stage, code string, terminal bool) bool {
	if stage == "" || code == "" {
		return stage == "" && code == ""
	}
	if !validPlayerPhaseStage(stage, terminal) {
		return false
	}
	switch code {
	case "dns_error":
		if stage != "dns" {
			return false
		}
	case "tls_error":
		if stage != "tls" {
			return false
		}
	case "connection_refused":
		if stage != "connect" {
			return false
		}
	case "http_error", "redirect_error":
		if stage != "response_headers" {
			return false
		}
	case "limit_exceeded":
		if stage != "proxy_limit" {
			return false
		}
	case "invalid_playlist":
		if stage != "playlist" {
			return false
		}
	case "proxy_write_error":
		if stage != "proxy_write" {
			return false
		}
	}
	switch code {
	case "dns_error", "tls_error", "connection_refused", "timeout", "canceled", "transport_error", "redirect_error", "http_error", "limit_exceeded", "invalid_playlist", "proxy_write_error":
		return true
	}
	return false
}

func validPlayerPhaseStage(stage string, terminal bool) bool {
	switch stage {
	case "dns", "connect", "tls", "request_send", "response_first_byte", "response_headers":
		return true
	}
	return terminal && (stage == "receive" || stage == "proxy_write" || stage == "proxy_limit" || stage == "playlist")
}

func playerHopBytes(hop PlayerNetworkHop) int { data, _ := json.Marshal(hop); return len(data) }

// Reserve the longest allowed numeric representation so body progress and
// negative capture-clock shifts cannot overflow the persisted metadata budget.
func playerHopBudgetBytes(hop PlayerNetworkHop) int {
	hop = clonePlayerNetworkHops([]PlayerNetworkHop{hop})[0]
	hop.StartMS, hop.EndMS = maxPlayerNetworkOffsetMS, maxPlayerNetworkOffsetMS
	if hop.TTFBMS != nil {
		value := int64(maxPlayerNetworkOffsetMS)
		hop.TTFBMS = &value
	}
	if hop.ConnectionReused != nil {
		value := false
		hop.ConnectionReused = &value
	}
	for i := range hop.Stages {
		hop.Stages[i].StartMS, hop.Stages[i].EndMS = maxPlayerNetworkOffsetMS, maxPlayerNetworkOffsetMS
	}
	return playerHopBytes(hop)
}

func (n *playerNetwork) droppedHopLocked(e *playerNetworkEntry) {
	e.request.DroppedHops = min(1_000_000_000, e.request.DroppedHops+1)
}

func (n *playerNetwork) dropHop(e *playerNetworkEntry) {
	if e == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.frozen || n.stopped {
		return
	}
	e.hopCount++
	e.lastFailureStage, e.lastFailureCode = "", ""
	n.droppedHopLocked(e)
}

func (n *playerNetwork) recordHop(e *playerNetworkEntry, timing HTTPRequestTiming, reused *bool) {
	if e == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.frozen || n.stopped {
		return
	}
	e.hopCount++
	hop := PlayerNetworkHop{Number: e.hopCount, StartMS: max(e.request.StartMS, timing.StartMS), EndMS: max(e.request.StartMS, timing.EndMS), HTTPStatus: timing.HTTPStatus}
	if timing.TTFBMS != nil {
		value := *timing.TTFBMS
		hop.TTFBMS = &value
	}
	if reused != nil {
		value := *reused
		hop.ConnectionReused = &value
	}
	for _, stage := range timing.Stages {
		if !validPlayerPhaseStage(stage.Name, false) {
			continue
		}
		stage.StartMS, stage.EndMS = max(hop.StartMS, stage.StartMS), max(hop.StartMS, stage.EndMS)
		hop.Stages = append(hop.Stages, stage)
	}
	if timing.FailureCode != "" {
		hop.FailureStage, hop.FailureCode = playerPhaseFailure(timing.FailureStage, timing.FailureCode)
	}
	e.lastFailureStage, e.lastFailureCode = hop.FailureStage, hop.FailureCode
	// Reserve the terminal hop's receive phase rather than silently discarding
	// a setup stage when native trace callbacks exhaust the per-hop bound.
	if e.hopCount > maxPlayerNetworkHops || len(hop.Stages) >= maxPlayerNetworkHopStages || hop.EndMS > maxPlayerNetworkOffsetMS || (hop.TTFBMS != nil && (*hop.TTFBMS < 0 || *hop.TTFBMS > hop.EndMS-hop.StartMS)) || n.metadataBytes+playerHopBudgetBytes(hop) > maxPlayerNetworkHopBytes {
		n.droppedHopLocked(e)
		return
	}
	n.metadataBytes += playerHopBudgetBytes(hop)
	e.request.Hops = append(e.request.Hops, hop)
}

func (n *playerNetwork) doRequest(client *http.Client, request *http.Request, e *playerNetworkEntry) (*http.Response, error) {
	if e == nil {
		return client.Do(request)
	}
	n.mu.Lock()
	start := n.origin.Add(-time.Duration(n.initialOffsetMS) * time.Millisecond)
	n.mu.Unlock()
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	transport := &observedHTTPTransport{base: base, sampleStart: start, onRequest: func(timing HTTPRequestTiming, reused *bool) { n.recordHop(e, timing, reused) }, onDropped: func() { n.dropHop(e) }}
	copy := *client
	copy.Transport = transport
	return copy.Do(request)
}

func (n *playerNetwork) fail(e *playerNetworkEntry, stage, code string) {
	if e == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.frozen || n.stopped {
		return
	}
	e.request.FailureStage, e.request.FailureCode = playerPhaseFailure(stage, code)
}

func (n *playerNetwork) requestError(e *playerNetworkEntry, err, contextErr error, response *http.Response) {
	stage, code := httpFailure("request_send", err)
	if e != nil {
		n.mu.Lock()
		if e.lastFailureCode != "" {
			stage, code = e.lastFailureStage, e.lastFailureCode
		}
		n.mu.Unlock()
	}
	if response != nil && response.StatusCode >= 300 && response.StatusCode < 400 {
		stage, code = "response_headers", "redirect_error"
	}
	if errors.Is(contextErr, context.DeadlineExceeded) {
		code = "timeout"
	} else if errors.Is(contextErr, context.Canceled) {
		code = "canceled"
	}
	n.fail(e, stage, code)
}

// Receive is the proxy's body read window, including upstream wait and time
// between reads. It may overlap downstream writes; it is not pure download or
// browser-delivery time.
func (n *playerNetwork) receive(e *playerNetworkEntry, begin, finish bool) {
	if e == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.frozen || n.stopped || len(e.request.Hops) == 0 {
		return
	}
	index := len(e.request.Hops) - 1
	old := e.request.Hops[index]
	if old.Number != e.hopCount {
		return
	}
	hop := clonePlayerNetworkHops([]PlayerNetworkHop{old})[0]
	at := min(int64(maxPlayerNetworkOffsetMS), n.offset())
	if begin {
		if len(hop.Stages) >= maxPlayerNetworkHopStages {
			return
		}
		hop.Stages = append(hop.Stages, CaptureStage{Name: "receive", StartMS: hop.EndMS, EndMS: at})
		e.receiveActive = true
	} else if e.receiveActive && len(hop.Stages) > 0 && hop.Stages[len(hop.Stages)-1].Name == "receive" {
		hop.Stages[len(hop.Stages)-1].EndMS = at
	} else {
		return
	}
	hop.EndMS = at
	if finish {
		e.receiveActive = false
	}
	bytes := n.metadataBytes - playerHopBudgetBytes(old) + playerHopBudgetBytes(hop)
	if bytes > maxPlayerNetworkHopBytes {
		n.metadataBytes -= playerHopBudgetBytes(old)
		e.request.Hops = e.request.Hops[:index]
		n.droppedHopLocked(e)
		e.receiveActive = false
		return
	}
	n.metadataBytes = bytes
	e.request.Hops[index] = hop
}

func validatePlayerNetworkPhases(report *PlayerNetworkReport) error {
	metadata, dropped := 0, 0
	for _, request := range report.Requests {
		if report.Version == 1 {
			if request.PlaylistBoundary != nil || request.CacheAgeSeconds != nil || request.PlaylistSequence != nil || request.Hops != nil || request.DroppedHops != 0 || request.FailureStage != "" || request.FailureCode != "" || report.DroppedHops != 0 {
				return ErrInvalid
			}
			continue
		}
		if len(request.Hops) > maxPlayerNetworkHops || request.DroppedHops < 0 || request.DroppedHops > 1_000_000_000 || !validPlayerPhaseFailure(request.FailureStage, request.FailureCode, true) {
			return ErrInvalid
		}
		if request.Outcome == "complete" && request.FailureCode != "" {
			return ErrInvalid
		}
		dropped += request.DroppedHops
		lastNumber, lastEnd := 0, request.StartMS
		for index, hop := range request.Hops {
			if hop.Number <= lastNumber || hop.Number > maxPlayerNetworkHops || hop.Number > len(request.Hops)+request.DroppedHops || hop.StartMS < lastEnd || hop.EndMS < hop.StartMS || hop.EndMS > request.EndMS || (hop.HTTPStatus != 0 && (hop.HTTPStatus < 100 || hop.HTTPStatus > 599)) || len(hop.Stages) > maxPlayerNetworkHopStages || !validPlayerPhaseFailure(hop.FailureStage, hop.FailureCode, false) {
				return ErrInvalid
			}
			if hop.TTFBMS != nil && (*hop.TTFBMS < 0 || *hop.TTFBMS > hop.EndMS-hop.StartMS) {
				return ErrInvalid
			}
			previousStart := hop.StartMS
			firstByteEnd := hop.StartMS - 1
			firstHeaderOrReceive := hop.EndMS
			for stageIndex, stage := range hop.Stages {
				if !validPlayerPhaseStage(stage.Name, stage.Name == "receive") || stage.StartMS < previousStart || stage.EndMS < stage.StartMS || stage.EndMS > hop.EndMS || (stage.Name == "receive" && (index != len(request.Hops)-1 || stageIndex != len(hop.Stages)-1 || hop.Number != len(request.Hops)+request.DroppedHops)) {
					return ErrInvalid
				}
				previousStart = stage.StartMS
				if stage.Name == "response_first_byte" {
					firstByteEnd = stage.EndMS
				}
				if stage.Name == "response_headers" || stage.Name == "receive" {
					firstHeaderOrReceive = min(firstHeaderOrReceive, stage.StartMS)
				}
			}
			// Body reception can extend the hop long after its first byte.
			// Bound TTFB by the proven final first-byte point, or a later phase's
			// start when the transport did not expose that native callback.
			ttfbLimit := firstHeaderOrReceive - hop.StartMS
			if firstByteEnd >= hop.StartMS {
				ttfbLimit = firstByteEnd - hop.StartMS
			}
			if hop.TTFBMS != nil && *hop.TTFBMS > ttfbLimit {
				return ErrInvalid
			}
			metadata += playerHopBytes(hop)
			lastNumber, lastEnd = hop.Number, hop.EndMS
		}
	}
	if dropped != report.DroppedHops || metadata > maxPlayerNetworkHopBytes {
		return ErrInvalid
	}
	return nil
}

func validLegacyPlayerNetworkJSON(data []byte, version int) bool {
	if version != 1 {
		return true
	}
	var document map[string]json.RawMessage
	if json.Unmarshal(data, &document) != nil {
		return false
	}
	if _, exists := document["dropped_hops"]; exists {
		return false
	}
	var requests []map[string]json.RawMessage
	if json.Unmarshal(document["requests"], &requests) != nil {
		return false
	}
	for _, request := range requests {
		for _, key := range []string{"playlist_boundary", "cache_age_seconds", "playlist_sequence", "hops", "dropped_hops", "failure_stage", "failure_code"} {
			if _, exists := request[key]; exists {
				return false
			}
		}
	}
	return true
}
