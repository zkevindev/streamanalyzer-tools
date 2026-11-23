package appserver

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/whep"

	"github.com/pion/webrtc/v4"
)

func TestWHEPSimulatorNegotiatesAndSeparatesFailureStages(t *testing.T) {
	app := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/whep"
	page := diagnosticRequest(t, app, "GET", "/simulators", 200, nil)
	if !strings.Contains(string(page), "href=\"/simulators/whep\"") {
		t.Fatal("WHEP is missing from the simulator entry")
	}
	diagnosticRequest(t, app, "POST", api+"/start", 400, []byte(`{"mode":"invalid"}`))
	for _, mode := range []string{"normal", "ice_timeout", "no_media", "media_stop"} {
		t.Run(mode, func(t *testing.T) {
			source := decodeDiagnostic[whep.Status](t, diagnosticRequest(t, app, "POST", api+"/start", 201, []byte(`{"mode":"`+mode+`"}`)))
			t.Cleanup(func() { app.simulators.whep.Stop() })
			diagnosticRequest(t, app, "POST", source.Endpoint, 415, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
			defer cancel()

			var settings webrtc.SettingEngine
			settings.SetIncludeLoopbackCandidate(true)
			settings.SetIPFilter(net.IP.IsLoopback)
			settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
			receiver, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				t.Fatal(err)
			}
			defer receiver.Close()
			if _, err := receiver.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
				webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
				t.Fatal(err)
			}
			gotRTP := make(chan string, 1)
			receiver.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
				for {
					packet, _, err := track.ReadRTP()
					if err != nil {
						return
					}
					if len(packet.Payload) > 0 {
						select {
						case gotRTP <- track.Codec().MimeType:
						default:
						}
					}
				}
			})

			offer, err := receiver.CreateOffer(nil)
			if err != nil {
				t.Fatal(err)
			}
			gathered := webrtc.GatheringCompletePromise(receiver)
			if err := receiver.SetLocalDescription(offer); err != nil {
				t.Fatal(err)
			}
			select {
			case <-gathered:
			case <-ctx.Done():
				t.Fatal("local receiver did not gather candidates")
			}
			postOffer := func(body string, expected int) (string, string) {
				t.Helper()
				req, err := http.NewRequestWithContext(ctx, "POST", app.URL()+source.Endpoint, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/sdp")
				response, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				raw, err := io.ReadAll(response.Body)
				if err != nil || response.StatusCode != expected {
					t.Fatalf("WHEP offer returned %d: %.400s (%v)", response.StatusCode, raw, err)
				}
				if expected == 201 && response.Header.Get("Content-Type") != "application/sdp" {
					t.Fatal("SDP answer is missing its content type")
				}
				return string(raw), response.Header.Get("Location")
			}
			postOffer("invalid SDP", 400)
			answer, path := postOffer(receiver.LocalDescription().SDP, 201)
			if !strings.HasPrefix(path, api+"/sessions/") || !strings.Contains(answer, "a=ice-lite") ||
				!strings.Contains(answer, "a=sendonly") || !strings.Contains(answer, " UDP ") && !strings.Contains(answer, " udp ") {
				t.Fatalf("bad WHEP session metadata: %s", path)
			}
			if err := receiver.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
				t.Fatal(err)
			}
			get := func() whep.SessionStatus {
				t.Helper()
				return decodeDiagnostic[whep.SessionStatus](t, diagnosticRequest(t, app, "GET", path, 200, nil))
			}
			wait := func(ready func(whep.SessionStatus) bool) whep.SessionStatus {
				t.Helper()
				var status whep.SessionStatus
				for ctx.Err() == nil {
					status = get()
					if ready(status) {
						return status
					}
					time.Sleep(20 * time.Millisecond)
				}
				t.Fatalf("unexpected WHEP observation: %+v", status)
				return status
			}
			connected := func(s whep.SessionStatus) bool { return s.DTLS == "connected" && s.Peer == "connected" }
			switch mode {
			case "ice_timeout":
				status := wait(func(s whep.SessionStatus) bool { return s.ICEPacketsDropped > 0 })
				if connected(status) || status.RTPPackets != 0 || !status.ConnectedAt.IsZero() {
					t.Fatalf("ICE fault progressed to media: %+v", status)
				}
			case "no_media":
				wait(connected)
				select {
				case <-gotRTP:
					t.Fatal("media arrived despite no-media mode")
				case <-time.After(250 * time.Millisecond):
				}
				if status := get(); status.RTPPackets != 0 || !status.FirstRTPAt.IsZero() {
					t.Fatalf("source sent video in no-media mode: %+v", status)
				}
			default:
				select {
				case codec := <-gotRTP:
					if codec != webrtc.MimeTypeH264 {
						t.Fatalf("unexpected video codec: %s", codec)
					}
				case <-ctx.Done():
					t.Fatal("receiver did not receive video RTP")
				}
				if mode == "media_stop" {
					ended := wait(func(s whep.SessionStatus) bool { return !s.MediaStoppedAt.IsZero() })
					time.Sleep(300 * time.Millisecond)
					later := get()
					if !connected(later) || ended.LastRTPAt.After(ended.MediaStoppedAt) ||
						later.RTPPackets != ended.RTPPackets || ended.RTPPackets == 0 {
						t.Fatalf("wrong connected media stall: %+v → %+v", ended, later)
					}
				}
			}
			diagnosticRequest(t, app, "DELETE", path, 204, nil)
			diagnosticRequest(t, app, "GET", path, 404, nil)
			after := decodeDiagnostic[whep.Status](t, diagnosticRequest(t, app, "GET", api, 200, nil))
			if !after.Active || after.Sessions != 0 {
				t.Fatalf("session was not released: %+v", after)
			}
		})
	}
}

