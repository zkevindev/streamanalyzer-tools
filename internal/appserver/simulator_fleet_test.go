package appserver

import (
	"bufio"
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

const fleetAPI = "/api/simulators/gb28181"
const fleetPlatformID = "34020000002000000001"
const fleetFirstDevice = "34020000001320000001"
const fleetFirstChannel = "34020000001320000002"
const fleetSecondDevice = "34020000001320000004"

func TestGB28181BatchUDPAndConcurrentPreviews(t *testing.T) {
	platform := openSimulatorUDP(t)
	server := testDiagnosticServer(t, t.TempDir())
	startSimulatorBatch(t, server, "udp", platform.LocalAddr().(*net.UDPAddr).Port)

	channels := map[string][]string{
		fleetFirstDevice:  {fleetFirstChannel, "34020000001320000003"},
		fleetSecondDevice: {"34020000001320000005", "34020000001320000006"},
	}
	addresses := make(map[string]*net.UDPAddr)
	initial := make(map[string]string)
	for range channels {
		req, addr := receiveSimulatorSIP(t, platform, "REGISTER ", time.Second)
		id := simulatorRegistrationID(req)
		if addresses[id] != nil || len(channels[id]) == 0 || req.headers["expires"] != "3600" {
			t.Fatalf("unexpected REGISTER: %+v from %s", req, addr)
		}
		addresses[id] = addr
		initial[id] = req.headers["call-id"]
		sendSimulatorSIP(t, platform, addr, req.response("200 OK", "Expires: 600\r\n"))
	}
	if addresses[fleetFirstDevice].String() == addresses[fleetSecondDevice].String() || initial[fleetFirstDevice] == initial[fleetSecondDevice] {
		t.Fatal("registrations must use independent socket endpoints and Call-IDs")
	}
	detail := func(device, channel string) string {
		path := fleetAPI + "?device_id=" + device
		if channel != "" {
			path += "&channel_id=" + channel
		}
		return path
	}
	send := func(device, channel, method, to, callID, contentType, body string, seq int) {
		t.Helper()
		addr := addresses[device]
		uri := "sip:" + channel + "@" + addr.String()
		if to == "" {
			to = "<" + uri + ">"
		}
		from := "<sip:" + fleetPlatformID + "@3402000000>;tag=fleet-platform"
		branch := callID + "-" + channel + "-" + method
		sendSimulatorRequest(t, platform, addr, from, method, uri, to, callID, branch, contentType, body, seq)
	}

	for device, ids := range channels {
		waitSimulator(t, server, detail(device, ""), "registered")
		send(device, device, "MESSAGE", "", "catalog-"+device, "Application/MANSCDP+xml", simulatorCatalogQuery(device), 1)
		receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
		for _, id := range ids {
			reply, addr := receiveSimulatorSIP(t, platform, "MESSAGE ", time.Second)
			if addr.String() != addresses[device].String() || !strings.Contains(reply.body, "<SN>51</SN>") ||
				!strings.Contains(reply.body, "<SumNum>2</SumNum><DeviceList Num=\"1\">") ||
				!strings.Contains(reply.body, "<Item><DeviceID>"+id+"</DeviceID>") ||
				!strings.Contains(reply.body, "<ParentID>"+device+"</ParentID>") {
				t.Fatalf("wrong Catalog page: %s from %s", reply.body, addr)
			}
			sendSimulatorSIP(t, platform, addr, reply.response("200 OK", ""))
		}
	}

	type preview struct {
		device, channel, callID, to string
		ssrc                        uint32
		receiver                    *net.UDPConn
	}
	var previews []preview
	for _, device := range []string{fleetFirstDevice, fleetSecondDevice} {
		for _, channel := range channels[device] {
			p := preview{device: device, channel: channel, receiver: openSimulatorUDP(t),
				callID: "fleet-preview", ssrc: uint32(100000001 + len(previews))}
			offer := fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 127.0.0.1\r\ns=Play\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n"+
				"m=video %d RTP/AVP 96\r\na=rtpmap:96 PS/90000\r\ny=%010d\r\n", fleetPlatformID, p.receiver.LocalAddr().(*net.UDPAddr).Port, p.ssrc)
			send(device, channel, "INVITE", "", p.callID, "application/sdp", offer, 1)
			answer, addr := receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
			if addr.String() != addresses[device].String() || answer.headers["call-id"] != p.callID ||
				!strings.Contains(answer.body, fmt.Sprintf("y=%010d", p.ssrc)) {
				t.Fatalf("wrong INVITE answer: %+v from %s", answer, addr)
			}
			p.to = answer.headers["to"]
			send(device, channel, "ACK", p.to, p.callID, "", "", 1)
			if len(previews) == 0 {
				send(device, channel, "INVITE", "", "duplicate-channel", "application/sdp", offer, 1)
				receiveSimulatorSIP(t, platform, "SIP/2.0 486 ", time.Second)
				send(fleetSecondDevice, channel, "INVITE", "", "cross-device-channel", "application/sdp", offer, 1)
				receiveSimulatorSIP(t, platform, "SIP/2.0 404 ", time.Second)
			}
			previews = append(previews, p)
		}
	}
	for _, p := range previews {
		packet := make([]byte, 1500)
		p.receiver.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := p.receiver.ReadFromUDP(packet)
		if err != nil || n < 12 || binary.BigEndian.Uint32(packet[8:12]) != p.ssrc {
			t.Fatalf("wrong RTP for %s: %x (%v)", p.channel, packet[:min(n, 12)], err)
		}
		status := waitSimulatorMedia(t, server, detail(p.device, p.channel), "streaming", time.Second)
		if status.DeviceID != p.device || status.ChannelID != p.channel || len(status.Devices) != 2 || len(status.Channels) != 2 ||
			status.Devices[0].ActiveMedia != 2 || status.Devices[1].ActiveMedia != 2 || len(status.Sessions) != 1 {
			t.Fatalf("wrong concurrent status for %s: %+v", p.channel, status)
		}
	}

	ended := previews[0]
	send(ended.device, ended.channel, "BYE", ended.to, ended.callID, "", "", 2)
	receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
	waitSimulatorMedia(t, server, detail(ended.device, ended.channel), "idle", time.Second)
	for _, p := range previews[1:] {
		path := detail(p.device, p.channel)
		first := waitSimulatorMedia(t, server, path, "streaming", time.Second)
		for _, mark := range first.Sessions[0].Timeline {
			if mark.Stage == "bye" || mark.Stage == "ended" {
				t.Fatalf("BYE on %s leaked into the evidence for %s", ended.channel, p.channel)
			}
		}
		deadline := time.Now().Add(time.Second)
		for {
			current := waitSimulatorMedia(t, server, path, "streaming", time.Second)
			if current.RTPPackets > first.RTPPackets {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("RTP stopped for %s after the BYE on %s", p.channel, ended.channel)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	differentDevice := previews[3]
	status := waitSimulatorMedia(t, server, detail(differentDevice.device, differentDevice.channel), "streaming", time.Second)
	session := status.Sessions[0]
	archive := diagnosticRequest(t, server, http.MethodGet, fleetAPI+"/sessions/"+session.ID+"/evidence.zip", http.StatusOK, nil)
	review := decodeDiagnostic[gb28181.EvidenceReview](t, diagnosticRequest(t, server, http.MethodPost, fleetAPI+"/evidence/import", http.StatusOK, archive))
	if review.Session.DeviceID != differentDevice.device || review.Session.ChannelID != differentDevice.channel || review.RTP == nil {
		t.Fatalf("wrong evidence attribution: %+v", review.Session)
	}
	trace := decodeDiagnostic[gb28181.Status](t, diagnosticRequest(t, server, http.MethodGet,
		fleetAPI+"/trace.json?device_id="+fleetSecondDevice, http.StatusOK, nil))
	if trace.DeviceID != fleetSecondDevice || len(trace.Events) == 0 {
		t.Fatalf("wrong trace device: %s", trace.DeviceID)
	}
	diagnosticRequest(t, server, http.MethodGet, detail(fleetSecondDevice, fleetFirstChannel), http.StatusNotFound, nil)

	diagnosticRequest(t, server, http.MethodPost, fleetAPI+"/stop", http.StatusAccepted, nil)
	stopped := make(map[string]bool)
	for range channels {
		req, addr := receiveSimulatorSIP(t, platform, "REGISTER ", time.Second)
		id := simulatorRegistrationID(req)
		if stopped[id] || req.headers["expires"] != "0" || initial[id] != req.headers["call-id"] {
			t.Fatalf("wrong batch unregister: %+v", req)
		}
		stopped[id] = true
		sendSimulatorSIP(t, platform, addr, req.response("200 OK", ""))
	}
	status = waitSimulator(t, server, fleetAPI, "stopped")
	for _, device := range status.Devices {
		if device.State != "stopped" || device.ActiveMedia != 0 {
			t.Fatalf("device did not stop: %+v", device)
		}
	}
}

func TestGB28181BatchTCPCatalog(t *testing.T) {
	platform := openSimulatorTCP(t)
	server := testDiagnosticServer(t, t.TempDir())
	startSimulatorBatch(t, server, "tcp", platform.Addr().(*net.TCPAddr).Port)
	want := map[string][]string{
		fleetFirstDevice:  {fleetFirstChannel, "34020000001320000003"},
		fleetSecondDevice: {"34020000001320000005", "34020000001320000006"},
	}
	type link struct {
		conn   *net.TCPConn
		reader *bufio.Reader
	}
	links := make(map[string]link)
	for range want {
		platform.SetDeadline(time.Now().Add(time.Second))
		conn, err := platform.AcceptTCP()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		reader := bufio.NewReader(conn)
		req := readSIPStream(t, conn, reader, "REGISTER ", time.Second)
		id := simulatorRegistrationID(req)
		if len(want[id]) == 0 || links[id].conn != nil {
			t.Fatalf("unexpected TCP registration: %+v", req)
		}
		links[id] = link{conn, reader}
		sendSIPStream(t, conn, req.response("200 OK", "Expires: 600\r\n"))
	}
	for id, l := range links {
		uri := "sip:" + id + "@" + l.conn.RemoteAddr().String() + ";transport=tcp"
		query := simulatorCatalogQuery(id)
		request := fmt.Sprintf("MESSAGE %s SIP/2.0\r\nVia: SIP/2.0/TCP %s;branch=z9hG4bKcatalog-%s\r\n"+
			"From: <sip:%s@3402000000>;tag=fleet-platform\r\nTo: <%s>\r\nCall-ID: catalog-%s\r\nCSeq: 1 MESSAGE\r\n"+
			"Max-Forwards: 70\r\nContent-Type: Application/MANSCDP+xml\r\nContent-Length: %d\r\n\r\n%s",
			uri, platform.Addr(), id, fleetPlatformID, uri, id, len(query), query)
		sendSIPStream(t, l.conn, request)
		readSIPStream(t, l.conn, l.reader, "SIP/2.0 200 ", time.Second)
		resp := readSIPStream(t, l.conn, l.reader, "MESSAGE ", time.Second)
		if !strings.Contains(resp.body, "<SumNum>2</SumNum><DeviceList Num=\"2\">") {
			t.Fatalf("TCP Catalog should return the entire directory: %s", resp.body)
		}
		for _, channel := range want[id] {
			if !strings.Contains(resp.body, "<Item><DeviceID>"+channel+"</DeviceID>") {
				t.Fatalf("TCP Catalog missed %s: %s", channel, resp.body)
			}
		}
		sendSIPStream(t, l.conn, resp.response("200 OK", ""))
	}
	diagnosticRequest(t, server, http.MethodPost, fleetAPI+"/stop", http.StatusAccepted, nil)
	for _, l := range links {
		req := readSIPStream(t, l.conn, l.reader, "REGISTER ", time.Second)
		if req.headers["expires"] != "0" {
			t.Fatalf("expected TCP unregister: %+v", req)
		}
		sendSIPStream(t, l.conn, req.response("200 OK", ""))
	}
	waitSimulator(t, server, fleetAPI, "stopped")
}

func startSimulatorBatch(t *testing.T, server *Server, transport string, port int) {
	t.Helper()
	body, err := json.Marshal(gb28181.Config{PlatformHost: "127.0.0.1", PlatformPort: port,
		PlatformID: fleetPlatformID, DeviceID: fleetFirstDevice, ChannelID: fleetFirstChannel,
		DeviceCount: 2, ChannelsPerDevice: 2, RegisterIntervalMS: 30, SIPTransport: transport, CaptureRTP: true})
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, fleetAPI+"/start", http.StatusAccepted, body)
}

func simulatorRegistrationID(req simulatorSIP) string {
	_, id, _ := strings.Cut(req.headers["from"], "sip:")
	id, _, _ = strings.Cut(id, "@")
	return id
}

func simulatorCatalogQuery(id string) string {
	return "<Query><CmdType>Catalog</CmdType><SN>51</SN><DeviceID>" + id + "</DeviceID></Query>"
}
