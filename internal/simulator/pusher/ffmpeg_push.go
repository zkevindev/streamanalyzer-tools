package pusher

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"streamanalyzer/internal/simulator/mediafile"
)

func findFFmpeg() (string, error) {
	path, err := mediafile.FindTool("ffmpeg", "")
	if err != nil {
		return "", fmt.Errorf("未找到本机 ffmpeg，请先安装 FFmpeg（如 brew install ffmpeg）：%w", err)
	}
	return path, nil
}

func buildFFmpegArgs(cfg Config) []string {
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-nostats",
		"-progress", "pipe:1",
	}

	switch cfg.SourceKind {
	case SourceFile, SourceUpload:
		args = append(args, "-re", "-fflags", "+genpts")
		if cfg.Loop != nil && *cfg.Loop {
			if cfg.LoopCount <= 0 {
				args = append(args, "-stream_loop", "-1")
			} else if cfg.LoopCount > 1 {
				args = append(args, "-stream_loop", strconv.Itoa(cfg.LoopCount-1))
			}
		}
		args = append(args, "-i", cfg.FilePath)
		args = append(args, "-map", "0:v:0?")
		if cfg.AudioMode != "none" {
			args = append(args, "-map", "0:a:0?")
		}

	case SourcePattern:
		args = append(args, "-re", "-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%s:rate=%d", cfg.Resolution, cfg.FPS))
		if cfg.AudioMode != "none" {
			args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000")
			args = append(args, "-map", "0:v:0", "-map", "1:a:0")
		} else {
			args = append(args, "-map", "0:v:0")
		}
		if cfg.Loop == nil || !*cfg.Loop {
			args = append(args, "-t", "30")
		} else if cfg.LoopCount > 0 {
			args = append(args, "-t", strconv.Itoa(cfg.LoopCount*30))
		}

	case SourceBrowser:
		args = append(args,
			"-fflags", "+nobuffer+genpts",
			"-flags", "low_delay",
			"-analyzeduration", "1000000",
			"-probesize", "500000",
			"-i", "pipe:0",
			"-map", "0:v:0?",
		)
		if cfg.AudioMode != "none" {
			args = append(args, "-map", "0:a:0?")
		}
	}

	// Video codec
	if cfg.VideoMode == "copy" && (cfg.SourceKind == SourceFile || cfg.SourceKind == SourceUpload) {
		args = append(args, "-c:v", "copy")
	} else {
		preset := "veryfast"
		if cfg.SourceKind == SourceBrowser {
			preset = "ultrafast"
		}
		gop := cfg.FPS * 2
		if gop < 25 {
			gop = 25
		}
		bitrate := fmt.Sprintf("%dk", cfg.VideoBitrateKbps)
		bufsize := fmt.Sprintf("%dk", cfg.VideoBitrateKbps*2)
		args = append(args,
			"-c:v", "libx264",
			"-preset", preset,
			"-tune", "zerolatency",
			"-pix_fmt", "yuv420p",
			"-g", strconv.Itoa(gop),
			"-keyint_min", strconv.Itoa(gop),
			"-sc_threshold", "0",
			"-b:v", bitrate,
			"-maxrate", bitrate,
			"-bufsize", bufsize,
		)
	}

	// Audio codec
	switch cfg.AudioMode {
	case "none":
		args = append(args, "-an")
	case "copy":
		args = append(args, "-c:a", "copy")
	case "opus":
		args = append(args, "-c:a", "libopus", "-ar", "48000", "-ac", "2", "-b:a", "96k")
	default: // aac
		args = append(args, "-c:a", "aac", "-ar", "48000", "-ac", "2", "-b:a", "128k")
	}

	// Output muxer
	switch cfg.Protocol {
	case ProtocolRTMP:
		args = append(args, "-f", "flv", cfg.TargetURL)
	case ProtocolRTSP:
		args = append(args, "-rtsp_transport", cfg.RTSPTransport, "-f", "rtsp", cfg.TargetURL)
	}

	return args
}

