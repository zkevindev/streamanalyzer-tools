package appserver

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/gb28181"
)

func TestGB28181SimulatorWorkflow(t *testing.T) {
	clearDiagnosticModelEnv(t)
	platform := openSimulatorUDP(t)
	media := openSimulatorUDP(t)
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/gb28181"
	const user = "34020000001320000001"
	const channel = "34020000001320000002"
	const platformID = "34020000002000000001"
	const password = "test-password"
	const realm = "3402000000"
	const nonce = "test-nonce"

	page := diagnosticRequest(t, server, http.MethodGet, "/simulators", http.StatusOK, nil)
	if !bytes.Contains(page, []byte("GB28181 模拟器")) {
		t.Fatal("missing simulator page")
	}
	diagnosticRequest(t, server, http.MethodGet, api+"/trace.json", http.StatusNotFound, nil)
	body, err := json.Marshal(gb28181.Config{
		PlatformHost: "127.0.0.1", PlatformPort: platform.LocalAddr().(*net.UDPAddr).Port,
		PlatformID: platformID, DeviceID: user, ChannelID: channel, Password: password, Heartbeat: 5, CaptureRTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, api+"/start", http.StatusBadRequest, []byte("{}"))
	diagnosticRequest(t, server, http.MethodPost, api+"/start", http.StatusAccepted, body)
	diagnosticRequest(t, server, http.MethodPost, api+"/start", http.StatusConflict, body)

	register, deviceAddr := receiveSimulatorSIP(t, platform, "REGISTER ", 2*time.Second)
	challenge := "Digest realm=" + fmt.Sprintf("%q", realm) + ", nonce=" + fmt.Sprintf("%q", nonce) + ", algorithm=MD5"
	sendSimulatorSIP(t, platform, deviceAddr, register.response("401 Unauthorized", "WWW-Authenticate: "+challenge+"\r\n"))
	authenticated, authAddr := receiveSimulatorSIP(t, platform, "REGISTER ", 2*time.Second)
	if authAddr.String() != deviceAddr.String() || authenticated.headers["call-id"] != register.headers["call-id"] ||
		authenticated.headers["cseq"] == register.headers["cseq"] || authenticated.headers["via"] == register.headers["via"] {
		t.Fatalf("invalid digest retry: %+v", authenticated.headers)
	}
	uri := strings.Fields(authenticated.line)[1]
	a1 := md5.Sum([]byte(user + ":" + realm + ":" + password))
	a2 := md5.Sum([]byte("REGISTER:" + uri))
	digest := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%x:%s:%x", a1, nonce, a2))))
	if !strings.Contains(authenticated.headers["authorization"], "response=\""+digest+"\"") {
		t.Fatal("REGISTER did not authenticate using the platform's challenge")
	}
	sendSimulatorSIP(t, platform, deviceAddr, authenticated.response("200 OK", "Expires: 600\r\n"))

	from := "<sip:" + platformID + "@3402000000>;tag=mock-platform"
	deviceURI := "sip:" + user + "@" + deviceAddr.String()
	channelURI := "sip:" + channel + "@" + deviceAddr.String()
	send := func(method, uri, to, callID, branch, contentType, payload string, sequence int) {
		t.Helper()
		sendSimulatorRequest(t, platform, deviceAddr, from, method, uri, to, callID, branch, contentType, payload, sequence)
	}

	query := `<?xml version="1.0" encoding="GB2312"?><Query><CmdType>Catalog</CmdType><SN>42</SN><DeviceID>` + user + "</DeviceID></Query>"
	send("MESSAGE", deviceURI, "<"+deviceURI+">", "directory", "directory", "Application/MANSCDP+xml", query, 1)
	receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
	catalog, _ := receiveSimulatorSIP(t, platform, "MESSAGE ", time.Second)
	if !strings.Contains(catalog.body, "<SN>42</SN>") || !strings.Contains(catalog.body, "<Item><DeviceID>"+channel+"</DeviceID>") {
		t.Fatalf("wrong Catalog response: %s", catalog.body)
	}
	sendSimulatorSIP(t, platform, deviceAddr, catalog.response("200 OK", ""))
	waitSimulator(t, server, api, "registered")

	offer := fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 127.0.0.1\r\ns=Play\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=video %d RTP/AVP 97 96\r\na=rtpmap:97 PS/90000\r\na=recvonly\r\ny=0100000001\r\n",
		platformID, media.LocalAddr().(*net.UDPAddr).Port)
	send("INVITE", channelURI, "<"+channelURI+">", "preview", "invite", "application/sdp", offer, 1)
	inviteOK, _ := receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
	if !strings.Contains(inviteOK.body, "a=sendonly") || !strings.Contains(inviteOK.body, "a=rtpmap:97 PS/90000") ||
		!strings.Contains(inviteOK.body, "y=0100000001") {
		t.Fatalf("unexpected SDP answer: %s", inviteOK.body)
	}

	packet := make([]byte, 1500)
	media.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, err := media.ReadFromUDP(packet); err == nil {
		t.Fatal("device sent media before ACK")
	}
	retryOK, _ := receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
	if retryOK.headers["to"] != inviteOK.headers["to"] || retryOK.body != inviteOK.body {
		t.Fatal("INVITE retransmission changed the dialog tag or advertised media")
	}
	send("ACK", channelURI, inviteOK.headers["to"], "preview", "ack", "", "", 1)
	media.SetReadDeadline(time.Now().Add(time.Second))
	var frame []byte
	var sequence uint16
	var stamp uint32
	for range 12 {
		n, _, err := media.ReadFromUDP(packet)
		if err != nil {
			t.Fatalf("receive RTP: %v", err)
		}
		if n < 12 || packet[0] != 0x80 || packet[1]&0x7f != 97 || binary.BigEndian.Uint32(packet[8:12]) != 100000001 {
			t.Fatalf("unexpected RTP header: %x", packet[:min(n, 12)])
		}
		if len(frame) != 0 && (binary.BigEndian.Uint16(packet[2:4]) != sequence+1 || binary.BigEndian.Uint32(packet[4:8]) != stamp) {
			t.Fatal("non-contiguous RTP fragments within the first frame")
		}
		sequence, stamp = binary.BigEndian.Uint16(packet[2:4]), binary.BigEndian.Uint32(packet[4:8])
		frame = append(frame, packet[12:n]...)
		if packet[1]&0x80 != 0 {
			break
		}
	}
	if packet[1]&0x80 == 0 || !bytes.HasPrefix(frame, []byte{0, 0, 1, 0xba}) ||
		!bytes.Contains(frame, []byte{0, 0, 1, 0xbc}) || !bytes.Contains(frame, []byte{0, 0, 1, 0xe0}) {
		t.Fatal("missing PS pack, stream map, or video PES in the first frame")
	}
	status := waitSimulator(t, server, api, "registered")
	if status.MediaState != "streaming" || status.RTPPackets == 0 {
		t.Fatalf("media status did not reflect RTP: %+v", status)
	}
	send("BYE", channelURI, inviteOK.headers["to"], "preview", "bye", "", "", 2)
	receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
	status = waitSimulator(t, server, api, "registered")
	if status.MediaState != "idle" {
		t.Fatal("BYE did not end the media session")
	}

	keepalive, _ := receiveSimulatorSIP(t, platform, "MESSAGE ", 7*time.Second)
	if !strings.Contains(keepalive.body, "<CmdType>Keepalive</CmdType>") {
		t.Fatalf("expected keepalive, got %s", keepalive.body)
	}
	sendSimulatorSIP(t, platform, deviceAddr, keepalive.response("200 OK", ""))

	diagnosticRequest(t, server, http.MethodPost, api+"/stop", http.StatusAccepted, nil)
	unregister, _ := receiveSimulatorSIP(t, platform, "REGISTER ", time.Second)
	if unregister.headers["expires"] != "0" {
		t.Fatal("stop did not send a SIP unregister")
	}
	sendSimulatorSIP(t, platform, deviceAddr, unregister.response("200 OK", ""))
	status = waitSimulator(t, server, api, "stopped")
	if len(status.Sessions) != 1 || status.Sessions[0].CaptureBytes == 0 {
		t.Fatalf("missing SIP session evidence: %+v", status.Sessions)
	}
	session := status.Sessions[0]
	for _, stage := range []string{"invite", "invite_ok", "ack", "media_connected", "first_rtp", "first_keyframe", "bye", "ended"} {
		found := false
		for _, mark := range session.Timeline {
			if mark.Stage == stage {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s in %+v", stage, session.Timeline)
		}
	}
	archive := diagnosticRequest(t, server, http.MethodGet, api+"/sessions/"+session.ID+"/evidence.zip", http.StatusOK, nil)
	review := decodeDiagnostic[gb28181.EvidenceReview](t, diagnosticRequest(t, server, http.MethodPost, api+"/evidence/import", http.StatusOK, archive))
	if review.Session.CallID != session.CallID || review.RTP == nil || review.RTP.VideoUnits == 0 ||
		review.RTP.VideoType != 0x1b || review.RTP.SequenceGaps != 0 {
		t.Fatalf("unexpected offline GB28181 review: %+v", review)
	}
	diagnosticRequest(t, server, http.MethodPost, api+"/evidence/import", http.StatusBadRequest, []byte("invalid zip"))

	trace := diagnosticRequest(t, server, http.MethodGet, api+"/trace.json", http.StatusOK, nil)
	export := decodeDiagnostic[gb28181.Status](t, trace)
	if status.Active || export.State != "stopped" || len(export.Events) != len(status.Events) ||
		!bytes.Contains(trace, []byte("Authorization: [redacted]")) ||
		bytes.Contains(trace, []byte(password)) || bytes.Contains(trace, []byte(digest)) {
		t.Fatal("incomplete shutdown or incorrect redaction")
	}
}

func openSimulatorUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

type simulatorSIP struct {
	line    string
	headers map[string]string
	body    string
}

func receiveSimulatorSIP(t *testing.T, conn *net.UDPConn, prefix string, timeout time.Duration) (simulatorSIP, *net.UDPAddr) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	p := make([]byte, 8192)
	for {
		n, addr, err := conn.ReadFromUDP(p)
		if err != nil {
			t.Fatalf("receive %s: %v", prefix, err)
		}
		head, body, _ := strings.Cut(string(p[:n]), "\r\n\r\n")
		lines := strings.Split(head, "\r\n")
		if !strings.HasPrefix(lines[0], prefix) {
			continue
		}
		msg := simulatorSIP{line: lines[0], headers: make(map[string]string), body: body}
		for _, line := range lines[1:] {
			name, value, _ := strings.Cut(line, ":")
			msg.headers[strings.ToLower(name)] = strings.TrimSpace(value)
		}
		return msg, addr
	}
}

