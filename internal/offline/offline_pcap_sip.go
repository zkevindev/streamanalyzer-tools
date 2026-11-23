package offline

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"streamanalyzer/internal/models"
)

type sipDatagram struct {
	srcIP     string
	srcPort   uint16
	dstIP     string
	dstPort   uint16
	transport string // "udp" or "tcp"
	ts        time.Time
	payload   []byte
}

type sipPeerKey struct {
	transport string
	epA       string
	epB       string
}

func makeSIPPeerKey(transport, srcIP string, srcPort uint16, dstIP string, dstPort uint16) sipPeerKey {
	a := fmt.Sprintf("%s:%d", srcIP, srcPort)
	b := fmt.Sprintf("%s:%d", dstIP, dstPort)
	if a > b {
		a, b = b, a
	}
	return sipPeerKey{transport: transport, epA: a, epB: b}
}

func looksLikeSIPPayload(data []byte) bool {
	if len(data) < 16 {
		return false
	}
	if bytes.HasPrefix(data, []byte("SIP/2.0 ")) {
		return true
	}
	for _, method := range [][]byte{
		[]byte("INVITE "),
		[]byte("ACK "),
		[]byte("BYE "),
		[]byte("CANCEL "),
		[]byte("REGISTER "),
		[]byte("OPTIONS "),
		[]byte("MESSAGE "),
		[]byte("NOTIFY "),
		[]byte("SUBSCRIBE "),
		[]byte("INFO "),
		[]byte("PRACK "),
		[]byte("UPDATE "),
	} {
		if bytes.HasPrefix(data, method) {
			lineEnd := bytes.Index(data[:min(len(data), 512)], []byte("\r\n"))
			if lineEnd < 0 {
				lineEnd = bytes.IndexByte(data[:min(len(data), 512)], '\n')
			}
			if lineEnd > 0 && bytes.Contains(data[:lineEnd], []byte("SIP/2.0")) {
				return true
			}
		}
	}
	return false
}

type parsedSIPMsg struct {
	src       string
	dst       string
	ts        time.Time
	startLine string
	callID    string
	summary   string
	isGB28181 bool
	hasSDP    bool
	rawSize   int
}

func parseSIPMessage(raw []byte) (parsedSIPMsg, bool) {
	hdrEnd := bytes.Index(raw, []byte("\r\n\r\n"))
	sepLen := 4
	if hdrEnd < 0 {
		hdrEnd = bytes.Index(raw, []byte("\n\n"))
		sepLen = 2
	}
	if hdrEnd <= 0 {
		return parsedSIPMsg{}, false
	}
	hdrText := strings.ReplaceAll(string(raw[:hdrEnd]), "\r\n", "\n")
	lines := strings.Split(hdrText, "\n")
	if len(lines) == 0 {
		return parsedSIPMsg{}, false
	}
	startLine := strings.TrimSpace(lines[0])
	if !strings.Contains(startLine, "SIP/2.0") {
		return parsedSIPMsg{}, false
	}

	var kept []string
	kept = append(kept, startLine)
	contentType := ""
	contentLen := -1
	callID := ""
	isGB := false

	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "content-type:") || strings.HasPrefix(lower, "c:") {
			idx := strings.IndexByte(trimmed, ':')
			contentType = strings.ToLower(strings.TrimSpace(trimmed[idx+1:]))
			if strings.Contains(contentType, "manscdp") {
				isGB = true
			}
		}
		if strings.HasPrefix(lower, "content-length:") || strings.HasPrefix(lower, "l:") {
			idx := strings.IndexByte(trimmed, ':')
			if n, err := strconv.Atoi(strings.TrimSpace(trimmed[idx+1:])); err == nil && n >= 0 {
				contentLen = n
			}
		}
		if strings.HasPrefix(lower, "call-id:") || strings.HasPrefix(lower, "i:") {
			idx := strings.IndexByte(trimmed, ':')
			callID = strings.TrimSpace(trimmed[idx+1:])
		}
		if strings.HasPrefix(lower, "subject:") {
			isGB = true
		}
		// Keep key non-secret SIP headers (exclude Authorization/Proxy-Authorization).
		if strings.HasPrefix(lower, "via:") ||
			strings.HasPrefix(lower, "from:") || strings.HasPrefix(lower, "f:") ||
			strings.HasPrefix(lower, "to:") || strings.HasPrefix(lower, "t:") ||
			strings.HasPrefix(lower, "call-id:") || strings.HasPrefix(lower, "i:") ||
			strings.HasPrefix(lower, "cseq:") ||
			strings.HasPrefix(lower, "contact:") ||
			strings.HasPrefix(lower, "subject:") ||
			strings.HasPrefix(lower, "expires:") ||
			strings.HasPrefix(lower, "user-agent:") ||
			strings.HasPrefix(lower, "content-type:") || strings.HasPrefix(lower, "c:") {
			kept = append(kept, trimmed)
		}
	}

	bodyBytes := raw[hdrEnd+sepLen:]
	if contentLen >= 0 && contentLen < len(bodyBytes) {
		bodyBytes = bodyBytes[:contentLen]
	}
	bodyText := strings.TrimSpace(string(bodyBytes))
	hasSDP := strings.Contains(contentType, "application/sdp") || (strings.HasPrefix(bodyText, "v=0") && strings.Contains(bodyText, "m="))
	if hasSDP && (strings.Contains(bodyText, "\ny=") || strings.Contains(bodyText, "a=setup:") || strings.Contains(bodyText, "PS/90000")) {
		isGB = true
	}
	if strings.Contains(bodyText, "<CmdType>") || strings.Contains(bodyText, "<DeviceID>") {
		isGB = true
	}

	entry := strings.Join(kept, "\n")
	if len(bodyText) > 0 && (hasSDP || strings.Contains(contentType, "xml") || strings.Contains(contentType, "text/")) {
		if len(bodyText) > 4096 {
			bodyText = bodyText[:4096] + "\n... (truncated)"
		}
		entry += "\n\n" + bodyText
	}

	return parsedSIPMsg{
		startLine: startLine,
		callID:    callID,
		summary:   entry,
		isGB28181: isGB,
		hasSDP:    hasSDP,
		rawSize:   len(raw),
	}, true
}

