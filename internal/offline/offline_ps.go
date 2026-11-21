package offline

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
)

// runPS parses an ISO/IEC 13818-1 MPEG-PS (.ps / .mpg, commonly used by GB28181)
// file, extracts elementary video (H.264/H.265) and audio (AAC / G.711->WAV),
// records exact frame byte offsets for Inspector, and attaches full media profile.
func (m *Manager) runPS(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	ctx := m.taskContext(task.ID)
	m.setProgressPercent(task.ID, "读取 MPEG-PS 码流", 8, 0)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	raw, err := os.ReadFile(task.InputPath)
	if err != nil {
		return err
	}
	if len(raw) < 14 {
		return fmt.Errorf("MPEG-PS 文件过小")
	}

	outDir := filepath.Join(taskDir, "ps")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	flow, err := m.parsePSBytes(ctx, task.ID, raw, task.InputPath, outDir, 1, "ps")
	if err != nil {
		return err
	}
	summary.Flows = append(summary.Flows, *flow)
	return nil
}

func (m *Manager) parsePSBytes(ctx context.Context, taskID string, raw []byte, rawPath, outDir string, flowID int, direction string) (*models.OfflineFlowResult, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	videoPath := filepath.Join(outDir, "video.annexb")
	vf, err := os.Create(videoPath)
	if err != nil {
		return nil, err
	}
	defer vf.Close()

	var (
		videoCodec      string
		audioCodec      string
		videoPos        int64
		audioRawBuf     bytes.Buffer
		audioFile       *os.File
		audioPath       string
		audioPos        int64
		packCount       int
		psmCount        int
		pesCount        int
		naluCount       int
		naluHist        = make(map[string]int)
		videoMeta       *codec.VideoMeta
		audioSampleRate int
		audioChannels   int
		videoDTSClock   = &ptsClock{}
		videoPTSClock   = &ptsClock{}
		audioDTSClock   = &ptsClock{}
		audioPTSClock   = &ptsClock{}
		streamTypeByID  = map[byte]byte{}
		frameDetails    []models.OfflineFrameDetail
		samples         []timedSample
	)
	defer func() {
		if audioFile != nil {
			_ = audioFile.Close()
		}
	}()

	// Accumulate multi-PES video access unit when consecutive PES packets share PTS or lack PTS.
	var (
		pendingVideoBuf  bytes.Buffer
		pendingVideoDTS  int64
		pendingVideoPTS  int64
		hasPendingVideo  bool
		lastVideoParamHash string
	)

	flushVideoAU := func() error {
		if !hasPendingVideo || pendingVideoBuf.Len() == 0 {
			pendingVideoBuf.Reset()
			hasPendingVideo = false
			return nil
		}
		au := pendingVideoBuf.Bytes()
		off := videoPos
		if _, err := vf.Write(au); err != nil {
			return err
		}
		videoPos += int64(len(au))

		nalus := splitAnnexBNALUs(au)
		isHEVC := videoCodec == "h265"
		if videoCodec == "" && len(nalus) > 0 {
			hevcGuess, meta := detectAnnexBCodecAndMeta(nalus, "")
			isHEVC = hevcGuess
			if isHEVC {
				videoCodec = "h265"
			} else {
				videoCodec = "h264"
			}
			if meta != nil && videoMeta == nil {
				videoMeta = meta
			}
		}

		var (
			ft         = "P"
			isKey      = false
			isB        = false
			sliceKnown = false
			hasVPS     = false
			hasSPS     = false
			hasPPS     = false
			paramParts [][]byte
			frameW     int
			frameH     int
		)
		for _, n := range nalus {
			if len(n) == 0 {
				continue
			}
			naluCount++
			if isHEVC {
				t := int(h265NALUType(n))
				naluHist[codec.NALUTypeName(t, true)]++
				switch {
				case t == 32:
					hasVPS = true
					paramParts = append(paramParts, n)
				case t == 33:
					hasSPS = true
					paramParts = append(paramParts, n)
					if meta := codec.VideoMetaFromNAL(n); meta != nil {
						frameW, frameH = meta.Width, meta.Height
						if videoMeta == nil {
							videoMeta = meta
						}
					}
				case t == 34:
					hasPPS = true
					paramParts = append(paramParts, n)
				case isH265KeyNALUType(uint8(t)):
					isKey = true
					ft = "I"
				}
			} else {
				t := int(h264NALUType(n))
				naluHist[codec.NALUTypeName(t, false)]++
				switch t {
				case 7:
					hasSPS = true
					paramParts = append(paramParts, n)
					if meta, err := codec.ParseH264SPS(n); err == nil && meta != nil {
						frameW, frameH = meta.Width, meta.Height
						if videoMeta == nil {
							videoMeta = meta
						}
					}
				case 8:
					hasPPS = true
					paramParts = append(paramParts, n)
				case 5:
					isKey = true
					ft = "I"
					sliceKnown = true
				case 1:
					if st := codec.H264SliceType(n); st != "" {
						sliceKnown = true
						if st == "B" {
							isB = true
							ft = "B"
						} else if st == "I" && !isB {
							ft = "I"
							isKey = true
						}
					}
				}
			}
		}
		hasInbandParams := (isHEVC && hasVPS && hasSPS && hasPPS) || (!isHEVC && hasSPS && hasPPS)
		pHash := ""
		if len(paramParts) > 0 {
			pHash = hashNALSlices(paramParts)
			lastVideoParamHash = pHash
		} else {
			pHash = lastVideoParamHash
		}

		idx := len(frameDetails)
		frameDetails = append(frameDetails, models.OfflineFrameDetail{
			MediaType:    "video",
			DTS:          pendingVideoDTS,
			PTS:          pendingVideoPTS,
			CTS:          pendingVideoPTS - pendingVideoDTS,
			FrameLen:     len(au),
			FrameType:    ft,
			StreamSource: "video",
			StreamOffset: off,
			StreamLen:    len(au),
		})
		samples = append(samples, timedSample{
			frameIndex:      idx,
			media:           "video",
			dts:             pendingVideoDTS,
			pts:             pendingVideoPTS,
			cts:             pendingVideoPTS - pendingVideoDTS,
			size:            len(au),
			key:             isKey,
			b:               isB,
			sliceKnown:      sliceKnown,
			hasInbandParams: hasInbandParams,
			paramHash:       pHash,
			videoWidth:      frameW,
			videoHeight:     frameH,
		})

		pendingVideoBuf.Reset()
		hasPendingVideo = false
		return nil
	}

	pos := 0
	totalLen := len(raw)
	lastPct := 10
	for pos+4 <= totalLen {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Find next 0x000001 start code
		scIdx := bytes.Index(raw[pos:], []byte{0x00, 0x00, 0x01})
		if scIdx < 0 {
			break
		}
		pos += scIdx
		if pos+4 > totalLen {
			break
		}
		code := raw[pos+3]

		if pct := 10 + (pos*80)/totalLen; pct >= lastPct+5 && m != nil && taskID != "" {
			lastPct = pct
			m.setProgressPercent(taskID, "解析 MPEG-PS 包", float64(pct), int64(pos))
		}

		switch {
		case code == 0xBA: // Pack Header
			packCount++
			if pos+14 > totalLen {
				pos = totalLen
				break
			}
			// MPEG-2 PS pack header: byte 4 starts with '01'
			if (raw[pos+4] & 0xC0) == 0x40 {
				stuffing := int(raw[pos+13] & 0x07)
				pos += 14 + stuffing
			} else {
				pos += 12
			}

		case code == 0xBB: // System Header
			if pos+6 > totalLen {
				pos = totalLen
				break
			}
			hdrLen := int(binary.BigEndian.Uint16(raw[pos+4 : pos+6]))
			pos += 6 + hdrLen

		case code == 0xBC: // Program Stream Map (PSM)
			psmCount++
			if pos+6 > totalLen {
				pos = totalLen
				break
			}
			psmLen := int(binary.BigEndian.Uint16(raw[pos+4 : pos+6]))
			end := pos + 6 + psmLen
			if end > totalLen {
				end = totalLen
			}
			parsePSMMap(raw[pos+6:end], streamTypeByID)
			for _, st := range streamTypeByID {
				switch st {
				case 0x1B:
					if videoCodec == "" {
						videoCodec = "h264"
					}
				case 0x24:
					if videoCodec == "" {
						videoCodec = "h265"
					}
				case 0x0F:
					if audioCodec == "" {
						audioCodec = "aac"
					}
				case 0x90:
					if audioCodec == "" {
						audioCodec = "pcma"
					}
				case 0x91:
					if audioCodec == "" {
						audioCodec = "pcmu"
					}
				}
			}
			pos = end

		case code == 0xB9: // MPEG Program End Code
			pos += 4

		case (code >= 0xE0 && code <= 0xEF) || (code >= 0xC0 && code <= 0xDF): // Video or Audio PES
			if pos+6 > totalLen {
				pos = totalLen
				break
			}
			pesLen := int(binary.BigEndian.Uint16(raw[pos+4 : pos+6]))
			end := pos + 6 + pesLen
			if pesLen == 0 || end > totalLen {
				// Unbounded video PES or truncated tail: scan to next PS start code
				nextSC := findNextPSHeader(raw, pos+6)
				if nextSC > pos+6 {
					end = nextSC
				} else {
					end = totalLen
				}
			}
			if pos+9 > end {
				pos = end
				break
			}
			pesCount++
			ptsDtsFlags := (raw[pos+7] >> 6) & 0x03
			hdrDataLen := int(raw[pos+8])
			payloadStart := pos + 9 + hdrDataLen
			if payloadStart > end {
				pos = end
				break
			}
			var ptsTick, dtsTick int64
			var hasPTS, hasDTS bool
			if (ptsDtsFlags == 2 || ptsDtsFlags == 3) && pos+14 <= payloadStart {
				ptsTick = decodePS90kTimestamp(raw[pos+9 : pos+14])
				hasPTS = true
				dtsTick = ptsTick
			}
			if ptsDtsFlags == 3 && pos+19 <= payloadStart {
				dtsTick = decodePS90kTimestamp(raw[pos+14 : pos+19])
				hasDTS = true
			}
			payload := raw[payloadStart:end]
			pos = end

			if len(payload) == 0 {
				continue
			}

			if code >= 0xE0 && code <= 0xEF {
				if st, ok := streamTypeByID[code]; ok {
					if st == 0x24 && videoCodec == "" {
						videoCodec = "h265"
					} else if st == 0x1B && videoCodec == "" {
						videoCodec = "h264"
					}
				}
				dtsMs := pendingVideoDTS
				ptsMs := pendingVideoPTS
				if hasPTS || hasDTS {
					dtsMs = ptsToMs(videoDTSClock.push(dtsTick))
					ptsMs = ptsToMs(videoPTSClock.push(ptsTick))
					if hasPendingVideo && (dtsMs != pendingVideoDTS || ptsMs != pendingVideoPTS) {
						if err := flushVideoAU(); err != nil {
							return nil, err
						}
					}
					pendingVideoDTS = dtsMs
					pendingVideoPTS = ptsMs
				}
				pendingVideoBuf.Write(payload)
				hasPendingVideo = true
			} else {
				// Audio PES
				if st, ok := streamTypeByID[code]; ok {
					switch st {
					case 0x0F:
						audioCodec = "aac"
					case 0x90:
						audioCodec = "pcma"
					case 0x91:
						audioCodec = "pcmu"
					}
				}
				if audioCodec == "" {
					if len(payload) >= 7 && payload[0] == 0xFF && (payload[1]&0xF0) == 0xF0 {
						audioCodec = "aac"
					} else {
						audioCodec = "pcma"
					}
				}
				dtsMs := int64(0)
				ptsMs := int64(0)
				if hasPTS || hasDTS {
					dtsMs = ptsToMs(audioDTSClock.push(dtsTick))
					ptsMs = ptsToMs(audioPTSClock.push(ptsTick))
				}

				var off int64
				if audioCodec == "pcma" || audioCodec == "pcmu" {
					audioSampleRate = 8000
					audioChannels = 1
					off = int64(audioRawBuf.Len())
					audioRawBuf.Write(payload)
				} else {
					if audioFile == nil {
						audioPath = filepath.Join(outDir, "audio.adts.aac")
						audioFile, err = os.Create(audioPath)
						if err != nil {
							return nil, err
						}
					}
					off = audioPos
					if _, err := audioFile.Write(payload); err != nil {
						return nil, err
					}
					audioPos += int64(len(payload))
					if rate, ch, ok := adtsLayout(payload); ok {
						audioSampleRate, audioChannels = rate, ch
					}
				}

				idx := len(frameDetails)
				frameDetails = append(frameDetails, models.OfflineFrameDetail{
					MediaType:    "audio",
					DTS:          dtsMs,
					PTS:          ptsMs,
					CTS:          0,
					FrameLen:     len(payload),
					FrameType:    "-",
					StreamSource: "audio",
					StreamOffset: off,
					StreamLen:    len(payload),
				})
				samples = append(samples, timedSample{
					frameIndex:      idx,
					media:           "audio",
					dts:             dtsMs,
					pts:             ptsMs,
					size:            len(payload),
					audioSampleRate: audioSampleRate,
					audioChannels:   audioChannels,
				})
			}

		default:
			pos += 4
		}
	}

	if err := flushVideoAU(); err != nil {
		return nil, err
	}
	_ = vf.Close()
	if audioFile != nil {
		_ = audioFile.Close()
		audioFile = nil
	}

	var artifacts []string
	if audioRawBuf.Len() > 0 && (audioCodec == "pcma" || audioCodec == "pcmu") {
		rawAudPath := filepath.Join(outDir, "audio."+audioCodec)
		_ = os.WriteFile(rawAudPath, audioRawBuf.Bytes(), 0o644)
		audioPath = rawAudPath

		wavPath := filepath.Join(outDir, "audio.wav")
		if wf, err := os.Create(wavPath); err == nil {
			if err := codec.WriteG711WAV(wf, audioRawBuf.Bytes(), audioCodec); err == nil {
				artifacts = append(artifacts, wavPath)
			}
			_ = wf.Close()
		}
	}

	flow := &models.OfflineFlowResult{
		FlowID:        flowID,
		Protocol:      "ps",
		Direction:     direction,
		DumpRawDir:    "single",
		RawPath:       rawPath,
		VideoPath:     videoPath,
		AudioPath:     audioPath,
		ArtifactPaths: artifacts,
		PayloadBytes:  len(raw),
		PATCount:      packCount,
		PMTCount:      psmCount,
		PESCount:      pesCount,
		NALUCount:     naluCount,
		VideoCodec:    videoCodec,
		AudioCodec:    audioCodec,
	}
	cleanupEmptyFile(&flow.VideoPath)
	cleanupEmptyFile(&flow.AudioPath)

	seed := models.OfflineMediaProfile{
		VideoCodec:      videoCodec,
		AudioCodec:      audioCodec,
		AudioSampleRate: audioSampleRate,
		AudioChannels:   audioChannels,
		NALUHistogram:   naluHist,
	}
	if videoMeta != nil {
		seed.Width = videoMeta.Width
		seed.Height = videoMeta.Height
		seed.Profile = videoMeta.ProfileName
		seed.Level = videoMeta.Level
		if videoCodec == "h265" {
			seed.LevelName = codec.H265LevelName(videoMeta.Level)
		} else {
			seed.LevelName = codec.H264LevelName(videoMeta.Level)
		}
		seed.FrameRate = videoMeta.FrameRate
	}

	attachMediaProfileWithSamples(flow, frameDetails, samples, seed, healthContext{
		Mode:                  models.OfflineModePS,
		IsHEVC:                videoCodec == "h265",
		HasContainerSeqHeader: false,
		CheckIDRParams:        videoCodec == "h264" || videoCodec == "h265",
	})
	return flow, nil
}

