package offline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"

	"github.com/asticode/go-astits"
)

func (m *Manager) runTS(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	outDir := filepath.Join(taskDir, "ts")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	in, err := os.Open(task.InputPath)
	if err != nil {
		return err
	}
	defer in.Close()

	ctx := context.Background()
	dmx := astits.NewDemuxer(ctx, in)

	type streamOut struct {
		programNumber uint16
		pid           uint16
		streamType    astits.StreamType
		category      string
		codecName     string
		hevc          bool
		artifactRel   string
		file          *os.File
		size          int64
		pesCount      int
		naluCount     int
	}

	streams := make(map[uint16]*streamOut)
	programToPMTPID := map[uint16]uint16{}
	defer func() {
		for _, s := range streams {
			if s != nil && s.file != nil {
				_ = s.file.Close()
			}
		}
	}()

	var pesTotal int
	var naluTotal int
	var patTotal int
	var pmtTotal int

	const (
		maxPESDetailRows   = 8000
		maxNALUBriefPerPES = 48
	)
	pesDetails := make([]models.OfflinePESDetail, 0, 1024)
	var pesSeq int
	var samples []timedSample
	dtsClocks := map[uint16]*ptsClock{}
	ptsClocks := map[uint16]*ptsClock{}
	naluHist := map[string]int{}
	var videoMeta *codec.VideoMeta
	var spsHEVC bool
	var audioRate, audioChannels int

	for {
		d, err := dmx.NextData()
		if errors.Is(err, io.EOF) || errors.Is(err, astits.ErrNoMorePackets) {
			break
		}
		if err != nil {
			return err
		}
		if d == nil {
			continue
		}
		if d.PAT != nil {
			patTotal++
			for _, p := range d.PAT.Programs {
				programToPMTPID[p.ProgramNumber] = p.ProgramMapID
			}
		}
		if d.PMT != nil {
			pmtTotal++
			for _, es := range d.PMT.ElementaryStreams {
				if es == nil {
					continue
				}
				info := describeES(es)
				if existing := streams[es.ElementaryPID]; existing != nil {
					if existing.streamType == 0 {
						existing.streamType = es.StreamType
						existing.category = info.category
						existing.codecName = info.codec
						existing.hevc = info.hevc
						existing.programNumber = d.PMT.ProgramNumber
					}
					continue
				}
				st := &streamOut{
					programNumber: d.PMT.ProgramNumber,
					pid:           es.ElementaryPID,
					streamType:    es.StreamType,
					category:      info.category,
					codecName:     info.codec,
					hevc:          info.hevc,
				}
				name := fmt.Sprintf("pid_%d_%s.es", st.pid, safeName(es.StreamType.String()))
				st.artifactRel = filepath.ToSlash(filepath.Join("ts", name))
				outPath := filepath.Join(taskDir, st.artifactRel)
				f, createErr := os.Create(outPath)
				if createErr != nil {
					return createErr
				}
				st.file = f
				streams[es.ElementaryPID] = st
			}
		}
		if d.PES == nil {
			continue
		}

		pesTotal++
		pesSeq++
		st := streams[d.PID]
		if st == nil {
			st = &streamOut{
				programNumber: 0,
				pid:           d.PID,
				streamType:    0,
				category:      "unknown",
				artifactRel:   filepath.ToSlash(filepath.Join("ts", fmt.Sprintf("pid_%d_unknown.es", d.PID))),
			}
			outPath := filepath.Join(taskDir, st.artifactRel)
			f, createErr := os.Create(outPath)
			if createErr != nil {
				return createErr
			}
			st.file = f
			streams[d.PID] = st
		}

		n, writeErr := st.file.Write(d.PES.Data)
		if writeErr != nil {
			return writeErr
		}
		st.size += int64(n)
		st.pesCount++
		payloadLen := len(d.PES.Data)
		var naluThisPES int
		var naluBriefs []models.OfflineNALUBrief
		hevcNAL := st.hevc || st.streamType == astits.StreamTypeH265Video
		if st.streamType == astits.StreamTypeH264Video || st.streamType == astits.StreamTypeH265Video || st.category == "video" {
			nalus := splitAnnexBNALUs(d.PES.Data)
			naluThisPES = len(nalus)
			st.naluCount += naluThisPES
			naluTotal += naluThisPES
			idx := 0
			for _, nalu := range nalus {
				if len(nalu) == 0 {
					continue
				}
				idx++
				if videoMeta == nil {
					if meta := codec.VideoMetaFromNAL(nalu); meta != nil {
						videoMeta = meta
						spsHEVC = hevcNAL || int((nalu[0]>>1)&0x3f) == 33
					}
				}
				if hevcNAL {
					typ := h265NALUType(nalu)
					tName := codec.NALUTypeName(int(typ), true)
					naluHist[tName]++
					if len(naluBriefs) < maxNALUBriefPerPES {
						naluBriefs = append(naluBriefs, models.OfflineNALUBrief{
							Index:    idx,
							Codec:    "H265",
							Type:     int(typ),
							TypeName: tName,
							Len:      len(nalu),
							Key:      isH265KeyNALUType(typ),
						})
					}
				} else {
					typ := h264NALUType(nalu)
					tName := codec.NALUTypeName(int(typ), false)
					naluHist[tName]++
					if len(naluBriefs) < maxNALUBriefPerPES {
						naluBriefs = append(naluBriefs, models.OfflineNALUBrief{
							Index:    idx,
							Codec:    "H264",
							Type:     int(typ),
							TypeName: tName,
							Len:      len(nalu),
							Key:      typ == 5,
						})
					}
				}
			}
		}

		if len(pesDetails) < maxPESDetailRows {
			detail := models.OfflinePESDetail{
				Seq:           pesSeq,
				PID:           st.pid,
				ProgramNumber: st.programNumber,
				StreamType:    st.streamType.String(),
				Category:      st.category,
				PayloadLen:    payloadLen,
				NALUCount:     naluThisPES,
				NALUs:         naluBriefs,
			}
			if d.PES.Header != nil {
				detail.StreamID = d.PES.Header.StreamID
				if oh := d.PES.Header.OptionalHeader; oh != nil {
					if oh.PTS != nil {
						detail.PTSBase = oh.PTS.Base
						detail.PTSValid = true
					}
					if oh.DTS != nil {
						detail.DTSBase = oh.DTS.Base
						detail.DTSValid = true
					}
				}
			}
			pesDetails = append(pesDetails, detail)
		}
		if audioRate == 0 && st.category == "audio" && (st.codecName == "aac" || st.streamType == astits.StreamTypeAACAudio) {
			if rate, channels, ok := adtsLayout(d.PES.Data); ok {
				audioRate, audioChannels = rate, channels
			}
		}
		if st.category == "video" || st.category == "audio" {
			dtsTick, ptsTick, ok := pesTicks(d.PES)
			if ok {
				dc := dtsClocks[st.pid]
				if dc == nil {
					dc = &ptsClock{}
					dtsClocks[st.pid] = dc
				}
				pc := ptsClocks[st.pid]
				if pc == nil {
					pc = &ptsClock{}
					ptsClocks[st.pid] = pc
				}
				dtsMs := ptsToMs(dc.push(dtsTick))
				ptsMs := ptsToMs(pc.push(ptsTick))
				key := false
				for _, brief := range naluBriefs {
					if brief.Key {
						key = true
						break
					}
				}
				samples = append(samples, timedSample{
					pid:   int(st.pid),
					media: st.category,
					dts:   dtsMs,
					pts:   ptsMs,
					cts:   ptsMs - dtsMs,
					size:  payloadLen,
					key:   key,
				})
			}
		}
	}

	flow := models.OfflineFlowResult{
		FlowID:       1,
		Direction:    "ts",
		DumpRawDir:   "single",
		RawPath:      task.InputPath,
		PayloadBytes: 0,
		ProgramCount: len(programToPMTPID),
		PATCount:     patTotal,
		PMTCount:     pmtTotal,
		PESCount:     pesTotal,
		NALUCount:    naluTotal,
	}

	videoPIDCount := 0
	audioPIDCount := 0
	pids := make([]int, 0, len(streams))
	for pid := range streams {
		pids = append(pids, int(pid))
	}
	sort.Ints(pids)
	for _, pid := range pids {
		s := streams[uint16(pid)]
		flow.PayloadBytes += int(s.size)
		if flow.VideoPath == "" && s.category == "video" {
			flow.VideoPath = filepath.Join(taskDir, s.artifactRel)
			flow.VideoCodec = firstNonEmpty(s.codecName, s.streamType.String())
		}
		if flow.AudioPath == "" && s.category == "audio" {
			flow.AudioPath = filepath.Join(taskDir, s.artifactRel)
			flow.AudioCodec = firstNonEmpty(s.codecName, s.streamType.String())
		}
		if s.category == "video" {
			videoPIDCount++
		}
		if s.category == "audio" {
			audioPIDCount++
		}
	}
	flow.VideoPIDCount = videoPIDCount
	flow.AudioPIDCount = audioPIDCount

	tsScan := scanTSPackets(task.InputPath)

	flow.PIDDetails = make([]models.OfflinePIDDetail, 0, len(pids))
	for _, pid := range pids {
		s := streams[uint16(pid)]
		detail := models.OfflinePIDDetail{
			PID:        s.pid,
			StreamType: s.streamType.String(),
			Codec:      s.codecName,
			Category:   s.category,
			PESCount:   s.pesCount,
			NALUCount:  s.naluCount,
			Bytes:      s.size,
			OutputPath: filepath.Join(taskDir, s.artifactRel),
		}
		if ps := tsScan.pids[s.pid]; ps != nil {
			detail.PacketCount = ps.packets
			detail.CCErrors = ps.ccErrors
			detail.PCRCount = ps.pcrCount
		}
		flow.PIDDetails = append(flow.PIDDetails, detail)
	}

	if len(programToPMTPID) == 0 && pesTotal == 0 {
		flow.Error = "no PAT/PMT/PES parsed from TS"
	}
	if len(programToPMTPID) > 0 || pesTotal > 0 {
		flow.Direction = "ts"
	}

	flow.PESDetailTotal = pesTotal
	flow.PESDetails = pesDetails
	flow.PESDetailsTruncated = pesTotal > len(pesDetails)
	seed := profileFromVideoMeta(videoMeta, spsHEVC || flow.VideoCodec == "h265")
	seed.VideoCodec = flow.VideoCodec
	seed.AudioCodec = flow.AudioCodec
	seed.AudioSampleRate = audioRate
	seed.AudioChannels = audioChannels
	seed.TSPacketCount = tsScan.packets
	seed.TSSyncErrors = tsScan.syncErrors
	seed.TSTEIErrors = tsScan.teiErrors
	seed.TSCCErrors = tsScan.ccErrors
	seed.PCRCount = tsScan.pcrCount
	seed.PCRMaxGapMS = tsScan.pcrMaxGapMS
	if len(naluHist) > 0 {
		seed.NALUHistogram = naluHist
	}
	if tsScan.ccErrors > 0 {
		seed.Warnings = append(seed.Warnings, fmt.Sprintf("TS 检测到 %d 次连续计数 (CC) 跳变，存在丢包或乱序", tsScan.ccErrors))
	}
	if tsScan.teiErrors > 0 {
		seed.Warnings = append(seed.Warnings, fmt.Sprintf("TS 检测到 %d 个传输错误包 (TEI=1)", tsScan.teiErrors))
	}
	if tsScan.syncErrors > 0 {
		seed.Warnings = append(seed.Warnings, fmt.Sprintf("TS 检测到 %d 次同步字节 (0x47) 丢失", tsScan.syncErrors))
	}
	if tsScan.pcrMaxGapMS > 100 {
		seed.Warnings = append(seed.Warnings, fmt.Sprintf("TS PCR 最大间隔达到 %d ms（超过 100 ms 规范上限）", tsScan.pcrMaxGapMS))
	}
	attachSampleProfile(&flow, samples, seed)

	summary.Flows = append(summary.Flows, flow)
	return nil
}

