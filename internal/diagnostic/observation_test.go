package diagnostic

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"
)

func TestHTTPAttemptKeepsStagesAndSanitizedFailure(t *testing.T) {
	start := time.Now()
	observation := Observation{}
	ctx, finish := recordHTTPAttempt(t.Context(), &observation, start)
	trace := httptrace.ContextClientTrace(ctx)
	trace.DNSStart(httptrace.DNSStartInfo{Host: "private-host"})
	trace.DNSDone(httptrace.DNSDoneInfo{})
	trace.ConnectStart("tcp", "private-host:443")
	trace.ConnectDone("tcp", "private-host:443", nil)
	trace.TLSHandshakeStart()
	trace.TLSHandshakeDone(tls.ConnectionState{}, context.DeadlineExceeded)
	finish(context.DeadlineExceeded)
	finish(nil)
	observation.ElapsedMS = time.Since(start).Milliseconds()
	if observation.FailureStage != "tls" || observation.FailureCode != "timeout" || len(observation.Stages) != 3 {
		t.Fatalf("incorrect phase diagnostics: %+v", observation)
	}
	if err := validateObservationDetails(observation); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(observation)
	if strings.Contains(string(data), "private-host") {
		t.Fatal("trace exported a raw network address")
	}
	observation.Stages[0].EndMS = observation.ElapsedMS + 1
	if !errors.Is(validateObservationDetails(observation), ErrInvalid) {
		t.Fatal("out-of-range imported phase timestamp was accepted")
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
