package diagnostic

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"streamanalyzer/internal/hlsdelivery/m3u8"
	hlsmedia "streamanalyzer/internal/hlsdelivery/media"
	"streamanalyzer/internal/rtmpraw"

	"github.com/yutopp/go-rtmp"
	"github.com/yutopp/go-rtmp/message"
)

var investigationMediaID = regexp.MustCompile(`^S00[1-8]$`)

type investigationMediaInput struct {
	path      string
	alignment string
	anchorMS  int64
	note      string
	rtsp      *RTSPMediaEvidence
	partial   bool
}

type investigationMediaProbe struct {
	durationMS         int64
	hasVideo, hasAudio bool
}

// extractStallMedia decodes only persisted source bytes. Derived MP4 playback
// contains the media content, not the receive-side waiting intervals.
func extractStallMedia(parent context.Context, dir string, report Report, item StallInvestigation) InvestigationMedia {
	result := InvestigationMedia{Status: "unavailable", Assets: []DerivedArtifact{}}
	if !investigationMediaID.MatchString(item.ID) {
		result.Reason = "补查编号无效，未生成媒体产物。"
		return result
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return investigationMediaInterrupted(result, ctx)
	}
	if isRTSPProtocol(report.Manifest.Protocol) {
		if _, err := investigationRTSPAnchor(report, item); err != nil {
			result.Reason = "原始 RTP 媒体补查不可用：" + err.Error()
			return result
		}
	}
	ffmpeg, err := investigationMediaTool("ffmpeg", "")
	if err != nil {
		result.Reason = "未找到 FFmpeg，请将 ffmpeg 和 ffprobe 放在程序旁或加入 PATH 后重试。"
		return result
	}
	ffprobe, err := investigationMediaTool("ffprobe", filepath.Dir(ffmpeg))
	if err != nil {
		result.Reason = "未找到 ffprobe，请将 ffmpeg 和 ffprobe 放在程序旁或加入 PATH 后重试。"
		return result
	}
	stage, err := os.MkdirTemp(dir, ".stall-media-*")
	if err != nil {
		result.Status, result.Reason = "failed", "无法创建本地媒体补查目录。"
		return result
	}
	defer os.RemoveAll(stage)
	input, err := investigationMediaPrepare(ctx, dir, stage, report, item)
	if err != nil {
		if ctx.Err() != nil {
			return investigationMediaInterrupted(result, ctx)
		}
		result.Reason = err.Error()
		if isRTSPProtocol(report.Manifest.Protocol) {
			result.Reason = "原始 RTP 媒体补查不可用：" + result.Reason
		}
		return result
	}
	result.Alignment = input.alignment
	result.RTSP = input.rtsp
	probe, err := investigationMediaInspect(ctx, ffprobe, input.path)
	if err != nil || !probe.hasVideo && !probe.hasAudio {
		if ctx.Err() != nil {
			return investigationMediaInterrupted(result, ctx)
		}
		result.Reason = "采样媒体无法解码，可能是封装不完整、编码不受支持或采样范围内没有媒体。"
		return result
	}
	availableMS := probe.durationMS
	if input.rtsp != nil {
		availableMS = min(availableMS, input.rtsp.DurationMS)
	}
	startMS, endMS := investigationMediaWindow(input.anchorMS, item.Stall.DurationMS, availableMS)
	if endMS-startMS < 100 {
		result.Reason = "播放器时间点不在已采集的可解码媒体范围内，未提取不相关片段。"
		return result
	}
	assetsDir := filepath.Join(stage, "assets")
	if err := os.Mkdir(assetsDir, 0o700); err != nil {
		result.Status, result.Reason = "failed", "无法写入本地媒体补查目录。"
		return result
	}
	clip := filepath.Join(assetsDir, "clip.mp4")
	args := append(investigationMediaInputArgs(input.path), "-ss", investigationMediaSeconds(startMS), "-i", input.path,
		"-t", investigationMediaSeconds(endMS-startMS), "-map", "0:v:0?", "-map", "0:a:0?",
		"-vf", "scale=w='min(1280,iw)':h='min(720,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2,format=yuv420p",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "28", "-maxrate", "1500k", "-bufsize", "3000k", "-threads", "1",
		"-c:a", "aac", "-b:a", "96k")
	if input.rtsp != nil {
		// Keep surviving source frame times across RTP loss. MOV edit lists
		// represent encoder delay without shifting the presentation origin.
		args = append(args, "-fps_mode:v", "vfr", "-avoid_negative_ts", "disabled")
	} else {
		args = append(args, "-avoid_negative_ts", "make_zero")
	}
	args = append(args, "-movflags", "+faststart", "-fs", strconv.Itoa(maxDerivedClipBytes), "-y", clip)
	if err := investigationMediaRun(ctx, ffmpeg, args, nil); err != nil {
		if ctx.Err() != nil {
			return investigationMediaInterrupted(result, ctx)
		}
		result.Status, result.Reason = "failed", "异常附近媒体无法转为 H.264/AAC 片段，请检查源流解码结果和 FFmpeg 编码器支持。"
		return result
	}
	clipProbe, err := investigationMediaInspect(ctx, ffprobe, clip)
	if err != nil || clipProbe.durationMS <= 0 {
		result.Status, result.Reason = "failed", "生成的片段不完整，未加入证据包。"
		return result
	}
	actualEndMS := startMS + min(endMS-startMS, clipProbe.durationMS)
	if input.rtsp != nil {
		actualEndMS = min(actualEndMS, input.rtsp.DurationMS)
	}
	result.StartMS, result.EndMS = &startMS, &actualEndMS
	duration := clipProbe.durationMS
	asset, err := investigationMediaArtifact(clip, "stalls/"+item.ID+"/clip.mp4", "clip", startMS, &duration)
	if err != nil {
		result.Status, result.Reason = "failed", "生成的片段超出大小限制或无法读取。"
		return result
	}
	result.Assets = append(result.Assets, asset)
	result.Status = "ready"
	if !probe.hasVideo {
		result.Status, result.Reason = "partial", "采样媒体只有音频，已保留音频片段，无法生成源流画面截图。"
	} else {
		imageExtentMS := duration
		if input.rtsp != nil {
			imageExtentMS = min(imageExtentMS, actualEndMS-startMS)
		}
		positions := []int64{min(int64(50), imageExtentMS/4), min(max(int64(0), input.anchorMS-startMS), max(int64(0), imageExtentMS-100)), max(int64(0), imageExtentMS-100)}
		if input.rtsp != nil {
			positions, err = investigationRTSPFramePositions(ctx, ffprobe, clip, positions, imageExtentMS)
			if err != nil || len(positions) != 3 {
				positions = nil
				result.Status, result.Reason = "partial", "片段已提取，无法确认派生视频的实际解码帧位置，未生成猜测位置的截图。"
			}
		}
		names := []string{"before.jpg", "during.jpg", "after.jpg"}
		for index, position := range positions {
			name := names[index]
			if ctx.Err() != nil {
				result.Status, result.Reason = "partial", "片段已提取，截图任务未完成。"
				break
			}
			path := filepath.Join(assetsDir, name)
			args := append(investigationMediaInputArgs(clip), "-ss", investigationMediaSeconds(position), "-i", clip,
				"-frames:v", "1", "-an", "-c:v", "mjpeg", "-q:v", "5", "-threads", "1", "-fs", strconv.Itoa(maxDerivedImageBytes), "-y", path)
			if err := investigationMediaRun(ctx, ffmpeg, args, nil); err != nil {
				_ = os.Remove(path)
				result.Status, result.Reason = "partial", "片段已提取，部分源流截图无法解码。"
				continue
			}
			image, err := investigationMediaArtifact(path, "stalls/"+item.ID+"/"+name, "screenshot", startMS+position, nil)
			if err != nil {
				_ = os.Remove(path)
				result.Status, result.Reason = "partial", "片段已提取，部分截图超出大小限制或无法读取。"
				continue
			}
			result.Assets = append(result.Assets, image)
		}
	}
	if input.partial && result.Status == "ready" {
		result.Status = "partial"
	}
	if input.note != "" {
		if result.Reason != "" {
			result.Reason += " "
		}
		result.Reason += input.note
	}
	if result.Reason != "" && result.Status == "ready" {
		result.Reason += " 片段反映源流媒体内容，原始收流停顿请对照补查时间线。"
	}
	parentDir := filepath.Join(dir, "stalls")
	if err := os.MkdirAll(parentDir, 0o700); err != nil {
		return InvestigationMedia{Status: "failed", Reason: "媒体产物保存失败。", Assets: []DerivedArtifact{}}
	}
	if info, err := os.Lstat(parentDir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return InvestigationMedia{Status: "failed", Reason: "媒体产物目录无效。", Assets: []DerivedArtifact{}}
	}
	targetDir := filepath.Join(parentDir, item.ID)
	if _, err := os.Lstat(targetDir); err == nil && len(item.Media.Assets) == 0 {
		// A process interruption may have published files before their report.
		// Only remove this case's known regular files when none are canonical.
		if err := investigationMediaRemoveOrphans(targetDir); err != nil {
			return InvestigationMedia{Status: "failed", Reason: "媒体产物目录包含无法替换的文件。", Assets: []DerivedArtifact{}}
		}
	}
	if err := os.Rename(assetsDir, targetDir); err != nil {
		return InvestigationMedia{Status: "failed", Reason: "媒体产物保存失败或该补查已存在。", Assets: []DerivedArtifact{}}
	}
	return result
}

