package offline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"streamanalyzer/internal/models"
	"streamanalyzer/internal/pcaprtmp"
	"streamanalyzer/internal/rtmpraw"

	"github.com/google/gopacket/layers"
)

func (m *Manager) runPCAP(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	ctx := m.taskContext(task.ID)
	m.setProgressPercent(task.ID, "重组 PCAP TCP 流", 10, 0)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	opt := pcaprtmp.DefaultOptions(layers.TCPPort(task.ServerPort))
	flows, err := pcaprtmp.ExtractFlowsFromPCAP(task.InputPath, opt)
	if err != nil {
		return err
	}
	if len(flows) == 0 {
		return fmt.Errorf("no candidate RTMP flows found")
	}

	for i, f := range flows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		pct := 25.0 + float64(i)*70.0/float64(len(flows))
		m.setProgressPercent(task.ID, fmt.Sprintf("解析 RTMP 流 %d/%d", i+1, len(flows)), pct, task.InputSize*int64(i+1)/int64(len(flows)))

		flowID := i + 1
		dir := pcaprtmp.DetectDirection(f, task.SkipBytes, opt.DetectLimitBytes)
		raw := []byte(nil)
		rawDir := ""
		switch dir {
		case pcaprtmp.DirectionPull:
			raw = f.ServerToClient
			rawDir = "server->client"
		case pcaprtmp.DirectionPush:
			raw = f.ClientToServer
			rawDir = "client->server"
		default:
			// keep summary entry even if unknown
		}

		fr := models.OfflineFlowResult{
			FlowID:       flowID,
			HasSYN:       f.Meta.HasSYN,
			SYNCount:     f.Meta.SYNCount,
			TCPPktCount:  f.Meta.TCPPktCount,
			PayloadBytes: f.Meta.PayloadBytes,
			ClientIP:     f.Meta.ClientIP,
			ClientPort:   uint16(f.Meta.ClientPort),
			ServerIP:     f.Meta.ServerIP,
			ServerPort:   uint16(f.Meta.ServerPort),
			Direction:    string(dir),
			DumpRawDir:   rawDir,
		}

		if dir == pcaprtmp.DirectionUnknown || len(raw) == 0 {
			fr.Error = "no media direction detected or empty raw stream"
			summary.Flows = append(summary.Flows, fr)
			continue
		}

		dirOut := filepath.Join(taskDir, string(dir))
		if err := os.MkdirAll(dirOut, 0o755); err != nil {
			return err
		}

		fr.RawPath = filepath.Join(dirOut, fmt.Sprintf("flow%03d_%s.raw", flowID, rawDir))
		if err := os.WriteFile(fr.RawPath, raw, 0o644); err != nil {
			return err
		}

		fr.VideoPath = filepath.Join(dirOut, fmt.Sprintf("flow%03d_video.annexb", flowID))
		fr.AudioPath = filepath.Join(dirOut, fmt.Sprintf("flow%03d_audio.adts.aac", flowID))

		vf, err := os.Create(fr.VideoPath)
		if err != nil {
			return err
		}
		af, err := os.Create(fr.AudioPath)
		if err != nil {
			_ = vf.Close()
			return err
		}
		frameDetails := make([]models.OfflineFrameDetail, 0, 2048)
		samples := make([]timedSample, 0, 2048)
		hasContainerSeq := false
		res, parseErr := rtmpraw.ParseRTMPRaw(bytes.NewReader(raw), rtmpraw.Options{SkipBytes: task.SkipBytes, Context: ctx}, rtmpraw.Output{
			VideoWriter: vf,
			AudioWriter: af,
			OnFrame: func(fi rtmpraw.FrameInfo) {
				idx := len(frameDetails)
				if fi.HasContainerParams {
					hasContainerSeq = true
				}
				frameDetails = append(frameDetails, models.OfflineFrameDetail{
					MediaType:    fi.MediaType,
					DTS:          fi.DTS,
					PTS:          fi.PTS,
					CTS:          fi.CTS,
					FrameLen:     fi.FrameLen,
					FrameType:    fi.FrameType,
					StreamSource: fi.StreamSource,
					StreamOffset: fi.StreamOffset,
					StreamLen:    fi.StreamLen,
				})
				samples = append(samples, timedSample{
					frameIndex:         idx,
					media:              fi.MediaType,
					dts:                fi.DTS,
					pts:                fi.PTS,
					cts:                fi.CTS,
					size:               fi.FrameLen,
					key:                isKeyframe(fi.FrameType),
					b:                  isBFrame(fi.FrameType),
					sliceKnown:         isExplicitSliceType(fi.FrameType),
					hasContainerParams: fi.HasContainerParams,
					hasInbandParams:    fi.HasInbandParams,
					paramHash:          fi.ParamHash,
					videoWidth:         fi.VideoWidth,
					videoHeight:        fi.VideoHeight,
					audioSampleRate:    fi.AudioSampleRate,
					audioChannels:      fi.AudioChannels,
				})
			},
		})
		_ = vf.Close()
		_ = af.Close()
		if errors.Is(parseErr, context.Canceled) || ctx.Err() != nil {
			return context.Canceled
		}
		fr.VideoCodec = res.VideoCodec
		fr.AudioCodec = res.AudioCodec
		if parseErr != nil {
			fr.Error = parseErr.Error()
		}

		cleanupEmptyFile(&fr.AudioPath)
		retargetAudioFile(&fr.AudioPath, res.AudioCodec)
		hctx := healthContext{
			Mode:                  models.OfflineModePCAP,
			IsHEVC:                res.HEVC,
			HasContainerSeqHeader: hasContainerSeq,
			CheckIDRParams:        res.VideoCodec == "h264" || res.VideoCodec == "h265",
		}
		attachMediaProfileWithSamples(&fr, frameDetails, samples, profileFromRTMP(res), hctx)
		summary.Flows = append(summary.Flows, fr)
	}
	return nil
}
