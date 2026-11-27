package offline

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"streamanalyzer/internal/models"
	"streamanalyzer/internal/simulator/mediafile"
)

var (
	reBlackDetect   = regexp.MustCompile(`black_start:([0-9.]+)\s+black_end:([0-9.]+)\s+black_duration:([0-9.]+)`)
	reFreezeStart   = regexp.MustCompile(`freeze_start:\s*([0-9.]+)`)
	reFreezeEnd     = regexp.MustCompile(`freeze_end:\s*([0-9.]+)\s+freeze_duration:\s*([0-9.]+)`)
	reSilenceStart  = regexp.MustCompile(`silence_start:\s*([0-9.]+)`)
	reSilenceEnd    = regexp.MustCompile(`silence_end:\s*([0-9.]+)\s*\|\s*silence_duration:\s*([0-9.]+)`)
	reDecodeErrHint = regexp.MustCompile(`(?i)(error while decoding|corrupt|invalid data|missing reference|non-existing pps|concealing .* errors|decode_slice_header error|broken frame|truncated|nal unit|cabac|reference picture missing)`)
)

func findFFmpegTool() (string, error) {
	p, err := mediafile.FindTool("ffmpeg", "")
	if err != nil {
		return "", fmt.Errorf("未找到本机 ffmpeg，请先安装 FFmpeg（或在排障工作台配置路径）：%w", err)
	}
	return p, nil
}

// resolvePlayableSource picks the best FFmpeg-readable input path for a task/flow.
func (m *Manager) resolvePlayableSource(task *models.OfflineTask, flow *models.OfflineFlowResult) (string, string, error) {
	if task == nil {
		return "", "", fmt.Errorf("task not found")
	}
	// Prefer original container file when FFmpeg can seek/read it directly.
	switch task.Mode {
	case models.OfflineModeMP4, models.OfflineModeFLV, models.OfflineModeTS,
		models.OfflineModePS, models.OfflineModeWebM, models.OfflineModeAnnexB:
		if task.InputPath != "" {
			if st, err := os.Stat(task.InputPath); err == nil && !st.IsDir() {
				ext := strings.ToLower(filepath.Ext(task.InputPath))
				if ext == "" {
					switch task.Mode {
					case models.OfflineModeFLV:
						ext = ".flv"
					case models.OfflineModeTS:
						ext = ".ts"
					case models.OfflineModePS:
						ext = ".mpg"
					case models.OfflineModeWebM:
						ext = ".webm"
					case models.OfflineModeAnnexB:
						ext = ".h264"
					default:
						ext = ".mp4"
					}
				}
				return task.InputPath, ext, nil
			}
		}
	}
	// For PCAP / RTMP Raw or missing original, fall back to extracted video/raw file.
	if flow != nil {
		for _, cand := range []string{flow.VideoPath, flow.RawPath} {
			if cand == "" {
				continue
			}
			if st, err := os.Stat(cand); err == nil && !st.IsDir() && st.Size() > 0 {
				ext := strings.ToLower(filepath.Ext(cand))
				if ext == "" || ext == ".annexb" {
					if strings.EqualFold(flow.VideoCodec, "h265") {
						ext = ".h265"
					} else {
						ext = ".h264"
					}
				}
				return cand, ext, nil
			}
		}
	}
	return "", "", fmt.Errorf("当前任务没有可供 FFmpeg 读取的媒体文件")
}

