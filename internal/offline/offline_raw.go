package offline

import (
	"os"
	"path/filepath"

	"streamanalyzer/internal/models"
	"streamanalyzer/internal/rtmpraw"
)

func (m *Manager) runRaw(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	outDir := filepath.Join(taskDir, "raw")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	videoPath := filepath.Join(outDir, "video.annexb")
	audioPath := filepath.Join(outDir, "audio.adts.aac")

	in, err := os.Open(task.InputPath)
	if err != nil {
		return err
	}
	defer in.Close()

	vf, err := os.Create(videoPath)
	if err != nil {
		return err
	}
	af, err := os.Create(audioPath)
	if err != nil {
		_ = vf.Close()
		return err
	}

	frameDetails := make([]models.OfflineFrameDetail, 0, 2048)
	res, parseErr := rtmpraw.ParseRTMPRaw(in, rtmpraw.Options{SkipBytes: task.SkipBytes}, rtmpraw.Output{
		VideoWriter: vf,
		AudioWriter: af,
		OnFrame: func(fi rtmpraw.FrameInfo) {
			frameDetails = append(frameDetails, models.OfflineFrameDetail{
				MediaType: fi.MediaType,
				DTS:       fi.DTS,
				PTS:       fi.PTS,
				FrameLen:  fi.FrameLen,
				FrameType: fi.FrameType,
			})
		},
	})
	_ = vf.Close()
	_ = af.Close()

	flow := models.OfflineFlowResult{
		FlowID:       1,
		Direction:    "raw",
		DumpRawDir:   "single",
		RawPath:      task.InputPath,
		VideoPath:    videoPath,
		AudioPath:    audioPath,
		VideoCodec:   res.VideoCodec,
		FrameDetails: frameDetails,
	}
	if parseErr != nil {
		flow.Error = parseErr.Error()
	}
	summary.Flows = append(summary.Flows, flow)

	// Cleanup: if audio file is empty, delete and hide it, and compute payload size
	// from produced artifacts (video/audio) instead of TCP payload bytes.
	cleanupEmptyFile(&summary.Flows[len(summary.Flows)-1].AudioPath)
	i := len(summary.Flows) - 1
	var videoSize int64
	if st, err := os.Stat(summary.Flows[i].VideoPath); err == nil {
		videoSize = st.Size()
	}
	var audioSize int64
	if summary.Flows[i].AudioPath != "" {
		if st, err := os.Stat(summary.Flows[i].AudioPath); err == nil {
			audioSize = st.Size()
		}
	}
	summary.Flows[i].PayloadBytes = int(videoSize + audioSize)
	return parseErr
}

func cleanupEmptyFile(path *string) {
	if path == nil || *path == "" {
		return
	}
	st, err := os.Stat(*path)
	if err != nil {
		return
	}
	if st.Size() == 0 {
		_ = os.Remove(*path)
		*path = ""
	}
}