func parsePSMMap(body []byte, out map[byte]byte) {
	if len(body) < 10 {
		return
	}
	infoLen := int(binary.BigEndian.Uint16(body[2:4]))
	off := 4 + infoLen
	if off+2 > len(body) {
		return
	}
	esMapLen := int(binary.BigEndian.Uint16(body[off : off+2]))
	off += 2
	end := off + esMapLen
	if end > len(body)-4 {
		end = len(body) - 4
	}
	for off+4 <= end {
		st := body[off]
		esID := body[off+1]
		esInfoLen := int(binary.BigEndian.Uint16(body[off+2 : off+4]))
		out[esID] = st
		off += 4 + esInfoLen
	}
}

func decodePS90kTimestamp(b []byte) int64 {
	p1 := int64((b[0] >> 1) & 0x07)
	p2 := int64(b[1])
	p3 := int64((b[2] >> 1) & 0x7F)
	p4 := int64(b[3])
	p5 := int64((b[4] >> 1) & 0x7F)
	return (p1 << 30) | (p2 << 22) | (p3 << 15) | (p4 << 7) | p5
}

func findNextPSHeader(raw []byte, start int) int {
	pos := start
	for pos+4 <= len(raw) {
		i := bytes.Index(raw[pos:], []byte{0x00, 0x00, 0x01})
		if i < 0 {
			return -1
		}
		pos += i
		if pos+4 > len(raw) {
			return -1
		}
		c := raw[pos+3]
		if c == 0xBA || c == 0xBB || c == 0xBC || c == 0xB9 ||
			(c >= 0xC0 && c <= 0xDF) || (c >= 0xE0 && c <= 0xEF) {
			return pos
		}
		pos += 3
	}
	return -1
}
