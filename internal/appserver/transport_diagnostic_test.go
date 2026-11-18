package appserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
)

func TestTransportStreamWorkflowExportsAndReplays(t *testing.T) {
	clearDiagnosticModelEnv(t)
	packet := append([]byte{0x47, 0x1f, 0xff, 0x10}, bytes.Repeat([]byte{0xff}, 184)...)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte("mpegts")) || bytes.Contains(body, []byte("127.0.0.1")) || bytes.Contains(body, []byte("private-token")) {
			t.Error("model request omitted TS measurements or exposed connection details")
		}
		const answer = `{"summary":"已检查传输流证据","hypotheses":[{"claim":"检查节目表","evidence_ids":["F001"],"next_check":"核对复用器配置"}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": answer}}},
		})
	}))
	defer model.Close()
	t.Setenv("STREAMANALYZER_MODEL_PROVIDER", diagnostic.ProviderOpenAICompatible)
	t.Setenv("STREAMANALYZER_MODEL_BASE_URL", model.URL+"/v1")
	t.Setenv("STREAMANALYZER_MODEL_NAME", "transport-fixture")
	server := testDiagnosticServer(t, t.TempDir())
	for _, protocol := range []string{diagnostic.ProtocolHTTPTS, diagnostic.ProtocolUDPTS, diagnostic.ProtocolRTPTS} {
		t.Run(protocol, func(t *testing.T) {
			var target string
			closeSource := func() {}
			if protocol == diagnostic.ProtocolHTTPTS {
				source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "video/mp2t")
					_, _ = w.Write(bytes.Repeat(packet, 8))
				}))
				closeSource = source.Close
				target = source.URL + "/live.ts?token=private-token"
			} else {
				reservation, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
				if err != nil {
					t.Fatal(err)
				}
				address := reservation.LocalAddr().(*net.UDPAddr)
				_ = reservation.Close()
				sender, err := net.DialUDP("udp4", nil, address)
				if err != nil {
					t.Fatal(err)
				}
				scheme := "udp"
				if protocol == diagnostic.ProtocolRTPTS {
					scheme = "rtp"
				}
				target = scheme + "://" + address.String()
				stop := make(chan struct{})
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					ticker := time.NewTicker(30 * time.Millisecond)
					defer ticker.Stop()
					sequence := uint16(0)
					for {
						select {
						case <-stop:
							return
						case <-ticker.C:
							data := packet
							if protocol == diagnostic.ProtocolRTPTS {
								data = append([]byte{0x80, 33, byte(sequence >> 8), byte(sequence), 0, 0, 0, 0, 1, 2, 3, 4}, packet...)
								sequence++
							}
							_, _ = sender.Write(data)
						}
					}
				}()
				closeSource = func() { close(stop); <-finished; _ = sender.Close() }
			}
			defer closeSource()
			const api = "/api/diagnostics"
			request, _ := json.Marshal(diagnostic.SampleRequest{Protocol: protocol, URL: target, Seconds: 1})
			run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/runs", http.StatusAccepted, request))
			path := api + "/runs/" + run.ID
			captured := waitDiagnostic(t, server, path)
			if captured.Status != "done" || captured.Report == nil || captured.Report.MPEGTS == nil || captured.Report.MPEGTS.Packets == 0 {
				t.Fatalf("unexpected capture result for %s: %+v", protocol, captured)
			}
			raw := diagnosticRequest(t, server, http.MethodGet, path+"/dump", http.StatusOK, nil)
			if len(raw) == 0 {
				t.Fatal("missing raw transport artifact")
			}
			html := diagnosticRequest(t, server, http.MethodGet, path+"/report.html", http.StatusOK, nil)
			if !bytes.Contains(html, []byte(captured.DumpName)) || bytes.Contains(html, []byte("private-token")) {
				t.Fatal("exported HTML omitted artifact details or exposed the signed query")
			}
			bundle := diagnosticRequest(t, server, http.MethodGet, path+"/evidence.zip", http.StatusOK, nil)
			replay := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/import", http.StatusCreated, bundle))
			replayed := waitDiagnostic(t, server, api+"/runs/"+replay.ID)
			if !reflect.DeepEqual(captured.Report, replayed.Report) {
				t.Fatalf("offline replay differs from original %s report", protocol)
			}
			explanation := decodeDiagnostic[diagnostic.Explanation](t, diagnosticRequest(t, server, http.MethodPost, fmt.Sprintf("%s/runs/%s/explain", api, replay.ID), http.StatusOK, nil))
			if explanation.Model != "transport-fixture" {
				t.Fatal("imported transport evidence could not be explained")
			}
		})
	}
}
