package diagnostic

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"streamanalyzer/internal/hlsdelivery/m3u8"
	"streamanalyzer/internal/hlsdelivery/media"
)

const (
	maxStallBufferSamples = 128
	maxStallGaps          = 16
	maxStallKeyframes     = 16
	maxStallRequests      = 16
	maxStallFindings      = 24
)

func correlateStall(dir string, report Report, id string, stallIndex int) (StallCorrelation, error) {
	if report.Player == nil || stallIndex < 1 || stallIndex > len(report.Player.Stalls) ||
		len(id) != 4 || id[0] != 'S' || id[1] != '0' || id[2] != '0' || id[3] < '1' || int(id[3]-'0') > maxStallInvestigations {
		return StallCorrelation{}, fmt.Errorf("%w: 请选择已记录的卡顿区间", ErrInvalid)
	}
	stall := report.Player.Stalls[stallIndex-1]
	if stall.EndMS < stall.StartMS || stall.DurationMS != stall.EndMS-stall.StartMS || report.Manifest.Observation.ElapsedMS < 0 {
		return StallCorrelation{}, fmt.Errorf("%w: 卡顿区间无效", ErrInvalid)
	}
	result := StallCorrelation{
		Source: report.Player.Source, WindowStartMS: min(report.Manifest.Observation.ElapsedMS, max(0, stall.StartMS-3000)),
		WindowEndMS:   min(report.Manifest.Observation.ElapsedMS, max(0, stall.EndMS+3000)),
		BufferSamples: []PlayerBufferSample{}, Gaps: []ReceptionGap{}, Keyframes: []Event{}, Findings: []Finding{}, Notes: []string{},
	}
	if result.WindowEndMS < result.WindowStartMS {
		result.WindowEndMS = result.WindowStartMS
	}
	if report.PlayerNetwork != nil {
		// Playback proxy observations can continue after the independent
		// sampler ends. Do not clip this evidence to the sampling duration.
		result.WindowEndMS = max(result.WindowEndMS, min(max(0, report.Player.StartOffsetMS+report.Player.ElapsedMS), max(0, stall.EndMS+3000)))
	}
	if stall.StartMS > report.Manifest.Observation.ElapsedMS || stall.EndMS < 0 {
		result.Notes = append(result.Notes, "所选播放器卡顿位于后端采样时段之外；本次缺少同期收流证据，仅可对照已保存的相邻媒体。")
	}
	add := func(code, severity, message string, start, end int64, evidence ...Event) {
		if len(result.Findings) >= maxStallFindings {
			return
		}
		result.Findings = append(result.Findings, Finding{
			ID: fmt.Sprintf("%s-E%03d", id, len(result.Findings)+1), Code: code, Severity: severity,
			Message: message, StartMS: max(0, start), EndMS: max(0, end), Occurrences: 1,
			Evidence: slices.Clone(evidence),
		})
	}
	add("stall_selected", "info", fmt.Sprintf("选中的播放器卡顿持续 %d ms", stall.DurationMS), stall.StartMS, stall.EndMS,
		Event{Kind: "player_stall", Artifact: playerName, ArrivalMS: stall.StartMS, DurationMS: stall.DurationMS})
	if result.Source == "independent_pull" {
		result.Notes = append(result.Notes, "HLS 播放器与采样使用独立连接；本次收流、关键帧和请求记录仅供同一时间段对照，不能据此证明播放器连接的故障原因。")
		add("stall_independent_connection", "info", result.Notes[len(result.Notes)-1], result.WindowStartMS, result.WindowEndMS)
	}
	for _, sample := range report.Player.BufferSamples {
		if sample.OffsetMS >= result.WindowStartMS && sample.OffsetMS <= result.WindowEndMS {
			result.BufferSamples = append(result.BufferSamples, sample)
		}
	}
	result.BufferSamples = boundedStallSamples(result.BufferSamples)
	if len(result.BufferSamples) > 0 {
		var minimum, maximum int64
		valid := false
		for _, sample := range result.BufferSamples {
			if sample.Paused || sample.Seeking || sample.Hidden {
				continue
			}
			if !valid || sample.BufferMS < minimum {
				minimum = sample.BufferMS
			}
			maximum = max(maximum, sample.BufferMS)
			valid = true
		}
		if valid {
			add("stall_buffer_window", "info", fmt.Sprintf("前后台状态正常的窗口样本中，可播放缓冲最小 %d ms、最大 %d ms；缓冲样本是离散观测", minimum, maximum), result.BufferSamples[0].OffsetMS, result.BufferSamples[len(result.BufferSamples)-1].OffsetMS)
		}
	} else {
		result.Notes = append(result.Notes, "所选时间窗口没有播放器缓冲样本，无法补推缓冲变化。")
	}

	result.Scheduling = schedulingWindow(report.Player.Scheduling, result.WindowStartMS, result.WindowEndMS)
	for _, finding := range schedulingCaseFindings(result.Scheduling, stall) {
		add(finding.Code, finding.Severity, finding.Message, finding.StartMS, finding.EndMS, finding.Evidence...)
	}
	if result.Scheduling != nil {
		result.Notes = append(result.Notes, "页面调度按已记录的前台播放覆盖裁剪，长任务与定时器延后分开统计；调度阻塞期间可能缺少缓冲样本，不推断其变化。")
	}
	correlatePlayerRequests(&result, report, stall)
	for _, request := range result.PlayerRequests {
		if code := playerRequestFindingCode(request); code != "" {
			label := "处理耗时较长"
			if code == "stall_player_request_failed" {
				label = "失败"
			}
			number := request.Number
			add(code, "warn", fmt.Sprintf("实际 HLS 播放代理请求 #%d（%s）%s：状态 %d，耗时 %d ms，上游读取 %d bytes", request.Number, request.Kind, label, request.HTTPStatus, request.EndMS-request.StartMS, request.Bytes), request.StartMS, request.EndMS,
				Event{Kind: "player_request", Artifact: "player-network.json", ArrivalMS: request.StartMS, DurationMS: request.EndMS - request.StartMS, Sequence: &number, HTTPStatus: request.HTTPStatus})
		}
	}

	events, notes, err := stallCaptureEvents(dir, report.Manifest)
	if err != nil {
		return StallCorrelation{}, err
	}
	result.Notes = append(result.Notes, notes...)
	slices.SortStableFunc(events, func(a, b Event) int { return cmp.Compare(a.ArrivalMS, b.ArrivalMS) })
	result.Gaps = stallReceiveGaps(events, report.Manifest.Observation.ElapsedMS, result.WindowStartMS, result.WindowEndMS)
	for _, gap := range result.Gaps {
		label := "视频"
		if gap.Kind == "audio" {
			label = "音频"
		}
		message := fmt.Sprintf("采样连接的%s接收间隔为 %d ms", label, gap.DurationMS)
		var evidence []Event
		if gap.Before != nil {
			evidence = append(evidence, *gap.Before)
		}
		if gap.After != nil {
			evidence = append(evidence, *gap.After)
		} else {
			message += "（区间在采样结束处截断）"
		}
		severity := "info"
		if gap.DurationMS >= 500 && gap.EndMS >= stall.StartMS && gap.StartMS <= stall.EndMS {
			severity = "warn"
		}
		add("stall_"+gap.Kind+"_receive_gap", severity, message, gap.StartMS, gap.EndMS, evidence...)
	}
	var before *Event
	for _, event := range events {
		if event.Kind != "video" || !event.Keyframe {
			continue
		}
		if event.ArrivalMS <= stall.StartMS {
			copy := event
			before = &copy
		}
		if event.ArrivalMS >= result.WindowStartMS && event.ArrivalMS <= result.WindowEndMS {
			result.Keyframes = append(result.Keyframes, event)
		}
	}
	if before != nil && (len(result.Keyframes) == 0 || before.ArrivalMS < result.Keyframes[0].ArrivalMS) {
		result.Keyframes = append([]Event{*before}, result.Keyframes...)
	}
	if len(result.Keyframes) > maxStallKeyframes {
		// Prioritize the closest boundary frames around the selected stall.
		slices.SortStableFunc(result.Keyframes, func(a, b Event) int {
			return cmp.Compare(stallDistance(a.ArrivalMS, stall.StartMS), stallDistance(b.ArrivalMS, stall.StartMS))
		})
		result.Keyframes = result.Keyframes[:maxStallKeyframes]
		if before != nil && !slices.ContainsFunc(result.Keyframes, func(event Event) bool { return event == *before }) {
			result.Keyframes[len(result.Keyframes)-1] = *before
		}
		slices.SortStableFunc(result.Keyframes, func(a, b Event) int { return cmp.Compare(a.ArrivalMS, b.ArrivalMS) })
	}
	if before != nil {
		add("stall_previous_keyframe", "info", fmt.Sprintf("卡顿开始前最近观测到的关键帧到达于 %d ms，距卡顿开始 %d ms", before.ArrivalMS, stall.StartMS-before.ArrivalMS), before.ArrivalMS, stall.StartMS, *before)
	}
	if len(result.Keyframes) == 0 {
		result.Notes = append(result.Notes, "该窗口及此前没有可确认的关键帧证据；未观测到不能等同于源流没有关键帧。")
	}
	requests := report.Requests
	if len(requests) == 0 {
		requests = report.Manifest.Observation.Requests
	}
	for _, request := range requests {
		end := request.EndMS
		if request.BodyEndMS != nil {
			end = *request.BodyEndMS
		}
		if end < result.WindowStartMS || request.StartMS > result.WindowEndMS {
			continue
		}
		if len(result.Requests) == maxStallRequests {
			result.Notes = append(result.Notes, "窗口请求较多，仅保留前 16 条对照记录。")
			break
		}
		request.Stages = slices.Clone(request.Stages)
		request.RequestHeaders = slices.Clone(request.RequestHeaders)
		request.ResponseHeaders = slices.Clone(request.ResponseHeaders)
		result.Requests = append(result.Requests, request)
	}
	for _, finding := range report.Findings {
		if strings.HasPrefix(finding.Code, "player_") ||
			(strings.HasPrefix(finding.ID, "S") && (strings.Contains(finding.ID, "-E") || strings.Contains(finding.ID, "-A"))) ||
			finding.EndMS < result.WindowStartMS || finding.StartMS > result.WindowEndMS {
			continue
		}
		add(finding.Code, finding.Severity, finding.Message, finding.StartMS, finding.EndMS, finding.Evidence...)
	}
	if len(events) == 0 {
		result.Notes = append(result.Notes, "采样中没有可识别的音视频到达记录，无法计算音视频收流间隔。")
	}
	return result, nil
}

