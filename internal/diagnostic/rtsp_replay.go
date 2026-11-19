package diagnostic

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/conn"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
	"github.com/pion/rtcp"
)

type rtspCountingReader struct {
	io.Reader
	count   int64
	readErr error
}

func (reader *rtspCountingReader) Read(data []byte) (int, error) {
	n, err := reader.Reader.Read(data)
	reader.count += int64(n)
	if err != nil && err != io.EOF {
		reader.readErr = err
	}
	return n, err
}

type rtspTrackState struct {
	ssrcs            map[uint32]*rtspSequenceState
	lastSSRC         uint32
	received         bool
	gapStart, gapEnd int64
	gapEvidence      []Event
	sequenceEvidence []Event
	lastEvent        *Event
}

func replayRTSP(dir string, manifest Manifest) (Report, error) {
	return replayRTSPWithEvents(dir, manifest, nil)
}

// walkRTSPMediaEvents streams the bounded, accepted RTP analysis prefix from
// the original native evidence. Callback kinds describe SDP media, not frames.
func walkRTSPMediaEvents(dir string, manifest Manifest, visit func(Event)) error {
	_, err := replayRTSPWithEvents(dir, manifest, visit)
	return err
}

func replayRTSPWithEvents(dir string, manifest Manifest, visit func(Event)) (Report, error) {
	archive, archiveFile, err := openEvidenceZIP(filepath.Join(dir, rtspStreamName))
	if err != nil {
		return Report{}, fmt.Errorf("%w: RTSP 原始证据不可读", ErrInvalid)
	}
	defer archiveFile.Close()
	entries := map[string]*zip.File{}
	limits := map[string]uint64{rtspWireName: maxStreamBytes, rtspSDPName: maxRTSPSDPBytes, rtspMetadataName: maxRTSPMetadataBytes, rtspUDPDataName: maxStreamBytes, rtspUDPIndexName: maxRTSPUDPIndexBytes}
	var total uint64
	for _, entry := range archive.File {
		limit, ok := limits[entry.Name]
		if !ok || entries[entry.Name] != nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 > limit {
			return Report{}, ErrInvalid
		}
		total += entry.UncompressedSize64
		entries[entry.Name] = entry
	}
	if (len(entries) != 3 && len(entries) != 5) || entries[rtspWireName] == nil || entries[rtspSDPName] == nil || entries[rtspMetadataName] == nil || total > maxStreamBytes {
		return Report{}, ErrInvalid
	}
	readSmall := func(name string, limit int64) ([]byte, error) {
		file, err := entries[name].Open()
		if err != nil {
			return nil, err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, limit+1))
		if len(data) > int(limit) {
			return nil, ErrInvalid
		}
		return data, err
	}
	metadataData, err := readSmall(rtspMetadataName, maxRTSPMetadataBytes)
	if err != nil {
		return Report{}, err
	}
	var metadata rtspArchiveMetadata
	decoder := json.NewDecoder(bytes.NewReader(metadataData))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&metadata) != nil || decoder.Decode(new(any)) != io.EOF || (metadata.Version != 1 && metadata.Version != 2 && metadata.Version != rtspLegacyPreviewArchiveVersion && metadata.Version != rtspPreviewArchiveVersion) || metadata.Connections < 0 || metadata.Connections > 1 || len(metadata.Requests) > maxRTSPRequests || metadata.RequestsTruncated < 0 || metadata.RequestsTruncated > 1_000_000_000 {
		return Report{}, ErrInvalid
	}
	// Archive version 3 adds a recorded preview result without changing the
	// native transport's raw evidence or its source/port validation boundary.
	udp := metadata.Transport == "udp"
	if metadata.Version == 1 {
		if len(entries) != 3 || !validRTSPLegacyArchiveFields(metadata, metadataData) {
			return Report{}, ErrInvalid
		}
	} else if udp {
		if len(entries) != 5 || entries[rtspUDPDataName] == nil || entries[rtspUDPIndexName] == nil || manifest.Protocol != ProtocolRTSP || len(metadata.Channels) != 0 {
			return Report{}, ErrInvalid
		}
	} else if (metadata.Version != rtspLegacyPreviewArchiveVersion && metadata.Version != rtspPreviewArchiveVersion) || len(entries) != 3 || !validRTSPTCPArchiveFields(metadataData) {
		return Report{}, ErrInvalid
	}
	if metadata.Version < rtspLegacyPreviewArchiveVersion && rtspArchiveHasField(metadataData, "preview") {
		return Report{}, ErrInvalid
	}

	sdpData, err := readSmall(rtspSDPName, maxRTSPSDPBytes)
	if err != nil {
		return Report{}, err
	}
	session, tracks, err := rtspDescription(sdpData)
	invalidSDP := err != nil
	if invalidSDP {
		session, tracks = nil, []RTSPTrackMetrics{}
	}
	if metadata.Version < rtspLegacyPreviewArchiveVersion && metadata.Preview != nil || metadata.Version >= rtspLegacyPreviewArchiveVersion && (!validRTSPPreview(metadata.Preview, tracks) || metadata.Preview.State == "not_requested") || !validRTSPPreviewArchiveVersion(metadata, metadataData) {
		return Report{}, ErrInvalid
	}
	spans, err := readRTSPSpans(dir, manifest.Observation, int64(entries[rtspWireName].UncompressedSize64), metadata.Connections)
	if err != nil {
		return Report{}, err
	}
	if metadata.SDPConnectionID < 0 || metadata.SDPConnectionID > 1 || len(sdpData) != 0 && metadata.SDPConnectionID != 1 {
		return Report{}, ErrInvalid
	}
	channels := map[int]rtspChannel{}
	mediaSeen := map[int]bool{}
	for _, channel := range metadata.Channels {
		if session == nil || channel.ConnectionID != 1 || channel.MediaID < 1 || channel.MediaID > len(session.Medias) || channel.RTPChannel < 0 || channel.RTPChannel > 255 || channel.RTCPChannel < 0 || channel.RTCPChannel > 255 || channel.RTPChannel == channel.RTCPChannel || mediaSeen[channel.MediaID] {
			return Report{}, ErrInvalid
		}
		if _, exists := channels[channel.RTPChannel]; exists {
			return Report{}, ErrInvalid
		}
		if _, exists := channels[channel.RTCPChannel]; exists {
			return Report{}, ErrInvalid
		}
		channels[channel.RTPChannel], channels[channel.RTCPChannel], mediaSeen[channel.MediaID] = channel, channel, true
	}
	if len(metadata.Channels) > maxRTSPTracks {
		return Report{}, ErrInvalid
	}
	if err := validRTSPRequests(metadata.Requests, manifest.Observation.ElapsedMS, metadata.Connections); err != nil {
		return Report{}, err
	}
	metrics := &RTSPMetrics{Preview: metadata.Preview, Boundary: "decrypted_rtsp_application", Transport: "tcp_interleaved", Secure: manifest.Protocol == ProtocolRTSPS, SDPAvailable: len(sdpData) != 0, Connections: metadata.Connections, Requests: metadata.Requests, RequestsTruncated: metadata.RequestsTruncated, Tracks: tracks, AnalysisLimited: invalidSDP}
	report := Report{RuleVersion: "rtsp/1", Manifest: manifest, RTSP: metrics, Metrics: Metrics{MediaTimingUnit: "rtp"}, Findings: []Finding{}}
	if udp {
		report.RuleVersion = "rtsp/2"
		metrics.Transport, metrics.RequestedTransport, metrics.Boundary = "udp", "udp", "rtsp_control_and_udp_datagrams"
	}
	if metadata.Version == rtspLegacyPreviewArchiveVersion {
		report.RuleVersion = "rtsp/3"
	}
	if metadata.Version == rtspPreviewArchiveVersion {
		report.RuleVersion = "rtsp/4"
	}
	report.Manifest.Observation.FirstPayloadMS = nil
	if len(spans) > 0 {
		first := spans[0].ArrivalMS
		report.Manifest.Observation.FirstPayloadMS = &first
	}
	if invalidSDP {
		report.Findings = append(report.Findings, Finding{ID: "F001", Code: "rtsp_sdp_invalid", Severity: "error", Message: "DESCRIBE 返回的 SDP 无法解析或超出本版轨道分析范围，已保留原文。", NextCheck: "对照设备的 SDP 输出、媒体类型与轨道数量，原始 session.sdp 可离线复查。", StartMS: 0, EndMS: manifest.Observation.ElapsedMS, Occurrences: 1})
	}
	for _, track := range tracks {
		if track.Kind == "video" {
			report.Metrics.VideoCodecs = appendUnique(report.Metrics.VideoCodecs, track.Codec)
		} else if track.Kind == "audio" {
			report.Metrics.AudioCodecs = appendUnique(report.Metrics.AudioCodecs, track.Codec)
		}
	}
	wire, err := entries[rtspWireName].Open()
	if err != nil {
		return Report{}, err
	}
	defer wire.Close()
	counter := &rtspCountingReader{Reader: wire}
	buffer := bufio.NewReaderSize(counter, 64<<10)
	parser := conn.NewConn(buffer, io.Discard)
	position := func() int64 { return counter.count - int64(buffer.Buffered()) }
	spanIndex := 0
	arrival := func(end int64) int64 {
		for spanIndex < len(spans) && spans[spanIndex].Offset+int64(spans[spanIndex].Size) < end {
			spanIndex++
		}
		if spanIndex == len(spans) {
			return manifest.Observation.ElapsedMS
		}
		return spans[spanIndex].ArrivalMS
	}
	responses := map[int]*base.Response{}
	responseArrivals := map[int]int64{}
	states := make([]rtspTrackState, len(tracks))
	for index := range states {
		states[index].ssrcs = map[uint32]*rtspSequenceState{}
	}
	var events []Event
	packets := 0
	for position() < int64(entries[rtspWireName].UncompressedSize64) {
		before := position()
		object, readErr := parser.Read()
		if readErr != nil {
			metrics.TrailingBytes = int64(entries[rtspWireName].UncompressedSize64) - before
			if len(events) < 2 {
				events = append(events, Event{Artifact: rtspWireName, Offset: before, Size: int(metrics.TrailingBytes), ArrivalMS: arrival(int64(entries[rtspWireName].UncompressedSize64)), Kind: "rtsp_wire_tail"})
			}
			break
		}
		switch value := object.(type) {
		case *base.Response:
			cseq, _ := strconv.Atoi(rtspHeader(value.Header, "CSeq"))
			if len(responses) < maxRTSPRequests+8 {
				responses[cseq] = value
				responseArrivals[cseq] = arrival(position())
			}
		case *base.InterleavedFrame:
			packets++
			if packets > maxTags {
				metrics.AnalysisLimited = true
				continue
			}
			at, offset := arrival(position()), position()-int64(len(value.Payload))
			channel, known := channels[value.Channel]
			if !known {
				metrics.UnknownChannelPackets++
				continue
			}
			if value.Channel == channel.RTCPChannel {
				parsed, err := rtcp.Unmarshal(value.Payload)
				if err != nil {
					metrics.MalformedPackets++
					if len(events) < 2 {
						events = append(events, Event{Artifact: rtspWireName, Offset: offset, Size: len(value.Payload), ArrivalMS: at, Kind: "rtcp_invalid"})
					}
				} else {
					metrics.RTCPPackets += len(parsed)
				}
				continue
			}
			observeRTSPRTP(metrics, states, channel.MediaID, value.Payload, rtspWireName, offset, at, &events, visit)
		}
	}
	if _, err := io.Copy(io.Discard, counter); err != nil || counter.readErr != nil {
		return Report{}, fmt.Errorf("%w: RTSP 原始接收字节未通过 ZIP 校验", ErrInvalid)
	}
	sdpProven := len(sdpData) == 0 && metadata.SDPConnectionID == 0
	var negotiated [][2]int
	for _, request := range metadata.Requests {
		if request.Complete {
			response := responses[request.CSeq]
			if response == nil || int(response.StatusCode) != request.StatusCode {
				return Report{}, ErrInvalid
			}
			if responseArrivals[request.CSeq] < request.StartMS || responseArrivals[request.CSeq] > request.EndMS {
				return Report{}, ErrInvalid
			}
			if request.Method == "DESCRIBE" && request.StatusCode == 200 && !bytes.Equal(response.Body, sdpData) {
				return Report{}, ErrInvalid
			}
			if request.Method == "DESCRIBE" && request.StatusCode == 200 {
				if metadata.SDPConnectionID != 1 {
					return Report{}, ErrInvalid
				}
				sdpProven = true
			}
			if request.Method == "SETUP" && request.StatusCode == 200 {
				var transport headers.Transport
				if transport.Unmarshal(response.Header["Transport"]) == nil && transport.Protocol == headers.TransportProtocolTCP && transport.InterleavedIDs != nil {
					negotiated = append(negotiated, *transport.InterleavedIDs)
				}
			}
		}
	}
	if !sdpProven {
		return Report{}, ErrInvalid
	}
	var expectedMedias []int
	if session != nil {
		for index, media := range session.Medias {
			if !media.IsBackChannel {
				expectedMedias = append(expectedMedias, index+1)
			}
		}
	}
	for index, channel := range metadata.Channels {
		if index >= len(expectedMedias) || index >= len(negotiated) || channel.MediaID != expectedMedias[index] || negotiated[index] != [2]int{channel.RTPChannel, channel.RTCPChannel} {
			return Report{}, ErrInvalid
		}
	}
	if udp {
		if err := replayRTSPUDP(&report, metadata, session, entries, responses, states, &events, visit); err != nil {
			return Report{}, err
		}
	}
	addRTSPFindings(&report, events, states)
	addRTSPPreviewFinding(&report)
	return report, nil
}

