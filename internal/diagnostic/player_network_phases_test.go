package diagnostic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type phaseRoundTripper func(*http.Request) (*http.Response, error)

func (f phaseRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func phaseResignNetwork(files map[string][]byte) {
	var manifest Manifest
	json.Unmarshal(files[manifestName], &manifest)
	hash := sha256.Sum256(files[playerNetworkName])
	manifest.PlayerNetwork = &Artifact{Bytes: int64(len(files[playerNetworkName])), SHA256: hex.EncodeToString(hash[:])}
	files[manifestName], _ = json.Marshal(manifest)
}

func TestPlayerNetworkPhasesRedirectRangeReuseAndPrivacy(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Private-Header", "response-secret")
		if r.URL.Path == "/entry" {
			http.Redirect(w, r, "/body.ts?secret=redirect", 302)
			return
		}
		w.Header().Set("Content-Range", "bytes 0-4/20")
		w.WriteHeader(206)
		io.WriteString(w, "12345")
	}))
	defer origin.Close()
	target, _ := url.Parse(origin.URL + "/entry?secret=source")
	client := captureClient()
	transport := client.Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = false
	client.Transport = transport
	defer client.CloseIdleConnections()
	player := newPlayerSession(t.Context(), ProtocolHLS, target, client)
	defer player.Close()
	resource := player.hls.remember(target, false)
	networkProxyRequest(t, player, "GET", resource)
	networkProxyRequest(t, player, "GET", resource)
	value := player.hls.network.snapshot(1, false)
	request := value.Requests[0]
	if value.Version != 2 || len(request.Hops) != 2 || request.Hops[0].HTTPStatus != 302 || request.Hops[1].HTTPStatus != 206 || request.Outcome != "complete" {
		t.Fatalf("redirect chain lost: %+v", value)
	}
	last := request.Hops[1]
	if last.ConnectionReused == nil || !*last.ConnectionReused || last.TTFBMS == nil || !slices.ContainsFunc(last.Stages, func(stage CaptureStage) bool { return stage.Name == "receive" }) || slices.ContainsFunc(last.Stages, func(stage CaptureStage) bool {
		return stage.Name == "dns" || stage.Name == "connect" || stage.Name == "tls"
	}) {
		t.Fatalf("reused connection invented setup phases or lost body/TTFB observation: %+v", last)
	}
	if err := validatePlayerNetwork(value, &PlayerReport{Revision: 1}, ProtocolHLS, false); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(value)
	for _, private := range []string{"secret", origin.URL, `"request_headers":`, `"response_headers":`, "Location", "127.0.0.1", "X-Private-Header"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatalf("raw HTTP metadata leaked into playback ledger: %s", data)
		}
	}
}

