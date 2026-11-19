package diagnostic

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"

	"streamanalyzer/internal/simulator/hlsorigin"
	"strings"
	"sync"
	"time"
)

const (
	playerHLSResourceLimit = 2048
	playerHLSURIBytesLimit = 4 << 20
)

type playerHLSResource struct {
	target    *url.URL
	playlist  bool
	kind      string
	rendition string
}

type playerHLS struct {
	client  *http.Client
	fetch   chan struct{}
	mu      sync.Mutex
	byID    map[string]playerHLSResource
	byURL   map[string]string
	bytes   int
	network *playerNetwork
}

func newPlayerHLS(target *url.URL, client *http.Client) *playerHLS {
	return &playerHLS{client: client, fetch: make(chan struct{}, 8), network: newPlayerNetwork(),
		byID:  map[string]playerHLSResource{"index.m3u8": {target: target, playlist: true, kind: "playlist"}},
		byURL: map[string]string{"p:" + target.String(): "index.m3u8"}, bytes: len(target.String()),
	}
}

func (h *playerHLS) close() {
	h.network.stop()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.byID, h.byURL = nil, nil
	h.bytes = 0
}

// ServeHLS proxies only the source URL or an opaque resource discovered in an
// upstream playlist. The local access token is never sent upstream.
func (p *PlayerSession) ServeHLS(w http.ResponseWriter, r *http.Request, resource string) {
	if p.hls == nil || p.ctx.Err() != nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	p.hls.serve(w, r.WithContext(ctx), resource)
}

func (h *playerHLS) serve(w http.ResponseWriter, r *http.Request, resource string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.mu.Lock()
	entry, ok := h.byID[resource]
	h.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	query := r.URL.Query()
	_, directiveErr := strconv.ParseUint(query.Get("_HLS_msn"), 10, 63)
	evidence := h.network.beginRendition(resource, entry.kind, r.Method, entry.rendition, entry.playlist && directiveErr == nil)
	outcome := "incomplete"
	defer func() { h.network.finish(evidence, outcome) }()
	select {
	case h.fetch <- struct{}{}:
		defer func() { <-h.fetch }()
	default:
		h.network.fail(evidence, "proxy_limit", "limit_exceeded")
		http.Error(w, "本地播放器请求过多", http.StatusServiceUnavailable)
		return
	}
	upstream, err := http.NewRequestWithContext(r.Context(), r.Method, entry.target.String(), nil)
	if err != nil {
		hlsPlaybackError(w)
		return
	}
	upstream.Header.Set("Accept-Encoding", "identity")
	upstream.Header.Set("User-Agent", hlsorigin.PlayerUserAgent)
	if entry.playlist {
		upstream.Header.Set("Accept", "application/vnd.apple.mpegurl, application/x-mpegURL, */*")
		appendPlayerReloadQuery(upstream.URL, query)
	} else if value := r.Header.Get("Range"); strings.HasPrefix(value, "bytes=") && len(value) <= 64 && !strings.Contains(value, ",") {
		upstream.Header.Set("Range", value)
	}
	resp, err := h.network.doRequest(h.client, upstream, evidence)
	if err != nil {
		h.network.requestError(evidence, err, r.Context().Err(), resp)
		if resp != nil {
			h.network.header(evidence, resp.StatusCode)
		}
		outcome = playerNetworkOutcome(err, r.Context().Err())
		hlsPlaybackError(w)
		return
	}
	defer resp.Body.Close()
	h.network.header(evidence, resp.StatusCode)
	observed := &playerNetworkReader{ReadCloser: resp.Body, network: h.network, entry: evidence}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		h.network.fail(evidence, "response_headers", "http_error")
		outcome = "http_error"
		if resp.StatusCode >= 400 && resp.StatusCode <= 599 {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "HLS 源返回错误状态", resp.StatusCode)
		} else {
			hlsPlaybackError(w)
		}
		return
	}
	if r.Method != http.MethodHead {
		h.network.receive(evidence, true, false)
		defer h.network.receive(evidence, false, true)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	isPlaylist := entry.playlist || strings.Contains(mediaType, "mpegurl")
	body := bufio.NewReader(observed)
	if !isPlaylist && r.Method != http.MethodHead {
		prefix, _ := body.Peek(10)
		isPlaylist = bytes.HasPrefix(bytes.TrimPrefix(prefix, []byte("\xef\xbb\xbf")), []byte("#EXTM3U"))
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if isPlaylist {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			outcome = "complete"
			return
		}
		data, err := io.ReadAll(io.LimitReader(body, hlsPlaylistLimit+1))
		if err != nil || len(data) > hlsPlaylistLimit {
			if err != nil {
				outcome = playerNetworkOutcome(err, r.Context().Err())
				h.network.fail(evidence, "receive", outcome)
			} else {
				h.network.fail(evidence, "proxy_limit", "limit_exceeded")
			}
			hlsPlaybackError(w)
			return
		}
		base := upstream.URL
		if resp.Request != nil {
			base = resp.Request.URL
		}
		rewritten, err := h.rewriteRendition(data, base, r.URL.Query().Get("token"), entry.rendition)
		if err != nil {
			h.network.fail(evidence, "playlist", "invalid_playlist")
			hlsPlaybackError(w)
			return
		}
		for line := range strings.SplitSeq(rewritten, "\n") {
			if value, ok := strings.CutPrefix(strings.TrimSpace(line), "#EXT-X-MEDIA-SEQUENCE:"); ok {
				if sequence, err := strconv.ParseUint(value, 10, 64); err == nil {
					h.network.playlistSequence(evidence, sequence)
					if age, err := strconv.ParseUint(resp.Header.Get("Age"), 10, 32); err == nil {
						h.network.playlistCacheAge(evidence, uint32(age))
					}
				}
				break
			}
		}
		if _, err := io.WriteString(w, rewritten); err != nil {
			outcome = "canceled"
			h.network.fail(evidence, "proxy_write", "proxy_write_error")
		} else {
			outcome = "complete"
		}
		return
	}
	if resp.ContentLength > hlsSegmentLimit {
		h.network.fail(evidence, "proxy_limit", "limit_exceeded")
		hlsPlaybackError(w)
		return
	}
	w.Header().Set("Content-Type", playerMediaType(resource, mediaType))
	if resp.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	if resp.StatusCode == http.StatusPartialContent {
		w.Header().Set("Content-Range", resp.Header.Get("Content-Range"))
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		count, err := io.Copy(w, io.LimitReader(body, hlsSegmentLimit))
		if err != nil {
			if observed.err != nil {
				outcome = playerNetworkOutcome(observed.err, r.Context().Err())
				h.network.fail(evidence, "receive", outcome)
			} else {
				outcome = "canceled"
				h.network.fail(evidence, "proxy_write", "proxy_write_error")
			}
			return
		}
		if count == hlsSegmentLimit && resp.ContentLength != count && !observed.eof {
			h.network.fail(evidence, "proxy_limit", "limit_exceeded")
			return
		}
	}
	outcome = "complete"
}