func h264NALUType(nalu []byte) uint8 {
	if len(nalu) == 0 {
		return 0
	}
	return nalu[0] & 0x1F
}

func h265NALUType(nalu []byte) uint8 {
	if len(nalu) < 2 {
		return 0
	}
	return (nalu[0] >> 1) & 0x3F
}

func isH265KeyNALUType(t uint8) bool {
	return (t >= 16 && t <= 21) || t == 19 || t == 20
}

func streamCategory(t astits.StreamType) string {
	if t.IsVideo() {
		return "video"
	}
	if t.IsAudio() {
		return "audio"
	}
	return "other"
}

type esIdentity struct {
	category string
	codec    string
	hevc     bool
}

func describeES(es *astits.PMTElementaryStream) esIdentity {
	video, audio := tsCodecName(es.StreamType)
	if video == "" && audio == "" {
		video, audio = codecFromDescriptors(es.ElementaryStreamDescriptors)
	}
	identity := esIdentity{category: streamCategory(es.StreamType), hevc: es.StreamType == astits.StreamTypeH265Video}
	if es.StreamType.IsVideo() || (video != "" && !es.StreamType.IsAudio()) {
		identity.category = "video"
		identity.codec = video
	} else if es.StreamType.IsAudio() || audio != "" {
		identity.category = "audio"
		identity.codec = audio
	}
	return identity
}