func TestPlayerNetworkPhasesAllowlistedDNSConnectTLSTTFBAndHeaderFailures(t *testing.T) {
	for _, test := range []struct {
		name, stage, code string
		trace             func(*httptrace.ClientTrace)
		failure           error
	}{
		{"dns", "dns", "dns_error", func(trace *httptrace.ClientTrace) {
			trace.DNSStart(httptrace.DNSStartInfo{Host: "private.invalid"})
			trace.DNSDone(httptrace.DNSDoneInfo{})
		}, &net.DNSError{Err: "private-resolver-detail", Name: "private.invalid", IsNotFound: true}},
		{"connect", "connect", "transport_error", func(trace *httptrace.ClientTrace) {
			trace.ConnectStart("tcp", "private-address")
			trace.ConnectDone("tcp", "private-address", errors.New("private socket error"))
		}, errors.New("private socket error")},
		{"tls", "tls", "tls_error", func(trace *httptrace.ClientTrace) {
			trace.TLSHandshakeStart()
			trace.TLSHandshakeDone(tls.ConnectionState{}, errors.New("private TLS alert"))
		}, tls.RecordHeaderError{}},
		{"first byte", "response_first_byte", "timeout", func(trace *httptrace.ClientTrace) {
			trace.GotConn(httptrace.GotConnInfo{})
			trace.WroteRequest(httptrace.WroteRequestInfo{})
		}, context.DeadlineExceeded},
		{"headers", "response_headers", "transport_error", func(trace *httptrace.ClientTrace) {
			trace.GotConn(httptrace.GotConnInfo{})
			trace.WroteRequest(httptrace.WroteRequestInfo{})
			trace.GotFirstResponseByte()
		}, io.ErrUnexpectedEOF},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: phaseRoundTripper(func(request *http.Request) (*http.Response, error) {
				test.trace(httptrace.ContextClientTrace(request.Context()))
				return nil, test.failure
			})}
			target, _ := url.Parse("http://private.invalid/body.ts?secret=source")
			player := newPlayerSession(t.Context(), ProtocolHLS, target, client)
			defer player.Close()
			networkProxyRequest(t, player, "GET", player.hls.remember(target, false))
			value := player.hls.network.snapshot(1, false)
			request := value.Requests[0]
			if request.FailureStage != test.stage || request.FailureCode != test.code || len(request.Hops) != 1 || request.Hops[0].FailureStage != test.stage || request.Hops[0].FailureCode != test.code {
				t.Fatalf("failure was not safely localized to observed phase: %+v", request)
			}
			if err := validatePlayerNetwork(value, &PlayerReport{Revision: 1}, ProtocolHLS, false); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(value)
			if bytes.Contains(data, []byte("private")) {
				t.Fatalf("transport error text or endpoint escaped projection: %s", data)
			}
		})
	}
}

type phaseFailWriter struct{ header http.Header }

func (w *phaseFailWriter) Header() http.Header { return w.header }
func (*phaseFailWriter) WriteHeader(int)       {}
func (*phaseFailWriter) Write([]byte) (int, error) {
	return 0, errors.New("private downstream failure")
}

func TestPlayerNetworkPhasesUnknownZeroAndProxyWriteAreDistinct(t *testing.T) {
	target, _ := url.Parse("http://private.invalid/a.ts")
	client := &http.Client{Transport: phaseRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"video/mp2t"}}, Body: io.NopCloser(strings.NewReader("body")), ContentLength: 4, Request: request}, nil
	})}
	player := newPlayerSession(t.Context(), ProtocolHLS, target, client)
	defer player.Close()
	resource := player.hls.remember(target, false)
	player.ServeHLS(&phaseFailWriter{header: make(http.Header)}, httptest.NewRequestWithContext(t.Context(), "GET", "http://local/"+resource, nil), resource)
	value := player.hls.network.snapshot(1, false)
	request := value.Requests[0]
	if request.FailureStage != "proxy_write" || request.FailureCode != "proxy_write_error" || request.Hops[0].TTFBMS != nil || request.Hops[0].ConnectionReused != nil {
		t.Fatalf("downstream write or unobserved native trace was misrepresented: %+v", request)
	}
	if len(request.Hops[0].Stages) != 1 || request.Hops[0].Stages[0].Name != "receive" {
		t.Fatalf("custom transport invented DNS/TLS/send/header durations: %+v", request.Hops[0])
	}
	n := newPlayerNetwork()
	e := n.begin("r1.ts", "media", "GET")
	zero := int64(0)
	n.recordHop(e, HTTPRequestTiming{StartMS: e.request.StartMS, EndMS: e.request.StartMS, TTFBMS: &zero, HTTPStatus: 200}, nil)
	n.header(e, 200)
	n.finish(e, "complete")
	measured := n.snapshot(1, false)
	if measured.Requests[0].Hops[0].TTFBMS == nil || *measured.Requests[0].Hops[0].TTFBMS != 0 {
		t.Fatal("measured zero TTFB became unknown")
	}
}