func hlsPlaybackError(w http.ResponseWriter) {
	http.Error(w, "无法读取本地播放器的 HLS 资源", http.StatusBadGateway)
}

func appendPlayerReloadQuery(target *url.URL, values url.Values) {
	for _, key := range []string{"_HLS_msn", "_HLS_part", "_HLS_skip"} {
		value := values.Get(key)
		_, err := strconv.ParseUint(value, 10, 63)
		if !(key == "_HLS_skip" && (value == "YES" || value == "v2")) && (key == "_HLS_skip" || err != nil) {
			continue
		}
		if target.RawQuery != "" {
			target.RawQuery += "&"
		}
		target.RawQuery += key + "=" + value
	}
}

func playerMediaType(resource, actual string) string {
	switch path.Ext(resource) {
	case ".ts":
		return "video/mp2t"
	case ".mp4", ".m4s":
		return "video/mp4"
	case ".aac":
		return "audio/aac"
	case ".mp3":
		return "audio/mpeg"
	case ".vtt":
		return "text/vtt"
	}
	switch actual {
	case "video/mp2t", "video/mp4", "audio/mp4", "audio/aac", "audio/mpeg", "text/vtt":
		return actual
	default:
		return "application/octet-stream"
	}
}

var errPlayerPlaylist = errors.New("invalid player playlist")

func (h *playerHLS) rewrite(data []byte, base *url.URL, token string) (string, error) {
	return h.rewriteRendition(data, base, token, "")
}

