package diagnostic

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const contentQualityVersion = "content-quality/1"

type ContentQualityThresholds struct {
	BlackMinimumMS    int64   `json:"black_minimum_ms"`
	BlackPixelRatio   float64 `json:"black_pixel_ratio"`
	BlackPictureRatio float64 `json:"black_picture_ratio"`
	FreezeMinimumMS   int64   `json:"freeze_minimum_ms"`
	FreezeNoiseDB     float64 `json:"freeze_noise_db"`
	SilenceMinimumMS  int64   `json:"silence_minimum_ms"`
	SilenceNoiseDB    float64 `json:"silence_noise_db"`
}

type ContentQualityInterval struct {
	StartMS    int64 `json:"start_ms"`
	EndMS      int64 `json:"end_ms"`
	DurationMS int64 `json:"duration_ms"`
}

type ContentAVSync struct {
	State          string `json:"state"`
	SamplePairs    int    `json:"sample_pairs"`
	InitialDeltaMS *int64 `json:"initial_delta_ms,omitempty"`
	FinalDeltaMS   *int64 `json:"final_delta_ms,omitempty"`
	DriftMS        *int64 `json:"drift_ms,omitempty"`
	MaxAbsDeltaMS  *int64 `json:"max_abs_delta_ms,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type ContentQualityReport struct {
	Version          string                   `json:"version"`
	State            string                   `json:"state"`
	Reason           string                   `json:"reason,omitempty"`
	SourceArtifact   Artifact                 `json:"source_artifact"`
	CreatedAt        time.Time                `json:"created_at"`
	DurationMS       int64                    `json:"duration_ms"`
	HasVideo         bool                     `json:"has_video"`
	HasAudio         bool                     `json:"has_audio"`
	Thresholds       ContentQualityThresholds `json:"thresholds"`
	BlackIntervals   []ContentQualityInterval `json:"black_intervals"`
	FreezeIntervals  []ContentQualityInterval `json:"freeze_intervals"`
	SilenceIntervals []ContentQualityInterval `json:"silence_intervals"`
	Truncated        bool                     `json:"truncated"`
	AVSync           ContentAVSync            `json:"av_sync"`
	Limitations      []string                 `json:"limitations"`
}

func defaultContentQualityThresholds() ContentQualityThresholds {
	return ContentQualityThresholds{BlackMinimumMS: 1000, BlackPixelRatio: 0.10, BlackPictureRatio: 0.98,
		FreezeMinimumMS: 2000, FreezeNoiseDB: -55, SilenceMinimumMS: 2000, SilenceNoiseDB: -45}
}

func newContentQualityReport(source Artifact) ContentQualityReport {
	return ContentQualityReport{Version: contentQualityVersion, State: "unavailable", SourceArtifact: source, CreatedAt: time.Now().UTC(),
		Thresholds: defaultContentQualityThresholds(), BlackIntervals: []ContentQualityInterval{}, FreezeIntervals: []ContentQualityInterval{},
		SilenceIntervals: []ContentQualityInterval{}, AVSync: ContentAVSync{State: "unavailable"},
		Limitations: []string{"检测仅描述已保存源流的可解码内容，不能重现或证明本机播放器黑屏、冻结或无声。", "末尾尚未结束的检测区间不计数；加密、低帧率、暗场和正常静音内容需要人工复核。"}}
}

func contentQualityInput(ctx context.Context, dir, stage string, report Report) (string, error) {
	if !sourceArtifactMatches(dir, dumpName(report.Manifest.Protocol), report.Manifest.Stream, maxStreamBytes) {
		return "", errors.New("原始流未通过指纹校验")
	}
	switch report.Manifest.Protocol {
	case ProtocolHTTPFLV, ProtocolHTTPTS:
		return filepath.Join(dir, dumpName(report.Manifest.Protocol)), nil
	case ProtocolRTMP:
		path := filepath.Join(stage, "input.flv")
		return path, investigationMediaRTMP(ctx, filepath.Join(dir, rtmpStreamName), path)
	case ProtocolUDPTS, ProtocolRTPTS:
		path := filepath.Join(stage, "input.ts")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return "", err
		}
		writer := bufio.NewWriterSize(file, 64<<10)
		err = walkTSChunks(dir, report.Manifest, func(_ tsSpan, data []byte) error {
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
		return path, errors.Join(err, writer.Flush(), file.Close())
	case ProtocolHLS:
		input, err := investigationMediaHLS(ctx, dir, stage, report, StallInvestigation{Stall: PlayerStall{StartMS: 0, EndMS: report.Manifest.Observation.ElapsedMS}})
		return input.path, err
	case ProtocolRTSP, ProtocolRTSPS:
		input, err := investigationMediaRTSPContent(ctx, dir, stage, report)
		return input.path, err
	default:
		return "", errors.New("当前协议暂不支持整段源内容检测")
	}
}

func analyzeContentQuality(ctx context.Context, dir string, report Report) (ContentQualityReport, error) {
	result := newContentQualityReport(report.Manifest.Stream)
	if isRTSPProtocol(report.Manifest.Protocol) {
		result.Limitations = append(result.Limitations, "RTSP 内容直接根据 SDP 和原始 RTP 离线重建，使用接收端 RTP 时钟；丢包导致的不完整访问单元会被丢弃，重建媒体时间不代表播放器实际呈帧时间。")
	}
	ffmpeg, err := investigationMediaTool("ffmpeg", "")
	if err != nil {
		result.Reason = "未找到 FFmpeg，无法执行源内容检测"
		return result, nil
	}
	ffprobe, err := investigationMediaTool("ffprobe", filepath.Dir(ffmpeg))
	if err != nil {
		result.Reason = "未找到 FFprobe，无法执行源内容检测"
		return result, nil
	}
	stage, err := os.MkdirTemp(dir, ".content-quality-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	input, err := contentQualityInput(ctx, dir, stage, report)
	if err != nil {
		result.Reason = "无法准备可解码源流：" + err.Error()
		return result, nil
	}
	probe, err := investigationMediaInspect(ctx, ffprobe, input)
	if err != nil {
		result.Reason = "源流无法解码或缺少可分析的音视频轨道"
		return result, nil
	}
	result.DurationMS, result.HasVideo, result.HasAudio = probe.durationMS, probe.hasVideo, probe.hasAudio
	logs := make(chan string, 2)
	errs := make(chan error, 2)
	runs := 0
	if probe.hasVideo {
		runs++
		go func() {
			args := contentQualityInputArgs(input)
			args = append(args, "-i", input, "-map", "0:v:0", "-vf", "blackdetect=d=1:pix_th=0.10:pic_th=0.98,freezedetect=n=-55dB:d=2", "-an", "-f", "null", "-")
			output, err := contentQualityFilterRun(ctx, ffmpeg, args)
			logs <- output
			errs <- err
		}()
	}
	if probe.hasAudio {
		runs++
		go func() {
			args := contentQualityInputArgs(input)
			args = append(args, "-i", input, "-map", "0:a:0", "-af", "silencedetect=noise=-45dB:d=2", "-vn", "-f", "null", "-")
			output, err := contentQualityFilterRun(ctx, ffmpeg, args)
			logs <- output
			errs <- err
		}()
	}
	var runErr error
	for index := 0; index < runs; index++ {
		output := <-logs
		if err := <-errs; err != nil {
			runErr = errors.Join(runErr, err)
		}
		result.BlackIntervals, result.FreezeIntervals, result.SilenceIntervals = parseContentQualityLog(output, result.BlackIntervals, result.FreezeIntervals, result.SilenceIntervals)
	}
	if runs == 0 || runErr != nil && len(result.BlackIntervals)+len(result.FreezeIntervals)+len(result.SilenceIntervals) == 0 {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		result.Reason = "FFmpeg 未能完成源内容过滤分析"
		return result, nil
	}
	result.State = "complete"
	if runErr != nil {
		result.State = "partial"
		result.Reason = "部分 FFmpeg 过滤器未完成，已保留成功解析的区间"
	}
	for _, intervals := range [][]ContentQualityInterval{result.BlackIntervals, result.FreezeIntervals, result.SilenceIntervals} {
		if len(intervals) >= 64 {
			result.Truncated = true
		}
	}
	result.AVSync = probeContentAVSync(ctx, ffprobe, input)
	return result, nil
}

func contentQualityInputArgs(path string) []string {
	args := investigationMediaInputArgs(path)
	for index := range args {
		if args[index] == "error" {
			args[index] = "info"
		}
	}
	return args
}

func contentQualityFilterRun(ctx context.Context, program string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.WaitDelay = 2 * time.Second
	var output investigationMediaLimitedBuffer
	output.limit = 1 << 20
	cmd.Stdout = io.Discard
	cmd.Stderr = &output
	err := cmd.Run()
	return output.String(), err
}

var blackIntervalPattern = regexp.MustCompile(`black_start:([0-9.]+) black_end:([0-9.]+) black_duration:([0-9.]+)`)
var freezeStartPattern = regexp.MustCompile(`freeze_start: ([0-9.]+)`)
var freezeDurationPattern = regexp.MustCompile(`freeze_duration: ([0-9.]+)`)
var freezeEndPattern = regexp.MustCompile(`freeze_end: ([0-9.]+)`)
var silenceStartPattern = regexp.MustCompile(`silence_start: ([0-9.]+)`)
var silenceEndPattern = regexp.MustCompile(`silence_end: ([0-9.]+) \| silence_duration: ([0-9.]+)`)

func contentQualityMilliseconds(value string) int64 {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return -1
	}
	return int64(math.Round(seconds * 1000))
}

func parseContentQualityLog(log string, black, freeze, silence []ContentQualityInterval) ([]ContentQualityInterval, []ContentQualityInterval, []ContentQualityInterval) {
	freezeStart, freezeDuration, silenceStart := int64(-1), int64(-1), int64(-1)
	for _, line := range strings.Split(log, "\n") {
		if match := blackIntervalPattern.FindStringSubmatch(line); len(match) == 4 && len(black) < 64 {
			start, end, duration := contentQualityMilliseconds(match[1]), contentQualityMilliseconds(match[2]), contentQualityMilliseconds(match[3])
			if validContentInterval(start, end, duration) {
				black = append(black, ContentQualityInterval{StartMS: start, EndMS: end, DurationMS: duration})
			}
		}
		if match := freezeStartPattern.FindStringSubmatch(line); len(match) == 2 {
			freezeStart = contentQualityMilliseconds(match[1])
		}
		if match := freezeDurationPattern.FindStringSubmatch(line); len(match) == 2 {
			freezeDuration = contentQualityMilliseconds(match[1])
		}
		if match := freezeEndPattern.FindStringSubmatch(line); len(match) == 2 && len(freeze) < 64 {
			end := contentQualityMilliseconds(match[1])
			if validContentInterval(freezeStart, end, freezeDuration) {
				freeze = append(freeze, ContentQualityInterval{StartMS: freezeStart, EndMS: end, DurationMS: freezeDuration})
			}
			freezeStart, freezeDuration = -1, -1
		}
		if match := silenceStartPattern.FindStringSubmatch(line); len(match) == 2 {
			silenceStart = contentQualityMilliseconds(match[1])
		}
		if match := silenceEndPattern.FindStringSubmatch(line); len(match) == 3 && len(silence) < 64 {
			end, duration := contentQualityMilliseconds(match[1]), contentQualityMilliseconds(match[2])
			if validContentInterval(silenceStart, end, duration) {
				silence = append(silence, ContentQualityInterval{StartMS: silenceStart, EndMS: end, DurationMS: duration})
			}
			silenceStart = -1
		}
	}
	return black, freeze, silence
}

func validContentInterval(start, end, duration int64) bool {
	return start >= 0 && end > start && duration > 0 && duration <= 300_000 && end-start >= duration-100
}

func probeContentAVSync(ctx context.Context, ffprobe, input string) ContentAVSync {
	result := ContentAVSync{State: "unavailable"}
	args := append(investigationMediaInputArgs(input), "-show_frames", "-show_entries", "frame=media_type,best_effort_timestamp_time", "-of", "json", input)
	var output investigationMediaLimitedBuffer
	output.limit = 16 << 20
	if err := investigationMediaRun(ctx, ffprobe, args, &output); err != nil || output.Len() >= output.limit {
		result.Reason = "音视频帧时间戳读取失败或超出分析上限"
		return result
	}
	var decoded struct {
		Frames []struct {
			MediaType string `json:"media_type"`
			Timestamp string `json:"best_effort_timestamp_time"`
		} `json:"frames"`
	}
	if json.Unmarshal(output.Bytes(), &decoded) != nil {
		result.Reason = "音视频帧时间戳格式无效"
		return result
	}
	var video, audio []int64
	for _, frame := range decoded.Frames {
		at := contentQualityMilliseconds(frame.Timestamp)
		if at < 0 {
			continue
		}
		if frame.MediaType == "video" && len(video) < 100_000 {
			video = append(video, at)
		}
		if frame.MediaType == "audio" && len(audio) < 100_000 {
			audio = append(audio, at)
		}
	}
	if len(video) == 0 || len(audio) == 0 {
		result.Reason = "缺少可比较的音视频帧时间戳"
		return result
	}
	var deltas []int64
	for _, at := range video {
		index := sortSearchInt64(audio, at)
		best := int64(-1)
		if index < len(audio) {
			best = audio[index]
		}
		if index > 0 && (best < 0 || abs64(at-audio[index-1]) < abs64(at-best)) {
			best = audio[index-1]
		}
		if best >= 0 && abs64(at-best) <= 3000 {
			deltas = append(deltas, at-best)
		}
	}
	if len(deltas) == 0 {
		result.Reason = "没有落在 ±3 秒内的音视频时间戳对"
		return result
	}
	initial, final := deltas[0], deltas[len(deltas)-1]
	drift, maximum := final-initial, int64(0)
	for _, delta := range deltas {
		maximum = max(maximum, abs64(delta))
	}
	result.State, result.SamplePairs, result.InitialDeltaMS, result.FinalDeltaMS, result.DriftMS, result.MaxAbsDeltaMS = "complete", len(deltas), &initial, &final, &drift, &maximum
	return result
}

func sortSearchInt64(values []int64, target int64) int {
	return sortSearch(len(values), func(index int) bool { return values[index] >= target })
}

func sortSearch(n int, match func(int) bool) int {
	low, high := 0, n
	for low < high {
		middle := int(uint(low+high) >> 1)
		if match(middle) {
			high = middle
		} else {
			low = middle + 1
		}
	}
	return low
}

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func validContentQualityReport(value ContentQualityReport, source Artifact) bool {
	if value.Version != contentQualityVersion || value.SourceArtifact != source || value.CreatedAt.IsZero() || value.DurationMS < 0 || value.DurationMS > 300_000 ||
		!slices.Contains([]string{"complete", "partial", "unavailable"}, value.State) || len(value.BlackIntervals) > 64 || len(value.FreezeIntervals) > 64 || len(value.SilenceIntervals) > 64 {
		return false
	}
	for _, intervals := range [][]ContentQualityInterval{value.BlackIntervals, value.FreezeIntervals, value.SilenceIntervals} {
		for _, interval := range intervals {
			if !validContentInterval(interval.StartMS, interval.EndMS, interval.DurationMS) {
				return false
			}
		}
	}
	return true
}

func decodeStoredContentQuality(stored Report, report *Report) error {
	if stored.ContentQuality == nil {
		return nil
	}
	if !validContentQualityReport(*stored.ContentQuality, report.Manifest.Stream) {
		return ErrInvalid
	}
	value := *stored.ContentQuality
	report.ContentQuality = &value
	return nil
}
