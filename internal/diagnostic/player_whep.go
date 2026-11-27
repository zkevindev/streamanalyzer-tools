package diagnostic

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// playerWHEP proxies browser WebRTC WHEP signaling (POST Offer / DELETE teardown)
// on the loopback origin so the browser avoids CORS issues and never sees upstream
// credentials or custom headers.
type playerWHEP struct {
	target  *url.URL
	client  *http.Client
	headers http.Header

	mu          sync.Mutex
	locationURL string
}

func newPlayerWHEP(target *url.URL, client *http.Client) *playerWHEP {
	if client == nil {
		client = captureClient()
	}
	return &playerWHEP{
		target: target,
		client: client,
	}
}

func (w *playerWHEP) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if w == nil || w.target == nil {
		http.Error(rw, "whep player proxy unavailable", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodPost:
		w.handleOffer(rw, r)
	case http.MethodDelete:
		w.handleDelete(rw, r)
	default:
		rw.Header().Set("Allow", "POST, DELETE")
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (w *playerWHEP) handleOffer(rw http.ResponseWriter, r *http.Request) {
	offerBytes, err := io.ReadAll(io.LimitReader(r.Body, 512<<10))
	if err != nil || len(offerBytes) == 0 {
		http.Error(rw, "empty sdp offer", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, w.target.String(), strings.NewReader(string(offerBytes)))
	if err != nil {
		http.Error(rw, "invalid upstream target", http.StatusBadRequest)
		return
	}
	upReq.Header.Set("Content-Type", "application/sdp")
	upReq.Header.Set("Accept", "application/sdp")
	for k, vals := range w.headers {
		if strings.EqualFold(k, "Content-Type") || strings.EqualFold(k, "Accept") || strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vals {
			upReq.Header.Add(k, v)
		}
	}
	resp, err := w.client.Do(upReq)
	if err != nil {
		http.Error(rw, "upstream whep signaling failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if loc := strings.TrimSpace(resp.Header.Get("Location")); loc != "" {
		if resolved, rerr := resolveWHEPLocation(w.target, loc); rerr == nil {
			w.mu.Lock()
			w.locationURL = resolved.String()
			w.mu.Unlock()
		}
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		rw.Header().Set("Content-Type", ct)
	} else {
		rw.Header().Set("Content-Type", "application/sdp")
	}
	for _, lk := range resp.Header.Values("Link") {
		rw.Header().Add("Link", lk)
	}
	rw.WriteHeader(resp.StatusCode)
	_, _ = rw.Write(body)
}

func (w *playerWHEP) handleDelete(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	loc := w.locationURL
	w.locationURL = ""
	w.mu.Unlock()
	if loc == "" {
		rw.WriteHeader(http.StatusNoContent)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	upReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, loc, nil)
	if err != nil {
		rw.WriteHeader(http.StatusNoContent)
		return
	}
	for k, vals := range w.headers {
		for _, v := range vals {
			upReq.Header.Add(k, v)
		}
	}
	if resp, err := w.client.Do(upReq); err == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}
	rw.WriteHeader(http.StatusNoContent)
}

func (w *playerWHEP) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	loc := w.locationURL
	w.locationURL = ""
	w.mu.Unlock()
	if loc == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if upReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, loc, nil); err == nil {
		for k, vals := range w.headers {
			for _, v := range vals {
				upReq.Header.Add(k, v)
			}
		}
		if resp, err := w.client.Do(upReq); err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
		}
	}
}
