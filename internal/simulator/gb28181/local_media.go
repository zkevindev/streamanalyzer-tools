package gb28181

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"streamanalyzer/internal/codec"

	"github.com/yutopp/go-flv"
	"github.com/yutopp/go-flv/tag"
)

const MaxLocalVideoBytes = 512 << 20

var ErrVideoTooLarge = errors.New("本地视频超过 512 MiB")

const localVideoFormats = "mov,matroska,webm,flv,mpegts,avi,mpeg"

type SourceInfo struct {
	Filename   string `json:"filename,omitempty"`
	VideoCodec string `json:"video_codec,omitempty"`
	VideoMode  string `json:"video_mode,omitempty"`
	AudioCodec string `json:"audio_codec,omitempty"`
}

type localMedia struct {
	info       SourceInfo
	path       string
	ffmpeg     string
	videoIndex int
	audioIndex int
}

func prepareLocalMedia(ctx context.Context, name, audio string, input io.Reader) (*localMedia, error) {
	switch audio {
	case "":
		audio = "g711a"
	case "g711a", "g711u", "none":
	default:
		return nil, errors.New("请选择 G.711 A-law、G.711 μ-law 或仅视频")
	}

	ffmpeg, err := findMediaTool("ffmpeg", "")
	if err != nil {
		return nil, err
	}
	ffprobe, err := findMediaTool("ffprobe", filepath.Dir(ffmpeg))
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
		info: SourceInfo{Filename: name, VideoMode: "transcode", AudioCodec: "none"}}
	if err := profile.probe(ctx, ffprobe, audio); err != nil {
		return nil, err
	}
	keep = true
	return profile, nil
}

func findMediaTool(name, alongside string) (string, error) {
	dirs := []string{}
	if alongside != "" {
		dirs = append(dirs, alongside)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	dirs = append(dirs, "") // Resolve through PATH.
	if runtime.GOOS == "darwin" {
		// LaunchServices apps typically don't inherit the terminal PATH.
		dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin")
	}
	for _, dir := range dirs {
		if resolved, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("未找到 %s，本地视频需要安装 FFmpeg 和 ffprobe", name)
}

func (m *localMedia) remove() {
	_ = os.Remove(m.path)
}

func (m *localMedia) probe(parent context.Context, ffprobe, audio string) error {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, ffprobe, "-v", "error",
		"-protocol_whitelist", "file", "-format_whitelist", localVideoFormats,
		"-show_entries", "stream=index,codec_name,codec_type:stream_disposition=attached_pic", "-of", "json", m.path).Output()
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
	for _, stream := range file.Streams {
		switch {
		case stream.Type == "video" && stream.Flags.Picture == 0 && m.videoIndex < 0:
			m.videoIndex, m.info.VideoCodec = stream.Index, stream.Codec
			if stream.Codec == "h264" {
				m.info.VideoMode = "copy"
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

func (m *localMedia) play(parent context.Context, send func(mediaUnit) error) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "file", "-format_whitelist", localVideoFormats,
		"-re", "-stream_loop", "-1", "-i", m.path, "-map", "0:" + strconv.Itoa(m.videoIndex)}
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
		"-flvflags", "no_duration_filesize+no_metadata", "-f", "flv", "pipe:1")
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
	readErr := readMediaFLV(bufio.NewReaderSize(stdout, 32<<10), send)
	if !errors.Is(readErr, io.EOF) {
		cancel()
	}
	waitErr := cmd.Wait()
	if err := parent.Err(); err != nil {
		return err
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
	return errors.New("本地视频循环提前结束")
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

func readMediaFLV(reader io.Reader, send func(mediaUnit) error) error {
	decoder, err := flv.NewDecoder(reader)
	if err != nil {
		return fmt.Errorf("读取 FFmpeg 媒体头失败：%w", err)
	}
	var params []byte
	var lengthSize int
	var lastTimestamp uint32
	var epoch int64
	annexStart := []byte{0, 0, 0, 1}
	for {
		var entry tag.FlvTag
		if err := decoder.Decode(&entry); err != nil {
			return fmt.Errorf("读取 FFmpeg 媒体包失败：%w", err)
		}
		if lastTimestamp > entry.Timestamp && lastTimestamp-entry.Timestamp > 1<<31 {
			epoch += 1 << 32
		}
		lastTimestamp = entry.Timestamp
		dts := (epoch + int64(entry.Timestamp)) * 90
		unit := mediaUnit{pts: dts, dts: dts}
		switch data := entry.Data.(type) {
		case *tag.VideoData:
			payload, err := io.ReadAll(data)
			if err != nil {
				return err
			}
			if data.CodecID != tag.CodecIDAVC {
				return errors.New("FFmpeg 视频输出应为 H.264")
			}
			switch data.AVCPacketType {
			case tag.AVCPacketTypeSequenceHeader:
				cfg, err := codec.ParseAVCDecoderConfigurationRecord(payload)
				if err != nil {
					return fmt.Errorf("读取 H.264 配置失败：%w", err)
				}
				lengthSize, params = int(cfg.LengthSizeMinusOne)+1, params[:0]
				for _, sets := range [][][]byte{cfg.SequenceParameterSets, cfg.PictureParameterSets} {
					for _, nal := range sets {
						params = append(params, annexStart...)
						params = append(params, nal...)
					}
				}
				continue
			case tag.AVCPacketTypeNALU:
				if lengthSize == 0 {
					return errors.New("H.264 视频缺少参数集配置")
				}
				annex, err := codec.LengthPrefixedNALToAnnexB(payload, lengthSize)
				if err != nil {
					return fmt.Errorf("读取 H.264 访问单元失败：%w", err)
				}
				unit.stream, unit.pts = videoStreamID, dts+int64(data.CompositionTime)*90
				unit.data = annex
				if data.FrameType == tag.FrameTypeKeyFrame {
					unit.key = true
					unit.data = prependParameterSets(annex, params)
				}
			default:
				continue
			}
		case *tag.AudioData:
			if data.SoundFormat != tag.SoundFormatG711ALawLogarithmicPCM && data.SoundFormat != tag.SoundFormatG711muLawLogarithmicPCM {
				return errors.New("FFmpeg 音频输出应为 G.711")
			}
			unit.stream = audioStreamID
			unit.data, err = io.ReadAll(data)
			if err != nil {
				return err
			}
		default:
			continue
		}
		if len(unit.data) != 0 {
			if err := send(unit); err != nil {
				return err
			}
		}
	}
}

// Keep an in-band access-unit delimiter ahead of any reinserted SPS and PPS.
func prependParameterSets(annex, params []byte) []byte {
	if len(annex) < 5 || len(params) == 0 {
		return annex
	}
	prefixLen := 0
	if annex[4]&0x1f == 9 {
		next := bytes.Index(annex[5:], []byte{0, 0, 0, 1})
		if next < 0 {
			return annex
		}
		prefixLen = 5 + next
	}
	out := make([]byte, 0, len(annex)+len(params))
	out = append(out, annex[:prefixLen]...)
	out = append(out, params...)
	return append(out, annex[prefixLen:]...)
}