func TestPlayerNetworkPhasesInflightReceiveCutoffAndFrozenDeepClone(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer origin.Close()
	target, _ := url.Parse(origin.URL + "/body.ts")
	player := newPlayerSession(t.Context(), ProtocolHLS, target, captureClient())
	defer player.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	resource := player.hls.remember(target, false)
	done := make(chan struct{})
	go func() {
		defer close(done)
		player.ServeHLS(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, "GET", "http://local/"+resource, nil), resource)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		player.hls.network.mu.Lock()
		ready := len(player.hls.network.entries) > 0 && player.hls.network.entries[0].receiveActive
		player.hls.network.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("body observation did not start")
		}
		time.Sleep(time.Millisecond)
	}
	partial := player.hls.network.snapshot(1, false)
	hop := partial.Requests[0].Hops[0]
	if partial.Requests[0].Outcome != "incomplete" || hop.EndMS != partial.Requests[0].EndMS || hop.Stages[len(hop.Stages)-1].EndMS != hop.EndMS {
		t.Fatalf("inflight body stage did not stop at snapshot cutoff: %+v", partial)
	}
	final := player.hls.network.snapshot(2, true)
	partial.Requests[0].Hops[0].Stages[0].StartMS = -50000
	if final.Requests[0].Hops[0].Stages[0].StartMS == -50000 {
		t.Fatal("nested stages alias another snapshot")
	}
	cancel()
	<-done
	if !reflect.DeepEqual(final, player.hls.network.snapshot(2, true)) {
		t.Fatal("cancellation completion changed a frozen snapshot")
	}
	if err := validatePlayerNetwork(final, &PlayerReport{Revision: 2}, ProtocolHLS, true); err != nil {
		t.Fatal(err)
	}
}

func TestPlayerNetworkPhasesBudgetsBoundWorstCaseConcurrentSnapshots(t *testing.T) {
	n := newPlayerNetwork()
	var wait sync.WaitGroup
	for index := 0; index < maxPlayerNetworkRequests; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			entry := n.begin("r2047.m3u8", "auxiliary", "GET")
			for hop := 0; hop < 10; hop++ {
				start := entry.request.StartMS + int64(hop*2)
				timing := HTTPRequestTiming{StartMS: start, EndMS: start + 1, HTTPStatus: 302}
				for stage := 0; stage < 15; stage++ {
					timing.Stages = append(timing.Stages, CaptureStage{Name: "response_first_byte", StartMS: start, EndMS: start + 1})
				}
				n.recordHop(entry, timing, nil)
			}
			n.mu.Lock()
			entry.request.EndMS = entry.request.StartMS + 30
			entry.done = true
			n.mu.Unlock()
		}()
	}
	for revision := 1; revision <= 10; revision++ {
		n.snapshot(revision, false)
	}
	wait.Wait()
	value := n.snapshot(11, true)
	data, err := json.Marshal(value)
	if err != nil || len(data) > maxPlayerNetworkBytes || value.DroppedHops == 0 || len(value.Requests) != maxPlayerNetworkRequests {
		t.Fatalf("bounded metadata ledger overflowed: bytes=%d dropped=%d %v", len(data), value.DroppedHops, err)
	}
	if err := validatePlayerNetwork(value, &PlayerReport{Revision: 11}, ProtocolHLS, true); err != nil {
		t.Fatal(err)
	}
	for _, request := range value.Requests {
		if len(request.Hops)+request.DroppedHops != 10 {
			t.Fatal("known omitted hops were not explicitly counted")
		}
	}
	limited := newPlayerNetwork()
	entry := limited.begin("r1.ts", "media", "GET")
	limited.metadataBytes = maxPlayerNetworkHopBytes
	limited.recordHop(entry, HTTPRequestTiming{StartMS: entry.request.StartMS, EndMS: entry.request.StartMS, FailureStage: "tls", FailureCode: "timeout"}, nil)
	limited.requestError(entry, context.DeadlineExceeded, nil, nil)
	if entry.request.DroppedHops != 1 || entry.request.FailureStage != "tls" || entry.request.FailureCode != "timeout" {
		t.Fatal("metadata truncation discarded a known terminal phase or substituted a guessed phase")
	}
}

