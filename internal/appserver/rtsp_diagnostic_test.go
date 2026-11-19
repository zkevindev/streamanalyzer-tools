package appserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/onvif"
)

func TestRTSPDiagnosticHTTPWorkflow(t *testing.T) {
	clearDiagnosticModelEnv(t)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Contains(body, []byte(`rtsp`)) || !bytes.Contains(body, []byte(`rtp_packets`)) {
			t.Error("model request omitted native RTSP measurements")
		}
		for _, private := range []string{"camera-password-private", "camera-user-private", "camera-token-private", "127.0.0.1", "a=control", "Authorization"} {
			if bytes.Contains(body, []byte(private)) {
				t.Errorf("RTSP model exposed %s", private)
			}
		}
		const answer = `{"summary":"已检查原生 RTSP 接收证据","hypotheses":[]}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}})
	}))
	defer model.Close()
	t.Setenv("STREAMANALYZER_MODEL_PROVIDER", diagnostic.ProviderOpenAICompatible)
	t.Setenv("STREAMANALYZER_MODEL_BASE_URL", model.URL+"/v1")
	t.Setenv("STREAMANALYZER_MODEL_NAME", "rtsp-model-fixture")
	server := testDiagnosticServer(t, t.TempDir())
	manager := &onvif.Manager{}
	t.Cleanup(manager.Close)
	status, err := manager.Start(onvif.Config{BindIP: "127.0.0.1", RTSPUsername: "camera-user-private", RTSPPassword: "camera-password-private"})
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(status.StreamURL)
	target.User = url.UserPassword("camera-user-private", "camera-password-private")
	target.RawQuery = "token=camera-token-private"
	request, _ := json.Marshal(diagnostic.SampleRequest{Protocol: diagnostic.ProtocolRTSP, URL: target.String(), Seconds: 1, Playback: false})
	const api = "/api/diagnostics"
	run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/runs", http.StatusAccepted, request))
	if run.Player != nil {
		t.Fatal("RTSP capture advertised unsupported browser preview")
	}
	path := api + "/runs/" + run.ID
	captured := waitDiagnostic(t, server, path)
	if captured.Status != "done" || captured.Report == nil || captured.Report.RTSP == nil || captured.Report.RTSP.RTPPackets == 0 || captured.DumpName != "stream.rtsp.zip" {
		t.Fatalf("native RTSP capture failed: %+v", captured)
	}
	raw := diagnosticRequest(t, server, http.MethodGet, path+"/dump", http.StatusOK, nil)
	if len(raw) == 0 {
		t.Fatal("empty native RTSP artifact")
	}
	html := diagnosticRequest(t, server, http.MethodGet, path+"/report.html", http.StatusOK, nil)
	if !bytes.Contains(html, []byte("RTSP")) || !bytes.Contains(html, []byte("stream.rtsp.zip")) || bytes.Contains(html, []byte("camera-password-private")) || bytes.Contains(html, []byte("camera-token-private")) {
		t.Fatal("RTSP HTML omitted native evidence or exposed credentials")
	}
	bundle := diagnosticRequest(t, server, http.MethodGet, path+"/evidence.zip", http.StatusOK, nil)
	manager.Close() // Offline replay must not reconnect to the camera.
	replayedRun := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/import", http.StatusCreated, bundle))
	replayPath := api + "/runs/" + replayedRun.ID
	replayed := waitDiagnostic(t, server, replayPath)
	if !reflect.DeepEqual(captured.Report, replayed.Report) {
		t.Fatal("RTSP report changed after offline import")
	}
	if got := diagnosticRequest(t, server, http.MethodGet, replayPath+"/dump", http.StatusOK, nil); !bytes.Equal(got, raw) {
		t.Fatal("RTSP raw artifact changed after import")
	}
	explanation := decodeDiagnostic[diagnostic.Explanation](t, diagnosticRequest(t, server, http.MethodPost, replayPath+"/explain", http.StatusOK, nil))
	if explanation.Model != "rtsp-model-fixture" {
		t.Fatal("RTSP evidence could not be explained with configured model")
	}
}