func tsCodecName(t astits.StreamType) (video string, audio string) {
	switch t {
	case astits.StreamTypeH264Video:
		return "h264", ""
	case astits.StreamTypeH265Video:
		return "h265", ""
	case astits.StreamTypeMPEG1Video:
		return "mpeg1", ""
	case astits.StreamTypeMPEG2Video:
		return "mpeg2", ""
	case astits.StreamTypeMPEG4Video:
		return "mpeg4", ""
	case astits.StreamTypeVC1Video:
		return "vc1", ""
	case astits.StreamTypeDIRACVideo:
		return "dirac", ""
	case astits.StreamTypeCAVSVideo:
		return "cavs", ""
	case astits.StreamTypeAACAudio, astits.StreamTypeAACLATMAudio:
		return "", "aac"
	case astits.StreamTypeMPEG1Audio, astits.StreamTypeMPEG2Audio:
		return "", "mp2"
	case astits.StreamTypeAC3Audio:
		return "", "ac3"
	case astits.StreamTypeEAC3Audio:
		return "", "eac3"
	case astits.StreamTypeDTSAudio:
		return "", "dts"
	case astits.StreamTypeTRUEHDAudio:
		return "", "truehd"
	default:
		return "", ""
	}
}

func codecFromDescriptors(list []*astits.Descriptor) (video string, audio string) {
	for _, d := range list {
		if d == nil {
			continue
		}
		switch d.Tag {
		case astits.DescriptorTagAC3:
			return "", "ac3"
		case astits.DescriptorTagEnhancedAC3:
			return "", "eac3"
		case astits.DescriptorTagRegistration:
			if d.Registration == nil {
				continue
			}
			switch d.Registration.FormatIdentifier {
			case 0x41432d33: // "AC-3"
				return "", "ac3"
			case 0x45414333: // "EAC3"
				return "", "eac3"
			}
		}
	}
	return "", ""
}

