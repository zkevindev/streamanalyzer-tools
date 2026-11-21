package offline

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
)

type rtpFlowKey struct {
	srcIP   string
	srcPort uint16
	dstIP   string
	dstPort uint16
	ssrc    uint32
}

type rtpPacketRec struct {
	ts      time.Time
	pkt     rtp.Packet
	rawSize int
}

type rtpFlowAccum struct {
	key        rtpFlowKey
	packets    []rtpPacketRec
	rtcpCount  int
	senderPkts uint32
	fracLost   float64
}

type udpFlowKey struct {
	srcIP   string
	srcPort uint16
	dstIP   string
	dstPort uint16
}

type tcpStreamKey struct {
	srcIP   string
	srcPort uint16
	dstIP   string
	dstPort uint16
}

type tcpSeg struct {
	seq     uint32
	payload []byte
}

// extractExtendedPCAPFlows scans a PCAP / PCAPNG file for non-RTMP media streams:
// 1. UDP RTP / RTCP flows (per SSRC: loss, out-of-order, jitter, payload depacketization for H.264/H.265/PS/TS/G.711)
// 2. UDP raw MPEG-TS flows (188-byte sync 0x47)
// 3. TCP HTTP-FLV & HTTP MPEG-TS responses
func (m *Manager) extractExtendedPCAPFlows(ctx context.Context, task *models.OfflineTask, taskDir string, nextFlowID int) ([]models.OfflineFlowResult, error) {
	f, err := os.Open(task.InputPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var head [4]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var src *gopacket.PacketSource
	if head == [4]byte{0x0a, 0x0d, 0x0d, 0x0a} {
		ng, err := pcapgo.NewNgReader(f, pcapgo.DefaultNgReaderOptions)
		if err != nil {
			return nil, err
		}
		src = gopacket.NewPacketSource(ng, ng.LinkType())
	} else {
		r, err := pcapgo.NewReader(f)
		if err != nil {
			return nil, err
		}
		src = gopacket.NewPacketSource(r, r.LinkType())
	}

	rtpFlows := map[rtpFlowKey]*rtpFlowAccum{}
	rtcpBySSRC := map[uint32]struct {
		count      int
		senderPkts uint32
		fracLost   float64
	}{}
	udpTS := map[udpFlowKey]*bytes.Buffer{}
	tcpSegs := map[tcpStreamKey][]tcpSeg{}

	for pkt := range src.Packets() {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		netLayer := pkt.NetworkLayer()
		if netLayer == nil {
			continue
		}
		srcIP := netLayer.NetworkFlow().Src().String()
		dstIP := netLayer.NetworkFlow().Dst().String()

		if udpLayer := pkt.Layer(layers.LayerTypeUDP); udpLayer != nil {
			udp := udpLayer.(*layers.UDP)
			payload := udp.Payload
			if len(payload) < 8 {
				continue
			}
			// Check UDP raw MPEG-TS (multiple of 188 starting with 0x47)
			if len(payload) >= 188 && len(payload)%188 == 0 && payload[0] == 0x47 && (len(payload) < 376 || payload[188] == 0x47) {
				uk := udpFlowKey{srcIP, uint16(udp.SrcPort), dstIP, uint16(udp.DstPort)}
				buf := udpTS[uk]
				if buf == nil {
					buf = &bytes.Buffer{}
					udpTS[uk] = buf
				}
				buf.Write(payload)
				continue
			}
			// Check RTCP (RFC 5761: PT 200..204, version == 2)
			if len(payload) >= 8 && (payload[0]>>6) == 2 && payload[1] >= 200 && payload[1] <= 204 {
				if pkts, err := rtcp.Unmarshal(payload); err == nil {
					for _, cp := range pkts {
						switch rp := cp.(type) {
						case *rtcp.SenderReport:
							e := rtcpBySSRC[rp.SSRC]
							e.count++
							e.senderPkts = rp.PacketCount
							rtcpBySSRC[rp.SSRC] = e
						case *rtcp.ReceiverReport:
							for _, rep := range rp.Reports {
								e := rtcpBySSRC[rep.SSRC]
								e.count++
								e.fracLost = float64(rep.FractionLost) * 100.0 / 256.0
								rtcpBySSRC[rep.SSRC] = e
							}
						}
					}
				}
				continue
			}
			// Check RTP (version == 2, length >= 12, PT not in RTCP range 64..95)
			if len(payload) >= 12 && (payload[0]>>6) == 2 {
				pt := payload[1] & 0x7F
				if pt < 64 || pt >= 96 {
					var rp rtp.Packet
					if err := rp.Unmarshal(payload); err == nil && len(rp.Payload) > 0 {
						rk := rtpFlowKey{srcIP, uint16(udp.SrcPort), dstIP, uint16(udp.DstPort), rp.SSRC}
						acc := rtpFlows[rk]
						if acc == nil {
							acc = &rtpFlowAccum{key: rk}
							rtpFlows[rk] = acc
						}
						acc.packets = append(acc.packets, rtpPacketRec{
							ts:      pkt.Metadata().Timestamp,
							pkt:     rp,
							rawSize: len(payload),
						})
					}
				}
			}
			continue
		}

		if tcpLayer := pkt.Layer(layers.LayerTypeTCP); tcpLayer != nil {
			tcp := tcpLayer.(*layers.TCP)
			if len(tcp.Payload) == 0 {
				continue
			}
			// Skip RTMP standard port handled by pcaprtmp unless user set another port; still inspect HTTP-FLV/TS on any port.
			tk := tcpStreamKey{srcIP, uint16(tcp.SrcPort), dstIP, uint16(tcp.DstPort)}
			cp := make([]byte, len(tcp.Payload))
			copy(cp, tcp.Payload)
			tcpSegs[tk] = append(tcpSegs[tk], tcpSeg{seq: tcp.Seq, payload: cp})
		}
	}

	var results []models.OfflineFlowResult

	// 1. Process UDP RTP flows (require >= 3 packets per SSRC to avoid random UDP noise)
	rtpKeys := make([]rtpFlowKey, 0, len(rtpFlows))
	for k, acc := range rtpFlows {
		if len(acc.packets) >= 3 {
			rtpKeys = append(rtpKeys, k)
		}
	}
	sort.Slice(rtpKeys, func(i, j int) bool { return rtpKeys[i].ssrc < rtpKeys[j].ssrc })

	for _, k := range rtpKeys {
		acc := rtpFlows[k]
		if rinfo, ok := rtcpBySSRC[k.ssrc]; ok {
			acc.rtcpCount = rinfo.count
			acc.senderPkts = rinfo.senderPkts
			acc.fracLost = rinfo.fracLost
		}
		flow := m.buildRTPFlowResult(ctx, task.ID, acc, taskDir, nextFlowID)
		results = append(results, flow)
		nextFlowID++
	}

	// 2. Process UDP raw MPEG-TS flows
	for uk, buf := range udpTS {
		if buf.Len() < 188*3 {
			continue
		}
		outDir := filepath.Join(taskDir, fmt.Sprintf("pcap_udpts_%03d", nextFlowID))
		_ = os.MkdirAll(outDir, 0o755)
		tsPath := filepath.Join(outDir, "stream.ts")
		if err := os.WriteFile(tsPath, buf.Bytes(), 0o644); err != nil {
			continue
		}
		subSum := &models.OfflineSummary{}
		subTask := &models.OfflineTask{ID: "", Mode: models.OfflineModeTS, InputPath: tsPath, InputSize: int64(buf.Len())}
		if err := m.runTS(subTask, outDir, subSum); err == nil && len(subSum.Flows) > 0 {
			fr := subSum.Flows[0]
			fr.FlowID = nextFlowID
			fr.Protocol = "mpeg-ts"
			fr.Direction = "udp-ts"
			fr.ServerIP = uk.srcIP
			fr.ServerPort = uk.srcPort
			fr.ClientIP = uk.dstIP
			fr.ClientPort = uk.dstPort
			fr.RawPath = tsPath
			results = append(results, fr)
			nextFlowID++
		}
	}

	// 3. Process TCP HTTP-FLV & HTTP MPEG-TS flows
	for tk, segs := range tcpSegs {
		streamBytes := reassembleTCPSegments(segs)
		if len(streamBytes) < 32 {
			continue
		}
		// Look for HTTP/1.x response header separator
		if !bytes.HasPrefix(streamBytes, []byte("HTTP/1.")) {
			continue
		}
		hdrEnd := bytes.Index(streamBytes, []byte("\r\n\r\n"))
		if hdrEnd < 0 || hdrEnd+4 >= len(streamBytes) {
			continue
		}
		hdr := string(streamBytes[:hdrEnd])
		body := streamBytes[hdrEnd+4:]
		if strings.Contains(strings.ToLower(hdr), "transfer-encoding: chunked") {
			body = decodeHTTPChunked(body)
		}
		if len(body) < 16 {
			continue
		}

		// HTTP-FLV
		if len(body) >= 9 && bytes.Equal(body[:3], []byte{'F', 'L', 'V'}) && body[3] == 1 {
			outDir := filepath.Join(taskDir, fmt.Sprintf("pcap_httpflv_%03d", nextFlowID))
			_ = os.MkdirAll(outDir, 0o755)
			flvPath := filepath.Join(outDir, "stream.flv")
			if err := os.WriteFile(flvPath, body, 0o644); err != nil {
				continue
			}
			subSum := &models.OfflineSummary{}
			subTask := &models.OfflineTask{ID: "", Mode: models.OfflineModeFLV, InputPath: flvPath, InputSize: int64(len(body))}
			if err := m.runFLV(subTask, outDir, subSum); err == nil && len(subSum.Flows) > 0 {
				fr := subSum.Flows[0]
				fr.FlowID = nextFlowID
				fr.Protocol = "http-flv"
				fr.Direction = "http-flv"
				fr.ServerIP = tk.srcIP
				fr.ServerPort = tk.srcPort
				fr.ClientIP = tk.dstIP
				fr.ClientPort = tk.dstPort
				fr.RawPath = flvPath
				results = append(results, fr)
				nextFlowID++
			}
			continue
		}

		// HTTP MPEG-TS
		if stride, _ := detectTSSync(body); stride > 0 {
			outDir := filepath.Join(taskDir, fmt.Sprintf("pcap_httpts_%03d", nextFlowID))
			_ = os.MkdirAll(outDir, 0o755)
			tsPath := filepath.Join(outDir, "stream.ts")
			if err := os.WriteFile(tsPath, body, 0o644); err != nil {
				continue
			}
			subSum := &models.OfflineSummary{}
			subTask := &models.OfflineTask{ID: "", Mode: models.OfflineModeTS, InputPath: tsPath, InputSize: int64(len(body))}
			if err := m.runTS(subTask, outDir, subSum); err == nil && len(subSum.Flows) > 0 {
				fr := subSum.Flows[0]
				fr.FlowID = nextFlowID
				fr.Protocol = "mpeg-ts"
				fr.Direction = "http-ts"
				fr.ServerIP = tk.srcIP
				fr.ServerPort = tk.srcPort
				fr.ClientIP = tk.dstIP
				fr.ClientPort = tk.dstPort
				fr.RawPath = tsPath
				results = append(results, fr)
				nextFlowID++
			}
		}
	}

	return results, nil
}

func (m *Manager) buildRTPFlowResult(ctx context.Context, taskID string, acc *rtpFlowAccum, taskDir string, flowID int) models.OfflineFlowResult {
	stats := computeRTPStats(acc)
	outDir := filepath.Join(taskDir, fmt.Sprintf("pcap_rtp_%03d", flowID))
	_ = os.MkdirAll(outDir, 0o755)

	// Sort packets by extended sequence number for media depacketization.
	ordered := make([]rtpPacketRec, len(acc.packets))
	copy(ordered, acc.packets)
	sort.SliceStable(ordered, func(i, j int) bool {
		diff := int16(ordered[i].pkt.SequenceNumber - ordered[j].pkt.SequenceNumber)
		return diff < 0
	})

	pt := stats.PayloadType
	var flow models.OfflineFlowResult

	// 1. PT=33 MPEG-TS
	if pt == 33 {
		var tsBuf bytes.Buffer
		for _, r := range ordered {
			tsBuf.Write(r.pkt.Payload)
		}
		tsPath := filepath.Join(outDir, "rtp_mpegts.ts")
		if err := os.WriteFile(tsPath, tsBuf.Bytes(), 0o644); err == nil {
			subSum := &models.OfflineSummary{}
			subTask := &models.OfflineTask{ID: "", Mode: models.OfflineModeTS, InputPath: tsPath, InputSize: int64(tsBuf.Len())}
			if err := m.runTS(subTask, outDir, subSum); err == nil && len(subSum.Flows) > 0 {
				flow = subSum.Flows[0]
				flow.RawPath = tsPath
			}
		}
	} else if pt == 0 || pt == 8 {
		// G.711 PCMU (0) / PCMA (8)
		codecName := "pcma"
		if pt == 0 {
			codecName = "pcmu"
		}
		var rawAud bytes.Buffer
		var frames []models.OfflineFrameDetail
		var samples []timedSample
		baseTS := ordered[0].pkt.Timestamp
		for _, r := range ordered {
			off := int64(rawAud.Len())
			rawAud.Write(r.pkt.Payload)
			dtsMs := int64(r.pkt.Timestamp-baseTS) * 1000 / 8000
			idx := len(frames)
			frames = append(frames, models.OfflineFrameDetail{
				MediaType:    "audio",
				DTS:          dtsMs,
				PTS:          dtsMs,
				FrameLen:     len(r.pkt.Payload),
				FrameType:    "-",
				StreamSource: "audio",
				StreamOffset: off,
				StreamLen:    len(r.pkt.Payload),
			})
			samples = append(samples, timedSample{
				frameIndex:      idx,
				media:           "audio",
				dts:             dtsMs,
				pts:             dtsMs,
				size:            len(r.pkt.Payload),
				audioSampleRate: 8000,
				audioChannels:   1,
			})
		}
		rawAudPath := filepath.Join(outDir, "audio."+codecName)
		_ = os.WriteFile(rawAudPath, rawAud.Bytes(), 0o644)
		var artifacts []string
		wavPath := filepath.Join(outDir, "audio.wav")
		if wf, err := os.Create(wavPath); err == nil {
			if codec.WriteG711WAV(wf, rawAud.Bytes(), codecName) == nil {
				artifacts = append(artifacts, wavPath)
			}
			_ = wf.Close()
		}
		flow = models.OfflineFlowResult{
			AudioPath:     rawAudPath,
			ArtifactPaths: artifacts,
			AudioCodec:    codecName,
		}
		attachMediaProfileWithSamples(&flow, frames, samples, models.OfflineMediaProfile{
			AudioCodec:      codecName,
			AudioSampleRate: 8000,
			AudioChannels:   1,
		}, healthContext{Mode: models.OfflineModePCAP})
	} else {
		// Dynamic PT (96..127): check MPEG-PS first (GB28181 RTP starts with 0x000001BA)
		isPS := false
		for i := 0; i < len(ordered) && i < 10; i++ {
			if bytes.HasPrefix(ordered[i].pkt.Payload, []byte{0x00, 0x00, 0x01, 0xBA}) {
				isPS = true
				break
			}
		}
		if isPS {
			stats.PayloadName = "MPEG-PS"
			var psBuf bytes.Buffer
			for _, r := range ordered {
				psBuf.Write(r.pkt.Payload)
			}
			psPath := filepath.Join(outDir, "rtp_stream.ps")
			_ = os.WriteFile(psPath, psBuf.Bytes(), 0o644)
			if parsed, err := m.parsePSBytes(ctx, "", psBuf.Bytes(), psPath, outDir, flowID, "rtp-ps"); err == nil && parsed != nil {
				flow = *parsed
			}
		} else {
			// Try RFC 6184 H.264 / RFC 7798 H.265 depacketization
			depackFlow := depacketizeRTPVideo(ordered, outDir, &stats)
			flow = depackFlow
		}
	}

	flow.FlowID = flowID
	flow.Protocol = "rtp"
	if flow.Direction == "" {
		flow.Direction = "rtp"
	}
	flow.DumpRawDir = fmt.Sprintf("ssrc_%08x", acc.key.ssrc)
	flow.ServerIP = acc.key.srcIP
	flow.ServerPort = acc.key.srcPort
	flow.ClientIP = acc.key.dstIP
	flow.ClientPort = acc.key.dstPort
	flow.TCPPktCount = stats.Packets
	flow.PayloadBytes = int(stats.Bytes)
	flow.RTPStats = &stats

	// Inject RTP loss/jitter warning into media profile
	if flow.Profile == nil {
		flow.Profile = &models.OfflineMediaProfile{}
	}
	if stats.Lost > 0 {
		flow.Profile.Warnings = append(flow.Profile.Warnings,
			fmt.Sprintf("RTP SSRC 0x%08X 检测到 %d 个丢包（丢包率 %.2f%%）", stats.SSRC, stats.Lost, stats.LossRatePct))
	}
	if stats.OutOfOrder > 0 {
		flow.Profile.Warnings = append(flow.Profile.Warnings,
			fmt.Sprintf("RTP SSRC 0x%08X 检测到 %d 个乱序包", stats.SSRC, stats.OutOfOrder))
	}
	if stats.MaxJitterMS >= 30 {
		flow.Profile.Warnings = append(flow.Profile.Warnings,
			fmt.Sprintf("RTP SSRC 0x%08X 峰值 Jitter %.2f ms 偏高", stats.SSRC, stats.MaxJitterMS))
	}
	return flow
}

func computeRTPStats(acc *rtpFlowAccum) models.OfflineRTPStats {
	first := acc.packets[0]
	pt := first.pkt.PayloadType
	clockRate := uint32(90000)
	payloadName := fmt.Sprintf("Dynamic PT %d", pt)
	switch pt {
	case 0:
		clockRate = 8000
		payloadName = "PCMU (G.711u)"
	case 8:
		clockRate = 8000
		payloadName = "PCMA (G.711a)"
	case 33:
		clockRate = 90000
		payloadName = "MPEG-TS"
	}

	seen := map[uint16]bool{}
	maxSeq := first.pkt.SequenceNumber
	minSeq := first.pkt.SequenceNumber
	var (
		cycles      uint32
		outOfOrder  int
		duplicates  int
		markerCount int
		totalBytes  int64
		jitter      float64 // in clock ticks
		maxJitter   float64
		prevTransit float64
		hasPrev     bool
	)

	for i, r := range acc.packets {
		totalBytes += int64(r.rawSize)
		if r.pkt.Marker {
			markerCount++
		}
		seq := r.pkt.SequenceNumber
		if i > 0 {
			if seen[seq] {
				duplicates++
			} else {
				diff := int16(seq - maxSeq)
				if diff < 0 {
					outOfOrder++
				} else {
					if seq < maxSeq {
						cycles += 1 << 16
					}
					maxSeq = seq
				}
			}
		}
		seen[seq] = true

		// RFC 3550 interarrival jitter
		if !r.ts.IsZero() {
			arrivalTicks := float64(r.ts.UnixNano()) * float64(clockRate) / 1e9
			transit := arrivalTicks - float64(r.pkt.Timestamp)
			if hasPrev {
				d := math.Abs(transit - prevTransit)
				jitter += (d - jitter) / 16.0
				if jitter > maxJitter {
					maxJitter = jitter
				}
			}
			prevTransit = transit
			hasPrev = true
		}
	}

	extendedMax := cycles + uint32(maxSeq)
	expected := int(extendedMax-uint32(minSeq)) + 1
	if expected < len(acc.packets)-duplicates {
		expected = len(acc.packets) - duplicates
	}
	uniqueRecv := len(acc.packets) - duplicates
	lost := expected - uniqueRecv
	if lost < 0 {
		lost = 0
	}
	lossPct := 0.0
	if expected > 0 {
		lossPct = float64(lost) * 100.0 / float64(expected)
	}

	jitterMS := jitter * 1000.0 / float64(clockRate)
	maxJitterMS := maxJitter * 1000.0 / float64(clockRate)

	return models.OfflineRTPStats{
		SSRC:           acc.key.ssrc,
		PayloadType:    pt,
		PayloadName:    payloadName,
		ClockRate:      clockRate,
		Packets:        len(acc.packets),
		Expected:       expected,
		Lost:           lost,
		LossRatePct:    math.Round(lossPct*100) / 100,
		OutOfOrder:     outOfOrder,
		Duplicates:     duplicates,
		JitterMS:       math.Round(jitterMS*100) / 100,
		MaxJitterMS:    math.Round(maxJitterMS*100) / 100,
		Bytes:          totalBytes,
		FirstSeq:       minSeq,
		LastSeq:        maxSeq,
		MarkerCount:    markerCount,
		RTCPCount:      acc.rtcpCount,
		RTCPSenderPkts: acc.senderPkts,
		RTCPFraction:   math.Round(acc.fracLost*100) / 100,
	}
}

func depacketizeRTPVideo(ordered []rtpPacketRec, outDir string, stats *models.OfflineRTPStats) models.OfflineFlowResult {
	// Detect H.264 vs H.265 heuristically from NAL types.
	isHEVC := false
	for _, r := range ordered {
		p := r.pkt.Payload
		if len(p) >= 3 && (p[0]&0x80) == 0 {
			hevcType := (p[0] >> 1) & 0x3F
			if hevcType == 32 || hevcType == 33 || hevcType == 34 || hevcType == 49 {
				isHEVC = true
				break
			}
			h264Type := p[0] & 0x1F
			if h264Type == 7 || h264Type == 8 || h264Type == 28 || h264Type == 24 {
				isHEVC = false
				break
			}
		}
	}

	videoCodec := "h264"
	stats.PayloadName = "H.264 (RFC 6184)"
	if isHEVC {
		videoCodec = "h265"
		stats.PayloadName = "H.265 (RFC 7798)"
	}

	videoPath := filepath.Join(outDir, "video.annexb")
	vf, _ := os.Create(videoPath)
	defer func() {
		if vf != nil {
			_ = vf.Close()
		}
	}()

	var (
		fuBuf        bytes.Buffer
		inFU         bool
		auBuf        bytes.Buffer
		auTS         uint32
		hasAU        bool
		baseTS       = ordered[0].pkt.Timestamp
		videoPos     int64
		frames       []models.OfflineFrameDetail
		samples      []timedSample
		naluHist     = map[string]int{}
		naluCount    int
		videoMeta    *codec.VideoMeta
	)

	flushAU := func() {
		if !hasAU || auBuf.Len() == 0 {
			auBuf.Reset()
			hasAU = false
			return
		}
		au := auBuf.Bytes()
		off := videoPos
		if vf != nil {
			_, _ = vf.Write(au)
		}
		videoPos += int64(len(au))

		nalus := splitAnnexBNALUs(au)
		ft := "P"
		isKey := false
		for _, n := range nalus {
			if len(n) == 0 {
				continue
			}
			naluCount++
			if isHEVC {
				t := int(h265NALUType(n))
				naluHist[codec.NALUTypeName(t, true)]++
				if t == 33 && videoMeta == nil {
					videoMeta = codec.VideoMetaFromNAL(n)
				}
				if isH265KeyNALUType(uint8(t)) {
					isKey = true
					ft = "I"
				}
			} else {
				t := int(h264NALUType(n))
				naluHist[codec.NALUTypeName(t, false)]++
				if t == 7 && videoMeta == nil {
					if m, err := codec.ParseH264SPS(n); err == nil {
						videoMeta = m
					}
				}
				if t == 5 {
					isKey = true
					ft = "I"
				}
			}
		}

		dtsMs := int64(auTS-baseTS) * 1000 / 90000
		idx := len(frames)
		frames = append(frames, models.OfflineFrameDetail{
			MediaType:    "video",
			DTS:          dtsMs,
			PTS:          dtsMs,
			FrameLen:     len(au),
			FrameType:    ft,
			StreamSource: "video",
			StreamOffset: off,
			StreamLen:    len(au),
		})
		samples = append(samples, timedSample{
			frameIndex: idx,
			media:      "video",
			dts:        dtsMs,
			pts:        dtsMs,
			size:       len(au),
			key:        isKey,
		})
		auBuf.Reset()
		hasAU = false
	}

	writeNAL := func(nal []byte) {
		if len(nal) == 0 {
			return
		}
		auBuf.Write([]byte{0x00, 0x00, 0x00, 0x01})
		auBuf.Write(nal)
		hasAU = true
	}

	for _, r := range ordered {
		p := r.pkt.Payload
		if len(p) < 2 {
			continue
		}
		if hasAU && r.pkt.Timestamp != auTS {
			flushAU()
		}
		auTS = r.pkt.Timestamp

		if !isHEVC {
			nalType := p[0] & 0x1F
			switch {
			case nalType >= 1 && nalType <= 23:
				writeNAL(p)
			case nalType == 24: // STAP-A
				for off := 1; off+2 <= len(p); {
					sz := int(binary.BigEndian.Uint16(p[off : off+2]))
					off += 2
					if sz <= 0 || off+sz > len(p) {
						break
					}
					writeNAL(p[off : off+sz])
					off += sz
				}
			case nalType == 28: // FU-A
				fuHdr := p[1]
				start := (fuHdr & 0x80) != 0
				end := (fuHdr & 0x40) != 0
				origHdr := (p[0] & 0xE0) | (fuHdr & 0x1F)
				if start {
					fuBuf.Reset()
					fuBuf.WriteByte(origHdr)
					fuBuf.Write(p[2:])
					inFU = true
				} else if inFU {
					fuBuf.Write(p[2:])
				}
				if end && inFU {
					writeNAL(fuBuf.Bytes())
					fuBuf.Reset()
					inFU = false
				}
			}
		} else {
			nalType := (p[0] >> 1) & 0x3F
			switch {
			case nalType < 48:
				writeNAL(p)
			case nalType == 48: // AP
				for off := 2; off+2 <= len(p); {
					sz := int(binary.BigEndian.Uint16(p[off : off+2]))
					off += 2
					if sz <= 0 || off+sz > len(p) {
						break
					}
					writeNAL(p[off : off+sz])
					off += sz
				}
			case nalType == 49 && len(p) >= 3: // FU
				fuHdr := p[2]
				start := (fuHdr & 0x80) != 0
				end := (fuHdr & 0x40) != 0
				fuType := fuHdr & 0x3F
				h0 := (p[0] & 0x81) | (fuType << 1)
				h1 := p[1]
				if start {
					fuBuf.Reset()
					fuBuf.WriteByte(h0)
					fuBuf.WriteByte(h1)
					fuBuf.Write(p[3:])
					inFU = true
				} else if inFU {
					fuBuf.Write(p[3:])
				}
				if end && inFU {
					writeNAL(fuBuf.Bytes())
					fuBuf.Reset()
					inFU = false
				}
			}
		}

		if r.pkt.Marker {
			flushAU()
		}
	}
	flushAU()
	if vf != nil {
		_ = vf.Close()
		vf = nil
	}

	flow := models.OfflineFlowResult{
		VideoPath:  videoPath,
		VideoCodec: videoCodec,
		NALUCount:  naluCount,
	}
	cleanupEmptyFile(&flow.VideoPath)
	seed := models.OfflineMediaProfile{
		VideoCodec:    videoCodec,
		NALUHistogram: naluHist,
	}
	if videoMeta != nil {
		seed.Width = videoMeta.Width
		seed.Height = videoMeta.Height
		seed.Profile = videoMeta.ProfileName
		seed.Level = videoMeta.Level
		if isHEVC {
			seed.LevelName = codec.H265LevelName(videoMeta.Level)
		} else {
			seed.LevelName = codec.H264LevelName(videoMeta.Level)
		}
		seed.FrameRate = videoMeta.FrameRate
	}
	attachMediaProfileWithSamples(&flow, frames, samples, seed, healthContext{
		Mode:   models.OfflineModePCAP,
		IsHEVC: isHEVC,
	})
	return flow
}

func reassembleTCPSegments(segs []tcpSeg) []byte {
	if len(segs) == 0 {
		return nil
	}
	sort.SliceStable(segs, func(i, j int) bool {
		return int32(segs[i].seq-segs[j].seq) < 0
	})
	var out bytes.Buffer
	nextSeq := segs[0].seq
	for i, s := range segs {
		if i == 0 {
			out.Write(s.payload)
			nextSeq = s.seq + uint32(len(s.payload))
			continue
		}
		diff := int32(s.seq - nextSeq)
		if diff >= 0 {
			out.Write(s.payload)
			nextSeq = s.seq + uint32(len(s.payload))
		} else {
			overlap := int(-diff)
			if overlap < len(s.payload) {
				out.Write(s.payload[overlap:])
				nextSeq = s.seq + uint32(len(s.payload))
			}
		}
	}
	return out.Bytes()
}

func decodeHTTPChunked(body []byte) []byte {
	var out bytes.Buffer
	pos := 0
	for pos < len(body) {
		nl := bytes.Index(body[pos:], []byte("\r\n"))
		if nl < 0 {
			break
		}
		sizeLine := strings.TrimSpace(string(body[pos : pos+nl]))
		if semi := strings.IndexByte(sizeLine, ';'); semi >= 0 {
			sizeLine = strings.TrimSpace(sizeLine[:semi])
		}
		var chunkSize int
		if _, err := fmt.Sscanf(sizeLine, "%x", &chunkSize); err != nil || chunkSize <= 0 {
			break
		}
		pos += nl + 2
		if pos+chunkSize > len(body) {
			out.Write(body[pos:])
			break
		}
		out.Write(body[pos : pos+chunkSize])
		pos += chunkSize
		if pos+2 <= len(body) && body[pos] == '\r' && body[pos+1] == '\n' {
			pos += 2
		}
	}
	if out.Len() == 0 {
		return body
	}
	return out.Bytes()
}