func boundedStallSamples(samples []PlayerBufferSample) []PlayerBufferSample {
	if len(samples) <= maxStallBufferSamples {
		return samples
	}
	// Preserve brief buffer depletion and source endpoints while bounding long stalls.
	selected := map[int]bool{0: true, len(samples) - 1: true}
	low, high := 0, 0
	active := false
	for i, sample := range samples {
		if sample.Paused || sample.Seeking || sample.Hidden {
			continue
		}
		if !active {
			low, high, active = i, i, true
		}
		if sample.BufferMS < samples[low].BufferMS {
			low = i
		}
		if sample.BufferMS > samples[high].BufferMS {
			high = i
		}
	}
	selected[low], selected[high] = true, true
	for i := 0; i < maxStallBufferSamples-4; i++ {
		selected[i*(len(samples)-1)/(maxStallBufferSamples-5)] = true
	}
	for i := 0; len(selected) < maxStallBufferSamples && i < len(samples); i++ {
		selected[i] = true
	}
	result := make([]PlayerBufferSample, 0, maxStallBufferSamples)
	for i, sample := range samples {
		if selected[i] {
			result = append(result, sample)
		}
	}
	return result
}

func stallDistance(a, b int64) int64 {
	if a > b {
		return a - b
	}
	return b - a
}

