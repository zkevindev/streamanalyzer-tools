package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"

	"github.com/gorilla/websocket"
)

func TestDiagnosticPlayerWebSocketRelaysSameCapture(t *testing.T) {
	clearDiagnosticModelEnv(t)
	server := testDiagnosticServer(t, t.TempDir())
	packet := append([]byte{0x47, 0x1f, 0xff, 0x10}, bytes.Repeat([]byte{0xff}, 184)...)
	for _, item := range []struct {
		protocol, transport string
		payload             []byte
	}{
		{diagnostic.ProtocolHTTPFLV, "flv", testFLV()},
		{diagnostic.ProtocolHTTPTS, "mpegts", bytes.Repeat(packet, 20)},
	} {
		t.Run(item.protocol, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls.Add(1)
				if r.URL.Query().Get("token") != "private-source-secret" {
					t.Error("media source did not receive its own grant")
				}
				_, _ = w.Write(item.payload)
			}))
			defer upstream.Close()
			request, _ := json.Marshal(diagnostic.SampleRequest{
				URL: upstream.URL + "/media?token=private-source-secret", Protocol: item.protocol, Seconds: 1, Playback: true,
			})
			const api = "/api/diagnostics"
			created := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/runs", http.StatusAccepted, request))
			if created.Player == nil || created.Player.Source != "same_capture" || created.Player.Transport != item.transport ||
				created.Player.Token == "" || strings.Contains(created.Player.URL, "private-source-secret") {
				t.Fatal("no local grant for the sampled transport")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			wsURL := "ws" + strings.TrimPrefix(server.URL(), "http") + created.Player.URL
			ws, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, http.Header{"Origin": []string{server.URL()}})
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
			var relayed bytes.Buffer
			for {
				kind, data, err := ws.ReadMessage()
				if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					break
				}
				if err != nil || kind != websocket.BinaryMessage {
					t.Fatalf("incorrect media frame: type=%d err=%v", kind, err)
				}
				relayed.Write(data)
			}
			runPath := api + "/runs/" + created.ID
			got := waitDiagnostic(t, server, runPath)
			if got.Status != "done" || !bytes.Equal(relayed.Bytes(), item.payload) || upstreamCalls.Load() != 1 {
				t.Fatalf("preview does not match sampled bytes: status=%s bytes=%d origin_requests=%d", got.Status, relayed.Len(), upstreamCalls.Load())
			}
			dump := diagnosticRequest(t, server, http.MethodGet, runPath+"/dump", http.StatusOK, nil)
			if !bytes.Equal(dump, relayed.Bytes()) {
				t.Fatal("websocket and downloadable dump differ")
			}
			diagnosticRequest(t, server, http.MethodGet, runPath+"/player/stream?token=invalid", http.StatusNotFound, nil)
		})
	}
}
