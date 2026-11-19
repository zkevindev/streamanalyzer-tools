package appserver

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/sipphone"
)

func TestSIPSessionTimerUpdateAndExpiry(t *testing.T) {
	for _, mode := range []string{"normal", "reject", "timeout", "local"} {
		t.Run(mode, func(t *testing.T) {
			pbx := openSimulatorUDP(t)
			audio := openSimulatorUDP(t)
			server := testDiagnosticServer(t, t.TempDir())
			configMode := mode
			role, side := "uas", "peer"
			if mode == "local" {
				configMode, role, side = "normal", "uac", "local"
			}
			cfg, err := json.Marshal(sipphone.Config{Host: "127.0.0.1", Port: pbx.LocalAddr().(*net.UDPAddr).Port,
				User: "2001", SessionTimer: configMode, SessionSeconds: 2})
			if err != nil {
				t.Fatal(err)
			}
			diagnosticRequest(t, server, "POST", sipAPI+"/start", http.StatusAccepted, cfg)
			register, endpoint := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
			sendSimulatorSIP(t, pbx, endpoint, register.response("200 OK", ""))
			waitSIPPhone(t, server, "registered", "")
			diagnosticRequest(t, server, "POST", sipAPI+"/dial", http.StatusAccepted, []byte(`{"target":"2002"}`))
			invite, _ := receiveSimulatorSIP(t, pbx, "INVITE ", time.Second)
			if invite.headers["min-se"] != "2" || invite.headers["session-expires"] != "2;refresher=uas" {
				t.Fatalf("missing session negotiation: %+v", invite.headers)
			}
			head := "Contact: <sip:2002@" + pbx.LocalAddr().String() + ">\r\nSession-Expires: 2;refresher=" + role + "\r\n"
			sendSimulatorSIP(t, pbx, endpoint, sipTestResponse(invite, "200 OK", head, sipTestSDP(audio, "0", "PCMU", "")))
			ack, _ := receiveSimulatorSIP(t, pbx, "ACK ", time.Second)
			initial := waitSIPPhone(t, server, "registered", "incall").Call.SessionTimer
			if initial == nil || initial.Refresher != side {
				t.Fatalf("unexpected session: %+v", initial)
			}
			if err := audio.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, _, err := audio.ReadFromUDP(make([]byte, 2048)); err != nil {
				t.Fatal(err)
			}
			if mode == "local" {
				refresh, _ := receiveSimulatorSIP(t, pbx, "UPDATE ", 1500*time.Millisecond)
				if refresh.headers["session-expires"] != "2;refresher=uac" || refresh.headers["call-id"] != invite.headers["call-id"] {
					t.Fatalf("wrong refresh dialog: %+v", refresh.headers)
				}
				sendSimulatorSIP(t, pbx, endpoint, sipTestResponse(refresh, "200 OK", "Session-Expires: 2;refresher=uac\r\n", ""))
				wait := time.Now().Add(500 * time.Millisecond)
				for {
					state := waitSIPPhone(t, server, "registered", "incall").Call.SessionTimer
					if state.State == "refreshed" && state.Deadline.After(initial.Deadline) {
						break
					}
					if time.Now().After(wait) {
						t.Fatalf("local refresh did not renew session: %+v", state)
					}
					time.Sleep(20 * time.Millisecond)
				}
				diagnosticRequest(t, server, "POST", sipAPI+"/hangup", http.StatusAccepted, nil)
				bye, _ := receiveSimulatorSIP(t, pbx, "BYE ", time.Second)
				sendSimulatorSIP(t, pbx, endpoint, bye.response("200 OK", ""))
				return
			}
			time.Sleep(150 * time.Millisecond)
			update := fmt.Sprintf("UPDATE sip:2001@%s SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bKsession-refresh\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: 2 UPDATE\r\nMax-Forwards: 70\r\nSession-Expires: 2;refresher=uac\r\nContent-Length: 0\r\n\r\n",
				endpoint, pbx.LocalAddr(), ack.headers["to"], invite.headers["from"], invite.headers["call-id"])
			sendSimulatorSIP(t, pbx, endpoint, update)
			switch mode {
			case "normal":
				receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
				state := waitSIPPhone(t, server, "registered", "incall").Call.SessionTimer
				if state.State != "refreshed" || !state.Deadline.After(initial.Deadline) {
					t.Fatalf("timer not renewed: %+v", state)
				}
				diagnosticRequest(t, server, "POST", sipAPI+"/hangup", http.StatusAccepted, nil)
			case "reject":
				receiveSimulatorSIP(t, pbx, "SIP/2.0 488 ", time.Second)
			}
			bye, _ := receiveSimulatorSIP(t, pbx, "BYE ", 3*time.Second)
			sendSimulatorSIP(t, pbx, endpoint, bye.response("200 OK", ""))
			if mode == "normal" {
				return
			}
			state := waitSIPPhone(t, server, "registered", "failed").Call
			if !state.SessionTimer.Expired || state.PacketsSent == 0 ||
				(mode == "reject" && state.SessionTimer.State != "refresh_rejected") ||
				(mode == "timeout" && state.SessionTimer.State != "refresh_unanswered") {
				t.Fatalf("expiry evidence missing: %+v %+v", state, state.SessionTimer)
			}
		})
	}
}
