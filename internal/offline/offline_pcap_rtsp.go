package offline

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"streamanalyzer/internal/models"
)

type rtspParsedStream struct {
	isRequest  bool
	isPublish  bool
	signaling  []string
	sdp        string
	rtpBySSRC  map[uint32]*rtpFlowAccum
	rtcpBySSRC map[uint32]struct {
		count      int
		senderPkts uint32
		fracLost   float64
	}
}

func looksLikeRTSPStream(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	if bytes.HasPrefix(data, []byte("RTSP/1.0 ")) {
		return true
	}
	for _, method := range [][]byte{
		[]byte("OPTIONS "),
		[]byte("DESCRIBE "),
		[]byte("ANNOUNCE "),
		[]byte("SETUP "),
		[]byte("PLAY "),
		[]byte("RECORD "),
		[]byte("PAUSE "),
		[]byte("TEARDOWN "),
		[]byte("GET_PARAMETER "),
		[]byte("SET_PARAMETER "),
	} {
		if bytes.HasPrefix(data, method) {
			lineEnd := bytes.Index(data[:min(len(data), 512)], []byte("\r\n"))
			if lineEnd > 0 && bytes.Contains(data[:lineEnd], []byte("RTSP/1.0")) {
				return true
			}
		}
	}
	return false
}

// parseRTSPTCPStream walks a reassembled directional RTSP-over-TCP byte stream,
// extracting both RTSP text signaling/SDP messages and RFC 2326 interleaved
// binary frames ('$' + 1B channel + 2B length + RTP/RTCP payload).
func parseRTSPTCPStream(tk tcpStreamKey, data []byte, baseTS time.Time) *rtspParsedStream {
	if !looksLikeRTSPStream(data) {
		return nil
	}
	out := &rtspParsedStream{
		isRequest: !bytes.HasPrefix(data, []byte("RTSP/1.0 ")),
		isPublish: bytes.HasPrefix(data, []byte("ANNOUNCE ")),
		rtpBySSRC: map[uint32]*rtpFlowAccum{},
		rtcpBySSRC: map[uint32]struct {
			count      int
			senderPkts uint32
			fracLost   float64
		}{},
	}

	pos := 0
	for pos < len(data) {
		// 1. Interleaved binary frame: '$' (0x24) <channel> <len_hi> <len_lo> <payload>
		if data[pos] == '$' {
			if pos+4 > len(data) {
				break
			}
			frameLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
			if pos+4+frameLen > len(data) {
				break
			}
			payload := data[pos+4 : pos+4+frameLen]
			pos += 4 + frameLen
			if len(payload) < 8 || (payload[0]>>6) != 2 {
				continue
			}
			// RTCP (PT 200..204)
			if payload[1] >= 200 && payload[1] <= 204 {
				if pkts, err := rtcp.Unmarshal(payload); err == nil {
					for _, cp := range pkts {
						switch rp := cp.(type) {
						case *rtcp.SenderReport:
							e := out.rtcpBySSRC[rp.SSRC]
							e.count++
							e.senderPkts = rp.PacketCount
							out.rtcpBySSRC[rp.SSRC] = e
						case *rtcp.ReceiverReport:
							for _, rep := range rp.Reports {
								e := out.rtcpBySSRC[rep.SSRC]
								e.count++
								e.fracLost = float64(rep.FractionLost) * 100.0 / 256.0
								out.rtcpBySSRC[rep.SSRC] = e
							}
						}
					}
				}
				continue
			}
			// RTP
			if len(payload) >= 12 {
				var rp rtp.Packet
				if err := rp.Unmarshal(payload); err == nil && len(rp.Payload) > 0 {
					acc := out.rtpBySSRC[rp.SSRC]
					if acc == nil {
						acc = &rtpFlowAccum{
							key: rtpFlowKey{
								srcIP:   tk.srcIP,
								srcPort: tk.srcPort,
								dstIP:   tk.dstIP,
								dstPort: tk.dstPort,
								ssrc:    rp.SSRC,
							},
						}
						out.rtpBySSRC[rp.SSRC] = acc
					}
					acc.packets = append(acc.packets, rtpPacketRec{
						ts:      baseTS,
						pkt:     rp,
						rawSize: len(payload),
					})
				}
			}
			continue
		}

		// 2. RTSP text message
		hdrEnd := bytes.Index(data[pos:], []byte("\r\n\r\n"))
		if hdrEnd < 0 {
			// Resync to next '$' if trailing text is truncated
			nextDollar := bytes.IndexByte(data[pos:], '$')
			if nextDollar > 0 {
				pos += nextDollar
				continue
			}
			break
		}
		hdrBytes := data[pos : pos+hdrEnd]
		pos += hdrEnd + 4

		lines := strings.Split(string(hdrBytes), "\r\n")
		if len(lines) == 0 {
			continue
		}
		startLine := strings.TrimSpace(lines[0])
		if strings.HasPrefix(startLine, "ANNOUNCE ") {
			out.isPublish = true
		}

		contentLen := 0
		contentType := ""
		var summaryLines []string
		summaryLines = append(summaryLines, startLine)
		for _, line := range lines[1:] {
			lower := strings.ToLower(line)
			if strings.HasPrefix(lower, "content-length:") {
				if n, err := strconv.Atoi(strings.TrimSpace(line[len("content-length:"):])); err == nil && n > 0 {
					contentLen = n
				}
			}
			if strings.HasPrefix(lower, "content-type:") {
				contentType = strings.ToLower(strings.TrimSpace(line[len("content-type:"):]))
			}
			// Keep key non-secret headers in signaling transcript
			if strings.HasPrefix(lower, "cseq:") ||
				strings.HasPrefix(lower, "transport:") ||
				strings.HasPrefix(lower, "session:") ||
				strings.HasPrefix(lower, "content-type:") ||
				strings.HasPrefix(lower, "rtp-info:") ||
				strings.HasPrefix(lower, "location:") {
				summaryLines = append(summaryLines, strings.TrimSpace(line))
			}
		}

		var body []byte
		if contentLen > 0 {
			if pos+contentLen > len(data) {
				body = data[pos:]
				pos = len(data)
			} else {
				body = data[pos : pos+contentLen]
				pos += contentLen
			}
		}

		if len(out.signaling) < 64 {
			entry := strings.Join(summaryLines, "\n")
			if len(body) > 0 && strings.Contains(contentType, "application/sdp") {
				sdpText := strings.TrimSpace(string(body))
				if out.sdp == "" {
					out.sdp = sdpText
				}
				entry += "\n\n" + sdpText
			}
			out.signaling = append(out.signaling, entry)
		}
	}

	if len(out.signaling) == 0 && len(out.rtpBySSRC) == 0 {
		return nil
	}
	return out
}

