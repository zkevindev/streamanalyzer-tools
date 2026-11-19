package diagnostic

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"io"
	"net"
	"strings"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
	"github.com/pion/rtcp"
)

func validRTSPUDPMetadata(metadata rtspArchiveMetadata, session *description.Session, responses map[int]*base.Response) (map[int]rtspUDPTransport, error) {
	if (metadata.Version != 2 && metadata.Version != rtspPreviewArchiveVersion) || metadata.Transport != "udp" || len(metadata.Channels) != 0 || len(metadata.UDPSockets) > maxRTSPUDPSockets || len(metadata.UDPTransports) > maxRTSPTracks {
		return nil, ErrInvalid
	}
	if metadata.Connections == 0 {
		if metadata.ControlPeerIP != "" || len(metadata.UDPSockets) != 0 || len(metadata.UDPTransports) != 0 {
			return nil, ErrInvalid
		}
	} else if !canonicalRTSPIP(metadata.ControlPeerIP) {
		return nil, ErrInvalid
	}
	for index, socket := range metadata.UDPSockets {
		if socket.ID != index+1 || socket.LocalPort < 1 || socket.LocalPort > 65535 || socket.ReaderBufferBytes < 0 || socket.ReaderBufferBytes > 65536 {
			return nil, ErrInvalid
		}
	}
	var expected []int
	if session != nil {
		for index, media := range session.Medias {
			if !media.IsBackChannel {
				expected = append(expected, index+1)
			}
		}
	}
	bySocket := map[int]rtspUDPTransport{}
	usedCSeq := map[int]bool{}
	var setupCSeqs []int
	for _, request := range metadata.Requests {
		if request.Method == "SETUP" && request.Complete && request.StatusCode == 200 {
			setupCSeqs = append(setupCSeqs, request.CSeq)
		}
	}
	previousCSeq := 0
	for index, transport := range metadata.UDPTransports {
		if index >= len(expected) || transport.MediaID != expected[index] || transport.CSeq < 1 || usedCSeq[transport.CSeq] || transport.RTPSocketID < 1 || transport.RTPSocketID > len(metadata.UDPSockets) || transport.RTCPSocketID < 1 || transport.RTCPSocketID > len(metadata.UDPSockets) || transport.RTPSocketID == transport.RTCPSocketID || !canonicalRTSPIP(transport.SourceIP) || transport.RTPServerPort < 1 || transport.RTPServerPort > 65535 || transport.RTCPServerPort < 1 || transport.RTCPServerPort > 65535 {
			return nil, ErrInvalid
		}
		// SETUPs are issued in SDP media order. A valid socket pair from a
		// different SETUP cannot be relabelled as another media with the same PT.
		if transport.CSeq <= previousCSeq || index < len(setupCSeqs) && transport.CSeq != setupCSeqs[index] || index >= len(setupCSeqs) && metadata.RequestsTruncated == 0 {
			return nil, ErrInvalid
		}
		previousCSeq = transport.CSeq
		if _, exists := bySocket[transport.RTPSocketID]; exists {
			return nil, ErrInvalid
		}
		if _, exists := bySocket[transport.RTCPSocketID]; exists {
			return nil, ErrInvalid
		}
		response := responses[transport.CSeq]
		if response == nil || response.StatusCode != base.StatusOK {
			return nil, ErrInvalid
		}
		methodProven := false
		for _, request := range metadata.Requests {
			if request.CSeq == transport.CSeq {
				if request.Method != "SETUP" || !request.Complete || request.StatusCode != 200 {
					return nil, ErrInvalid
				}
				methodProven = true
			}
		}
		if !methodProven && metadata.RequestsTruncated == 0 {
			return nil, ErrInvalid
		}
		var negotiated headers.Transport
		if negotiated.Unmarshal(response.Header["Transport"]) != nil || negotiated.Protocol != headers.TransportProtocolUDP || negotiated.Profile != headers.TransportProfileAVP || negotiated.ServerPorts == nil || *negotiated.ServerPorts != [2]int{transport.RTPServerPort, transport.RTCPServerPort} || negotiated.Delivery != nil && *negotiated.Delivery != headers.TransportDeliveryUnicast {
			return nil, ErrInvalid
		}
		if negotiated.ClientPorts != nil && *negotiated.ClientPorts != [2]int{metadata.UDPSockets[transport.RTPSocketID-1].LocalPort, metadata.UDPSockets[transport.RTCPSocketID-1].LocalPort} {
			return nil, ErrInvalid
		}
		if negotiated.Source2 == nil {
			if transport.SourceIP != metadata.ControlPeerIP {
				return nil, ErrInvalid
			}
		} else if source := net.ParseIP(*negotiated.Source2); source != nil {
			if source.String() != transport.SourceIP {
				return nil, ErrInvalid
			}
		}
		usedCSeq[transport.CSeq] = true
		bySocket[transport.RTPSocketID], bySocket[transport.RTCPSocketID] = transport, transport
	}
	return bySocket, nil
}

