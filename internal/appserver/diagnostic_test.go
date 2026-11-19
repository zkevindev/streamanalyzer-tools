package appserver

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
)

func TestHTTPFLVDiagnosticWorkflow(t *testing.T) {
	clearDiagnosticModelEnv(t)
	flv := testFLV()
	var modelCalls atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test/live.flv":
			if r.URL.Query().Get("token") != "private-test-token" {
				t.Error("playback query was dropped before capture")
			}
			w.Header().Set("Content-Type", "video/x-flv")
			_, _ = w.Write(flv)
		case "/v1/chat/completions":
			body, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-model-key" ||
				bytes.Contains(body, []byte("private-test-token")) || bytes.Contains(body, []byte("live.flv")) ||
				!bytes.Contains(body, []byte("video_dts_gap")) {
				t.Error("unexpected provider request, or playback details leaked")
			}
			id := "NOT-IN-REPORT"
			if modelCalls.Add(1) > 1 {
				id = "F001"
			}
			answer := fmt.Sprintf(`{"summary":"测试摘要","hypotheses":[{"priority":"P1","claim":"检查编码时间戳","evidence_ids":[%q],"next_check":"对照源站采样"}]}`, id)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]string{"content": answer}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	t.Setenv("STREAMANALYZER_OPENAI_BASE_URL", origin.URL+"/v1")
	t.Setenv("STREAMANALYZER_OPENAI_MODEL", "test-model")
	t.Setenv("STREAMANALYZER_OPENAI_API_KEY", "test-model-key")

	dir := t.TempDir()
	server := testDiagnosticServer(t, dir)
	path := "/api/diagnostics"
	job := diagnosticRequest(t, server, http.MethodPost, path+"/runs", http.StatusAccepted,
		[]byte(fmt.Sprintf(`{"url":%q,"seconds":2}`, origin.URL+"/test/live.flv?token=private-test-token")))
	run := decodeDiagnostic[diagnostic.Run](t, job)
	runPath := path + "/runs/" + run.ID
	captured := waitDiagnostic(t, server, runPath)
	if captured.Status != "done" || captured.Report == nil || captured.Report.Metrics.VideoFrames != 2 ||
		captured.Report.Metrics.AudioFrames != 1 || captured.Origin != origin.URL ||
		captured.Protocol != diagnostic.ProtocolHTTPFLV || captured.DumpName != "stream.flv" ||
		!reflect.DeepEqual(captured.Report.Metrics.VideoCodecs, []string{"h264"}) ||
		!reflect.DeepEqual(captured.Report.Metrics.AudioCodecs, []string{"aac"}) ||
		len(captured.Report.Findings) != 1 || captured.Report.Findings[0].Code != "video_dts_gap" {
		t.Fatalf("unexpected rule report: %+v", captured.Report)
	}
	finding := captured.Report.Findings[0]
	if len(finding.Evidence) != 2 || finding.Evidence[0].Offset != 13 || finding.Evidence[1].DTS != 3200 {
		t.Fatalf("incorrect evidence location: %+v", finding)
	}
	raw := diagnosticRequest(t, server, http.MethodGet, runPath+"/stream.flv", http.StatusOK, nil)
	if !bytes.Equal(raw, flv) {
		t.Fatal("raw dump differs from the HTTP response")
	}
	diagnosticRequest(t, server, http.MethodPost, runPath+"/explain", http.StatusBadGateway, nil)
	explanation := decodeDiagnostic[diagnostic.Explanation](t, diagnosticRequest(t, server, http.MethodPost, runPath+"/explain", http.StatusOK, nil))
	if explanation.Model != "test-model" || explanation.Hypotheses[0].EvidenceIDs[0] != finding.ID || explanation.Hypotheses[0].Priority != "P1" || explanation.ReportInputEvidence == nil || explanation.ReportInputEvidence.Mode != "text" || explanation.ReportInputEvidence.FallbackCode != "no_images" {
		t.Fatalf("incorrect saved explanation: %+v", explanation)
	}
	diagnosticRequest(t, server, http.MethodPost, runPath+"/explain", http.StatusOK, nil)
	if modelCalls.Load() != 2 {
		t.Fatal("accepted model output was not reused")
	}
	html := diagnosticRequest(t, server, http.MethodGet, runPath+"/report.html", http.StatusOK, nil)
	if !bytes.Contains(html, []byte(`href="#F001"`)) || bytes.Contains(html, []byte("private-test-token")) {
		t.Fatal("missing evidence link or URL token appeared in HTML")
	}
	bundle := diagnosticRequest(t, server, http.MethodGet, runPath+"/evidence.zip", http.StatusOK, nil)

	// Replay with the playback and model server offline. The rule report, global
	// explanation with priorities, and original dump all come from the ZIP alone.
	origin.Close()
	replay := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, path+"/import", http.StatusCreated, bundle))
	detail := waitDiagnostic(t, server, path+"/runs/"+replay.ID)
	if detail.Mode != "replay" || !reflect.DeepEqual(detail.Report, captured.Report) || !reflect.DeepEqual(detail.Explanation, &explanation) || modelCalls.Load() != 2 {
		t.Fatalf("offline report differs: %+v", detail)
	}
	diagnosticRequest(t, server, http.MethodPost, path+"/import", http.StatusBadRequest, tamperWithDiagnosticBundle(t, bundle))

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened := testDiagnosticServer(t, dir)
	if got := waitDiagnostic(t, reopened, runPath); got.Explanation == nil || !reflect.DeepEqual(got.Report, captured.Report) {
		t.Fatalf("history did not survive restart: %+v", got)
	}
}

