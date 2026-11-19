package appserver

import (
	"bytes"
	"encoding/json"
	"html"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/simulator/onvif"
)

func TestRTSPSenderClockStepInTCPAndUDP(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	for _, transport := range []string{"tcp", "udp"} {
		t.Run(transport, func(t *testing.T) {
			manager := &onvif.Manager{}
			defer manager.Close()
			camera, err := manager.Start(onvif.Config{RTCPClockStepMS: 350})
			if err != nil {
				t.Fatal(err)
			}
			const api = "/api/diagnostics/runs"
			input, _ := json.Marshal(diagnostic.SampleRequest{Protocol: diagnostic.ProtocolRTSP, Transport: transport, URL: camera.StreamURL, Seconds: 5})
			run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", api, http.StatusAccepted, input))
			path := api + "/" + run.ID
			detail := waitDiagnostic(t, server, path)
			if detail.Report == nil || detail.Report.RTSP == nil || len(detail.Report.RTSP.SenderClocks) != 2 {
				t.Fatalf("expected two active clocks: %+v", detail.Report)
			}
			metrics := detail.Report.RTSP
			for _, clock := range metrics.SenderClocks {
				track := metrics.Tracks[clock.TrackID-1]
				var expected float64
				if track.Kind == "audio" {
					expected = 350
				}
				if clock.Intervals < 3 || math.Abs(clock.MaxStepMS-expected) > 0.5 || track.RTPPackets < 10 ||
					track.SequenceGaps != 0 || clock.MaxArrivalVariationMS > 220 {
					t.Fatalf("unexpected %s clock: %+v, track: %+v", track.Kind, clock, track)
				}
			}
			raw := diagnosticRequest(t, server, "GET", path+"/evidence.zip", 200, nil)
			rendered := diagnosticRequest(t, server, "GET", path+"/report.html", 200, nil)
			if !bytes.Contains(rendered, []byte("RTCP 发送端时钟")) || !strings.Contains(html.UnescapeString(string(rendered)), "+350.0") {
				t.Fatal("missing clocks in HTML")
			}
			manager.Close()
			replay := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/import", 201, raw))
			offline := waitDiagnostic(t, server, api+"/"+replay.ID)
			if offline.Report == nil || !reflect.DeepEqual(offline.Report.RTSP.SenderClocks, metrics.SenderClocks) {
				t.Fatal("archive analysis changed sender clocks")
			}
		})
	}
}
