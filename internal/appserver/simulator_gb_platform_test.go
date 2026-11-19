package appserver

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/gb28181"

	"github.com/pion/rtp"
)

const platformAPI = "/api/simulators/gb28181/platform"
const platformDeviceAPI = "/api/simulators/gb28181"
const platformTestID = "34020000002000000001"
const platformDeviceID = "34020000001320000001"
const platformChannelID = "34020000001320000002"

func TestGB28181PlatformReceivesAndCorrelatesPreview(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	for _, path := range []string{"/simulators", "/simulators/gb28181/platform"} {
		if body := diagnosticRequest(t, server, http.MethodGet, path, http.StatusOK, nil); !strings.Contains(string(body), "GB28181 平台侧收流") {
			t.Fatalf("missing platform entry at %s", path)
		}
	}
	status := startGBPreviewPlatform(t, server)
	_, port, err := net.SplitHostPort(status.Address)
	if err != nil {
		t.Fatal(err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	cfg := gb28181.Config{PlatformHost: "127.0.0.1", PlatformPort: portNumber, PlatformID: platformTestID, DeviceID: platformDeviceID, ChannelID: platformChannelID}
	for _, scenario := range []struct {
		state string
		delay int
		skip  bool
	}{{"passed", 0, false}, {"no_ack", 0, true}, {"no_rtp", 2000, false}} {
		t.Run(scenario.state, func(t *testing.T) {
			if scenario.delay != 0 {
				cfg.Faults = &gb28181.FaultConfig{FirstDelayMS: scenario.delay}
			}
			body, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			diagnosticRequest(t, server, http.MethodPost, platformDeviceAPI+"/start", http.StatusAccepted, body)
			waitSimulator(t, server, platformDeviceAPI, "registered")
			startGBPlatformPreview(t, server, scenario.skip)
			got := waitGBPlatformPreview(t, server, scenario.state)
			if got.DeviceSession == nil || got.Preview.CallID != got.DeviceSession.CallID || got.Preview.SIPCode != 200 {
				t.Fatalf("preview did not match the device timeline: %+v / %+v", got.Preview, got.DeviceSession)
			}
			if scenario.state == "passed" && (got.Preview.Media.Packets == 0 || got.Preview.Media.Keyframes == 0) {
				t.Fatalf("received no decodable keyframe: %+v", got.Preview)
			}
			if scenario.state == "no_ack" && !hasGBPreviewStage(got.Preview.Timeline, "ack_withheld") {
				t.Fatalf("ACK control was not recorded: %+v", got.Preview)
			}
			if scenario.state == "no_rtp" && (!hasGBPreviewStage(got.Preview.Timeline, "ack") || got.Preview.Media.Packets != 0) {
				t.Fatalf("no-RTP result has inconsistent evidence: %+v", got.Preview)
			}
			export := decodeDiagnostic[gb28181.PlatformStatus](t, diagnosticRequest(t, server, http.MethodGet, platformAPI+"/trace.json", http.StatusOK, nil))
			if export.Preview.CallID != got.Preview.CallID {
				t.Fatal("unexpected trace")
			}
			diagnosticRequest(t, server, http.MethodPost, platformDeviceAPI+"/stop", http.StatusAccepted, nil)
			waitSimulator(t, server, platformDeviceAPI, "stopped")
		})
	}
	diagnosticRequest(t, server, http.MethodPost, platformAPI+"/stop", http.StatusOK, nil)
}

func TestGB28181PlatformClassifiesRTPWithoutKeyframe(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	status := startGBPreviewPlatform(t, server)
	addr, err := net.ResolveUDPAddr("udp4", status.Address)
	if err != nil {
		t.Fatal(err)
	}
	device := openSimulatorUDP(t)
	uri := "sip:" + platformTestID + "@" + status.Address
	deviceURI := "<sip:" + platformDeviceID + "@" + device.LocalAddr().String() + ">"
	sendSimulatorRequest(t, device, addr, deviceURI+";tag=fake", "REGISTER", uri, deviceURI, "manual-register", "manual-register", "", "", 1)
	receiveSimulatorSIP(t, device, "SIP/2.0 200 ", time.Second)
	started := startGBPlatformPreview(t, server, false)
	invite, _ := receiveSimulatorSIP(t, device, "INVITE ", time.Second)
	sendSimulatorSIP(t, device, addr, sipTestResponse(invite, "200 OK", "Contact: "+deviceURI+"\r\n", invite.body))
	receiveSimulatorSIP(t, device, "ACK ", time.Second)
	target, err := net.ResolveUDPAddr("udp4", started.Preview.RTPAddress)
	if err != nil {
		t.Fatal(err)
	}
	// A complete PS pack with a video PES containing a non-IDR Annex B slice.
	ps := []byte{0, 0, 1, 0xba, 0x44, 0, 4, 0, 4, 1, 1, 1, 3, 0xf8, 0, 0, 1, 0xe0, 0, 8, 0x80, 0, 0, 0, 0, 1, 0x41, 0x80}
	packet := rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 96, Marker: true, SSRC: started.Preview.SSRC}, Payload: ps}
	buf, err := packet.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = device.WriteToUDP(buf, target); err != nil {
		t.Fatal(err)
	}
	bye, _ := receiveSimulatorSIP(t, device, "BYE ", 2*time.Second)
	sendSimulatorSIP(t, device, addr, bye.response("200 OK", ""))
	result := waitGBPlatformPreview(t, server, "no_keyframe")
	if result.Preview.Media.Packets != 1 || result.Preview.Media.PSUnits != 1 || result.Preview.Media.Keyframes != 0 || result.DeviceSession != nil {
		t.Fatalf("wrong receiver evidence: %+v", result.Preview)
	}
}

