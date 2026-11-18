package diagnostic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func headerValues(fields []HTTPHeader, name string) []string {
	var values []string
	for _, field := range fields {
		if field.Name == name {
			values = append(values, field.Value)
		}
	}
	return values
}

func TestObservedHTTPHeadersRetainRedirectsAndTransportDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Cache", "HIT")
		w.Header().Add("X-Cache", "EDGE")
		w.Header().Set("Set-Cookie", "customer-session=server-secret")
		w.Header().Set("X-Cdn-Token", "response-secret")
		if r.URL.Path == "/start" {
			w.Header().Set("Location", "/media?token=redirect-secret#private-fragment")
			w.WriteHeader(http.StatusFound)
			return
		}
		if r.URL.Query().Get("token") != "redirect-secret" {
			t.Error("capture redaction modified actual playback request")
		}
		w.Header().Set("Content-Type", "video/x-flv")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "media")
	}))
	defer server.Close()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/start?token=entry-secret", nil)
	req.Header.Set("Authorization", "Bearer client-secret")
	req.Header.Set("Cookie", "session=client-cookie")
	req.Header.Set("X-Api-Key", "client-api-key")
	req.Header.Add("X-Tag", "first")
	req.Header.Add("X-Tag", "second")
	obs, start := Observation{}, time.Now()
	resp, err := doObservedRequest(&http.Client{}, req, &obs, start)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	obs.ElapsedMS = time.Since(start).Milliseconds()
	if err := validateObservationDetails(obs); err != nil {
		t.Fatal(err)
	}
	if len(obs.Requests) != 2 {
		t.Fatalf("missing HTTP hop: %+v", obs.Requests)
	}
	for index, request := range obs.Requests {
		if !request.HeadersRecorded || request.Method != "GET" || request.HTTPProtocol != "HTTP/1.1" ||
			len(headerValues(request.RequestHeaders, "Host")) != 1 ||
			!slices.Equal(headerValues(request.RequestHeaders, "User-Agent"), []string{"Go-http-client/1.1"}) ||
			!slices.Equal(headerValues(request.RequestHeaders, "Accept-Encoding"), []string{"gzip"}) ||
			!slices.Equal(headerValues(request.RequestHeaders, "X-Tag"), []string{"first", "second"}) ||
			!slices.Equal(headerValues(request.ResponseHeaders, "X-Cache"), []string{"HIT", "EDGE"}) {
			t.Fatalf("incorrect headers for hop %d: %+v", index, request)
		}
	}
	if !slices.Equal(headerValues(obs.Requests[0].ResponseHeaders, "Location"), []string{"/media"}) {
		t.Fatal("redirect target not preserved safely")
	}
	if !slices.Equal(headerValues(obs.Requests[1].ResponseHeaders, "Transfer-Encoding"), []string{"chunked"}) {
		t.Fatal("parsed chunked transfer field omitted")
	}
	data, _ := json.Marshal(obs)
	for _, secret := range []string{"client-secret", "client-cookie", "client-api-key", "entry-secret", "redirect-secret", "private-fragment", "server-secret", "response-secret"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("persisted %s in headers", secret)
		}
	}
}

