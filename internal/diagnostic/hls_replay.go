package diagnostic

import (
	"archive/zip"
	"bufio"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"streamanalyzer/internal/hlsdelivery/m3u8"
	"streamanalyzer/internal/hlsdelivery/media"
)

const maxHLSReportRequests = 64

type HLSMetrics struct {
	PlaylistsCaptured     int   `json:"playlists_captured"`
	PlaylistFailures      int   `json:"playlist_failures"`
	InitSegmentsCaptured  int   `json:"init_segments_captured"`
	InitSegmentsFailed    int   `json:"init_segments_failed"`
	SegmentsCaptured      int   `json:"segments_captured"`
	AudioSegmentsCaptured int   `json:"audio_segments_captured"`
	SegmentsFailed        int   `json:"segments_failed"`
	SegmentsSkipped       int   `json:"segments_skipped"`
	EncryptedSegments     int   `json:"encrypted_segments"`
	MediaBytes            int64 `json:"media_bytes"`
	MaxSegmentDownloadMS  int64 `json:"max_segment_download_ms"`
	RenditionBandwidth    int   `json:"rendition_bandwidth"`
}

type hlsReplay struct {
	report       Report
	seenFindings map[string]bool
	masterID     int
	audioAllowed bool
	tracks       map[string]*hlsReplayTrack
	inits        map[int]hlsReplayInit
}

type hlsReplayTrack struct {
	playlist      *m3u8.Playlist
	playlistID    int
	seenSegments  map[int]bool
	latestSegment int
	latestEvent   *Event
	cachedInitID  int
	cachedInit    []byte
}

type hlsReplayInit struct {
	rendition string
	resource  m3u8.Map
	file      *zip.File
}

func replayHLS(dir string, manifest Manifest) (Report, error) {
	archive, archiveFile, err := openEvidenceZIP(filepath.Join(dir, hlsStreamName))
	if err != nil {
		return Report{}, hlsInvalid("原始 HLS ZIP 不可读")
	}
	defer archiveFile.Close()
	entries, err := hlsArchiveEntries(archive.File)
	if err != nil {
		return Report{}, err
	}

	timeline, err := os.Open(filepath.Join(dir, timelineName))
	if err != nil {
		return Report{}, err
	}
	defer timeline.Close()
	scanner := bufio.NewScanner(timeline)
	// The maximum 8 HTTP hops can each contain 16 stages.
	scanner.Buffer(make([]byte, 1024), 32<<10)

	replay := hlsReplay{
		report: Report{
			RuleVersion: "hls/3", Manifest: manifest, HLS: &HLSMetrics{}, Findings: []Finding{},
			Metrics: Metrics{MediaTimingUnit: "segment"},
		},
		seenFindings: make(map[string]bool), tracks: make(map[string]*hlsReplayTrack), inits: make(map[int]hlsReplayInit),
	}
	var lastArrival int64
	id := 0
	for scanner.Scan() {
		id++
		var row hlsRecord
		if id > hlsMaxRecords || json.Unmarshal(scanner.Bytes(), &row) != nil ||
			!validHLSRecord(row, id, lastArrival, manifest.Observation.ElapsedMS) {
			return Report{}, hlsInvalid("HLS 请求记录无效")
		}
		if id == 1 && !validHLSInitialRequest(row, manifest.Observation) {
			return Report{}, hlsInvalid("HLS 入口请求与采样元数据不一致")
		}
		replay.recordRequests(row)

		lastArrival = row.ArrivalMS
		var entry *zip.File
		var body []byte
		if row.Artifact != "" {
			entry = entries[row.Artifact]
			if entry == nil || entry.UncompressedSize64 != uint64(row.Bytes) {
				return Report{}, hlsInvalid("HLS 请求记录与 ZIP 原始产物不一致")
			}
			delete(entries, row.Artifact)
			body, err = readHLSObject(entry)
			if err != nil {
				return Report{}, err
			}
		}
		if err := replay.observe(id, row, body, entry); err != nil {
			return Report{}, err
		}
	}
	if err := scanner.Err(); err != nil {
		return Report{}, hlsInvalid("HLS 请求记录不可读")
	}
	if len(entries) != 0 {
		return Report{}, hlsInvalid("HLS ZIP 包含未引用的原始产物")
	}
	return replay.finish(), nil
}

func hlsInvalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalid, message)
}

func hlsArchiveEntries(files []*zip.File) (map[string]*zip.File, error) {
	if len(files) > hlsMaxRecords {
		return nil, hlsInvalid("HLS ZIP 条目数量超限")
	}
	entries := make(map[string]*zip.File, len(files))
	var bytes uint64
	for _, entry := range files {
		if !entry.Mode().IsRegular() || entry.Method != zip.Store ||
			entry.CompressedSize64 != entry.UncompressedSize64 ||
			entry.UncompressedSize64 > hlsSegmentLimit || entries[entry.Name] != nil {
			return nil, hlsInvalid("HLS ZIP 条目无效")
		}
		bytes += entry.UncompressedSize64
		if bytes > maxStreamBytes-hlsZipOverhead {
			return nil, hlsInvalid("HLS ZIP 原始产物总大小超限")
		}
		entries[entry.Name] = entry
	}
	return entries, nil
}

func readHLSObject(entry *zip.File) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, hlsInvalid("HLS ZIP 原始产物不可读")
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, int64(entry.UncompressedSize64)+1))
	if err != nil || uint64(len(body)) != entry.UncompressedSize64 {
		return nil, hlsInvalid("HLS ZIP 原始产物校验失败")
	}
	return body, nil
}

func validHLSRecord(row hlsRecord, id int, lastArrival, elapsed int64) bool {
	if (row.Rendition != hlsMain && row.Rendition != hlsAudio) || row.ArrivalMS < lastArrival || row.ArrivalMS > elapsed ||
		row.DurationMS < 0 || row.DurationMS > row.ArrivalMS ||
		(row.TTFBMS != nil && (*row.TTFBMS < 0 || *row.TTFBMS > row.DurationMS)) ||
		(row.HTTPStatus != 0 && (row.HTTPStatus < 100 || row.HTTPStatus > 599)) ||
		row.Bytes < 0 || int64(row.Bytes) > hlsObjectLimit(row.Kind) ||
		row.Playlist < 0 || row.Playlist >= id || row.Init < 0 || row.Init >= id {
		return false
	}
	if len(row.Requests) > 0 {
		startMS := row.ArrivalMS - row.DurationMS
		if row.Kind == hlsGap || !validHTTPRequests(row.Requests, max(lastArrival, startMS), row.ArrivalMS) {
			return false
		}
		last := row.Requests[len(row.Requests)-1]
		if row.Result != "request_error" && row.HTTPStatus != last.HTTPStatus {
			return false
		}
		if (last.TTFBMS == nil) != (row.TTFBMS == nil) ||
			(last.TTFBMS != nil && *row.TTFBMS != last.StartMS+*last.TTFBMS-startMS) {
			return false
		}
	}
	if row.Artifact != "" && hlsArtifactName(row.Kind, id) != row.Artifact {
		return false
	}
	if (row.Bytes > 0) != (row.Artifact != "") {
		return false
	}
	switch row.Kind {
	case hlsPlaylist:
		if row.Sequence != nil || row.Init != 0 {
			return false
		}
	case hlsInit, hlsSegment, hlsGap:
		if row.Sequence == nil || *row.Sequence < 0 || row.Playlist == 0 ||
			(row.Kind != hlsSegment && row.Init != 0) {
			return false
		}
	default:
		return false
	}
	if row.Kind == hlsGap {
		return row.Result == "gap" && row.Bytes == 0 && row.HTTPStatus == 0 && row.DurationMS == 0 && row.TTFBMS == nil
	}
	switch row.Result {
	case "ok":
		return row.Bytes > 0 && (row.HTTPStatus == http.StatusOK || row.HTTPStatus == http.StatusPartialContent)
	case "http_status":
		return row.Bytes == 0 && row.HTTPStatus != 0
	case "request_error":
		return row.Bytes == 0 && row.HTTPStatus == 0
	case "body_error", "size_limit":
		return row.HTTPStatus == http.StatusOK || row.HTTPStatus == http.StatusPartialContent
	case "range_error":
		return row.Kind != hlsPlaylist
	default:
		return false
	}
}

