package offline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
)

func (m *Manager) runAnnexB(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	outDir := filepath.Join(taskDir, "annexb")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	raw, err := os.ReadFile(task.InputPath)
	if err != nil {
		return err
	}
	nalus := splitAnnexBNALUs(raw)
	if len(nalus) == 0 {
		return fmt.Errorf("未检测到有效的 Annex B 起始码 (0x000001 / 0x00000001)")
	}

	isHEVC, videoMeta := detectAnnexBCodecAndMeta(nalus, task.InputName)
	videoCodec := "h264"
	if isHEVC {
		videoCodec = "h265"
	}

	// Copy normalized copy into output dir so download link works uniformly.
	videoPath := filepath.Join(outDir, "video.annexb")
	if err := os.WriteFile(videoPath, raw, 0o644); err != nil {
		return err
	}

	histogram := make(map[string]int)
	type accessUnit struct {
		bytes     int
		frameType string
		hasVCL    bool
		isKey     bool
	}
	var units []accessUnit
	var cur accessUnit

	flush := func() {
		if !cur.hasVCL {
			return
		}
		if cur.frameType == "" {
			if cur.isKey {
				cur.frameType = "I"
			} else {
				cur.frameType = "P"
			}
		}
		units = append(units, cur)
		cur = accessUnit{}
	}

	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		nalBytes := len(nalu) + 4
		if isHEVC {
			typ := int(h265NALUType(nalu))
			name := codec.NALUTypeName(typ, true)
			histogram[name]++
			switch {
			case typ == 35 || typ == 32 || typ == 33 || typ == 34:
				if cur.hasVCL {
					flush()
				}
				cur.bytes += nalBytes
			case typ <= 31:
				firstSlice := len(nalu) >= 3 && (nalu[2]&0x80) != 0
				if cur.hasVCL && firstSlice {
					flush()
				}
				cur.hasVCL = true
				cur.bytes += nalBytes
				if isH265KeyNALUType(uint8(typ)) {
					cur.isKey = true
					cur.frameType = "I"
				} else if cur.frameType == "" {
					cur.frameType = "P"
				}
			default:
				cur.bytes += nalBytes
			}
			continue
		}

		typ := int(h264NALUType(nalu))
		name := codec.NALUTypeName(typ, false)
		histogram[name]++
		switch typ {
		case 9, 7, 8:
			if cur.hasVCL {
				flush()
			}
			cur.bytes += nalBytes
		case 1, 5:
			// New picture starts when first_mb_in_slice == 0 (top bit of RBSP byte 0 is 1 in Exp-Golomb for 0).
			firstMBZero := len(nalu) >= 2 && (nalu[1]&0x80) != 0
			if cur.hasVCL && firstMBZero {
				flush()
			}
			cur.hasVCL = true
			cur.bytes += nalBytes
			if typ == 5 {
				cur.isKey = true
				cur.frameType = "I"
			} else {
				st := codec.H264SliceType(nalu)
				if st == "B" {
					cur.frameType = "B"
				} else if st == "P" && cur.frameType != "B" {
					cur.frameType = "P"
				} else if st == "I" && cur.frameType == "" {
					cur.frameType = "I"
				}
			}
		default:
			cur.bytes += nalBytes
		}
	}
	flush()

	fps := 25.0
	estimatedFPS := true
	if videoMeta != nil && videoMeta.FrameRate > 0 {
		fps = videoMeta.FrameRate
		estimatedFPS = false
	}

	frames := make([]models.OfflineFrameDetail, 0, len(units))
	for i, u := range units {
		dts := int64(float64(i) * 1000.0 / fps)
		frames = append(frames, models.OfflineFrameDetail{
			MediaType: "video",
			DTS:       dts,
			PTS:       dts,
			FrameLen:  u.bytes,
			FrameType: u.frameType,
		})
	}

	flow := models.OfflineFlowResult{
		FlowID:       1,
		Direction:    "annexb",
		DumpRawDir:   "single",
		RawPath:      task.InputPath,
		VideoPath:    videoPath,
		VideoCodec:   videoCodec,
		PayloadBytes: len(raw),
		NALUCount:    len(nalus),
	}

	seed := profileFromVideoMeta(videoMeta, isHEVC)
	seed.VideoCodec = videoCodec
	seed.NALUHistogram = histogram
	if estimatedFPS {
		seed.Warnings = append(seed.Warnings, "Annex B 裸码流不含容器时间戳且 SPS 无 VUI 帧率，时长与码率按 25 fps 估算")
	} else {
		seed.Warnings = append(seed.Warnings, fmt.Sprintf("Annex B 裸码流不含容器时间戳，时间线按 SPS 帧率 %.2f fps 推算", fps))
	}
	attachMediaProfile(&flow, frames, seed)
	summary.Flows = append(summary.Flows, flow)
	return nil
}

func detectAnnexBCodecAndMeta(nalus [][]byte, filename string) (bool, *codec.VideoMeta) {
	ext := strings.ToLower(filepath.Ext(filename))
	hintHEVC := ext == ".h265" || ext == ".265" || ext == ".hevc"
	hintH264 := ext == ".h264" || ext == ".264"

	var h264Meta, h265Meta *codec.VideoMeta
	var h264Score, h265Score int

	for _, nal := range nalus {
		if len(nal) < 2 {
			continue
		}
		t264 := nal[0] & 0x1f
		if t264 == 7 && h264Meta == nil {
			if m, err := codec.ParseH264SPS(nal); err == nil && m.Width > 0 && m.Height > 0 {
				h264Meta = m
				h264Score += 10
			}
		}
		if t264 == 5 || t264 == 8 {
			h264Score++
		}
		if (nal[0] & 0x80) == 0 {
			t265 := (nal[0] >> 1) & 0x3f
			if t265 == 32 {
				h265Score += 5
			}
			if t265 == 33 && h265Meta == nil {
				if m := codec.VideoMetaFromNAL(nal); m != nil && m.Width > 0 && m.Height > 0 {
					// Make sure it actually matched H265 rather than H264
					if (nal[0] & 0x1f) != 7 {
						h265Meta = m
						h265Score += 10
					}
				}
			}
			if t265 == 34 || (t265 >= 16 && t265 <= 21) {
				h265Score++
			}
		}
	}

	if hintHEVC && h265Meta != nil {
		return true, h265Meta
	}
	if hintH264 && h264Meta != nil {
		return false, h264Meta
	}
	if h265Meta != nil && h264Meta == nil {
		return true, h265Meta
	}
	if h264Meta != nil && h265Meta == nil {
		return false, h264Meta
	}
	if h265Score > h264Score {
		return true, h265Meta
	}
	return false, h264Meta
}
