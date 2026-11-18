package appserver

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"streamanalyzer/internal/diagnostic"
)

func TestDiagnosticPlayerRecordingReportAndOfflineReplay(t *testing.T) {
	clearDiagnosticModelEnv(t)
	const sourceToken = "private-playback-source-key"
	var modelCalls atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test.flv":
			if r.URL.Query().Get("token") != sourceToken {
				t.Error("source playback access parameter was lost")
			}
			_, _ = w.Write(testFLV())
		case "/v1/chat/completions":
			modelCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			for _, needed := range []string{"player", "stall_count", "first_screen_ms", "same_capture"} {
				if !bytes.Contains(body, []byte(needed)) {
					t.Errorf("model request omitted %s", needed)
				}
			}
			if bytes.Contains(body, []byte(sourceToken)) || bytes.Contains(body, []byte("test.flv")) || bytes.Contains(body, []byte("127.0.0.1")) {
				t.Error("model request exposed source details")
			}
			const answer = `{"summary":"播放器已记录首屏和卡顿","hypotheses":[{"claim":"检查采样窗口内卡顿","evidence_ids":["F001"],"next_check":"对照媒体时间戳"}]}`
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	t.Setenv("STREAMANALYZER_OPENAI_BASE_URL", origin.URL+"/v1")
	t.Setenv("STREAMANALYZER_OPENAI_MODEL", "player-fixture")
	t.Setenv("STREAMANALYZER_OPENAI_API_KEY", "test-model-key")
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/diagnostics"
	request, _ := json.Marshal(diagnostic.SampleRequest{URL: origin.URL + "/test.flv?token=" + sourceToken, Seconds: 1, Playback: true})
	created := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/runs", http.StatusAccepted, request))
	if created.Player == nil || created.Player.Token == "" {
		t.Fatal("recording grant missing")
	}
	runPath := api + "/runs/" + created.ID
	captured := waitDiagnostic(t, server, runPath)
	if captured.Report == nil || captured.Report.Player == nil || captured.Report.Player.State != "awaiting" {
		t.Fatal("sample did not produce an initial player report")
	}
	record := diagnostic.PlayerRecording{
		Version: 1, Revision: 1, StartedAt: captured.Report.Manifest.Observation.StartedAt, ElapsedMS: 1200,
		Engine: "mpegtsjs", Source: "same_capture", Complete: true,
		Events: []diagnostic.PlayerEvent{
			{AtMS: 0, Kind: "start"}, {AtMS: 100, Kind: "waiting"}, {AtMS: 200, Kind: "playing"},
			{AtMS: 210, Kind: "first_frame", Method: "video_frame_callback"}, {AtMS: 400, Kind: "waiting"},
			{AtMS: 950, Kind: "playing"}, {AtMS: 1200, Kind: "stop"},
		},
		Samples: []diagnostic.PlayerSample{
			{AtMS: 210, BufferMS: 600, ReadyState: 4}, {AtMS: 450, CurrentMS: 200, ReadyState: 2},
			{AtMS: 1000, CurrentMS: 250, BufferMS: 450, ReadyState: 3},
		},
	}
	save := func(record diagnostic.PlayerRecording, token string, status int) []byte {
		t.Helper()
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL()+runPath+"/player", bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-StreamAnalyzer-Player", token)
		req.Header.Set("Origin", server.URL())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != status {
			t.Fatalf("expected player save status %d, got %d: %.500s (%v)", status, resp.StatusCode, body, err)
		}
		return body
	}
	save(record, "stale-grant", http.StatusNotFound)
	saved := decodeDiagnostic[diagnostic.PlayerReport](t, save(record, created.Player.Token, http.StatusOK))
	if saved.State != "complete" || saved.FirstScreenMS == nil || *saved.FirstScreenMS != 210 || saved.FirstScreenMethod != "video_frame_callback" ||
		saved.StallCount != 1 || saved.TotalStallMS != 550 || saved.MaxBufferMS != 600 || len(saved.BufferSamples) != 3 {
		t.Fatalf("incorrect QoE report: %+v", saved)
	}
	save(record, created.Player.Token, http.StatusOK)
	record.ElapsedMS++
	save(record, created.Player.Token, http.StatusConflict)
	jsonReport := diagnosticRequest(t, server, http.MethodGet, runPath+"/report.json", http.StatusOK, nil)
	report := decodeDiagnostic[diagnostic.Report](t, jsonReport)
	if report.Manifest.Player == nil || len(report.Manifest.Player.SHA256) != 64 || !reflect.DeepEqual(report.Player, &saved) ||
		len(report.Manifest.Observation.Requests) != 1 || report.Manifest.Observation.Requests[0].HTTPStatus != http.StatusOK {
		t.Fatalf("unified report omitted timing or player evidence: %+v", report)
	}
	html := diagnosticRequest(t, server, http.MethodGet, runPath+"/report.html", http.StatusOK, nil)
	for _, text := range []string{"播放器体验", "可播放缓冲", "卡顿区间", "550 ms"} {
		if !bytes.Contains(html, []byte(text)) {
			t.Errorf("HTML report omitted %s", text)
		}
	}
	for _, body := range [][]byte{jsonReport, html} {
		if bytes.Contains(body, []byte(sourceToken)) || bytes.Contains(body, []byte(created.Player.Token)) {
			t.Fatal("report exposed private playback credentials")
		}
	}
	explained := decodeDiagnostic[diagnostic.Explanation](t, diagnosticRequest(t, server, http.MethodPost, runPath+"/explain", http.StatusOK, nil))
	if explained.Model != "player-fixture" || modelCalls.Load() != 1 {
		t.Fatal("report could not be explained using player evidence")
	}
	bundle := diagnosticRequest(t, server, http.MethodGet, runPath+"/evidence.zip", http.StatusOK, nil)
	origin.Close()
	imported := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/import", http.StatusCreated, bundle))
	replay := waitDiagnostic(t, server, api+"/runs/"+imported.ID)
	if replay.Mode != "replay" || replay.Player != nil || !reflect.DeepEqual(replay.Report, &report) {
		t.Fatalf("offline player report differs: %+v", replay.Report)
	}
	tampered := diagnosticRequest(t, server, http.MethodPost, api+"/import", http.StatusBadRequest, tamperPlayerBundle(t, bundle))
	if !bytes.Contains(tampered, []byte("SHA-256")) {
		t.Fatal("tampered player artifact was not rejected by its hash")
	}
}

func tamperPlayerBundle(t *testing.T, input []byte) []byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(input), int64(len(input)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	seen := false
	for _, entry := range archive.File {
		if entry.Name != "player.json" {
			if err := writer.Copy(entry); err != nil {
				t.Fatal(err)
			}
			continue
		}
		seen = true
		source, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(source)
		source.Close()
		if err != nil || !bytes.Contains(data, []byte(`"revision":1`)) {
			t.Fatalf("unexpected player artifact: %v", err)
		}
		data = bytes.Replace(data, []byte(`"revision":1`), []byte(`"revision":2`), 1)
		target, err := writer.Create(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := target.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil || !seen {
		t.Fatalf("unable to tamper with player recording: %v", err)
	}
	return output.Bytes()
}
