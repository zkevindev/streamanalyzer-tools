package offline

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
)

// Matroska / WebM EBML Element IDs
const (
	ebmlIDHeader        = 0x1A45DFA3
	ebmlIDDocType       = 0x4282
	ebmlIDSegment       = 0x18538067
	ebmlIDInfo          = 0x1549A966
	ebmlIDTimecodeScale = 0x2AD7B1
	ebmlIDDuration      = 0x4489
	ebmlIDTracks        = 0x1654AE6B
	ebmlIDTrackEntry    = 0xAE
	ebmlIDTrackNumber   = 0xD7
	ebmlIDTrackType     = 0x83
	ebmlIDCodecID       = 0x86
	ebmlIDCodecPrivate  = 0x63A2
	ebmlIDVideo         = 0xE0
	ebmlIDPixelWidth    = 0xB0
	ebmlIDPixelHeight   = 0xBA
	ebmlIDAudio         = 0xE1
	ebmlIDSampleFreq    = 0xB5
	ebmlIDChannels      = 0x9F
	ebmlIDCluster       = 0x1F43B675
	ebmlIDClusterTime   = 0xE7
	ebmlIDSimpleBlock   = 0xA3
	ebmlIDBlockGroup    = 0xA0
	ebmlIDBlock         = 0xA1
	ebmlIDRefBlock      = 0xFB
)

type webmTrack struct {
	Number       uint64
	TrackType    uint64 // 1=video, 2=audio
	CodecID      string
	CodecShort   string
	CodecPrivate []byte
	Width        int
	Height       int
	SampleRate   int
	Channels     int
	NALULenSize  int
	ParamAnnexB  []byte
}