func validHLSInitialRequest(row hlsRecord, observation Observation) bool {
	if row.Kind != hlsPlaylist || row.Rendition != hlsMain || row.HTTPStatus != observation.HTTPStatus {
		return false
	}
	ttfb := row.TTFBMS
	if len(row.Requests) > 0 {
		if !reflect.DeepEqual(row.Requests, observation.Requests) {
			return false
		}
		last := row.Requests[len(row.Requests)-1]
		ttfb = nil
		if last.TTFBMS != nil {
			firstByte := last.StartMS + *last.TTFBMS
			ttfb = &firstByte
		}
	}
	return reflect.DeepEqual(ttfb, observation.TTFBMS)
}

func (r *hlsReplay) recordRequests(row hlsRecord) {
	for _, request := range row.Requests {
		if len(r.report.Requests) >= maxHLSReportRequests {
			r.report.RequestsTruncated++
			continue
		}
		request.Number = len(r.report.Requests) + 1
		request.Kind = row.Kind
		r.report.Requests = append(r.report.Requests, request)
	}
}

func (r *hlsReplay) observe(id int, row hlsRecord, body []byte, entry *zip.File) error {
	if row.Rendition == hlsAudio && !r.audioAllowed {
		return hlsInvalid("HLS 请求记录包含未声明的独立音频变体")
	}
	track := r.tracks[row.Rendition]
	if track == nil {
		track = &hlsReplayTrack{seenSegments: make(map[int]bool)}
		r.tracks[row.Rendition] = track
	}
	event := Event{
		Rendition: row.Rendition, Kind: row.Kind, Artifact: row.Artifact, Sequence: row.Sequence, Size: row.Bytes,
		ArrivalMS: row.ArrivalMS, HTTPStatus: row.HTTPStatus, DurationMS: row.DurationMS,
	}
	if row.Kind == hlsPlaylist {
		return r.observePlaylist(track, id, row, event, body)
	}
	if row.Playlist != track.playlistID || track.playlist == nil {
		return hlsInvalid("HLS 分片缺少对应的媒体播放列表")
	}
	index := *row.Sequence - track.playlist.MediaSequence
	if index < 0 || index >= len(track.playlist.Segments) {
		return hlsInvalid("HLS 请求的媒体序号不在所引用的播放列表")
	}
	segment := track.playlist.Segments[index]
	if segment.Gap != (row.Kind == hlsGap) {
		return hlsInvalid("HLS 请求记录与播放列表的 EXT-X-GAP 不一致")
	}
	if row.Kind == hlsInit {
		if segment.Map == nil {
			return hlsInvalid("HLS 初始化段缺少 EXT-X-MAP")
		}
		if row.Result != "ok" {
			r.report.HLS.InitSegmentsFailed++
			r.addRequestFinding("hls_init_fetch_failed", "初始化段", row, event)
			entry = nil
		} else {
			if !validHLSBodyRange(row, segment.Map.ByteRange) {
				return hlsInvalid("HLS 初始化段与字节范围不一致")
			}
			r.report.HLS.InitSegmentsCaptured++
		}
		r.inits[id] = hlsReplayInit{rendition: row.Rendition, resource: *segment.Map, file: entry}
		return nil
	}
	if track.seenSegments[segment.Sequence] {
		return hlsInvalid("HLS 请求记录重复引用同一个媒体序号")
	}
	track.seenSegments[segment.Sequence] = true
	return r.observeSegment(track, row, event, body, segment)
}

func (r *hlsReplay) observePlaylist(track *hlsReplayTrack, id int, row hlsRecord, event Event, body []byte) error {
	if row.Playlist != r.masterID {
		return hlsInvalid("HLS 请求记录与主播放列表引用不一致")
	}
	if row.Result != "ok" {
		r.report.HLS.PlaylistFailures++
		r.addRequestFinding("hls_playlist_fetch_failed", "播放列表", row, event)
		return nil
	}
	if row.HTTPStatus != http.StatusOK {
		return hlsInvalid("HLS 播放列表响应状态无效")
	}
	r.report.HLS.PlaylistsCaptured++
	playlist, err := decodeHLSPlaylist(body)
	if err != nil || (id != 1 && playlist.IsMaster()) {
		r.report.HLS.PlaylistFailures++
		r.add("hls_playlist_invalid", "error", "响应无法用作当前 HLS 播放列表", event.ArrivalMS, event.ArrivalMS, event)
		return nil
	}
	if playlist.IsMaster() {
		r.masterID = id
		variant := highestHLSVariant(playlist)
		r.audioAllowed = selectedHLSAudio(playlist, variant.AudioGroup) != ""
		r.report.HLS.RenditionBandwidth = max(0, variant.Bandwidth)
		return nil
	}
	track.playlist, track.playlistID = playlist, id
	if len(playlist.Segments) == 0 {
		return nil
	}

	latest := playlist.Segments[len(playlist.Segments)-1].Sequence
	event.Sequence = &latest
	if track.latestEvent == nil || latest > track.latestSegment {
		track.latestSegment, track.latestEvent = latest, &event
	} else if !playlist.EndList && event.ArrivalMS-track.latestEvent.ArrivalMS > max(hlsMS(playlist.TargetDuration)*3, 3000) {
		message := fmt.Sprintf("媒体播放列表连续 %d ms 没有新的分片序号", event.ArrivalMS-track.latestEvent.ArrivalMS)
		r.add("hls_playlist_stalled", "warn", message, track.latestEvent.ArrivalMS, event.ArrivalMS, *track.latestEvent, event)
	}
	return nil
}

