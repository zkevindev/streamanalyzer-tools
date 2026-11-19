package appserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/onvif"
)

func TestRTSPUDPDiagnosticHTTPWorkflow(t *testing.T) {
	clearDiagnosticModelEnv(t)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte("native_rtsp_udp_receive")) || !bytes.Contains(body, []byte("rtp_packets")) || bytes.Contains(body, []byte("127.0.0.1")) || bytes.Contains(body, []byte("source_ip")) {
			t.Error("model lost UDP boundary or exposed raw addressing")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"summary":"已核对 UDP 接收证据","hypotheses":[]}`}}}})
	}))
	defer model.Close()
	t.Setenv("STREAMANALYZER_MODEL_PROVIDER", diagnostic.ProviderOpenAICompatible)
	t.Setenv("STREAMANALYZER_MODEL_BASE_URL", model.URL+"/v1")
	t.Setenv("STREAMANALYZER_MODEL_NAME", "udp-model-fixture")
	server := testDiagnosticServer(t, t.TempDir())
	manager := &onvif.Manager{}
	t.Cleanup(manager.Close)
	status, err := manager.Start(onvif.Config{BindIP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(diagnostic.SampleRequest{Protocol: diagnostic.ProtocolRTSP, Transport: "udp", URL: status.StreamURL, Seconds: 1})
	const api = "/api/diagnostics"
	run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/runs", http.StatusAccepted, request))
	path := api + "/runs/" + run.ID
	detail := waitDiagnostic(t, server, path)
	if detail.Report == nil || detail.Report.RTSP == nil || detail.Report.RTSP.Transport != "udp" || detail.Report.RTSP.RTPPackets == 0 || detail.Report.RTSP.UDPDatagrams == 0 || detail.Report.RTSP.FilteredDatagrams != 0 {
		t.Fatalf("UDP sample failed: %+v", detail.Report)
	}
	raw := diagnosticRequest(t, server, http.MethodGet, path+"/dump", http.StatusOK, nil)
	html := diagnosticRequest(t, server, http.MethodGet, path+"/report.html", http.StatusOK, nil)
	if !bytes.Contains(html, []byte("UDP 单播 RTP/RTCP")) || !bytes.Contains(html, []byte("udp-datagrams.bin")) {
		t.Fatal("UDP HTML omitted transport or raw boundaries")
	}
	bundle := diagnosticRequest(t, server, http.MethodGet, path+"/evidence.zip", http.StatusOK, nil)
	manager.Close()
	replay := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/import", http.StatusCreated, bundle))
	replayPath := api + "/runs/" + replay.ID
	replayed := waitDiagnostic(t, server, replayPath)
	if !reflect.DeepEqual(detail.Report, replayed.Report) {
		t.Fatal("UDP report changed during offline replay")
	}
	if !bytes.Equal(raw, diagnosticRequest(t, server, http.MethodGet, replayPath+"/dump", http.StatusOK, nil)) {
		t.Fatal("UDP original archive changed")
	}
	explanation := decodeDiagnostic[diagnostic.Explanation](t, diagnosticRequest(t, server, http.MethodPost, replayPath+"/explain", http.StatusOK, nil))
	if explanation.Model != "udp-model-fixture" {
		t.Fatal("UDP imported evidence could not be explained")
	}
}

func TestRTSPUDPTracksWrongSourceAndRestoration(t *testing.T) {
	app := testDiagnosticServer(t, t.TempDir())
	camera := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, app, "POST", onvifAPI+"/start", 201, []byte(`{"udp_source_fault_ms":1200}`)))
	input, _ := json.Marshal(diagnostic.SampleRequest{Protocol: diagnostic.ProtocolRTSP, Transport: "udp", URL: camera.StreamURL, Seconds: 5})
	run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, app, "POST", "/api/diagnostics/runs", 202, input))
	path := "/api/diagnostics/runs/" + run.ID
	detail := waitDiagnostic(t, app, path)
	state := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, app, "GET", onvifAPI, 200, nil))
	if detail.Report == nil || detail.Report.RTSP == nil || len(detail.Report.RTSP.UDPSourceEvidence) != 1 || state.UDPSourceFault == nil {
		t.Fatalf("missing UDP source evidence: %+v, state=%+v", detail.Report, state)
	}
	fault := state.UDPSourceFault
	evidence := detail.Report.RTSP.UDPSourceEvidence[0]
	if !evidence.SameIP || evidence.NegotiatedPort != fault.NegotiatedPort || evidence.ObservedPort != fault.AlternatePort ||
		evidence.FilteredPackets < 1 || uint64(evidence.FilteredPackets) != fault.AlternatePackets || fault.RestoredAt.IsZero() ||
		evidence.RestoredMS == nil || *evidence.RestoredMS <= evidence.LastMS || detail.Report.RTSP.FirstVideoRTPMS == nil ||
		*detail.Report.RTSP.FirstVideoRTPMS >= evidence.FirstMS {
		t.Fatalf("failed port roundtrip: fault=%+v, evidence=%+v", fault, evidence)
	}
	body := diagnosticRequest(t, app, "GET", path+"/report.html", 200, nil)
	if !bytes.Contains(body, []byte("RTP 来源过滤")) {
		t.Fatal("report omitted source evidence")
	}
}