func appendUnique(values []string, value string) []string {
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}

func readRTSPSpans(dir string, observation Observation, bytes int64, connections int) ([]rtspWireSpan, error) {
	file, err := os.Open(filepath.Join(dir, timelineName))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(io.LimitReader(file, maxTimeline+1))
	scanner.Buffer(make([]byte, 1024), 1024)
	spans := []rtspWireSpan{}
	offset, previous := int64(0), int64(0)
	for scanner.Scan() {
		var span rtspWireSpan
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&span) != nil || decoder.Decode(new(any)) != io.EOF || span.Offset != offset || span.Size <= 0 || int64(span.Size) > bytes-offset || span.ArrivalMS < previous || span.ArrivalMS > observation.ElapsedMS || span.ConnectionID != 1 || connections != 1 {
			return nil, ErrInvalid
		}
		spans = append(spans, span)
		offset += int64(span.Size)
		previous = span.ArrivalMS
	}
	if scanner.Err() != nil || offset != bytes {
		return nil, ErrInvalid
	}
	return spans, nil
}

func validRTSPRequests(requests []RTSPRequestTiming, elapsed int64, connections int) error {
	previous := int64(0)
	seen := map[int]bool{}
	for index, request := range requests {
		if request.Number != index+1 || request.ConnectionID != 1 || connections != 1 || request.CSeq < 1 || seen[request.CSeq] || request.StartMS < previous || request.EndMS < request.StartMS || request.EndMS > elapsed || request.Complete != (request.StatusCode != 0) || request.StatusCode < 0 || request.StatusCode > 599 || request.Complete && request.StatusCode < 100 || !validCaptureFailure(request.FailureCode) {
			return ErrInvalid
		}
		switch request.Method {
		case "OPTIONS", "DESCRIBE", "SETUP", "PLAY", "TEARDOWN", "GET_PARAMETER", "SET_PARAMETER", "OTHER":
		default:
			return ErrInvalid
		}
		previous, seen[request.CSeq] = request.StartMS, true
	}
	return nil
}
