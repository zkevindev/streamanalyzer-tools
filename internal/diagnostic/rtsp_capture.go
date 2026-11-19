package diagnostic

import (
	"archive/zip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
)

type rtspCapture struct {
	mu                sync.Mutex
	start             time.Time
	ctx               context.Context
	cancel            context.CancelFunc
	wire, timeline    *artifactWriter
	metadata          rtspArchiveMetadata
	sdp               []byte
	stages            []CaptureStage
	connections       []net.Conn
	reason            string
	err               error
	udpData, udpIndex *artifactWriter
	packetConnections []net.PacketConn
	udpClientPorts    map[int][2]int
	udpSources        map[string]net.IPAddr
}

type rtspCaptureConn struct {
	net.Conn
	capture *rtspCapture
	id      int
}

func (connection *rtspCaptureConn) Read(data []byte) (int, error) {
	n, err := connection.Conn.Read(data)
	if n == 0 {
		return n, err
	}
	kept, keepErr := connection.capture.keep(data[:n], connection.id)
	if keepErr != nil {
		return kept, keepErr
	}
	if kept != n {
		return kept, io.EOF
	}
	return n, err
}

func (capture *rtspCapture) now() int64 { return max(0, time.Since(capture.start).Milliseconds()) }

func (capture *rtspCapture) keep(data []byte, id int) (int, error) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.err != nil {
		return 0, capture.err
	}
	remaining := maxStreamBytes - (1 << 20) - capture.wire.bytes
	if capture.udpData != nil {
		remaining -= capture.udpData.bytes + capture.udpIndex.bytes
	}
	n := min(len(data), int(max(0, remaining)))
	if n == 0 {
		capture.reason = "size_limit"
		capture.cancel()
		return 0, io.EOF
	}
	span := rtspWireSpan{Offset: capture.wire.bytes, Size: n, ArrivalMS: capture.now(), ConnectionID: id}
	line, _ := json.Marshal(span)
	line = append(line, '\n')
	if int64(len(line)) > maxTimeline-capture.timeline.bytes {
		capture.reason = "timeline_limit"
		capture.cancel()
		return 0, io.EOF
	}
	if _, err := capture.wire.Write(data[:n]); err != nil {
		capture.err = err
		capture.cancel()
		return 0, err
	}
	if _, err := capture.timeline.Write(line); err != nil {
		capture.err = err
		capture.cancel()
		return n, err
	}
	if n < len(data) {
		capture.reason = "size_limit"
		capture.cancel()
	}
	return n, nil
}

func (capture *rtspCapture) stage(name string, start int64) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.stages) < 127 {
		capture.stages = append(capture.stages, CaptureStage{Name: name, StartMS: start, EndMS: capture.now()})
	}
}

func (capture *rtspCapture) dial(ctx context.Context, network, address string, config *tls.Config, secure bool) (net.Conn, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(capture.ctx, cancel)
	defer cancel()
	defer stop()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses := []string{host}
	if net.ParseIP(host) == nil {
		start := capture.now()
		resolved, resolveErr := net.DefaultResolver.LookupIPAddr(ctx, host)
		capture.stage("dns", start)
		if resolveErr != nil {
			return nil, resolveErr
		}
		addresses = nil
		for _, ip := range resolved {
			addresses = append(addresses, ip.String())
		}
	}
	var connection net.Conn
	for index, ip := range addresses {
		if index >= 8 {
			break
		}
		start := capture.now()
		connection, err = (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip, port))
		capture.stage("connect", start)
		if err == nil {
			break
		}
	}
	if connection == nil {
		if err == nil {
			err = errors.New("RTSP connection unavailable")
		}
		return nil, err
	}
	if secure {
		if config == nil {
			config = &tls.Config{}
		} else {
			config = config.Clone()
		}
		if config.ServerName == "" {
			config.ServerName = host
		}
		tlsConnection := tls.Client(connection, config)
		start := capture.now()
		err = tlsConnection.HandshakeContext(ctx)
		capture.stage("tls", start)
		if err != nil {
			connection.Close()
			return nil, err
		}
		connection = tlsConnection
	}
	capture.mu.Lock()
	if capture.ctx.Err() != nil || ctx.Err() != nil {
		contextErr := capture.ctx.Err()
		if contextErr == nil {
			contextErr = ctx.Err()
		}
		capture.mu.Unlock()
		connection.Close()
		return nil, contextErr
	}
	if len(capture.connections) >= maxRTSPConnections {
		capture.mu.Unlock()
		connection.Close()
		return nil, errors.New("RTSP connection limit")
	}
	capture.connections = append(capture.connections, connection)
	id := len(capture.connections)
	capture.metadata.Connections = id
	if capture.metadata.Transport == "udp" {
		if address, ok := connection.RemoteAddr().(*net.TCPAddr); ok {
			capture.metadata.ControlPeerIP = address.IP.String()
		}
	}
	capture.mu.Unlock()
	return &rtspCaptureConn{Conn: connection, capture: capture, id: id}, nil
}

