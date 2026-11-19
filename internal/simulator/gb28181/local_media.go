package gb28181

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"streamanalyzer/internal/simulator/mediafile"
)

const MaxLocalVideoBytes = 512 << 20

var ErrVideoTooLarge = errors.New("本地视频超过 512 MiB")

const localVideoFormats = "mov,matroska,webm,flv,mpegts,avi,mpeg"

type SourceInfo struct {
	Filename    string `json:"filename,omitempty"`
	VideoCodec  string `json:"video_codec,omitempty"`
	OutputCodec string `json:"output_codec,omitempty"`
	VideoMode   string `json:"video_mode,omitempty"`
	AudioCodec  string `json:"audio_codec,omitempty"`
}

type localMedia struct {
	info       SourceInfo
	path       string
	ffmpeg     string
	videoIndex int
	audioIndex int
	duration   time.Duration
	recordings []RecordingInfo
}

func prepareLocalMedia(ctx context.Context, name, audio string, input io.Reader) (*localMedia, error) {
	switch audio {
	case "":
		audio = "g711a"
	case "g711a", "g711u", "none":
	default:
		return nil, errors.New("请选择 G.711 A-law、G.711 μ-law 或仅视频")
	}

	ffmpeg, err := mediafile.FindTool("ffmpeg", "")
	if err != nil {
		return nil, err
	}
	ffprobe, err := mediafile.FindTool("ffprobe", filepath.Dir(ffmpeg))
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", "streamanalyzer-gb28181-*.video")
	if err != nil {
		return nil, errors.New("无法创建本地视频临时文件")
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(file.Name())
		}
	}()
	size, err := io.Copy(file, io.LimitReader(input, MaxLocalVideoBytes+1))
	if err := errors.Join(err, file.Close()); err != nil {
		return nil, fmt.Errorf("读取所选视频失败：%w", err)
	}
	if size > MaxLocalVideoBytes {
		return nil, ErrVideoTooLarge
	}
	if size == 0 {
		return nil, errors.New("所选视频文件为空")
	}
	name = truncate(path.Base(strings.ReplaceAll(name, "\\", "/")), 240)
	profile := &localMedia{path: file.Name(), ffmpeg: ffmpeg, videoIndex: -1, audioIndex: -1,
		info: SourceInfo{Filename: name, VideoMode: "transcode", OutputCodec: "h264", AudioCodec: "none"}}
	if err := profile.probe(ctx, ffprobe, audio); err != nil {
		return nil, err
	}
	keep = true
	return profile, nil
}

func (m *localMedia) remove() {
	_ = os.Remove(m.path)
}

func (m *localMedia) probe(parent context.Context, ffprobe, audio string) error {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, ffprobe, "-v", "error",
		"-protocol_whitelist", "file", "-format_whitelist", localVideoFormats,
		"-show_entries", "stream=index,codec_name,codec_type:stream_disposition=attached_pic:format=duration", "-of", "json", m.path).Output()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("识别视频文件失败：%w", ctx.Err())
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("无法读取所选视频：%s", m.cleanError(string(exit.Stderr)))
		}
		return fmt.Errorf("启动 ffprobe 失败：%w", err)
	}
	var file struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Index int    `json:"index"`
			Codec string `json:"codec_name"`
			Type  string `json:"codec_type"`
			Flags struct {
				Picture int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &file); err != nil {
		return fmt.Errorf("无法读取视频轨道信息：%w", err)
	}
	if seconds, err := strconv.ParseFloat(file.Format.Duration, 64); err == nil && seconds > 0 && seconds < float64((1<<63-1)/int64(time.Second)) {
		m.duration = time.Duration(seconds * float64(time.Second))
	}
	for _, stream := range file.Streams {
		switch {
		case stream.Type == "video" && stream.Flags.Picture == 0 && m.videoIndex < 0:
			m.videoIndex, m.info.VideoCodec = stream.Index, stream.Codec
			if stream.Codec == "h264" || stream.Codec == "hevc" {
				m.info.VideoMode, m.info.OutputCodec = "copy", stream.Codec
			}
		case stream.Type == "audio" && audio != "none" && m.audioIndex < 0:
			m.audioIndex, m.info.AudioCodec = stream.Index, audio
		}
	}
	if m.videoIndex < 0 {
		return errors.New("所选文件没有可点播的视频轨道")
	}
	return nil
}

func (m *localMedia) cleanError(s string) string {
	return truncate(strings.TrimSpace(strings.ReplaceAll(s, m.path, m.info.Filename)), 700)
}

func (m *localMedia) play(ctx context.Context, send func(mediaUnit) error) error {
	if err := m.read(ctx, nil, paceMedia(ctx, 1, send)); !errors.Is(err, io.EOF) {
		return err
	}
	return errors.New("本地视频循环提前结束")
}

func (m *localMedia) read(parent context.Context, span *fileRange, send func(mediaUnit) error) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file", "-format_whitelist", localVideoFormats,
	}
	if span == nil {
		args = append(args, "-stream_loop", "-1")
	} else if span.start > 0 {
		args = append(args, "-ss", secondsArg(span.start))
	}
	args = append(args, "-i", m.path, "-map", "0:"+strconv.Itoa(m.videoIndex))
	if span != nil {
		args = append(args, "-t", secondsArg(span.end-span.start))
	}
	if m.info.VideoMode == "copy" {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p",
			"-fps_mode", "passthrough", "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2")
	}
	if m.audioIndex >= 0 {
		audioEncoder := "pcm_alaw"
		if m.info.AudioCodec == "g711u" {
			audioEncoder = "pcm_mulaw"
		}
		args = append(args, "-map", "0:"+strconv.Itoa(m.audioIndex), "-c:a", audioEncoder,
			"-af", "aresample=8000,asetnsamples=n=160:p=0", "-ar", "8000", "-ac", "1")
	}
	args = append(args, "-max_interleave_delta", "1000000", "-flush_packets", "1",
		"-avoid_negative_ts", "make_non_negative", "-mpegts_copyts", "1", "-muxdelay", "0",
		"-omit_video_pes_length", "0", "-pes_payload_size", "0",
		"-mpegts_start_pid", "256", "-f", "mpegts", "pipe:1")
	cmd := exec.CommandContext(ctx, m.ffmpeg, args...)
	var stderr mediaStderr
	cmd.Stderr = &stderr
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("打开 FFmpeg 输出失败：%w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 FFmpeg 失败：%w", err)
	}
	readErr := readMediaTS(ctx, bufio.NewReaderSize(stdout, 32<<10), m.info.OutputCodec, m.audioIndex >= 0, send)
	if !errors.Is(readErr, io.EOF) {
		cancel()
	}
	waitErr := cmd.Wait()
	if err := parent.Err(); err != nil {
		return err
	}
	if errors.Is(readErr, errPlaybackSeek) || errors.Is(readErr, errPlaybackStop) {
		return readErr
	}
	if detail := m.cleanError(stderr.String()); detail != "" && waitErr != nil {
		return fmt.Errorf("FFmpeg 处理本地视频失败：%s", detail)
	}
	if !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if waitErr != nil {
		return fmt.Errorf("FFmpeg 停止推流：%w", waitErr)
	}
	return io.EOF
}

// Keep stderr draining without accumulating repeated decode errors over a long session.
type mediaStderr struct{ strings.Builder }

func (w *mediaStderr) Write(p []byte) (int, error) {
	remaining := 2048 - w.Len()
	if remaining > 0 {
		_, _ = w.Builder.Write(p[:min(len(p), remaining)])
	}
	return len(p), nil
}
