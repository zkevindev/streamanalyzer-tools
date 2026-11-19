package appserver

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/onvif"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
	"github.com/pion/rtp"
)

func TestONVIFEventRecordingSearchAndClockReplay(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	status := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, server, "POST", onvifAPI+"/start", http.StatusCreated, []byte(`{"recording_fixture":true}`)))
	fixture := status.Recording
	if fixture == nil || !fixture.RecordedMotion.Before(fixture.Until) || !fixture.MissingMotion.After(fixture.Until) {
		t.Fatalf("invalid recording fixture: %+v", fixture)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	const recording = "http://www.onvif.org/ver10/recording/wsdl"
	const search = "http://www.onvif.org/ver10/search/wsdl"
	const replay = "http://www.onvif.org/ver10/replay/wsdl"
	body := queryONVIF(t, client, status.DeviceURL, simulatorDeviceNamespace, "GetServices", "<o:IncludeCapability>true</o:IncludeCapability>", 200)
	for _, address := range []string{fixture.RecordingURL, fixture.SearchURL, fixture.ReplayURL} {
		if !strings.Contains(body, address) {
			t.Fatalf("GetServices omitted %s", address)
		}
	}
	body = queryONVIF(t, client, fixture.RecordingURL, recording, "GetRecordings", "", 200)
	if !strings.Contains(body, "<tt:RecordingToken>"+fixture.Token+"</tt:RecordingToken>") {
		t.Fatal(body)
	}
	body = queryONVIF(t, client, fixture.SearchURL, search, "FindRecordings", "<o:Scope/><o:KeepAliveTime>PT10S</o:KeepAliveTime>", 200)
	token := onvifEventText(body, "tse:SearchToken")
	if token == "" {
		t.Fatal(body)
	}
	tokenXML := "<o:SearchToken>" + token + "</o:SearchToken>"
	body = queryONVIF(t, client, fixture.SearchURL, search, "GetRecordingSearchResults", tokenXML, 200)
	if !strings.Contains(body, fixture.From.Format(time.RFC3339)) || !strings.Contains(body, fixture.Until.Format(time.RFC3339)) {
		t.Fatalf("search omitted the recording interval: %s", body)
	}
	queryONVIF(t, client, fixture.SearchURL, search, "EndSearch", tokenXML, 200)
	queryONVIF(t, client, fixture.SearchURL, search, "GetRecordingSearchResults", tokenXML, 400)
	body = queryONVIF(t, client, fixture.ReplayURL, replay, "GetReplayUri", "<o:RecordingToken>"+fixture.Token+"</o:RecordingToken>", 200)
	target := onvifEventText(body, "trp:Uri")
	if target == "" || target != fixture.StreamURL {
		t.Fatal(body)
	}

	sub := queryONVIF(t, client, status.EventURL, simulatorEventNamespace, "CreatePullPointSubscription", "", 200)
	eventURL := onvifEventText(sub, "wsa:Address")
	for _, tc := range []struct {
		recorded string
		at       time.Time
		code     base.StatusCode
	}{{"true", fixture.RecordedMotion, base.StatusOK}, {"false", fixture.MissingMotion, base.StatusInvalidRange}} {
		diagnosticRequest(t, server, "POST", onvifAPI+"/recording-motion", http.StatusOK, []byte(`{"recorded":`+tc.recorded+`}`))
		events := queryONVIF(t, client, eventURL, simulatorEventNamespace, "PullMessages", "<o:Timeout>PT0S</o:Timeout><o:MessageLimit>10</o:MessageLimit>", 200)
		if !strings.Contains(events, tc.at.Format(time.RFC3339)) || !strings.Contains(events, `Name="IsMotion" Value="true"`) {
			t.Fatalf("missing event at archive clock: %s", events)
		}
		playRecordingRange(t, target, tc.at, tc.code)
	}
	final := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, server, "GET", onvifAPI, http.StatusOK, nil))
	if final.Recording.Plays != 1 || final.Recording.MissingRange != 1 {
		t.Fatalf("replay evidence not split between playable and no-recording cases: %+v", final.Recording)
	}
}