func (s simulatorSIP) response(status, extraHeaders string) string {
	return fmt.Sprintf("SIP/2.0 %s\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=mock-platform\r\nCall-ID: %s\r\nCSeq: %s\r\n%sContent-Length: 0\r\n\r\n",
		status, s.headers["via"], s.headers["from"], s.headers["to"], s.headers["call-id"], s.headers["cseq"], extraHeaders)
}

func sendSimulatorRequest(t *testing.T, platform *net.UDPConn, deviceAddr *net.UDPAddr, from, method, uri, to, callID, branch, contentType, payload string, sequence int) {
	t.Helper()
	packet := fmt.Sprintf("%s %s SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bK%s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %d %s\r\nMax-Forwards: 70\r\n",
		method, uri, platform.LocalAddr(), branch, from, to, callID, sequence, method)
	if contentType != "" {
		packet += "Content-Type: " + contentType + "\r\n"
	}
	packet += fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
	sendSimulatorSIP(t, platform, deviceAddr, packet)
}

func sendSimulatorSIP(t *testing.T, conn *net.UDPConn, addr *net.UDPAddr, packet string) {
	t.Helper()
	if _, err := conn.WriteToUDP([]byte(packet), addr); err != nil {
		t.Fatal(err)
	}
}

func waitSimulator(t *testing.T, server *Server, api, state string) gb28181.Status {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		status := decodeDiagnostic[gb28181.Status](t, diagnosticRequest(t, server, http.MethodGet, api, http.StatusOK, nil))
		if status.State == state && (state != "stopped" || !status.Active) {
			return status
		}
		if status.State == "failed" || time.Now().After(deadline) {
			t.Fatalf("waiting for %s, simulator returned %+v", state, status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