func (r *hlsReplay) observeSegment(track *hlsReplayTrack, row hlsRecord, event Event, body []byte, segment m3u8.Segment) error {
	if row.Kind == hlsGap {
		r.report.HLS.SegmentsSkipped++
		r.add("hls_segment_gap", "warn", fmt.Sprintf("媒体序号 %d 标为 EXT-X-GAP", segment.Sequence), event.ArrivalMS, event.ArrivalMS, event)
		return nil
	}

	init, initOK := r.inits[row.Init]
	if segment.Map == nil {
		if row.Init != 0 {
			return hlsInvalid("HLS 分片引用了播放列表未声明的初始化段")
		}
	} else if !initOK || init.rendition != row.Rendition || init.resource.URI != segment.Map.URI || !sameHLSRange(init.resource.ByteRange, segment.Map.ByteRange) {
		return hlsInvalid("HLS 分片的初始化段引用与播放列表不一致")
	}
	if row.Result != "ok" {
		r.report.HLS.SegmentsFailed++
		r.addRequestFinding("hls_segment_fetch_failed", fmt.Sprintf("媒体序号 %d 的分片", segment.Sequence), row, event)
		return nil
	}
	if !validHLSBodyRange(row, segment.ByteRange) {
		return hlsInvalid("HLS 分片与字节范围不一致")
	}
	metrics := r.report.HLS
	metrics.SegmentsCaptured++
	if row.Rendition == hlsAudio {
		metrics.AudioSegmentsCaptured++
	}
	metrics.MediaBytes += int64(row.Bytes)
	metrics.MaxSegmentDownloadMS = max(metrics.MaxSegmentDownloadMS, row.DurationMS)
	declared := hlsMS(segment.Duration)
	if declared > 0 && row.DurationMS > declared {
		message := fmt.Sprintf("媒体序号 %d 下载耗时 %d ms，超过分片声明时长 %d ms", segment.Sequence, row.DurationMS, declared)
		r.add("hls_segment_slow", "warn", message, row.ArrivalMS-row.DurationMS, row.ArrivalMS, event)
	}
	if segment.Key != nil {
		metrics.EncryptedSegments++
		r.add("hls_encrypted_segment", "info", "证据包保留加密分片，媒体检查需要解密密钥", event.ArrivalMS, event.ArrivalMS, event)
		return nil
	}
	if initOK {
		if init.file == nil {
			return nil
		}
		if track.cachedInitID != row.Init {
			var err error
			track.cachedInit, err = readHLSObject(init.file)
			if err != nil {
				return err
			}
			track.cachedInitID = row.Init
		}
	}
	var initBody []byte
	if initOK {
		initBody = track.cachedInit
	}
	video, audio := media.InspectCodecs(body, initBody)
	r.report.Metrics.VideoCodecs = appendHLSCodecs(r.report.Metrics.VideoCodecs, video)
	r.report.Metrics.AudioCodecs = appendHLSCodecs(r.report.Metrics.AudioCodecs, audio)
	info := media.Inspect(body, initBody)
	if info.Format == "" && len(audio) > 0 {
		info.Format = "packed-audio"
	}
	if r.report.Metrics.FirstVideoMS == nil && len(video) > 0 || r.report.Metrics.FirstAudioMS == nil && len(audio) > 0 {
		var hasVideo, hasAudio bool
		switch info.Format {
		case "ts":
			hasVideo, hasAudio = hlsTSPESKinds(body)
		case "mp4":
			hasVideo, hasAudio = media.FragmentMediaKinds(body, initBody)
		case "adts":
			hasAudio = true
		}
		if hasVideo {
			markMediaArrival(&r.report.Metrics, "video", event.ArrivalMS)
		}
		if hasAudio {
			markMediaArrival(&r.report.Metrics, "audio", event.ArrivalMS)
		}
	}
	r.inspectMedia(info, segment.Sequence, declared, event, track.playlist.IndependentSegments)
	return nil
}