func stallReceiveGaps(events []Event, elapsed, start, end int64) []ReceptionGap {
	last := make(map[string]Event)
	var result []ReceptionGap
	appendGap := func(kind string, before Event, after *Event, until int64) {
		if until-before.ArrivalMS < 250 || until < start || before.ArrivalMS > end {
			return
		}
		copy := before
		result = append(result, ReceptionGap{Kind: kind, StartMS: before.ArrivalMS, EndMS: until, DurationMS: until - before.ArrivalMS, Before: &copy, After: after})
	}
	for _, event := range events {
		if event.Kind != "video" && event.Kind != "audio" {
			continue
		}
		if previous, ok := last[event.Kind]; ok {
			copy := event
			appendGap(event.Kind, previous, &copy, event.ArrivalMS)
		}
		last[event.Kind] = event
	}
	for _, kind := range []string{"video", "audio"} {
		if previous, ok := last[kind]; ok {
			appendGap(kind, previous, nil, elapsed)
		}
	}
	slices.SortStableFunc(result, func(a, b ReceptionGap) int {
		if n := cmp.Compare(b.DurationMS, a.DurationMS); n != 0 {
			return n
		}
		if n := cmp.Compare(a.StartMS, b.StartMS); n != 0 {
			return n
		}
		return strings.Compare(a.Kind, b.Kind)
	})
	if len(result) > maxStallGaps {
		result = result[:maxStallGaps]
	}
	slices.SortStableFunc(result, func(a, b ReceptionGap) int {
		if n := cmp.Compare(a.StartMS, b.StartMS); n != 0 {
			return n
		}
		return strings.Compare(a.Kind, b.Kind)
	})
	if result == nil {
		return []ReceptionGap{}
	}
	return result
}

