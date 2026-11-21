package appserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/whep"
)

func TestWHEPDiagnosticEndToEndWithPlayerProxy(t *testing.T) {
	app := testDiagnosticServer(t, t.TempDir())
	srv := httptest.NewServer(app.httpServer.Handler)
	t.Cleanup(srv.Close)

	// 1. Start built-in WHEP simulator in normal mode.
	source := decodeDiagnostic[whep.Status](t, diagnosticRequest(t, app, "POST", "/api/simulators/whep/start", 201, []byte(`{"mode":"normal"}`)))
	t.Cleanup(func() { app.simulators.whep.Stop() })

	// 2. Start a WHEP diagnostic run with playback enabled.
	reqBody, _ := json.Marshal(map[string]any{
		"protocol":             "whep",
		"url":                  srv.URL + source.Endpoint,
		"seconds":              3,
		"playback":             true,
		"ice_transport_policy": "all",
	})
	run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, app, "POST", "/api/diagnostics/runs", 202, reqBody))
	if run.Player == nil {
		t.Fatalf("expected Player grant for WHEP playback")
	}
	if run.Player.Transport != "whep" || !strings.Contains(run.Player.URL, "/player/whep?token=") {
		t.Fatalf("unexpected WHEP player grant: %+v", run.Player)
	}

	// 3. Submit a browser WebRTC player recording (Phase 2) using the issued token.
	rtt := 42.0
	playerRec, _ := json.Marshal(diagnostic.PlayerRecording{
		Version:   1,
		Revision:  1,
		Engine:    "webrtc",
		Source:    "independent_pull",
		StartedAt: time.Now().UTC(),
		ElapsedMS: 3000,
		Complete:  true,
		Events: []diagnostic.PlayerEvent{
			{AtMS: 250, Kind: "playing"},
			{AtMS: 260, Kind: "first_frame", Method: "video_frame_callback"},
		},
		WebRTC: &diagnostic.PlayerWebRTCStats{
			ICEState:               "connected",
			ConnectionState:        "connected",
			SelectedCandidateType:  "host",
			SelectedRemoteType:     "host",
			SelectedProtocol:       "udp",
			CurrentRTTMS:           &rtt,
			VideoCodec:             "h264",
			PacketsReceived:        120,
			PacketsLost:            0,
			JitterMS:               2.1,
			JitterBufferAvgMS:      15.0,
			FramesDecoded:          90,
			FreezeCount:            0,
			TotalFreezesDurationMS: 0,
			BitrateKbps:            980.5,
			StatsSamples:           3,
		},
	})
	postReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/diagnostics/runs/"+run.ID+"/player", bytes.NewReader(playerRec))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("X-StreamAnalyzer-Player", run.Player.Token)
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("POST player recording: %v", err)
	}
	_ = postResp.Body.Close()
	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("POST player recording status=%d", postResp.StatusCode)
	}

	// 4. Wait for run completion and verify both server-side WHEP metrics and browser WebRTC stats exist.
	deadline := time.Now().Add(15 * time.Second)
	var detail diagnostic.Detail
	for time.Now().Before(deadline) {
		detail = decodeDiagnostic[diagnostic.Detail](t, diagnosticRequest(t, app, "GET", "/api/diagnostics/runs/"+run.ID, 200, nil))
		if detail.Run.Status != "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if detail.Run.Status != "done" {
		t.Fatalf("run status=%q err=%q, want done", detail.Run.Status, detail.Run.Error)
	}
	if detail.Report == nil || detail.Report.WHEP == nil || len(detail.Report.WHEP.Tracks) == 0 {
		t.Fatalf("missing server WHEP report: %+v", detail.Report)
	}
	if detail.Report.Player == nil || detail.Report.Player.WebRTC == nil || detail.Report.Player.WebRTC.VideoCodec != "h264" {
		t.Fatalf("missing browser Player.WebRTC stats in report: %+v", detail.Report.Player)
	}

	// 5. Verify HTML report endpoint renders both server WHEP and browser WebRTC sections.
	html := string(diagnosticRequest(t, app, "GET", "/api/diagnostics/runs/"+run.ID+"/report.html", 200, nil))
	if !strings.Contains(html, "WHEP 信令、ICE/DTLS 与 RTP 接收") || !strings.Contains(html, "浏览器原生 WebRTC 播放 getStats() 指标") {
		t.Fatalf("HTML report missing expected WHEP/WebRTC sections")
	}
}
