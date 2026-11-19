package appserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/hlsorigin"
)

func TestHLSLiveFaultRecoveryOnPlaybackProxy(t *testing.T) {
	const api = "/api/simulators/hls"
	server := testDiagnosticServer(t, t.TempDir())
	for _, kind := range []string{"not_found", "truncate"} {
		t.Run(kind, func(t *testing.T) {
			live := decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "POST", api+"/start", http.StatusCreated, []byte(`{"mode":"live"}`)))
			defer diagnosticRequest(t, server, "POST", api+"/stop", http.StatusOK, nil)
			armed := decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "POST", api+"/live-fault", http.StatusOK, []byte(`{"kind":"`+kind+`"}`)))
			name := fmt.Sprintf("live-%d.ts", armed.LiveFault.Sequence)
			direct := path.Join(path.Dir(live.Playlist), name)
			body, err := json.Marshal(diagnostic.SampleRequest{URL: server.URL() + live.Playlist, Protocol: diagnostic.ProtocolHLS, Seconds: 8, Playback: true})
			if err != nil {
				t.Fatal(err)
			}
			run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/runs", http.StatusAccepted, body))
			if run.Player == nil {
				t.Fatal("missing player grant")
			}
			playlist := diagnosticRequest(t, server, "GET", run.Player.URL, http.StatusOK, nil)
			if strings.Count(string(playlist), "#EXTINF:") != 3 {
				t.Fatal(string(playlist))
			}
			time.Sleep(2030 * time.Millisecond)
			full := diagnosticRequest(t, server, "GET", direct, http.StatusOK, nil)
			before := decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "GET", api, http.StatusOK, nil))
			if before.LiveFault.HitAt != nil {
				t.Fatal("background GET consumed playback-only fault")
			}
			playlist = diagnosticRequest(t, server, "GET", run.Player.URL, http.StatusOK, nil)
			var items []string
			for _, line := range strings.Split(string(playlist), "\n") {
				if line != "" && line[0] != '#' {
					items = append(items, line)
				}
			}
			if len(items) != 3 {
				t.Fatal(string(playlist))
			}
			rel := path.Dir(run.Player.URL) + "/" + items[len(items)-1]
			res, err := http.Get(server.URL() + rel)
			if err != nil {
				t.Fatal(err)
			}
			part, readErr := io.ReadAll(res.Body)
			res.Body.Close()
			if kind == "not_found" && res.StatusCode != http.StatusNotFound {
				t.Fatalf("expected first 404, got %d", res.StatusCode)
			}
			if kind == "truncate" && (res.StatusCode != http.StatusOK || readErr != io.ErrUnexpectedEOF || len(part) != len(full)/2) {
				t.Fatalf("expected truncated HTTP 200, got %d %d bytes %v", res.StatusCode, len(part), readErr)
			}
			if body := diagnosticRequest(t, server, "GET", rel, http.StatusOK, nil); string(body) != string(full) {
				t.Fatalf("retry payload mismatch, expected %d bytes got %d", len(full), len(body))
			}
			after := decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "GET", api, http.StatusOK, nil))
			if after.FaultHits != 1 || after.LiveFault.Attempts != 2 || after.LiveFault.HitAt == nil || after.LiveFault.RetryAt == nil {
				t.Fatalf("missing injection and retry observations: %+v", after.LiveFault)
			}
			diagnosticRequest(t, server, "POST", "/api/diagnostics/runs/"+run.ID+"/stop", http.StatusAccepted, nil)
			waitDiagnostic(t, server, "/api/diagnostics/runs/"+run.ID)
		})
	}
}
