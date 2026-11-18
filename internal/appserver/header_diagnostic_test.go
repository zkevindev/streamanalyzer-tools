package appserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"streamanalyzer/internal/diagnostic"
)

func TestDiagnosticHTTPHeadersSurviveOfflineReplay(t *testing.T) {
	clearDiagnosticModelEnv(t)
	server := testDiagnosticServer(t, t.TempDir())
	packet := append([]byte{0x47, 0x1f, 0xff, 0x10}, bytes.Repeat([]byte{0xff}, 184)...)
	for _, protocol := range []string{diagnostic.ProtocolHTTPFLV, diagnostic.ProtocolHTTPTS, diagnostic.ProtocolHLS} {
		t.Run(protocol, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "public, max-age=1")
				w.Header().Set("Age", "7")
				w.Header().Set("X-Cache", "HIT")
				w.Header().Set("X-Request-Id", "fixture-request-id")
				w.Header().Add("Via", "edge-a")
				w.Header().Add("Via", "edge-b")
				w.Header().Set("Set-Cookie", "sid=response-cookie-secret; HttpOnly")
				w.Header().Set("X-Auth-Token", "response-token-secret")
				switch r.URL.Path {
				case "/entry":
					http.Redirect(w, r, "/media?token=redirect-query-secret", http.StatusFound)
				case "/media":
					if r.URL.Query().Get("token") != "redirect-query-secret" {
						t.Error("redirect authentication query lost")
					}
					switch protocol {
					case diagnostic.ProtocolHTTPFLV:
						w.Header().Set("Content-Type", "video/x-flv")
						_, _ = w.Write(testFLV())
					case diagnostic.ProtocolHTTPTS:
						w.Header().Set("Content-Type", "video/mp2t")
						_, _ = w.Write(bytes.Repeat(packet, 3))
					case diagnostic.ProtocolHLS:
						w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
						_, _ = fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\nsegment.ts?token=segment-query-secret\n#EXT-X-ENDLIST\n")
					}
				case "/segment.ts":
					_, _ = w.Write(bytes.Repeat(packet, 3))
				default:
					http.NotFound(w, r)
				}
			}))
			defer origin.Close()
			body, _ := json.Marshal(diagnostic.SampleRequest{URL: origin.URL + "/entry", Protocol: protocol, Seconds: 1})
			const api = "/api/diagnostics"
			run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/runs", http.StatusAccepted, body))
			path := api + "/runs/" + run.ID
			detail := waitDiagnostic(t, server, path)
			if detail.Report == nil {
				t.Fatalf("capture has no report: %+v", detail)
			}
			requests := detail.Report.Requests
			if len(requests) == 0 {
				requests = detail.Report.Manifest.Observation.Requests
			}
			if len(requests) < 2 || requests[0].HTTPStatus != http.StatusFound || requests[1].HTTPStatus != http.StatusOK {
				t.Fatalf("redirect headers collapsed: %+v", requests)
			}
			for _, request := range requests {
				if !request.HeadersRecorded || request.Method != "GET" || request.HTTPProtocol != "HTTP/1.1" || len(request.RequestHeaders) == 0 || len(request.ResponseHeaders) == 0 {
					t.Fatalf("HTTP exchange missing metadata: %+v", request)
				}
				var via int
				foundCache, foundID := false, false
				for _, h := range request.ResponseHeaders {
					if strings.EqualFold(h.Name, "Via") {
						via++
					}
					if strings.EqualFold(h.Name, "X-Cache") && h.Value == "HIT" {
						foundCache = true
					}
					if strings.EqualFold(h.Name, "X-Request-Id") && h.Value == "fixture-request-id" {
						foundID = true
					}
					if strings.EqualFold(h.Name, "Location") && h.Value != "/media" {
						t.Fatalf("redirect location was not sanitized: %q", h.Value)
					}
				}
				if via != 2 || !foundCache || !foundID {
					t.Fatalf("diagnostic headers missing or collapsed: %+v", request.ResponseHeaders)
				}
			}
			jsonReport := diagnosticRequest(t, server, http.MethodGet, path+"/report.json", http.StatusOK, nil)
			html := diagnosticRequest(t, server, http.MethodGet, path+"/report.html", http.StatusOK, nil)
			for _, result := range [][]byte{jsonReport, html} {
				for _, secret := range []string{"response-cookie-secret", "response-token-secret", "redirect-query-secret", "segment-query-secret"} {
					if bytes.Contains(result, []byte(secret)) {
						t.Fatalf("header report leaked %s", secret)
					}
				}
				if !bytes.Contains(result, []byte("fixture-request-id")) || !bytes.Contains(result, []byte("edge-b")) {
					t.Fatal("header evidence missing from export")
				}
			}
			bundle := diagnosticRequest(t, server, http.MethodGet, path+"/evidence.zip", http.StatusOK, nil)
			origin.Close()
			replay := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/import", http.StatusCreated, bundle))
			got := waitDiagnostic(t, server, api+"/runs/"+replay.ID)
			if !reflect.DeepEqual(detail.Report, got.Report) {
				t.Fatal("offline replay changed HTTP headers or timing")
			}
		})
	}
}

func TestDiagnosticLargeHLSRedirectHeaderLedger(t *testing.T) {
	clearDiagnosticModelEnv(t)
	packet := append([]byte{0x47, 0x1f, 0xff, 0x10}, bytes.Repeat([]byte{0xff}, 184)...)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range []string{"X-Fill-A", "X-Fill-B", "X-Fill-C", "X-Fill-D"} {
			w.Header().Set(name, strings.Repeat("<", 512))
		}
		switch r.URL.Path {
		case "/0", "/1", "/2", "/3":
			hop := int(r.URL.Path[1]-'0') + 1
			http.Redirect(w, r, fmt.Sprintf("/%d", hop), http.StatusFound)
		case "/4":
			_, _ = fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n/seg.ts\n#EXT-X-ENDLIST\n")
		case "/seg.ts":
			_, _ = w.Write(bytes.Repeat(packet, 3))
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/diagnostics"
	body, _ := json.Marshal(diagnostic.SampleRequest{URL: origin.URL + "/0", Protocol: diagnostic.ProtocolHLS, Seconds: 1})
	run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/runs", http.StatusAccepted, body))
	path := api + "/runs/" + run.ID
	detail := waitDiagnostic(t, server, path)
	if detail.Report == nil || len(detail.Report.Manifest.Observation.Requests) != 5 || detail.Report.HLS == nil || detail.Report.HLS.SegmentsCaptured != 1 {
		t.Fatalf("bounded large header ledger failed: %+v", detail)
	}
	bundle := diagnosticRequest(t, server, http.MethodGet, path+"/evidence.zip", http.StatusOK, nil)
	origin.Close()
	replay := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/import", http.StatusCreated, bundle))
	got := waitDiagnostic(t, server, api+"/runs/"+replay.ID)
	if !reflect.DeepEqual(detail.Report, got.Report) {
		t.Fatal("large header evidence could not be replayed")
	}
}
