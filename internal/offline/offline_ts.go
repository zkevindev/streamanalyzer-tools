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
				if streams[es.ElementaryPID] != nil {
					continue
				}
				st := &streamOut{
					programNumber: d.PMT.ProgramNumber,
					pid:           es.ElementaryPID,
					streamType:    es.StreamType,
					category:      streamCategory(es.StreamType),
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
				if len(naluBriefs) >= maxNALUBriefPerPES {
					break
				}
				if st.streamType == astits.StreamTypeH265Video {
					typ := h265NALUType(nalu)
					naluBriefs = append(naluBriefs, models.OfflineNALUBrief{
						Index:    idx,
						Codec:    "H265",
						Type:     int(typ),
						TypeName: fmt.Sprintf("nalu_t%d", typ),
						Len:      len(nalu),
						Key:      isH265KeyNALUType(typ),
					})
				} else {
					typ := h264NALUType(nalu)
					naluBriefs = append(naluBriefs, models.OfflineNALUBrief{
						Index:    idx,
						Codec:    "H264",
						Type:     int(typ),
						TypeName: h264TypeName(typ),
						Len:      len(nalu),
						Key:      typ == 5,
					})
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
			flow.VideoCodec = s.streamType.String()
		}
		if flow.AudioPath == "" && s.category == "audio" {
			flow.AudioPath = filepath.Join(taskDir, s.artifactRel)
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

	flow.PIDDetails = make([]models.OfflinePIDDetail, 0, len(pids))
	for _, pid := range pids {
		s := streams[uint16(pid)]
		flow.PIDDetails = append(flow.PIDDetails, models.OfflinePIDDetail{
			PID:        s.pid,
			StreamType: s.streamType.String(),
			Category:   s.category,
			PESCount:   s.pesCount,
			NALUCount:  s.naluCount,
			Bytes:      s.size,
			OutputPath: filepath.Join(taskDir, s.artifactRel),
		})
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

func h264TypeName(t uint8) string {
	switch t {
	case 1:
		return "non-IDR"
	case 5:
		return "IDR"
	case 6:
		return "SEI"
	case 7:
		return "SPS"
	case 8:
		return "PPS"
	case 9:
		return "AUD"
	default:
		return "other"
	}
}

func streamCategory(t astits.StreamType) string {
	switch t {
	case astits.StreamTypeH264Video, astits.StreamTypeH265Video, astits.StreamTypeMPEG1Video, astits.StreamTypeMPEG2Video:
		return "video"
	case astits.StreamTypeAACAudio, astits.StreamTypeMPEG1Audio, astits.StreamTypeMPEG2Audio:
		return "audio"
	default:
		return "other"
	}
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