func TestPlayerNetworkPhasesNestedClockShiftDoesNotShiftTTFBDuration(t *testing.T) {
	service, id, _, _, record := networkServiceFixture(t)
	if _, err := service.SavePlayerRecording(id, "network-test-token", record); err != nil {
		t.Fatal(err)
	}
	detail, _ := service.Detail(id)
	report := *detail.Report
	report.Manifest.Observation.StartedAt = record.StartedAt.Add(time.Second)
	ttfb, reused := int64(10), false
	value := &PlayerNetworkReport{Version: 2, Boundary: "upstream_proxy", PlayerRevision: 1, Requests: []PlayerNetworkRequest{{Number: 1, ResourceID: "r1.ts", Kind: "media", Method: "GET", StartMS: 100, HeaderMS: assessmentNumber(130), FirstBodyMS: assessmentNumber(150), EndMS: 200, HTTPStatus: 200, Bytes: 1, Outcome: "complete", Hops: []PlayerNetworkHop{{Number: 1, StartMS: 100, EndMS: 190, HTTPStatus: 200, TTFBMS: &ttfb, ConnectionReused: &reused, Stages: []CaptureStage{{Name: "response_first_byte", StartMS: 100, EndMS: 110}, {Name: "response_headers", StartMS: 110, EndMS: 130}, {Name: "receive", StartMS: 130, EndMS: 190}}}}}}}
	before := clonePlayerNetwork(value)
	if err := persistPlayerNetwork(service.runDir(id), &report, value, record.StartedAt); err != nil {
		t.Fatal(err)
	}
	actual := report.PlayerNetwork.Requests[0].Hops[0]
	if actual.StartMS != -900 || actual.EndMS != -810 || actual.Stages[0].StartMS != -900 || actual.Stages[2].EndMS != -810 || *actual.TTFBMS != 10 || !reflect.DeepEqual(value, before) {
		t.Fatalf("nested capture clock shift altered durations or raw snapshot: %+v", actual)
	}
}