// runWebM parses WebM / Matroska (.webm / .mkv) files using a pure-Go EBML reader,
// extracting video/audio frame timelines, AV1/H.264/H.265 codec metadata, Annex B
// video export (when H.264/H.265), exact byte offsets for Inspector, and full health checks.
func (m *Manager) runWebM(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	ctx := m.taskContext(task.ID)
	m.setProgressPercent(task.ID, "读取 WebM / Matroska 结构", 8, 0)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	raw, err := os.ReadFile(task.InputPath)
	if err != nil {
		return err
	}
	if len(raw) < 12 || raw[0] != 0x1A || raw[1] != 0x45 || raw[2] != 0xDF || raw[3] != 0xA3 {
		return fmt.Errorf("未检测到有效的 EBML / WebM / Matroska 文件头")
	}

	outDir := filepath.Join(taskDir, "webm")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	var (
		docType       = "webm"
		timecodeScale = uint64(1000000) // default 1ms in ns
		tracks        = map[uint64]*webmTrack{}
		videoTrack    *webmTrack
		audioTrack    *webmTrack
		videoCodec    string
		audioCodec    string
		videoMeta     *codec.VideoMeta
		av1Meta       *codec.AV1Meta
		videoOut      *os.File
		videoPath     string
		videoPos      int64
		frameDetails  []models.OfflineFrameDetail
		samples       []timedSample
		naluHist      = map[string]int{}
		naluCount     int
	)
	defer func() {
		if videoOut != nil {
			_ = videoOut.Close()
		}
	}()

	ensureVideoOut := func() error {
		if videoOut != nil {
			return nil
		}
		videoPath = filepath.Join(outDir, "video.annexb")
		f, err := os.Create(videoPath)
		if err != nil {
			return err
		}
		videoOut = f
		return nil
	}

	pos := 0
	total := len(raw)
	lastPct := 10
	for pos < total {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		id, idLen, ok := readEBMLID(raw[pos:])
		if !ok {
			break
		}
		pos += idLen
		sz, szLen, unknownSz, ok := readEBMLSize(raw[pos:])
		if !ok {
			break
		}
		pos += szLen
		elemEnd := total
		if !unknownSz && pos+int(sz) <= total {
			elemEnd = pos + int(sz)
		}

		switch id {
		case ebmlIDHeader:
			parseEBMLHeader(raw[pos:elemEnd], &docType)
			pos = elemEnd

		case ebmlIDSegment:
			// Walk children of Segment
			segPos := pos
			for segPos < elemEnd {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if pct := 10 + (segPos*80)/total; pct >= lastPct+5 {
					lastPct = pct
					m.setProgressPercent(task.ID, "解析 WebM / MKV Cluster", float64(pct), int64(segPos))
				}
				cid, cidLen, ok := readEBMLID(raw[segPos:])
				if !ok {
					break
				}
				segPos += cidLen
				csz, cszLen, cUnknown, ok := readEBMLSize(raw[segPos:])
				if !ok {
					break
				}
				segPos += cszLen
				cEnd := elemEnd
				if !cUnknown && segPos+int(csz) <= elemEnd {
					cEnd = segPos + int(csz)
				}

				switch cid {
				case ebmlIDInfo:
					parseSegmentInfo(raw[segPos:cEnd], &timecodeScale)
					segPos = cEnd

				case ebmlIDTracks:
					parseTracks(raw[segPos:cEnd], tracks)
					for _, tr := range tracks {
						if tr.TrackType == 1 && videoTrack == nil {
							videoTrack = tr
							videoCodec = tr.CodecShort
							if len(tr.CodecPrivate) > 0 {
								switch tr.CodecShort {
								case "h264":
									if rec, err := codec.ParseAVCDecoderConfigurationRecord(tr.CodecPrivate); err == nil {
										tr.NALULenSize = int(rec.LengthSizeMinusOne) + 1
										tr.ParamAnnexB = buildStartCodedBytes(rec.SequenceParameterSets, rec.PictureParameterSets)
									}
									if meta, err := codec.ParseAVCDecoderConfig(tr.CodecPrivate); err == nil {
										videoMeta = meta
									}
								case "h265":
									if rec, err := codec.ParseHEVCDecoderConfigurationRecord(tr.CodecPrivate); err == nil {
										tr.NALULenSize = int(rec.LengthSizeMinusOne) + 1
										var vps, sps, pps [][]byte
										for _, arr := range rec.Arrays {
											switch arr.NALUnitType {
											case 32:
												vps = append(vps, arr.NALUnits...)
											case 33:
												sps = append(sps, arr.NALUnits...)
											case 34:
												pps = append(pps, arr.NALUnits...)
											}
										}
										tr.ParamAnnexB = buildStartCodedBytes(vps, sps, pps)
									}
									if meta, err := codec.ParseHEVCDecoderConfig(tr.CodecPrivate); err == nil {
										videoMeta = meta
									}
								case "av1":
									if am, err := codec.ParseAV1SequenceHeader(tr.CodecPrivate); err == nil {
										av1Meta = am
									}
								}
							}
						} else if tr.TrackType == 2 && audioTrack == nil {
							audioTrack = tr
							audioCodec = tr.CodecShort
						}
					}
					segPos = cEnd

				case ebmlIDCluster:
					clusterEnd := cEnd
					if cUnknown {
						clusterEnd = findNextClusterOrEnd(raw, segPos, elemEnd)
					}
					var clusterTime uint64
					clPos := segPos
					for clPos < clusterEnd {
						bid, bidLen, ok := readEBMLID(raw[clPos:])
						if !ok {
							break
						}
						clPos += bidLen
						bsz, bszLen, bUnknown, ok := readEBMLSize(raw[clPos:])
						if !ok {
							break
						}
						clPos += bszLen
						bEnd := clusterEnd
						if !bUnknown && clPos+int(bsz) <= clusterEnd {
							bEnd = clPos + int(bsz)
						}

						switch bid {
						case ebmlIDClusterTime:
							clusterTime = readEBMLUint(raw[clPos:bEnd])
						case ebmlIDSimpleBlock:
							handleWebMBlock(raw[clPos:bEnd], int64(clPos), clusterTime, timecodeScale, true, false,
								tracks, ensureVideoOut, &videoOut, &videoPos, &av1Meta, &frameDetails, &samples, naluHist, &naluCount)
						case ebmlIDBlockGroup:
							var blkSlice []byte
							var blkFileOff int64
							hasRef := false
							bgPos := clPos
							for bgPos < bEnd {
								gid, gidLen, ok := readEBMLID(raw[bgPos:])
								if !ok {
									break
								}
								bgPos += gidLen
								gsz, gszLen, gUnknown, ok := readEBMLSize(raw[bgPos:])
								if !ok {
									break
								}
								bgPos += gszLen
								gEnd := bEnd
								if !gUnknown && bgPos+int(gsz) <= bEnd {
									gEnd = bgPos + int(gsz)
								}
								if gid == ebmlIDBlock {
									blkSlice = raw[bgPos:gEnd]
									blkFileOff = int64(bgPos)
								} else if gid == ebmlIDRefBlock {
									hasRef = true
								}
								bgPos = gEnd
							}
							if len(blkSlice) > 0 {
								handleWebMBlock(blkSlice, blkFileOff, clusterTime, timecodeScale, false, !hasRef,
									tracks, ensureVideoOut, &videoOut, &videoPos, &av1Meta, &frameDetails, &samples, naluHist, &naluCount)
							}
						}
						clPos = bEnd
					}
					segPos = clusterEnd

				default:
					if cUnknown {
						segPos = elemEnd
					} else {
						segPos = cEnd
					}
				}
			}
			pos = elemEnd

		default:
			if unknownSz {
				pos = total
			} else {
				pos = elemEnd
			}
		}
	}

	if videoOut != nil {
		_ = videoOut.Close()
		videoOut = nil
	}

	flow := models.OfflineFlowResult{
		FlowID:       1,
		Protocol:     docType,
		Direction:    docType,
		DumpRawDir:   "single",
		RawPath:      task.InputPath,
		VideoPath:    videoPath,
		PayloadBytes: len(raw),
		NALUCount:    naluCount,
		VideoCodec:   videoCodec,
		AudioCodec:   audioCodec,
	}
	cleanupEmptyFile(&flow.VideoPath)

	seed := models.OfflineMediaProfile{
		VideoCodec:    videoCodec,
		AudioCodec:    audioCodec,
		NALUHistogram: naluHist,
	}
	if videoTrack != nil {
		seed.Width = videoTrack.Width
		seed.Height = videoTrack.Height
	}
	if videoMeta != nil {
		if seed.Width == 0 {
			seed.Width = videoMeta.Width
			seed.Height = videoMeta.Height
		}
		seed.Profile = videoMeta.ProfileName
		seed.Level = videoMeta.Level
		if videoCodec == "h265" {
			seed.LevelName = codec.H265LevelName(videoMeta.Level)
		} else {
			seed.LevelName = codec.H264LevelName(videoMeta.Level)
		}
		seed.FrameRate = videoMeta.FrameRate
	}
	if av1Meta != nil {
		if seed.Width == 0 {
			seed.Width = av1Meta.Width
			seed.Height = av1Meta.Height
		}
		seed.Profile = fmt.Sprintf("%s (%d-bit %s)", av1Meta.ProfileName, av1Meta.BitDepth, av1Meta.ChromaFormat)
		seed.LevelName = av1Meta.LevelName
	}
	if audioTrack != nil {
		seed.AudioSampleRate = audioTrack.SampleRate
		seed.AudioChannels = audioTrack.Channels
	}

	hasContainerSeq := videoTrack != nil && len(videoTrack.CodecPrivate) > 0
	attachMediaProfileWithSamples(&flow, frameDetails, samples, seed, healthContext{
		Mode:                  models.OfflineModeWebM,
		IsHEVC:                videoCodec == "h265",
		HasContainerSeqHeader: hasContainerSeq,
		CheckIDRParams:        videoCodec == "h264" || videoCodec == "h265",
	})
	summary.Flows = append(summary.Flows, flow)
	return nil
}

