package appserver

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/hlsorigin"
)

func TestHLSSimulatorServesBaselineAndFaults(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/hls"
	start := func(config hlsorigin.Config) hlsorigin.Status {
		t.Helper()
		body, _ := json.Marshal(config)
		return decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "POST", api+"/start", http.StatusCreated, body))
	}
	media := func(status hlsorigin.Status, name string) string { return path.Join(path.Dir(status.Playlist), name) }
	stop := func() { diagnosticRequest(t, server, "POST", api+"/stop", http.StatusOK, nil) }
	diagnosticRequest(t, server, "GET", "/simulators/hls", http.StatusOK, nil)
	diagnosticRequest(t, server, "POST", api+"/start", http.StatusBadRequest, []byte(`{"mode":"delay","segment":13,"delay_ms":500}`))
	normal := start(hlsorigin.Config{Mode: "normal"})
	playlist := diagnosticRequest(t, server, "GET", normal.Playlist, http.StatusOK, nil)
	if strings.Count(string(playlist), "#EXTINF:") != hlsorigin.SegmentCount {
		t.Fatalf("invalid fixture playlist: %s", playlist)
	}
	clip := diagnosticRequest(t, server, "GET", media(normal, "segment-04.ts"), http.StatusOK, nil)
	if len(clip) < 188 || len(clip)%188 != 0 || clip[0] != 0x47 {
		t.Fatal("expected MPEG-TS fixture")
	}
	stop()
	diagnosticRequest(t, server, "GET", normal.Playlist, http.StatusNotFound, nil)

	delayed := start(hlsorigin.Config{Mode: "delay", Segment: 4, DelayMS: 500})
	begin := time.Now()
	diagnosticRequest(t, server, "GET", media(delayed, "segment-04.ts"), http.StatusOK, nil)
	if time.Since(begin) < 490*time.Millisecond {
		t.Fatal("configured segment did not wait")
	}
	status := decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "GET", api, http.StatusOK, nil))
	if status.FaultHits != 1 || status.LastFault == nil || status.LastFault.Before(begin) {
		t.Fatalf("missing fault trigger: %+v", status)
	}
	stop()

	missing := start(hlsorigin.Config{Mode: "not_found", Segment: 4})
	diagnosticRequest(t, server, "HEAD", media(missing, "segment-04.ts"), http.StatusOK, nil)
	diagnosticRequest(t, server, "GET", media(missing, "segment-03.ts"), http.StatusOK, nil)
	diagnosticRequest(t, server, "GET", media(missing, "segment-04.ts"), http.StatusNotFound, nil)

	// The workbench sends its playback requests through the per-run proxy.
	// Confirm the proxy exposes the requested fault while neighbors stay valid.
	request, _ := json.Marshal(diagnostic.SampleRequest{URL: server.URL() + missing.Playlist, Protocol: diagnostic.ProtocolHLS, Seconds: 3, Playback: true})
	created := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/runs", http.StatusAccepted, request))
	if created.Player == nil || created.Player.Transport != "hls" {
		t.Fatalf("no HLS playback grant: %+v", created.Player)
	}
	proxyPlaylist := diagnosticRequest(t, server, "GET", created.Player.URL, http.StatusOK, nil)
	var segments []string
	for _, line := range strings.Split(string(proxyPlaylist), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			segments = append(segments, path.Dir(created.Player.URL)+"/"+line)
		}
	}
	if len(segments) != hlsorigin.SegmentCount {
		t.Fatalf("unexpected playback playlist: %s", proxyPlaylist)
	}
	diagnosticRequest(t, server, "GET", segments[2], http.StatusOK, nil)
	diagnosticRequest(t, server, "GET", segments[3], http.StatusNotFound, nil)
	waitDiagnostic(t, server, "/api/diagnostics/runs/"+created.ID)
}