type ptsClock struct {
	has       bool
	lastRaw   int64
	unwrapped int64
}

func (c *ptsClock) push(raw int64) int64 {
	if !c.has {
		c.has = true
		c.lastRaw = raw
		c.unwrapped = raw
		return raw
	}
	delta := raw - c.lastRaw
	const mod = int64(1) << 33
	if delta < -mod/2 {
		delta += mod
	} else if delta > mod/2 {
		delta -= mod
	}
	c.lastRaw = raw
	c.unwrapped += delta
	return c.unwrapped
}

func pesTicks(pes *astits.PESData) (dts int64, pts int64, ok bool) {
	if pes == nil || pes.Header == nil || pes.Header.OptionalHeader == nil {
		return 0, 0, false
	}
	header := pes.Header.OptionalHeader
	switch {
	case header.DTS != nil && header.PTS != nil:
		return header.DTS.Base, header.PTS.Base, true
	case header.DTS != nil:
		return header.DTS.Base, header.DTS.Base, true
	case header.PTS != nil:
		return header.PTS.Base, header.PTS.Base, true
	default:
		return 0, 0, false
	}
}

type tsPIDScan struct {
	packets   int
	ccErrors  int
	pcrCount  int
	hasCC     bool
	lastCC    uint8
	dupCount  int
	pcrClock  ptsClock
	hasPCR    bool
	lastPCRMs int64
}

type tsScanResult struct {
	packets     int
	syncErrors  int
	teiErrors   int
	ccErrors    int
	pcrCount    int
	pcrMaxGapMS int64
	pids        map[uint16]*tsPIDScan
}

