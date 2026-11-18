package appserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
)

func TestDiagnosticRedirectTimingSurvivesEvidenceReplay(t *testing.T) {
	clearDiagnosticModelEnv(t)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/entry":
			time.Sleep(15 * time.Millisecond)
			http.Redirect(w, r, "/stream.flv?token=redirect-secret", http.StatusFound)
		case "/stream.flv":
			time.Sleep(20 * time.Millisecond)
			w.Header().Set("Content-Type", "video/x-flv")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			time.Sleep(25 * time.Millisecond)
			_, _ = w.Write(testFLV())
		default:
			http.NotFound(w, r)
		}
	}))
	defer source.Close()
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/diagnostics"
	body, _ := json.Marshal(diagnostic.SampleRequest{URL: source.URL + "/entry", Seconds: 1})
	run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/runs", http.StatusAccepted, body))
	path := api + "/runs/" + run.ID
	captured := waitDiagnostic(t, server, path)
	report := captured.Report
	if report == nil {
		t.Fatalf("capture has no report: %+v", captured)
	}
	requests := report.Manifest.Observation.Requests
	if len(requests) != 2 || requests[0].HTTPStatus != 302 || requests[1].HTTPStatus != 200 ||
		requests[0].TotalMS < 10 || requests[1].TotalMS < 15 {
		t.Fatalf("redirect response timings missing: %+v", requests)
	}
	payload := report.Manifest.Observation.FirstPayloadMS
	if payload == nil || *payload < requests[1].EndMS+15 ||
		report.Metrics.FirstAudioMS == nil || report.Metrics.FirstVideoMS == nil ||
		*report.Metrics.FirstVideoMS < *payload || *report.Metrics.FirstAudioMS < *payload {
		t.Fatalf("headers, payload and media milestones differ: %+v", report)
	}
	exported := diagnosticRequest(t, server, http.MethodGet, path+"/report.json", http.StatusOK, nil)
	html := diagnosticRequest(t, server, http.MethodGet, path+"/report.html", http.StatusOK, nil)
	if bytes.Contains(exported, []byte("redirect-secret")) || bytes.Contains(html, []byte("redirect-secret")) {
		t.Fatal("timing report exposed a redirect query")
	}
	bundle := diagnosticRequest(t, server, http.MethodGet, path+"/evidence.zip", http.StatusOK, nil)
	source.Close()
	imported := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/import", http.StatusCreated, bundle))
	replayed := waitDiagnostic(t, server, api+"/runs/"+imported.ID)
	if !reflect.DeepEqual(report, replayed.Report) {
		t.Fatal("offline replay changed captured request timings")
	}
}