func handleWebMBlock(
	block []byte, fileOff int64, clusterTime, timecodeScale uint64,
	isSimple, blockGroupKey bool,
	tracks map[uint64]*webmTrack,
	ensureVideoOut func() error,
	videoOut **os.File, videoPos *int64,
	av1Meta **codec.AV1Meta,
	frameDetails *[]models.OfflineFrameDetail,
	samples *[]timedSample,
	naluHist map[string]int,
	naluCount *int,
) {
	if len(block) < 4 {
		return
	}
	trackNum, nLen, _, ok := readEBMLSize(block)
	if !ok || nLen+3 > len(block) {
		return
	}
	relTime := int16(binary.BigEndian.Uint16(block[nLen : nLen+2]))
	flags := block[nLen+2]
	payload := block[nLen+3:]
	payloadFileOff := fileOff + int64(nLen+3)

	tr := tracks[trackNum]
	if tr == nil || len(payload) == 0 {
		return
	}

	rawTicks := int64(clusterTime) + int64(relTime)
	if rawTicks < 0 {
		rawTicks = 0
	}
	dtsMs := (rawTicks * int64(timecodeScale)) / 1000000

	isKey := blockGroupKey
	if isSimple {
		isKey = (flags & 0x80) != 0
	}

	if tr.TrackType == 1 {
		ft := "P"
		if isKey {
			ft = "I"
		}
		streamSrc := "input"
		streamOff := payloadFileOff
		streamLen := len(payload)
		naluLenSize := 0
		isB := false
		sliceKnown := false
		hasInbandParams := false

		switch tr.CodecShort {
		case "av1":
			if *av1Meta == nil {
				if am, err := codec.ParseAV1SequenceHeader(payload); err == nil {
					*av1Meta = am
				}
			}
		case "h264", "h265":
			isHEVC := tr.CodecShort == "h265"
			lenSize := tr.NALULenSize
			if lenSize <= 0 {
				lenSize = 4
			}
			naluLenSize = lenSize
			nalus := codec.SplitAVCC(payload, lenSize, isHEVC)
			hasSPS, hasPPS, hasVPS := false, false, false
			for _, n := range nalus {
				*naluCount++
				naluHist[n.Name]++
				if isHEVC {
					switch n.Type {
					case 32:
						hasVPS = true
					case 33:
						hasSPS = true
					case 34:
						hasPPS = true
					}
					if isH265KeyNALUType(uint8(n.Type)) {
						isKey = true
						ft = "I"
					}
				} else {
					switch n.Type {
					case 7:
						hasSPS = true
					case 8:
						hasPPS = true
					case 5:
						isKey = true
						ft = "I"
						sliceKnown = true
					}
				}
			}
			hasInbandParams = (isHEVC && hasVPS && hasSPS && hasPPS) || (!isHEVC && hasSPS && hasPPS)
			if kind := codec.FrameKindFromNALUs(payload, lenSize, isHEVC); kind != "" {
				ft = kind
				sliceKnown = true
				isB = kind == "B"
				if kind == "I" {
					isKey = true
				}
			}
			if annexb, err := codec.LengthPrefixedNALToAnnexB(payload, lenSize); err == nil && len(annexb) > 0 {
				if err := ensureVideoOut(); err == nil && *videoOut != nil {
					writeOff := *videoPos
					wrote := 0
					if isKey && len(tr.ParamAnnexB) > 0 && !hasInbandParams {
						if n, err := (*videoOut).Write(tr.ParamAnnexB); err == nil {
							wrote += n
						}
					}
					if n, err := (*videoOut).Write(annexb); err == nil {
						wrote += n
					}
					*videoPos += int64(wrote)
					streamSrc = "video"
					streamOff = writeOff
					streamLen = wrote
					naluLenSize = 0
				}
			}
		}

		idx := len(*frameDetails)
		*frameDetails = append(*frameDetails, models.OfflineFrameDetail{
			MediaType:    "video",
			DTS:          dtsMs,
			PTS:          dtsMs,
			CTS:          0,
			FrameLen:     len(payload),
			FrameType:    ft,
			StreamSource: streamSrc,
			StreamOffset: streamOff,
			StreamLen:    streamLen,
			NALULenSize:  naluLenSize,
		})
		*samples = append(*samples, timedSample{
			frameIndex:      idx,
			media:           "video",
			dts:             dtsMs,
			pts:             dtsMs,
			size:            len(payload),
			key:             isKey,
			b:               isB,
			sliceKnown:      sliceKnown,
			hasInbandParams: hasInbandParams,
		})
	} else if tr.TrackType == 2 {
		idx := len(*frameDetails)
		*frameDetails = append(*frameDetails, models.OfflineFrameDetail{
			MediaType:    "audio",
			DTS:          dtsMs,
			PTS:          dtsMs,
			CTS:          0,
			FrameLen:     len(payload),
			FrameType:    "-",
			StreamSource: "input",
			StreamOffset: payloadFileOff,
			StreamLen:    len(payload),
		})
		*samples = append(*samples, timedSample{
			frameIndex:      idx,
			media:           "audio",
			dts:             dtsMs,
			pts:             dtsMs,
			size:            len(payload),
			audioSampleRate: tr.SampleRate,
			audioChannels:   tr.Channels,
		})
	}
}

