package appserver

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/sipphone"

	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/zaf/g711"
)

const sipAPI = "/api/simulators/sip"

func TestSIPPhoneRegisterIncomingOutgoingAndAudio(t *testing.T) {
	pbx := openSimulatorUDP(t)
	server := testDiagnosticServer(t, t.TempDir())
	const user = "2001"
	const called = "2002"
	const password = "digest-only-secret"
	for _, path := range []string{"/simulators", "/simulators/onvif", "/simulators/sip"} {
		page := diagnosticRequest(t, server, http.MethodGet, path, http.StatusOK, nil)
		if !strings.Contains(string(page), `href="/simulators/sip"`) || path == "/simulators/sip" && !strings.Contains(string(page), "通用 SIP 模拟器") {
			t.Fatalf("missing SIP simulator link or page at %s", path)
		}
	}
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/dial", http.StatusConflict, []byte(`{"target":"2002"}`))
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/start", http.StatusBadRequest, []byte(`{"host":"127.0.0.1","user":"@broken"}`))
	cfg, err := json.Marshal(sipphone.Config{Host: "127.0.0.1", Port: pbx.LocalAddr().(*net.UDPAddr).Port,
		User: user, Domain: "lab.test", Username: "voice-account", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/start", http.StatusAccepted, cfg)
	first, endpoint := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
	if !strings.HasPrefix(first.line, "REGISTER sip:lab.test SIP/2.0") || first.headers["expires"] != "3600" ||
		!strings.Contains(first.headers["to"], "sip:"+user+"@lab.test") ||
		!strings.Contains(first.headers["contact"], "sip:"+user+"@"+endpoint.String()) {
		t.Fatalf("incorrect REGISTER identity/contact: %+v", first)
	}
	challenge := `WWW-Authenticate: Digest realm="lab.test", nonce="fixed-test-nonce", algorithm=MD5, qop="auth"` + "\r\n"
	sendSimulatorSIP(t, pbx, endpoint, first.response("401 Unauthorized", challenge))
	authenticated, _ := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
	if !strings.Contains(authenticated.headers["authorization"], `username="voice-account"`) ||
		authenticated.headers["call-id"] != first.headers["call-id"] || authenticated.headers["cseq"] == first.headers["cseq"] {
		t.Fatalf("incorrect digest retry or REGISTER sequence: %+v", authenticated)
	}
	sendSimulatorSIP(t, pbx, endpoint, authenticated.response("200 OK", "Expires: 600\r\n"))
	status := waitSIPPhone(t, server, "registered", "")
	if status.LocalAddress != endpoint.String() || status.User != user {
		t.Fatalf("unexpected registration in workbench: %+v", status)
	}
	diagnosticRequest(t, server, http.MethodGet, sipAPI+"/received.wav", http.StatusNotFound, nil)
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/start", http.StatusConflict, cfg)

	peerURI := "sip:" + called + "@" + pbx.LocalAddr().String()
	peerContact := "Contact: <" + peerURI + ">\r\n"
	peerIdentity := "<sip:" + called + "@lab.test>;tag=remote-tag"
	localURI := "sip:" + user + "@" + endpoint.String()
	localTo := "<" + localURI + ">"
	dial := func() simulatorSIP {
		t.Helper()
		diagnosticRequest(t, server, http.MethodPost, sipAPI+"/dial", http.StatusAccepted, []byte(`{"target":"2002"}`))
		req, addr := receiveSimulatorSIP(t, pbx, "INVITE ", time.Second)
		if addr.String() != endpoint.String() || !strings.Contains(req.line, "sip:2002@lab.test") || !strings.Contains(req.body, "RTP/AVP 0 8") {
			t.Fatalf("unexpected INVITE: %+v", req)
		}
		return req
	}
	send := func(method, to, callID, branch, media string, seq int) {
		t.Helper()
		contentType := ""
		if media != "" {
			contentType = "application/sdp"
		}
		sendSimulatorRequest(t, pbx, endpoint, peerIdentity, method, localURI, to, callID, branch, contentType, media, seq)
	}

	outgoing := dial()
	proxyChallenge := `Proxy-Authenticate: Digest realm="lab.test", nonce="invite-only-nonce", algorithm=MD5` + "\r\n"
	sendSimulatorSIP(t, pbx, endpoint, outgoing.response("407 Proxy Authentication Required", proxyChallenge))
	receiveSimulatorSIP(t, pbx, "ACK ", time.Second)
	authorizedCall, _ := receiveSimulatorSIP(t, pbx, "INVITE ", time.Second)
	if !strings.Contains(authorizedCall.headers["proxy-authorization"], `username="voice-account"`) ||
		authorizedCall.headers["call-id"] != outgoing.headers["call-id"] {
		t.Fatalf("missing INVITE proxy authorization: %+v", authorizedCall)
	}
	outgoing = authorizedCall
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/dial", http.StatusConflict, []byte(`{"target":"2003"}`))
	sendSimulatorSIP(t, pbx, endpoint, outgoing.response("180 Ringing", peerContact))
	waitSIPPhone(t, server, "registered", "ringing")
	pbxMedia := openSimulatorUDP(t)
	audio := sipTestSDP(pbxMedia, "0", "PCMU", "")
	sendSimulatorSIP(t, pbx, endpoint, sipTestResponse(outgoing, "200 OK", peerContact, audio))
	ack, _ := receiveSimulatorSIP(t, pbx, "ACK ", time.Second)
	if ack.headers["call-id"] != outgoing.headers["call-id"] || !strings.Contains(ack.line, peerURI) {
		t.Fatalf("ACK did not target the remote SIP dialog: %+v", ack)
	}
	status = waitSIPPhone(t, server, "registered", "incall")
	if status.Call.Direction != "outbound" || status.Call.Codec != "PCMU" || status.Call.ID != outgoing.headers["call-id"] {
		t.Fatalf("wrong outbound call state: %+v", status.Call)
	}
	checkSIPAudio(t, server, pbxMedia, outgoing.body, 0, false)

	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/hangup", http.StatusAccepted, nil)
	bye, _ := receiveSimulatorSIP(t, pbx, "BYE ", time.Second)
	if bye.headers["call-id"] != outgoing.headers["call-id"] || !strings.Contains(bye.line, peerURI) {
		t.Fatalf("BYE did not target the outbound dialog: %+v", bye)
	}
	sendSimulatorSIP(t, pbx, endpoint, bye.response("200 OK", ""))
	waitSIPPhone(t, server, "registered", "ended")

	canceled := dial()
	sendSimulatorSIP(t, pbx, endpoint, canceled.response("180 Ringing", peerContact))
	waitSIPPhone(t, server, "registered", "ringing")
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/hangup", http.StatusAccepted, nil)
	cancel, _ := receiveSimulatorSIP(t, pbx, "CANCEL ", time.Second)
	if cancel.headers["call-id"] != canceled.headers["call-id"] ||
		strings.Fields(cancel.headers["cseq"])[0] != strings.Fields(canceled.headers["cseq"])[0] {
		t.Fatalf("CANCEL does not match INVITE: %+v", cancel)
	}
	sendSimulatorSIP(t, pbx, endpoint, cancel.response("200 OK", ""))
	sendSimulatorSIP(t, pbx, endpoint, canceled.response("487 Request Terminated", ""))
	receiveSimulatorSIP(t, pbx, "ACK ", time.Second)
	waitSIPPhone(t, server, "registered", "ended")

	// A video offer preceding a dynamic G.711 payload keeps its SDP position.
	incomingMedia := openSimulatorUDP(t)
	mixed := sipTestSDP(incomingMedia, "96", "PCMA", "m=video 32000 RTP/AVP 99\r\na=rtpmap:99 H264/90000\r\n")
	sendInvite := func(id string) {
		t.Helper()
		packet := fmt.Sprintf("INVITE %s SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bK-%s\r\n"+
			"From: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: 1 INVITE\r\nMax-Forwards: 70\r\n%s"+
			"Content-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", localURI, pbx.LocalAddr(), id, peerIdentity, localTo, id, peerContact, len(mixed), mixed)
		sendSimulatorSIP(t, pbx, endpoint, packet)
	}
	sendInvite("remote-first")
	ringing, _ := receiveSimulatorSIP(t, pbx, "SIP/2.0 180 ", time.Second)
	status = waitSIPPhone(t, server, "registered", "ringing")
	if status.Call.Direction != "inbound" || ringing.headers["call-id"] != status.Call.ID {
		t.Fatalf("wrong inbound call state: %+v", status.Call)
	}
	sendInvite("remote-busy")
	receiveSimulatorSIP(t, pbx, "SIP/2.0 486 ", time.Second)
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/answer", http.StatusAccepted, nil)
	answer, _ := receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
	if answer.headers["to"] != ringing.headers["to"] || !strings.Contains(answer.body, "m=video 0 RTP/AVP 99\r\nm=audio ") ||
		!strings.Contains(answer.body, "a=rtpmap:96 PCMA/8000") {
		t.Fatalf("wrong incoming SDP answer: %+v", answer)
	}
	send("ACK", answer.headers["to"], "remote-first", "remote-ack", "", 1)
	waitSIPPhone(t, server, "registered", "incall")
	checkSIPAudio(t, server, incomingMedia, answer.body, 96, true)
	send("BYE", localTo+";tag=wrong-dialog", "remote-first", "wrong-bye", "", 2)
	receiveSimulatorSIP(t, pbx, "SIP/2.0 481 ", time.Second)
	waitSIPPhone(t, server, "registered", "incall")
	send("BYE", answer.headers["to"], "remote-first", "remote-bye", "", 3)
	receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
	waitSIPPhone(t, server, "registered", "ended")

	sendInvite("remote-local-bye")
	receiveSimulatorSIP(t, pbx, "SIP/2.0 180 ", time.Second)
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/answer", http.StatusAccepted, nil)
	answer, _ = receiveSimulatorSIP(t, pbx, "SIP/2.0 200 ", time.Second)
	send("ACK", answer.headers["to"], "remote-local-bye", "-remote-local-bye", "", 1)
	waitSIPPhone(t, server, "registered", "incall")
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/hangup", http.StatusAccepted, nil)
	localBye, _ := receiveSimulatorSIP(t, pbx, "BYE ", time.Second)
	if localBye.headers["call-id"] != "remote-local-bye" || !strings.Contains(localBye.line, peerURI) {
		t.Fatalf("BYE did not target the inbound dialog: %+v", localBye)
	}
	sendSimulatorSIP(t, pbx, endpoint, localBye.response("200 OK", ""))
	waitSIPPhone(t, server, "registered", "ended")

	sendInvite("remote-cancel")
	receiveSimulatorSIP(t, pbx, "SIP/2.0 180 ", time.Second)
	send("CANCEL", localTo, "remote-cancel", "-remote-cancel", "", 1)
	replies := map[string]bool{}
	for range 2 {
		response, _ := receiveSimulatorSIP(t, pbx, "SIP/2.0 ", time.Second)
		replies[response.line] = true
	}
	if !replies["SIP/2.0 200 OK"] || !replies["SIP/2.0 487 Request Terminated"] {
		t.Fatalf("expected CANCEL acknowledgment and INVITE termination: %v", replies)
	}
	waitSIPPhone(t, server, "registered", "ended")

	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/stop", http.StatusAccepted, nil)
	unregister, _ := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
	if unregister.headers["expires"] != "0" || unregister.headers["call-id"] != first.headers["call-id"] {
		t.Fatalf("stop did not remove the registered Contact: %+v", unregister)
	}
	sendSimulatorSIP(t, pbx, endpoint, unregister.response("200 OK", ""))
	stopped := waitSIPPhone(t, server, "stopped", "ended")
	if stopped.Active {
		t.Fatalf("simulator still active: %+v", stopped)
	}
	output := diagnosticRequest(t, server, http.MethodGet, sipAPI, http.StatusOK, nil)
	if strings.Contains(string(output), password) || strings.Contains(string(output), "nonce=") {
		t.Fatal("SIP credentials leaked in status/events")
	}
}

func TestSIPPhoneRenewsRegisterContact(t *testing.T) {
	pbx := openSimulatorUDP(t)
	server := testDiagnosticServer(t, t.TempDir())
	cfg, err := json.Marshal(sipphone.Config{Host: "127.0.0.1", Port: pbx.LocalAddr().(*net.UDPAddr).Port, User: "1234"})
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/start", http.StatusAccepted, cfg)
	first, endpoint := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
	shortLease := "Contact: <sip:1234@127.0.0.1:5099>;expires=600\r\nContact: " +
		first.headers["contact"] + ";expires=2\r\n"
	sendSimulatorSIP(t, pbx, endpoint, first.response("200 OK", shortLease))
	waitSIPPhone(t, server, "registered", "")
	retry, addr := receiveSimulatorSIP(t, pbx, "REGISTER ", 3*time.Second)
	if retry.headers["call-id"] != first.headers["call-id"] || retry.headers["cseq"] == first.headers["cseq"] ||
		retry.headers["expires"] != "2" || addr.String() != endpoint.String() {
		t.Fatalf("invalid REGISTER refresh: %+v from %s", retry, addr)
	}
	sendSimulatorSIP(t, pbx, endpoint, retry.response("200 OK", "Expires: 600\r\n"))
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/stop", http.StatusAccepted, nil)
	unregister, _ := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
	if unregister.headers["expires"] != "0" {
		t.Fatal("expected unregister")
	}
	sendSimulatorSIP(t, pbx, endpoint, unregister.response("200 OK", ""))
	waitSIPPhone(t, server, "stopped", "")
}

func waitSIPPhone(t *testing.T, server *Server, state, call string) sipphone.Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		status := decodeDiagnostic[sipphone.Status](t, diagnosticRequest(t, server, http.MethodGet, sipAPI, http.StatusOK, nil))
		if status.State == state && (call == "" || status.Call != nil && status.Call.State == call) {
			return status
		}
		if status.State == "failed" || time.Now().After(deadline) {
			t.Fatalf("waiting for SIP %s/%s got %+v call %+v", state, call, status, status.Call)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func sipTestSDP(media *net.UDPConn, pt, codec, extra string) string {
	return fmt.Sprintf("v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=PBX\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n"+
		"%sm=audio %d RTP/AVP %s\r\na=rtpmap:%s %s/8000\r\na=sendrecv\r\n",
		extra, media.LocalAddr().(*net.UDPAddr).Port, pt, pt, codec)
}

func sipTestResponse(req simulatorSIP, code, headers, body string) string {
	response := req.response(code, headers+"Content-Type: application/sdp\r\n")
	return strings.Replace(response, "Content-Length: 0\r\n\r\n", fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body), 1)
}

func checkSIPAudio(t *testing.T, server *Server, pbx *net.UDPConn, localSDP string, pt uint8, alaw bool) {
	t.Helper()
	var session sdp.SessionDescription
	if err := session.Unmarshal([]byte(localSDP)); err != nil {
		t.Fatal(err)
	}
	var target *net.UDPAddr
	for _, media := range session.MediaDescriptions {
		if media.MediaName.Media == "audio" {
			target = &net.UDPAddr{IP: net.ParseIP(session.ConnectionInformation.Address.Address), Port: media.MediaName.Port.Value}
			break
		}
	}
	if target == nil {
		t.Fatal("missing audio in SDP")
	}
	encode, decode := g711.EncodeUlawFrame, g711.DecodeUlawFrame
	if alaw {
		encode, decode = g711.EncodeAlawFrame, g711.DecodeAlawFrame
	}
	payload := make([]byte, 160)
	for i := range payload {
		payload[i] = encode(int16(i*22 - 1500))
	}
	packet := rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: pt, SequenceNumber: 1, SSRC: 100}, Payload: payload}
	data, err := packet.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pbx.WriteToUDP(data, target); err != nil {
		t.Fatal(err)
	}
	pbx.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1500)
	var previous uint32
	for i := range 2 {
		n, addr, err := pbx.ReadFromUDP(buf)
		if err != nil {
			t.Fatal(err)
		}
		var sent rtp.Packet
		if err := sent.Unmarshal(buf[:n]); err != nil {
			t.Fatal(err)
		}
		if addr.String() != target.String() || sent.PayloadType != pt || len(sent.Payload) != 160 ||
			decode(sent.Payload[1]) == decode(sent.Payload[3]) || i > 0 && sent.Timestamp-previous != 160 {
			t.Fatalf("unexpected G.711 packet from %s: %+v", addr, sent.Header)
		}
		previous = sent.Timestamp
	}
	deadline := time.Now().Add(time.Second)
	for {
		status := waitSIPPhone(t, server, "registered", "incall")
		if status.Call.PacketsReceived > 0 && status.Call.PacketsSent >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("workbench did not see bidirectional RTP")
		}
		time.Sleep(10 * time.Millisecond)
	}
	wav := diagnosticRequest(t, server, http.MethodGet, sipAPI+"/received.wav", http.StatusOK, nil)
	if len(wav) != 44+2*len(payload) || string(wav[:4]) != "RIFF" || binary.LittleEndian.Uint32(wav[24:28]) != 8000 ||
		int16(binary.LittleEndian.Uint16(wav[44:46])) != decode(payload[0]) {
		t.Fatalf("recorded WAV does not contain decoded received G.711 (%d bytes)", len(wav))
	}
}
