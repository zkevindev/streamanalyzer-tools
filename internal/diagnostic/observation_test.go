package diagnostic

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestHTTPAttemptKeepsStagesAndSanitizedFailure(t *testing.T) {
	client := &http.Client{Transport: observedTestTransport(func(r *http.Request) (*http.Response, error) {
		trace := httptrace.ContextClientTrace(r.Context())
		trace.DNSStart(httptrace.DNSStartInfo{Host: "private-host"})
		trace.DNSDone(httptrace.DNSDoneInfo{})
		trace.ConnectStart("tcp", "private-host:443")
		trace.ConnectDone("tcp", "private-host:443", nil)
		trace.TLSHandshakeStart()
		trace.TLSHandshakeDone(tls.ConnectionState{}, context.DeadlineExceeded)
		return nil, context.DeadlineExceeded
	})}
	start := time.Now()
	observation := Observation{}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://private-host/path?secret=token", nil)
	_, err := doObservedRequest(client, request, &observation, start)
	observation.ElapsedMS = time.Since(start).Milliseconds()
	if !errors.Is(err, context.DeadlineExceeded) || observation.FailureStage != "tls" || observation.FailureCode != "timeout" ||
		len(observation.Requests) != 1 || len(observation.Requests[0].Stages) != 3 || observation.Requests[0].FailureStage != "tls" {
		t.Fatalf("incorrect phase diagnostics: %+v", observation)
	}
	if err := validateObservationDetails(observation); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(observation)
	if strings.Contains(string(data), "path?secret") || strings.Contains(string(data), "secret=token") {
		t.Fatal("trace exported the raw request URL or query")
	}
	observation.Requests[0].Stages[0].EndMS = observation.ElapsedMS + 1
	if !errors.Is(validateObservationDetails(observation), ErrInvalid) {
		t.Fatal("out-of-range imported phase timestamp was accepted")
	}
}

type observedTestTransport func(*http.Request) (*http.Response, error)

func (f observedTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestObservedHTTPRedirectTimesEachResponseBeforeBody(t *testing.T) {
	bodyReady := make(chan struct{})
	const redirectDelay = 22 * time.Millisecond
	const finalDelay = 30 * time.Millisecond
	const bodyDelay = 20 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			time.Sleep(redirectDelay)
			http.Redirect(w, r, "/final?internal-token=second", http.StatusFound)
			return
		}
		if r.Referer() != "" {
			t.Error("signed redirect referer escaped")
		}
		time.Sleep(finalDelay)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-bodyReady:
			time.Sleep(bodyDelay)
			_, _ = io.WriteString(w, "delayed response body")
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	start := time.Now()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/redirect?internal-token=first", nil)
	observation := Observation{}
	response, err := doObservedRequest(captureClient(), request, &observation, start)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if len(observation.Requests) != 2 {
		t.Fatalf("recorded %d requests, want 2", len(observation.Requests))
	}
	observation.ElapsedMS = time.Since(start).Milliseconds()
	if err := validateObservationDetails(observation); err != nil {
		t.Fatal(err)
	}
	first, second := observation.Requests[0], observation.Requests[1]
	if first.HTTPStatus != http.StatusFound || second.HTTPStatus != http.StatusOK || first.Number != 1 || second.Number != 2 ||
		first.EndMS > second.StartMS || first.TTFBMS == nil || second.TTFBMS == nil ||
		*first.TTFBMS < redirectDelay.Milliseconds()-1 || *second.TTFBMS < finalDelay.Milliseconds()-1 ||
		observation.TTFBMS == nil || *observation.TTFBMS != second.StartMS+*second.TTFBMS {
		t.Fatalf("incorrect redirect timing: %+v", observation.Requests)
	}
	for _, attempt := range observation.Requests {
		for _, stage := range []string{"connect", "request_send", "response_first_byte", "response_headers"} {
			if !slices.ContainsFunc(attempt.Stages, func(measurement CaptureStage) bool { return measurement.Name == stage }) {
				t.Errorf("request %d missing %s: %+v", attempt.Number, stage, attempt.Stages)
			}
		}
	}
	data, _ := json.Marshal(observation)
	if strings.Contains(string(data), "internal-token") {
		t.Fatal("redirect trace exported a signed target query")
	}
	bodyStart := time.Since(start).Milliseconds()
	close(bodyReady)
	if body, err := io.ReadAll(response.Body); err != nil || string(body) != "delayed response body" {
		t.Fatalf("body was consumed or changed by timings: %q %v", body, err)
	}
	observation.ElapsedMS = time.Since(start).Milliseconds()
	recordHTTPBody(&observation, CaptureStage{Name: "receive", StartMS: bodyStart, EndMS: observation.ElapsedMS})
	first, second = observation.Requests[0], observation.Requests[1]
	if first.BodyEndMS != nil || second.BodyEndMS == nil || *second.BodyEndMS-bodyStart < bodyDelay.Milliseconds()-1 ||
		second.TotalMS != second.EndMS-second.StartMS || second.Stages[len(second.Stages)-1].Name != "receive" {
		t.Fatalf("incorrect body timing: %+v", observation.Requests)
	}
	if err := validateObservationDetails(observation); err != nil {
		t.Fatal(err)
	}
	badEnd := observation.ElapsedMS + 1
	observation.Requests[1].BodyEndMS = &badEnd
	if !errors.Is(validateObservationDetails(observation), ErrInvalid) {
		t.Fatal("out-of-range imported body completion timestamp was accepted")
	}
}