func (r *hlsReplay) inspectMedia(info media.Info, sequence int, declared int64, event Event, independent bool) {
	if info.Format == "" {
		r.add("hls_segment_format_unknown", "info", "分片容器无法识别，已保留原始字节", event.ArrivalMS, event.ArrivalMS, event)
	}
	if independent && info.Keyframe != nil && !*info.Keyframe {
		message := fmt.Sprintf("媒体序号 %d 的分片声明可独立解码，但检测到非关键帧开头", sequence)
		r.add("hls_non_independent_segment", "warn", message, event.ArrivalMS, event.ArrivalMS, event)
	}
	if actual := hlsMS(info.Duration); info.DurationKnown && declared > 0 && actual > 0 &&
		max(declared-actual, actual-declared) > max(500, declared/4) {
		message := fmt.Sprintf("媒体序号 %d 声明时长 %d ms，容器时间线估算 %d ms", sequence, declared, actual)
		r.add("hls_segment_duration_drift", "warn", message, event.ArrivalMS, event.ArrivalMS, event)
	}
}

func appendHLSCodecs(current, observed []string) []string {
	for _, codec := range observed {
		if !slices.Contains(current, codec) {
			current = append(current, codec)
		}
	}
	slices.Sort(current)
	return current
}

func sameHLSRange(left, right *m3u8.ByteRange) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func validHLSBodyRange(row hlsRecord, byteRange *m3u8.ByteRange) bool {
	if byteRange == nil {
		return row.HTTPStatus == http.StatusOK
	}
	return row.HTTPStatus == http.StatusPartialContent && byteRange.Length == row.Bytes
}

func (r *hlsReplay) addRequestFinding(code, resource string, row hlsRecord, event Event) {
	var reason string
	switch row.Result {
	case "http_status":
		reason = fmt.Sprintf("HTTP %d", row.HTTPStatus)
	case "body_error":
		reason = "响应体为空或读取中断"
	case "size_limit":
		reason = "达到产物大小上限，已保留响应前缀"
	case "range_error":
		reason = "HTTP 字节范围响应不符"
	default:
		reason = "无法完成请求"
	}
	r.add(code, "error", resource+"获取失败（"+reason+"）", row.ArrivalMS-row.DurationMS, row.ArrivalMS, event)
}

func (r *hlsReplay) add(code, severity, message string, start, end int64, evidence ...Event) {
	key := code
	if len(evidence) > 0 {
		key += "/" + evidence[0].Rendition
	}
	if r.seenFindings[key] {
		return
	}
	r.seenFindings[key] = true
	r.report.Findings = append(r.report.Findings, Finding{
		Code: code, Severity: severity, Message: message, StartMS: start, EndMS: end, Evidence: evidence,
	})
}

func (r *hlsReplay) finish() Report {
	observation := r.report.Manifest.Observation
	if r.report.HLS.SegmentsCaptured == 0 {
		r.add("hls_no_media", "warn", "采样窗口内没有获取到完整的 HLS 媒体分片", 0, observation.ElapsedMS)
	}
	switch observation.EndReason {
	case "size_limit", "tag_limit", "canceled":
		r.add(observation.EndReason, "info", endReasonText(observation.EndReason), observation.ElapsedMS, observation.ElapsedMS)
	}
	addObservationFindings(&r.report)
	slices.SortStableFunc(r.report.Findings, func(left, right Finding) int { return cmp.Compare(left.StartMS, right.StartMS) })
	for i := range r.report.Findings {
		r.report.Findings[i].ID = fmt.Sprintf("F%03d", i+1)
	}
	enrichFindings(&r.report)
	return r.report
}