func rtspHeader(header base.Header, name string) string {
	for key, values := range header {
		if strings.EqualFold(key, name) && len(values) == 1 {
			return values[0]
		}
	}
	return ""
}

func rtspMethod(method base.Method) string {
	switch method {
	case base.Options, base.Describe, base.Setup, base.Play, base.Teardown, base.GetParameter, base.SetParameter:
		return string(method)
	}
	return "OTHER"
}

func rtspMethodStage(method string) string {
	switch method {
	case "OPTIONS":
		return "rtsp_options"
	case "DESCRIBE":
		return "rtsp_describe"
	case "SETUP":
		return "rtsp_setup"
	case "PLAY":
		return "rtsp_play"
	}
	return "rtsp_receive"
}

func (capture *rtspCapture) onRequest(request *base.Request) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	cseq, _ := strconv.Atoi(rtspHeader(request.Header, "CSeq"))
	if capture.metadata.Transport == "udp" && request.Method == base.Setup {
		var transport headers.Transport
		if transport.Unmarshal(request.Header["Transport"]) == nil && transport.ClientPorts != nil {
			capture.udpClientPorts[cseq] = *transport.ClientPorts
		}
	}
	if len(capture.metadata.Requests) == maxRTSPRequests {
		capture.metadata.RequestsTruncated++
		return
	}
	capture.metadata.Requests = append(capture.metadata.Requests, RTSPRequestTiming{Number: len(capture.metadata.Requests) + 1, ConnectionID: len(capture.connections), Method: rtspMethod(request.Method), CSeq: cseq, StartMS: capture.now()})
}

func (capture *rtspCapture) onResponse(response *base.Response) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	cseq, _ := strconv.Atoi(rtspHeader(response.Header, "CSeq"))
	for index := len(capture.metadata.Requests) - 1; index >= 0; index-- {
		request := &capture.metadata.Requests[index]
		if request.CSeq != cseq || request.Complete {
			continue
		}
		request.EndMS, request.StatusCode, request.Complete = capture.now(), int(response.StatusCode), true
		if len(capture.stages) < 127 {
			capture.stages = append(capture.stages, CaptureStage{Name: rtspMethodStage(request.Method), StartMS: request.StartMS, EndMS: request.EndMS})
		}
		if request.Method == "DESCRIBE" && response.StatusCode == base.StatusOK {
			if len(response.Body) > maxRTSPSDPBytes {
				capture.reason = "rtsp_error"
				capture.cancel()
			} else {
				capture.sdp = append([]byte(nil), response.Body...)
				capture.metadata.SDPConnectionID = request.ConnectionID
			}
		}
		break
	}
	// Do not recurse through arbitrary locations or forward embedded source
	// credentials. The exact original response remains in server-wire.bin.
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		for key := range response.Header {
			if strings.EqualFold(key, "Location") {
				delete(response.Header, key)
			}
		}
	}
}

func sampleRTSP(ctx context.Context, dir string, target *url.URL, seconds int, protocol string) (Report, error) {
	return sampleRTSPWithTLS(ctx, dir, target, seconds, protocol, nil)
}