func TestPlayerNetworkPhasesSemanticValidationAndVersionOneCompatibility(t *testing.T) {
	zero := int64(0)
	base := &PlayerNetworkReport{Version: 2, Boundary: "upstream_proxy", PlayerRevision: 1, Requests: []PlayerNetworkRequest{{Number: 1, ResourceID: "r1.ts", Kind: "media", Method: "GET", StartMS: 0, EndMS: 100, Outcome: "incomplete", Hops: []PlayerNetworkHop{{Number: 1, StartMS: 0, EndMS: 100, TTFBMS: &zero, Stages: []CaptureStage{{Name: "dns", StartMS: 0, EndMS: 10}}}}}}}
	for _, test := range []struct {
		name   string
		mutate func(*PlayerNetworkReport)
	}{
		{"v1 with phase fields", func(value *PlayerNetworkReport) { value.Version = 1 }},
		{"too many hops", func(value *PlayerNetworkReport) {
			for len(value.Requests[0].Hops) <= 8 {
				value.Requests[0].Hops = append(value.Requests[0].Hops, value.Requests[0].Hops[0])
			}
		}},
		{"unordered number", func(value *PlayerNetworkReport) { value.Requests[0].Hops[0].Number = 0 }},
		{"hop beyond request", func(value *PlayerNetworkReport) { value.Requests[0].Hops[0].EndMS = 101 }},
		{"private phase name", func(value *PlayerNetworkReport) { value.Requests[0].Hops[0].Stages[0].Name = "private-domain.invalid" }},
		{"invalid phase clock", func(value *PlayerNetworkReport) { value.Requests[0].Hops[0].Stages[0].EndMS = 101 }},
		{"negative TTFB", func(value *PlayerNetworkReport) { value.Requests[0].Hops[0].TTFBMS = assessmentNumber(-1) }},
		{"TTFB beyond hop", func(value *PlayerNetworkReport) { value.Requests[0].Hops[0].TTFBMS = assessmentNumber(101) }},
		{"unmatched failure code", func(value *PlayerNetworkReport) { value.Requests[0].FailureCode = "timeout" }},
		{"private failure reason", func(value *PlayerNetworkReport) {
			value.Requests[0].FailureStage, value.Requests[0].FailureCode = "dns", "private-host-failed"
		}},
		{"aggregate drop mismatch", func(value *PlayerNetworkReport) { value.DroppedHops = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := clonePlayerNetwork(base)
			test.mutate(value)
			if err := validatePlayerNetwork(value, &PlayerReport{Revision: 1}, ProtocolHLS, false); !errors.Is(err, ErrInvalid) {
				t.Fatalf("malformed phases accepted: %v", err)
			}
		})
	}
	negative := &PlayerNetworkReport{Version: 2, Boundary: "upstream_proxy", PlayerRevision: 1, Requests: []PlayerNetworkRequest{{Number: 1, ResourceID: "r1.ts", Kind: "media", Method: "GET", StartMS: -1000, EndMS: 9000, Outcome: "incomplete", Hops: []PlayerNetworkHop{{Number: 1, StartMS: -1000, EndMS: 9000, TTFBMS: assessmentNumber(3000), Stages: []CaptureStage{{Name: "response_headers", StartMS: -950, EndMS: -900}, {Name: "receive", StartMS: -900, EndMS: 9000}}}}}}}
	if err := validatePlayerNetwork(negative, &PlayerReport{Revision: 1}, ProtocolHLS, false); !errors.Is(err, ErrInvalid) {
		t.Fatal("negative capture offsets bypassed the missing-callback header upper bound")
	}
	service, id, _, _, record := networkServiceFixture(t)
	player, _ := service.PlayerSession(id, "network-test-token")
	networkProxyRequest(t, player, "GET", "index.m3u8")
	if _, err := service.SavePlayerRecording(id, "network-test-token", record); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal(err)
	}
	var legacyData []byte
	legacy := networkRewriteBundle(t, archive.Bytes(), func(files map[string][]byte) {
		var value PlayerNetworkReport
		json.Unmarshal(files[playerNetworkName], &value)
		value.Version, value.DroppedHops = 1, 0
		for i := range value.Requests {
			value.Requests[i].Hops = nil
			value.Requests[i].DroppedHops = 0
			value.Requests[i].FailureStage, value.Requests[i].FailureCode = "", ""
		}
		legacyData, _ = json.Marshal(value)
		files[playerNetworkName] = legacyData
		phaseResignNetwork(files)
	})
	imported, err := service.Import(bytes.NewReader(legacy))
	if err != nil {
		t.Fatal(err)
	}
	replay, _ := service.Detail(imported.ID)
	if replay.Report.PlayerNetwork.Version != 1 || len(replay.Report.PlayerNetwork.Requests[0].Hops) != 0 {
		t.Fatal("v1 network ledger was silently upgraded or assigned phase data")
	}
	actual, err := readPlayerNetworkArtifact(service.runDir(imported.ID), *replay.Report.Manifest.PlayerNetwork)
	if err != nil || !bytes.Equal(actual, legacyData) {
		t.Fatal("legacy canonical sidecar changed byte-for-byte")
	}
	malformed := networkRewriteBundle(t, archive.Bytes(), func(files map[string][]byte) {
		var value PlayerNetworkReport
		json.Unmarshal(files[playerNetworkName], &value)
		value.Requests[0].Hops[0].Stages = []CaptureStage{{Name: "raw-private-host", StartMS: 0, EndMS: 0}}
		files[playerNetworkName], _ = json.Marshal(value)
		phaseResignNetwork(files)
	})
	if _, err := service.Import(bytes.NewReader(malformed)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("checksum-valid malformed phase archive imported: %v", err)
	}
}