func playRecordingRange(t *testing.T, target string, at time.Time, code base.StatusCode) {
	t.Helper()
	u, err := base.ParseURL(target)
	if err != nil {
		t.Fatal(err)
	}
	transport := gortsplib.ProtocolTCP
	client := &gortsplib.Client{Scheme: u.Scheme, Host: u.Host, Protocol: &transport, ReadTimeout: 2 * time.Second}
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	desc, _, err := client.Describe(u)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetupAll(desc.BaseURL, desc.Medias); err != nil {
		t.Fatal(err)
	}
	packets := make(chan int, 8)
	client.OnPacketRTP(desc.Medias[0], desc.Medias[0].Formats[0], func(packet *rtp.Packet) {
		select {
		case packets <- len(packet.Payload):
		default:
		}
	})
	end := at.Add(time.Second)
	response, err := client.Play(&headers.Range{Value: &headers.RangeUTC{Start: at, End: &end}})
	if code != base.StatusOK {
		var rejected liberrors.ErrClientBadStatusCode
		if !errors.As(err, &rejected) || rejected.Code != code {
			t.Fatalf("wrong missing recording result: %+v (%v)", response, err)
		}
		return
	}
	if err != nil || response == nil || response.StatusCode != code {
		t.Fatalf("wrong RTSP range response: %+v (%v)", response, err)
	}
	select {
	case bytes := <-packets:
		if bytes == 0 {
			t.Fatal("no playable payload")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for archive video RTP")
	}
}

func TestONVIFRecordingGapAndCrossSegmentReplay(t *testing.T) {
	app := testDiagnosticServer(t, t.TempDir())
	camera := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, app, "POST", onvifAPI+"/start", http.StatusCreated,
		[]byte(`{"recording_fixture":true,"recording_gap_seconds":8}`)))
	recording := camera.Recording
	if recording == nil || len(recording.Segments) != 2 || recording.Segments[1].From.Sub(recording.Segments[0].Until) != 8*time.Second {
		t.Fatalf("missing gap fixture: %+v", recording)
	}
	httpClient := &http.Client{Timeout: time.Second}
	const search = "http://www.onvif.org/ver10/search/wsdl"
	summary := queryONVIF(t, httpClient, recording.SearchURL, search, "GetRecordingSummary", "", 200)
	if !strings.Contains(summary, recording.From.Format(time.RFC3339)) || !strings.Contains(summary, recording.Until.Format(time.RFC3339)) {
		t.Fatal(summary)
	}
	sub := queryONVIF(t, httpClient, camera.EventURL, simulatorEventNamespace, "CreatePullPointSubscription", "", 200)
	diagnosticRequest(t, app, "POST", onvifAPI+"/recording-motion", 200, []byte(`{"recorded":false}`))
	event := queryONVIF(t, httpClient, onvifEventText(sub, "wsa:Address"), simulatorEventNamespace, "PullMessages",
		"<o:Timeout>PT0S</o:Timeout><o:MessageLimit>4</o:MessageLimit>", 200)
	if !strings.Contains(event, recording.MissingMotion.Format(time.RFC3339)) {
		t.Fatal(event)
	}
	playRecordingRange(t, recording.StreamURL, recording.MissingMotion, base.StatusInvalidRange)
	playRecordingRange(t, recording.StreamURL, recording.RecordedMotion, base.StatusOK)

	u, err := base.ParseURL(recording.StreamURL)
	if err != nil {
		t.Fatal(err)
	}
	transport := gortsplib.ProtocolTCP
	client := &gortsplib.Client{Scheme: u.Scheme, Host: u.Host, Protocol: &transport, ReadTimeout: 4 * time.Second}
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	desc, _, err := client.Describe(u)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetupAll(desc.BaseURL, desc.Medias); err != nil {
		t.Fatal(err)
	}
	start := recording.Segments[0].Until.Add(-time.Second)
	end := recording.Segments[1].From.Add(time.Second)
	res, err := client.Play(&headers.Range{Value: &headers.RangeUTC{Start: start, End: &end}})
	if err != nil {
		t.Fatal(err)
	}
	var reply headers.Range
	if reply.Unmarshal(res.Header["Range"]) != nil || !reply.Value.(*headers.RangeUTC).Start.Equal(start) ||
		!reply.Value.(*headers.RangeUTC).End.Equal(end) {
		t.Fatalf("wrong response Range: %+v", res)
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		state := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, app, "GET", onvifAPI, 200, nil)).Recording
		if len(state.Playback) != 3 {
			continue
		}
		trace := state.Playback[2]
		if len(trace.Keyframes) < 2 {
			continue
		}
		if state.Playback[0].Code != 457 || trace.Code != 200 || !trace.Requested.From.Equal(start) ||
			!trace.Returned.Until.Equal(end) || len(trace.Parts) != 2 || trace.Keyframes[1].Sub(trace.Keyframes[0]) > 2*time.Second {
			t.Fatalf("incorrect gap playback trace: %+v", state.Playback)
		}
		return
	}
	t.Fatal("did not see keyframes from both recording segments")
}
