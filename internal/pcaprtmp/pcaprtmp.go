package pcaprtmp

import (
	"bytes"
	"encoding/binary"
	"net"
	"sort"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

// Direction indicates which side carries media payload for RTMP.
// - Pull: client plays, media goes server->client.
// - Push: client publishes, media goes client->server.
type Direction string

const (
	DirectionUnknown Direction = ""
	DirectionPull    Direction = "pull"
	DirectionPush    Direction = "push"
)

type FlowMeta struct {
	HasSYN      bool
	SYNCount    int
	TCPPktCount int
	PayloadBytes int

	ServerIP   string
	ServerPort layers.TCPPort
	ClientIP   string
	ClientPort layers.TCPPort
}

type Flow struct {
	Meta FlowMeta

	// Assembled bi-directional TCP payload stream.
	ClientToServer []byte
	ServerToClient []byte
}

// Options controls flow selection and direction detection.
type Options struct {
	ServerPort layers.TCPPort

	// RequireSYN enforces: if a TCP flow doesn't include SYN packets, skip it.
	RequireSYN bool

	// DetectLimitBytes limits scan length for "play/publish" keywords.
	DetectLimitBytes int
}

func DefaultOptions(serverPort layers.TCPPort) Options {
	return Options{
		ServerPort:       serverPort,
		RequireSYN:       true,
		DetectLimitBytes: 64 * 1024,
	}
}

// ExtractFlowsFromPCAP extracts RTMP candidate TCP flows from pcap/pcapng (pcap.OpenOffline),
// and assembles payload streams for both directions.
//
// It is intentionally strict about gaps: if a direction has missing segments, the flow
// will be returned without assembled bytes for that direction.
func ExtractFlowsFromPCAP(pcapPath string, opt Options) ([]Flow, error) {
	selected, err := pass1CollectFlows(pcapPath, opt)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, nil
	}

	segs, err := pass2CollectSegments(pcapPath, opt, selected)
	if err != nil {
		return nil, err
	}

	type keyed struct {
		key  connKey
		meta FlowMeta
	}
	keys := make([]keyed, 0, len(selected))
	for k, m := range selected {
		keys = append(keys, keyed{key: k, meta: m})
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i].meta, keys[j].meta
		if a.ServerIP != b.ServerIP {
			return a.ServerIP < b.ServerIP
		}
		if a.ServerPort != b.ServerPort {
			return a.ServerPort < b.ServerPort
		}
		if a.ClientIP != b.ClientIP {
			return a.ClientIP < b.ClientIP
		}
		return a.ClientPort < b.ClientPort
	})

	out := make([]Flow, 0, len(keys))
	for _, item := range keys {
		fs := segs[item.key]
		cbytes, okC := assembleSegments(fs.clientToServer)
		sbytes, okS := assembleSegments(fs.serverToClient)

		flow := Flow{
			Meta: item.meta,
		}
		if okC {
			flow.ClientToServer = cbytes
		}
		if okS {
			flow.ServerToClient = sbytes
		}
		out = append(out, flow)
	}
	return out, nil
}

// DetectDirection tries to determine push/pull from client->server payload.
// If it can't detect by keywords, it falls back to comparing post-skip sizes.
func DetectDirection(flow Flow, handshakeSkip int, detectLimit int) Direction {
	limit := detectLimit
	if limit <= 0 {
		limit = 64 * 1024
	}
	blob := flow.ClientToServer
	if len(blob) > limit {
		blob = blob[:limit]
	}

	playIdx := bytes.Index(blob, []byte("play"))
	pubIdx := bytes.Index(blob, []byte("publish"))

	switch {
	case pubIdx >= 0 && (playIdx < 0 || pubIdx < playIdx):
		return DirectionPush
	case playIdx >= 0 && pubIdx < 0:
		return DirectionPull
	case playIdx >= 0 && pubIdx >= 0 && playIdx < pubIdx:
		return DirectionPull
	}

	cRem := len(flow.ClientToServer)
	if cRem > handshakeSkip {
		cRem -= handshakeSkip
	} else {
		cRem = 0
	}
	sRem := len(flow.ServerToClient)
	if sRem > handshakeSkip {
		sRem -= handshakeSkip
	} else {
		sRem = 0
	}
	if sRem >= cRem {
		return DirectionPull
	}
	return DirectionPush
}

