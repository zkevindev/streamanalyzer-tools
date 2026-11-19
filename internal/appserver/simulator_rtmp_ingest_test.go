package appserver

import (
	"bytes"
	"context"
	"fmt"
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
	audio chan relayVideo
}

func (r *relayReader) OnVideo(timestamp uint32, payload io.Reader) error {
	data, err := io.ReadAll(payload)
	if err == nil {
		r.video <- relayVideo{timestamp, data}
	}
	return err
}

func (r *relayReader) OnAudio(timestamp uint32, payload io.Reader) error {
	data, err := io.ReadAll(payload)
	if err == nil && r.audio != nil {
		r.audio <- relayVideo{timestamp, data}
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

func TestRTMPFailoverWhenPrimaryKeepsConnection(t *testing.T) {
	for _, stable := range []int{0, 700} {
		t.Run(fmt.Sprint(stable), func(t *testing.T) { checkRTMPStableFailback(t, stable, "") })
	}
}

func TestRTMPFailoverWhenVideoStallsAndAudioContinues(t *testing.T) {
	checkRTMPStableFailback(t, 700, "video")
}

func checkRTMPStableFailback(t *testing.T, stable int, stallTrack string) {
	t.Helper()
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/rtmp"
	status := decodeDiagnostic[mediasource.RTMPStatus](t, diagnosticRequest(t, server, "POST", api+"/start", 201,
		fmt.Appendf(nil, `{"fault":"publish_failover","stall_ms":500,"failback_stable_ms":%d,"stall_track":%q}`, stable, stallTrack)))
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	reader := &relayReader{video: make(chan relayVideo, 16), audio: make(chan relayVideo, 16)}
	viewer, _, target, err := rtmp.DialAndPlay(ctx, status.URL, reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	publish := func(name string) (video, audio func(uint32, []byte)) {
		t.Helper()
		pub, err := rtmp.Dial("rtmp", target.Host, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pub.Close() })
		if err := pub.ConnectContext(ctx, &message.NetConnectionConnect{Command: message.NetConnectionConnectCommand{App: target.App, TCURL: target.TCURL}}); err != nil {
			t.Fatal(err)
		}
		stream, err := pub.CreateStreamContext(ctx, nil, 4096)
		if err != nil {
			t.Fatal(err)
		}
		if err := stream.Publish(&message.NetStreamPublish{PublishingName: name, PublishingType: "live"}); err != nil {
			t.Fatal(err)
		}
		video = func(stamp uint32, data []byte) {
			t.Helper()
			if err := stream.Write(6, stamp, &message.VideoMessage{Payload: bytes.NewReader(data)}); err != nil {
				t.Fatal(err)
			}
		}
		audio = func(stamp uint32, data []byte) {
			t.Helper()
			if err := stream.Write(5, stamp, &message.AudioMessage{Payload: bytes.NewReader(data)}); err != nil {
				t.Fatal(err)
			}
		}
		return video, audio
	}
	get := func() *mediasource.RTMPIngestStatus {
		return decodeDiagnostic[mediasource.RTMPStatus](t, diagnosticRequest(t, server, "GET", api, 200, nil)).Ingest
	}
	for get().Viewers != 1 && ctx.Err() == nil {
		time.Sleep(10 * time.Millisecond)
	}
	primary, primaryAudio := publish("test")
	backup, backupAudio := publish("backup")
	nextMedia := func(packets <-chan relayVideo, want []byte) relayVideo {
		t.Helper()
		select {
		case packet := <-packets:
			if !bytes.Equal(packet.payload, want) {
				t.Fatalf("unexpected frame %x, expected %x", packet.payload, want)
			}
			return packet
		case <-viewer.Done():
			t.Fatalf("viewer disconnected: %v", viewer.LastError())
		case <-ctx.Done():
			t.Fatal("viewer did not receive frame")
		}
		return relayVideo{}
	}
	next := func(want []byte) relayVideo {
		t.Helper()
		return nextMedia(reader.video, want)
	}
	mainHeader, backupHeader := []byte{0x17, 0, 0, 0, 0, 1, 1}, []byte{0x17, 0, 0, 0, 0, 1, 2}
	mainKey, backupKey := []byte{0x17, 1, 0, 0, 0, 0, 0, 0, 2, 0x65, 1}, []byte{0x17, 1, 0, 0, 0, 0, 0, 0, 2, 0x65, 2}
	primary(0, mainHeader)
	primary(10, mainKey)
	next(mainHeader)
	main := next(mainKey)
	backup(0, backupHeader)
	backup(10, backupKey)
	var lastOldAudio uint32
	oldAudio, newAudio := []byte{0xaf, 1, 1}, []byte{0xaf, 1, 2}
	keepAudio := func(stamp uint32) {
		t.Helper()
		primaryAudio(stamp, oldAudio)
		sample := nextMedia(reader.audio, oldAudio)
		if sample.timestamp <= lastOldAudio || sample.timestamp <= main.timestamp {
			t.Fatalf("primary audio timeline stopped with video: %d <= %d", sample.timestamp, lastOldAudio)
		}
		lastOldAudio = sample.timestamp
	}
	if stallTrack == "video" {
		for i := range uint32(6) {
			keepAudio(100 + i*100)
			time.Sleep(100 * time.Millisecond)
		}
	} else {
		time.Sleep(560 * time.Millisecond)
	}
	backup(600, []byte{0x27, 1, 0, 0, 0, 0, 0, 0, 2, 0x41, 2})
	for get().Failover.StallDetectedAt.IsZero() && ctx.Err() == nil {
		time.Sleep(10 * time.Millisecond)
	}
	if !get().Failover.SwitchedAt.IsZero() {
		t.Fatal("switched before a keyframe")
	}
	if stallTrack == "video" {
		keepAudio(700)
		pending := get().Failover
		if !pending.PrimaryLastAudioAt.After(pending.StallDetectedAt) {
			t.Fatalf("audio stopped while waiting for backup keyframe: %+v", pending)
		}
	}
	backup(650, backupKey)
	next(backupHeader)
	switched := next(backupKey)
	result := get()
	failover := result.Failover
	if switched.timestamp <= main.timestamp || result.Viewers != 1 || !failover.PrimaryConnected ||
		failover.SwitchedAt.IsZero() || failover.GapMS == nil || *failover.GapMS < 500 || len(result.Sessions) != 2 {
		t.Fatalf("invalid failover: %+v / %+v", result, failover)
	}
	if stallTrack == "video" {
		backupAudio(680, newAudio)
		firstNewAudio := nextMedia(reader.audio, newAudio)
		observed := get().Failover
		change := observed.Switches[0]
		if !observed.PrimaryLastAudioAt.After(observed.PrimaryLastVideoAt) || *observed.GapMS <= change.MediaGapMS ||
			firstNewAudio.timestamp <= lastOldAudio || firstNewAudio.timestamp-lastOldAudio > 150 ||
			change.AudioBeforeMS == nil || *change.AudioBeforeMS != lastOldAudio ||
			change.AudioAfterMS == nil || *change.AudioAfterMS != firstNewAudio.timestamp {
			t.Fatalf("missing continuous audio and separate video gap: %+v", observed)
		}
		// Audio alone must not age the recovery window or trigger failback.
		for i := range uint32(8) {
			primaryAudio(800+i*100, oldAudio)
			time.Sleep(100 * time.Millisecond)
		}
	}
	primary(1600, mainKey)
	backup(700, backupKey)
	next(backupKey)
	if stable == 0 {
		return
	}

	time.Sleep(560 * time.Millisecond)
	mainDelta := []byte{0x27, 1, 0, 0, 0, 0, 0, 0, 2, 0x41, 1}
	backupDelta := []byte{0x27, 1, 0, 0, 0, 0, 0, 0, 2, 0x41, 2}
	for index := range 6 {
		primary(2200+uint32(index)*150, mainDelta)
		backup(1300+uint32(index)*150, backupDelta)
		next(backupDelta)
		time.Sleep(150 * time.Millisecond)
	}
	if got := get().Failover; len(got.Switches) != 1 || got.RecoveryRestarts != 1 || got.ActivePublisher != "backup" {
		t.Fatalf("stable interval must wait for a keyframe: %+v", got)
	}
	primary(3100, mainKey)
	next(mainHeader)
	resumed := next(mainKey)
	after := get().Failover
	if len(after.Switches) != 2 || after.Switches[1].To != "primary" || after.ActivePublisher != "primary" ||
		resumed.timestamp <= switched.timestamp {
		t.Fatalf("main stream did not resume on the original viewer: %+v", after)
	}
	time.Sleep(560 * time.Millisecond)
	backup(2800, backupKey)
	next(backupHeader)
	second := next(backupKey)
	final := get()
	if second.timestamp <= resumed.timestamp || final.Viewers != 1 || len(final.Failover.Switches) != 3 ||
		final.Failover.Switches[2].To != "backup" || final.Failover.Switches[2].MediaGapMS < 500 {
		t.Fatalf("repeat switch missing: %+v", final.Failover)
	}
}
