package appserver

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/onvif"
)

func TestRTSPReconnectAfterSimulatorOutage(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	source := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, server, "POST", onvifAPI+"/start", http.StatusCreated, []byte(`{"rtsp_disconnect_ms":1600}`)))
	const runs = "/api/diagnostics/runs"
	sample := func(seconds int) diagnostic.Run {
		t.Helper()
		body, err := json.Marshal(diagnostic.SampleRequest{Protocol: diagnostic.ProtocolRTSP, URL: source.StreamURL, Seconds: seconds, Playback: true})
		if err != nil {
			t.Fatal(err)
		}
		run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", runs, http.StatusAccepted, body))
		if run.Player == nil {
			t.Fatal("RTSP preview grant missing")
		}
		return run
	}
	first := sample(10)
	before := waitDiagnostic(t, server, runs+"/"+first.ID)
	if before.Report == nil || before.Report.RTSP == nil || before.Report.RTSP.RTPPackets == 0 ||
		!slices.Contains([]string{"eof", "rtsp_error"}, before.Report.Manifest.Observation.EndReason) ||
		before.Report.Manifest.Observation.ElapsedMS > 5000 {
		t.Fatalf("first session did not receive media and encounter an early disconnect: %+v", before.Report)
	}
	refusedAttempt := sample(2)
	refused := waitDiagnostic(t, server, runs+"/"+refusedAttempt.ID)
	if refused.Report == nil || refused.Report.RTSP == nil || !hasRTSPStatus(refused.Report.RTSP, "DESCRIBE", 503) || refused.Report.RTSP.RTPPackets != 0 {
		t.Fatalf("outage response was not captured: %+v", refused.Report)
	}
	readStatus := func() onvif.RTSPOutageStatus {
		t.Helper()
		status := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, server, "GET", onvifAPI, http.StatusOK, nil))
		if len(status.RTSPOutages) != 2 {
			t.Fatalf("invalid RTSP outage status: %+v", status)
		}
		return status.RTSPOutages[0]
	}
	outage := readStatus()
	if outage.Closed != 1 || outage.Rejected == 0 || outage.End.Sub(outage.Begin) != 1600*time.Millisecond {
		t.Fatalf("outage did not follow configured lifecycle: %+v", outage)
	}
	time.Sleep(max(0, time.Until(outage.End)) + 30*time.Millisecond)
	newAttempt := sample(2)
	reconnected := waitDiagnostic(t, server, runs+"/"+newAttempt.ID)
	if newAttempt.ID == first.ID || reconnected.Report == nil || reconnected.Report.RTSP == nil ||
		reconnected.Report.RTSP.RTPPackets == 0 || reconnected.Report.RTSP.Preview == nil ||
		reconnected.Report.RTSP.Preview.VideoAUs == 0 {
		t.Fatalf("new RTSP connection did not produce preview media: %+v", reconnected.Report)
	}
	for _, method := range []string{"DESCRIBE", "SETUP", "PLAY"} {
		if !hasRTSPStatus(reconnected.Report.RTSP, method, 200) {
			t.Fatalf("new connection skipped successful %s", method)
		}
	}
	if state := readStatus(); state.FirstPlay.Before(state.End) {
		t.Fatalf("missing successful PLAY after the outage: %+v", state)
	}
}

func hasRTSPStatus(metrics *diagnostic.RTSPMetrics, method string, status int) bool {
	return slices.ContainsFunc(metrics.Requests, func(request diagnostic.RTSPRequestTiming) bool {
		return request.Method == method && request.StatusCode == status
	})
}