// ExtractFrameThumbnail extracts a JPEG thumbnail at the specified timestamp (ms)
// using FFmpeg and caches the result under taskDir/thumbs/.
func (m *Manager) ExtractFrameThumbnail(ctx context.Context, taskID string, flowIdx int, timeMS int64) ([]byte, error) {
	if !isSafeTaskID(taskID) {
		return nil, fmt.Errorf("invalid task id")
	}
	task, summary, err := m.loadTaskAndSummary(taskID)
	if err != nil {
		return nil, err
	}
	var flow *models.OfflineFlowResult
	if summary != nil && len(summary.Flows) > 0 {
		if flowIdx < 0 || flowIdx >= len(summary.Flows) {
			flowIdx = 0
		}
		flow = &summary.Flows[flowIdx]
	}

	srcPath, _, err := m.resolvePlayableSource(task, flow)
	if err != nil {
		return nil, err
	}
	if timeMS < 0 {
		timeMS = 0
	}

	thumbDir := filepath.Join(m.baseDir, "offline", taskID, "thumbs")
	_ = os.MkdirAll(thumbDir, 0o755)
	cacheFile := filepath.Join(thumbDir, fmt.Sprintf("f%d_t%d.jpg", flowIdx, timeMS))
	if b, err := os.ReadFile(cacheFile); err == nil && len(b) > 0 {
		return b, nil
	}

	ffmpegPath, err := findFFmpegTool()
	if err != nil {
		return nil, err
	}

	secStr := fmt.Sprintf("%.3f", float64(timeMS)/1000.0)
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// Try fast input seeking first; if empty (e.g. raw annexb without index), retry with output seeking.
	data, err := runFFmpegThumb(runCtx, ffmpegPath, srcPath, secStr, true)
	if err != nil || len(data) == 0 {
		data, err = runFFmpegThumb(runCtx, ffmpegPath, srcPath, secStr, false)
	}
	if err != nil || len(data) == 0 {
		// Last fallback: grab very first decodable frame.
		data, err = runFFmpegThumb(runCtx, ffmpegPath, srcPath, "0", true)
		if err != nil || len(data) == 0 {
			return nil, fmt.Errorf("抽取画面截图失败：%v", err)
		}
	}

	_ = os.WriteFile(cacheFile, data, 0o644)
	return data, nil
}

