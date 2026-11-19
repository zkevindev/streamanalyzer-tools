package appserver

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/gb28181"
)

func TestGB28181PlatformRecoversAfterSIPInterruption(t *testing.T) {
	for _, scenario := range []struct{ transport, mode string }{
		{"tcp", "sip_tcp_disconnect"},
		{"udp", "udp_keepalive_timeout"},
	} {
		t.Run(scenario.transport, func(t *testing.T) {
			t.Parallel()
			server := testDiagnosticServer(t, t.TempDir())
			config := fmt.Appendf(nil, `{"sip_transport":%q,"platform_id":%q,"device_id":%q}`, scenario.transport, platformTestID, platformDeviceID)
			status := decodeDiagnostic[gb28181.PlatformStatus](t, diagnosticRequest(t, server, "POST", platformAPI+"/start", http.StatusCreated, config))
			host, port, err := net.SplitHostPort(status.Address)
			if err != nil {
				t.Fatal(err)
			}
			number, err := strconv.Atoi(port)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := json.Marshal(gb28181.Config{PlatformHost: host, PlatformPort: number, PlatformID: platformTestID,
				DeviceID: platformDeviceID, ChannelID: platformChannelID, SIPTransport: scenario.transport, Heartbeat: 5})
			if err != nil {
				t.Fatal(err)
			}
			diagnosticRequest(t, server, "POST", platformDeviceAPI+"/start", http.StatusAccepted, cfg)
			waitSimulator(t, server, platformDeviceAPI, "registered")
			preview := fmt.Appendf(nil, `{"channel_id":%q,"window_ms":1600,"recovery":%q}`, platformChannelID, scenario.mode)
			diagnosticRequest(t, server, "POST", platformAPI+"/preview", http.StatusAccepted, preview)
			deadline := time.Now().Add(34 * time.Second)
			for {
				status = decodeDiagnostic[gb28181.PlatformStatus](t, diagnosticRequest(t, server, "GET", platformAPI, http.StatusOK, nil))
				if !status.Busy && status.Recovery != nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("recovery observation did not finish: %+v", status.Recovery)
				}
				time.Sleep(150 * time.Millisecond)
			}
			r := status.Recovery
			if r.State != "passed" || r.Before.CallID == status.Preview.CallID || r.Before.Media.Keyframes == 0 || status.Preview.Media.Keyframes == 0 ||
				r.PreviousSession == nil || !hasGBPreviewStage(r.PreviousSession.Timeline, "ended") || hasGBPreviewStage(r.Before.Timeline, "bye") ||
				r.RecoveryMS == nil || *r.RecoveryMS < 1 || !r.InterruptedAt.Before(r.RegisteredAt) || r.RegisteredAt.After(r.CatalogAt) ||
				r.CatalogAt.After(r.FirstKeyframeAt) || status.DeviceSession == nil || status.DeviceSession.CallID != status.Preview.CallID {
				t.Fatalf("recovery did not include two separate calls and ordered evidence: recovery=%+v old=%+v new=%+v", r, r.Before, status.Preview)
			}
			if scenario.transport == "udp" && r.IgnoredKeepalives != 3 {
				t.Fatalf("expected three distinct unanswered heartbeats, got %d", r.IgnoredKeepalives)
			}
			export := decodeDiagnostic[gb28181.PlatformStatus](t, diagnosticRequest(t, server, "GET", platformAPI+"/trace.json", http.StatusOK, nil))
			if export.Recovery.Before.CallID != r.Before.CallID || export.Preview.CallID != status.Preview.CallID {
				t.Fatalf("exported evidence is missing a call: %+v", export.Recovery)
			}
		})
	}
}
