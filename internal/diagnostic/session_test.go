package diagnostic

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPFLVReconnectCreatesIndependentReplayableSessions(t *testing.T) {
	var connections atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "session-secret" {
			t.Error("reconnect lost access options")
		}
		connections.Add(1)
		_, _ = w.Write(accessTestFLV())
	}))
	defer origin.Close()
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	run, err := service.Start(SampleRequest{URL: origin.URL + "/live.flv", Seconds: 1, ReconnectAttempts: 1, ReconnectDelayMS: 1, Headers: map[string]string{"X-Test": "session-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	if run.SessionGroupID == "" {
		t.Fatal("missing reconnect session group")
	}
	var group SessionGroup
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		group, err = service.SessionGroup(run.SessionGroupID)
		if err != nil {
			t.Fatal(err)
		}
		if group.Status == "done" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if group.Status != "done" || len(group.Runs) != 2 || connections.Load() != 2 {
		t.Fatalf("reconnect did not create two sessions: %+v connections=%d", group, connections.Load())
	}
	for index, session := range group.Runs {
		detail, err := service.Detail(session.ID)
		if err != nil || detail.Report == nil {
			t.Fatalf("session %d has no report: %v", index, err)
		}
		var bundle bytes.Buffer
		if err := service.WriteBundle(session.ID, &bundle); err != nil {
			t.Fatalf("session %d evidence is not exportable: %v", index, err)
		}
		if _, err := readBundle(t.TempDir(), &bundle); err != nil {
			t.Fatalf("session %d evidence is not replayable: %v", index, err)
		}
	}
}

func TestReconnectValidation(t *testing.T) {
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for _, req := range []SampleRequest{
		{Protocol: ProtocolHLS, URL: "http://example.test/live.m3u8", ReconnectAttempts: 1},
		{Protocol: ProtocolHTTPFLV, URL: "http://example.test/live.flv", ReconnectAttempts: 4},
		{Protocol: ProtocolHTTPFLV, URL: "http://example.test/live.flv", ReconnectAttempts: 1, Playback: true},
	} {
		if _, err := service.Start(req); err == nil {
			t.Fatalf("accepted invalid reconnect request: %+v", req)
		}
	}
}

func TestReconnectValidationAllowsWHEPAndHTTPTS(t *testing.T) {
	for _, proto := range []string{ProtocolHTTPFLV, ProtocolHTTPTS, ProtocolRTMP, ProtocolRTSP, ProtocolRTSPS, ProtocolWHEP} {
		if err := validateReconnect(SampleRequest{Protocol: proto, ReconnectAttempts: 2, ReconnectDelayMS: 200}); err != nil {
			t.Fatalf("validateReconnect rejected supported protocol %s: %v", proto, err)
		}
	}
	for _, proto := range []string{ProtocolHLS, ProtocolUDPTS, ProtocolRTPTS} {
		if err := validateReconnect(SampleRequest{Protocol: proto, ReconnectAttempts: 1}); err == nil {
			t.Fatalf("validateReconnect unexpectedly accepted %s", proto)
		}
	}
}