func runFFmpegThumb(ctx context.Context, ffmpegPath, srcPath, secStr string, fastSeek bool) ([]byte, error) {
	var args []string
	if fastSeek {
		args = []string{
			"-hide_banner", "-loglevel", "error",
			"-ss", secStr,
			"-i", srcPath,
			"-frames:v", "1",
			"-vf", "scale='min(960,iw)':-2",
			"-q:v", "4",
			"-f", "image2pipe", "-vcodec", "mjpeg", "pipe:1",
		}
	} else {
		args = []string{
			"-hide_banner", "-loglevel", "error",
			"-i", srcPath,
			"-ss", secStr,
			"-frames:v", "1",
			"-vf", "scale='min(960,iw)':-2",
			"-q:v", "4",
			"-f", "image2pipe", "-vcodec", "mjpeg", "pipe:1",
		}
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// RunDecodeAudit executes FFmpeg decode-error, blackdetect, freezedetect and silencedetect
// analysis on the task/flow and caches the JSON result in taskDir.
func (m *Manager) RunDecodeAudit(ctx context.Context, taskID string, flowIdx int, force bool) (*models.OfflineDecodeAudit, error) {
	if !isSafeTaskID(taskID) {
		return nil, fmt.Errorf("invalid task id")
	}
	task, summary, err := m.loadTaskAndSummary(taskID)
	if err != nil {
		return nil, err
	}
	var flow *models.OfflineFlowResult
	if summary != nil && len(summary.Flows) > 0 {
		if flowIdx < 0 || flowIdx >= len(summary.Flows) {
			flowIdx = 0
		}
		flow = &summary.Flows[flowIdx]
	}

	cachePath := filepath.Join(m.baseDir, "offline", taskID, fmt.Sprintf("decode_audit_f%d.json", flowIdx))
	if !force {
		if b, err := os.ReadFile(cachePath); err == nil {
			var cached models.OfflineDecodeAudit
			if json.Unmarshal(b, &cached) == nil && cached.TaskID == taskID {
				return &cached, nil
			}
		}
	}

	srcPath, _, err := m.resolvePlayableSource(task, flow)
	if err != nil {
		return nil, err
	}
	ffmpegPath, err := findFFmpegTool()
	if err != nil {
		return nil, err
	}

	hasAudio := flow == nil || flow.AudioCodec != "" || flow.AudioPath != ""
	if task.Mode == models.OfflineModeAnnexB {
		hasAudio = false
	}

	args := []string{
		"-hide_banner", "-nostats",
		"-loglevel", "info",
		"-err_detect", "explode",
		"-i", srcPath,
		"-vf", "blackdetect=d=0.4:pix_th=0.10,freezedetect=n=-50dB:d=1.0",
	}
	if hasAudio {
		args = append(args, "-af", "silencedetect=n=-45dB:d=1.5")
	} else {
		args = append(args, "-an")
	}
	args = append(args, "-f", "null", "-")

	runCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(runCtx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// If -af failed because stream has no audio track, retry video-only cleanly.
	if runErr != nil && hasAudio && strings.Contains(strings.ToLower(stderr.String()), "matches no streams") {
		stderr.Reset()
		argsNoAudio := []string{
			"-hide_banner", "-nostats",
			"-loglevel", "info",
			"-i", srcPath,
			"-vf", "blackdetect=d=0.4:pix_th=0.10,freezedetect=n=-50dB:d=1.0",
			"-an", "-f", "null", "-",
		}
		cmd2 := exec.CommandContext(runCtx, ffmpegPath, argsNoAudio...)
		cmd2.Stderr = &stderr
		_ = cmd2.Run()
	}

	audit := parseFFmpegAuditLog(stderr.String())
	audit.TaskID = taskID
	audit.FlowIndex = flowIdx
	audit.InputPath = filepath.Base(srcPath)
	audit.DurationMS = time.Since(start).Milliseconds()
	audit.GeneratedAt = time.Now()

	if len(audit.BlackFrames) > 0 {
		audit.Warnings = append(audit.Warnings, fmt.Sprintf("检测到 %d 段黑帧", len(audit.BlackFrames)))
	}
	if len(audit.FreezeFrames) > 0 {
		audit.Warnings = append(audit.Warnings, fmt.Sprintf("检测到 %d 段画面冻结（≥1.0s）", len(audit.FreezeFrames)))
	}
	if len(audit.SilenceRanges) > 0 {
		audit.Warnings = append(audit.Warnings, fmt.Sprintf("检测到 %d 段静音（≥1.5s）", len(audit.SilenceRanges)))
	}
	if audit.DecodeErrors > 0 {
		audit.Warnings = append(audit.Warnings, fmt.Sprintf("检测到 %d 条解码错误日志", audit.DecodeErrors))
	}

	if b, err := json.MarshalIndent(audit, "", "  "); err == nil {
		_ = os.WriteFile(cachePath, b, 0o644)
	}
	return audit, nil
}

func parseFFmpegAuditLog(logText string) *models.OfflineDecodeAudit {
	audit := &models.OfflineDecodeAudit{}
	var pendingFreezeStart *float64
	var pendingSilenceStart *float64

	sc := bufio.NewScanner(strings.NewReader(logText))
	sc.Buffer(make([]byte, 0, 16*1024), 256*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if m := reBlackDetect.FindStringSubmatch(line); len(m) == 4 {
			st, _ := strconv.ParseFloat(m[1], 64)
			en, _ := strconv.ParseFloat(m[2], 64)
			dur, _ := strconv.ParseFloat(m[3], 64)
			audit.BlackFrames = append(audit.BlackFrames, models.OfflineTimeRange{
				StartSec: st, EndSec: en, DurationSec: dur, Detail: "黑帧区间",
			})
			continue
		}
		if m := reFreezeStart.FindStringSubmatch(line); len(m) == 2 {
			st, _ := strconv.ParseFloat(m[1], 64)
			pendingFreezeStart = &st
			continue
		}
		if m := reFreezeEnd.FindStringSubmatch(line); len(m) == 3 {
			en, _ := strconv.ParseFloat(m[1], 64)
			dur, _ := strconv.ParseFloat(m[2], 64)
			st := en - dur
			if pendingFreezeStart != nil {
				st = *pendingFreezeStart
				pendingFreezeStart = nil
			}
			if st < 0 {
				st = 0
			}
			audit.FreezeFrames = append(audit.FreezeFrames, models.OfflineTimeRange{
				StartSec: st, EndSec: en, DurationSec: dur, Detail: "画面冻结",
			})
			continue
		}
		if m := reSilenceStart.FindStringSubmatch(line); len(m) == 2 {
			st, _ := strconv.ParseFloat(m[1], 64)
			pendingSilenceStart = &st
			continue
		}
		if m := reSilenceEnd.FindStringSubmatch(line); len(m) == 3 {
			en, _ := strconv.ParseFloat(m[1], 64)
			dur, _ := strconv.ParseFloat(m[2], 64)
			st := en - dur
			if pendingSilenceStart != nil {
				st = *pendingSilenceStart
				pendingSilenceStart = nil
			}
			if st < 0 {
				st = 0
			}
			audit.SilenceRanges = append(audit.SilenceRanges, models.OfflineTimeRange{
				StartSec: st, EndSec: en, DurationSec: dur, Detail: "静音段",
			})
			continue
		}
		if reDecodeErrHint.MatchString(line) && !strings.Contains(line, "blackdetect") && !strings.Contains(line, "freezedetect") {
			audit.DecodeErrors++
			if len(audit.ErrorSamples) < 20 {
				audit.ErrorSamples = append(audit.ErrorSamples, line)
			}
		}
	}
	return audit
}

// ExportClip creates a lossless (-c copy) clip for [startSec, endSec] under taskDir/clips/
// and returns the relative artifact path usable with /api/offline/tasks/:id/files/<rel>.
func (m *Manager) ExportClip(ctx context.Context, taskID string, flowIdx int, startSec, endSec float64) (relPath string, sizeBytes int64, err error) {
	if !isSafeTaskID(taskID) {
		return "", 0, fmt.Errorf("invalid task id")
	}
	if startSec < 0 || endSec <= startSec {
		return "", 0, fmt.Errorf("切片区间无效：要求 0 <= start_sec < end_sec")
	}
	if endSec-startSec > 600 {
		return "", 0, fmt.Errorf("单次切片时长上限为 600 秒")
	}

	task, summary, err := m.loadTaskAndSummary(taskID)
	if err != nil {
		return "", 0, err
	}
	var flow *models.OfflineFlowResult
	if summary != nil && len(summary.Flows) > 0 {
		if flowIdx < 0 || flowIdx >= len(summary.Flows) {
			flowIdx = 0
		}
		flow = &summary.Flows[flowIdx]
	}

	srcPath, ext, err := m.resolvePlayableSource(task, flow)
	if err != nil {
		return "", 0, err
	}
	ffmpegPath, err := findFFmpegTool()
	if err != nil {
		return "", 0, err
	}

	outExt := ".mp4"
	switch ext {
	case ".flv", ".ts", ".webm", ".mkv", ".mp4":
		outExt = ext
	case ".mpg", ".ps":
		outExt = ".ts"
	case ".h264", ".h265":
		outExt = ".mp4"
	}

	clipDir := filepath.Join(m.baseDir, "offline", taskID, "clips")
	if err := os.MkdirAll(clipDir, 0o755); err != nil {
		return "", 0, err
	}
	fileName := fmt.Sprintf("clip_f%d_%.1fs_%.1fs%s", flowIdx, startSec, endSec, outExt)
	outPath := filepath.Join(clipDir, fileName)

	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-ss", fmt.Sprintf("%.3f", startSec),
		"-to", fmt.Sprintf("%.3f", endSec),
		"-i", srcPath,
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
	}
	if outExt == ".mp4" {
		args = append(args, "-movflags", "+faststart")
	}
	args = append(args, outPath)

	cmd := exec.CommandContext(runCtx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(outPath)
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", 0, fmt.Errorf("无损切片失败：%s", msg)
		}
		return "", 0, fmt.Errorf("无损切片失败：%w", err)
	}
	st, err := os.Stat(outPath)
	if err != nil || st.Size() == 0 {
		_ = os.Remove(outPath)
		return "", 0, fmt.Errorf("无损切片输出为空，请确认区间在媒体时长范围内")
	}

	return filepath.ToSlash(filepath.Join("clips", fileName)), st.Size(), nil
}
