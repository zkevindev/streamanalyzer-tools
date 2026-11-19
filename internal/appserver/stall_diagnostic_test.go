package appserver

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
)

func TestDiagnosticStallEvidenceWorkflowAndOfflineReplay(t *testing.T) {
	clearDiagnosticModelEnv(t)
	ffmpeg := stallDiagnosticMediaTool(t, "ffmpeg")
	_ = stallDiagnosticMediaTool(t, "ffprobe")
	fixture := filepath.Join(t.TempDir(), "source.flv")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=20", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "6", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-g", "20", "-c:a", "aac", "-f", "flv", "-y", fixture)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("FFmpeg fixture encoders unavailable: %v %.300s", err, output)
	}
	media, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var originCalls, modelCalls atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originCalls.Add(1)
		if r.URL.Query().Get("token") != "private-stall-source-token" {
			t.Error("source query lost")
		}
		w.Header().Set("Content-Type", "video/x-flv")
		w.Header().Set("X-Auth-Token", "private-stall-header-token")
		_, _ = w.Write(media[:len(media)/2])
		w.(http.Flusher).Flush()
		timer := time.NewTimer(1200 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			_, _ = w.Write(media[len(media)/2:])
		case <-r.Context().Done():
		}
	}))
	defer origin.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if json.Unmarshal(body, &request) != nil || len(request.Messages) != 2 {
			t.Error("invalid model request")
			http.Error(w, "bad request", 400)
			return
		}
		var input struct {
			Findings []struct {
				ID string `json:"id"`
			} `json:"findings"`
			Investigations []struct {
				ID                  string `json:"id"`
				ImagePixelsProvided *bool  `json:"image_pixels_provided"`
				ClipCount           int    `json:"clip_count"`
				ScreenshotCount     int    `json:"screenshot_count"`
			} `json:"stall_investigations"`
		}
		if json.Unmarshal([]byte(request.Messages[1].Content), &input) != nil || len(input.Investigations) != 1 || input.Investigations[0].ID != "S001" || input.Investigations[0].ImagePixelsProvided == nil || *input.Investigations[0].ImagePixelsProvided || input.Investigations[0].ClipCount != 1 || input.Investigations[0].ScreenshotCount != 3 {
			t.Errorf("model did not receive scoped temporal evidence: %.1000s", request.Messages[1].Content)
		}
		found := false
		for _, finding := range input.Findings {
			if finding.ID == "S001-E001" {
				found = true
			}
			if !strings.HasPrefix(finding.ID, "S001-E") {
				t.Errorf("model received unrelated evidence ID %s", finding.ID)
			}
		}
		if !found {
			t.Error("selected stall evidence missing from model input")
		}
		for _, secret := range []string{"private-stall-source-token", "private-stall-header-token", "127.0.0.1", "source.flv"} {
			if bytes.Contains(body, []byte(secret)) {
				t.Errorf("model exposed local evidence metadata %s", secret)
			}
		}
		for _, header := range []string{`"request_headers":`, `"response_headers":`} {
			if strings.Contains(request.Messages[1].Content, header) {
				t.Errorf("model received header text property %s", header)
			}
		}
		const answer = `{"summary":"已对照卡顿区间内的时序证据","hypotheses":[{"claim":"核对缓冲耗尽前后的源端发送间隔","evidence_ids":["S001-E001"],"next_check":"对照同一时段的推流端日志"}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}})
	}))
	defer model.Close()
	t.Setenv("STREAMANALYZER_OPENAI_BASE_URL", model.URL+"/v1")
	t.Setenv("STREAMANALYZER_OPENAI_MODEL", "stall-evidence-fixture")
	t.Setenv("STREAMANALYZER_OPENAI_API_KEY", "test-model-key")
	// This original workflow verifies the text-only model contract. Image
	// requests and fallback are exercised by the dedicated multimodal flows.
	t.Setenv("STREAMANALYZER_MODEL_IMAGE_INPUT", diagnostic.ImageInputText)
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/diagnostics"
	request, _ := json.Marshal(diagnostic.SampleRequest{URL: origin.URL + "/source.flv?token=private-stall-source-token", Seconds: 3, Playback: true})
	created := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/runs", http.StatusAccepted, request))
	if created.Player == nil || created.Player.Token == "" {
		t.Fatal("player grant missing")
	}
	path := api + "/runs/" + created.ID
	captured := waitDiagnostic(t, server, path)
	if captured.Report == nil || captured.Status != "done" {
		t.Fatalf("media capture failed: %+v", captured)
	}
	record := diagnostic.PlayerRecording{Version: 1, Revision: 1, StartedAt: captured.Report.Manifest.Observation.StartedAt, ElapsedMS: 1400, Complete: true, Engine: "mpegtsjs", Source: "same_capture",
		Events:  []diagnostic.PlayerEvent{{Kind: "start"}, {AtMS: 100, Kind: "playing"}, {AtMS: 110, Kind: "first_frame", Method: "video_frame_callback"}, {AtMS: 400, Kind: "waiting"}, {AtMS: 1000, Kind: "playing"}, {AtMS: 1200, Kind: "stop"}},
		Samples: []diagnostic.PlayerSample{{AtMS: 100, CurrentMS: 100, BufferMS: 900, ReadyState: 4}, {AtMS: 350, CurrentMS: 500, BufferMS: 50, ReadyState: 3}, {AtMS: 450, CurrentMS: 500, BufferMS: 0, ReadyState: 2}, {AtMS: 700, CurrentMS: 500, BufferMS: 0, ReadyState: 2}, {AtMS: 1100, CurrentMS: 600, BufferMS: 1200, ReadyState: 4}, {AtMS: 1200, CurrentMS: 700, BufferMS: 1100, ReadyState: 4}},
	}
	telemetry, _ := json.Marshal(record)
	save, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL()+path+"/player", bytes.NewReader(telemetry))
	if err != nil {
		t.Fatal(err)
	}
	save.Header.Set("Content-Type", "application/json")
	save.Header.Set("X-StreamAnalyzer-Player", created.Player.Token)
	save.Header.Set("Origin", server.URL())
	saved, err := http.DefaultClient.Do(save)
	if err != nil {
		t.Fatal(err)
	}
	saveBody, _ := io.ReadAll(saved.Body)
	saved.Body.Close()
	if saved.StatusCode != http.StatusOK {
		t.Fatalf("save telemetry failed: %d %.500s", saved.StatusCode, saveBody)
	}
	diagnosticRequest(t, server, http.MethodPost, path+"/stalls", http.StatusBadRequest, []byte(`{"stall_index":99}`))
	queued := decodeDiagnostic[diagnostic.StallInvestigation](t, diagnosticRequest(t, server, http.MethodPost, path+"/stalls", http.StatusAccepted, []byte(`{"stall_index":1}`)))
	if queued.ID != "S001" || queued.StallIndex != 1 {
		t.Fatalf("wrong investigation accepted: %+v", queued)
	}
	completed := waitStallDiagnostic(t, server, path)
	item := completed.Report.StallInvestigations[0]
	if item.Status != "complete" || item.Media.Status != "ready" || len(item.Media.Assets) != 4 || item.ModelState != "complete" || item.Explanation == nil || item.Explanation.Model != "stall-evidence-fixture" {
		t.Fatalf("investigation did not complete: %+v", item)
	}
	if len(item.Correlation.Gaps) == 0 || len(item.Correlation.BufferSamples) == 0 || len(item.Correlation.Keyframes) == 0 || len(item.Correlation.Findings) == 0 {
		t.Fatalf("missing linked receive/buffer/keyframe evidence: %+v", item.Correlation)
	}
	if modelCalls.Load() != 1 {
		t.Fatalf("unexpected model calls %d", modelCalls.Load())
	}
	repeated := decodeDiagnostic[diagnostic.StallInvestigation](t, diagnosticRequest(t, server, http.MethodPost, path+"/stalls", http.StatusOK, []byte(`{"stall_index":1}`)))
	if !reflect.DeepEqual(repeated, item) || modelCalls.Load() != 1 {
		t.Fatal("selecting the same stall repeated the investigation")
	}
	for _, asset := range item.Media.Assets {
		assetPath := path + "/stall-assets/" + item.ID + "/" + filepath.Base(asset.Name)
		data := diagnosticRequest(t, server, http.MethodGet, assetPath, http.StatusOK, nil)
		hash := sha256.Sum256(data)
		if asset.Bytes != int64(len(data)) || asset.SHA256 != hex.EncodeToString(hash[:]) {
			t.Fatal("downloaded asset differs from recorded hash")
		}
		if asset.Kind == "screenshot" && !bytes.HasPrefix(data, []byte{0xff, 0xd8}) {
			t.Fatal("screenshot endpoint returned invalid image")
		}
		if asset.Kind == "clip" {
			rangeRequest, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL()+assetPath, nil)
			rangeRequest.Header.Set("Range", "bytes=0-15")
			response, err := http.DefaultClient.Do(rangeRequest)
			if err != nil {
				t.Fatal(err)
			}
			partial, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != http.StatusPartialContent || len(partial) != 16 || !bytes.Equal(partial, data[:16]) || !strings.HasPrefix(response.Header.Get("Content-Range"), "bytes 0-15/") {
				t.Fatal("clip endpoint does not support browser range playback")
			}
		}
	}
	diagnosticRequest(t, server, http.MethodGet, path+"/stall-assets/S001/unknown.jpg", http.StatusNotFound, nil)
	html := diagnosticRequest(t, server, http.MethodGet, path+"/report.html", http.StatusOK, nil)
	if !bytes.Contains(html, []byte("data:image/jpeg;base64,")) || !bytes.Contains(html, []byte("S001-E001")) || !bytes.Contains(html, []byte(item.Explanation.Summary)) {
		t.Fatal("report omitted embedded screenshots or scoped advice")
	}
	dump := diagnosticRequest(t, server, http.MethodGet, path+"/dump", http.StatusOK, nil)
	if !bytes.Equal(dump, media) {
		t.Fatal("investigation modified the original dump")
	}
	bundle := diagnosticRequest(t, server, http.MethodGet, path+"/evidence.zip", http.StatusOK, nil)
	origin.Close()
	model.Close()
	imported := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/import", http.StatusCreated, bundle))
	replayed := waitDiagnostic(t, server, api+"/runs/"+imported.ID)
	if !reflect.DeepEqual(completed.Report, replayed.Report) {
		t.Fatalf("offline report changed investigation or advice: original=%+v replayed=%+v", completed.Report.StallInvestigations, replayed.Report.StallInvestigations)
	}
	if !bytes.Equal(diagnosticRequest(t, server, http.MethodGet, api+"/runs/"+imported.ID+"/dump", http.StatusOK, nil), media) {
		t.Fatal("offline import changed raw stream")
	}
	if originCalls.Load() != 1 || modelCalls.Load() != 1 {
		t.Fatal("offline import made a source or model request")
	}
	tampered := diagnosticRequest(t, server, http.MethodPost, api+"/import", http.StatusBadRequest, tamperStallDiagnosticAsset(t, bundle))
	if !bytes.Contains(tampered, []byte("SHA-256")) {
		t.Fatalf("asset tampering was not rejected by hash: %s", tampered)
	}
}

func stallDiagnosticMediaTool(t *testing.T, name string) string {
	t.Helper()
	for _, dir := range []string{"", "/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"} {
		if path, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return path
		}
	}
	t.Skipf("%s not installed", name)
	return ""
}

func waitStallDiagnostic(t *testing.T, server *Server, path string) diagnostic.Detail {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last diagnostic.Detail
	for time.Now().Before(deadline) {
		last = decodeDiagnostic[diagnostic.Detail](t, diagnosticRequest(t, server, http.MethodGet, path, http.StatusOK, nil))
		if last.Report != nil && len(last.Report.StallInvestigations) > 0 {
			status := last.Report.StallInvestigations[0].Status
			if status != "queued" && status != "running" {
				return last
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("stall investigation did not finish: %+v", last.Report)
	return diagnostic.Detail{}
}

func tamperStallDiagnosticAsset(t *testing.T, input []byte) []byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(input), int64(len(input)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	seen := false
	for _, entry := range archive.File {
		if entry.Name != "stalls/S001/during.jpg" {
			if err := writer.Copy(entry); err != nil {
				t.Fatal(err)
			}
			continue
		}
		seen = true
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || len(data) == 0 {
			t.Fatal("derived screenshot missing")
		}
		data[len(data)-1] ^= 1
		target, err := writer.CreateHeader(&zip.FileHeader{Name: entry.Name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := target.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil || !seen {
		t.Fatalf("unable to tamper with derived asset: %v", err)
	}
	return output.Bytes()
}