func buildStartCodedBytes(groups ...[][]byte) []byte {
	var out []byte
	for _, g := range groups {
		for _, nal := range g {
			out = append(out, 0x00, 0x00, 0x00, 0x01)
			out = append(out, nal...)
		}
	}
	return out
}

func parseEBMLHeader(data []byte, docType *string) {
	for pos := 0; pos < len(data); {
		id, idLen, ok := readEBMLID(data[pos:])
		if !ok {
			break
		}
		pos += idLen
		sz, szLen, unk, ok := readEBMLSize(data[pos:])
		if !ok || unk || pos+szLen+int(sz) > len(data) {
			break
		}
		pos += szLen
		if id == ebmlIDDocType {
			dt := strings.TrimRight(string(data[pos:pos+int(sz)]), "\x00")
			if dt != "" {
				*docType = strings.ToLower(dt)
			}
		}
		pos += int(sz)
	}
}

func parseSegmentInfo(data []byte, timecodeScale *uint64) {
	for pos := 0; pos < len(data); {
		id, idLen, ok := readEBMLID(data[pos:])
		if !ok {
			break
		}
		pos += idLen
		sz, szLen, unk, ok := readEBMLSize(data[pos:])
		if !ok || unk || pos+szLen+int(sz) > len(data) {
			break
		}
		pos += szLen
		if id == ebmlIDTimecodeScale {
			if v := readEBMLUint(data[pos : pos+int(sz)]); v > 0 {
				*timecodeScale = v
			}
		}
		pos += int(sz)
	}
}

