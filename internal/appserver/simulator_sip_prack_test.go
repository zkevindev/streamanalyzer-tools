package appserver

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/sipphone"
)

func startPRACKPhone(t *testing.T) (*Server, *net.UDPConn, *net.UDPAddr, func()) {
	t.Helper()
	pbx := openSimulatorUDP(t)
	server := testDiagnosticServer(t, t.TempDir())
	cfg, err := json.Marshal(sipphone.Config{
		Host: "127.0.0.1", Port: pbx.LocalAddr().(*net.UDPAddr).Port, User: "2001", EarlyMedia: true, ReliableEarly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/start", http.StatusAccepted, cfg)
	register, endpoint := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
	sendSimulatorSIP(t, pbx, endpoint, register.response("200 OK", ""))
	waitSIPPhone(t, server, "registered", "")
	return server, pbx, endpoint, func() {
		diagnosticRequest(t, server, http.MethodPost, sipAPI+"/stop", http.StatusAccepted, nil)
		stop, _ := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
		sendSimulatorSIP(t, pbx, endpoint, stop.response("200 OK", ""))
		waitSIPPhone(t, server, "stopped", "")
	}
}

func TestSIPPhoneOutboundPRACK(t *testing.T) {
	for _, scenario := range []struct{ state, response string }{
		{"confirmed", "200 OK"}, {"rejected", "481 Call/Transaction Does Not Exist"}, {"timeout", ""},
	} {
		t.Run(scenario.state, func(t *testing.T) {
			t.Parallel()
			server, pbx, endpoint, stop := startPRACKPhone(t)
			defer stop()
			media := openSimulatorUDP(t)
			diagnosticRequest(t, server, http.MethodPost, sipAPI+"/dial", http.StatusAccepted, []byte(`{"target":"2002"}`))
			invite, _ := receiveSimulatorSIP(t, pbx, "INVITE ", time.Second)
			seq, err := strconv.Atoi(strings.Fields(invite.headers["cseq"])[0])
			if err != nil || invite.headers["supported"] != "100rel" {
				t.Fatalf("unexpected INVITE: %+v", invite)
			}
			contact := "Contact: <sip:2002@" + pbx.LocalAddr().String() + ">\r\n"
			extra := contact + "Require: 100rel\r\nRSeq: 42\r\n"
			progress := sipTestResponse(invite, "183 Session Progress", extra, sipTestSDP(media, "0", "PCMU", ""))
			sendSimulatorSIP(t, pbx, endpoint, progress)
			prack, _ := receiveSimulatorSIP(t, pbx, "PRACK ", time.Second)
			if prack.headers["rack"] != fmt.Sprintf("42 %d INVITE", seq) || prack.headers["cseq"] != fmt.Sprintf("%d PRACK", seq+1) ||
				prack.headers["call-id"] != invite.headers["call-id"] || !strings.Contains(prack.headers["to"], ";tag=") {
				t.Fatalf("incorrect PRACK: %+v", prack)
			}
			if scenario.response != "" {
				sendSimulatorSIP(t, pbx, endpoint, prack.response(scenario.response, ""))
			}
			if scenario.state == "confirmed" {
				sendSimulatorSIP(t, pbx, endpoint, progress)
				sendSimulatorSIP(t, pbx, endpoint, invite.response("200 OK", contact))
				ack, _ := receiveSimulatorSIP(t, pbx, "ACK ", time.Second)
				if ack.headers["cseq"] != fmt.Sprintf("%d ACK", seq) {
					t.Fatalf("ACK must use the INVITE sequence: %+v", ack)
				}
				call := waitSIPPhone(t, server, "registered", "incall").Call
				if call.PRACK == nil || call.PRACK.State != scenario.state || call.PRACK.FinalCode != 200 || call.PRACK.ResponseAt.Before(call.PRACK.RequestAt) {
					t.Fatalf("missing PRACK evidence: %+v", call.PRACK)
				}
				diagnosticRequest(t, server, http.MethodPost, sipAPI+"/hangup", http.StatusAccepted, nil)
				bye, _ := receiveSimulatorSIP(t, pbx, "BYE ", time.Second)
				if bye.headers["cseq"] != fmt.Sprintf("%d BYE", seq+2) {
					t.Fatalf("unexpected PRACK / BYE sequence: %+v", bye)
				}
				sendSimulatorSIP(t, pbx, endpoint, bye.response("200 OK", ""))
				waitSIPPhone(t, server, "registered", "ended")
				return
			}
			cancel, _ := receiveSimulatorSIP(t, pbx, "CANCEL ", 6*time.Second)
			if cancel.headers["cseq"] != fmt.Sprintf("%d CANCEL", seq) {
				t.Fatalf("CANCEL must use the INVITE sequence: %+v", cancel)
			}
			sendSimulatorSIP(t, pbx, endpoint, cancel.response("200 OK", ""))
			sendSimulatorSIP(t, pbx, endpoint, invite.response("487 Request Terminated", ""))
			call := waitSIPPhone(t, server, "registered", "failed").Call
			if call.PRACK == nil || call.PRACK.State != scenario.state || scenario.state == "rejected" && call.PRACK.FinalCode != 481 {
				t.Fatalf("missing PRACK failure evidence: %+v", call.PRACK)
			}
		})
	}
}

func TestSIPPhoneInboundPRACKGatesEarlyMedia(t *testing.T) {
	server, pbx, endpoint, stop := startPRACKPhone(t)
	defer stop()
	media := openSimulatorUDP(t)
	localURI := "sip:2001@" + endpoint.String()
	peerURI := "sip:2002@" + pbx.LocalAddr().String()
	peer := "<" + peerURI + ">;tag=peer-prack"
	to := "<" + localURI + ">"
	const id = "inbound-reliable-fixture"
	const branch = "z9hG4bK-invite-reliable"
	body := sipTestSDP(media, "0", "PCMU", "")
	invite := fmt.Sprintf("INVITE %s SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=%s\r\n"+
		"From: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: 4 INVITE\r\nMax-Forwards: 70\r\nContact: <%s>\r\n"+
		"Supported: timer, 100rel\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s",
		localURI, pbx.LocalAddr(), branch, peer, to, id, peerURI, len(body), body)
	sendSimulatorSIP(t, pbx, endpoint, invite)
	progress, _ := receiveSimulatorSIP(t, pbx, "SIP/2.0 183 ", time.Second)
	if progress.headers["rseq"] == "" || progress.headers["require"] != "100rel" || progress.body == "" {
		t.Fatalf("missing reliable early SDP: %+v", progress)
	}
	retransmit, _ := receiveSimulatorSIP(t, pbx, "SIP/2.0 183 ", time.Second)
	if retransmit.headers["rseq"] != progress.headers["rseq"] || retransmit.headers["to"] != progress.headers["to"] {
		t.Fatalf("retransmitted 183 changed dialog: %+v", retransmit)
	}
	call := waitSIPPhone(t, server, "registered", "ringing").Call
	if call.EarlySend.Packets != 0 || call.PRACK == nil || call.PRACK.State != "waiting" {
		t.Fatalf("early media began before PRACK: %+v", call)
	}
	send := func(seq int, rack, transaction string) {
		packet := fmt.Sprintf("PRACK %s SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bK-%s\r\n"+
			"From: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %d PRACK\r\nMax-Forwards: 70\r\n"+
			"RAck: %s\r\nContent-Length: 0\r\n\r\n", localURI, pbx.LocalAddr(), transaction, peer, progress.headers["to"], id, seq, rack)
		sendSimulatorSIP(t, pbx, endpoint, packet)
	}
	send(5, progress.headers["rseq"]+" 3 INVITE", "wrong-rack")
	receiveSimulatorSIP(t, pbx, "SIP/2.0 481 ", time.Second)
	send(6, progress.headers["rseq"]+" 4 INVITE", "right-rack")
	receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
	if err := media.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1500)
	if _, _, err := media.ReadFromUDP(buf); err != nil {
		t.Fatalf("early media did not start after PRACK: %v", err)
	}
	call = waitSIPPhone(t, server, "registered", "ringing").Call
	if call.PRACK == nil || call.PRACK.State != "confirmed" || call.EarlySend.FirstAt.Before(call.PRACK.ResponseAt) {
		t.Fatalf("incorrect PRACK / early RTP timing: %+v", call)
	}
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/answer", http.StatusAccepted, nil)
	answer, _ := receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
	sendSimulatorRequest(t, pbx, endpoint, peer, "ACK", localURI, answer.headers["to"], id, "prack-ack", "", "", 4)
	waitSIPPhone(t, server, "registered", "incall")
	sendSimulatorRequest(t, pbx, endpoint, peer, "BYE", localURI, answer.headers["to"], id, "prack-bye", "", "", 7)
	receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
	waitSIPPhone(t, server, "registered", "ended")
}