func TestMultiProtocolDiagnosticWorkflow(t *testing.T) {
	clearDiagnosticModelEnv(t)
	const playlist = "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:3\n#EXTINF:2,\nclip.ts?token=hls-test-secret\n#EXTINF:2,\nmissing.ts\n#EXT-X-ENDLIST\n"
	segment := make([]byte, 188*2)
	segment[0], segment[188] = 0x47, 0x47
	hlsOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/live.m3u8":
			io.WriteString(w, playlist)
		case "/clip.ts":
			if r.URL.Query().Get("token") != "hls-test-secret" {
				t.Error("playlist reference lost its authorization query")
			}
			w.Write(segment)
		default:
			http.NotFound(w, r)
		}
	}))
	defer hlsOrigin.Close()

	rtmpOrigin, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rtmpOrigin.Close()
	go func() {
		conn, err := rtmpOrigin.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	modelCalls := make(chan string, 2)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		var summary struct {
			Protocol string `json:"protocol"`
		}
		if r.URL.Path != "/v1/chat/completions" ||
			json.Unmarshal(data, &request) != nil || len(request.Messages) != 2 ||
			json.Unmarshal([]byte(request.Messages[1].Content), &summary) != nil ||
			bytes.Contains(data, []byte("test-secret")) || bytes.Contains(data, []byte("127.0.0.1")) ||
			bytes.Contains(data, []byte("clip.ts")) {
			t.Error("unknown protocol or raw connection details were sent to the model")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		modelCalls <- summary.Protocol
		const answer = `{"summary":"已检查采样证据","hypotheses":[{"claim":"检查源站","evidence_ids":["F001"],"next_check":"在源站复测"}]}`
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": answer}}},
		})
	}))
	defer model.Close()
	t.Setenv("STREAMANALYZER_OPENAI_BASE_URL", model.URL+"/v1")
	t.Setenv("STREAMANALYZER_OPENAI_MODEL", "test-model")
	t.Setenv("STREAMANALYZER_OPENAI_API_KEY", "")

	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/diagnostics"
	for _, tc := range []struct {
		protocol, target, dumpName, status string
		closeOrigin                        func()
	}{
		{diagnostic.ProtocolHLS, hlsOrigin.URL + "/live.m3u8", "stream.hls.zip", "partial", hlsOrigin.Close},
		{diagnostic.ProtocolRTMP, "rtmp://" + rtmpOrigin.Addr().String() + "/live/stream?token=rtmp-test-secret", "stream.rtmp", "failed", func() { _ = rtmpOrigin.Close() }},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			body, err := json.Marshal(diagnostic.SampleRequest{Protocol: tc.protocol, URL: tc.target, Seconds: 2})
			if err != nil {
				t.Fatal(err)
			}
			run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/runs", http.StatusAccepted, body))
			path := api + "/runs/" + run.ID
			detail := waitDiagnostic(t, server, path)
			if detail.Protocol != tc.protocol || detail.Status != tc.status || detail.DumpName != tc.dumpName ||
				detail.Report == nil || detail.Report.Manifest.Protocol != tc.protocol {
				t.Fatalf("unexpected protocol detail: %+v", detail)
			}
			raw := diagnosticRequest(t, server, http.MethodGet, path+"/dump", http.StatusOK, nil)
			if tc.protocol == diagnostic.ProtocolHLS {
				stats := detail.Report.HLS
				if stats == nil || stats.SegmentsCaptured != 1 || stats.SegmentsFailed != 1 {
					t.Fatalf("unexpected HLS report: %+v", detail.Report)
				}
				assertHLSRawObjects(t, raw, []byte(playlist), segment)
			}
			html := diagnosticRequest(t, server, http.MethodGet, path+"/report.html", http.StatusOK, nil)
			if !bytes.Contains(html, []byte(tc.dumpName)) || bytes.Contains(html, []byte("test-secret")) {
				t.Fatal("incorrect raw artifact name or signed URL in HTML")
			}
			bundle := diagnosticRequest(t, server, http.MethodGet, path+"/evidence.zip", http.StatusOK, nil)
			tc.closeOrigin()
			importRun := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/import", http.StatusCreated, bundle))
			importPath := api + "/runs/" + importRun.ID
			replayed := waitDiagnostic(t, server, importPath)
			if replayed.Protocol != tc.protocol || replayed.Mode != "replay" || !reflect.DeepEqual(detail.Report, replayed.Report) {
				t.Fatalf("replay did not reproduce the original report: %+v", replayed)
			}
			explanation := decodeDiagnostic[diagnostic.Explanation](t, diagnosticRequest(t, server, http.MethodPost, importPath+"/explain", http.StatusOK, nil))
			if explanation.Model != "test-model" || <-modelCalls != tc.protocol {
				t.Fatalf("wrong model protocol or metadata: %+v", explanation)
			}
			diagnosticRequest(t, server, http.MethodGet, path+"/stream.flv", http.StatusNotFound, nil)
		})
	}
}