func splitTCPSIPMessages(data []byte) [][]byte {
	if !looksLikeSIPPayload(data) {
		return nil
	}
	var out [][]byte
	pos := 0
	for pos < len(data) && len(out) < 128 {
		for pos < len(data) && (data[pos] == '\r' || data[pos] == '\n' || data[pos] == ' ') {
			pos++
		}
		if pos >= len(data) || !looksLikeSIPPayload(data[pos:]) {
			break
		}
		hdrEnd := bytes.Index(data[pos:], []byte("\r\n\r\n"))
		sepLen := 4
		if hdrEnd < 0 {
			hdrEnd = bytes.Index(data[pos:], []byte("\n\n"))
			sepLen = 2
		}
		if hdrEnd < 0 {
			break
		}
		hdrBytes := data[pos : pos+hdrEnd]
		contentLen := 0
		for _, line := range bytes.Split(hdrBytes, []byte("\n")) {
			lower := strings.ToLower(strings.TrimSpace(string(line)))
			if strings.HasPrefix(lower, "content-length:") {
				if n, err := strconv.Atoi(strings.TrimSpace(lower[len("content-length:"):])); err == nil && n > 0 {
					contentLen = n
				}
			} else if strings.HasPrefix(lower, "l:") {
				if n, err := strconv.Atoi(strings.TrimSpace(lower[2:])); err == nil && n > 0 {
					contentLen = n
				}
			}
		}
		msgEnd := pos + hdrEnd + sepLen + contentLen
		if msgEnd > len(data) {
			msgEnd = len(data)
		}
		out = append(out, data[pos:msgEnd])
		pos = msgEnd
	}
	return out
}

