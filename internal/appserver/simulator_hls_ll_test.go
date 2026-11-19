package appserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/hlsorigin"
)

func TestHLSLowLatencyBlockingReloadAndDelayedPart(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/hls"
	source := decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "POST", api+"/start", http.StatusCreated, []byte(`{"mode":"ll"}`)))
	req, err := json.Marshal(diagnostic.SampleRequest{Protocol: diagnostic.ProtocolHLS, URL: server.URL() + source.Playlist, Seconds: 10, Playback: true})
	if err != nil {
		t.Fatal(err)
	}
	run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/runs", http.StatusAccepted, req))
	if run.Player == nil {
		t.Fatal("missing playback proxy")
	}
	armed := decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "POST", api+"/part-delay", http.StatusOK, []byte(`{"delay_ms":1700}`)))
	fault := armed.LowLatency.Fault
	client := &http.Client{Timeout: 4 * time.Second}
	base := server.URL() + run.Player.URL
	reloadURL := fmt.Sprintf("%s&_HLS_msn=%d&_HLS_part=%d", base, fault.Sequence, fault.Part)
	before := time.Now()
	res, err := client.Get(reloadURL)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(res.Body)
	res.Body.Close()
	reloadElapsed := time.Since(before)
	if readErr != nil || res.StatusCode != http.StatusOK || reloadElapsed < 190*time.Millisecond || reloadElapsed > 1800*time.Millisecond ||
		!strings.Contains(string(body), "CAN-BLOCK-RELOAD=YES") {
		t.Fatalf("unexpected unblock: %s elapsed %s: %v", body, reloadElapsed, readErr)
	}
	lastPart := regexp.MustCompile(`(?m)^#EXT-X-PART:[^\n]*URI="([^"]+)"[^\n]*$`).FindAllStringSubmatch(string(body), -1)
	if len(lastPart) < 16 {
		t.Fatalf("target part missing after reload: %s", body)
	}
	target := server.URL() + run.Player.URL[:strings.LastIndex(run.Player.URL, "/")+1] + lastPart[len(lastPart)-1][1]
	delay := make(chan error, 1)
	go func() {
		start := time.Now()
		res, err := client.Get(target)
		if err != nil {
			delay <- err
			return
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != 200 || !strings.Contains(string(data), "moof") || time.Since(start) < 1150*time.Millisecond {
			delay <- fmt.Errorf("part delay or bytes incorrect: %s (%d bytes) after %s: %v", res.Status, len(data), time.Since(start), err)
			return
		}
		delay <- nil
	}()
	// A subsequent blocking reload can complete while the target body waits.
	nextURL := fmt.Sprintf("%s&_HLS_msn=%d&_HLS_part=%d", base, fault.Sequence, fault.Part+1)
	res, err = client.Get(nextURL)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if readErr != nil || res.StatusCode != 200 {
		t.Fatalf("failed to refresh playlist while target Part waits: %v, %s", readErr, res.Status)
	}
	select {
	case err := <-delay:
		t.Fatalf("Part finished before the independent reload: %v", err)
	default:
	}
	if err := <-delay; err != nil {
		t.Fatal(err)
	}
	status := decodeDiagnostic[hlsorigin.Status](t, diagnosticRequest(t, server, "GET", api, http.StatusOK, nil))
	if status.LowLatency.BlockedReloads < 2 || status.LowLatency.Fault.RequestedAt.IsZero() ||
		status.LowLatency.Fault.UnblockedAt.Before(status.LowLatency.Fault.ReleaseAt) {
		t.Fatalf("inconsistent LL-HLS observations: %+v %+v", status.LowLatency, status.LowLatency.Fault)
	}
}