func (m *Manager) extractRTSPTCPFlows(ctx context.Context, taskID string, tcpSegs map[tcpStreamKey][]tcpSeg, taskDir string, nextFlowID int) ([]models.OfflineFlowResult, int) {
	parsed := map[tcpStreamKey]*rtspParsedStream{}
	for tk, segs := range tcpSegs {
		streamBytes := reassembleTCPSegments(segs)
		if len(streamBytes) < 16 {
			continue
		}
		var baseTS time.Time
		if len(segs) > 0 {
			baseTS = segs[0].ts
		}
		if p := parseRTSPTCPStream(tk, streamBytes, baseTS); p != nil {
			parsed[tk] = p
		}
	}
	if len(parsed) == 0 {
		return nil, nextFlowID
	}

	keys := make([]tcpStreamKey, 0, len(parsed))
	for k := range parsed {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].srcIP != keys[j].srcIP {
			return keys[i].srcIP < keys[j].srcIP
		}
		if keys[i].srcPort != keys[j].srcPort {
			return keys[i].srcPort < keys[j].srcPort
		}
		return keys[i].dstPort < keys[j].dstPort
	})

	var results []models.OfflineFlowResult
	for _, tk := range keys {
		p := parsed[tk]
		if len(p.rtpBySSRC) == 0 {
			continue
		}
		// Collect signaling from both directions of this TCP 4-tuple
		peerKey := tcpStreamKey{srcIP: tk.dstIP, srcPort: tk.dstPort, dstIP: tk.srcIP, dstPort: tk.srcPort}
		peer := parsed[peerKey]

		var sigBuf strings.Builder
		fmt.Fprintf(&sigBuf, "# RTSP-over-TCP Signaling (%s:%d <-> %s:%d)\n\n", tk.srcIP, tk.srcPort, tk.dstIP, tk.dstPort)
		if peer != nil && len(peer.signaling) > 0 {
			sigBuf.WriteString("## Peer Direction\n")
			for _, msg := range peer.signaling {
				sigBuf.WriteString(msg)
				sigBuf.WriteString("\n\n")
			}
		}
		if len(p.signaling) > 0 {
			sigBuf.WriteString("## Media Direction\n")
			for _, msg := range p.signaling {
				sigBuf.WriteString(msg)
				sigBuf.WriteString("\n\n")
			}
		}

		ssrcs := make([]uint32, 0, len(p.rtpBySSRC))
		for ssrc, acc := range p.rtpBySSRC {
			if len(acc.packets) >= 3 {
				ssrcs = append(ssrcs, ssrc)
			}
		}
		sort.Slice(ssrcs, func(i, j int) bool { return ssrcs[i] < ssrcs[j] })

		for _, ssrc := range ssrcs {
			acc := p.rtpBySSRC[ssrc]
			if rinfo, ok := p.rtcpBySSRC[ssrc]; ok {
				acc.rtcpCount = rinfo.count
				acc.senderPkts = rinfo.senderPkts
				acc.fracLost = rinfo.fracLost
			} else if peer != nil {
				if rinfo, ok := peer.rtcpBySSRC[ssrc]; ok {
					acc.rtcpCount = rinfo.count
					acc.senderPkts = rinfo.senderPkts
					acc.fracLost = rinfo.fracLost
				}
			}

			flow := m.buildRTPFlowResult(ctx, taskID, acc, taskDir, nextFlowID)
			flow.Protocol = "rtsp-tcp"
			if p.isPublish || (peer != nil && peer.isPublish) {
				flow.Direction = "rtsp-publish"
			} else {
				flow.Direction = "rtsp-interleaved"
			}

			// Persist RTSP signaling + SDP transcript alongside the flow artifacts
			sigDir := filepath.Join(taskDir, fmt.Sprintf("pcap_rtp_%03d", nextFlowID))
			_ = os.MkdirAll(sigDir, 0o755)
			sigPath := filepath.Join(sigDir, "rtsp_signaling.txt")
			if os.WriteFile(sigPath, []byte(sigBuf.String()), 0o644) == nil {
				flow.ArtifactPaths = append(flow.ArtifactPaths, sigPath)
			}

			results = append(results, flow)
			nextFlowID++
		}
	}

	return results, nextFlowID
}