func canonicalRTSPIP(value string) bool {
	ip := net.ParseIP(value)
	return ip != nil && ip.String() == value
}

func replayRTSPUDP(report *Report, metadata rtspArchiveMetadata, session *description.Session, entries map[string]*zip.File, responses map[int]*base.Response, states []rtspTrackState, events *[]Event, visit func(Event)) error {
	bySocket, err := validRTSPUDPMetadata(metadata, session, responses)
	if err != nil {
		return err
	}
	data, err := entries[rtspUDPDataName].Open()
	if err != nil {
		return ErrInvalid
	}
	defer data.Close()
	index, err := entries[rtspUDPIndexName].Open()
	if err != nil {
		return ErrInvalid
	}
	defer index.Close()
	reader := &rtspCountingReader{Reader: io.LimitReader(index, maxRTSPUDPIndexBytes+1)}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 1024)
	buffer := make([]byte, 65535)
	offset, previous := int64(0), int64(0)
	metrics := report.RTSP
	analyzed := 0
	for scanner.Scan() {
		var span rtspUDPSpan
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&span) != nil || decoder.Decode(new(any)) != io.EOF || span.Offset != offset || span.Size < 0 || span.Size > 65535 || int64(span.Size) > int64(entries[rtspUDPDataName].UncompressedSize64)-offset || span.ArrivalMS < previous || span.ArrivalMS > report.Manifest.Observation.ElapsedMS || span.SocketID < 1 || span.SocketID > len(metadata.UDPSockets) || !canonicalRTSPIP(span.SourceIP) || span.SourcePort < 0 || span.SourcePort > 65535 || metadata.UDPSockets[span.SocketID-1].ReaderBufferBytes == 0 {
			return ErrInvalid
		}
		payload := buffer[:span.Size]
		if _, err := io.ReadFull(data, payload); err != nil {
			return ErrInvalid
		}
		offset += int64(span.Size)
		previous = span.ArrivalMS
		metrics.UDPDatagrams++
		metrics.UDPBytes += int64(span.Size)
		if span.Size > metadata.UDPSockets[span.SocketID-1].ReaderBufferBytes {
			metrics.UDPOversizeDatagrams++
		}
		transport, exists := bySocket[span.SocketID]
		if !exists {
			// A bound socket can receive before SETUP succeeds (or after SETUP
			// is rejected). Preserve those original bytes without asserting a peer.
			metrics.FilteredDatagrams++
			continue
		}
		port := transport.RTPServerPort
		isRTCP := span.SocketID == transport.RTCPSocketID
		if isRTCP {
			port = transport.RTCPServerPort
		}
		if span.SourceIP != transport.SourceIP || span.SourcePort != port {
			metrics.FilteredDatagrams++
			continue
		}
		metrics.AcceptedDatagrams++
		analyzed++
		if analyzed > maxTags {
			metrics.AnalysisLimited = true
			continue
		}
		if isRTCP {
			packets, err := rtcp.Unmarshal(payload)
			if err != nil || len(packets) == 0 {
				metrics.MalformedPackets++
				if len(*events) < 2 {
					*events = append(*events, Event{Artifact: rtspUDPDataName, Offset: span.Offset, Size: span.Size, ArrivalMS: span.ArrivalMS, Kind: "rtcp_invalid"})
				}
			} else {
				metrics.RTCPPackets += len(packets)
				if metrics.FirstRTCPMS == nil {
					first := span.ArrivalMS
					metrics.FirstRTCPMS = &first
				}
			}
		} else {
			observeRTSPRTP(metrics, states, transport.MediaID, payload, rtspUDPDataName, span.Offset, span.ArrivalMS, events, visit)
		}
	}
	if scanner.Err() != nil || reader.readErr != nil || reader.count > maxRTSPUDPIndexBytes || offset != int64(entries[rtspUDPDataName].UncompressedSize64) {
		return ErrInvalid
	}
	if _, err := io.Copy(io.Discard, data); err != nil {
		return ErrInvalid
	}
	return nil
}

func validRTSPLegacyArchiveFields(metadata rtspArchiveMetadata, data []byte) bool {
	if metadata.Version != 1 {
		return true
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return false
	}
	for _, key := range []string{"transport", "control_peer_ip", "udp_sockets", "udp_transports"} {
		if _, exists := fields[key]; exists {
			return false
		}
	}
	return true
}

// TCP archives never carry UDP socket/peer metadata, including empty fields.
// Version 3's preview field is intentionally allowed by this shape check.
func validRTSPTCPArchiveFields(data []byte) bool {
	for _, key := range []string{"transport", "control_peer_ip", "udp_sockets", "udp_transports"} {
		if rtspArchiveHasField(data, key) {
			return false
		}
	}
	return true
}
func rtspArchiveHasField(data []byte, key string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return true
	}
	_, exists := fields[key]
	return exists
}
