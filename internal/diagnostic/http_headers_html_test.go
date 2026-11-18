package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func TestHTTPHeaderHTMLIncludesPerHopDetailsAndEscapesValues(t *testing.T) {
	requests := []HTTPRequestTiming{
		{Number: 1, HTTPStatus: 302, Method: "GET", HTTPProtocol: "HTTP/1.1", HeadersRecorded: true,
			RequestHeaders:  []HTTPHeader{{Name: "Authorization", Value: "[REDACTED]"}},
			ResponseHeaders: []HTTPHeader{{Name: "Location", Value: "https://edge.example/live.flv"}, {Name: "Via", Value: "edge-a"}, {Name: "Via", Value: "edge-b"}}},
		{Number: 2, HTTPStatus: 200, Method: "GET", HTTPProtocol: "HTTP/2.0", HeadersRecorded: true, ResponseHeadersTruncated: true,
			ResponseHeaders: []HTTPHeader{{Name: "Content-Type", Value: "video/x-flv"}, {Name: "X-Debug", Value: `<img src=x onerror="alert(1)">`}}},
		{Number: 3, Method: "GET", HeadersRecorded: true},
		{Number: 4, HTTPStatus: 200},
	}
	for _, topLevel := range []bool{false, true} {
		report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV, Observation: Observation{Requests: requests}}}
		if topLevel {
			report.Manifest.Protocol = ProtocolHLS
			report.Requests = requests
			report.Manifest.Observation.Requests = nil
		}
		var html bytes.Buffer
		if err := RenderHTML(&html, report, nil, ""); err != nil {
			t.Fatal(err)
		}
		body := html.String()
		for _, value := range []string{"HTTP 请求与响应头", "#1 · HTTP 302 · GET · HTTP/1.1", "#2 · HTTP 200 · GET · HTTP/2.0", "edge-a", "edge-b", "[REDACTED]", "部分字段或值已截断", "未收到 HTTP 响应头", "该记录未采集 HTTP Header", "&lt;img"} {
			if !strings.Contains(body, value) {
				t.Errorf("report omitted %q", value)
			}
		}
		if strings.Contains(body, `<img src=x`) || strings.Contains(body, "<script") {
			t.Fatal("header values became active HTML")
		}
		if got := strings.Count(body, `class="http-header-block"`); got != 4 {
			t.Fatalf("expected four independently inspectable HTTP hops, got %d", got)
		}
	}
}
