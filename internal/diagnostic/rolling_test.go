package diagnostic

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRollingSegmentsRetainRecentIndependentEvidence(t *testing.T) {
	var connections atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connections.Add(1)
		_, _ = w.Write(accessTestFLV())
	}))
	defer origin.Close()
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	first, err := service.Start(SampleRequest{URL: origin.URL + "/live.flv", SegmentSeconds: 1, RollingSegments: 2, ReconnectDelayMS: 10})
	if err != nil {
		t.Fatal(err)
	}
	var group SessionGroup
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		group, err = service.SessionGroup(first.SessionGroupID)
		if err != nil {
			t.Fatal(err)
		}
		if len(group.Runs) >= 2 && connections.Load() >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !group.Rolling || group.RetainSegments != 2 || len(group.Runs) != 2 {
		t.Fatalf("rolling session did not retain two segments: %+v connections=%d", group, connections.Load())
	}
	group, err = service.StopSessionGroup(group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if group.Status != "done" {
		t.Fatalf("rolling session did not stop: %+v", group)
	}
	if _, err := service.getRun(first.ID); err != ErrNotFound {
		t.Fatal("oldest rolling segment was not deleted")
	}
	for _, run := range group.Runs {
		var evidence bytes.Buffer
		if err := service.WriteBundle(run.ID, &evidence); err != nil {
			t.Fatalf("retained segment is not exportable: %v", err)
		}
		if _, err := readBundle(t.TempDir(), &evidence); err != nil {
			t.Fatalf("retained segment is not replayable: %v", err)
		}
	}
}

func TestRollingValidation(t *testing.T) {
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for _, req := range []SampleRequest{
		{Protocol: ProtocolUDPTS, URL: "udp://127.0.0.1:5000", SegmentSeconds: 1},
		{Protocol: ProtocolHTTPFLV, URL: "http://example.test/live.flv", SegmentSeconds: 1, RollingSegments: 13},
		{Protocol: ProtocolHTTPFLV, URL: "http://example.test/live.flv", SegmentSeconds: 1, Playback: true},
	} {
		if _, err := service.Start(req); err == nil {
			t.Fatalf("accepted invalid rolling request: %+v", req)
		}
	}
}

func TestRollingValidationAllowsWHEPAndHTTPTS(t *testing.T) {
	for _, proto := range []string{ProtocolHTTPFLV, ProtocolHTTPTS, ProtocolRTMP, ProtocolHLS, ProtocolRTSP, ProtocolRTSPS, ProtocolWHEP} {
		req := SampleRequest{Protocol: proto, SegmentSeconds: 5}
		if err := validateRolling(&req); err != nil {
			t.Fatalf("validateRolling rejected supported protocol %s: %v", proto, err)
		}
		if req.RollingSegments != 3 || req.Seconds != 5 {
			t.Fatalf("validateRolling did not normalize defaults for %s: %+v", proto, req)
		}
	}
	for _, proto := range []string{ProtocolUDPTS, ProtocolRTPTS} {
		req := SampleRequest{Protocol: proto, SegmentSeconds: 5}
		if err := validateRolling(&req); err == nil {
			t.Fatalf("validateRolling unexpectedly accepted %s", proto)
		}
	}
}
