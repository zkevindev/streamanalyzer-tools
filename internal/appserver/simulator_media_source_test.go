package appserver

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/mediasource"
)

func TestMediaSimulatorReportsControlledFaults(t *testing.T) {
	const api = "/api/simulators/media"
	for _, test := range []struct {
		protocol, fault, code string
	}{
		{diagnostic.ProtocolHTTPFLV, "long_gop", "long_gop"},
		{diagnostic.ProtocolHTTPFLV, "dts_backwards", "video_dts_backwards"},
		{diagnostic.ProtocolHTTPTS, "continuity", "ts_continuity_error"},
		{diagnostic.ProtocolHTTPTS, "pcr_jump", "ts_pcr_backwards"},
	} {
		t.Run(test.fault, func(t *testing.T) {
			t.Parallel()
			server := testDiagnosticServer(t, t.TempDir())
			page := diagnosticRequest(t, server, "GET", "/simulators/media", http.StatusOK, nil)
			if !strings.Contains(string(page), "HTTP-FLV / MPEG-TS 模拟器") {
				t.Fatal("media simulator entry missing")
			}
			diagnosticRequest(t, server, "POST", api+"/start", http.StatusBadRequest, []byte(`{"protocol":"http-flv","fault":"continuity"}`))
			raw, err := json.Marshal(mediasource.Config{Protocol: test.protocol, Fault: test.fault})
			if err != nil {
				t.Fatal(err)
			}
			started := decodeDiagnostic[mediasource.Status](t, diagnosticRequest(t, server, "POST", api+"/start", http.StatusCreated, raw))
			if !started.Active || started.URL == "" || started.DurationMS < 5000 {
				t.Fatalf("invalid media source: %+v", started)
			}
			diagnosticRequest(t, server, "HEAD", started.URL, http.StatusOK, nil)
			req, err := json.Marshal(diagnostic.SampleRequest{Protocol: test.protocol, Seconds: 16, URL: server.URL() + started.URL})
			if err != nil {
				t.Fatal(err)
			}
			created := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/runs", http.StatusAccepted, req))
			runPath := "/api/diagnostics/runs/" + created.ID
			var result diagnostic.Detail
			deadline := time.Now().Add(19 * time.Second)
			for time.Now().Before(deadline) {
				result = decodeDiagnostic[diagnostic.Detail](t, diagnosticRequest(t, server, "GET", runPath, http.StatusOK, nil))
				if result.Status != "running" {
					break
				}
				time.Sleep(80 * time.Millisecond)
			}
			if result.Report == nil || result.Status != "done" {
				t.Fatalf("diagnostic source did not finish: %+v", result)
			}
			if !slices.ContainsFunc(result.Report.Findings, func(f diagnostic.Finding) bool {
				return f.Code == test.code && len(f.Evidence) > 0
			}) {
				t.Fatalf("report omitted %s with evidence: %+v", test.code, result.Report.Findings)
			}
			if test.fault == "pcr_jump" && !slices.ContainsFunc(result.Report.Findings, func(f diagnostic.Finding) bool {
				return f.Code == "ts_pcr_gap"
			}) {
				t.Fatal("PCR jump did not report both edges")
			}
			state := decodeDiagnostic[mediasource.Status](t, diagnosticRequest(t, server, "GET", api, http.StatusOK, nil))
			if state.Requests != 1 || state.FaultHits != 1 || state.LastFault == nil {
				t.Fatalf("incorrect source trigger evidence: %+v", state)
			}
			diagnosticRequest(t, server, "POST", api+"/stop", http.StatusOK, nil)
			diagnosticRequest(t, server, "GET", started.URL, http.StatusNotFound, nil)
		})
	}
}
