package diagnostic

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"streamanalyzer/internal/hlsdelivery/m3u8"
)

const (
	hlsMaxRecords     = 1024
	hlsZipOverhead    = 256 << 10
	hlsPlaylistLimit  = 1 << 20
	hlsInitLimit      = 4 << 20
	hlsSegmentLimit   = 32 << 20
	maxHLSRecordBytes = 256 << 10

	hlsPlaylist = "hls_playlist"
	hlsInit     = "hls_init"
	hlsSegment  = "hls_segment"
	hlsGap      = "hls_gap"
	hlsMain     = "main"
	hlsAudio    = "audio"
)

type hlsRecord struct {
	Requests   []HTTPRequestTiming `json:"requests,omitempty"`
	Kind       string              `json:"kind"`
	Rendition  string              `json:"rendition"`
	Result     string              `json:"result"`
	Artifact   string              `json:"artifact,omitempty"`
	Playlist   int                 `json:"playlist,omitempty"`
	Sequence   *int                `json:"sequence,omitempty"`
	Init       int                 `json:"init,omitempty"`
	Bytes      int                 `json:"bytes"`
	ArrivalMS  int64               `json:"arrival_ms"`
	DurationMS int64               `json:"duration_ms"`
	TTFBMS     *int64              `json:"ttfb_ms,omitempty"`
	HTTPStatus int                 `json:"http_status,omitempty"`
}

type hlsCapture struct {
	client      *http.Client
	start       time.Time
	archive     *zip.Writer
	ledger      io.Writer
	ledgerBytes int64
	count       int
	remaining   int64
	observation *Observation
	variant     *int
	bandwidth   int
}

func sampleHLS(ctx context.Context, client *http.Client, dir string, target *url.URL, seconds int) (Report, error) {
	stream, err := newArtifact(dir, hlsStreamName)
	if err != nil {
		return Report{}, err
	}
	timeline, err := newArtifact(dir, timelineName)
	if err != nil {
		return Report{}, errors.Join(err, stream.close())
	}

	start := time.Now()
	obs := Observation{Origin: origin(target), StartedAt: start.UTC(), RequestedSeconds: seconds}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	capture := hlsCapture{
		client: client, start: start, archive: zip.NewWriter(stream), ledger: timeline,
		remaining:   max(0, captureLimit(ctx)-hlsZipOverhead),
		observation: &obs,
	}
	reason, err := capture.run(ctx, target, &obs)
	obs.EndReason = reason
	obs.ElapsedMS = time.Since(start).Milliseconds()
	if err := errors.Join(err, capture.archive.Close(), stream.close(), timeline.close()); err != nil {
		return Report{}, err
	}
	manifest := Manifest{
		Version: multiProtocolSchemaVersion, Protocol: ProtocolHLS, Observation: obs,
		HLSVariant: capture.variant, HLSVariantBandwidth: capture.bandwidth,
		Stream: stream.artifact(), Timeline: timeline.artifact(),
	}
	applyAnalysisConfig(ctx, &manifest)
	return replayHLS(dir, manifest)
}

type hlsCaptureTrack struct {
	name      string
	target    *url.URL
	base      *url.URL
	playlist  int
	mediaSeen bool
	endList   bool
	delay     time.Duration
	nextPoll  time.Time
	queued    []m3u8.Segment
	seen      map[int]bool
	inits     map[hlsInitKey]int
	floor     int
	done      bool
}

func newHLSCaptureTrack(name string, target *url.URL) *hlsCaptureTrack {
	return &hlsCaptureTrack{
		name: name, target: target, seen: make(map[int]bool), inits: make(map[hlsInitKey]int),
	}
}

func (t *hlsCaptureTrack) setPlaylist(pl *m3u8.Playlist, base *url.URL, id int) {
	t.base, t.playlist, t.endList = base, id, pl.EndList
	t.delay = time.Duration(min(max(hlsMS(pl.TargetDuration)/2, 500), 5000)) * time.Millisecond
	if len(t.seen) == 0 && !pl.EndList && len(pl.Segments) > 3 {
		t.floor = pl.Segments[len(pl.Segments)-3].Sequence
	}
	t.mediaSeen = true
	for _, segment := range pl.Segments {
		if segment.Sequence >= t.floor && !t.seen[segment.Sequence] {
			t.queued = append(t.queued, segment)
		}
	}
	t.advance()
}

func (t *hlsCaptureTrack) advance() {
	if len(t.queued) > 0 {
		t.nextPoll = time.Time{}
		return
	}
	t.nextPoll = time.Now().Add(t.delay)
	t.done = t.endList
}

