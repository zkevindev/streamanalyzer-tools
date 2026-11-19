package appserver

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/mediasource"
)

func TestRTMPSimulatorSeparatesFailureStages(t *testing.T) {
	for _, test := range []struct{ fault, stage string }{
		{"normal", ""}, {"tls_untrusted", "tls"}, {"handshake_stall", "handshake"}, {"app_reject", "app_connect"}, {"stream_not_found", "play"}, {"no_media", "receive"},
	} {
		t.Run(test.fault, func(t *testing.T) {
			t.Parallel()
			server := testDiagnosticServer(t, t.TempDir())
			const api = "/api/simulators/rtmp"
			status := decodeDiagnostic[mediasource.RTMPStatus](t, diagnosticRequest(t, server, "POST", api+"/start", http.StatusCreated, []byte(`{"fault":"`+test.fault+`"}`)))
			raw, _ := json.Marshal(diagnostic.SampleRequest{Protocol: diagnostic.ProtocolRTMP, URL: status.URL, Seconds: 2})
			run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/runs", http.StatusAccepted, raw))
			report := waitDiagnostic(t, server, "/api/diagnostics/runs/"+run.ID).Report
			if report == nil {
				t.Fatal("missing diagnostic report")
			}
			got := report.Manifest.Observation
			if test.stage == "receive" {
				hasStage := func(name string) bool {
					return slices.ContainsFunc(got.Stages, func(stage diagnostic.CaptureStage) bool { return stage.Name == name })
				}
				if !hasStage("receive") || !hasStage("play") || report.Metrics.VideoFrames != 0 || report.Metrics.AudioFrames != 0 {
					t.Fatalf("PLAY should succeed without any frames: %+v", report)
				}
			} else if got.FailureStage != test.stage {
				t.Fatalf("wanted stage %q, got %+v", test.stage, got)
			}
			if test.fault == "normal" && (report.Metrics.VideoFrames == 0 || report.Metrics.AudioFrames == 0) {
				t.Fatal("normal stream did not provide audio and video")
			}
			state := decodeDiagnostic[mediasource.RTMPStatus](t, diagnosticRequest(t, server, "GET", api, http.StatusOK, nil))
			if state.Connections != 1 || (test.fault != "normal" && state.FaultHits != 1) {
				t.Fatalf("unexpected source evidence: %+v", state)
			}
			diagnosticRequest(t, server, "POST", api+"/stop", http.StatusOK, nil)
		})
	}
}
