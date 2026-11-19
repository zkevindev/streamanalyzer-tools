package diagnostic

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/conn"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
)

type rtspUDPTestPacket struct {
	data          []byte
	rtcp, foreign bool
}

// A native server fixture sends exact UDP bytes, including bytes that the
// SDK's parsed/re-marshalled RTP path would change or refuse to deliver.
func rtspUDPTestServer(t *testing.T, packets []rtspUDPTestPacket, reject bool) (*url.URL, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var senders []*net.UDPConn
	for range 3 {
		sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		senders = append(senders, sender)
	}
	target, _ := url.Parse("rtsp://" + listener.Addr().String() + "/private-camera")
	var connections atomic.Int32
	var activeMu sync.Mutex
	var active net.Conn
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			activeMu.Lock()
			active = client
			activeMu.Unlock()
			func() {
				defer client.Close()
				reader := conn.NewConn(bufio.NewReader(client), client)
				var ports [2]int
				for {
					_ = client.SetReadDeadline(time.Now().Add(6 * time.Second))
					value, err := reader.Read()
					if err != nil {
						return
					}
					request, ok := value.(*base.Request)
					if !ok {
						continue
					}
					if request.Method == base.Teardown {
						return
					}
					response := base.Response{StatusCode: base.StatusOK, Header: base.Header{"CSeq": request.Header["CSeq"]}}
					switch request.Method {
					case base.Options:
						response.Header["Public"] = base.HeaderValue{"DESCRIBE, SETUP, PLAY, TEARDOWN"}
					case base.Describe:
						response.Header["Content-Type"] = base.HeaderValue{"application/sdp"}
						response.Header["Content-Base"] = base.HeaderValue{target.String() + "/"}
						response.Body = []byte(rtspIntegrationVideoSDP())
					case base.Setup:
						var transport headers.Transport
						if transport.Unmarshal(request.Header["Transport"]) != nil || transport.Protocol != headers.TransportProtocolUDP || transport.ClientPorts == nil {
							t.Error("explicit UDP capture attempted another transport")
							return
						}
						ports = *transport.ClientPorts
						if reject {
							response.StatusCode = base.StatusUnsupportedTransport
						} else {
							serverPorts := [2]int{senders[0].LocalAddr().(*net.UDPAddr).Port, senders[1].LocalAddr().(*net.UDPAddr).Port}
							transport.ServerPorts = &serverPorts
							response.Header["Transport"] = transport.Marshal()
							response.Header["Session"] = base.HeaderValue{"udp-fixture;timeout=60"}
						}
					case base.Play:
						response.Header["Session"] = base.HeaderValue{"udp-fixture;timeout=60"}
					}
					data, _ := response.Marshal()
					if _, err := client.Write(data); err != nil {
						return
					}
					if request.Method == base.Play {
						// Let the client start its readers, then preserve deterministic
						// order on each socket without relying on TCP packet boundaries.
						time.Sleep(20 * time.Millisecond)
						for _, packet := range packets {
							sender, port := senders[0], ports[0]
							if packet.rtcp {
								sender, port = senders[1], ports[1]
							}
							if packet.foreign {
								sender = senders[2]
							}
							_, _ = sender.WriteToUDP(packet.data, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
							time.Sleep(3 * time.Millisecond)
						}
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		activeMu.Lock()
		if active != nil {
			_ = active.Close()
		}
		activeMu.Unlock()
		for _, sender := range senders {
			_ = sender.Close()
		}
		group.Wait()
	})
	return target, &connections
}

func TestRTSPUDPOriginalDatagramsAndSequenceReplay(t *testing.T) {
	var packets []rtspUDPTestPacket
	for index, sequence := range []uint16{65534, 65535, 0, 2, 1, 2} {
		packets = append(packets, rtspUDPTestPacket{data: rtspIntegrationRTP(96, sequence, uint32(index*3600), 123, []byte{0x61, 0x88}, index == 0)})
	}
	packets = append(packets,
		rtspUDPTestPacket{data: rtspIntegrationRTP(96, 100, 0, 456, []byte{0x65, 0x88}, false)},
		rtspUDPTestPacket{data: rtspIntegrationRTP(96, 101, 3600, 456, bytes.Repeat([]byte{0x61}, 3000), true)},
		rtspUDPTestPacket{data: rtspIntegrationRTP(111, 102, 7200, 456, []byte{1}, false)},
		rtspUDPTestPacket{data: []byte{}},
		rtspUDPTestPacket{data: rtspIntegrationRTP(96, 999, 0, 999, []byte{0x65}, false), foreign: true},
		rtspUDPTestPacket{data: []byte{0x80, 201, 0, 1, 0, 0, 0, 123}, rtcp: true},
	)
	target, _ := rtspUDPTestServer(t, packets, false)
	dir := t.TempDir()
	report, err := sampleRTSPTransport(context.Background(), dir, target, 1, ProtocolRTSP, "udp")
	if err != nil {
		t.Fatal(err)
	}
	metrics := report.RTSP
	if metrics.Transport != "udp" || metrics.UDPDatagrams != len(packets) || metrics.AcceptedDatagrams != len(packets)-1 || metrics.FilteredDatagrams != 1 || metrics.UDPOversizeDatagrams != 1 || metrics.MalformedPackets != 1 || metrics.RTPPackets != 9 || metrics.RTCPPackets != 1 || metrics.FirstRTCPMS == nil {
		t.Fatalf("wrong UDP accounting: %+v", metrics)
	}
	track := metrics.Tracks[0]
	if track.RTPPackets != 8 || track.SSRCChanges != 1 || track.DuplicatePackets != 1 || track.OutOfOrderPackets != 1 || track.SequenceGaps != 1 || track.MissingSequencePackets != 0 || metrics.UnknownPayloadPackets != 1 {
		t.Fatalf("wrong original sequence accounting: %+v", track)
	}
	if report.Metrics.VideoFrames != 0 || report.Metrics.AudioFrames != 0 {
		t.Fatal("RTP packets were invented as decoded frames")
	}
	raw, err := os.ReadFile(filepath.Join(dir, rtspStreamName))
	if err != nil {
		t.Fatal(err)
	}
	files := rtspIntegrationZIP(t, raw)
	if len(files) != 5 || !bytes.Equal(files[rtspSDPName], []byte(rtspIntegrationVideoSDP())) {
		t.Fatal("UDP archive lost exact SDP or required entries")
	}
	index := bufio.NewScanner(bytes.NewReader(files[rtspUDPIndexName]))
	var spans []rtspUDPSpan
	for index.Scan() {
		var span rtspUDPSpan
		if err := json.Unmarshal(index.Bytes(), &span); err != nil {
			t.Fatal(err)
		}
		spans = append(spans, span)
	}
	for index, span := range spans {
		if !bytes.Equal(files[rtspUDPDataName][span.Offset:span.Offset+int64(span.Size)], packets[index].data) {
			t.Fatalf("original datagram %d changed (padding/extensions or oversized packet)", index)
		}
	}
	var mediaEvents []Event
	if err := walkRTSPMediaEvents(dir, report.Manifest, func(event Event) { mediaEvents = append(mediaEvents, event) }); err != nil {
		t.Fatal(err)
	}
	if len(mediaEvents) != track.RTPPackets {
		t.Fatal("walker included filtered, RTCP, malformed or unknown-PT datagrams")
	}
	for _, event := range mediaEvents {
		if event.Kind != "video_rtp" || event.Artifact != rtspUDPDataName || event.Track != 1 || event.RTPSequence == nil || event.Keyframe {
			t.Fatalf("walker fabricated packet semantics: %+v", event)
		}
	}
	replayed, err := replayRTSP(dir, report.Manifest)
	if err != nil || !reflect.DeepEqual(report, replayed) {
		t.Fatalf("offline UDP report changed: %v", err)
	}
	t.Run("unnegotiated_socket_retains_stray_datagram", func(t *testing.T) {
		changed := map[string][]byte{}
		for key, value := range files {
			changed[key] = bytes.Clone(value)
		}
		var metadata rtspArchiveMetadata
		_ = json.Unmarshal(changed[rtspMetadataName], &metadata)
		id := len(metadata.UDPSockets) + 1
		metadata.UDPSockets = append(metadata.UDPSockets, rtspUDPSocket{ID: id, LocalPort: 12345, ReaderBufferBytes: 2048})
		changed[rtspMetadataName], _ = json.Marshal(metadata)
		altered := append([]rtspUDPSpan(nil), spans...)
		altered[0].SocketID = id
		var encoded bytes.Buffer
		for _, span := range altered {
			_ = json.NewEncoder(&encoded).Encode(span)
		}
		changed[rtspUDPIndexName] = encoded.Bytes()
		rtspUDPTestWriteArchive(t, dir, changed)
		result, err := replayRTSP(dir, report.Manifest)
		if err != nil || result.RTSP.UDPDatagrams != metrics.UDPDatagrams || result.RTSP.FilteredDatagrams != metrics.FilteredDatagrams+1 || result.RTSP.RTPPackets != metrics.RTPPackets-1 {
			t.Fatalf("stray data before failed SETUP made the capture unreplayable: %v", err)
		}
	})

	// Every mutation is re-archived with a valid ZIP checksum; rejection must
	// come from semantic proof, not a damaged compressed stream.
	for _, name := range []string{"wrong_source_port", "bad_offset", "future_arrival", "unknown_socket", "negative_size", "extra_index_field", "missing_entry", "wrong_version", "bad_socket_buffer"} {
		t.Run(name, func(t *testing.T) {
			changed := map[string][]byte{}
			for key, value := range files {
				changed[key] = bytes.Clone(value)
			}
			var metadata rtspArchiveMetadata
			_ = json.Unmarshal(changed[rtspMetadataName], &metadata)
			altered := append([]rtspUDPSpan(nil), spans...)
			switch name {
			case "wrong_source_port":
				metadata.UDPTransports[0].RTPServerPort = metadata.UDPTransports[0].RTPServerPort%65535 + 1
			case "bad_offset":
				altered[1].Offset++
			case "future_arrival":
				altered[len(altered)-1].ArrivalMS = report.Manifest.Observation.ElapsedMS + 1
			case "unknown_socket":
				altered[0].SocketID = len(metadata.UDPSockets) + 1
			case "negative_size":
				altered[0].Size = -1
			case "wrong_version":
				metadata.Version = 1
			case "bad_socket_buffer":
				metadata.UDPSockets[altered[0].SocketID-1].ReaderBufferBytes = 0
			}
			changed[rtspMetadataName], _ = json.Marshal(metadata)
			var encoded bytes.Buffer
			encoder := json.NewEncoder(&encoded)
			for _, span := range altered {
				_ = encoder.Encode(span)
			}
			changed[rtspUDPIndexName] = encoded.Bytes()
			if name == "extra_index_field" {
				changed[rtspUDPIndexName] = bytes.Replace(changed[rtspUDPIndexName], []byte("{\"offset\""), []byte("{\"unknown\":1,\"offset\""), 1)
			}
			if name == "missing_entry" {
				delete(changed, rtspUDPIndexName)
			}
			rtspUDPTestWriteArchive(t, dir, changed)
			if _, err := replayRTSP(dir, report.Manifest); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted inconsistent UDP evidence: %v", err)
			}
		})
	}
}

func rtspUDPTestWriteArchive(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for _, name := range []string{rtspWireName, rtspSDPName, rtspMetadataName, rtspUDPDataName, rtspUDPIndexName} {
		value, exists := files[name]
		if !exists {
			continue
		}
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rtspStreamName), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRTSPUDPNoFallbackAndBlackhole(t *testing.T) {
	for _, reject := range []bool{true, false} {
		t.Run(strconv.FormatBool(reject), func(t *testing.T) {
			target, connections := rtspUDPTestServer(t, nil, reject)
			report, err := sampleRTSPTransport(context.Background(), t.TempDir(), target, 4, ProtocolRTSP, "udp")
			if err != nil {
				t.Fatal(err)
			}
			if connections.Load() != 1 || report.RTSP.Transport != "udp" || report.RTSP.RTPPackets != 0 || report.RTSP.FirstRTPMS != nil {
				t.Fatalf("UDP silently fell back or invented media: %+v", report.RTSP)
			}
			if reject {
				if report.Manifest.Observation.FailureStage != "rtsp_setup" || report.Manifest.Observation.FailureCode != "rtsp_transport_error" {
					t.Fatalf("bad SETUP attribution: %+v", report.Manifest.Observation)
				}
			} else {
				if report.Manifest.Observation.FailureStage != "rtsp_receive" || report.Manifest.Observation.FailureCode != "timeout" {
					t.Fatalf("UDP blackhole was classified as control/connect failure: %+v", report.Manifest.Observation)
				}
				found := false
				for _, finding := range report.Findings {
					found = found || finding.Code == "rtsp_udp_no_rtp"
				}
				if !found {
					t.Fatal("PLAY-success UDP blackhole has no actionable finding")
				}
			}
		})
	}
}

func TestRTSPUDPMetadataBindsSDPMediaToSetupOrder(t *testing.T) {
	session, _, err := rtspDescription([]byte(rtspIntegrationMulticodecSDP()))
	if err != nil {
		t.Fatal(err)
	}
	metadata := rtspArchiveMetadata{Version: 2, Connections: 1, Transport: "udp", ControlPeerIP: "127.0.0.1"}
	responses := map[int]*base.Response{}
	for index := range 2 {
		ports := [2]int{5000 + index*2, 5001 + index*2}
		serverPorts := [2]int{6000 + index*2, 6001 + index*2}
		transport := headers.Transport{Protocol: headers.TransportProtocolUDP, ClientPorts: &ports, ServerPorts: &serverPorts}
		cseq := index + 3
		metadata.Requests = append(metadata.Requests, RTSPRequestTiming{Method: "SETUP", CSeq: cseq, Complete: true, StatusCode: 200})
		metadata.UDPSockets = append(metadata.UDPSockets, rtspUDPSocket{ID: index*2 + 1, LocalPort: ports[0]}, rtspUDPSocket{ID: index*2 + 2, LocalPort: ports[1]})
		metadata.UDPTransports = append(metadata.UDPTransports, rtspUDPTransport{MediaID: index + 1, RTPSocketID: index*2 + 1, RTCPSocketID: index*2 + 2, SourceIP: "127.0.0.1", RTPServerPort: serverPorts[0], RTCPServerPort: serverPorts[1], CSeq: cseq})
		responses[cseq] = &base.Response{StatusCode: base.StatusOK, Header: base.Header{"Transport": transport.Marshal()}}
	}
	if _, err := validRTSPUDPMetadata(metadata, session, responses); err != nil {
		t.Fatal(err)
	}
	metadata.UDPTransports[0], metadata.UDPTransports[1] = metadata.UDPTransports[1], metadata.UDPTransports[0]
	metadata.UDPTransports[0].MediaID, metadata.UDPTransports[1].MediaID = 1, 2
	if _, err := validRTSPUDPMetadata(metadata, session, responses); !errors.Is(err, ErrInvalid) {
		t.Fatal("same-PT H264/H265 SETUP sockets could be swapped between media")
	}
}

// Compile-time proof that the SDK receives the capabilities required for UDP
// socket setup; wrapping only net.PacketConn would panic inside gortsplib.
var _ interface {
	net.PacketConn
	SetReadBuffer(int) error
} = (*rtspUDPCaptureConn)(nil)

func TestRTSPMediaWalkerPreservesTCPReportAndOriginalLocations(t *testing.T) {
	packet := rtspIntegrationRTP(96, 65535, 0, 100, []byte{0x65, 0x88}, true)
	fixture := newRTSPIntegrationFixture(t, rtspIntegrationConfig{packets: []rtspIntegrationPacket{{raw: packet}}, closeAfterPlay: true})
	target, _ := url.Parse(fixture.address())
	dir := t.TempDir()
	report, err := sampleRTSP(context.Background(), dir, target, 1, ProtocolRTSP)
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	withEvents, err := replayRTSPWithEvents(dir, report.Manifest, func(event Event) { events = append(events, event) })
	if err != nil || !reflect.DeepEqual(report, withEvents) || len(events) != 1 {
		t.Fatalf("walker changed the legacy report: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, rtspStreamName))
	files := rtspIntegrationZIP(t, raw)
	event := events[0]
	if event.Kind != "video_rtp" || event.Artifact != rtspWireName || event.RTPSequence == nil || *event.RTPSequence != 65535 || !bytes.Equal(files[rtspWireName][event.Offset:event.Offset+int64(event.Size)], packet) {
		t.Fatalf("walker lost exact TCP packet location: %+v", event)
	}
}

func TestRTSPUDPBoundsRejectWholeDatagramWithoutPartialEvidence(t *testing.T) {
	for _, limit := range []string{"index", "combined_bytes"} {
		t.Run(limit, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			capture := &rtspCapture{ctx: ctx, cancel: cancel, start: time.Now()}
			var err error
			capture.wire, err = newArtifact(dir, rtspWireName)
			if err != nil {
				t.Fatal(err)
			}
			capture.udpData, _ = newArtifact(dir, rtspUDPDataName)
			capture.udpIndex, _ = newArtifact(dir, rtspUDPIndexName)
			defer capture.wire.close()
			defer capture.udpData.close()
			defer capture.udpIndex.close()
			reason := "timeline_limit"
			if limit == "index" {
				capture.udpIndex.bytes = maxRTSPUDPIndexBytes - 1
			} else {
				reason = "size_limit"
				capture.wire.bytes = maxStreamBytes - (1 << 20) - 1
			}
			if capture.keepUDP([]byte{1, 2, 3}, 1, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 6000}) == nil || capture.reason != reason || ctx.Err() == nil {
				t.Fatal("capture did not stop at the shared raw evidence bound")
			}
			for _, name := range []string{rtspUDPDataName, rtspUDPIndexName} {
				stat, err := os.Stat(filepath.Join(dir, name))
				if err != nil || stat.Size() != 0 {
					t.Fatal("bounded capture retained a partial datagram/index")
				}
			}
		})
	}
}