func startGBPreviewPlatform(t *testing.T, s *Server) gb28181.PlatformStatus {
	t.Helper()
	return decodeDiagnostic[gb28181.PlatformStatus](t, diagnosticRequest(t, s, http.MethodPost, platformAPI+"/start", http.StatusCreated,
		[]byte(`{"platform_id":"`+platformTestID+`","device_id":"`+platformDeviceID+`"}`)))
}

func startGBPlatformPreview(t *testing.T, s *Server, skip bool) gb28181.PlatformStatus {
	t.Helper()
	return decodeDiagnostic[gb28181.PlatformStatus](t, diagnosticRequest(t, s, http.MethodPost, platformAPI+"/preview", http.StatusAccepted,
		[]byte(`{"channel_id":"`+platformChannelID+`","window_ms":550,"skip_ack":`+strconv.FormatBool(skip)+"}")))
}

func waitGBPlatformPreview(t *testing.T, s *Server, state string) gb28181.PlatformStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		result := decodeDiagnostic[gb28181.PlatformStatus](t, diagnosticRequest(t, s, http.MethodGet, platformAPI, http.StatusOK, nil))
		if result.Preview != nil && !result.Busy {
			if result.Preview.State != state {
				t.Fatalf("expected %s, got %+v", state, result.Preview)
			}
			return result
		}
		if time.Now().After(deadline) {
			t.Fatalf("preview did not complete: %+v", result.Preview)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func hasGBPreviewStage(marks []gb28181.SessionMark, want string) bool {
	for _, mark := range marks {
		if mark.Stage == want {
			return true
		}
	}
	return false
}

func TestGB28181PlatformTCPMediaRoles(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	status := decodeDiagnostic[gb28181.PlatformStatus](t, diagnosticRequest(t, server, "POST", platformAPI+"/start", http.StatusCreated,
		[]byte(`{"platform_id":"`+platformTestID+`","device_id":"`+platformDeviceID+`","sip_transport":"tcp"}`)))
	_, portText, err := net.SplitHostPort(status.Address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := json.Marshal(gb28181.Config{PlatformHost: "127.0.0.1", PlatformPort: port, PlatformID: platformTestID, DeviceID: platformDeviceID, ChannelID: platformChannelID, SIPTransport: "tcp"})
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, "POST", platformDeviceAPI+"/start", http.StatusAccepted, cfg)
	waitSimulator(t, server, platformDeviceAPI, "registered")
	for _, tc := range []struct {
		role string
		skip bool
		want string
	}{
		{"udp", false, "passed"}, {"tcp-active", false, "passed"}, {"tcp-passive", false, "passed"}, {"tcp-active", true, "no_media_connection"},
	} {
		t.Run(tc.role+"/"+tc.want, func(t *testing.T) {
			req := []byte(`{"channel_id":"` + platformChannelID + `","window_ms":650,"media_transport":"` + tc.role + `","skip_connect":` + strconv.FormatBool(tc.skip) + `}`)
			diagnosticRequest(t, server, "POST", platformAPI+"/preview", http.StatusAccepted, req)
			got := waitGBPlatformPreview(t, server, tc.want)
			if got.SIPTransport != "tcp" || got.DeviceSession == nil || got.DeviceSession.CallID != got.Preview.CallID {
				t.Fatalf("unexpected TCP SIP status / correlation: %+v", got)
			}
			if (tc.want == "passed") != (got.Preview.Media.Keyframes > 0) ||
				(tc.role != "udp" && !tc.skip) != hasGBPreviewStage(got.Preview.Timeline, "media_connected") {
				t.Fatalf("unexpected TCP media evidence: %+v", got.Preview)
			}
		})
	}
	diagnosticRequest(t, server, "POST", platformDeviceAPI+"/stop", http.StatusAccepted, nil)
	waitSimulator(t, server, platformDeviceAPI, "stopped")
}
