package onvifdiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/onvif"
)

func awaitScan(t *testing.T, m *Manager, id string) Discovery {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		value, err := m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if value.State != "running" {
			return value
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("discovery did not stop")
	return Discovery{}
}

func TestRealSimulatorDiscoveryDigestClockAndSelection(t *testing.T) {
	camera := &onvif.Manager{}
	defer camera.Close()
	started, err := camera.Start(onvif.Config{BindIP: "127.0.0.1", Name: "Office & <NVR>", SOAPUsername: "camera<&", SOAPPassword: "soap-private-password", RTSPUsername: "separate-rtsp", RTSPPassword: "rtsp-private-password", ClockOffset: 600})
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{}
	defer m.Close()
	scan, err := m.Start(DiscoverRequest{BindIP: "127.0.0.1", Seconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	result := awaitScan(t, m, scan.ID)
	if result.State != "complete" {
		t.Fatalf("search failed: %+v", result)
	}
	var device Device
	for _, value := range result.Devices {
		if value.EndpointID == started.EndpointID {
			device = value
			break
		}
	}
	if device.ID == "" || device.PeerIP != "127.0.0.1" || device.Name != "Office & <NVR>" {
		t.Fatalf("real multicast simulator not discovered: %+v", result)
	}
	credentials := Credentials{Username: "camera<&", Password: "soap-private-password"}
	profiles, err := m.Profiles(t.Context(), ProfilesRequest{DiscoveryID: scan.ID, DeviceID: device.ID, BindIP: "127.0.0.1", Credentials: credentials})
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles.Profiles) != 2 || profiles.Profiles[0].VideoCodec != "H264" || profiles.Profiles[0].Width != 320 || profiles.Profiles[0].Height != 180 || profiles.ClockStatus != "device_utc" || profiles.ClockOffsetSeconds == nil || *profiles.ClockOffsetSeconds < 590 || *profiles.ClockOffsetSeconds > 610 {
		t.Fatalf("incorrect authenticated profiles/clock: %+v", profiles)
	}
	for _, transport := range []string{"tcp", "udp"} {
		stream, err := m.Stream(t.Context(), StreamRequest{SelectionID: profiles.SelectionID, ProfileToken: profiles.Profiles[1].Token, Transport: transport, Credentials: credentials})
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(stream.URL)
		if err != nil || u.User != nil || u.Path != "/sub" || stream.Transport != transport || stream.Protocol != "rtsp" {
			t.Fatalf("incorrect review-only stream result: %+v", stream)
		}
	}
	encoded, _ := json.Marshal([]any{result, profiles})
	for _, private := range []string{"camera<&", "soap-private-password", "separate-rtsp", "rtsp-private-password"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("credentials exposed in discovery/profile JSON: %s", private)
		}
	}
	// Snapshots cannot mutate the manager's published result.
	result.Devices[0].ServiceURL = "http://invalid/"
	again, _ := m.Get(scan.ID)
	if again.Devices[0].ServiceURL == "http://invalid/" {
		t.Fatal("discovery result aliases manager state")
	}
	if _, err := m.Profiles(t.Context(), ProfilesRequest{ServiceURL: started.DeviceURL, BindIP: "127.0.0.1", Credentials: Credentials{Username: credentials.Username, Password: "wrong-secret"}}); errorCode(err) != "auth_failed" {
		t.Fatalf("incorrect rejected-auth result: %v", err)
	}
	if camera.Status().RTSPPlays != 0 {
		t.Fatal("ONVIF selection started RTSP playback without user capture")
	}
}

func TestDiscoveryCancelAndShutdown(t *testing.T) {
	m := &Manager{}
	job, err := m.Start(DiscoverRequest{BindIP: "127.0.0.1", Seconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(DiscoverRequest{BindIP: "127.0.0.1", Seconds: 1}); errorCode(err) != "busy" {
		t.Fatal("unbounded concurrent scans accepted")
	}
	start := time.Now()
	if _, err := m.Stop(job.ID); err != nil {
		t.Fatal(err)
	}
	value := awaitScan(t, m, job.ID)
	if value.State != "canceled" || time.Since(start) > time.Second {
		t.Fatalf("cancel did not interrupt UDP read: %+v", value)
	}
	m.Close()
	if _, err := m.Start(DiscoverRequest{BindIP: "127.0.0.1"}); errorCode(err) != "closed" {
		t.Fatal("closed discovery manager restarted")
	}
}

func errorCode(err error) string {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}
func reply(operation, body string) string {
	return fmt.Sprintf(`<s:Envelope xmlns:s="%s" xmlns:tds="%s" xmlns:trt="%s" xmlns:tt="%s"><s:Body><tds:%sResponse>%s</tds:%sResponse></s:Body></s:Envelope>`, soapNS, deviceNS, mediaNS, schemaNS, operation, body, operation)
}

func TestSOAPScopeRedirectBodyLimitsAndTLS(t *testing.T) {
	for _, kind := range []string{"foreign_media", "redirect", "auth_html", "oversize", "doctype", "tls"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int64
			trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
			defer trap.Close()
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				if strings.Contains(string(data), "never-plain-secret") {
					t.Error("plaintext password sent in SOAP")
				}
				if strings.Contains(string(data), "GetSystemDateAndTime") {
					w.Header().Set("Content-Type", "application/soap+xml")
					io.WriteString(w, reply("GetSystemDateAndTime", ""))
					return
				}
				switch kind {
				case "foreign_media":
					io.WriteString(w, reply("GetCapabilities", `<tds:Capabilities><tt:Media><tt:XAddr>`+strings.Replace(trap.URL, "127.0.0.1", "127.0.0.2", 1)+`</tt:XAddr></tt:Media></tds:Capabilities>`))
				case "redirect":
					w.Header().Set("Location", trap.URL)
					w.WriteHeader(302)
				case "auth_html":
					w.WriteHeader(401)
					io.WriteString(w, "denied never-plain-secret")
				case "oversize":
					io.WriteString(w, strings.Repeat("x", (1<<20)+1))
				case "doctype":
					io.WriteString(w, `<!DOCTYPE x [<!ENTITY secret SYSTEM "file:///etc/passwd">]>`+reply("GetCapabilities", ""))
				}
			}))
			if kind == "tls" {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			m := &Manager{}
			defer m.Close()
			_, err := m.Profiles(t.Context(), ProfilesRequest{ServiceURL: server.URL, BindIP: "127.0.0.1", Credentials: Credentials{Username: "test", Password: "never-plain-secret"}})
			want := map[string]string{"foreign_media": "endpoint_scope", "redirect": "redirect_stopped", "auth_html": "auth_failed", "oversize": "response_limit", "doctype": "soap_parse_error", "tls": "tls_error"}[kind]
			if errorCode(err) != want {
				t.Fatalf("expected %s, got %v", want, err)
			}
			if calls.Load() != 0 {
				t.Fatal("credentials followed an advertised foreign endpoint or redirect")
			}
			if err != nil && strings.Contains(err.Error(), "never-plain-secret") {
				t.Fatal("remote error reflected sensitive body")
			}
		})
	}
}