func (m *Manager) extractPCAPSIPFlows(udpSIP []sipDatagram, tcpSegs map[tcpStreamKey][]tcpSeg, taskDir string, nextFlowID int) ([]models.OfflineFlowResult, int) {
	byPeer := map[sipPeerKey][]parsedSIPMsg{}

	for _, dg := range udpSIP {
		msg, ok := parseSIPMessage(dg.payload)
		if !ok {
			continue
		}
		msg.src = fmt.Sprintf("%s:%d", dg.srcIP, dg.srcPort)
		msg.dst = fmt.Sprintf("%s:%d", dg.dstIP, dg.dstPort)
		msg.ts = dg.ts
		key := makeSIPPeerKey("udp", dg.srcIP, dg.srcPort, dg.dstIP, dg.dstPort)
		byPeer[key] = append(byPeer[key], msg)
	}

	for tk, segs := range tcpSegs {
		streamBytes := reassembleTCPSegments(segs)
		if len(streamBytes) < 16 || !looksLikeSIPPayload(streamBytes) {
			continue
		}
		var baseTS time.Time
		if len(segs) > 0 {
			baseTS = segs[0].ts
		}
		for _, rawMsg := range splitTCPSIPMessages(streamBytes) {
			msg, ok := parseSIPMessage(rawMsg)
			if !ok {
				continue
			}
			msg.src = fmt.Sprintf("%s:%d", tk.srcIP, tk.srcPort)
			msg.dst = fmt.Sprintf("%s:%d", tk.dstIP, tk.dstPort)
			msg.ts = baseTS
			key := makeSIPPeerKey("tcp", tk.srcIP, tk.srcPort, tk.dstIP, tk.dstPort)
			byPeer[key] = append(byPeer[key], msg)
		}
	}

	if len(byPeer) == 0 {
		return nil, nextFlowID
	}

	keys := make([]sipPeerKey, 0, len(byPeer))
	for k := range byPeer {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].transport != keys[j].transport {
			return keys[i].transport < keys[j].transport
		}
		if keys[i].epA != keys[j].epA {
			return keys[i].epA < keys[j].epA
		}
		return keys[i].epB < keys[j].epB
	})

	var results []models.OfflineFlowResult
	for _, key := range keys {
		msgs := byPeer[key]
		if len(msgs) == 0 {
			continue
		}
		sort.SliceStable(msgs, func(i, j int) bool {
			return msgs[i].ts.Before(msgs[j].ts)
		})

		isGB := false
		sdpCount := 0
		totalBytes := 0
		callIDs := map[string]bool{}
		for _, msg := range msgs {
			if msg.isGB28181 {
				isGB = true
			}
			if msg.hasSDP {
				sdpCount++
			}
			totalBytes += msg.rawSize
			if msg.callID != "" {
				callIDs[msg.callID] = true
			}
		}

		proto := "sip"
		title := "SIP Signaling"
		if isGB {
			proto = "gb28181-sip"
			title = "GB28181 / SIP Signaling"
		}

		var buf strings.Builder
		fmt.Fprintf(&buf, "# %s (%s · %s <-> %s)\n", title, strings.ToUpper(key.transport), key.epA, key.epB)
		fmt.Fprintf(&buf, "# Messages: %d · Call-IDs: %d · SDP Bodies: %d\n\n", len(msgs), len(callIDs), sdpCount)
		limit := min(len(msgs), 128)
		for i := 0; i < limit; i++ {
			msg := msgs[i]
			tsLabel := "-"
			if !msg.ts.IsZero() {
				tsLabel = msg.ts.UTC().Format("15:04:05.000")
			}
			fmt.Fprintf(&buf, "--- #%d [%s] %s -> %s ---\n%s\n\n", i+1, tsLabel, msg.src, msg.dst, msg.summary)
		}
		if len(msgs) > limit {
			fmt.Fprintf(&buf, "... (%d more SIP messages omitted)\n", len(msgs)-limit)
		}

		sigDir := filepath.Join(taskDir, fmt.Sprintf("pcap_sip_%03d", nextFlowID))
		_ = os.MkdirAll(sigDir, 0o755)
		sigPath := filepath.Join(sigDir, "sip_signaling.txt")
		var artifacts []string
		if os.WriteFile(sigPath, []byte(buf.String()), 0o644) == nil {
			artifacts = append(artifacts, sigPath)
		}

		srcHost, srcPort := splitHostPortNum(key.epA)
		dstHost, dstPort := splitHostPortNum(key.epB)
		results = append(results, models.OfflineFlowResult{
			FlowID:        nextFlowID,
			Protocol:      proto,
			Direction:     "sip-" + key.transport,
			ClientIP:      srcHost,
			ClientPort:    srcPort,
			ServerIP:      dstHost,
			ServerPort:    dstPort,
			TCPPktCount:   len(msgs),
			PayloadBytes:  totalBytes,
			ArtifactPaths: artifacts,
		})
		nextFlowID++
	}

	return results, nextFlowID
}

func splitHostPortNum(ep string) (string, uint16) {
	idx := strings.LastIndexByte(ep, ':')
	if idx < 0 {
		return ep, 0
	}
	p, _ := strconv.Atoi(ep[idx+1:])
	return ep[:idx], uint16(p)
}