func (c *hlsCapture) run(ctx context.Context, target *url.URL, obs *Observation) (string, error) {
	if reason := c.end(ctx); reason != "" {
		return reason, nil
	}
	initial := hlsRecord{Kind: hlsPlaylist, Rendition: hlsMain}
	row, body, base := c.fetch(ctx, target, initial, nil)
	obs.HTTPStatus = row.HTTPStatus
	if err := c.record(row, body); err != nil {
		return "", err
	}
	if row.Result != "ok" {
		return captureEnd(ctx, hlsEndReason(row.Result)), nil
	}
	pl, err := decodeHLSPlaylist(body)
	if err != nil {
		return "hls_error", nil
	}

	var masterID int
	main := newHLSCaptureTrack(hlsMain, target)
	tracks := []*hlsCaptureTrack{main}
	if pl.IsMaster() {
		masterID = 1
		variant, index, ok := selectHLSVariant(pl, captureAccessFromContext(ctx).hlsVariant)
		if !ok {
			return "hls_error", nil
		}
		c.variant, c.bandwidth = &index, max(0, variant.Bandwidth)
		main.target = resolveHLS(base, variant.URI)
		if audio := selectedHLSAudio(pl, variant.AudioGroup); audio != "" {
			tracks = append(tracks, newHLSCaptureTrack(hlsAudio, resolveHLS(base, audio)))
		}
	} else {
		main.setPlaylist(pl, base, 1)
	}

	mainEnd := "eof"
	for {
		if reason := c.end(ctx); reason != "" {
			return reason, nil
		}
		var next time.Time
		active := false
		for _, track := range tracks {
			if track.done {
				continue
			}
			if reason := c.end(ctx); reason != "" {
				return reason, nil
			}
			if !time.Now().Before(track.nextPoll) {
				failure, err := c.step(ctx, track, masterID)
				if err != nil {
					return "", err
				}
				if failure != "" && (track == main || main.done) {
					track.done = true
					if track == main {
						mainEnd = failure
					}
				}
			}
			if !track.done && (!active || track.nextPoll.Before(next)) {
				next = track.nextPoll
				active = true
			}
		}
		if reason := c.end(ctx); reason != "" {
			return reason, nil
		}
		if !active {
			return captureEnd(ctx, mainEnd), nil
		}
		if delay := time.Until(next); delay > 0 {
			waitHLS(ctx, delay)
		}
	}
}

func (c *hlsCapture) step(ctx context.Context, track *hlsCaptureTrack, masterID int) (string, error) {
	if len(track.queued) > 0 {
		segment := track.queued[0]
		track.queued = track.queued[1:]
		if err := c.captureSegment(ctx, track, segment); err != nil {
			return "", err
		}
		track.seen[segment.Sequence] = true
		track.advance()
		return "", nil
	}

	record := hlsRecord{Kind: hlsPlaylist, Rendition: track.name, Playlist: masterID}
	row, body, base := c.fetch(ctx, track.target, record, nil)
	if err := c.record(row, body); err != nil {
		return "", err
	}
	track.nextPoll = time.Now().Add(time.Second)
	if row.Result != "ok" {
		if !track.mediaSeen {
			return hlsEndReason(row.Result), nil
		}
		return "", nil
	}
	pl, err := decodeHLSPlaylist(body)
	if err != nil {
		if !track.mediaSeen {
			return "hls_error", nil
		}
		return "", nil
	}
	if pl.IsMaster() {
		return "hls_error", nil
	}
	track.setPlaylist(pl, base, c.count)
	return "", nil
}

type hlsInitKey struct {
	URL    string
	Ref    string
	Range  m3u8.ByteRange
	Ranged bool
}

func (c *hlsCapture) captureSegment(ctx context.Context, track *hlsCaptureTrack, segment m3u8.Segment) error {
	row := hlsRecord{Kind: hlsSegment, Rendition: track.name, Playlist: track.playlist, Sequence: &segment.Sequence}
	if segment.Gap {
		row.Kind, row.Result = hlsGap, "gap"
		row.ArrivalMS = time.Since(c.start).Milliseconds()
		return c.record(row, nil)
	}
	if segment.Map != nil {
		target := resolveHLS(track.base, segment.Map.URI)
		key := hlsInitKey{Ref: segment.Map.URI}
		if target != nil {
			key.URL = target.String()
		}
		if segment.Map.ByteRange != nil {
			key.Range, key.Ranged = *segment.Map.ByteRange, true
		}
		row.Init = track.inits[key]
		if row.Init == 0 {
			init, body, _ := c.fetch(ctx, target, hlsRecord{Kind: hlsInit, Rendition: track.name, Playlist: track.playlist, Sequence: row.Sequence}, segment.Map.ByteRange)
			row.Init = c.count + 1
			if err := c.record(init, body); err != nil {
				return err
			}
			if init.Result == "ok" {
				track.inits[key] = row.Init
			}
		}
	}
	if c.end(ctx) != "" {
		return nil
	}
	row, body, _ := c.fetch(ctx, resolveHLS(track.base, segment.URI), row, segment.ByteRange)
	return c.record(row, body)
}

