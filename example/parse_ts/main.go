package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/asticode/go-astits"
)

type streamInfo struct {
	ProgramNumber uint16
	PID           uint16
	StreamType    astits.StreamType
	Category      string
	OutputPath    string
	File          *os.File
	BytesWritten  int64
	PESCount      int64
	NALUCount     int64
}

func main() {
	input := flag.String("i", "", "输入 TS 文件路径")
	outputDir := flag.String("o", "./output", "输出目录")
	printPAT := flag.Bool("print-pat", true, "是否打印 PAT 信息")
	printPMT := flag.Bool("print-pmt", true, "是否打印 PMT 信息")
	printPES := flag.Bool("print-pes", true, "是否打印 PES 信息")
	parseNALU := flag.Bool("parse-nalu", true, "是否解析视频 NALU 信息(H264/H265)")
	flag.Parse()

	if strings.TrimSpace(*input) == "" {
		fmt.Fprintln(os.Stderr, "请通过 -i 指定 TS 文件路径")
		os.Exit(2)
	}

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建输出目录失败: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Open(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开输入文件失败: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	ctx := context.Background()
	dmx := astits.NewDemuxer(ctx, f)

	programToPMTPID := map[uint16]uint16{}
	streams := map[uint16]*streamInfo{}
	defer closeAll(streams)

	var pesSeq int64

	for {
		d, err := dmx.NextData()
		if errors.Is(err, io.EOF) || errors.Is(err, astits.ErrNoMorePackets) {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "解析出错: %v\n", err)
			os.Exit(1)
		}
		if d == nil {
			continue
		}

		if d.PAT != nil {
			if *printPAT {
				fmt.Println("==== PAT ====")
			}
			for _, p := range d.PAT.Programs {
				programToPMTPID[p.ProgramNumber] = p.ProgramMapID
				if *printPAT {
					fmt.Printf("program=%d pmt_pid=%d\n", p.ProgramNumber, p.ProgramMapID)
				}
			}
		}

		if d.PMT != nil {
			if *printPMT {
				fmt.Println("==== PMT ====")
				fmt.Printf("program=%d pcr_pid=%d streams=%d\n", d.PMT.ProgramNumber, d.PMT.PCRPID, len(d.PMT.ElementaryStreams))
			}
			for _, es := range d.PMT.ElementaryStreams {
				category := streamCategory(es.StreamType)
				si := streams[es.ElementaryPID]
				if si == nil {
					si = &streamInfo{
						ProgramNumber: d.PMT.ProgramNumber,
						PID:           es.ElementaryPID,
						StreamType:    es.StreamType,
						Category:      category,
					}
					name := fmt.Sprintf("program_%d_pid_%d_%s.es", si.ProgramNumber, si.PID, safeName(si.StreamType.String()))
					si.OutputPath = filepath.Join(*outputDir, name)

					out, createErr := os.Create(si.OutputPath)
					if createErr != nil {
						fmt.Fprintf(os.Stderr, "创建输出文件失败(pid=%d): %v\n", es.ElementaryPID, createErr)
						os.Exit(1)
					}
					si.File = out
					streams[es.ElementaryPID] = si
				}
				if *printPMT {
					fmt.Printf("pid=%d stream_type=0x%x (%s) category=%s\n", es.ElementaryPID, uint8(es.StreamType), es.StreamType.String(), category)
				}
			}
		}

		if d.PES != nil {
			pesSeq++
			pid := d.PID
			si := streams[pid]
			if si == nil {
				// 有些流可能在 PMT 前就出现，这里按 PID 兜底创建。
				si = &streamInfo{
					ProgramNumber: 0,
					PID:           pid,
					StreamType:    0,
					Category:      "unknown",
				}
				si.OutputPath = filepath.Join(*outputDir, fmt.Sprintf("pid_%d_unknown.es", pid))
				out, createErr := os.Create(si.OutputPath)
				if createErr != nil {
					fmt.Fprintf(os.Stderr, "创建输出文件失败(pid=%d): %v\n", pid, createErr)
					os.Exit(1)
				}
				si.File = out
				streams[pid] = si
			}

			payloadLen := len(d.PES.Data)
			n, writeErr := si.File.Write(d.PES.Data)
			if writeErr != nil {
				fmt.Fprintf(os.Stderr, "写入 ES 文件失败(pid=%d): %v\n", pid, writeErr)
				os.Exit(1)
			}
			si.BytesWritten += int64(n)
			si.PESCount++

			if *printPES {
				streamID := byte(0)
				ptsText := "N/A"
				dtsText := "N/A"
				if d.PES.Header != nil {
					streamID = d.PES.Header.StreamID
					if d.PES.Header.OptionalHeader != nil {
						ptsText = formatClockReference(d.PES.Header.OptionalHeader.PTS)
						dtsText = formatClockReference(d.PES.Header.OptionalHeader.DTS)
					}
				}
				fmt.Printf("PES #%d pid=%d stream_id=0x%x pts=%s dts=%s payload=%d bytes out=%s\n",
					pesSeq, pid, streamID, ptsText, dtsText, payloadLen, si.OutputPath)
			}

			if *parseNALU && (si.StreamType == astits.StreamTypeH264Video || si.StreamType == astits.StreamTypeH265Video || si.Category == "video") {
				nalus := splitAnnexBNALUs(d.PES.Data)
				for _, nalu := range nalus {
					if len(nalu) == 0 {
						continue
					}
					si.NALUCount++
					if si.StreamType == astits.StreamTypeH265Video {
						typ := h265NALUType(nalu)
						fmt.Printf("  NALU pid=%d idx=%d codec=H265 type=%d len=%d key=%v\n",
							pid, si.NALUCount, typ, len(nalu), isH265KeyNALUType(typ))
					} else {
						typ := h264NALUType(nalu)
						fmt.Printf("  NALU pid=%d idx=%d codec=H264 type=%d(%s) len=%d key=%v\n",
							pid, si.NALUCount, typ, h264TypeName(typ), len(nalu), typ == 5)
					}
				}
			}
		}
	}

	printSummary(programToPMTPID, streams)
}