func TestWHEPSimulatorSignalingFaultsAndRTPDrop(t *testing.T) {
	app := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/whep"

	signalingCases := []struct {
		mode   string
		status int
	}{
		{mode: "auth_401", status: http.StatusUnauthorized},
		{mode: "not_found_404", status: http.StatusNotFound},
		{mode: "sdp_reject_406", status: http.StatusNotAcceptable},
	}
	for _, tc := range signalingCases {
		t.Run(tc.mode, func(t *testing.T) {
			source := decodeDiagnostic[whep.Status](t, diagnosticRequest(t, app, "POST", api+"/start", 201, []byte(`{"mode":"`+tc.mode+`"}`)))
			t.Cleanup(func() { app.simulators.whep.Stop() })
			req, err := http.NewRequestWithContext(t.Context(), "POST", app.URL()+source.Endpoint, strings.NewReader("v=0\r\n"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/sdp")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("mode %s returned HTTP %d, want %d", tc.mode, resp.StatusCode, tc.status)
			}
			if tc.mode == "auth_401" && !strings.Contains(resp.Header.Get("WWW-Authenticate"), "Bearer") {
				t.Fatalf("auth_401 missing WWW-Authenticate Bearer header: %q", resp.Header.Get("WWW-Authenticate"))
			}
			after := decodeDiagnostic[whep.Status](t, diagnosticRequest(t, app, "GET", api, 200, nil))
			if after.Sessions != 0 {
				t.Fatalf("signaling fault should not create session: %+v", after)
			}
		})
	}

	t.Run("rtp_drop", func(t *testing.T) {
		source := decodeDiagnostic[whep.Status](t, diagnosticRequest(t, app, "POST", api+"/start", 201, []byte(`{"mode":"rtp_drop"}`)))
		t.Cleanup(func() { app.simulators.whep.Stop() })
		ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
		defer cancel()

		var settings webrtc.SettingEngine
		settings.SetIncludeLoopbackCandidate(true)
		settings.SetIPFilter(net.IP.IsLoopback)
		settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
		receiver, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		defer receiver.Close()
		if _, err := receiver.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			t.Fatal(err)
		}
		receiver.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			for {
				if _, _, err := track.ReadRTP(); err != nil {
					return
				}
			}
		})
		offer, err := receiver.CreateOffer(nil)
		if err != nil {
			t.Fatal(err)
		}
		gathered := webrtc.GatheringCompletePromise(receiver)
		if err := receiver.SetLocalDescription(offer); err != nil {
			t.Fatal(err)
		}
		select {
		case <-gathered:
		case <-ctx.Done():
			t.Fatal("local receiver did not gather candidates")
		}
		req, err := http.NewRequestWithContext(ctx, "POST", app.URL()+source.Endpoint, strings.NewReader(receiver.LocalDescription().SDP))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/sdp")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		answerBytes, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("rtp_drop offer returned %d: %v", resp.StatusCode, err)
		}
		path := resp.Header.Get("Location")
		if err := receiver.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(answerBytes)}); err != nil {
			t.Fatal(err)
		}
		var status whep.SessionStatus
		for ctx.Err() == nil {
			status = decodeDiagnostic[whep.SessionStatus](t, diagnosticRequest(t, app, "GET", path, 200, nil))
			if status.RTPPackets > 0 && status.RTPPacketsDropped > 0 {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if status.RTPPackets == 0 || status.RTPPacketsDropped == 0 {
			t.Fatalf("expected both sent and dropped RTP packets in rtp_drop mode, got %+v", status)
		}
		diagnosticRequest(t, app, "DELETE", path, 204, nil)
	})
}