func investigationMediaRemoveOrphans(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid media directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "clip.mp4", "before.jpg", "during.jpg", "after.jpg":
		default:
			return errors.New("unknown media file")
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("invalid media file")
		}
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return os.Remove(dir)
}

func investigationMediaInterrupted(result InvestigationMedia, ctx context.Context) InvestigationMedia {
	result.Status = "failed"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.Reason = "媒体提取超过 45 秒，关联时序证据仍可查看。"
	} else {
		result.Reason = "媒体提取已取消，关联时序证据仍可查看。"
	}
	return result
}

func investigationMediaTool(name, alongside string) (string, error) {
	dirs := []string{}
	if alongside != "" {
		dirs = append(dirs, alongside)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	dirs = append(dirs, "")
	if runtime.GOOS == "darwin" {
		dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin")
	}
	for _, dir := range dirs {
		if path, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

func investigationMediaInputArgs(path string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "flv,mpegts,hls,mov,aac"}
	if strings.EqualFold(filepath.Ext(path), ".m3u8") {
		args = append(args, "-allowed_extensions", "ALL")
	}
	return args
}

type investigationMediaLimitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *investigationMediaLimitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < b.limit {
		_, _ = b.Buffer.Write(p[:min(len(p), b.limit-b.Len())])
	}
	return n, nil
}

func investigationMediaRun(ctx context.Context, program string, args []string, output io.Writer) error {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout = output
	cmd.Stderr = &investigationMediaLimitedBuffer{limit: 4096}
	return cmd.Run()
}

func investigationMediaInspect(ctx context.Context, ffprobe, path string) (investigationMediaProbe, error) {
	args := append(investigationMediaInputArgs(path), "-show_entries", "format=duration:stream=codec_type,duration", "-of", "json", path)
	var output investigationMediaLimitedBuffer
	output.limit = 128 << 10
	if err := investigationMediaRun(ctx, ffprobe, args, &output); err != nil {
		return investigationMediaProbe{}, err
	}
	var decoded struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			Duration  string `json:"duration"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		return investigationMediaProbe{}, err
	}
	result := investigationMediaProbe{durationMS: investigationMediaDuration(decoded.Format.Duration)}
	for _, stream := range decoded.Streams {
		result.hasVideo = result.hasVideo || stream.CodecType == "video"
		result.hasAudio = result.hasAudio || stream.CodecType == "audio"
		result.durationMS = max(result.durationMS, investigationMediaDuration(stream.Duration))
	}
	return result, nil
}

func investigationMediaDuration(value string) int64 {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 86400 {
		return 0
	}
	return int64(math.Round(seconds * 1000))
}
func investigationMediaSeconds(ms int64) string { return fmt.Sprintf("%.3f", float64(ms)/1000) }

func investigationMediaWindow(anchor, duration, available int64) (int64, int64) {
	start := max(int64(0), anchor-3000)
	end := max(start+2000, anchor+min(max(duration, 0), int64(9000))+3000)
	end = min(end, start+15000)
	if available > 0 {
		end = min(end, available)
	}
	return start, end
}

func investigationMediaArtifact(path, name, kind string, at int64, duration *int64) (DerivedArtifact, error) {
	info, err := os.Lstat(path)
	limit := int64(maxDerivedClipBytes)
	if kind == "screenshot" {
		limit = maxDerivedImageBytes
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return DerivedArtifact{}, errors.New("invalid derived artifact")
	}
	file, err := os.Open(path)
	if err != nil {
		return DerivedArtifact{}, err
	}
	defer file.Close()
	if kind == "screenshot" {
		var magic [2]byte
		if _, err := io.ReadFull(file, magic[:]); err != nil || magic != [2]byte{0xff, 0xd8} {
			return DerivedArtifact{}, errors.New("invalid JPEG")
		}
		_, _ = file.Seek(0, io.SeekStart)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return DerivedArtifact{}, err
	}
	return DerivedArtifact{Name: name, Kind: kind, Artifact: Artifact{Bytes: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil))}, MediaTimeMS: &at, DurationMS: duration}, nil
}

func investigationMediaPrepare(ctx context.Context, dir, stage string, report Report, item StallInvestigation) (investigationMediaInput, error) {
	if isRTSPProtocol(report.Manifest.Protocol) {
		return investigationMediaRTSP(ctx, dir, stage, report, item)
	}
	input := investigationMediaInput{alignment: "player_clock_estimate", note: "片段位置按本机播放器媒体时钟估算，截图来自采样源流。"}
	var sample *PlayerBufferSample
	if report.Player != nil && report.Player.Source == "same_capture" {
		for index := range report.Player.BufferSamples {
			candidate := &report.Player.BufferSamples[index]
			if candidate.OffsetMS <= item.Stall.StartMS && (sample == nil || candidate.OffsetMS > sample.OffsetMS) {
				sample = candidate
			}
		}
		if sample == nil {
			for index := range report.Player.BufferSamples {
				candidate := &report.Player.BufferSamples[index]
				if sample == nil || candidate.OffsetMS < sample.OffsetMS {
					sample = candidate
				}
			}
		}
	}
	if sample != nil {
		input.anchorMS = sample.CurrentMS
	} else if report.Manifest.Protocol != ProtocolHLS {
		return input, errors.New("缺少可关联的播放器媒体时钟，未按收流时间猜测片段位置。")
	}
	if report.Manifest.Stream.Bytes < 0 || report.Manifest.Stream.Bytes > maxStreamBytes {
		return input, errors.New("原始流大小无效。")
	}
	if ctx.Err() != nil {
		return input, ctx.Err()
	}
	switch report.Manifest.Protocol {
	case ProtocolHTTPFLV, ProtocolHTTPTS:
		input.path = filepath.Join(dir, dumpName(report.Manifest.Protocol))
		info, err := os.Lstat(input.path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxStreamBytes {
			return input, errors.New("原始媒体文件不可读。")
		}
	case ProtocolRTMP:
		input.path = filepath.Join(stage, "input.flv")
		if err := investigationMediaRTMP(ctx, filepath.Join(dir, rtmpStreamName), input.path); err != nil {
			return input, errors.New("RTMP 原始流无法还原为媒体片段，可能是协议数据不完整。")
		}
	case ProtocolUDPTS, ProtocolRTPTS:
		input.path = filepath.Join(stage, "input.ts")
		file, err := os.OpenFile(input.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return input, errors.New("无法创建媒体解封装文件。")
		}
		writer := bufio.NewWriterSize(file, 64<<10)
		err = walkTSChunks(dir, report.Manifest, func(_ tsSpan, data []byte) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if report.Manifest.Protocol == ProtocolRTPTS {
				header, end, _, _, reason := rtpTSHeader(data)
				if reason != "" {
					return nil
				}
				data = data[header:end]
			}
			_, err := writer.Write(data)
			return err
		})
		err = errors.Join(err, writer.Flush(), file.Close())
		if err != nil {
			return input, errors.New("UDP/RTP 原始流与接收时间线不一致，无法解封装。")
		}
	case ProtocolHLS:
		return investigationMediaHLS(ctx, dir, stage, report, item)
	default:
		return input, errors.New("当前采样协议不支持媒体提取。")
	}
	return input, nil
}

func investigationMediaRTMP(ctx context.Context, source, target string) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	var handshake [rtmpraw.DefaultHandshakeSkipBytes]byte
	if _, err := io.ReadFull(file, handshake[:]); err != nil || handshake[0] != 3 {
		return errors.New("invalid RTMP handshake")
	}
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	writer := bufio.NewWriterSize(output, 64<<10)
	defer output.Close()
	_, _ = writer.Write([]byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0})
	streamer := rtmp.NewChunkStreamer(bufio.NewReaderSize(file, 64<<10), io.Discard, &rtmp.StreamControlStateConfig{MaxChunkStreams: 128})
	defer streamer.Close()
	count := 0
	writeTag := func(kind byte, stamp uint32, payload []byte) error {
		if len(payload) > 0xffffff {
			return errors.New("RTMP media message too large")
		}
		var header [11]byte
		header[0] = kind
		putFLVUint24(header[1:4], uint32(len(payload)))
		putFLVTimestamp(header[4:8], stamp)
		if _, err := writer.Write(header[:]); err != nil {
			return err
		}
		if _, err := writer.Write(payload); err != nil {
			return err
		}
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(11+len(payload)))
		_, err := writer.Write(size[:])
		count++
		return err
	}
	for index := 0; index < 2*maxTags; index++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		packet, err := streamer.ReadPayload()
		if err != nil {
			if count > 0 {
				return writer.Flush()
			}
			return err
		}
		switch packet.TypeID {
		case message.TypeIDAudioMessage, message.TypeIDVideoMessage, message.TypeIDDataMessageAMF0:
			if err := writeTag(byte(packet.TypeID), packet.Timestamp, packet.Payload); err != nil {
				return err
			}
		case message.TypeIDAggregateMessage:
			reader := bytes.NewReader(packet.Payload)
			var firstStamp uint32
			for first := true; reader.Len() > 0; first = false {
				var header [11]byte
				if _, err := io.ReadFull(reader, header[:]); err != nil {
					return err
				}
				size := int(readUint24(header[1:4]))
				if size > reader.Len()-4 {
					return io.ErrUnexpectedEOF
				}
				payload := make([]byte, size)
				if _, err := io.ReadFull(reader, payload); err != nil {
					return err
				}
				var previous [4]byte
				if _, err := io.ReadFull(reader, previous[:]); err != nil || binary.BigEndian.Uint32(previous[:]) != uint32(size+11) {
					return errors.New("invalid aggregate tag")
				}
				stamp := readUint24(header[4:7]) | uint32(header[7])<<24
				if first {
					firstStamp = stamp
				}
				if header[0] == 8 || header[0] == 9 || header[0] == 18 {
					if err := writeTag(header[0], packet.Timestamp+stamp-firstStamp, payload); err != nil {
						return err
					}
				}
			}
		}
	}
	return writer.Flush()
}

type investigationMediaHLSSegment struct {
	row     hlsRecord
	segment m3u8.Segment
	init    string
}

func investigationMediaHLS(ctx context.Context, dir, stage string, report Report, item StallInvestigation) (investigationMediaInput, error) {
	input := investigationMediaInput{alignment: "independent_capture_reference", note: "HLS 播放器使用独立连接，此片段按同时间采样分片选取，仅作内容参考，无法还原播放器当时的确切画面。"}
	archive, backing, err := openEvidenceZIP(filepath.Join(dir, hlsStreamName))
	if err != nil {
		return input, errors.New("HLS 原始分片包不可读。")
	}
	defer backing.Close()
	entries, err := hlsArchiveEntries(archive.File)
	if err != nil {
		return input, errors.New("HLS 原始分片包格式无效。")
	}
	ledger, err := os.Open(filepath.Join(dir, timelineName))
	if err != nil {
		return input, errors.New("HLS 接收时间线不可读。")
	}
	defer ledger.Close()
	scanner := bufio.NewScanner(io.LimitReader(ledger, maxTimeline+1))
	scanner.Buffer(make([]byte, 1024), maxHLSRecordBytes)
	playlists := map[int]*m3u8.Playlist{}
	inits := map[int]string{}
	tracks := map[string][]investigationMediaHLSSegment{}
	id := 0
	last := int64(0)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return input, ctx.Err()
		}
		id++
		var row hlsRecord
		if id > hlsMaxRecords || json.Unmarshal(scanner.Bytes(), &row) != nil || !validHLSRecord(row, id, last, report.Manifest.Observation.ElapsedMS) {
			return input, errors.New("HLS 接收时间线无效。")
		}
		last = row.ArrivalMS
		if row.Result != "ok" {
			continue
		}
		entry := entries[row.Artifact]
		if entry == nil {
			return input, errors.New("HLS 原始分片不完整。")
		}
		switch row.Kind {
		case hlsPlaylist:
			body, err := readHLSObject(entry)
			if err != nil {
				return input, errors.New("HLS 播放列表不可读。")
			}
			playlist, err := decodeHLSPlaylist(body)
			if err == nil && !playlist.IsMaster() {
				playlists[id] = playlist
			}
		case hlsInit:
			inits[id] = row.Artifact
		case hlsSegment:
			playlist := playlists[row.Playlist]
			if playlist == nil || row.Sequence == nil {
				return input, errors.New("HLS 分片缺少本地播放列表。")
			}
			index := *row.Sequence - playlist.MediaSequence
			if index < 0 || index >= len(playlist.Segments) {
				return input, errors.New("HLS 分片序号无效。")
			}
			tracks[row.Rendition] = append(tracks[row.Rendition], investigationMediaHLSSegment{row: row, segment: playlist.Segments[index], init: inits[row.Init]})
		}
	}
	if scanner.Err() != nil {
		return input, errors.New("HLS 接收时间线不完整。")
	}
	main := tracks[hlsMain]
	if len(main) == 0 {
		return input, errors.New("采样范围内没有完整 HLS 主路媒体分片。")
	}
	// Select by capture arrival, not the independent player's media clock.
	first, lastIndex := 0, len(main)-1
	for index, segment := range main {
		if segment.row.ArrivalMS <= item.Stall.StartMS {
			first = index
		}
		if segment.row.ArrivalMS >= item.Stall.EndMS+3000 {
			lastIndex = index
			break
		}
	}
	first = max(0, first-1)
	if lastIndex < first {
		lastIndex = first
	}
	selected := main[first : lastIndex+1]
	// Keep the local reference bounded even when the stall lasts a long time.
	if len(selected) > 12 {
		selected = selected[:12]
	}
	anchor := int64(0)
	for _, segment := range selected {
		if segment.row.ArrivalMS < item.Stall.StartMS {
			anchor += int64(segment.segment.Duration * 1000)
		}
	}
	input.anchorMS = anchor
	if err := investigationMediaWriteHLS(ctx, stage, "main", selected, entries, report.Manifest.Observation.Origin); err != nil {
		return input, err
	}
	input.path = filepath.Join(stage, "main.m3u8")
	if audio := tracks[hlsAudio]; len(audio) > 0 {
		var selectedAudio []investigationMediaHLSSegment
		startArrival := selected[0].row.ArrivalMS
		endArrival := selected[len(selected)-1].row.ArrivalMS
		for _, segment := range audio {
			if segment.row.ArrivalMS >= startArrival-3000 && segment.row.ArrivalMS <= endArrival+3000 {
				selectedAudio = append(selectedAudio, segment)
			}
		}
		if len(selectedAudio) > 12 {
			selectedAudio = selectedAudio[:12]
		}
		if len(selectedAudio) > 0 {
			if err := investigationMediaWriteHLS(ctx, stage, "audio", selectedAudio, entries, report.Manifest.Observation.Origin); err != nil {
				return input, err
			}
			master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"audio\",DEFAULT=YES,AUTOSELECT=YES,URI=\"audio.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=2000000,AUDIO=\"audio\"\nmain.m3u8\n"
			input.path = filepath.Join(stage, "input.m3u8")
			if err := os.WriteFile(input.path, []byte(master), 0o600); err != nil {
				return input, errors.New("无法写入本地 HLS 媒体引用。")
			}
		}
	}
	return input, nil
}

func investigationMediaWriteHLS(ctx context.Context, stage, prefix string, segments []investigationMediaHLSSegment, entries map[string]*zip.File, origin string) error {
	var playlist strings.Builder
	playlist.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:60\n#EXT-X-MEDIA-SEQUENCE:0\n")
	lastInit := ""
	for index, item := range segments {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if item.segment.Key != nil && item.segment.Key.Method != "AES-128" {
			return errors.New("HLS 加密方式不是受支持的 AES-128。")
		}
		if item.segment.Key != nil && !hlsKeyFetchAllowed(ctx) {
			return errors.New("HLS 分片已加密，当前离线流程未启用临时密钥分析。")
		}
		if item.segment.Discontinuity || index > 0 && item.segment.Sequence != segments[index-1].segment.Sequence+1 {
			playlist.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		if item.init != "" && item.init != lastInit {
			entry := entries[item.init]
			if entry == nil {
				return errors.New("HLS 初始化段缺失，无法解码异常片段。")
			}
			name := prefix + "-init-" + strconv.Itoa(index) + ".mp4"
			if err := investigationMediaCopyZIP(entry, filepath.Join(stage, name)); err != nil {
				return errors.New("HLS 初始化段不可读。")
			}
			fmt.Fprintf(&playlist, "#EXT-X-MAP:URI=\"%s\"\n", name)
			lastInit = item.init
		} else if item.segment.Map != nil && item.init == "" {
			return errors.New("HLS 初始化段缺失，无法解码异常片段。")
		}
		extension := ".ts"
		if item.init != "" {
			extension = ".m4s"
		}
		name := fmt.Sprintf("%s-%03d%s", prefix, index, extension)
		entry := entries[item.row.Artifact]
		if entry == nil {
			return errors.New("HLS 媒体分片缺失。")
		}
		if err := investigationMediaCopyHLSSegment(ctx, entry, filepath.Join(stage, name), item.segment, origin); err != nil {
			return err
		}
		fmt.Fprintf(&playlist, "#EXTINF:%.6f,\n%s\n", max(0.001, item.segment.Duration), name)
	}
	playlist.WriteString("#EXT-X-ENDLIST\n")
	if err := os.WriteFile(filepath.Join(stage, prefix+".m3u8"), []byte(playlist.String()), 0o600); err != nil {
		return errors.New("无法写入本地 HLS 媒体引用。")
	}
	return nil
}

func investigationMediaCopyHLSSegment(ctx context.Context, entry *zip.File, path string, segment m3u8.Segment, origin string) error {
	if segment.Key == nil {
		return investigationMediaCopyZIP(entry, path)
	}
	if !hlsKeyFetchAllowed(ctx) {
		return errors.New("HLS 分片已加密，当前离线流程未启用临时密钥分析。")
	}
	target, err := resolveHLSKeyURL(origin, segment.Key.URI)
	if err != nil {
		return fmt.Errorf("无法解析 HLS 密钥地址：%w", err)
	}
	key, err := fetchHLSAnalysisKey(ctx, target)
	if err != nil {
		return err
	}
	defer func() {
		for index := range key {
			key[index] = 0
		}
	}()
	reader, err := entry.Open()
	if err != nil {
		return errors.New("HLS 加密分片不可读。")
	}
	ciphertext, err := io.ReadAll(io.LimitReader(reader, hlsSegmentLimit+1))
	reader.Close()
	if err != nil || len(ciphertext) == 0 || len(ciphertext) > hlsSegmentLimit {
		return errors.New("HLS 加密分片大小无效。")
	}
	iv := segment.Key.IV
	if len(iv) == 0 {
		iv = make([]byte, 16)
		binary.BigEndian.PutUint64(iv[8:], uint64(segment.Sequence))
	}
	plaintext, err := hlsmedia.DecryptAES128(ciphertext, key, iv)
	if err != nil {
		return errors.New("HLS AES-128 分片解密失败。")
	}
	if err := os.WriteFile(path, plaintext, 0o600); err != nil {
		return errors.New("无法写入临时解密分片。")
	}
	for index := range plaintext {
		plaintext[index] = 0
	}
	return nil
}

func investigationMediaCopyZIP(entry *zip.File, path string) error {
	if entry.UncompressedSize64 > hlsSegmentLimit {
		return errors.New("oversized HLS object")
	}
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, io.LimitReader(reader, int64(entry.UncompressedSize64)+1))
	return errors.Join(copyErr, file.Close())
}