func sampleRTSPWithTLS(ctx context.Context, dir string, target *url.URL, seconds int, protocol string, config *tls.Config) (Report, error) {
	return sampleRTSPTransportWithTLS(ctx, dir, target, seconds, protocol, "tcp", config)
}

func sampleRTSPTransport(ctx context.Context, dir string, target *url.URL, seconds int, protocol, transport string) (Report, error) {
	return sampleRTSPTransportWithTLS(ctx, dir, target, seconds, protocol, transport, nil)
}

func sampleRTSPTransportWithTLS(ctx context.Context, dir string, target *url.URL, seconds int, protocol, selected string, config *tls.Config) (Report, error) {
	return sampleRTSPTransportWithPreviewLookup(ctx, dir, target, seconds, protocol, selected, config, nil)
}

func sampleRTSPTransportWithPreviewLookup(ctx context.Context, dir string, target *url.URL, seconds int, protocol, selected string, config *tls.Config, lookup func() (string, error)) (Report, error) {
	selected, err := rtspTransportArgument(protocol, selected)
	if err != nil {
		return Report{}, err
	}
	wire, err := newArtifact(dir, rtspWireName)
	if err != nil {
		return Report{}, err
	}
	timeline, err := newArtifact(dir, timelineName)
	if err != nil {
		wire.close()
		return Report{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	start := time.Now()
	capture := &rtspCapture{ctx: ctx, cancel: cancel, start: start, wire: wire, timeline: timeline, metadata: rtspArchiveMetadata{Version: 1, Requests: []RTSPRequestTiming{}, Channels: []rtspChannel{}}}
	if selected == "udp" {
		capture.metadata.Version, capture.metadata.Transport = 2, "udp"
		capture.udpClientPorts = map[int][2]int{}
		capture.udpData, err = newArtifact(dir, rtspUDPDataName)
		if err == nil {
			capture.udpIndex, err = newArtifact(dir, rtspUDPIndexName)
		}
		if err != nil {
			wire.close()
			timeline.close()
			closeRTSPArtifact(capture.udpData)
			return Report{}, err
		}
	}
	stop := context.AfterFunc(ctx, func() {
		capture.mu.Lock()
		connections := append([]net.Conn(nil), capture.connections...)
		packets := append([]net.PacketConn(nil), capture.packetConnections...)
		capture.mu.Unlock()
		for _, connection := range connections {
			connection.Close()
		}
		for _, connection := range packets {
			connection.Close()
		}
	})
	defer stop()
	u, err := base.ParseURL(target.String())
	if err != nil {
		wire.close()
		timeline.close()
		closeRTSPArtifact(capture.udpData)
		closeRTSPArtifact(capture.udpIndex)
		return Report{}, ErrInvalid
	}
	transport := gortsplib.ProtocolTCP
	if selected == "udp" {
		transport = gortsplib.ProtocolUDP
	}
	client := &gortsplib.Client{Scheme: u.Scheme, Host: u.Host, Protocol: &transport, ReadTimeout: 10 * time.Second, WriteTimeout: 5 * time.Second, UserAgent: "StreamAnalyzer-Diagnostic/1.0", OnRequest: capture.onRequest, OnResponse: capture.onResponse, OnPacketsLost: func(uint64) {}, OnDecodeError: func(error) {}}
	client.DialContext = func(dialCtx context.Context, network, address string) (net.Conn, error) {
		return capture.dial(dialCtx, network, address, nil, false)
	}
	client.DialTLSContext = func(dialCtx context.Context, network, address string) (net.Conn, error) {
		return capture.dial(dialCtx, network, address, config, true)
	}
	if selected == "udp" {
		client.ListenPacket = capture.listenUDP
		client.ResolveIPAddr = capture.resolveUDPSource
	}
	observation := Observation{Origin: origin(target), StartedAt: start.UTC(), RequestedSeconds: seconds}
	failureStage := "rtsp_describe"
	playing := false
	var preview *rtspPreview
	if player := capturePlayer(ctx); player != nil {
		preview = newRTSPPreview(player)
		preview.g711Lookup = lookup
		capture.metadata.Version = rtspPreviewArchiveVersion
	}
	var desc *description.Session
	if err = client.Start(); err == nil {
		desc, _, err = client.Describe(u)
		if err == nil && len(desc.Medias) > maxRTSPTracks {
			err = errors.New("RTSP media limit")
		}
		if err == nil {
			formats := 0
			for _, media := range desc.Medias {
				formats += len(media.Formats)
			}
			if formats > maxRTSPTracks {
				err = errors.New("RTSP format limit")
			}
		}
		if err == nil {
			failureStage = "rtsp_setup"
			for mediaIndex, media := range desc.Medias {
				if media.IsBackChannel {
					continue
				}
				response, setupErr := client.Setup(desc.BaseURL, media, 0, 0)
				if setupErr != nil {
					err = setupErr
					break
				}
				var negotiated headers.Transport
				if negotiated.Unmarshal(response.Header["Transport"]) != nil {
					err = errors.New("RTSP transport unsupported")
					break
				}
				if selected == "udp" {
					if err = capture.registerUDPTransport(response, negotiated, mediaIndex+1); err != nil {
						break
					}
					continue
				}
				if negotiated.InterleavedIDs == nil {
					err = errors.New("RTSP transport unsupported")
					break
				}
				capture.mu.Lock()
				capture.metadata.Channels = append(capture.metadata.Channels, rtspChannel{ConnectionID: capture.metadata.Connections, MediaID: mediaIndex + 1, RTPChannel: negotiated.InterleavedIDs[0], RTCPChannel: negotiated.InterleavedIDs[1]})
				capture.mu.Unlock()
			}
		}
		if err == nil {
			if preview != nil {
				preview.start(client, desc)
			}
			failureStage = "rtsp_play"
			_, err = client.Play(nil)
			playing = err == nil
		}
		if err == nil {
			failureStage = "rtsp_receive"
			receiveStart := capture.now()
			err = client.Wait()
			capture.stage("rtsp_receive", receiveStart)
		}
		client.Close()
	}
	receiveEndMS := capture.now()
	if preview != nil {
		if !preview.started {
			preview.fail("capture_setup_failed")
			close(preview.done)
		} else if !playing && !preview.failed {
			preview.fail("capture_setup_failed")
		}
		result := preview.finish()
		capture.metadata.Preview = &result
	}
	observation.ElapsedMS = capture.now()
	if capture.metadata.Version == rtspPreviewArchiveVersion {
		observation.ElapsedMS = receiveEndMS
	}
	capture.mu.Lock()
	observation.EndReason = capture.reason
	if observation.EndReason == "" {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			observation.EndReason = "duration_limit"
		} else if ctx.Err() != nil {
			observation.EndReason = "canceled"
		} else if playing && errors.Is(err, io.EOF) {
			observation.EndReason = "eof"
		} else {
			observation.EndReason = "rtsp_error"
		}
	}
	if err != nil && !(playing && (ctx.Err() != nil || errors.Is(err, io.EOF))) {
		if len(capture.metadata.Requests) == 0 && len(capture.stages) > 0 {
			lastStage := capture.stages[len(capture.stages)-1].Name
			if lastStage == "dns" || lastStage == "connect" || lastStage == "tls" {
				failureStage = lastStage
			}
		}
		// Describe may fail inside the client's implicit OPTIONS exchange.
		// Attribute that failure to the request that was actually sent.
		if failureStage != "rtsp_receive" && capture.metadata.RequestsTruncated == 0 {
			for index := len(capture.metadata.Requests) - 1; index >= 0; index-- {
				request := capture.metadata.Requests[index]
				if request.Method == "TEARDOWN" {
					continue
				}
				if !request.Complete || request.StatusCode >= 300 {
					failureStage = rtspMethodStage(request.Method)
				}
				break
			}
		}
		observation.FailureStage, observation.FailureCode = rtspFailure(failureStage, err)
		if ctx.Err() != nil {
			_, observation.FailureCode = httpFailure(failureStage, ctx.Err())
		}
		for index := len(capture.metadata.Requests) - 1; index >= 0; index-- {
			request := &capture.metadata.Requests[index]
			if request.Method != "TEARDOWN" && rtspMethodStage(request.Method) == failureStage {
				request.FailureCode = observation.FailureCode
				break
			}
		}
	}
	for index := range capture.metadata.Requests {
		request := &capture.metadata.Requests[index]
		if !request.Complete {
			request.EndMS = observation.ElapsedMS
		}
	}
	observation.Stages = append([]CaptureStage(nil), capture.stages...)
	metadata, sdp, captureErr := capture.metadata, append([]byte(nil), capture.sdp...), capture.err
	capture.mu.Unlock()
	if err := errors.Join(captureErr, wire.close(), timeline.close(), closeRTSPArtifact(capture.udpData), closeRTSPArtifact(capture.udpIndex)); err != nil {
		return Report{}, err
	}
	artifact, err := writeRTSPArchive(dir, metadata, sdp)
	if err != nil {
		return Report{}, err
	}
	_ = os.Remove(filepath.Join(dir, rtspWireName))
	if selected == "udp" {
		_ = os.Remove(filepath.Join(dir, rtspUDPDataName))
		_ = os.Remove(filepath.Join(dir, rtspUDPIndexName))
	}
	manifest := Manifest{Version: multiProtocolSchemaVersion, Protocol: protocol, Observation: observation, Stream: artifact, Timeline: timeline.artifact()}
	return replayRTSP(dir, manifest)
}

func rtspFailure(stage string, err error) (string, string) {
	var status liberrors.ErrClientBadStatusCode
	if errors.As(err, &status) {
		if status.Code == base.StatusUnauthorized || status.Code == base.StatusForbidden {
			return stage, "rtsp_auth_error"
		}
		if status.Code == base.StatusUnsupportedTransport {
			return stage, "rtsp_transport_error"
		}
		return stage, "rtsp_status"
	}
	switch err.(type) {
	case liberrors.ErrClientTCPTimeout, liberrors.ErrClientRequestTimedOut, liberrors.ErrClientUDPTimeout:
		return stage, "timeout"
	case liberrors.ErrClientSDPInvalid, liberrors.ErrClientContentTypeMissing, liberrors.ErrClientContentTypeUnsupported:
		return stage, "rtsp_parse_error"
	case liberrors.ErrClientTransportHeaderInvalid, liberrors.ErrClientTransportHeaderNoInterleavedIDs, liberrors.ErrClientTransportHeaderInvalidInterleavedIDs, liberrors.ErrClientTransportHeaderInterleavedIDsInUse, liberrors.ErrClientServerRequestedUDP, liberrors.ErrClientServerRequestedTCP, liberrors.ErrClientServerPortsNotProvided:
		return stage, "rtsp_transport_error"
	}
	knownStage, code := httpFailure(stage, err)
	if code != "network_error" {
		return knownStage, code
	}
	return stage, "rtsp_error"
}

func writeRTSPArchive(dir string, metadata rtspArchiveMetadata, sdp []byte) (Artifact, error) {
	output, err := newArtifact(dir, rtspStreamName)
	if err != nil {
		return Artifact{}, err
	}
	archive := zip.NewWriter(output)
	metadataData, err := json.Marshal(metadata)
	if err != nil || len(metadataData) > maxRTSPMetadataBytes {
		output.close()
		return Artifact{}, ErrInvalid
	}
	names := []string{rtspWireName, rtspSDPName, rtspMetadataName}
	if metadata.Transport == "udp" {
		names = append(names, rtspUDPDataName, rtspUDPIndexName)
	}
	for _, name := range names {
		entry, createErr := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if createErr != nil {
			output.close()
			return Artifact{}, createErr
		}
		if name == rtspWireName || name == rtspUDPDataName || name == rtspUDPIndexName {
			err = copyFile(entry, filepath.Join(dir, name))
		} else if name == rtspSDPName {
			_, err = entry.Write(sdp)
		} else {
			_, err = entry.Write(metadataData)
		}
		if err != nil {
			output.close()
			return Artifact{}, err
		}
	}
	if err := errors.Join(archive.Close(), output.close()); err != nil {
		return Artifact{}, err
	}
	return output.artifact(), nil
}