func stallCaptureEvents(dir string, manifest Manifest) ([]Event, []string, error) {
	switch manifest.Protocol {
	case ProtocolHTTPFLV:
		events, err := replay(dir, manifest)
		return events, nil, err
	case ProtocolRTMP:
		stamps, err := loadRTMPStamps(dir, manifest)
		if err != nil {
			return nil, nil, err
		}
		stream, err := os.Open(filepath.Join(dir, rtmpStreamName))
		if err != nil {
			return nil, nil, err
		}
		defer stream.Close()
		events, err := parseRTMPCapture(bufio.NewReaderSize(stream, 64<<10), stamps)
		var notes []string
		if err != nil {
			notes = append(notes, "RTMP 原始数据在不完整消息处结束，收流关联使用此前可解析的媒体记录。")
		}
		return events, notes, nil
	case ProtocolHTTPTS, ProtocolUDPTS, ProtocolRTPTS:
		analyzer := newTSAnalyzer(manifest)
		var events []Event
		analyzer.onMedia = func(e Event) {
			if len(events) < maxTags {
				events = append(events, e)
			}
		}
		framer := tsFramer{analyzer: analyzer}
		err := walkTSChunks(dir, manifest, func(span tsSpan, data []byte) error {
			switch manifest.Protocol {
			case ProtocolHTTPTS:
				framer.feed(span, data)
			case ProtocolUDPTS:
				framer.datagram(span, data, 0, nil, true)
			case ProtocolRTPTS:
				analyzer.readRTP(&framer, span, data)
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		framer.finishHTTP()
		return events, []string{"MPEG-TS 接收点按已识别媒体 PID 的 PES 起始记录；本次未进行 TS 编码帧级关键帧检测，PES 起始不视为关键帧。"}, nil
	case ProtocolHLS:
		return stallHLSEvents(dir, manifest)
	default:
		return nil, nil, fmt.Errorf("%w: 不支持的补查协议", ErrInvalid)
	}
}

func stallHLSEvents(dir string, manifest Manifest) ([]Event, []string, error) {
	// Validate ledger references and original media before deriving new evidence.
	if _, err := replayHLS(dir, manifest); err != nil {
		return nil, nil, err
	}
	archive, backing, err := openEvidenceZIP(filepath.Join(dir, hlsStreamName))
	if err != nil {
		return nil, nil, err
	}
	defer backing.Close()
	entries, err := hlsArchiveEntries(archive.File)
	if err != nil {
		return nil, nil, err
	}
	timeline, err := os.Open(filepath.Join(dir, timelineName))
	if err != nil {
		return nil, nil, err
	}
	defer timeline.Close()
	scanner := bufio.NewScanner(timeline)
	scanner.Buffer(make([]byte, 1024), maxHLSRecordBytes)
	playlists := make(map[int]*m3u8.Playlist)
	inits := make(map[int]string)
	var events []Event
	encrypted := false
	id := 0
	for scanner.Scan() {
		id++
		var row hlsRecord
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			return nil, nil, fmt.Errorf("%w: HLS 补查时间线无效", ErrInvalid)
		}
		if row.Result != "ok" {
			continue
		}
		entry := entries[row.Artifact]
		if entry == nil {
			return nil, nil, fmt.Errorf("%w: HLS 补查缺少产物", ErrInvalid)
		}
		if row.Kind == hlsInit {
			inits[id] = row.Artifact
			continue
		}
		body, err := readHLSObject(entry)
		if err != nil {
			return nil, nil, err
		}
		if row.Kind == hlsPlaylist {
			pl, err := decodeHLSPlaylist(body)
			if err == nil {
				playlists[id] = pl
			}
			continue
		}
		if row.Kind != hlsSegment {
			continue
		}
		pl := playlists[row.Playlist]
		if pl == nil || row.Sequence == nil {
			continue
		}
		index := *row.Sequence - pl.MediaSequence
		if index < 0 || index >= len(pl.Segments) {
			continue
		}
		segment := pl.Segments[index]
		if segment.Key != nil {
			encrypted = true
			continue
		}
		var init []byte
		if name := inits[row.Init]; name != "" {
			init, err = readHLSObject(entries[name])
			if err != nil {
				return nil, nil, err
			}
		}
		video, audio := media.InspectCodecs(body, init)
		info := media.Inspect(body, init)
		var hasVideo, hasAudio bool
		switch info.Format {
		case "ts":
			hasVideo, hasAudio = hlsTSPESKinds(body)
		case "mp4":
			hasVideo, hasAudio = media.FragmentMediaKinds(body, init)
		case "adts":
			hasAudio = true
		}
		event := Event{Kind: "video", Artifact: row.Artifact, Rendition: row.Rendition, Sequence: row.Sequence, ArrivalMS: row.ArrivalMS, Size: row.Bytes, DurationMS: row.DurationMS, HTTPStatus: row.HTTPStatus}
		if info.PTSCount > 0 && info.Timescale > 0 && info.PTSStart <= math.MaxInt64/1000 {
			event.PTS = int64(info.PTSStart * 1000 / uint64(info.Timescale))
			event.DTS = event.PTS
		}
		if len(video) > 0 && hasVideo {
			event.Codec = strings.Join(video, ",")
			event.Keyframe = info.Keyframe != nil && *info.Keyframe
			events = append(events, event)
		}
		if len(audio) > 0 && hasAudio {
			event.Kind, event.Codec, event.Keyframe = "audio", strings.Join(audio, ","), false
			events = append(events, event)
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, err
	}
	notes := []string{"HLS 到达点按分片下载完成记录；关键帧仅表示容器检查确认的分片起始关键帧，分片内逐帧到达时刻不可恢复。"}
	if encrypted {
		notes = append(notes, "加密分片保留原始密文，未解密的分片不产生音视频或关键帧证据。")
	}
	return events, notes, nil
}
