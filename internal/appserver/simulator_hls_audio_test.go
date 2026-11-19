package appserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/hlsorigin"
)

func TestHLSIndependentAudioFaultAndRetry(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	const service = "/api/simulators/hls"
	source := decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "POST", service+"/start", http.StatusCreated, []byte(`{"mode":"audio"}`)))
	armed := decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "POST", service+"/live-fault", http.StatusOK, []byte(`{"kind":"not_found"}`)))
	if armed.LiveFault == nil || armed.LiveFault.Rendition != "audio" {
		t.Fatalf("fault scope: %+v", armed.LiveFault)
	}
	sample, err := json.Marshal(diagnostic.SampleRequest{URL: server.URL() + source.Playlist, Protocol: diagnostic.ProtocolHLS, Seconds: 8, Playback: true})
	if err != nil {
		t.Fatal(err)
	}
	run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/runs", http.StatusAccepted, sample))
	if run.Player == nil {
		t.Fatal("missing player grant")
	}
	read := func(name string, code int) string {
		return string(diagnosticRequest(t, server, "GET", name, code, nil))
	}
	playerDir := path.Dir(run.Player.URL) + "/"
	master := read(run.Player.URL, http.StatusOK)
	_, afterURI, ok := strings.Cut(master, `URI="`)
	audioList, _, _ := strings.Cut(afterURI, `"`)
	videoList := lastHLSURI(master)
	if !ok || audioList == "" || videoList == "" || audioList == videoList {
		t.Fatal(master)
	}
	deadline := time.Now().Add(4 * time.Second)
	sequence := fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", armed.LiveFault.Sequence-2)
	var audio, video string
	for time.Now().Before(deadline) {
		audio, video = read(playerDir+audioList, http.StatusOK), read(playerDir+videoList, http.StatusOK)
		if strings.Contains(audio, sequence) && strings.Contains(video, sequence) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(audio, sequence) || !strings.Contains(video, sequence) {
		t.Fatalf("target sequence absent\n%s\n%s", audio, video)
	}
	videoSegment := playerDir + lastHLSURI(video)
	audioSegment := playerDir + lastHLSURI(audio)
	if !strings.Contains(read(videoSegment, http.StatusOK), "moof") {
		t.Fatal("missing video fragment")
	}
	read(audioSegment, http.StatusNotFound)
	audioFull := read(audioSegment, http.StatusOK)
	name := fmt.Sprintf("audio-%d.m4s", armed.LiveFault.Sequence)
	if audioFull != read(path.Dir(source.Playlist)+"/"+name, http.StatusOK) || !strings.Contains(audioFull, "moof") {
		t.Fatal("retry did not return the full audio fragment")
	}
	read(videoSegment, http.StatusOK)
	status := decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "GET", service, http.StatusOK, nil))
	if status.FaultHits != 1 || status.LiveFault.Attempts != 2 || status.LiveFault.RetryAt == nil {
		t.Fatalf("fault/retry observations: %+v", status.LiveFault)
	}
	runPath := "/api/diagnostics/runs/" + run.ID
	diagnosticRequest(t, server, "POST", runPath+"/stop", http.StatusAccepted, nil)
	captured := waitDiagnostic(t, server, runPath)
	if captured.Report == nil {
		t.Fatal("missing sample report")
	}
	record := diagnostic.PlayerRecording{
		Version: 1, Revision: 1, Source: "independent_pull", Engine: "hlsjs", Complete: true,
		StartedAt: captured.Report.Manifest.Observation.StartedAt, ElapsedMS: 4000,
		Events: []diagnostic.PlayerEvent{{Kind: "start"}, {Kind: "stop", AtMS: 4000}}, Samples: []diagnostic.PlayerSample{},
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), "POST", server.URL()+runPath+"/player", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-StreamAnalyzer-Player", run.Player.Token)
	req.Header.Set("Origin", server.URL())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errorBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("save player report: %d %s", resp.StatusCode, errorBody)
	}
	report := decodeDiagnostic[diagnostic.Report](t, diagnosticRequest(t, server, "GET", runPath+"/report.json", http.StatusOK, nil))
	if report.PlayerNetwork == nil {
		t.Fatal("missing playback request evidence")
	}
	var audioFailed, audioRetried, videoComplete bool
	for _, request := range report.PlayerNetwork.Requests {
		if request.Kind != "media" {
			continue
		}
		switch request.ResourceID {
		case strings.SplitN(lastHLSURI(audio), "?", 2)[0]:
			if request.Rendition != "audio" {
				t.Fatalf("audio request missing rendition: %+v", request)
			}
			audioFailed = audioFailed || request.HTTPStatus == 404
			audioRetried = audioRetried || request.HTTPStatus == 200
		case strings.SplitN(lastHLSURI(video), "?", 2)[0]:
			videoComplete = videoComplete || request.Rendition == "" && request.HTTPStatus == 200
		}
	}
	if !audioFailed || !audioRetried || !videoComplete {
		t.Fatalf("missing track-level evidence: audio failed=%v audio retried=%v video complete=%v", audioFailed, audioRetried, videoComplete)
	}
}

func lastHLSURI(body string) string {
	var uri string
	for line := range strings.SplitSeq(body, "\n") {
		if line != "" && line[0] != '#' {
			uri = line
		}
	}
	return uri
}
