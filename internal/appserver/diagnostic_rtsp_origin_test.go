package appserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/onvif"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/pion/rtp"
)

func TestRTSPEvidenceReplayPreservesGapForIndependentClients(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	camera := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, server, "POST", onvifAPI+"/start", http.StatusCreated, []byte(`{"video_gap_ms":1500}`)))
	req, err := json.Marshal(diagnostic.SampleRequest{Protocol: diagnostic.ProtocolRTSP, Transport: "tcp", URL: camera.StreamURL, Seconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	const api = "/api/diagnostics"
	run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", api+"/runs", http.StatusAccepted, req))
	runPath := api + "/runs/" + run.ID
	captured := waitDiagnostic(t, server, runPath)
	if !captured.RTSPReplayAvailable || captured.Report == nil || captured.Report.RTSP == nil {
		t.Fatalf("RTSP capture was not replayable: %+v", captured)
	}
	index := slices.IndexFunc(captured.Report.RTSP.Tracks, func(track diagnostic.RTSPTrackMetrics) bool { return track.Kind == "video" })
	if index < 0 {
		t.Fatal("missing captured video track")
	}
	track := captured.Report.RTSP.Tracks[index]
	if track.MaxArrivalGapMS < 1250 {
		t.Fatalf("test source did not produce the expected gap: %+v", track)
	}
	dump := diagnosticRequest(t, server, "GET", runPath+"/evidence.zip", http.StatusOK, nil)
	diagnosticRequest(t, server, "POST", onvifAPI+"/stop", http.StatusOK, nil)
	imported := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", api+"/import", http.StatusCreated, dump))
	replayPath := api + "/runs/" + imported.ID + "/rtsp-replay"
	source := decodeDiagnostic[diagnostic.RTSPReplayStatus](t, diagnosticRequest(t, server, "POST", replayPath+"/start", http.StatusCreated, nil))
	if source.URL == "" || source.Packets < track.RTPPackets {
		t.Fatalf("invalid RTSP replay source: %+v", source)
	}
	type result struct {
		rtspReplayProbe
		err error
	}
	clients := make(chan result, 2)
	for i := range 2 {
		go func() {
			time.Sleep(time.Duration(i) * 220 * time.Millisecond)
			probe, err := readRTSPReplay(source.URL)
			clients <- result{probe, err}
		}()
	}
	first := <-clients
	second := <-clients
	for _, actual := range []result{first, second} {
		if actual.err != nil || actual.packets != track.RTPPackets ||
			actual.maxGap.Milliseconds() < track.MaxArrivalGapMS-200 || actual.maxGap.Milliseconds() > track.MaxArrivalGapMS+400 {
			t.Fatalf("replay client lost RTP or the timed gap: %+v, original=%+v", actual, track)
		}
	}
	if first.digest != second.digest {
		t.Fatal("clients did not independently receive the same RTP data")
	}
	active := decodeDiagnostic[diagnostic.RTSPReplayStatus](t, diagnosticRequest(t, server, "GET", replayPath, http.StatusOK, nil))
	if active.Plays != 2 {
		t.Fatalf("did not observe independent RTSP playback: %+v", active)
	}
	diagnosticRequest(t, server, "POST", replayPath+"/stop", http.StatusOK, nil)
	target, err := base.ParseURL(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	if socket, err := net.DialTimeout("tcp", target.Host, 100*time.Millisecond); err == nil {
		socket.Close()
		t.Fatal("stopped origin still accepts connections")
	}
}

type rtspReplayProbe struct {
	packets int
	digest  [32]byte
	maxGap  time.Duration
}

func readRTSPReplay(target string) (rtspReplayProbe, error) {
	u, err := base.ParseURL(target)
	if err != nil {
		return rtspReplayProbe{}, err
	}
	transport := gortsplib.ProtocolTCP
	client := &gortsplib.Client{Scheme: u.Scheme, Host: u.Host, Protocol: &transport, ReadTimeout: 8 * time.Second}
	if err := client.Start(); err != nil {
		return rtspReplayProbe{}, err
	}
	defer client.Close()
	desc, _, err := client.Describe(u)
	if err != nil {
		return rtspReplayProbe{}, err
	}
	for _, media := range desc.Medias {
		if media.Type != description.MediaTypeVideo {
			continue
		}
		if _, err := client.Setup(desc.BaseURL, media, 0, 0); err != nil {
			return rtspReplayProbe{}, err
		}
		var mu sync.Mutex
		var data bytes.Buffer
		var result rtspReplayProbe
		var previous time.Time
		client.OnPacketRTP(media, media.Formats[0], func(packet *rtp.Packet) {
			encoded, err := packet.Marshal()
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			now := time.Now()
			if !previous.IsZero() {
				result.maxGap = max(result.maxGap, now.Sub(previous))
			}
			previous = now
			result.packets++
			data.Write(encoded)
		})
		if _, err := client.Play(nil); err != nil {
			return rtspReplayProbe{}, err
		}
		_ = client.Wait()
		mu.Lock()
		defer mu.Unlock()
		result.digest = sha256.Sum256(data.Bytes())
		return result, nil
	}
	return rtspReplayProbe{}, fmt.Errorf("replay SDP omitted video")
}
