package diagnostic

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func accessTestFLV() []byte {
	return []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0}
}

func TestCustomHeadersReachOriginButSecretsStayOutOfReport(t *testing.T) {
	const secret = "private-access-secret"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret || r.Header.Get("Cookie") != "session="+secret ||
			r.Header.Get("Referer") != "https://player.example/watch?token="+secret || r.Header.Get("User-Agent") != "CustomPlayer/1.0" {
			t.Errorf("custom headers were not preserved: %+v", r.Header)
		}
		_, _ = w.Write(accessTestFLV())
	}))
	defer origin.Close()
	target, _ := url.Parse(origin.URL + "/live.flv")
	req := SampleRequest{Headers: map[string]string{
		"Authorization": "Bearer " + secret, "Cookie": "session=" + secret,
		"Referer": "https://player.example/watch?token=" + secret, "User-Agent": "CustomPlayer/1.0",
	}}
	access, err := prepareCaptureAccess(captureClient(), req)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withCaptureAccess(context.Background(), access)
	report, err := sample(ctx, access.client, t.TempDir(), target, 2)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), secret) || !strings.Contains(string(encoded), redactedHTTPHeader) ||
		!strings.Contains(string(encoded), "https://player.example/watch") {
		t.Fatalf("request credentials were not correctly redacted: %s", encoded)
	}
}

func TestHTTPProxyAndAdditionalCAAreUsed(t *testing.T) {
	t.Run("proxy", func(t *testing.T) {
		var requests atomic.Int32
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			if r.URL.Hostname() != "stream.invalid" {
				t.Errorf("request did not use the forward proxy: %s", r.URL)
			}
			_, _ = w.Write(accessTestFLV())
		}))
		defer proxy.Close()
		access, err := prepareCaptureAccess(captureClient(), SampleRequest{ProxyURL: proxy.URL})
		if err != nil {
			t.Fatal(err)
		}
		target, _ := url.Parse("http://stream.invalid/live.flv")
		ctx := withCaptureAccess(context.Background(), access)
		if _, err := sample(ctx, access.client, t.TempDir(), target, 2); err != nil || requests.Load() != 1 {
			t.Fatalf("proxy capture failed: requests=%d err=%v", requests.Load(), err)
		}
	})

	t.Run("CA", func(t *testing.T) {
		origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(accessTestFLV())
		}))
		defer origin.Close()
		certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: origin.Certificate().Raw}))
		access, err := prepareCaptureAccess(captureClient(), SampleRequest{CACertificate: certificate})
		if err != nil {
			t.Fatal(err)
		}
		target, _ := url.Parse(origin.URL + "/live.flv")
		ctx := withCaptureAccess(context.Background(), access)
		if _, err := sample(ctx, access.client, t.TempDir(), target, 2); err != nil {
			t.Fatal(err)
		}
	})
}

func TestHLSPlayerProxyReusesCaptureHeaders(t *testing.T) {
	const secret = "hls-player-secret"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Errorf("player request lost capture authorization")
		}
		_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-ENDLIST\n")
	}))
	defer origin.Close()
	target, _ := url.Parse(origin.URL + "/live.m3u8")
	player := newPlayerSession(context.Background(), ProtocolHLS, target, captureClient())
	defer player.Close()
	player.hls.headers = http.Header{"Authorization": {"Bearer " + secret}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://local/player/index.m3u8", nil)
	player.ServeHLS(response, request, "index.m3u8")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "#EXTM3U") {
		t.Fatalf("HLS player proxy failed: %d %s", response.Code, response.Body.String())
	}
}

func TestCaptureAccessRejectsUnsafeOrUnsupportedOptions(t *testing.T) {
	for _, req := range []SampleRequest{
		{Headers: map[string]string{"Host": "other.example"}},
		{Headers: map[string]string{"X-Test": "bad\r\nvalue"}},
		{ProxyURL: "file:///tmp/proxy"},
		{CACertificate: "not a certificate"},
	} {
		if _, err := prepareCaptureAccess(captureClient(), req); err == nil {
			t.Fatalf("accepted invalid access options: %+v", req)
		}
	}
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Start(SampleRequest{Protocol: ProtocolRTMP, URL: "rtmp://example.invalid/live/test", Seconds: 1, Headers: map[string]string{"X-Test": "value"}}); err == nil {
		t.Fatal("accepted HTTP-only access options for RTMP")
	}
}
