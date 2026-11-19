package diagnostic

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
)

func TestRTSPSequenceWrapLateBackfillDuplicateAndBoundedHistory(t *testing.T) {
	state := &rtspSequenceState{}
	metrics := &RTSPTrackMetrics{}
	for _, packet := range []struct {
		sequence  uint16
		timestamp uint32
	}{{65534, 0}, {65535, 100}, {0, 200}, {2, 400}, {1, 300}, {2, 400}} {
		state.observe(packet.sequence, packet.timestamp, metrics)
	}
	if metrics.SequenceGaps != 1 || metrics.MissingSequencePackets != 0 || metrics.OutOfOrderPackets != 1 || metrics.DuplicatePackets != 1 || metrics.SequenceResets != 0 || metrics.TimestampRegressions != 0 {
		t.Fatalf("sequence wrap/late packet was counted as permanent TCP loss or timestamp reset: %+v", metrics)
	}
	state.observe(3, 50, metrics)
	if metrics.TimestampRegressions != 1 {
		t.Fatal("forward packet timestamp regression lost")
	}
	state.observe(5000, 100, metrics)
	if metrics.SequenceResets != 1 || metrics.MissingSequencePackets != 0 {
		t.Fatalf("large sequence discontinuity manufactured thousands of missing packets: %+v", metrics)
	}
	for sequence := uint16(5001); sequence < 10000; sequence++ {
		state.observe(sequence, uint32(sequence), metrics)
	}
	if len(state.seen) > rtspSequenceWindow+1 || len(state.missing) > rtspSequenceWindow {
		t.Fatal("long replay retained an unbounded sequence history")
	}
}

func TestRTSPControlledCodecAndFailureClassificationNeverReturnSourceStrings(t *testing.T) {
	for _, test := range []struct {
		codec    format.Format
		expected string
	}{
		{&format.H264{}, "h264"}, {&format.H265{}, "h265"}, {&format.MPEG4Audio{}, "aac"}, {&format.Opus{}, "opus"}, {&format.G711{MULaw: true}, "pcmu"}, {&format.G711{MULaw: false}, "pcma"}, {&format.Generic{RTPMa: "private-camera.example/90000", ClockRat: 90000}, "unknown"},
	} {
		if actual := rtspCodec(test.codec); actual != test.expected {
			t.Fatalf("uncontrolled/incorrect codec label: %q expected %q", actual, test.expected)
		}
	}
	for _, test := range []struct {
		err      error
		expected string
	}{
		{liberrors.ErrClientBadStatusCode{Code: base.StatusUnauthorized, Message: "private auth realm"}, "rtsp_auth_error"},
		{liberrors.ErrClientBadStatusCode{Code: base.StatusUnsupportedTransport, Message: "private transport"}, "rtsp_transport_error"},
		{liberrors.ErrClientBadStatusCode{Code: base.StatusNotFound, Message: "rtsp://user:password@private-camera/path?token=secret"}, "rtsp_status"},
		{liberrors.ErrClientSDPInvalid{Err: errors.New("private SDP control URL")}, "rtsp_parse_error"},
		{liberrors.ErrClientRequestTimedOut{}, "timeout"},
	} {
		stage, code := rtspFailure("rtsp_describe", test.err)
		if stage != "rtsp_describe" || code != test.expected || strings.Contains(stage+code, "private") {
			t.Fatalf("uncontrolled terminal error: %q %q", stage, code)
		}
	}
}

func TestRTSPCanceledDialCannotRegisterConnectionAfterShutdownSnapshot(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			accepted <- connection
		}
	}()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	capture := &rtspCapture{ctx: ctx, start: time.Now()}
	// Block the post-connect phase recorder. This places cancellation exactly
	// after TCP succeeds but before connection registration, the race that
	// otherwise lets a late connection escape the one-shot shutdown callback.
	capture.mu.Lock()
	done := make(chan error, 1)
	go func() {
		connection, err := capture.dial(t.Context(), "tcp", listener.Addr().String(), nil, false)
		if connection != nil {
			connection.Close()
		}
		done <- err
	}()
	var peer net.Conn
	select {
	case peer = <-accepted:
	case <-time.After(time.Second):
		capture.mu.Unlock()
		t.Fatal("TCP fixture did not connect")
	}
	defer peer.Close()
	cancel()
	capture.mu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("late dial was registered after cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled dial did not return")
	}
	if capture.metadata.Connections != 0 || len(capture.connections) != 0 {
		t.Fatal("canceled connection entered the live connection ledger")
	}
	peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("late connection was not closed: %v", err)
	}
}