// --- Internal helpers (adapted from the example tool) ---

type ipPort struct {
	ip   string
	port layers.TCPPort
}

type connKey struct {
	a ipPort
	b ipPort
}

type tcpSeg struct {
	seq  uint32
	data []byte
}

type flowSegs struct {
	clientToServer []tcpSeg
	serverToClient []tcpSeg
}

func makeConnKey(ip1 string, port1 layers.TCPPort, ip2 string, port2 layers.TCPPort) connKey {
	a := ipPort{ip: ip1, port: port1}
	b := ipPort{ip: ip2, port: port2}
	if a.ip < b.ip || (a.ip == b.ip && a.port < b.port) {
		return connKey{a: a, b: b}
	}
	return connKey{a: b, b: a}
}

func pass1CollectFlows(pcapPath string, opt Options) (map[connKey]FlowMeta, error) {
	handle, err := pcap.OpenOffline(pcapPath)
	if err != nil {
		return nil, err
	}
	defer handle.Close()

	source := gopacket.NewPacketSource(handle, handle.LinkType())

	selected := make(map[connKey]FlowMeta)
	for packet := range source.Packets() {
		srcIP, dstIP, srcPort, dstPort, _, syn, payload, ok := parseTCPFromPacketData(packet.Data())
		if !ok {
			continue
		}

		if srcPort != opt.ServerPort && dstPort != opt.ServerPort {
			continue
		}

		// Determine server/client for this packet.
		var serverIPStr, clientIPStr string
		var clientPort layers.TCPPort
		if srcPort == opt.ServerPort {
			serverIPStr = srcIP.String()
			clientIPStr = dstIP.String()
			clientPort = dstPort
		} else {
			serverIPStr = dstIP.String()
			clientIPStr = srcIP.String()
			clientPort = srcPort
		}

		k := makeConnKey(srcIP.String(), srcPort, dstIP.String(), dstPort)
		meta := selected[k]
		if !meta.HasSYN && syn {
			meta.HasSYN = true
		}
		meta.TCPPktCount++
		if syn {
			meta.SYNCount++
		}
		meta.PayloadBytes += len(payload)
		if meta.ServerIP == "" {
			meta.ServerIP = serverIPStr
			meta.ServerPort = opt.ServerPort
			meta.ClientIP = clientIPStr
			meta.ClientPort = clientPort
		}
		selected[k] = meta
	}

	// Filter: require SYN if configured.
	out := make(map[connKey]FlowMeta, len(selected))
	for k, meta := range selected {
		if opt.RequireSYN && !meta.HasSYN {
			continue
		}
		if meta.ServerIP == "" || meta.ClientIP == "" || meta.ClientPort == 0 {
			continue
		}
		out[k] = meta
	}
	return out, nil
}

func pass2CollectSegments(pcapPath string, opt Options, selected map[connKey]FlowMeta) (map[connKey]flowSegs, error) {
	handle, err := pcap.OpenOffline(pcapPath)
	if err != nil {
		return nil, err
	}
	defer handle.Close()

	source := gopacket.NewPacketSource(handle, handle.LinkType())
	segs := make(map[connKey]flowSegs, len(selected))

	for packet := range source.Packets() {
		srcIP, dstIP, srcPort, dstPort, seq, _, payload, ok := parseTCPFromPacketData(packet.Data())
		if !ok {
			continue
		}
		if srcPort != opt.ServerPort && dstPort != opt.ServerPort {
			continue
		}
		if len(payload) == 0 {
			continue
		}

		k := makeConnKey(srcIP.String(), srcPort, dstIP.String(), dstPort)
		meta, ok := selected[k]
		if !ok {
			continue
		}

		// Direction based on endpoints.
		srcIPStr := srcIP.String()
		dstIPStr := dstIP.String()
		switch {
		case srcIPStr == meta.ClientIP && dstIPStr == meta.ServerIP &&
			srcPort == meta.ClientPort && dstPort == meta.ServerPort:
			segs[k] = appendSeg(segs[k], false, seq, payload)
		case srcIPStr == meta.ServerIP && dstIPStr == meta.ClientIP &&
			srcPort == meta.ServerPort && dstPort == meta.ClientPort:
			segs[k] = appendSeg(segs[k], true, seq, payload)
		default:
			// ignore
		}
	}
	return segs, nil
}

