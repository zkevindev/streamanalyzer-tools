package appserver

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/mediasource"

	"github.com/yutopp/go-rtmp"
	"github.com/yutopp/go-rtmp/message"
)

type relayVideo struct {
	timestamp uint32
	payload   []byte
}

type relayReader struct {
	rtmp.DefaultHandler
	video chan relayVideo
}

func (r *relayReader) OnVideo(timestamp uint32, payload io.Reader) error {
	data, err := io.ReadAll(payload)
	if err == nil {
		r.video <- relayVideo{timestamp, data}
	}
	return err
}

func TestRTMPSimulatorRelaysPublisherKeyframesAndReconnect(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/rtmp"
	status := decodeDiagnostic[mediasource.RTMPStatus](t, diagnosticRequest(t, server, "POST", api+"/start", http.StatusCreated, []byte(`{"fault":"publish_relay","delay_keyframe_ms":500}`)))
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	reader := &relayReader{video: make(chan relayVideo, 12)}
	viewer, _, target, err := rtmp.DialAndPlay(ctx, status.URL, reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	waitStatus := func(ready func(*mediasource.RTMPIngestStatus) bool) *mediasource.RTMPIngestStatus {
		t.Helper()
		for ctx.Err() == nil {
			current := decodeDiagnostic[mediasource.RTMPStatus](t, diagnosticRequest(t, server, "GET", api, http.StatusOK, nil))
			if current.Ingest != nil && ready(current.Ingest) {
				return current.Ingest
			}
			time.Sleep(15 * time.Millisecond)
		}
		t.Fatal("RTMP ingest status did not advance")
		return nil
	}
	waitStatus(func(s *mediasource.RTMPIngestStatus) bool { return s.Viewers == 1 })
	nextVideo := func(want []byte) relayVideo {
		t.Helper()
		select {
		case <-ctx.Done():
			t.Fatal("viewer did not receive expected video")
		case <-viewer.Done():
			t.Fatalf("viewer disconnected: %v", viewer.LastError())
		case packet := <-reader.video:
			if !bytes.Equal(packet.payload, want) {
				t.Fatalf("unexpected frame: %x, want %x", packet.payload, want)
			}
			return packet
		}
		return relayVideo{}
	}
	var lastTimestamp uint32
	for generation := 1; generation <= 2; generation++ {
		publisher, err := rtmp.Dial("rtmp", target.Host, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer publisher.Close()
		if err := publisher.ConnectContext(ctx, &message.NetConnectionConnect{Command: message.NetConnectionConnectCommand{
			App: target.App, TCURL: target.TCURL,
		}}); err != nil {
			t.Fatal(err)
		}
		stream, err := publisher.CreateStreamContext(ctx, nil, 4096)
		if err != nil {
			t.Fatal(err)
		}
		if err := stream.Publish(&message.NetStreamPublish{PublishingName: "test", PublishingType: "live"}); err != nil {
			t.Fatal(err)
		}
		pub := waitStatus(func(s *mediasource.RTMPIngestStatus) bool { return s.Publishing && len(s.Sessions) == generation }).Sessions[generation-1]
		if pub.PublishedAt.IsZero() || !pub.FirstMediaAt.IsZero() {
			t.Fatalf("invalid initial publishing evidence: %+v", pub)
		}
		send := func(stamp uint32, data []byte) {
			t.Helper()
			if err := stream.Write(6, stamp, &message.VideoMessage{Payload: bytes.NewReader(data)}); err != nil {
				t.Fatal(err)
			}
		}
		header := []byte{0x17, 0, 0, 0, 0, 1, byte(generation)}
		key := []byte{0x17, 1, 0, 0, 0, 0, 0, 0, 2, 0x65, byte(generation)}
		send(0, header)
		nextVideo(header)
		send(10, key)
		waitStatus(func(s *mediasource.RTMPIngestStatus) bool { return s.Sessions[generation-1].WithheldKeyframes == 1 })
		select {
		case unexpected := <-reader.video:
			t.Fatalf("withheld frame reached viewer: %+v", unexpected)
		default:
		}
		time.Sleep(max(0, time.Until(pub.PublishedAt.Add(520*time.Millisecond))))
		send(600, key)
		relayed := nextVideo(key)
		if relayed.timestamp <= lastTimestamp {
			t.Fatalf("relay timestamp reset after publisher reconnect: %d <= %d", relayed.timestamp, lastTimestamp)
		}
		lastTimestamp = relayed.timestamp
		if err := publisher.Close(); err != nil {
			t.Fatal(err)
		}
		held := waitStatus(func(s *mediasource.RTMPIngestStatus) bool { return !s.Publishing })
		ended := held.Sessions[generation-1]
		if ended.DisconnectedAt.IsZero() || ended.FirstMediaAt.IsZero() || ended.FirstKeyframeAt.IsZero() ||
			ended.RelayKeyframeAt.Sub(ended.PublishedAt) < 500*time.Millisecond || held.Viewers != 1 {
			t.Fatalf("publisher lifecycle missing evidence: %+v", held)
		}
	}
}