func (c *hlsCapture) end(ctx context.Context) string {
	if c.remaining == 0 || c.ledgerBytes >= maxTimeline-maxHLSRecordBytes {
		return "size_limit"
	}
	if c.count >= hlsMaxRecords {
		return "tag_limit"
	}
	return captureEnd(ctx, "")
}

func waitHLS(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (c *hlsCapture) record(row hlsRecord, body []byte) error {
	c.count++
	if len(body) != 0 || row.Result == "ok" {
		row.Artifact = hlsArtifactName(row.Kind, c.count)
		row.Bytes = len(body)
		entry, err := c.archive.CreateHeader(&zip.FileHeader{Name: row.Artifact, Method: zip.Store})
		if err != nil {
			return err
		}
		if _, err := entry.Write(body); err != nil {
			return err
		}
		c.remaining -= int64(len(body))
	}
	data, err := json.Marshal(row)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxHLSRecordBytes || int64(len(data)) > maxTimeline-c.ledgerBytes {
		return fmt.Errorf("%w: HLS 请求元数据超出大小上限", ErrInvalid)
	}
	n, err := c.ledger.Write(data)
	c.ledgerBytes += int64(n)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func hlsArtifactName(kind string, id int) string {
	switch kind {
	case hlsPlaylist:
		return fmt.Sprintf("playlist-%04d.m3u8", id)
	case hlsInit:
		return fmt.Sprintf("init-%04d.bin", id)
	case hlsSegment:
		return fmt.Sprintf("segment-%04d.bin", id)
	default:
		return ""
	}
}

func hlsObjectLimit(kind string) int64 {
	switch kind {
	case hlsPlaylist:
		return hlsPlaylistLimit
	case hlsInit:
		return hlsInitLimit
	default:
		return hlsSegmentLimit
	}
}

func (c *hlsCapture) fetch(ctx context.Context, target *url.URL, row hlsRecord, byteRange *m3u8.ByteRange) (result hlsRecord, body []byte, final *url.URL) {
	startMS := time.Since(c.start).Milliseconds()
	var timing Observation
	bodyEndMS := int64(-1)
	result = row
	defer func() {
		result.ArrivalMS = time.Since(c.start).Milliseconds()
		result.DurationMS = result.ArrivalMS - startMS
		if bodyEndMS >= 0 && len(timing.Requests) > 0 {
			last := timing.Requests[len(timing.Requests)-1]
			stage := CaptureStage{Name: "receive", StartMS: last.EndMS, EndMS: bodyEndMS}
			timing.Stages = append(timing.Stages, stage)
			recordHTTPBody(&timing, stage)
		}
		result.Requests = timing.Requests
		if timing.TTFBMS != nil {
			ms := *timing.TTFBMS - startMS
			result.TTFBMS = &ms
		}
		if c.count == 0 && c.observation != nil {
			c.observation.Stages = timing.Stages
			c.observation.Requests = timing.Requests
			c.observation.TTFBMS = timing.TTFBMS
			c.observation.FailureStage, c.observation.FailureCode = timing.FailureStage, timing.FailureCode
		}
	}()

	result.Result = "request_error"
	if target == nil {
		return result, nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return result, nil, nil
	}
	applyCaptureHeaders(ctx, req)
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "StreamAnalyzer-Diagnostic/1.0")
	}
	req.Header.Set("Accept-Encoding", "identity")
	if row.Kind == hlsPlaylist && req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.apple.mpegurl, application/x-mpegURL, */*")
	}
	var rangeStart, rangeEnd int64
	if byteRange != nil {
		rangeStart = int64(byteRange.Offset)
		if rangeStart < 0 || byteRange.Length <= 0 || int64(byteRange.Length-1) > math.MaxInt64-rangeStart {
			result.Result = "range_error"
			return result, nil, nil
		}
		rangeEnd = rangeStart + int64(byteRange.Length-1)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeStart, rangeEnd))
	}
	resp, err := doObservedRequest(c.client, req, &timing, c.start)
	if err != nil {
		return result, nil, nil
	}
	defer resp.Body.Close()
	result.HTTPStatus = resp.StatusCode

	if resp.StatusCode != http.StatusOK && (byteRange == nil || resp.StatusCode != http.StatusPartialContent) {
		result.Result = "http_status"
		timing.FailureStage, timing.FailureCode = "response_headers", "http_status"
		if len(timing.Requests) > 0 {
			last := &timing.Requests[len(timing.Requests)-1]
			last.FailureStage, last.FailureCode = timing.FailureStage, timing.FailureCode
		}
		return result, nil, resp.Request.URL
	}
	if byteRange != nil && !matchesHLSRange(resp, rangeStart, rangeEnd) {
		result.Result = "range_error"
		return result, nil, resp.Request.URL
	}

	limit := min(c.remaining, hlsObjectLimit(row.Kind))
	var payload io.Reader = resp.Body
	if c.count == 0 && c.observation != nil {
		payload = io.TeeReader(resp.Body, payloadWriter{io.Discard, c.observation, c.start})
	}
	body, err = io.ReadAll(io.LimitReader(payload, limit+1))
	bodyEndMS = time.Since(c.start).Milliseconds()
	switch {
	case int64(len(body)) > limit:
		body, result.Result = body[:limit], "size_limit"
	case err != nil:
		result.Result = "body_error"
	case byteRange != nil && len(body) != byteRange.Length:
		result.Result = "range_error"
	case len(body) == 0:
		result.Result = "body_error"
	default:
		result.Result = "ok"
	}
	if result.Result == "body_error" {
		timing.FailureStage, timing.FailureCode = "receive", "body_error"
		if err != nil {
			switch code := modelNetworkError(err, 0).Code; code {
			case "timeout", "canceled":
				timing.FailureCode = code
			}
		}
	}
	return result, body, resp.Request.URL
}

func matchesHLSRange(resp *http.Response, start, end int64) bool {
	if resp.StatusCode != http.StatusPartialContent {
		return false
	}
	total, match := strings.CutPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-%d/", start, end))
	if !match {
		return false
	}
	if total == "*" {
		return true
	}
	length, err := strconv.ParseInt(total, 10, 64)
	return err == nil && length > end
}

func resolveHLS(base *url.URL, reference string) *url.URL {
	if base == nil {
		return nil
	}
	parsed, err := url.Parse(reference)
	if err != nil || reference == "" {
		return nil
	}
	target, err := parseTarget(base.ResolveReference(parsed).String())
	if err != nil {
		return nil
	}
	return target
}

func decodeHLSPlaylist(body []byte) (*m3u8.Playlist, error) {
	text := strings.TrimPrefix(string(body), "\uFEFF")
	header, _, _ := strings.Cut(text, "\n")
	if strings.TrimSpace(header) != "#EXTM3U" {
		return nil, errors.New("invalid HLS playlist header")
	}
	playlist, err := m3u8.Parse(text)
	if err != nil || playlist.MediaSequence < 0 || playlist.MediaSequence > math.MaxInt-len(playlist.Segments) {
		return nil, errors.New("invalid HLS playlist")
	}
	return playlist, nil
}

func highestHLSVariant(playlist *m3u8.Playlist) m3u8.Variant {
	var highest m3u8.Variant
	for _, variant := range playlist.Variants {
		if !variant.IFrame && (highest.URI == "" || variant.Bandwidth > highest.Bandwidth) {
			highest = variant
		}
	}
	return highest
}

func selectHLSVariant(playlist *m3u8.Playlist, requested *int) (m3u8.Variant, int, bool) {
	if requested != nil && *requested >= 0 {
		index := 0
		for _, variant := range playlist.Variants {
			if variant.IFrame {
				continue
			}
			if index == *requested {
				return variant, index, true
			}
			index++
		}
		return m3u8.Variant{}, 0, false
	}
	variant := highestHLSVariant(playlist)
	index := 0
	for _, candidate := range playlist.Variants {
		if candidate.IFrame {
			continue
		}
		if candidate.URI == variant.URI {
			return variant, index, variant.URI != ""
		}
		index++
	}
	return m3u8.Variant{}, 0, false
}

func selectedHLSAudio(pl *m3u8.Playlist, group string) string {
	if group == "" {
		return ""
	}
	var first, auto string
	for _, rendition := range pl.Renditions {
		if rendition.Type != "AUDIO" || rendition.GroupID != group {
			continue
		}
		if rendition.Default {
			return rendition.URI
		}
		if first == "" {
			first = rendition.URI
		}
		if auto == "" && rendition.AutoSelect {
			auto = rendition.URI
		}
	}
	if auto != "" {
		return auto
	}
	return first
}

func hlsMS(seconds float64) int64 {
	if math.IsNaN(seconds) || seconds <= 0 || seconds > 86400 {
		return 0
	}
	return int64(math.Round(seconds * 1000))
}

func hlsEndReason(result string) string {
	switch result {
	case "request_error", "http_status", "body_error":
		return result
	default:
		return "hls_error"
	}
}
