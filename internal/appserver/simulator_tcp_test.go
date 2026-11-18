package appserver

import (
	"bytes"
	"encoding/binary"
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

func TestGB28181SimulatorTCPMedia(t *testing.T) {
	clearDiagnosticModelEnv(t)
	platform := openSimulatorUDP(t)
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/gb28181"
	const platformID = "34020000002000000001"
	const channelID = "34020000001320000002"

	body, err := json.Marshal(gb28181.Config{
		PlatformHost: "127.0.0.1", PlatformPort: platform.LocalAddr().(*net.UDPAddr).Port,
		PlatformID: platformID, DeviceID: "34020000001320000001", ChannelID: channelID,
	})
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, api+"/start", http.StatusAccepted, body)
	register, device := receiveSimulatorSIP(t, platform, "REGISTER ", 2*time.Second)
	sendSimulatorSIP(t, platform, device, register.response("200 OK", ""))
	waitSimulator(t, server, api, "registered")

	from := "<sip:" + platformID + "@3402000000>;tag=mock-platform"
	channel := "sip:" + channelID + "@" + device.String()
	invite := func(t *testing.T, id, setup string, port int, code string) simulatorSIP {
		t.Helper()
		offer := fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 127.0.0.1\r\ns=Play\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n%s"+
			"m=video %d TCP/RTP/AVP 97\r\na=connection:new\r\na=recvonly\r\na=rtpmap:97 PS/90000\r\ny=0100000001\r\n",
			platformID, setup, port)
		sendSimulatorRequest(t, platform, device, from, "INVITE", channel, "<"+channel+">", id, id+"-invite", "application/sdp", offer, 1)
		response, _ := receiveSimulatorSIP(t, platform, "SIP/2.0 "+code+" ", time.Second)
		if response.headers["call-id"] != id {
			t.Fatalf("unexpected response to %s: %+v", id, response)
		}
		return response
	}
	sendDialog := func(t *testing.T, method string, answer simulatorSIP, cseq int) {
		t.Helper()
		id := answer.headers["call-id"]
		sendSimulatorRequest(t, platform, device, from, method, channel, answer.headers["to"], id, id+"-"+method, "", "", cseq)
	}
	end := func(t *testing.T, answer simulatorSIP) {
		t.Helper()
		sendDialog(t, "BYE", answer, 2)
		response, _ := receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
		if response.headers["cseq"] != "2 BYE" {
			t.Fatalf("unexpected BYE response: %+v", response)
		}
		status := waitSimulatorMedia(t, server, api, "idle", time.Second)
		if status.State != "registered" {
			t.Fatalf("ending media affected registration: %+v", status)
		}
	}

	t.Run("unsupported TCP role", func(t *testing.T) {
		invite(t, "unsupported", "a=setup:holdconn\r\n", 9, "488")
		status := waitSimulatorMedia(t, server, api, "failed", time.Second)
		if !strings.Contains(status.MediaError, "setup") || status.State != "registered" {
			t.Fatalf("unexpected setup failure: %+v", status)
		}
	})

	t.Run("TCP dial refusal", func(t *testing.T) {
		listener := openSimulatorTCP(t)
		target := listener.Addr().String()
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		answer := invite(t, "connection-refused", "a=setup:passive\r\n", port, "200")
		sendDialog(t, "ACK", answer, 1)
		status := waitSimulatorMedia(t, server, api, "failed", time.Second)
		if !strings.Contains(status.MediaError, target) || status.MediaTransport != "tcp-active" ||
			status.State != "registered" || status.RTPPackets != 0 {
			t.Fatalf("unexpected dial failure: %+v", status)
		}
	})

	t.Run("device connects to platform", func(t *testing.T) {
		listener := openSimulatorTCP(t)
		target := listener.Addr().String()
		answer := invite(t, "tcp-active", "a=setup:passive\r\n", listener.Addr().(*net.TCPAddr).Port, "200")
		if !strings.Contains(answer.body, "m=video 9 TCP/RTP/AVP 97\r\n") ||
			!strings.Contains(answer.body, "a=setup:active\r\na=connection:new\r\n") {
			t.Fatalf("unexpected active answer: %s", answer.body)
		}
		listener.SetDeadline(time.Now().Add(100 * time.Millisecond))
		if premature, err := listener.AcceptTCP(); err == nil {
			premature.Close()
			t.Fatal("device connected before SIP ACK")
		}
		sendDialog(t, "ACK", answer, 1)
		listener.SetDeadline(time.Now().Add(time.Second))
		conn, err := listener.AcceptTCP()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		readSimulatorTCPFrame(t, conn)
		status := waitSimulatorMedia(t, server, api, "streaming", time.Second)
		if status.MediaTransport != "tcp-active" || status.MediaTarget != target ||
			status.MediaLocal != conn.RemoteAddr().String() || status.MediaError != "" {
			t.Fatalf("wrong active media endpoint: %+v", status)
		}
		end(t, answer)
		conn.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := io.Copy(io.Discard, conn); err != nil {
			t.Fatalf("BYE did not close the TCP stream: %v", err)
		}
	})

	t.Run("platform connects to device", func(t *testing.T) {
		answer := invite(t, "tcp-passive", "a=setup:active\r\n", 9, "200")
		target := passiveSimulatorAddress(t, answer)
		status := waitSimulatorMedia(t, server, api, "waiting_ack", time.Second)
		if status.MediaTransport != "tcp-passive" || status.MediaLocal != target || status.MediaTarget != "" {
			t.Fatalf("wrong negotiated listener: %+v", status)
		}
		conn, err := net.DialTimeout("tcp4", target, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		if _, err := conn.Read(make([]byte, 1)); err == nil {
			t.Fatal("device started sending before SIP ACK")
		}
		sendDialog(t, "ACK", answer, 1)
		readSimulatorTCPFrame(t, conn)
		status = waitSimulatorMedia(t, server, api, "streaming", time.Second)
		if status.MediaLocal != target || status.MediaTarget != conn.LocalAddr().String() || status.MediaError != "" {
			t.Fatalf("wrong passive media endpoint: %+v", status)
		}
		end(t, answer)
	})

	t.Run("BYE interrupts TCP accept", func(t *testing.T) {
		answer := invite(t, "cancel-accept", "", 9, "200")
		target := passiveSimulatorAddress(t, answer)
		sendDialog(t, "ACK", answer, 1)
		waitSimulatorMedia(t, server, api, "connecting", time.Second)
		end(t, answer)
		if conn, err := net.DialTimeout("tcp4", target, time.Second); err == nil {
			conn.Close()
			t.Fatal("BYE did not release the TCP listener")
		}
	})

	t.Run("no platform connection", func(t *testing.T) {
		answer := invite(t, "accept-timeout", "a=setup:actpass\r\n", 9, "200")
		target := passiveSimulatorAddress(t, answer)
		sendDialog(t, "ACK", answer, 1)
		status := waitSimulatorMedia(t, server, api, "failed", 7*time.Second)
		if status.State != "registered" || status.RTPPackets != 0 ||
			!strings.Contains(status.MediaError, "等待平台连入") || !strings.Contains(status.MediaError, target) {
			t.Fatalf("wrong passive timeout status: %+v", status)
		}
	})

	diagnosticRequest(t, server, http.MethodPost, api+"/stop", http.StatusAccepted, nil)
	unregister, _ := receiveSimulatorSIP(t, platform, "REGISTER ", time.Second)
	sendSimulatorSIP(t, platform, device, unregister.response("200 OK", ""))
	waitSimulator(t, server, api, "stopped")
}

