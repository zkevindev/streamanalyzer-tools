package diagnostic

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/whep"

	"github.com/pion/rtp"
)

// startTestWHEPServer exposes internal/simulator/whep.Manager over HTTP with standard WHEP semantics.
func startTestWHEPServer(t *testing.T, mode string) (*httptest.Server, string, *atomic.Int32) {
	t.Helper()
	mgr := &whep.Manager{}
	t.Cleanup(func() { mgr.Close() })
	status, err := mgr.Start(mode)
	if err != nil {
		t.Fatalf("start whep simulator (%s): %v", mode, err)
	}
	var deleted atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/simulators/whep/stream/{source}", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		answer, loc, err := mgr.Offer(r.Context(), r.PathValue("source"), string(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/sdp")
		w.Header().Set("Location", loc)
		w.Header().Set("ETag", `"v1"`)
		w.Header().Add("Link", `<stun:stun.example.org:3478>; rel="ice-server"`)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(answer))
	})
	mux.HandleFunc("DELETE /api/simulators/whep/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		deleted.Add(1)
		_ = mgr.Delete(r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, srv.URL + status.Endpoint, &deleted
}

func waitForDiagnosticDetail(t *testing.T, svc *Service, id string, timeout time.Duration) Detail {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		detail, err := svc.Detail(id)
		if err != nil {
			t.Fatalf("detail %s: %v", id, err)
		}
		if detail.Run.Status != "running" {
			return detail
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatalf("run %s did not finish within %s", id, timeout)
	return Detail{}
}

func TestWHEPSampleNormalExportImportAndHTML(t *testing.T) {
	_, endpoint, deleted := startTestWHEPServer(t, "normal")

	svc, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	run, err := svc.Start(SampleRequest{
		Protocol:           ProtocolWHEP,
		URL:                endpoint + "?token=super-secret-value",
		Seconds:            3,
		ICETransportPolicy: "all",
		Headers: map[string]string{
			"Authorization": "Bearer secret-whep-token",
		},
	})
	if err != nil {
		t.Fatalf("start whep run: %v", err)
	}
	detail := waitForDiagnosticDetail(t, svc, run.ID, 15*time.Second)
	if detail.Run.Status != "done" {
		t.Fatalf("status=%q err=%q, want done", detail.Run.Status, detail.Run.Error)
	}
	if detail.Report == nil || detail.Report.WHEP == nil {
		t.Fatalf("expected Report.WHEP to be populated: %+v", detail.Report)
	}
	w := detail.Report.WHEP
	if w.HTTPStatus != http.StatusCreated {
		t.Fatalf("http_status=%d, want 201", w.HTTPStatus)
	}
	if !w.LocationRecorded || w.DeleteStatus != http.StatusNoContent || deleted.Load() < 1 {
		t.Fatalf("expected Location + DELETE cleanup: location=%v deleteStatus=%d srvDeletes=%d",
			w.LocationRecorded, w.DeleteStatus, deleted.Load())
	}
	if len(w.LinkICEServers) != 1 {
		t.Fatalf("link_ice_servers=%v, want 1", w.LinkICEServers)
	}
	if w.ICEState != "connected" && w.ICEState != "completed" {
		t.Fatalf("unexpected ice_state=%q", w.ICEState)
	}
	if w.DTLSState != "connected" {
		t.Fatalf("unexpected dtls_state=%q", w.DTLSState)
	}
	if w.SelectedPair == nil {
		t.Fatalf("expected SelectedPair to be recorded")
	}
	if len(w.Tracks) == 0 {
		t.Fatalf("expected at least 1 RTP track, got 0")
	}
	vt := w.Tracks[0]
	if vt.Kind != "video" || vt.Packets == 0 || vt.Keyframes == 0 {
		t.Fatalf("unexpected video track metrics: %+v", vt)
	}
	if detail.Report.Metrics.FirstVideoMS == nil {
		t.Fatalf("expected FirstVideoMS to be set")
	}
	if detail.Report.Throughput == nil || len(detail.Report.Throughput.Samples) == 0 {
		t.Fatalf("expected non-empty throughput samples: %+v", detail.Report.Throughput)
	}

	// Verify HTML report renders WHEP section and does not leak secrets.
	var htmlBuf bytes.Buffer
	if err := RenderHTML(&htmlBuf, *detail.Report, nil, ""); err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	html := htmlBuf.String()
	if !strings.Contains(html, "WHEP 信令、ICE/DTLS 与 RTP 接收") || !strings.Contains(html, "stream.whep.zip") {
		t.Fatalf("HTML report missing WHEP section")
	}
	if strings.Contains(html, "super-secret-value") || strings.Contains(html, "secret-whep-token") {
		t.Fatalf("HTML report leaked secret query/header")
	}

	// Verify AI model input projection includes WHEP metrics cleanly.
	miBytes, err := modelInput(*detail.Report)
	if err != nil {
		t.Fatalf("modelInput: %v", err)
	}
	var mi map[string]any
	if err := json.Unmarshal(miBytes, &mi); err != nil {
		t.Fatalf("unmarshal modelInput: %v", err)
	}
	if _, ok := mi["whep"]; !ok {
		t.Fatalf("modelInput missing whep field: %s", string(miBytes))
	}
	if strings.Contains(string(miBytes), "super-secret-value") || strings.Contains(string(miBytes), "secret-whep-token") {
		t.Fatalf("modelInput leaked secret")
	}

	// Verify WriteBundle -> Import round-trip deterministic replay from stream.whep.zip.
	var exported bytes.Buffer
	if err := svc.WriteBundle(run.ID, &exported); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	importSvc, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = importSvc.Close() })
	importedRun, err := importSvc.Import(bytes.NewReader(exported.Bytes()))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	importedDetail, err := importSvc.Detail(importedRun.ID)
	if err != nil {
		t.Fatalf("imported Detail: %v", err)
	}
	if importedDetail.Report == nil || importedDetail.Report.WHEP == nil {
		t.Fatalf("imported run missing WHEP report")
	}
	if importedDetail.Report.WHEP.HTTPStatus != w.HTTPStatus ||
		len(importedDetail.Report.WHEP.Tracks) != len(w.Tracks) ||
		importedDetail.Report.WHEP.Tracks[0].Packets != vt.Packets ||
		importedDetail.Report.WHEP.Tracks[0].Keyframes != vt.Keyframes {
		t.Fatalf("imported WHEP mismatch:\ngot:  %+v\nwant: %+v", importedDetail.Report.WHEP, w)
	}
}

func TestWHEPSignalingAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	svc, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	run, err := svc.Start(SampleRequest{
		Protocol: ProtocolWHEP,
		URL:      srv.URL + "/whep/auth-fail",
		Seconds:  3,
	})
	if err != nil {
		t.Fatal(err)
	}
	detail := waitForDiagnosticDetail(t, svc, run.ID, 10*time.Second)
	if detail.Report == nil || detail.Report.WHEP == nil {
		t.Fatalf("expected WHEP report even on signaling failure")
	}
	if detail.Report.WHEP.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("http_status=%d, want 401", detail.Report.WHEP.HTTPStatus)
	}
	if !slices.ContainsFunc(detail.Report.Findings, func(f Finding) bool { return f.Code == "whep_signaling_failed" }) {
		t.Fatalf("missing whep_signaling_failed finding: %+v", detail.Report.Findings)
	}
}

func TestWHEPNoMediaAndMediaStop(t *testing.T) {
	t.Run("no_media", func(t *testing.T) {
		_, endpoint, _ := startTestWHEPServer(t, "no_media")
		svc, err := Open(t.TempDir(), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		run, err := svc.Start(SampleRequest{Protocol: ProtocolWHEP, URL: endpoint, Seconds: 2})
		if err != nil {
			t.Fatal(err)
		}
		detail := waitForDiagnosticDetail(t, svc, run.ID, 12*time.Second)
		if detail.Report == nil || detail.Report.WHEP == nil {
			t.Fatalf("expected Report.WHEP")
		}
		if !slices.ContainsFunc(detail.Report.Findings, func(f Finding) bool { return f.Code == "whep_no_media" }) {
			t.Fatalf("missing whep_no_media finding: %+v", detail.Report.Findings)
		}
	})

	t.Run("media_stop", func(t *testing.T) {
		_, endpoint, _ := startTestWHEPServer(t, "media_stop")
		svc, err := Open(t.TempDir(), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		// whep simulator media_stop stops sending RTP after ~2.5s; sampling 5s observes a clear >=2s stop gap.
		run, err := svc.Start(SampleRequest{Protocol: ProtocolWHEP, URL: endpoint, Seconds: 5})
		if err != nil {
			t.Fatal(err)
		}
		detail := waitForDiagnosticDetail(t, svc, run.ID, 15*time.Second)
		if detail.Report == nil || detail.Report.WHEP == nil {
			t.Fatalf("expected Report.WHEP")
		}
		hasStop := slices.ContainsFunc(detail.Report.Findings, func(f Finding) bool {
			return f.Code == "whep_media_stopped" || f.Code == "whep_video_gap"
		})
		if !hasStop {
			t.Fatalf("expected whep_media_stopped or whep_video_gap finding, got: %+v (gap=%dms)",
				detail.Report.Findings, detail.Report.WHEP.MediaStoppedGapMS)
		}
	})
}

func TestWHEPReplaySyntheticLossReorderJitter(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, whepStreamName)

	pliSentMS := int64(250)
	meta := whepArchiveMetadata{
		Version:            1,
		ICETransportPolicy: "all",
		HTTPStatus:         http.StatusCreated,
		LocationRecorded:   true,
		DeleteStatus:       http.StatusNoContent,
		ICEState:           "connected",
		DTLSState:          "connected",
		PeerState:          "connected",
		Tracks: []whepTrackMeta{
			{
				ID:             1,
				Kind:           "video",
				Codec:          "h264",
				MimeType:       "video/H264",
				PayloadType:    96,
				ClockRate:      90000,
				SSRC:           0x11223344,
				PLICount:       2, // PLI sent but never recovered -> whep_pli_no_response
				PLIFirstSentMS: &pliSentMS,
			},
		},
	}

	type pktSpec struct {
		seq     uint16
		ts      uint32
		atMS    int64
		payload []byte
		marker  bool
	}
	nonIDR := []byte{0x41, 0x9a, 0x01, 0x02}
	specs := []pktSpec{
		// No IDR keyframe -> triggers whep_pli_no_response
		{seq: 100, ts: 90000, atMS: 100, payload: nonIDR, marker: true},
		{seq: 101, ts: 93000, atMS: 133, payload: nonIDR, marker: true},
		// seq 102, 103 missing (2 packets lost)
		{seq: 105, ts: 105000, atMS: 266, payload: nonIDR, marker: true},
		{seq: 104, ts: 102000, atMS: 280, payload: nonIDR, marker: true}, // out-of-order #1
		{seq: 105, ts: 105000, atMS: 285, payload: nonIDR, marker: true}, // duplicate
		{seq: 107, ts: 111000, atMS: 800, payload: nonIDR, marker: true}, // high arrival jitter
		{seq: 106, ts: 108000, atMS: 810, payload: nonIDR, marker: true}, // out-of-order #2
		{seq: 109, ts: 117000, atMS: 1200, payload: nonIDR, marker: true},
		{seq: 108, ts: 114000, atMS: 1210, payload: nonIDR, marker: true}, // out-of-order #3
		{seq: 111, ts: 123000, atMS: 1500, payload: nonIDR, marker: true},
		{seq: 110, ts: 120000, atMS: 1510, payload: nonIDR, marker: true}, // out-of-order #4
		{seq: 113, ts: 129000, atMS: 1800, payload: nonIDR, marker: true},
		{seq: 112, ts: 126000, atMS: 1810, payload: nonIDR, marker: true}, // out-of-order #5
	}

	var rtpBin bytes.Buffer
	var rtpIdx bytes.Buffer
	for _, s := range specs {
		p := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: s.seq,
				Timestamp:      s.ts,
				SSRC:           0x11223344,
				Marker:         s.marker,
			},
			Payload: s.payload,
		}
		raw, err := p.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		off := int64(rtpBin.Len())
		rtpBin.Write(raw)

		entry := whepRTPIndexEntry{
			TrackID:   1,
			SSRC:      0x11223344,
			ArrivalMS: s.atMS,
			Offset:    off,
			Size:      len(raw),
		}
		line, _ := json.Marshal(entry)
		rtpIdx.Write(line)
		rtpIdx.WriteByte('\n')
	}

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	writeEntry := func(name string, data []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	metaBytes, _ := json.Marshal(meta)
	writeEntry("metadata.json", metaBytes)
	writeEntry("rtp-packets.bin", rtpBin.Bytes())
	writeEntry("rtp-index.ndjson", rtpIdx.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	manifest := Manifest{
		Protocol: ProtocolWHEP,
		Observation: Observation{
			ElapsedMS: 2000,
			EndReason: "duration_limit",
		},
	}
	report, err := replayWHEP(dir, manifest)
	if err != nil {
		t.Fatalf("replayWHEP: %v", err)
	}
	if report.WHEP == nil || len(report.WHEP.Tracks) != 1 {
		t.Fatalf("expected 1 track in WHEP report: %+v", report.WHEP)
	}
	tr := report.WHEP.Tracks[0]
	if tr.MissingSequencePackets < 2 {
		t.Fatalf("expected >=2 missing packets, got %d", tr.MissingSequencePackets)
	}
	if tr.OutOfOrderPackets < 1 {
		t.Fatalf("expected >=1 out-of-order packet, got %d", tr.OutOfOrderPackets)
	}
	if tr.DuplicatePackets < 1 {
		t.Fatalf("expected >=1 duplicate packet, got %d", tr.DuplicatePackets)
	}
	if tr.MaxJitterMS <= 30 {
		t.Fatalf("expected high max jitter >30ms, got %.2fms", tr.MaxJitterMS)
	}
	for _, wantCode := range []string{"whep_rtp_packet_loss", "whep_rtp_out_of_order", "whep_rtp_jitter_high", "whep_pli_no_response"} {
		if !slices.ContainsFunc(report.Findings, func(f Finding) bool { return f.Code == wantCode }) {
			t.Errorf("missing expected finding %q; got %+v", wantCode, report.Findings)
		}
	}
}

func TestWHEPPlayerProxyAndWebRTCFindings(t *testing.T) {
	// 1. Test playerWHEP proxy forwards custom Authorization header and DELETEs remote session.
	var gotAuth string
	var gotDelete atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/sdp")
			w.Header().Set("Location", "/whep/session/abc123")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=mock-answer\r\n"))
		case http.MethodDelete:
			if r.URL.Path == "/whep/session/abc123" {
				gotDelete.Add(1)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer upstream.Close()

	targetURL, err := url.Parse(upstream.URL + "/whep/live")
	if err != nil {
		t.Fatal(err)
	}
	pw := newPlayerWHEP(targetURL, nil)
	pw.headers = http.Header{
		"Authorization": []string{"Bearer upstream-secret"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/diagnostics/runs/test/player/whep", strings.NewReader("v=0\r\ns=offer\r\n"))
	req.Header.Set("Content-Type", "application/sdp")
	rec := httptest.NewRecorder()
	pw.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("proxy POST status=%d body=%q", rec.Code, rec.Body.String())
	}
	if gotAuth != "Bearer upstream-secret" {
		t.Fatalf("upstream did not receive Authorization header, got %q", gotAuth)
	}
	if !strings.Contains(rec.Body.String(), "s=mock-answer") {
		t.Fatalf("unexpected proxy answer: %q", rec.Body.String())
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/diagnostics/runs/test/player/whep", nil)
	delRec := httptest.NewRecorder()
	pw.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent || gotDelete.Load() != 1 {
		t.Fatalf("proxy DELETE failed: status=%d deletes=%d", delRec.Code, gotDelete.Load())
	}

	// 2. Test PlayerRecording with Engine="webrtc" & PlayerWebRTCStats triggers WebRTC findings & AI model projection.
	rtt := 125.5
	now := time.Now().UTC()
	recording := PlayerRecording{
		Version:   1,
		Revision:  1,
		Engine:    "webrtc",
		Source:    "independent_pull",
		StartedAt: now,
		ElapsedMS: 5000,
		Complete:  true,
		Events: []PlayerEvent{
			{AtMS: 320, Kind: "playing"},
			{AtMS: 330, Kind: "first_frame", Method: "video_frame_callback"},
		},
		WebRTC: &PlayerWebRTCStats{
			ICEState:               "connected",
			ConnectionState:        "connected",
			SelectedCandidateType:  "relay",
			SelectedRemoteType:     "host",
			SelectedProtocol:       "udp",
			CurrentRTTMS:           &rtt,
			VideoCodec:             "h264",
			AudioCodec:             "opus",
			PacketsReceived:        1000,
			PacketsLost:            50,
			JitterMS:               18.2,
			JitterBufferAvgMS:      260.0,
			FramesDecoded:          150,
			FramesDropped:          5,
			FreezeCount:            3,
			TotalFreezesDurationMS: 1200,
			PLICount:               4,
			NACKCount:              12,
			BitrateKbps:            1500,
			StatsSamples:           5,
		},
	}
	if err := validatePlayerRecording(recording, now); err != nil {
		t.Fatalf("validatePlayerRecording: %v", err)
	}
	obs := Observation{ElapsedMS: 5000}
	playerReport := analyzePlayer(recording, obs)
	if playerReport == nil || playerReport.WebRTC == nil || playerReport.WebRTC.FreezeCount != 3 {
		t.Fatalf("expected PlayerReport.WebRTC to be preserved: %+v", playerReport)
	}
	report := Report{
		Manifest: Manifest{
			Protocol:    ProtocolWHEP,
			Observation: obs,
		},
		Player: playerReport,
	}
	addPlayerFindings(&report)
	for _, wantCode := range []string{"player_webrtc_freeze", "player_webrtc_jitter_buffer_high", "player_webrtc_packet_loss"} {
		if !slices.ContainsFunc(report.Findings, func(f Finding) bool { return f.Code == wantCode }) {
			t.Errorf("missing finding %q in %+v", wantCode, report.Findings)
		}
	}
	miBytes, err := modelInput(report)
	if err != nil {
		t.Fatalf("modelInput: %v", err)
	}
	if !strings.Contains(string(miBytes), `"freeze_count":3`) {
		t.Fatalf("modelInput missing Player.WebRTC freeze_count: %s", string(miBytes))
	}
}
