package offline

import (
	"os"
	"path/filepath"
	"strings"

	"streamanalyzer/internal/models"
	"streamanalyzer/internal/mp4parse"
)

func (m *Manager) runMP4(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	outDir := filepath.Join(taskDir, "mp4")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	reportPath := filepath.Join(outDir, "mp4_report.txt")
	videoPath := filepath.Join(outDir, "video.annexb")
	audioPath := filepath.Join(outDir, "audio.adts.aac")

	rf, err := os.Create(reportPath)
	if err != nil {
		return err
	}
	err = mp4parse.Run(mp4parse.Config{
		InputPath:      task.InputPath,
		MaxSampleLines: 5000,
		VideoOut:       videoPath,
		AudioOut:       audioPath,
		ReportWriter:   rf,
	})
	_ = rf.Close()
	if err != nil && !missingMP4Track(err) {
		return err
	}

	frames, vcodec, acodec, profile, err := mp4parse.OfflineFrameDetailsFromPath(task.InputPath)
	if err != nil {
		return err
	}
	if profile == nil {
		profile = &models.OfflineMediaProfile{}
	}

	flow := models.OfflineFlowResult{
		FlowID:        1,
		Direction:     "mp4",
		DumpRawDir:    "single",
		RawPath:       task.InputPath,
		VideoPath:     videoPath,
		AudioPath:     audioPath,
		VideoCodec:    vcodec,
		AudioCodec:    acodec,
		ArtifactPaths: []string{reportPath},
	}
	attachMediaProfile(&flow, frames, *profile)

	cleanupEmptyFile(&flow.VideoPath)
	cleanupEmptyFile(&flow.AudioPath)
	if flow.VideoPath != "" {
		if st, e := os.Stat(flow.VideoPath); e == nil {
			flow.PayloadBytes += int(st.Size())
		}
	}
	if flow.AudioPath != "" {
		if st, e := os.Stat(flow.AudioPath); e == nil {
			flow.PayloadBytes += int(st.Size())
		}
	}

	summary.Flows = append(summary.Flows, flow)
	return nil
}

func missingMP4Track(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "未找到视频轨") || strings.Contains(msg, "未找到音频轨")
}
