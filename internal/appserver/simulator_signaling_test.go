package appserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/gb28181"
)

func TestGB28181SimulatorTCPSignaling(t *testing.T) {
	clearDiagnosticModelEnv(t)
	platform := openSimulatorTCP(t)
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/gb28181"
	const deviceID = "34020000001320000001"
	const channelID = "34020000001320000002"
	const platformID = "34020000002000000001"
	const password = "test-tcp-sip-key"

	cfg := gb28181.Config{
		PlatformHost: "127.0.0.1", PlatformPort: platform.Addr().(*net.TCPAddr).Port,
		PlatformID: platformID, DeviceID: deviceID, ChannelID: channelID, Password: password,
		SIPTransport: "TCP", Heartbeat: 5,
	}
	wrong := cfg
	wrong.SIPTransport = "unknown"
	raw, err := json.Marshal(wrong)
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, api+"/start", http.StatusBadRequest, raw)
	raw, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, api+"/start", http.StatusAccepted, raw)

	accept := func() *net.TCPConn {
		t.Helper()
		platform.SetDeadline(time.Now().Add(4 * time.Second))
		conn, err := platform.AcceptTCP()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	conn := accept()
	reader := bufio.NewReader(conn)
	register := readSIPStream(t, conn, reader, "REGISTER ", 2*time.Second)
	if !strings.HasPrefix(register.headers["via"], "SIP/2.0/TCP "+conn.RemoteAddr().String()) ||
		!strings.Contains(register.headers["contact"], "@"+conn.RemoteAddr().String()+";transport=tcp>") ||
		register.headers["expires"] != "3600" {
		t.Fatalf("TCP registration advertised the wrong socket: %+v", register)
	}
	sendSIPStream(t, conn, register.response("401 Unauthorized",
		"WWW-Authenticate: Digest realm=\"3402000000\", nonce=\"test-tcp\", algorithm=MD5\r\n"))
	auth := readSIPStream(t, conn, reader, "REGISTER ", time.Second)
	if !strings.HasPrefix(auth.headers["authorization"], "Digest ") ||
		auth.headers["call-id"] != register.headers["call-id"] ||
		auth.headers["cseq"] == register.headers["cseq"] {
		t.Fatalf("invalid TCP Digest retry: %+v", auth)
	}

	request := func(method, recipient, to, callID, branch, contentType, body string, cseq int) string {
		if to == "" {
			to = "<" + recipient + ">"
		}
		packet := fmt.Sprintf("%s %s SIP/2.0\r\nVia: SIP/2.0/TCP %s;branch=z9hG4bK%s\r\n"+
			"From: <sip:%s@3402000000>;tag=mock-platform\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %d %s\r\nMax-Forwards: 70\r\n",
			method, recipient, platform.Addr(), branch, platformID, to, callID, cseq, method)
		if contentType != "" {
			packet += "Content-Type: " + contentType + "\r\n"
		}
		return packet + fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	deviceURI := "sip:" + deviceID + "@" + conn.RemoteAddr().String() + ";transport=tcp"
	query := "<Query><CmdType>Catalog</CmdType><SN>72</SN><DeviceID>" + deviceID + "</DeviceID><Note>目录</Note></Query>"
	catalog := request("MESSAGE", deviceURI, "", "tcp-catalog", "catalog", "Application/MANSCDP+xml", query, 1)
	// A second SIP message begins in the same write as the REGISTER response,
	// and ends in a separate write, including a split inside the UTF-8 body.
	split := strings.Index(catalog, "目录") + 1
	sendSIPStream(t, conn, auth.response("200 OK", "Expires: 600\r\n")+catalog[:split])
	time.Sleep(20 * time.Millisecond)
	sendSIPStream(t, conn, catalog[split:])
	catalogOK := readSIPStream(t, conn, reader, "SIP/2.0 200 ", time.Second)
	items := readSIPStream(t, conn, reader, "MESSAGE ", time.Second)
	if catalogOK.headers["call-id"] != "tcp-catalog" || !strings.Contains(items.body, "<SN>72</SN>") ||
		!strings.Contains(items.body, "<DeviceID>"+channelID+"</DeviceID>") {
		t.Fatalf("unexpected Catalog acknowledgement/response: %+v %+v", catalogOK, items)
	}
	sendSIPStream(t, conn, items.response("200 OK", ""))

	heartbeat := readSIPStream(t, conn, reader, "MESSAGE ", 7*time.Second)
	if !strings.HasPrefix(heartbeat.headers["via"], "SIP/2.0/TCP ") ||
		!strings.Contains(heartbeat.body, "<CmdType>Keepalive</CmdType>") {
		t.Fatalf("invalid TCP heartbeat: %+v", heartbeat)
	}
	sendSIPStream(t, conn, heartbeat.response("200 OK", ""))

	preview := func(callID string) (string, simulatorSIP) {
		t.Helper()
		media := openSimulatorUDP(t)
		channel := "sip:" + channelID + "@" + conn.RemoteAddr().String() + ";transport=tcp"
		offer := fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 127.0.0.1\r\ns=Play\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n"+
			"m=video %d RTP/AVP 96\r\na=rtpmap:96 PS/90000\r\ny=0100000001\r\n",
			platformID, media.LocalAddr().(*net.UDPAddr).Port)
		sendSIPStream(t, conn, request("INVITE", channel, "", callID, callID, "application/sdp", offer, 1))
		answer := readSIPStream(t, conn, reader, "SIP/2.0 200 ", time.Second)
		if answer.headers["call-id"] != callID || !strings.Contains(answer.headers["contact"], ";transport=tcp>") ||
			!strings.Contains(answer.body, "RTP/AVP 96\r\n") || strings.Contains(answer.body, "TCP/RTP") {
			t.Fatalf("expected TCP signaling with UDP media: %+v", answer)
		}
		media.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		packet := make([]byte, 1500)
		if _, _, err := media.ReadFromUDP(packet); err == nil {
			t.Fatal("received media before TCP ACK")
		}
		// The first dialog uses the standard ACK branch. The second exercises
		// platforms that send their ACK with the original INVITE branch.
		branch := callID + "-ack"
		if callID == "preview-drop" {
			branch = callID
		}
		sendSIPStream(t, conn, request("ACK", channel, answer.headers["to"], callID, branch, "", "", 1))
		media.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := media.ReadFromUDP(packet)
		if err != nil || n < 16 || !bytes.HasPrefix(packet[12:n], []byte{0, 0, 1, 0xba}) {
			t.Fatalf("missing UDP PS/RTP after TCP ACK: %d bytes, %v", n, err)
		}
		status := waitSimulatorMedia(t, server, api, "streaming", time.Second)
		if status.SIPTransport != "tcp" || status.MediaTransport != "udp" || status.LocalAddress != conn.RemoteAddr().String() {
			t.Fatalf("incorrect workbench transport status: %+v", status)
		}
		return channel, answer
	}
	channel, answer := preview("preview-bye")
	sendSIPStream(t, conn, request("BYE", channel, answer.headers["to"], "preview-bye", "bye", "", "", 2))
	bye := readSIPStream(t, conn, reader, "SIP/2.0 200 ", time.Second)
	if bye.headers["cseq"] != "2 BYE" {
		t.Fatalf("invalid TCP BYE response: %+v", bye)
	}
	waitSimulatorMedia(t, server, api, "idle", time.Second)

	preview("preview-drop")
	conn.Close()
	waitSimulator(t, server, api, "retrying")
	conn = accept()
	reader = bufio.NewReader(conn)
	renewed := readSIPStream(t, conn, reader, "REGISTER ", time.Second)
	if renewed.headers["call-id"] != register.headers["call-id"] ||
		renewed.headers["cseq"] == auth.headers["cseq"] ||
		!strings.Contains(renewed.headers["contact"], conn.RemoteAddr().String()+";transport=tcp") {
		t.Fatalf("reconnection did not register the new socket: %+v", renewed)
	}
	sendSIPStream(t, conn, renewed.response("200 OK", ""))
	if status := waitSimulator(t, server, api, "registered"); status.MediaState != "idle" ||
		status.LocalAddress != conn.RemoteAddr().String() {
		t.Fatalf("previous media survived reconnection or SIP address is stale: %+v", status)
	}

	diagnosticRequest(t, server, http.MethodPost, api+"/stop", http.StatusAccepted, nil)
	unregister := readSIPStream(t, conn, reader, "REGISTER ", 2*time.Second)
	if unregister.headers["expires"] != "0" {
		t.Fatalf("TCP stop did not unregister: %+v", unregister)
	}
	sendSIPStream(t, conn, unregister.response("200 OK", ""))
	waitSimulator(t, server, api, "stopped")
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("SIP TCP connection was not closed: %v", err)
	}
	trace := diagnosticRequest(t, server, http.MethodGet, api+"/trace.json", http.StatusOK, nil)
	if !bytes.Contains(trace, []byte("SIP/2.0/TCP")) || !bytes.Contains(trace, []byte("Authorization: [redacted]")) ||
		!bytes.Contains(trace, []byte("tcp-catalog")) || bytes.Contains(trace, []byte(auth.headers["authorization"])) ||
		bytes.Contains(trace, []byte(password)) {
		t.Fatal("TCP SIP trace is incomplete or leaks credentials")
	}
}

func readSIPStream(t *testing.T, conn net.Conn, reader *bufio.Reader, prefix string, timeout time.Duration) simulatorSIP {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	for {
		start, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read TCP SIP %s: %v", prefix, err)
		}
		if start == "\r\n" {
			continue
		}
		message := simulatorSIP{line: strings.TrimRight(start, "\r\n"), headers: make(map[string]string)}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if line == "\r\n" {
				break
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				t.Fatalf("malformed SIP header: %q", line)
			}
			message.headers[strings.ToLower(key)] = strings.TrimSpace(value)
		}
		length, err := strconv.Atoi(message.headers["content-length"])
		if err != nil || length < 0 || length > 8192 {
			t.Fatalf("invalid TCP Content-Length in %s: %+v", message.line, message.headers)
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			t.Fatal(err)
		}
		message.body = string(body)
		if strings.HasPrefix(message.line, prefix) {
			return message
		}
	}
}

func sendSIPStream(t *testing.T, conn net.Conn, message string) {
	t.Helper()
	conn.SetWriteDeadline(time.Now().Add(time.Second))
	if n, err := io.WriteString(conn, message); err != nil || n != len(message) {
		t.Fatalf("send SIP over TCP: %d/%d bytes, %v", n, len(message), err)
	}
}