func (t *pushTask) runFFmpeg(ctx context.Context, ffmpegPath string) {
	args := buildFFmpegArgs(t.cfg)
	t.appendLog(fmt.Sprintf("启动 FFmpeg 推送 %s -> %s", strings.ToUpper(string(t.cfg.Protocol)), redactTargetURL(t.cfg.TargetURL)))

	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	if t.pipeReader != nil {
		cmd.Stdin = t.pipeReader
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.finish(StatusFailed, fmt.Sprintf("创建 stdout 管道失败：%v", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.finish(StatusFailed, fmt.Sprintf("创建 stderr 管道失败：%v", err))
		return
	}

	if err := cmd.Start(); err != nil {
		t.finish(StatusFailed, fmt.Sprintf("启动 ffmpeg 失败：%v", err))
		return
	}

	errDone := make(chan struct{})
	go func() {
		defer close(errDone)
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 16*1024), 256*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				t.appendLog(line)
			}
		}
	}()

	// Parse -progress pipe:1 output
	progDone := make(chan struct{})
	go func() {
		defer close(progDone)
		t.parseFFmpegProgress(stdout)
	}()

	waitErr := cmd.Wait()
	<-progDone
	<-errDone

	if t.isStopRequested() || errors.Is(ctx.Err(), context.Canceled) {
		t.finish(StatusStopped, "")
		return
	}
	if waitErr != nil {
		reason := t.lastErrorLog()
		if reason == "" {
			reason = waitErr.Error()
		}
		t.finish(StatusFailed, reason)
		return
	}
	t.finish(StatusStopped, "")
}

func (t *pushTask) parseFFmpegProgress(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 8*1024), 64*1024)

	var (
		frames      int64
		fps         float64
		totalBytes  int64
		outTimeUS   int64
		bitrateKbps float64
		hasData     bool
		prevBytes   int64
		prevTime    = time.Now()
	)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "frame":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				frames = n
				if n > 0 {
					hasData = true
				}
			}
		case "fps":
			if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
				fps = f
			}
		case "total_size":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
				totalBytes = n
				if n > 0 {
					hasData = true
				}
			}
		case "out_time_us":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
				outTimeUS = n
			}
		case "out_time_ms":
			// Note: FFmpeg's out_time_ms is historically microseconds.
			if outTimeUS == 0 {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
					outTimeUS = n
				}
			}
		case "bitrate":
			if f := parseFFmpegBitrateKbps(v); f > 0 {
				bitrateKbps = f
			}
		case "progress":
			now := time.Now()
			if bitrateKbps <= 0 && totalBytes > prevBytes {
				dt := now.Sub(prevTime).Seconds()
				if dt >= 0.2 {
					bitrateKbps = float64(totalBytes-prevBytes) * 8 / 1000 / dt
				}
			}
			prevBytes = totalBytes
			prevTime = now

			t.mu.Lock()
			if hasData && t.status == StatusStarting {
				t.status = StatusPushing
			}
			t.framesSent = frames
			t.fps = fps
			t.bytesSent = totalBytes
			t.outTimeMS = outTimeUS / 1000
			if bitrateKbps > 0 {
				t.bitrateKbps = bitrateKbps
			}
			t.mu.Unlock()
		}
	}
}

func parseFFmpegBitrateKbps(raw string) float64 {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" || s == "n/a" {
		return 0
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "kbits/s"):
		s = strings.TrimSuffix(s, "kbits/s")
	case strings.HasSuffix(s, "mbits/s"):
		s = strings.TrimSuffix(s, "mbits/s")
		mult = 1000.0
	case strings.HasSuffix(s, "bits/s"):
		s = strings.TrimSuffix(s, "bits/s")
		mult = 0.001
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 {
		return 0
	}
	return v * mult
}
