package offline

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"streamanalyzer/internal/models"
	"streamanalyzer/internal/pcaprtmp"
	"streamanalyzer/internal/rtmpraw"

	"github.com/google/gopacket/layers"
)

func (m *Manager) runPCAP(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	opt := pcaprtmp.DefaultOptions(layers.TCPPort(task.ServerPort))
	flows, err := pcaprtmp.ExtractFlowsFromPCAP(task.InputPath, opt)
	if err != nil {
		return err
	}
	if len(flows) == 0 {
		return fmt.Errorf("no candidate RTMP flows found")
	}

	for i, f := range flows {
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
		res, parseErr := rtmpraw.ParseRTMPRaw(bytes.NewReader(raw), rtmpraw.Options{SkipBytes: task.SkipBytes}, rtmpraw.Output{
			VideoWriter: vf,
			AudioWriter: af,
		})
		_ = vf.Close()
		_ = af.Close()
		if parseErr != nil {
			fr.Error = parseErr.Error()
		} else {
			fr.VideoCodec = res.VideoCodec
		}

		// Cleanup: if audio file is empty, delete and hide it.
		cleanupEmptyFile(&fr.AudioPath)
		summary.Flows = append(summary.Flows, fr)
	}
	return nil
}