func closeAll(streams map[uint16]*streamInfo) {
	for _, s := range streams {
		if s != nil && s.File != nil {
			_ = s.File.Close()
		}
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
	// Annex-B 起始码格式: 00 00 01 或 00 00 00 01
	var out [][]byte
	starts := findStartCodes(b)
	if len(starts) == 0 {
		return out
	}
	for i := 0; i < len(starts); i++ {
		start := starts[i]
		var next int
		if i+1 < len(starts) {
			next = starts[i+1]
		} else {
			next = len(b)
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
		if b[i] == 0x00 && b[i+1] == 0x00 {
			if b[i+2] == 0x01 {
				idx = append(idx, i)
			} else if i+3 < len(b) && b[i+2] == 0x00 && b[i+3] == 0x01 {
				idx = append(idx, i)
			}
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

func printSummary(programToPMTPID map[uint16]uint16, streams map[uint16]*streamInfo) {
	fmt.Println("==== 解析完成 ====")
	if len(programToPMTPID) > 0 {
		fmt.Println("PAT 概览:")
		programs := make([]int, 0, len(programToPMTPID))
		for pn := range programToPMTPID {
			programs = append(programs, int(pn))
		}
		sort.Ints(programs)
		for _, p := range programs {
			fmt.Printf("  program=%d pmt_pid=%d\n", p, programToPMTPID[uint16(p)])
		}
	}

	if len(streams) == 0 {
		fmt.Println("未发现任何 PES/ES 流")
		return
	}

	fmt.Println("流输出概览:")
	pids := make([]int, 0, len(streams))
	for pid := range streams {
		pids = append(pids, int(pid))
	}
	sort.Ints(pids)
	for _, pid := range pids {
		s := streams[uint16(pid)]
		fmt.Printf("  pid=%d program=%d stream_type=%s category=%s pes=%d nalu=%d bytes=%d file=%s\n",
			s.PID, s.ProgramNumber, s.StreamType.String(), s.Category, s.PESCount, s.NALUCount, s.BytesWritten, s.OutputPath)
	}
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

func formatClockReference(cr *astits.ClockReference) string {
	if cr == nil {
		return "N/A"
	}
	return fmt.Sprintf("%d(%.3fs)", cr.Base, cr.Duration().Seconds())
}