func TestPlayerNetworkPhasesTTFBIsBoundToFirstByteProofInsteadOfBodyWindow(t *testing.T) {
	for _, test := range []struct {
		name     string
		stages   []CaptureStage
		duration int64
		valid    bool
	}{
		{"first byte proves50ms", []CaptureStage{{Name: "response_first_byte", StartMS: 0, EndMS: 50}, {Name: "response_headers", StartMS: 50, EndMS: 100}, {Name: "receive", StartMS: 100, EndMS: 10000}}, 50, true},
		{"body cannot justify inflated TTFB", []CaptureStage{{Name: "response_first_byte", StartMS: 0, EndMS: 50}, {Name: "response_headers", StartMS: 50, EndMS: 100}, {Name: "receive", StartMS: 100, EndMS: 10000}}, 3000, false},
		{"headers bound missing first byte callback", []CaptureStage{{Name: "response_headers", StartMS: 50, EndMS: 100}, {Name: "receive", StartMS: 100, EndMS: 10000}}, 3000, false},
		{"receive start bounds missing header callbacks", []CaptureStage{{Name: "receive", StartMS: 100, EndMS: 10000}}, 3000, false},
		{"callbacks absent remain unknown", nil, 3000, true},
		{"reused connection measured zero", []CaptureStage{{Name: "response_first_byte", StartMS: 0, EndMS: 0}, {Name: "response_headers", StartMS: 0, EndMS: 0}, {Name: "receive", StartMS: 0, EndMS: 10000}}, 0, true},
		{"reused zero proof rejects inflated duration", []CaptureStage{{Name: "response_first_byte", StartMS: 0, EndMS: 0}, {Name: "response_headers", StartMS: 0, EndMS: 0}, {Name: "receive", StartMS: 0, EndMS: 10000}}, 3000, false},
		{"retried transport uses final first byte point", []CaptureStage{{Name: "response_first_byte", StartMS: 0, EndMS: 10}, {Name: "request_send", StartMS: 10, EndMS: 20}, {Name: "response_first_byte", StartMS: 20, EndMS: 50}, {Name: "response_headers", StartMS: 50, EndMS: 100}, {Name: "receive", StartMS: 100, EndMS: 10000}}, 50, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reused := true
			value := &PlayerNetworkReport{Version: 2, Boundary: "upstream_proxy", PlayerRevision: 1, Requests: []PlayerNetworkRequest{{Number: 1, ResourceID: "r1.ts", Kind: "media", Method: "GET", StartMS: 0, EndMS: 10000, Outcome: "incomplete", Hops: []PlayerNetworkHop{{Number: 1, StartMS: 0, EndMS: 10000, TTFBMS: &test.duration, ConnectionReused: &reused, Stages: test.stages}}}}}
			err := validatePlayerNetwork(value, &PlayerReport{Revision: 1}, ProtocolHLS, false)
			if (err == nil) != test.valid {
				t.Fatalf("TTFB proof validation=%v, expected valid=%v", err, test.valid)
			}
		})
	}
	service, id, _, _, record := networkServiceFixture(t)
	player, _ := service.PlayerSession(id, "network-test-token")
	networkProxyRequest(t, player, "GET", "index.m3u8")
	if _, err := service.SavePlayerRecording(id, "network-test-token", record); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal(err)
	}
	malformed := networkRewriteBundle(t, archive.Bytes(), func(files map[string][]byte) {
		var value PlayerNetworkReport
		json.Unmarshal(files[playerNetworkName], &value)
		request := &value.Requests[0]
		hop := &request.Hops[0]
		request.EndMS, hop.EndMS = hop.StartMS+10000, hop.StartMS+10000
		hop.TTFBMS = assessmentNumber(3000)
		for i := range hop.Stages {
			if hop.Stages[i].Name == "receive" {
				hop.Stages[i].EndMS = hop.EndMS
			}
		}
		files[playerNetworkName], _ = json.Marshal(value)
		phaseResignNetwork(files)
	})
	if _, err := service.Import(bytes.NewReader(malformed)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("checksum-valid TTFB inflated into the body window imported: %v", err)
	}
}
