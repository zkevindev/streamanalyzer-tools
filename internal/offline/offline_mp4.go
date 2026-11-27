package offline

import (
	"os"
	"path/filepath"
	"strings"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
	"streamanalyzer/internal/mp4parse"
)

func (m *Manager) runMP4(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	ctx := m.taskContext(task.ID)
	outDir := filepath.Join(taskDir, "mp4")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	reportPath := filepath.Join(outDir, "mp4_report.txt")
	videoPath := filepath.Join(outDir, "video.annexb")
	audioPath := filepath.Join(outDir, "audio.adts.aac")

	m.setProgressPercent(task.ID, "导出 MP4 结构报告与音视频裸流", 15, 0)
	if ctx.Err() != nil {
		return ctx.Err()
	}

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
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.setProgressPercent(task.ID, "提取 MP4 帧时间线与字节定位", 60, task.InputSize/2)

	frames, vcodec, acodec, profile, err := mp4parse.OfflineFrameDetailsWithContext(ctx, task.InputPath)
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

	cleanupEmptyFile(&flow.VideoPath)
	cleanupEmptyFile(&flow.AudioPath)

	// If audio export failed or produced no ADTS file, ensure audio frames do not point to missing audio file.
	if flow.AudioPath == "" {
		for i := range frames {
			if frames[i].StreamSource == "audio" {
				frames[i].StreamSource = ""
				frames[i].StreamOffset = 0
				frames[i].StreamLen = 0
			}
		}
	}

	m.setProgressPercent(task.ID, "执行 MP4 深度媒体体检", 88, task.InputSize)
	samples := buildMP4TimedSamples(task.InputPath, frames, vcodec, profile)
	hctx := healthContext{
		Mode:                  models.OfflineModeMP4,
		IsHEVC:                vcodec == "h265",
		HasContainerSeqHeader: profile.Width > 0 || vcodec == "h264" || vcodec == "h265",
		CheckIDRParams:        vcodec == "h264" || vcodec == "h265",
	}
	attachMediaProfileWithSamples(&flow, frames, samples, *profile, hctx)

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

func buildMP4TimedSamples(inputPath string, frames []models.OfflineFrameDetail, vcodec string, profile *models.OfflineMediaProfile) []timedSample {
	isHEVC := vcodec == "h265"
	hasContainerSeq := profile != nil && profile.Width > 0
	var inFile *os.File
	if (vcodec == "h264" || vcodec == "h265") && inputPath != "" {
		if f, err := os.Open(inputPath); err == nil {
			inFile = f
			defer inFile.Close()
		}
	}

	samples := make([]timedSample, 0, len(frames))
	keyChecked := 0
	for idx, f := range frames {
		isKey := isKeyframe(f.FrameType)
		s := timedSample{
			frameIndex:         idx,
			media:              f.MediaType,
			dts:                f.DTS,
			pts:                f.PTS,
			cts:                f.CTS,
			size:               f.FrameLen,
			key:                isKey,
			b:                  isBFrame(f.FrameType),
			sliceKnown:         isExplicitSliceType(f.FrameType),
			hasContainerParams: hasContainerSeq,
		}
		if f.MediaType == "video" && isKey && inFile != nil && f.StreamSource == "input" && f.StreamLen > 0 && keyChecked < 120 {
			keyChecked++
			readN := f.StreamLen
			if readN > 64*1024 {
				readN = 64 * 1024
			}
			buf := make([]byte, readN)
			if n, err := inFile.ReadAt(buf, f.StreamOffset); err == nil && n > 0 {
				nalLen := f.NALULenSize
				if nalLen < 1 || nalLen > 4 {
					nalLen = 4
				}
				nalus := codec.SplitAVCC(buf[:n], nalLen, isHEVC)
				var hasV, hasS, hasP bool
				for _, nl := range nalus {
					if isHEVC {
						switch nl.Type {
						case 32:
							hasV = true
						case 33:
							hasS = true
						case 34:
							hasP = true
						}
					} else {
						switch nl.Type {
						case 7:
							hasS = true
						case 8:
							hasP = true
						}
					}
				}
				if isHEVC {
					s.hasInbandParams = hasV && hasS && hasP
				} else {
					s.hasInbandParams = hasS && hasP
				}
			}
		}
		if f.MediaType == "audio" && profile != nil {
			s.audioSampleRate = profile.AudioSampleRate
			s.audioChannels = profile.AudioChannels
		}
		samples = append(samples, s)
	}
	return samples
}

func missingMP4Track(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "未找到视频轨") || strings.Contains(msg, "未找到音频轨")
}