func scanTSPackets(path string) tsScanResult {
	res := tsScanResult{pids: map[uint16]*tsPIDScan{}}
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 188 {
		return res
	}
	stride, off := detectTSSync(data)
	if stride == 0 {
		stride = 188
		off = 0
	}

	pos := off
	for pos+188 <= len(data) {
		if data[pos] != 0x47 {
			res.syncErrors++
			next := -1
			for j := pos + 1; j+stride < len(data); j++ {
				if data[j] == 0x47 && data[j+stride] == 0x47 {
					next = j
					break
				}
			}
			if next < 0 {
				break
			}
			pos = next
			continue
		}

		pkt := data[pos : pos+188]
		pos += stride
		res.packets++

		if (pkt[1] & 0x80) != 0 {
			res.teiErrors++
			continue
		}
		pid := uint16(pkt[1]&0x1F)<<8 | uint16(pkt[2])
		if pid == 0x1FFF {
			continue
		}

		ps := res.pids[pid]
		if ps == nil {
			ps = &tsPIDScan{}
			res.pids[pid] = ps
		}
		ps.packets++

		afc := (pkt[3] >> 4) & 0x03
		cc := pkt[3] & 0x0F
		hasAdapt := afc == 2 || afc == 3
		hasPayload := afc == 1 || afc == 3
		discontinuity := false

		if hasAdapt {
			afLen := int(pkt[4])
			if afLen >= 1 && 5+afLen <= 188 {
				flags := pkt[5]
				discontinuity = (flags & 0x80) != 0
				hasPCR := (flags & 0x10) != 0
				if hasPCR && afLen >= 7 {
					pcrBase := (int64(pkt[6]) << 25) |
						(int64(pkt[7]) << 17) |
						(int64(pkt[8]) << 9) |
						(int64(pkt[9]) << 1) |
						(int64(pkt[10]) >> 7)
					pcrMs := ptsToMs(ps.pcrClock.push(pcrBase))
					ps.pcrCount++
					res.pcrCount++
					if ps.hasPCR && !discontinuity {
						gap := pcrMs - ps.lastPCRMs
						if gap > res.pcrMaxGapMS {
							res.pcrMaxGapMS = gap
						}
					}
					ps.lastPCRMs = pcrMs
					ps.hasPCR = true
				}
			}
		}

		if discontinuity {
			ps.hasCC = false
			ps.dupCount = 0
		}
		if hasPayload {
			if !ps.hasCC {
				ps.hasCC = true
				ps.lastCC = cc
				ps.dupCount = 0
			} else if cc == ps.lastCC {
				ps.dupCount++
				if ps.dupCount > 1 {
					ps.ccErrors++
					res.ccErrors++
				}
			} else if cc == ((ps.lastCC + 1) & 0x0F) {
				ps.lastCC = cc
				ps.dupCount = 0
			} else {
				ps.ccErrors++
				res.ccErrors++
				ps.lastCC = cc
				ps.dupCount = 0
			}
		}
	}
	return res
}

func ptsToMs(ticks int64) int64 {
	return ticks * 1000 / 90000
}

func adtsLayout(b []byte) (rate int, channels int, ok bool) {
	rates := []int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
	for i := 0; i+7 <= len(b) && i < 16; i++ {
		if b[i] != 0xFF || b[i+1]&0xF0 != 0xF0 {
			continue
		}
		freqIdx := int((b[i+2] & 0x3C) >> 2)
		channels = int((b[i+2]&0x01)<<2 | (b[i+3] >> 6))
		if freqIdx < len(rates) {
			rate = rates[freqIdx]
		}
		return rate, channels, rate > 0 && channels > 0
	}
	return 0, 0, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" && value != "Unknown" {
			return value
		}
	}
	return ""
}

func splitAnnexBNALUs(b []byte) [][]byte {
	var out [][]byte
	starts := findStartCodes(b)
	if len(starts) == 0 {
		return out
	}
	for i := 0; i < len(starts); i++ {
		start := starts[i]
		next := len(b)
		if i+1 < len(starts) {
			next = starts[i+1]
		}
		scLen := startCodeLenAt(b, start)
		naluStart := start + scLen
		if naluStart >= next {
			continue
		}
		out = append(out, b[naluStart:next])
	}
	return out
}

func findStartCodes(b []byte) []int {
	var idx []int
	for i := 0; i+3 < len(b); i++ {
		if b[i] != 0x00 || b[i+1] != 0x00 {
			continue
		}
		if b[i+2] == 0x01 {
			idx = append(idx, i)
		} else if i+3 < len(b) && b[i+2] == 0x00 && b[i+3] == 0x01 {
			idx = append(idx, i)
		}
	}
	return dedupStartIndex(idx)
}

func dedupStartIndex(in []int) []int {
	if len(in) <= 1 {
		return in
	}
	out := make([]int, 0, len(in))
	out = append(out, in[0])
	for i := 1; i < len(in); i++ {
		if in[i]-in[i-1] <= 1 {
			continue
		}
		out = append(out, in[i])
	}
	return out
}

func startCodeLenAt(b []byte, i int) int {
	if i+3 < len(b) && b[i] == 0x00 && b[i+1] == 0x00 && b[i+2] == 0x00 && b[i+3] == 0x01 {
		return 4
	}
	return 3
}

func safeName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "unknown"
	}
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}
