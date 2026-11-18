package diagnostic

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"streamanalyzer/internal/rtmpraw"

	"github.com/yutopp/go-rtmp"
	"github.com/yutopp/go-rtmp/message"
)

func TestRTMPDiagnosticCaptureAndReplay(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	playSent := make(chan struct{})
	var recording *rtmpTestRecording
	connected := make(chan struct{})
	server := rtmp.NewServer(&rtmp.ServerConfig{OnConnect: func(conn net.Conn) (io.ReadWriteCloser, *rtmp.ConnConfig) {
		recording = &rtmpTestRecording{Conn: conn, playSent: playSent}
		close(connected)
		return recording, &rtmp.ConnConfig{Handler: &rtmpTestHandler{playSent: playSent}}
	}})
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		if err := <-served; !errors.Is(err, rtmp.ErrClosed) {
			t.Error(err)
		}
	})

	dir := t.TempDir()
	source := &url.URL{Scheme: "rtmp", Host: listener.Addr().String(), Path: "/live/stream", RawQuery: "token=test-secret"}
	report, err := sampleRTMP(t.Context(), dir, source, 1)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	default:
		t.Fatal("RTMP server did not accept capture connection")
	}
	if report.Manifest.Observation.Origin != "rtmp://"+source.Host || report.Manifest.Observation.EndReason != "duration_limit" {
		t.Fatalf("unexpected RTMP observation: %+v", report.Manifest.Observation)
	}
	if report.Metrics.VideoFrames != 2 || report.Metrics.AudioFrames != 1 || report.Manifest.Observation.TTFBMS == nil {
		t.Fatalf("unexpected RTMP metadata: %+v", report)
	}
	var stages []string
	for _, stage := range report.Manifest.Observation.Stages {
		stages = append(stages, stage.Name)
	}
	for _, name := range []string{"connect", "handshake", "app_connect", "create_stream", "play", "receive"} {
		if !slices.Contains(stages, name) {
			t.Fatalf("missing RTMP phase %s in %v", name, stages)
		}
	}
	if err := validateObservationDetails(report.Manifest.Observation); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Metrics.VideoCodecs, []string{"h264"}) || !reflect.DeepEqual(report.Metrics.AudioCodecs, []string{"aac"}) {
		t.Fatalf("unexpected RTMP codecs: video=%v audio=%v", report.Metrics.VideoCodecs, report.Metrics.AudioCodecs)
	}
	dump, err := os.ReadFile(filepath.Join(dir, rtmpStreamName))
	if err != nil {
		t.Fatal(err)
	}
	recording.mu.Lock()
	match := bytes.Equal(dump, recording.raw.Bytes())
	recording.mu.Unlock()
	if !match || len(dump) <= rtmpraw.DefaultHandshakeSkipBytes || dump[0] != 3 {
		t.Fatal("RTMP dump differs from recorded server bytes")
	}
	if len(report.Findings) != 1 || report.Findings[0].Code != "video_dts_gap" || len(report.Findings[0].Evidence) != 2 {
		t.Fatalf("unexpected RTMP findings: %+v", report.Findings)
	}
	for _, evidence := range report.Findings[0].Evidence {
		if evidence.Offset < rtmpraw.DefaultHandshakeSkipBytes || evidence.Offset+int64(evidence.Size) > int64(len(dump)) {
			t.Fatalf("invalid RTMP frame byte range: %+v", evidence)
		}
	}
	if err := persistReport(dir, report); err != nil {
		t.Fatal(err)
	}
	var bundle bytes.Buffer
	if err := writeBundle(&bundle, dir, report); err != nil {
		t.Fatal(err)
	}
	importDir := t.TempDir()
	replayed, err := readBundle(importDir, &bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replayed, report) {
		t.Fatalf("replayed report differs: %+v", replayed)
	}
	imported, err := os.ReadFile(filepath.Join(importDir, rtmpStreamName))
	if err != nil || !bytes.Equal(imported, dump) {
		t.Fatalf("round-trip RTMP dump mismatch: %v", err)
	}
}

func TestRTMPSCaptureIdentifiesTLSFailureStage(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	target, err := url.Parse("rtmps" + strings.TrimPrefix(server.URL, "https") + "/live/stream?token=private-fixture")
	if err != nil {
		t.Fatal(err)
	}
	report, err := sampleRTMP(t.Context(), t.TempDir(), target, 2)
	if err != nil {
		t.Fatal(err)
	}
	observation := report.Manifest.Observation
	if observation.FailureStage != "tls" || observation.FailureCode != "tls_error" {
		t.Fatalf("incorrect TLS setup failure: %+v", observation)
	}
	if err := validateObservationDetails(observation); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(observation)
	if strings.Contains(string(data), "private-fixture") {
		t.Fatal("RTMPS timing exposed a signed playback query")
	}
}

func TestRTMPDiagnosticCancelsStalledHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			close(accepted)
			_, _ = io.Copy(io.Discard, conn)
		}
	}()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		select {
		case <-accepted:
			cancel()
		case <-ctx.Done():
		}
	}()
	source := &url.URL{Scheme: "rtmp", Host: listener.Addr().String(), Path: "/live/stream"}
	report, err := sampleRTMP(ctx, t.TempDir(), source, 10)
	if err != nil {
		t.Fatal(err)
	}
	if report.Manifest.Observation.EndReason != "canceled" || report.Manifest.Stream.Bytes != 0 {
		t.Fatalf("unexpected stalled handshake result: %+v", report.Manifest)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("cancel did not close the stalled connection")
	}
}

func TestRTMPDiagnosticReplayAggregate(t *testing.T) {
	raw := make([]byte, rtmpraw.DefaultHandshakeSkipBytes)
	raw[0] = 3
	var payload bytes.Buffer
	writeTag := func(kind byte, stamp uint32, data []byte) {
		payload.Write([]byte{kind, 0, 0, byte(len(data)), 0, byte(stamp >> 8), byte(stamp), 0, 0, 0, 0})
		payload.Write(data)
		_ = binary.Write(&payload, binary.BigEndian, uint32(11+len(data)))
	}
	writeTag(9, 50, []byte{0x17, 1, 0, 0, 3, 0x65})
	writeTag(8, 70, []byte{0xaf, 1, 0x01})
	raw = append(raw, 6, 0, 3, 0xe8, 0, 0, byte(payload.Len()), byte(message.TypeIDAggregateMessage), 1, 0, 0, 0)
	raw = append(raw, payload.Bytes()...)
	stamps := []rtmpReadStamp{{EndOffset: int64(len(raw)), ArrivalMS: 22}}
	events, err := parseRTMPCapture(bytes.NewReader(raw), stamps)
	if err != nil || len(events) != 2 {
		t.Fatalf("aggregate parse = %v, %v", events, err)
	}
	if events[0].Kind != "video" || events[0].DTS != 1000 || events[0].PTS != 1003 || !events[0].Keyframe ||
		events[1].Kind != "audio" || events[1].DTS != 1020 || events[1].Offset != rtmpraw.DefaultHandshakeSkipBytes || events[1].ArrivalMS != 22 {
		t.Fatalf("unexpected aggregate timing or provenance: %+v", events)
	}
}

type rtmpTestHandler struct {
	rtmp.DefaultHandler
	conn     *rtmp.Conn
	playSent <-chan struct{}
}

func (h *rtmpTestHandler) OnServe(conn *rtmp.Conn) { h.conn = conn }

func (h *rtmpTestHandler) OnPlay(stream *rtmp.StreamContext, _ uint32, _ *message.NetStreamPlay) error {
	go func() {
		select {
		case <-h.playSent:
		case <-time.After(time.Second):
			return
		}
		video := append([]byte{0x17, 1, 0, 0, 0}, bytes.Repeat([]byte{0x65}, 260)...)
		for _, entry := range []struct {
			chunk     int
			timestamp uint32
			message   message.Message
		}{
			{6, 0x1000010, &message.VideoMessage{Payload: bytes.NewReader(video)}},
			{5, 0x100001a, &message.AudioMessage{Payload: bytes.NewReader([]byte{0xaf, 1, 0x01})}},
			{6, 0x1000010 + 3000, &message.VideoMessage{Payload: bytes.NewReader(video)}},
		} {
			if err := h.conn.Write(context.Background(), entry.chunk, entry.timestamp, &rtmp.ChunkMessage{StreamID: stream.StreamID, Message: entry.message}); err != nil {
				return
			}
		}
	}()
	return nil
}

type rtmpTestRecording struct {
	net.Conn
	mu       sync.Mutex
	raw      bytes.Buffer
	playSent chan struct{}
	once     sync.Once
}

func (c *rtmpTestRecording) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.raw.Write(p[:n])
	if bytes.Contains(c.raw.Bytes(), []byte("NetStream.Play.Start")) {
		c.once.Do(func() { close(c.playSent) })
	}
	return n, err
}
