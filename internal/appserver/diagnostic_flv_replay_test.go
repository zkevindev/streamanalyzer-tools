package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
)

func TestHTTPFLVEvidenceAsPacedOrigin(t *testing.T) {
	flv := testFLV()
	split := len(flv) - 26
	tail := []byte{9, 0, 0}
	recorded := append(bytes.Clone(flv), tail...)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		controller := http.NewResponseController(w)
		w.Header().Set("Content-Type", "video/x-flv")
		w.WriteHeader(http.StatusOK)
		_ = controller.Flush()
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write(recorded[:split])
		_ = controller.Flush()
		time.Sleep(450 * time.Millisecond)
		_, _ = w.Write(recorded[split:len(flv)])
		_ = controller.Flush()
		time.Sleep(190 * time.Millisecond)
		_, _ = w.Write(tail)
	}))
	defer source.Close()
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/diagnostics"
	create := func(target string) string {
		t.Helper()
		body, err := json.Marshal(diagnostic.SampleRequest{URL: target, Protocol: diagnostic.ProtocolHTTPFLV, Seconds: 3})
		if err != nil {
			t.Fatal(err)
		}
		run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", api+"/runs", http.StatusAccepted, body))
		return api + "/runs/" + run.ID
	}
	capturedPath := create(source.URL + "/live.flv")
	captured := waitDiagnostic(t, server, capturedPath)
	if captured.Report == nil || !captured.FLVReplayAvailable || captured.Report.Metrics.VideoFrames != 2 {
		t.Fatalf("capture unavailable for HTTP replay: %+v", captured)
	}
	bundle := diagnosticRequest(t, server, "GET", capturedPath+"/evidence.zip", http.StatusOK, nil)
	imported := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", api+"/import", http.StatusCreated, bundle))
	importPath := api + "/runs/" + imported.ID
	if detail := waitDiagnostic(t, server, importPath); !detail.FLVReplayAvailable {
		t.Fatalf("import unavailable for HTTP replay: %+v", detail)
	}
	source.Close()
	client := &http.Client{Timeout: 4 * time.Second}
	for _, path := range []string{capturedPath, importPath} {
		target := server.URL() + path + "/replay.flv"
		results := make(chan error, 2)
		for range 2 {
			go func() { results <- measureFLVReplay(client, target, recorded, split, len(flv)) }()
		}
		for range 2 {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
	}
	replayed := waitDiagnostic(t, server, create(server.URL()+importPath+"/replay.flv"))
	if replayed.Report == nil || replayed.Report.Metrics.VideoFrames != 2 ||
		replayed.Report.Metrics.MaxVideoGapMS < 350 || replayed.Report.Manifest.Stream.SHA256 != captured.Report.Manifest.Stream.SHA256 {
		t.Fatalf("replayed sample changed the recorded timing or bytes: %+v", replayed.Report)
	}
	diagnosticRequest(t, server, "GET", api+"/runs/00000000000000000000000000000000/replay.flv", http.StatusNotFound, nil)

	response, err := client.Get(server.URL() + importPath + "/replay.flv")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	interrupted, err := io.ReadAll(response.Body)
	if err != nil || len(interrupted) >= len(recorded) {
		t.Fatalf("server shutdown did not interrupt an active replay: %d bytes, %v", len(interrupted), err)
	}
}

func measureFLVReplay(client *http.Client, url string, expected []byte, split, complete int) error {
	start := time.Now()
	response, err := client.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "video/x-flv" {
		return fmt.Errorf("unexpected replay response: %s (%s)", response.Status, response.Header.Get("Content-Type"))
	}
	data := make([]byte, len(expected))
	if _, err := io.ReadFull(response.Body, data[:split]); err != nil {
		return err
	}
	first := time.Now()
	if delay := first.Sub(start); delay < 65*time.Millisecond || delay > 650*time.Millisecond {
		return fmt.Errorf("first payload delay %s lost the recorded arrival timing", delay)
	}
	if _, err := io.ReadFull(response.Body, data[split:complete]); err != nil {
		return err
	}
	second := time.Now()
	if gap := second.Sub(first); gap < 350*time.Millisecond || gap > 850*time.Millisecond {
		return fmt.Errorf("tag arrival gap %s differs from the captured gap", gap)
	}
	if _, err := io.ReadFull(response.Body, data[complete:]); err != nil {
		return err
	}
	if wait := time.Since(second); wait < 120*time.Millisecond || wait > 600*time.Millisecond {
		return fmt.Errorf("body tail wait %s differs from the observed end delay", wait)
	}
	if _, err := response.Body.Read(make([]byte, 1)); err != io.EOF || !bytes.Equal(data, expected) {
		return fmt.Errorf("replay changed FLV bytes or EOF: %v", err)
	}
	return nil
}
