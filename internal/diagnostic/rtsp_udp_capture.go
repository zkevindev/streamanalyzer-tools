package diagnostic

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
)

// Embed the concrete UDP connection because gortsplib also requires
// SyscallConn and SetReadBuffer. Read the whole datagram before copying it to
// the SDK's smaller receive buffer; oversized packets remain exact evidence.
type rtspUDPCaptureConn struct {
	*net.UDPConn
	capture *rtspCapture
	id      int
	mu      sync.Mutex
	buffer  [65536]byte
	closed  bool
}

func (connection *rtspUDPCaptureConn) Close() error {
	connection.capture.mu.Lock()
	connection.closed = true
	connection.capture.mu.Unlock()
	return connection.UDPConn.Close()
}

func (connection *rtspUDPCaptureConn) ReadFrom(data []byte) (int, net.Addr, error) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	connection.capture.mu.Lock()
	if connection.capture.metadata.UDPSockets[connection.id-1].ReaderBufferBytes == 0 {
		connection.capture.metadata.UDPSockets[connection.id-1].ReaderBufferBytes = len(data)
	}
	connection.capture.mu.Unlock()
	for {
		n, _, _, source, err := connection.UDPConn.ReadMsgUDP(connection.buffer[:], nil)
		if err != nil {
			return 0, source, err
		}
		if n > 65535 {
			connection.capture.mu.Lock()
			connection.capture.reason = "size_limit"
			connection.capture.cancel()
			connection.capture.mu.Unlock()
			return 0, source, io.ErrShortBuffer
		}
		if err := connection.capture.keepUDP(connection.buffer[:n], connection.id, source); err != nil {
			return 0, source, err
		}
		if n > len(data) {
			continue
		}
		copy(data, connection.buffer[:n])
		return n, source, nil
	}
}

func (capture *rtspCapture) listenUDP(network, address string) (net.PacketConn, error) {
	if err := capture.ctx.Err(); err != nil {
		return nil, err
	}
	listener, err := net.ListenPacket(network, address)
	if err != nil {
		return nil, err
	}
	udp, ok := listener.(*net.UDPConn)
	if !ok {
		listener.Close()
		return nil, ErrInvalid
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.ctx.Err() != nil {
		udp.Close()
		return nil, capture.ctx.Err()
	}
	if len(capture.packetConnections) >= maxRTSPUDPSockets {
		udp.Close()
		return nil, errors.New("RTSP UDP socket limit")
	}
	id := len(capture.packetConnections) + 1
	wrapped := &rtspUDPCaptureConn{UDPConn: udp, capture: capture, id: id}
	capture.packetConnections = append(capture.packetConnections, wrapped)
	capture.metadata.UDPSockets = append(capture.metadata.UDPSockets, rtspUDPSocket{ID: id, LocalPort: udp.LocalAddr().(*net.UDPAddr).Port})
	return wrapped, nil
}

func (capture *rtspCapture) keepUDP(data []byte, socket int, source *net.UDPAddr) error {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.err != nil {
		return capture.err
	}
	if capture.ctx.Err() != nil {
		return capture.ctx.Err()
	}
	span := rtspUDPSpan{Offset: capture.udpData.bytes, Size: len(data), ArrivalMS: capture.now(), SocketID: socket, SourceIP: source.IP.String(), SourcePort: source.Port}
	line, _ := json.Marshal(span)
	line = append(line, '\n')
	if int64(len(line)) > maxRTSPUDPIndexBytes-capture.udpIndex.bytes {
		capture.reason = "timeline_limit"
		capture.cancel()
		return io.EOF
	}
	if int64(len(data)+len(line)) > capture.limitBytes()-min(int64(1<<20), capture.limitBytes()/8)-capture.wire.bytes-capture.udpData.bytes-capture.udpIndex.bytes {
		capture.reason = "size_limit"
		capture.cancel()
		return io.EOF
	}
	if _, err := capture.udpData.Write(data); err != nil {
		capture.err = err
		capture.cancel()
		return err
	}
	if _, err := capture.udpIndex.Write(line); err != nil {
		capture.err = err
		capture.cancel()
		return err
	}
	return nil
}

func (capture *rtspCapture) resolveUDPSource(network, address string) (*net.IPAddr, error) {
	start := capture.now()
	resolved, err := net.DefaultResolver.LookupIPAddr(capture.ctx, address)
	capture.stage("dns", start)
	if err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		return nil, &net.DNSError{Err: "no address", Name: address, IsNotFound: true}
	}
	value := resolved[0]
	capture.mu.Lock()
	if capture.udpSources == nil {
		capture.udpSources = map[string]net.IPAddr{}
	}
	if len(capture.udpSources) < maxRTSPTracks {
		capture.udpSources[address] = value
	}
	capture.mu.Unlock()
	return &value, nil
}

func (capture *rtspCapture) registerUDPTransport(response *base.Response, negotiated headers.Transport, media int) error {
	if negotiated.Protocol != headers.TransportProtocolUDP || negotiated.Profile != headers.TransportProfileAVP || negotiated.ServerPorts == nil || negotiated.ServerPorts[0] < 1 || negotiated.ServerPorts[1] < 1 {
		return errors.New("RTSP UDP transport unsupported")
	}
	cseq, err := strconv.Atoi(rtspHeader(response.Header, "CSeq"))
	if err != nil {
		return ErrInvalid
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	ports, ok := capture.udpClientPorts[cseq]
	if !ok {
		return ErrInvalid
	}
	if negotiated.ClientPorts != nil && *negotiated.ClientPorts != ports {
		return ErrInvalid
	}
	source := capture.metadata.ControlPeerIP
	if negotiated.Source2 != nil {
		if ip := net.ParseIP(*negotiated.Source2); ip != nil {
			source = ip.String()
		} else if ip, exists := capture.udpSources[*negotiated.Source2]; exists {
			source = ip.IP.String()
		} else {
			return ErrInvalid
		}
	}
	rtpSocket, rtcpSocket := 0, 0
	for _, socket := range capture.metadata.UDPSockets {
		if capture.packetConnections[socket.ID-1].(*rtspUDPCaptureConn).closed {
			continue
		}
		if socket.LocalPort == ports[0] {
			rtpSocket = socket.ID
		}
		if socket.LocalPort == ports[1] {
			rtcpSocket = socket.ID
		}
	}
	if rtpSocket == 0 || rtcpSocket == 0 || rtpSocket == rtcpSocket {
		return ErrInvalid
	}
	capture.metadata.UDPTransports = append(capture.metadata.UDPTransports, rtspUDPTransport{MediaID: media, RTPSocketID: rtpSocket, RTCPSocketID: rtcpSocket, SourceIP: source, RTPServerPort: negotiated.ServerPorts[0], RTCPServerPort: negotiated.ServerPorts[1], CSeq: cseq})
	return nil
}

func closeRTSPArtifact(writer *artifactWriter) error {
	if writer == nil {
		return nil
	}
	return writer.close()
}

func rtspTransportArgument(protocol, transport string) (string, error) {
	if transport == "" {
		transport = "tcp"
	}
	if transport != "tcp" && transport != "udp" || transport == "udp" && protocol != ProtocolRTSP {
		return "", ErrInvalid
	}
	return transport, nil
}
