package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type MediaToolSettings struct {
	FFmpegPath  string `json:"ffmpeg_path,omitempty"`
	FFprobePath string `json:"ffprobe_path,omitempty"`
}

type MediaToolCapability struct {
	Available     bool   `json:"available"`
	Path          string `json:"path,omitempty"`
	Version       string `json:"version,omitempty"`
	Error         string `json:"error,omitempty"`
	AAC           bool   `json:"aac"`
	H264          bool   `json:"h264"`
	HEVC          bool   `json:"hevc"`
	Opus          bool   `json:"opus"`
	Blackdetect   bool   `json:"blackdetect"`
	Freezedetect  bool   `json:"freezedetect"`
	Silencedetect bool   `json:"silencedetect"`
}

type MediaToolsStatus struct {
	Settings MediaToolSettings   `json:"settings"`
	FFmpeg   MediaToolCapability `json:"ffmpeg"`
	FFprobe  MediaToolCapability `json:"ffprobe"`
}

func mediaToolEnv(settings MediaToolSettings) {
	_ = os.Setenv("STREAMANALYZER_FFMPEG", strings.TrimSpace(settings.FFmpegPath))
	_ = os.Setenv("STREAMANALYZER_FFPROBE", strings.TrimSpace(settings.FFprobePath))
}

func (s *Service) loadMediaTools() error {
	data, err := os.ReadFile(filepath.Join(s.root, "media-tools.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var settings MediaToolSettings
	if len(data) > 64<<10 || json.Unmarshal(data, &settings) != nil {
		return fmt.Errorf("media tools settings are invalid")
	}
	mediaToolEnv(settings)
	return nil
}

func (s *Service) MediaTools() MediaToolsStatus {
	return testMediaTools(MediaToolSettings{FFmpegPath: os.Getenv("STREAMANALYZER_FFMPEG"), FFprobePath: os.Getenv("STREAMANALYZER_FFPROBE")})
}

func (s *Service) SaveMediaTools(settings MediaToolSettings) (MediaToolsStatus, error) {
	settings.FFmpegPath = strings.TrimSpace(settings.FFmpegPath)
	settings.FFprobePath = strings.TrimSpace(settings.FFprobePath)
	previous := MediaToolSettings{FFmpegPath: os.Getenv("STREAMANALYZER_FFMPEG"), FFprobePath: os.Getenv("STREAMANALYZER_FFPROBE")}
	mediaToolEnv(settings)
	status := testMediaTools(settings)
	if !status.FFmpeg.Available || !status.FFprobe.Available {
		mediaToolEnv(previous)
		return status, fmt.Errorf("%w: FFmpeg 或 FFprobe 路径不可用", ErrInvalid)
	}
	saved := false
	defer func() {
		if !saved {
			mediaToolEnv(previous)
		}
	}()
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return status, err
	}
	path := filepath.Join(s.root, "media-tools.json")
	tmp, err := os.CreateTemp(s.root, ".media-tools-*.json")
	if err != nil {
		return status, err
	}
	defer os.Remove(tmp.Name())
	if err := errors.Join(tmp.Chmod(0o600), writeAndClose(tmp, data), os.Rename(tmp.Name(), path)); err != nil {
		return status, err
	}
	saved = true
	return status, nil
}

func writeAndClose(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func testMediaTools(settings MediaToolSettings) MediaToolsStatus {
	mediaToolEnv(settings)
	status := MediaToolsStatus{Settings: settings}
	status.FFmpeg = probeMediaTool("ffmpeg", "")
	if status.FFmpeg.Available {
		status.FFprobe = probeMediaTool("ffprobe", filepath.Dir(status.FFmpeg.Path))
	} else {
		status.FFprobe = probeMediaTool("ffprobe", "")
	}
	if status.FFmpeg.Available {
		encoders := mediaToolOutput("ffmpeg", "-encoders")
		filters := mediaToolOutput("ffmpeg", "-filters")
		status.FFmpeg.AAC = strings.Contains(encoders, " aac ") || strings.Contains(encoders, " aac\n")
		status.FFmpeg.H264 = strings.Contains(encoders, "libx264")
		status.FFmpeg.HEVC = strings.Contains(encoders, "libx265")
		status.FFmpeg.Opus = strings.Contains(encoders, "libopus")
		status.FFmpeg.Blackdetect = strings.Contains(filters, "blackdetect")
		status.FFmpeg.Freezedetect = strings.Contains(filters, "freezedetect")
		status.FFmpeg.Silencedetect = strings.Contains(filters, "silencedetect")
	}
	return status
}

func probeMediaTool(name, alongside string) MediaToolCapability {
	path, err := investigationMediaTool(name, alongside)
	if err != nil {
		return MediaToolCapability{Error: "未找到可执行文件"}
	}
	result := MediaToolCapability{Path: path, Available: true}
	result.Version = strings.SplitN(mediaToolOutput(path, "-version"), "\n", 2)[0]
	if result.Version == "" {
		result.Available = false
		result.Error = "无法读取工具版本"
	}
	return result
}

func mediaToolOutput(program string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	var output investigationMediaLimitedBuffer
	output.limit = 4 << 20
	cmd.Stdout = &output
	cmd.Stderr = &output
	if cmd.Run() != nil {
		return ""
	}
	return output.String()
}