func TestHTTPHeaderRedactionAndImportedValidation(t *testing.T) {
	fields := http.Header{
		"Authorization": {"Bearer private"}, "Proxy-Authorization": {"Basic private"}, "Cookie": {"session=private"},
		"Set-Cookie": {"secret=private"}, "X-Api-Key": {"private"}, "X-Session-Id": {"private"}, "X-Auth": {"private"},
		"Location":         {"https://user:private@example.test/video?token=private#private"},
		"Content-Location": {"//user:private@example.test/path?private=yes"},
		"Referer":          {"https://example.test/page?private=yes"},
		"Link":             {`<https://user:private@example.test/part?token=private>; rel="preload"; anchor="private"`},
		"Refresh":          {"0; url=https://user:private@example.test/retry?token=private#private"},
		"X-Original-Uri":   {"/play?token=private#private"}, "X-Encoded-Url": {"https%3A%2F%2Fuser%3Aprivate%40example.test%2Fplay%3Fprivate%3Dyes"},
	}
	headers, truncated := captureHTTPHeaders(fields)
	if truncated || !validHTTPHeaderBlock(headers) {
		t.Fatalf("captured headers do not satisfy import limits: %+v", headers)
	}
	data, _ := json.Marshal(headers)
	if strings.Contains(string(data), "private") || strings.Contains(string(data), "user:") {
		t.Fatalf("header credentials survived redaction: %s", data)
	}
	if !slices.Equal(headerValues(headers, "Location"), []string{"https://example.test/video"}) ||
		!slices.Equal(headerValues(headers, "Link"), []string{"<https://example.test/part>"}) ||
		!slices.Equal(headerValues(headers, "Refresh"), []string{"0; url=https://example.test/retry"}) {
		t.Fatalf("useful URI targets lost: %+v", headers)
	}
	for _, bad := range [][]HTTPHeader{
		{{Name: "Authorization", Value: "raw-secret"}}, {{Name: "Location", Value: "/play?token=secret"}},
		{{Name: "Referer", Value: "https://user:password@example.test/page"}}, {{Name: "Link", Value: `</x?secret=yes>; rel="next"`}},
		{{Name: "Content-Type", Value: "bad\r\nHeader: injected"}}, {{Name: "lower-case", Value: "x"}},
		{{Name: "Z-First", Value: "1"}, {Name: "A-Second", Value: "2"}},
	} {
		if validHTTPHeaderBlock(bad) {
			t.Fatalf("accepted unsanitized imported headers: %+v", bad)
		}
	}
}

func TestHTTPHeaderLimitsAndUnavailableResponse(t *testing.T) {
	fields := make(http.Header)
	for i := 0; i < 100; i++ {
		fields.Set(fmt.Sprintf("X-Header-%03d", i), strings.Repeat("界", 400))
	}
	fields.Set("Location", "https://example.test/"+strings.Repeat("a", 1000)+"?secret=private")
	headers, truncated := captureHTTPHeaders(fields)
	if !truncated || len(headers) > maxHTTPHeaderCount || !validHTTPHeaderBlock(headers) {
		t.Fatal("long headers violated capture/import bounds")
	}
	if len(headerValues(headers, "Location")) != 1 || headerValues(headers, "Location")[0] != redactedHTTPHeader {
		t.Fatal("oversized URI must be marked redacted")
	}
	countFields := make(http.Header)
	for i := 0; i < maxHTTPHeaderCount+1; i++ {
		countFields.Set(fmt.Sprintf("X-%02d", i), "v")
	}
	if got, short := captureHTTPHeaders(countFields); !short || len(got) != maxHTTPHeaderCount {
		t.Fatal("header count not bounded")
	}
	obs, start := Observation{}, time.Now()
	req, _ := http.NewRequestWithContext(t.Context(), "GET", "https://example.test/private?token=private", nil)
	req.Header.Set("Authorization", "Bearer private")
	client := &http.Client{Transport: observedTestTransport(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })}
	_, _ = doObservedRequest(client, req, &obs, start)
	obs.ElapsedMS = time.Since(start).Milliseconds()
	if len(obs.Requests) != 1 || !obs.Requests[0].HeadersRecorded || obs.Requests[0].HTTPProtocol != "" || len(obs.Requests[0].ResponseHeaders) != 0 ||
		!slices.Equal(headerValues(obs.Requests[0].RequestHeaders, "Authorization"), []string{redactedHTTPHeader}) {
		t.Fatalf("failed request lost configured headers: %+v", obs.Requests)
	}
	if err := validateObservationDetails(obs); err != nil {
		t.Fatal(err)
	}
	legacy := HTTPRequestTiming{Number: 1}
	if !validHTTPRequestHeaders(legacy) {
		t.Fatal("legacy timing no longer valid")
	}
	legacy.RequestHeaders = []HTTPHeader{{Name: "Authorization", Value: redactedHTTPHeader}}
	if validHTTPRequestHeaders(legacy) {
		t.Fatal("undeclared header capture accepted")
	}
	if got, _ := captureHTTPHeaders(http.Header{"X-Tag": {"first", "second"}}); !reflect.DeepEqual(got, []HTTPHeader{{Name: "X-Tag", Value: "first"}, {Name: "X-Tag", Value: "second"}}) {
		t.Fatalf("duplicate field changed: %+v", got)
	}
}