func parseTracks(data []byte, tracks map[uint64]*webmTrack) {
	for pos := 0; pos < len(data); {
		id, idLen, ok := readEBMLID(data[pos:])
		if !ok {
			break
		}
		pos += idLen
		sz, szLen, unk, ok := readEBMLSize(data[pos:])
		if !ok || unk || pos+szLen+int(sz) > len(data) {
			break
		}
		pos += szLen
		if id == ebmlIDTrackEntry {
			tr := parseTrackEntry(data[pos : pos+int(sz)])
			if tr != nil && tr.Number > 0 {
				tracks[tr.Number] = tr
			}
		}
		pos += int(sz)
	}
}

func parseTrackEntry(data []byte) *webmTrack {
	tr := &webmTrack{}
	for pos := 0; pos < len(data); {
		id, idLen, ok := readEBMLID(data[pos:])
		if !ok {
			break
		}
		pos += idLen
		sz, szLen, unk, ok := readEBMLSize(data[pos:])
		if !ok || unk || pos+szLen+int(sz) > len(data) {
			break
		}
		pos += szLen
		val := data[pos : pos+int(sz)]
		switch id {
		case ebmlIDTrackNumber:
			tr.Number = readEBMLUint(val)
		case ebmlIDTrackType:
			tr.TrackType = readEBMLUint(val)
		case ebmlIDCodecID:
			tr.CodecID = strings.TrimRight(string(val), "\x00")
			tr.CodecShort = normalizeMatroskaCodecID(tr.CodecID)
		case ebmlIDCodecPrivate:
			cp := make([]byte, len(val))
			copy(cp, val)
			tr.CodecPrivate = cp
		case ebmlIDVideo:
			parseTrackVideo(val, tr)
		case ebmlIDAudio:
			parseTrackAudio(val, tr)
		}
		pos += int(sz)
	}
	return tr
}