func openSimulatorTCP(t *testing.T) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener
}

func passiveSimulatorAddress(t *testing.T, answer simulatorSIP) string {
	t.Helper()
	if !strings.Contains(answer.body, "a=setup:passive\r\na=connection:new\r\n") {
		t.Fatalf("unexpected passive answer: %s", answer.body)
	}
	for _, line := range strings.Split(answer.body, "\r\n") {
		if spec, ok := strings.CutPrefix(line, "m=video "); ok {
			fields := strings.Fields(spec)
			if len(fields) != 3 || fields[1] != "TCP/RTP/AVP" || fields[2] != "97" {
				break
			}
			if port, err := strconv.Atoi(fields[0]); err == nil && port > 9 && port <= 65535 {
				return net.JoinHostPort("127.0.0.1", fields[0])
			}
			break
		}
	}
	t.Fatalf("no usable TCP listener advertised: %s", answer.body)
	return ""
}

func readSimulatorTCPFrame(t *testing.T, conn net.Conn) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var length [2]byte
	var packet [1212]byte
	var ps []byte
	var sequence uint16
	var stamp uint32
	for range 12 {
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			t.Fatalf("read RFC 4571 length: %v", err)
		}
		size := int(binary.BigEndian.Uint16(length[:]))
		if size < 12 || size > len(packet) {
			t.Fatalf("invalid framed RTP length: %d", size)
		}
		if _, err := io.ReadFull(conn, packet[:size]); err != nil {
			t.Fatalf("read framed RTP packet: %v", err)
		}
		if packet[0] != 0x80 || packet[1]&0x7f != 97 || binary.BigEndian.Uint32(packet[8:12]) != 100000001 {
			t.Fatalf("unexpected RTP header: %x", packet[:12])
		}
		if len(ps) != 0 && (binary.BigEndian.Uint16(packet[2:4]) != sequence+1 || binary.BigEndian.Uint32(packet[4:8]) != stamp) {
			t.Fatal("non-contiguous RTP fragments within the first frame")
		}
		sequence, stamp = binary.BigEndian.Uint16(packet[2:4]), binary.BigEndian.Uint32(packet[4:8])
		ps = append(ps, packet[12:size]...)
		if packet[1]&0x80 != 0 {
			if !bytes.HasPrefix(ps, []byte{0, 0, 1, 0xba}) || !bytes.Contains(ps, []byte{0, 0, 1, 0xe0}) {
				t.Fatal("first RTP frame lacks a PS pack or PES video")
			}
			return
		}
	}
	t.Fatal("first PS frame lacked an RTP marker")
}

func waitSimulatorMedia(t *testing.T, server *Server, api, state string, timeout time.Duration) gb28181.Status {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		status := decodeDiagnostic[gb28181.Status](t, diagnosticRequest(t, server, http.MethodGet, api, http.StatusOK, nil))
		if status.MediaState == state {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for media state %s, got %s (%s)", state, status.MediaState, status.MediaError)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
