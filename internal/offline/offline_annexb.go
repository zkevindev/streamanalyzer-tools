package offline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
)

type annexBNALUSpan struct {
	start int
	end   int
	nalu  []byte
}

func splitAnnexBNALUSpans(b []byte) []annexBNALUSpan {
	starts := findStartCodes(b)
	if len(starts) == 0 {
		return nil
	}
	out := make([]annexBNALUSpan, 0, len(starts))
	for i, start := range starts {
		next := len(b)
		if i+1 < len(starts) {
			next = starts[i+1]
		}
		scLen := startCodeLenAt(b, start)
		naluStart := start + scLen
		if naluStart >= next {
			continue
		}
		out = append(out, annexBNALUSpan{
			start: start,
			end:   next,
			nalu:  b[naluStart:next],
		})
	}
	return out
}

func (m *Manager) runAnnexB(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	ctx := m.taskContext(task.ID)
	outDir := filepath.Join(taskDir, "annexb")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	m.setProgressPercent(task.ID, "读取 Annex B 裸流", 10, 0)
	raw, err := os.ReadFile(task.InputPath)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	spans := splitAnnexBNALUSpans(raw)
	if len(spans) == 0 {
		return fmt.Errorf("未检测到有效的 Annex B 起始码 (0x000001 / 0x00000001)")
	}
	nalus := make([][]byte, len(spans))
	for i, sp := range spans {
		nalus[i] = sp.nalu
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
		startOff        int
		endOff          int
		hasSpan         bool
		frameType       string
		hasVCL          bool
		isKey           bool
		hasVPS          bool
		hasSPS          bool
		hasPPS          bool
		hasInbandParams bool
		paramHash       string
		width           int
		height          int
	}
	var units []accessUnit
	var cur accessUnit

	var curVPS, curSPS, curPPS [][]byte
	var activeParamHash string
	var activeW, activeH int
	if videoMeta != nil {
		activeW, activeH = videoMeta.Width, videoMeta.Height
	}

	addSpan := func(sp annexBNALUSpan) {
		if !cur.hasSpan {
			cur.startOff = sp.start
			cur.hasSpan = true
		}
		cur.endOff = sp.end
	}

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
		if isHEVC {
			cur.hasInbandParams = cur.hasVPS && cur.hasSPS && cur.hasPPS
		} else {
			cur.hasInbandParams = cur.hasSPS && cur.hasPPS
		}
		cur.paramHash = activeParamHash
		cur.width = activeW
		cur.height = activeH
		units = append(units, cur)
		cur = accessUnit{}
	}

	totalLen := int64(len(raw))
	for i, sp := range spans {
		if i%256 == 0 {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			m.updateProgress(task.ID, "切分与聚合 Annex B NALU", int64(sp.end), totalLen)
		}
		nalu := sp.nalu
		if len(nalu) == 0 {
			continue
		}
		if isHEVC {
			typ := int(h265NALUType(nalu))
			name := codec.NALUTypeName(typ, true)
			histogram[name]++
			switch {
			case typ == 35 || typ == 32 || typ == 33 || typ == 34:
				if cur.hasVCL {
					flush()
				}
				addSpan(sp)
				switch typ {
				case 32:
					cur.hasVPS = true
					curVPS = [][]byte{nalu}
				case 33:
					cur.hasSPS = true
					curSPS = [][]byte{nalu}
					if meta := codec.VideoMetaFromNAL(nalu); meta != nil && meta.Width > 0 {
						activeW, activeH = meta.Width, meta.Height
					}
				case 34:
					cur.hasPPS = true
					curPPS = [][]byte{nalu}
				}
				if len(curSPS) > 0 || len(curPPS) > 0 || len(curVPS) > 0 {
					activeParamHash = hashNALSlices(curVPS, curSPS, curPPS)
				}
			case typ <= 31:
				firstSlice := len(nalu) >= 3 && (nalu[2]&0x80) != 0
				if cur.hasVCL && firstSlice {
					flush()
				}
				cur.hasVCL = true
				addSpan(sp)
				if isH265KeyNALUType(uint8(typ)) {
					cur.isKey = true
					cur.frameType = "I"
				} else if cur.frameType == "" {
					cur.frameType = "P"
				}
			default:
				addSpan(sp)
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
			addSpan(sp)
			if typ == 7 {
				cur.hasSPS = true
				curSPS = [][]byte{nalu}
				if meta, err := codec.ParseH264SPS(nalu); err == nil && meta.Width > 0 {
					activeW, activeH = meta.Width, meta.Height
				}
			} else if typ == 8 {
				cur.hasPPS = true
				curPPS = [][]byte{nalu}
			}
			if len(curSPS) > 0 || len(curPPS) > 0 {
				activeParamHash = hashNALSlices(curSPS, curPPS)
			}
		case 1, 5:
			// New picture starts when first_mb_in_slice == 0 (top bit of RBSP byte 0 is 1 in Exp-Golomb for 0).
			firstMBZero := len(nalu) >= 2 && (nalu[1]&0x80) != 0
			if cur.hasVCL && firstMBZero {
				flush()
			}
			cur.hasVCL = true
			addSpan(sp)
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
			addSpan(sp)
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
	samples := make([]timedSample, 0, len(units))
	for i, u := range units {
		dts := int64(float64(i) * 1000.0 / fps)
		auLen := u.endOff - u.startOff
		if auLen < 0 {
			auLen = 0
		}
		frames = append(frames, models.OfflineFrameDetail{
			MediaType:    "video",
			DTS:          dts,
			PTS:          dts,
			FrameLen:     auLen,
			FrameType:    u.frameType,
			StreamSource: "video",
			StreamOffset: int64(u.startOff),
			StreamLen:    auLen,
		})
		samples = append(samples, timedSample{
			frameIndex:      i,
			media:           "video",
			dts:             dts,
			pts:             dts,
			size:            auLen,
			key:             u.isKey,
			b:               isBFrame(u.frameType),
			sliceKnown:      isExplicitSliceType(u.frameType),
			hasInbandParams: u.hasInbandParams,
			paramHash:       u.paramHash,
			videoWidth:      u.width,
			videoHeight:     u.height,
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
	hctx := healthContext{
		Mode:                  models.OfflineModeAnnexB,
		IsHEVC:                isHEVC,
		HasContainerSeqHeader: false,
		SyntheticTimestamps:   true,
		CheckIDRParams:        true,
	}
	attachMediaProfileWithSamples(&flow, frames, samples, seed, hctx)
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