func assertHLSRawObjects(t *testing.T, data, playlist, segment []byte) {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var foundPlaylist, foundSegment bool
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		foundPlaylist = foundPlaylist || bytes.Equal(body, playlist)
		foundSegment = foundSegment || bytes.Equal(body, segment)
	}
	if !foundPlaylist || !foundSegment {
		t.Fatal("HLS dump lacks exact playlist or segment bytes")
	}
}

func TestDiagnosticRunIndexMigratesExistingHistory(t *testing.T) {
	clearDiagnosticModelEnv(t)
	dir := t.TempDir()
	root := filepath.Join(dir, "diagnostics")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "index.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE runs (id TEXT PRIMARY KEY, mode TEXT NOT NULL, status TEXT NOT NULL,
		origin TEXT NOT NULL, created_at INTEGER NOT NULL, error TEXT NOT NULL, report_ready INTEGER NOT NULL);
		INSERT INTO runs VALUES ('old-run', 'sample', 'interrupted', 'http://origin.example', 1000, '', 0);`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	server := testDiagnosticServer(t, dir)
	runs := decodeDiagnostic[[]diagnostic.Run](t, diagnosticRequest(t, server, http.MethodGet, "/api/diagnostics/runs", http.StatusOK, nil))
	if len(runs) != 1 || runs[0].ID != "old-run" || runs[0].Protocol != diagnostic.ProtocolHTTPFLV || runs[0].Status != "interrupted" {
		t.Fatalf("unexpected migrated history: %+v", runs)
	}
}

func TestHTTPFLVDiagnosticInterruptedCapture(t *testing.T) {
	flv := testFLV()
	sent := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hold":
			_, _ = w.Write(flv)
			w.(http.Flusher).Flush()
			close(sent)
			<-r.Context().Done()
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer origin.Close()
	clearDiagnosticModelEnv(t)
	server := testDiagnosticServer(t, t.TempDir())
	for _, test := range []struct {
		path, status, reason string
		codes                int
	}{
		{"/hold", "canceled", "canceled", 200},
		{"/forbidden", "failed", "http_status", 403},
	} {
		run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, "/api/diagnostics/runs", http.StatusAccepted,
			[]byte(fmt.Sprintf(`{"url":%q,"seconds":3}`, origin.URL+test.path))))
		path := "/api/diagnostics/runs/" + run.ID
		if test.path == "/hold" {
			select {
			case <-sent:
			case <-time.After(2 * time.Second):
				t.Fatal("sample did not reach the HTTP origin")
			}
			diagnosticRequest(t, server, http.MethodPost, path+"/stop", http.StatusAccepted, nil)
		}
		got := waitDiagnostic(t, server, path)
		if got.Status != test.status || got.Report == nil || got.Report.Manifest.Observation.EndReason != test.reason ||
			got.Report.Manifest.Observation.HTTPStatus != test.codes {
			t.Fatalf("unexpected report for %s: %+v", test.path, got)
		}
	}
}

func testDiagnosticServer(t *testing.T, dir string) *Server {
	t.Helper()
	cfg := &Config{}
	cfg.Server.DataDir = dir
	server, err := Start(t.Context(), "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
	})
	return server
}

func diagnosticRequest(t *testing.T, server *Server, method, path string, status int, body []byte) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, server.URL()+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.HasSuffix(path, "/import") {
		req.Header.Set("Content-Type", "application/zip")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != status {
		t.Fatalf("%s %s: expected %d, got %d (%v): %.500s", method, path, status, resp.StatusCode, err, data)
	}
	return data
}

func decodeDiagnostic[T any](t *testing.T, data []byte) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func waitDiagnostic(t *testing.T, server *Server, path string) diagnostic.Detail {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		got := decodeDiagnostic[diagnostic.Detail](t, diagnosticRequest(t, server, http.MethodGet, path, http.StatusOK, nil))
		if got.Status != "running" {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("sample did not finish")
	return diagnostic.Detail{}
}

func testFLV() []byte {
	stream := bytes.NewBuffer([]byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0})
	writeTag := func(tagType byte, dts uint32, payload []byte) {
		stream.Write([]byte{tagType, 0, 0, byte(len(payload)), byte(dts >> 16), byte(dts >> 8), byte(dts), byte(dts >> 24), 0, 0, 0})
		stream.Write(payload)
		stream.Write(binary.BigEndian.AppendUint32(nil, uint32(11+len(payload))))
	}
	writeTag(9, 0, []byte{0x17, 1, 0, 0, 0, 0, 0, 0, 2, 0x65, 0x88})
	writeTag(8, 0, []byte{0xaf, 1, 0x11, 0x22})
	writeTag(9, 3200, []byte{0x27, 1, 0, 0, 10, 0, 0, 0, 2, 0x41, 0x88})
	return stream.Bytes()
}

func tamperWithDiagnosticBundle(t *testing.T, data []byte) []byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for _, entry := range archive.File {
		if entry.Name != "stream.flv" {
			if err := writer.Copy(entry); err != nil {
				t.Fatal(err)
			}
			continue
		}
		w, err := writer.Create(entry.Name)
		if err != nil {
			t.Fatal(err)
		}
		raw := testFLV()
		raw[len(raw)-1] ^= 1
		if _, err := w.Write(raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
