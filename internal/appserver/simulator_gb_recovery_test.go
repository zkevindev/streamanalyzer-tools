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
		{"udp", "media_rtp_timeout"},
		{"tcp", "media_rtp_timeout"},
	} {
		t.Run(scenario.transport+"/"+scenario.mode, func(t *testing.T) {
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
			device := gb28181.Config{PlatformHost: host, PlatformPort: number, PlatformID: platformTestID,
				DeviceID: platformDeviceID, ChannelID: platformChannelID, SIPTransport: scenario.transport, Heartbeat: 5}
			window := 1600
			if scenario.mode == "media_rtp_timeout" {
				window = 8500
				device.Faults = &gb28181.FaultConfig{SilenceAfterMS: 3200}
			}
			cfg, err := json.Marshal(device)
			if err != nil {
				t.Fatal(err)
			}
			diagnosticRequest(t, server, "POST", platformDeviceAPI+"/start", http.StatusAccepted, cfg)
			waitSimulator(t, server, platformDeviceAPI, "registered")
			preview := fmt.Appendf(nil, `{"channel_id":%q,"window_ms":%d,"recovery":%q}`, platformChannelID, window, scenario.mode)
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
			if scenario.mode == "media_rtp_timeout" {
				if r.State != "passed" || r.Before.State != "media_timeout" || !hasGBPreviewStage(r.Before.Timeline, "bye") ||
					r.PreviousSession == nil || !hasGBPreviewStage(r.PreviousSession.Timeline, "fault_media_silent") ||
					r.Before.CallID == status.Preview.CallID || r.Before.SSRC == status.Preview.SSRC || r.Before.RTPAddress == status.Preview.RTPAddress ||
					r.RegistrationChanges != 0 || !r.KeepaliveAt.After(r.LastRTPAt) || status.Preview.Media.Keyframes < 1 ||
					r.RecoveryMS == nil || *r.RecoveryMS < 3500 {
					t.Fatalf("media-only recovery evidence missing: %+v old=%+v new=%+v", r, r.Before, status.Preview)
				}
				return
			}
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

func TestGB28181PlatformConcurrentDeviceIsolation(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	const otherDevice = "34020000001320000003"
	const otherChannel = "34020000001320000004"
	start := gb28181.PlatformConfig{PlatformID: platformTestID, DeviceIDs: []string{platformDeviceID, otherDevice}, SIPTransport: "tcp"}
	input, _ := json.Marshal(start)
	platform := decodeDiagnostic[gb28181.PlatformStatus](t, diagnosticRequest(t, server, "POST", platformAPI+"/start", 201, input))
	host, port, err := net.SplitHostPort(platform.Address)
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	cfg := gb28181.Config{PlatformHost: host, PlatformPort: number, PlatformID: platformTestID, DeviceID: platformDeviceID, ChannelID: platformChannelID,
		DeviceCount: 2, ChannelsPerDevice: 1, SIPTransport: "tcp"}
	input, _ = json.Marshal(cfg)
	diagnosticRequest(t, server, "POST", platformDeviceAPI+"/start", 202, input)
	wait := func(match func([]gb28181.PlatformDeviceStatus) bool) []gb28181.PlatformDeviceStatus {
		t.Helper()
		for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline); time.Sleep(40 * time.Millisecond) {
			s := decodeDiagnostic[gb28181.PlatformStatus](t, diagnosticRequest(t, server, "GET", platformAPI, 200, nil))
			if len(s.Devices) == 2 && match(s.Devices) {
				return s.Devices
			}
		}
		t.Fatal("platform devices did not reach expected state")
		return nil
	}
	wait(func(s []gb28181.PlatformDeviceStatus) bool { return s[0].Peer != "" && s[1].Peer != "" })
	startPlayback := func(c gb28181.PreviewConfig) {
		input, _ := json.Marshal(c)
		diagnosticRequest(t, server, "POST", platformAPI+"/preview", 202, input)
	}
	startPlayback(gb28181.PreviewConfig{DeviceID: otherDevice, ChannelID: otherChannel, WindowMS: 12000})
	before := wait(func(s []gb28181.PlatformDeviceStatus) bool {
		return s[1].Preview != nil && s[1].Preview.Media.Keyframes > 0
	})[1]
	startPlayback(gb28181.PreviewConfig{DeviceID: platformDeviceID, ChannelID: platformChannelID, WindowMS: 1800, Recovery: "sip_tcp_disconnect"})
	after := wait(func(s []gb28181.PlatformDeviceStatus) bool {
		return s[0].Recovery != nil && s[0].Recovery.State == "passed" && !s[0].Busy &&
			s[1].Preview.Media.Keyframes > before.Preview.Media.Keyframes
	})
	a, b := after[0], after[1]
	if a.Recovery.Before.CallID == a.Preview.CallID || a.Recovery.PreviousSession == nil ||
		a.DeviceSession == nil || a.DeviceSession.CallID != a.Preview.CallID || !b.Busy ||
		b.Peer != before.Peer || b.Preview.CallID != before.Preview.CallID || b.Preview.SSRC != before.Preview.SSRC ||
		b.Preview.Media.Packets <= before.Preview.Media.Packets || b.Preview.Media.Keyframes <= before.Preview.Media.Keyframes ||
		b.DeviceSession == nil || b.DeviceSession.CallID != b.Preview.CallID || a.Preview.SSRC == b.Preview.SSRC {
		t.Fatalf("device activity was not isolated: A=%+v B=%+v", a, b)
	}
	export := decodeDiagnostic[gb28181.PlatformStatus](t, diagnosticRequest(t, server, "GET", platformAPI+"/trace.json", 200, nil))
	if len(export.Devices) != 2 || export.Devices[1].Preview.CallID != b.Preview.CallID {
		t.Fatal("missing concurrent evidence")
	}
}
