package appserver

import (
	"net/http"
	"testing"
)

func TestAutomaticDiagnosticRequiresPlaybackAndValidControls(t *testing.T) {
	clearDiagnosticModelEnv(t)
	server := testDiagnosticServer(t, t.TempDir())
	diagnosticRequest(t, server, http.MethodPost, "/api/diagnostics/runs", http.StatusBadRequest, []byte(`{"url":"http://127.0.0.1:1/live.flv","seconds":1,"auto_investigate":true}`))
	for _, control := range []string{"retry", "stop"} {
		endpoint := "/api/diagnostics/runs/00000000000000000000000000000000/automatic/" + control
		for _, body := range []string{`{"unexpected":true}`, `{} {}`, `[]`, `null`} {
			diagnosticRequest(t, server, http.MethodPost, endpoint, http.StatusBadRequest, []byte(body))
		}
		diagnosticRequest(t, server, http.MethodPost, endpoint, http.StatusNotFound, []byte(`{}`))
	}
}
