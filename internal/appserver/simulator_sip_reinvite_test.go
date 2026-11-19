package appserver

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/sipphone"

	"github.com/pion/rtp"
)

func TestSIPPhoneReinviteHoldAndMoveAudio(t *testing.T) {
	pbx := openSimulatorUDP(t)
	oldMedia := openSimulatorUDP(t)
	newMedia := openSimulatorUDP(t)
	server := testDiagnosticServer(t, t.TempDir())
	cfg, err := json.Marshal(sipphone.Config{Host: "127.0.0.1", Port: pbx.LocalAddr().(*net.UDPAddr).Port, User: "2001"})
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, "POST", sipAPI+"/start", http.StatusAccepted, cfg)
	reg, endpoint := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
	sendSimulatorSIP(t, pbx, endpoint, reg.response("200 OK", ""))
	waitSIPPhone(t, server, "registered", "")
	diagnosticRequest(t, server, "POST", sipAPI+"/dial", http.StatusAccepted, []byte(`{"target":"2002"}`))
	initial, _ := receiveSimulatorSIP(t, pbx, "INVITE ", time.Second)
	contact := "Contact: <sip:2002@" + pbx.LocalAddr().String() + ">\r\n"
	sendSimulatorSIP(t, pbx, endpoint, sipTestResponse(initial, "200 OK", contact, sipTestSDP(oldMedia, "0", "PCMU", "")))
	ack, _ := receiveSimulatorSIP(t, pbx, "ACK ", time.Second)
	waitSIPPhone(t, server, "registered", "incall")
	target := simulatorAudioAddress(t, initial.body)
	oldMedia.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, 1500)
	if _, _, err := oldMedia.ReadFromUDP(buffer); err != nil {
		t.Fatal(err)
	}

	localURI := "sip:2001@" + endpoint.String()
	send := func(method, content string, seq int, branch string) {
		t.Helper()
		headers := ""
		if content != "" {
			headers = "Content-Type: application/sdp\r\n"
		}
		wire := fmt.Sprintf("%s %s SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bK%s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %d %s\r\nMax-Forwards: 70\r\n%s%sContent-Length: %d\r\n\r\n%s",
			method, localURI, pbx.LocalAddr(), branch, ack.headers["to"], initial.headers["from"], initial.headers["call-id"], seq, method, contact, headers, len(content), content)
		sendSimulatorSIP(t, pbx, endpoint, wire)
	}
	hold := strings.Replace(sipTestSDP(oldMedia, "0", "PCMU", ""), "a=sendrecv", "a=inactive", 1)
	send("INVITE", hold, 2, "hold")
	answer, _ := receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
	if !strings.Contains(answer.body, "a=inactive") || !strings.Contains(answer.body, "o=- ") {
		t.Fatal(answer.body)
	}
	send("ACK", "", 1, "incorrect-ack")
	if call := waitSIPPhone(t, server, "registered", "incall").Call; call.MediaChanges != 0 {
		t.Fatalf("changed media before valid ACK: %+v", call)
	}
	send("ACK", "", 2, "hold-ack")
	holdStatus := waitSIPPhoneMediaChanges(t, server, 1)
	if holdStatus.MediaDirection != "inactive" {
		t.Fatalf("expected held media: %+v", holdStatus)
	}
	time.Sleep(120 * time.Millisecond)
	if status := waitSIPPhone(t, server, "registered", "incall").Call; status.PacketsSent != holdStatus.PacketsSent {
		t.Fatalf("hold did not stop RTP: %+v", status)
	}
	// Drain pre-hold datagrams before verifying no new media arrives.
	if err := oldMedia.SetReadDeadline(time.Now().Add(60 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	for {
		if _, _, err := oldMedia.ReadFromUDP(buffer); err != nil {
			break
		}
	}

	send("INVITE", sipTestSDP(newMedia, "8", "PCMA", ""), 3, "resume")
	resume, _ := receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
	if !strings.Contains(resume.body, "a=sendrecv") || strings.Contains(resume.body, "a=inactive") {
		t.Fatalf("resume answer did not restore both directions: %s", resume.body)
	}
	send("ACK", "", 3, "resume-ack")
	active := waitSIPPhoneMediaChanges(t, server, 2)
	if active.MediaDirection != "sendrecv" || active.MediaRemote != newMedia.LocalAddr().String() || active.PreviousRemote != oldMedia.LocalAddr().String() || active.Codec != "PCMA" {
		t.Fatalf("wrong resumed RTP endpoints: %+v", active)
	}
	packet := rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 8, SequenceNumber: 1, SSRC: 3000}, Payload: make([]byte, 160)}
	raw, err := packet.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = newMedia.WriteToUDP(raw, target); err != nil {
		t.Fatal(err)
	}
	newMedia.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := newMedia.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	var received rtp.Packet
	if err := received.Unmarshal(buffer[:n]); err != nil || received.PayloadType != 8 {
		t.Fatalf("new remote did not receive PCMA: %+v %v", received.Header, err)
	}
	oldMedia.SetReadDeadline(time.Now().Add(130 * time.Millisecond))
	if _, _, err = oldMedia.ReadFromUDP(buffer); err == nil {
		t.Fatal("old remote still received RTP")
	}
	if status := waitSIPPhone(t, server, "registered", "incall").Call; status.PacketsReceived != 1 {
		t.Fatalf("reverse RTP not received after resume: %+v", status)
	}
	send("BYE", "", 4, "bye")
	receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
	waitSIPPhone(t, server, "registered", "ended")
}

func waitSIPPhoneMediaChanges(t *testing.T, server *Server, changes int) *sipphone.CallStatus {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		call := waitSIPPhone(t, server, "registered", "incall").Call
		if call.MediaChanges == changes {
			return call
		}
		if time.Now().After(deadline) {
			t.Fatalf("SDP change not applied: %+v", call)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