func TestProbeCorrelationScopeAndInputBounds(t *testing.T) {
	nic, err := bindInterface("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	version := dialects[0]
	issued := map[string]dialect{"urn:test-message": version}
	message := fmt.Sprintf(`<s:Envelope xmlns:s="%s" xmlns:a="%s" xmlns:d="%s"><s:Header><a:Action>%s/ProbeMatches</a:Action><a:RelatesTo>urn:test-message</a:RelatesTo></s:Header><s:Body><d:ProbeMatches><d:ProbeMatch><a:EndpointReference><a:Address>urn:uuid:device</a:Address></a:EndpointReference><d:Types>dn:NetworkVideoStorage</d:Types><d:Scopes>onvif://www.onvif.org/name/NVR</d:Scopes><d:XAddrs>http://127.0.0.1/onvif/device_service</d:XAddrs></d:ProbeMatch></d:ProbeMatches></s:Body></s:Envelope>`, soapNS, version.addressing, version.discovery, version.discovery)
	good, _ := parseProbe([]byte(message), issued, net.ParseIP("127.0.0.1"), nic)
	if len(good) != 1 || good[0].Name != "NVR" {
		t.Fatalf("valid NVR ProbeMatches rejected: %+v", good)
	}
	for _, bad := range []string{strings.Replace(message, "urn:test-message", "urn:other", 1), strings.ReplaceAll(message, version.addressing, "urn:wrong-addressing"), strings.Replace(message, "http://127.0.0.1/", "http://192.0.2.1/", 1), strings.Repeat("x", 33<<10)} {
		if value, _ := parseProbe([]byte(bad), issued, net.ParseIP("127.0.0.1"), nic); len(value) != 0 {
			t.Fatal("uncorrelated or foreign discovery response accepted")
		}
	}
	if value, _ := parseProbe([]byte(message), issued, net.ParseIP("192.0.2.1"), nic); len(value) != 0 {
		t.Fatal("discovery accepted a response beyond selected interface subnet")
	}
	if _, err := resolveTarget(context.Background(), "file:///etc/passwd", nic, nil); errorCode(err) != "invalid_input" {
		t.Fatal("non HTTP service target accepted")
	}
}