func TestObservedHTTPKeepsRedirectOnCanceledNextHop(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/pending", http.StatusFound)
			return
		}
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	writes := make(chan struct{}, 2)
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			select {
			case writes <- struct{}{}:
			default:
			}
		}
	}})
	go func() {
		for _, ready := range []<-chan struct{}{entered, writes, writes} {
			select {
			case <-ready:
			case <-ctx.Done():
				return
			}
		}
		cancel()
	}()
	start := time.Now()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/redirect", nil)
	observation := Observation{}
	_, err := doObservedRequest(captureClient(), request, &observation, start)
	observation.ElapsedMS = time.Since(start).Milliseconds()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("request failed with %v, want cancellation", err)
	}
	if len(observation.Requests) != 2 || observation.Requests[0].HTTPStatus != http.StatusFound ||
		observation.Requests[1].HTTPStatus != 0 || observation.Requests[1].TTFBMS != nil ||
		observation.Requests[1].FailureStage != "response_first_byte" || observation.Requests[1].FailureCode != "canceled" ||
		observation.FailureStage != "response_first_byte" || observation.FailureCode != "canceled" {
		t.Fatalf("incorrect cancellation timing: %+v", observation)
	}
	if err := validateObservationDetails(observation); err != nil {
		t.Fatal(err)
	}
}

func TestObservedHTTPKeepsResponseWhenClientRejectsRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirect-target", http.StatusFound)
	}))
	defer server.Close()
	client := captureClient()
	redirectError := errors.New("rejected redirect with internal details")
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return redirectError }
	start := time.Now()
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	observation := Observation{}
	_, err := doObservedRequest(client, request, &observation, start)
	if !errors.Is(err, redirectError) || len(observation.Requests) != 1 ||
		observation.Requests[0].HTTPStatus != http.StatusFound || observation.Requests[0].FailureCode != "redirect_error" {
		t.Fatalf("incorrect rejected redirect trace: %+v %v", observation, err)
	}
	data, _ := json.Marshal(observation)
	if strings.Contains(string(data), redirectError.Error()) {
		t.Fatal("redirect rejection exported the raw error")
	}
}

func TestFrameFindingsCountRepeatedEventsAndAudioStalls(t *testing.T) {
	report := analyze(Manifest{
		Protocol: ProtocolHTTPFLV, Observation: Observation{ElapsedMS: 8000, EndReason: "duration_limit"},
	}, []Event{
		{Kind: "video", DTS: 1000, ArrivalMS: 0},
		{Kind: "audio", DTS: 1000, ArrivalMS: 10},
		{Kind: "video", DTS: 900, ArrivalMS: 100},
		{Kind: "video", DTS: 800, ArrivalMS: 200},
		{Kind: "audio", DTS: 6000, ArrivalMS: 6000},
	})
	var counted, audioGap, noKey bool
	for _, finding := range report.Findings {
		switch finding.Code {
		case "video_dts_backwards":
			counted = finding.Occurrences == 2 && finding.NextCheck != ""
		case "audio_arrival_gap":
			audioGap = len(finding.Evidence) == 2
		case "no_video_keyframe":
			noKey = true
		}
	}
	if !counted || !audioGap || !noKey || report.Metrics.MaxAudioGapMS != 5990 {
		t.Fatalf("detailed media findings missing: %+v", report)
	}
}