func parseTrackVideo(data []byte, tr *webmTrack) {
	for pos := 0; pos < len(data); {
		id, idLen, ok := readEBMLID(data[pos:])
		if !ok {
			break
		}
		pos += idLen
		sz, szLen, unk, ok := readEBMLSize(data[pos:])
		if !ok || unk || pos+szLen+int(sz) > len(data) {
			break
		}
		pos += szLen
		val := data[pos : pos+int(sz)]
		if id == ebmlIDPixelWidth {
			tr.Width = int(readEBMLUint(val))
		} else if id == ebmlIDPixelHeight {
			tr.Height = int(readEBMLUint(val))
		}
		pos += int(sz)
	}
}

func parseTrackAudio(data []byte, tr *webmTrack) {
	for pos := 0; pos < len(data); {
		id, idLen, ok := readEBMLID(data[pos:])
		if !ok {
			break
		}
		pos += idLen
		sz, szLen, unk, ok := readEBMLSize(data[pos:])
		if !ok || unk || pos+szLen+int(sz) > len(data) {
			break
		}
		pos += szLen
		val := data[pos : pos+int(sz)]
		if id == ebmlIDSampleFreq {
			tr.SampleRate = int(math.Round(readEBMLFloat(val)))
		} else if id == ebmlIDChannels {
			tr.Channels = int(readEBMLUint(val))
		}
		pos += int(sz)
	}
}

func normalizeMatroskaCodecID(id string) string {
	up := strings.ToUpper(strings.TrimSpace(id))
	switch {
	case strings.Contains(up, "V_MPEG4/ISO/AVC"):
		return "h264"
	case strings.Contains(up, "V_MPEGH/ISO/HEVC"):
		return "h265"
	case up == "V_VP8":
		return "vp8"
	case up == "V_VP9":
		return "vp9"
	case up == "V_AV1":
		return "av1"
	case up == "A_OPUS":
		return "opus"
	case strings.HasPrefix(up, "A_AAC"):
		return "aac"
	case up == "A_VORBIS":
		return "vorbis"
	case strings.Contains(up, "A_MPEG/L3"):
		return "mp3"
	default:
		return strings.ToLower(id)
	}
}

func readEBMLID(b []byte) (uint64, int, bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false
	}
	first := b[0]
	n := 1
	mask := byte(0x80)
	for (first & mask) == 0 {
		n++
		mask >>= 1
		if n > 4 || n > len(b) {
			return 0, 0, false
		}
	}
	var v uint64
	for i := 0; i < n; i++ {
		v = (v << 8) | uint64(b[i])
	}
	return v, n, true
}

func readEBMLSize(b []byte) (uint64, int, bool, bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false, false
	}
	first := b[0]
	n := 1
	mask := byte(0x80)
	for (first & mask) == 0 {
		n++
		mask >>= 1
		if n > 8 || n > len(b) {
			return 0, 0, false, false
		}
	}
	v := uint64(first &^ mask)
	allOnes := v == uint64(mask-1)
	for i := 1; i < n; i++ {
		v = (v << 8) | uint64(b[i])
		if b[i] != 0xFF {
			allOnes = false
		}
	}
	return v, n, allOnes, true
}

func readEBMLUint(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = (v << 8) | uint64(x)
	}
	return v
}

func readEBMLFloat(b []byte) float64 {
	if len(b) == 4 {
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
	}
	if len(b) == 8 {
		return math.Float64frombits(binary.BigEndian.Uint64(b))
	}
	return 0
}

func findNextClusterOrEnd(raw []byte, start, limit int) int {
	for p := start; p+4 <= limit; p++ {
		if raw[p] == 0x1F && raw[p+1] == 0x43 && raw[p+2] == 0xB6 && raw[p+3] == 0x75 {
			return p
		}
	}
	return limit
}
