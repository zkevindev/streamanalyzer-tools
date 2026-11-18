package diagnostic

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestTSInputHTTPCaptureRetainsShortResponse(t *testing.T) {
	packet := append([]byte{0x47, 0x1f, 0xff, 0x10}, bytes.Repeat([]byte{0xff}, 184)...)
	payload := bytes.Repeat(packet, 400)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)+188))
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL + "/live.ts?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	report, err := sampleTS(t.Context(), server.Client(), dir, target, 2, ProtocolHTTPTS)
	if err != nil {
		t.Fatal(err)
	}
	obs := report.Manifest.Observation
	if obs.Origin != server.URL || obs.HTTPStatus != 200 || obs.EndReason != "body_error" ||
		obs.FailureStage != "receive" || obs.FirstPayloadMS == nil || obs.TTFBMS == nil {
		t.Fatalf("unexpected HTTP TS capture observation: %+v", obs)
	}
	var captured bytes.Buffer
	var spanCount int
	err = walkTSChunks(dir, report.Manifest, func(_ tsSpan, data []byte) error {
		spanCount++
		_, err := captured.Write(data)
		return err
	})
	if err != nil || spanCount < 2 || !bytes.Equal(captured.Bytes(), payload) {
		t.Fatalf("HTTP TS prefix differs: spans=%d bytes=%d err=%v", spanCount, captured.Len(), err)
	}
}

func TestTSInputDatagramsPreserveWireBoundaries(t *testing.T) {
	for _, tc := range []struct {
		protocol string
		scheme   string
	}{
		{ProtocolUDPTS, "udp"},
		{ProtocolRTPTS, "rtp"},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			target := &url.URL{Scheme: tc.scheme, Host: "127.0.0.1:0"}
			receiver, err := listenTSDatagrams(target)
			if err != nil {
				t.Fatal(err)
			}
			defer receiver.Close()
			sender, err := net.DialUDP("udp4", nil, receiver.LocalAddr().(*net.UDPAddr))
			if err != nil {
				t.Fatal(err)
			}
			defer sender.Close()
			packets := [][]byte{{0x47, 1, 0, 0x10}, {0x47, 2, 0, 0x11, 0x20}}
			if tc.protocol == ProtocolRTPTS {
				for i := range packets {
					header := []byte{0x80, 33, 0, byte(i), 0, 0, 0, 0, 1, 2, 3, 4}
					packets[i] = append(header, packets[i]...)
				}
			}
			// Queue both packets before the sampler starts reading. As a result,
			// timestamps may be identical without merging datagram boundaries.
			for _, packet := range packets {
				if _, err := sender.Write(packet); err != nil {
					t.Fatal(err)
				}
			}

			dir := t.TempDir()
			stream, err := newArtifact(dir, dumpName(tc.protocol))
			if err != nil {
				t.Fatal(err)
			}
			defer stream.file.Close()
			timeline, err := newArtifact(dir, timelineName)
			if err != nil {
				t.Fatal(err)
			}
			defer timeline.file.Close()
			start := time.Now()
			obs := Observation{StartedAt: start.UTC(), RequestedSeconds: 1}
			capture := tsCapture{stream: stream, timeline: timeline, observation: &obs, start: start}
			ctx, cancel := context.WithTimeout(t.Context(), 120*time.Millisecond)
			defer cancel()
			if err := errors.Join(capture.receiveDatagrams(ctx, receiver), stream.close(), timeline.close()); err != nil {
				t.Fatal(err)
			}
			obs.ElapsedMS = time.Since(start).Milliseconds()
			if obs.EndReason != "duration_limit" || obs.FirstPayloadMS == nil {
				t.Fatalf("unexpected datagram observation: %+v", obs)
			}
			manifest := Manifest{Protocol: tc.protocol, Observation: obs, Stream: stream.artifact(), Timeline: timeline.artifact()}
			i, offset := 0, int64(0)
			err = walkTSChunks(dir, manifest, func(span tsSpan, data []byte) error {
				if i >= len(packets) || span.Offset != offset || !bytes.Equal(packets[i], data) {
					t.Fatalf("datagram %d differs: span=%+v bytes=%x", i, span, data)
				}
				i++
				offset += int64(len(data))
				return nil
			})
			if err != nil || i != len(packets) {
				t.Fatalf("datagram replay lost boundaries: packets=%d err=%v", i, err)
			}
		})
	}
}

func TestTSInputRejectsBrokenDatagramBoundary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, dumpName(ProtocolUDPTS)), []byte{0x47}, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Protocol: ProtocolUDPTS, Observation: Observation{ElapsedMS: 10}, Stream: Artifact{Bytes: 1}}
	for _, row := range []string{
		`{"offset":1,"size":1,"arrival_ms":0}`,
		`{"offset":0,"size":1}`,
		`{"offset":0,"size":65508,"arrival_ms":0}`,
		`{"offset":0,"size":1,"arrival_ms":11}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, timelineName), []byte(row+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := walkTSChunks(dir, manifest, func(tsSpan, []byte) error { return nil }); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid timeline %s was accepted: %v", row, err)
		}
	}
}

func TestTSInputClassifiesUnicastBindFailure(t *testing.T) {
	occupied, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	target := &url.URL{Scheme: "udp", Host: occupied.LocalAddr().String()}
	report, err := sampleTS(t.Context(), nil, t.TempDir(), target, 1, ProtocolUDPTS)
	if err != nil {
		t.Fatal(err)
	}
	obs := report.Manifest.Observation
	if obs.EndReason != "udp_bind_error" || obs.FailureStage != "listen" || obs.FailureCode != "udp_bind_error" ||
		len(obs.Stages) != 1 || obs.Stages[0].Name != "listen" || report.Manifest.Stream.Bytes != 0 {
		t.Fatalf("unexpected bind failure metadata: %+v", obs)
	}
}

func TestTSInputRetainsNoDataObservation(t *testing.T) {
	target := &url.URL{Scheme: "udp", Host: "127.0.0.1:0"}
	report, err := sampleTS(t.Context(), nil, t.TempDir(), target, 1, ProtocolUDPTS)
	if err != nil {
		t.Fatal(err)
	}
	obs := report.Manifest.Observation
	if obs.EndReason != "duration_limit" || obs.FirstPayloadMS != nil || obs.FailureStage != "receive" || obs.FailureCode != "timeout" ||
		len(obs.Stages) != 2 || obs.Stages[0].Name != "listen" || obs.Stages[1].Name != "receive" ||
		report.Manifest.Stream.Bytes != 0 || report.Manifest.Timeline.Bytes != 0 {
		t.Fatalf("unexpected empty capture metadata: %+v", obs)
	}
	if status := reportStatus(report); status != "failed" || len(report.Findings) == 0 {
		t.Fatalf("empty input should produce a failed run with findings: status=%s findings=%+v", status, report.Findings)
	}
}