func appendSeg(fs flowSegs, serverToClient bool, seq uint32, payload []byte) flowSegs {
	cp := make([]byte, len(payload))
	copy(cp, payload)
	seg := tcpSeg{seq: seq, data: cp}
	if serverToClient {
		fs.serverToClient = append(fs.serverToClient, seg)
	} else {
		fs.clientToServer = append(fs.clientToServer, seg)
	}
	return fs
}

func assembleSegments(segments []tcpSeg) ([]byte, bool) {
	if len(segments) == 0 {
		return nil, false
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].seq < segments[j].seq })

	currentEnd := segments[0].seq
	var out bytes.Buffer

	for _, seg := range segments {
		segStart := seg.seq
		segLen := uint32(len(seg.data))
		if segLen == 0 {
			continue
		}
		segEnd := segStart + segLen

		if segEnd <= currentEnd {
			continue
		}
		if segStart > currentEnd {
			return nil, false
		}

		startOffset := currentEnd - segStart
		if startOffset > uint32(len(seg.data)) {
			return nil, false
		}
		out.Write(seg.data[startOffset:])
		currentEnd = segEnd
	}
	return out.Bytes(), true
}

func parseTCPFromPacketData(data []byte) (srcIP, dstIP net.IP, srcPort, dstPort layers.TCPPort, seq uint32, syn bool, payload []byte, ok bool) {
	if len(data) < 20 {
		return nil, nil, 0, 0, 0, false, nil, false
	}

	// LINUX_SLL2 cooked header (20 bytes). Strip if present, or search IPv4 marker.
	if len(data) >= 22 && (data[20]>>4) == 4 && data[21] == 0 {
		data = data[20:]
	} else if len(data) > 40 {
		if idx := bytes.Index(data[:40], []byte{0x45, 0x00}); idx >= 0 {
			data = data[idx:]
		}
	}

	ver := data[0] >> 4
	switch ver {
	case 4:
		if data[9] != 6 {
			return nil, nil, 0, 0, 0, false, nil, false
		}
		ihl := int(data[0]&0x0f) * 4
		if ihl < 20 || len(data) < ihl+20 {
			return nil, nil, 0, 0, 0, false, nil, false
		}
		srcIP = net.IPv4(data[12], data[13], data[14], data[15])
		dstIP = net.IPv4(data[16], data[17], data[18], data[19])

		tcpStart := ihl
		srcPort = layers.TCPPort(binary.BigEndian.Uint16(data[tcpStart : tcpStart+2]))
		dstPort = layers.TCPPort(binary.BigEndian.Uint16(data[tcpStart+2 : tcpStart+4]))
		seq = binary.BigEndian.Uint32(data[tcpStart+4 : tcpStart+8])
		flags := data[tcpStart+13]
		syn = (flags & 0x02) != 0

		doff := int((data[tcpStart+12] >> 4) & 0x0f) * 4
		if doff < 20 || len(data) < tcpStart+doff {
			return nil, nil, 0, 0, 0, false, nil, false
		}
		payload = data[tcpStart+doff:]
		return srcIP, dstIP, srcPort, dstPort, seq, syn, payload, true
	default:
		return nil, nil, 0, 0, 0, false, nil, false
	}
}