func (h *playerHLS) rewriteRendition(data []byte, base *url.URL, token, rendition string) (string, error) {
	text := strings.TrimPrefix(string(data), "\uFEFF")
	header, _, _ := strings.Cut(text, "\n")
	if strings.TrimSpace(header) != "#EXTM3U" {
		return "", errPlayerPlaylist
	}
	if strings.Contains(text, "{$") {
		return "", errPlayerPlaylist
	}
	localKind := func(ref string, playlist bool, kind, role string) (string, error) {
		target := resolveHLS(base, ref)
		if target == nil {
			return "", errPlayerPlaylist
		}
		id := h.rememberRendition(target, playlist, kind, role)
		if id == "" {
			return "", errPlayerPlaylist
		}
		return id + "?token=" + url.QueryEscape(token), nil
	}
	localURL := func(ref string, playlist bool) (string, error) { return localKind(ref, playlist, "media", rendition) }
	var out strings.Builder
	playlistURI := false
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		var err error
		if line != "" && !strings.HasPrefix(line, "#") {
			line, err = localURL(line, playlistURI)
			playlistURI = false
		} else if tag, attrs, _ := strings.Cut(line, ":"); tag == "#EXT-X-STREAM-INF" {
			playlistURI = true
		} else if tag == "#EXT-X-CONTENT-STEERING" {
			// Steering can introduce new origins that were not in the playlist.
			continue
		} else if strings.HasPrefix(tag, "#EXT-X-") && attrs != "" {
			switch tag {
			case "#EXT-X-MEDIA", "#EXT-X-I-FRAME-STREAM-INF", "#EXT-X-IMAGE-STREAM-INF", "#EXT-X-RENDITION-REPORT":
				role := ""
				if tag == "#EXT-X-MEDIA" && playerAttribute(attrs, "TYPE") == "AUDIO" {
					role = "audio"
				}
				attrs, err = rewritePlayerURI(attrs, func(ref string) (string, error) { return localKind(ref, true, "playlist", role) })
			case "#EXT-X-MAP", "#EXT-X-KEY", "#EXT-X-SESSION-KEY", "#EXT-X-PART", "#EXT-X-PRELOAD-HINT", "#EXT-X-SESSION-DATA":
				kind := "media"
				if tag == "#EXT-X-MAP" {
					kind = "init"
				} else if tag == "#EXT-X-KEY" || tag == "#EXT-X-SESSION-KEY" {
					kind = "key"
				} else if tag == "#EXT-X-PART" || tag == "#EXT-X-PRELOAD-HINT" && playerAttribute(attrs, "TYPE") == "PART" {
					kind = "part"
				} else if tag == "#EXT-X-SESSION-DATA" {
					kind = "auxiliary"
				}
				attrs, err = rewritePlayerURI(attrs, func(ref string) (string, error) { return localKind(ref, false, kind, rendition) })
			}
			line = tag + ":" + attrs
		}
		if err != nil {
			return "", err
		}
		out.WriteString(line)
		out.WriteByte('\n')
		if out.Len() > 2*hlsPlaylistLimit {
			return "", errPlayerPlaylist
		}
	}
	return out.String(), nil
}

func playerAttribute(attrs, name string) string {
	start := 0
	quoted := false
	for i := 0; i <= len(attrs); i++ {
		if i < len(attrs) && attrs[i] == '"' {
			quoted = !quoted
		}
		if i < len(attrs) && (quoted || attrs[i] != ',') {
			continue
		}
		key, value, ok := strings.Cut(attrs[start:i], "=")
		if ok && strings.TrimSpace(key) == name {
			return strings.TrimSpace(value)
		}
		start = i + 1
	}
	return ""
}

// Attribute lists use literal quotes and may include commas inside them.
func rewritePlayerURI(attrs string, localURL func(string) (string, error)) (string, error) {
	inside := false
	start := 0
	var result string
	for i := 0; i <= len(attrs); i++ {
		if i < len(attrs) && attrs[i] == '"' {
			inside = !inside
		}
		if i < len(attrs) && (inside || attrs[i] != ',') {
			continue
		}
		key, value, assigned := strings.Cut(attrs[start:i], "=")
		if assigned && strings.TrimSpace(key) == "URI" {
			ref := strings.TrimSpace(value)
			if result != "" || len(ref) < 2 || ref[0] != '"' || ref[len(ref)-1] != '"' {
				return "", errPlayerPlaylist
			}
			local, err := localURL(ref[1 : len(ref)-1])
			if err != nil {
				return "", err
			}
			result = attrs[:start] + `URI="` + local + `"` + attrs[i:]
		}
		start = i + 1
	}
	if inside {
		return "", errPlayerPlaylist
	}
	if result != "" {
		return result, nil
	}
	return attrs, nil
}

func (h *playerHLS) remember(target *url.URL, playlist bool) string {
	return h.rememberKind(target, playlist, "media")
}

func (h *playerHLS) rememberKind(target *url.URL, playlist bool, kind string) string {
	return h.rememberRendition(target, playlist, kind, "")
}

func (h *playerHLS) rememberRendition(target *url.URL, playlist bool, kind, rendition string) string {
	key := "a:" + kind + ":" + target.String()
	ext := strings.ToLower(path.Ext(target.Path))
	if playlist || ext == ".m3u8" || ext == ".m3u" {
		playlist, ext, key = true, ".m3u8", "p:"+target.String()
		kind = "playlist"
	}
	if rendition != "" {
		key = rendition + ":" + key
	}
	switch ext {
	case ".m3u8", ".ts", ".m4s", ".mp4", ".aac", ".mp3", ".vtt":
	default:
		ext = ".bin"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if id := h.byURL[key]; id != "" {
		return id
	}
	if h.byURL == nil || len(h.byURL) >= playerHLSResourceLimit || len(key) > playerHLSURIBytesLimit-h.bytes {
		return ""
	}
	id := "r" + strconv.Itoa(len(h.byURL)) + ext
	h.byID[id], h.byURL[key] = playerHLSResource{target: target, playlist: playlist, kind: kind, rendition: rendition}, id
	h.bytes += len(key)
	return id
}
