package appserver

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/gb28181"
)

func TestGB28181CatalogSubscriptionAndChannelAvailability(t *testing.T) {
	platform := openSimulatorUDP(t)
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/gb28181"
	const device = "34020000001320000001"
	const first = "34020000001320000002"
	const sibling = "34020000001320000003"
	const platformID = "34020000002000000001"
	body := fmt.Appendf(nil, `{"platform_host":"127.0.0.1","platform_port":%d,"platform_id":%q,"device_id":%q,"channel_id":%q,"channels_per_device":2}`,
		platform.LocalAddr().(*net.UDPAddr).Port, platformID, device, first)
	diagnosticRequest(t, server, "POST", api+"/start", http.StatusAccepted, body)
	register, addr := receiveSimulatorSIP(t, platform, "REGISTER ", time.Second)
	sendSimulatorSIP(t, platform, addr, register.response("200 OK", "Expires: 600\r\n"))
	waitSimulator(t, server, api, "registered")

	from := "<sip:" + platformID + "@3402000000>;tag=platform-catalog"
	deviceURI := "sip:" + device + "@" + addr.String()
	platformContact := "<sip:" + platformID + "@" + platform.LocalAddr().String() + ">"
	const event = "Catalog;id=37"
	const subscribeID = "subscription-37"
	var notifySeq string
	readNotify := func(localTo, state, eventBody string) simulatorSIP {
		t.Helper()
		msg, peer := receiveSimulatorSIP(t, platform, "NOTIFY ", time.Second)
		if msg.headers["call-id"] != subscribeID || msg.headers["event"] != event ||
			msg.headers["to"] != from || msg.headers["from"] != localTo ||
			!strings.HasPrefix(msg.headers["subscription-state"], state) || !strings.Contains(msg.body, eventBody) ||
			msg.headers["cseq"] == notifySeq {
			t.Fatalf("invalid Catalog NOTIFY: %+v", msg)
		}
		notifySeq = msg.headers["cseq"]
		sendSimulatorSIP(t, platform, peer, msg.response("200 OK", ""))
		return msg
	}
	subscribe := func(to string, seq, expires int) simulatorSIP {
		t.Helper()
		query := fmt.Sprintf("<Query><CmdType>Catalog</CmdType><SN>%d</SN><DeviceID>%s</DeviceID></Query>", seq, device)
		packet := fmt.Sprintf("SUBSCRIBE %s SIP/2.0\r\nVia: SIP/2.0/UDP %s;rport;branch=z9hG4bKsubscribe-%d\r\n"+
			"From: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %d SUBSCRIBE\r\nMax-Forwards: 70\r\n"+
			"Contact: %s\r\nEvent: %s\r\nExpires: %d\r\nContent-Type: Application/MANSCDP+xml\r\nContent-Length: %d\r\n\r\n%s",
			deviceURI, platform.LocalAddr(), seq, from, to, subscribeID, seq, platformContact, event, expires, len(query), query)
		sendSimulatorSIP(t, platform, addr, packet)
		res, _ := receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
		if res.headers["call-id"] != subscribeID || res.headers["expires"] != fmt.Sprint(expires) || !strings.Contains(res.headers["to"], ";tag=") {
			t.Fatalf("invalid subscription response: %+v", res)
		}
		return res
	}
	ok := subscribe("<"+deviceURI+">", 1, 60)
	taggedTo := ok.headers["to"]
	readNotify(taggedTo, "active;expires=60", "<CmdType>Catalog</CmdType>")

	media1, media2 := openSimulatorUDP(t), openSimulatorUDP(t)
	invite := func(channel, id string, receiver *net.UDPConn) string {
		t.Helper()
		uri := "sip:" + channel + "@" + addr.String()
		offer := fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 127.0.0.1\r\ns=Play\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n"+
			"m=video %d RTP/AVP 96\r\na=rtpmap:96 PS/90000\r\ny=0100000001\r\n", platformID, receiver.LocalAddr().(*net.UDPAddr).Port)
		sendSimulatorRequest(t, platform, addr, from, "INVITE", uri, "<"+uri+">", id, id, "application/sdp", offer, 1)
		return uri
	}
	preview := func(channel, id string, receiver *net.UDPConn) {
		t.Helper()
		uri := invite(channel, id, receiver)
		answer, _ := receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
		sendSimulatorRequest(t, platform, addr, from, "ACK", uri, answer.headers["to"], id, id+"-ack", "", "", 1)
		receiver.SetReadDeadline(time.Now().Add(time.Second))
		if n, _, err := receiver.ReadFromUDP(make([]byte, 1500)); err != nil || n <= 12 {
			t.Fatalf("preview for %s did not send RTP: %v", channel, err)
		}
	}
	preview(first, "preview-a", media1)
	preview(sibling, "preview-b", media2)

	set := func(channel string, online bool) gb28181.Status {
		t.Helper()
		body := fmt.Appendf(nil, `{"device_id":%q,"channel_id":%q,"online":%t}`, device, channel, online)
		return decodeDiagnostic[gb28181.Status](t, diagnosticRequest(t, server, "POST", api+"/channel-state", http.StatusOK, body))
	}
	offline := set(first, false)
	if offline.ChannelOnline || offline.CatalogSubscriptions != 1 || offline.MediaState != "idle" ||
		len(offline.Channels) != 2 || !offline.Channels[1].Online || offline.Channels[1].MediaState != "streaming" {
		t.Fatalf("taking one channel offline disrupted the device or sibling: %+v", offline)
	}
	readNotify(taggedTo, "active;", "<DeviceID>"+first+"</DeviceID><Event>OFF</Event><Status>OFF</Status>")
	invite(first, "preview-offline", media1)
	receiveSimulatorSIP(t, platform, "SIP/2.0 480 ", time.Second)
	query := fmt.Sprintf("<Query><CmdType>Catalog</CmdType><SN>9</SN><DeviceID>%s</DeviceID></Query>", first)
	sendSimulatorRequest(t, platform, addr, from, "MESSAGE", deviceURI, "<"+deviceURI+">", "offline-directory", "offline-directory", "Application/MANSCDP+xml", query, 1)
	receiveSimulatorSIP(t, platform, "SIP/2.0 200 ", time.Second)
	catalog, peer := receiveSimulatorSIP(t, platform, "MESSAGE ", time.Second)
	if !strings.Contains(catalog.body, "<Status>OFF</Status>") {
		t.Fatalf("Catalog query omitted channel state: %s", catalog.body)
	}
	sendSimulatorSIP(t, platform, peer, catalog.response("200 OK", ""))

	if ack := subscribe(taggedTo, 2, 120); ack.headers["to"] != taggedTo {
		t.Fatal("refresh replaced the dialog tag")
	}
	readNotify(taggedTo, "active;expires=120", "<Event>OFF</Event>")
	if recovered := set(first, true); !recovered.ChannelOnline {
		t.Fatal("channel stayed offline")
	}
	readNotify(taggedTo, "active;", "<DeviceID>"+first+"</DeviceID><Event>ON</Event><Status>ON</Status>")
	preview(first, "preview-back", openSimulatorUDP(t))

	subscribe(taggedTo, 3, 0)
	final := readNotify(taggedTo, "terminated;", "")
	if final.body != "" {
		t.Fatalf("terminated NOTIFY should have no content: %s", final.body)
	}
	if stopped := set(first, false); stopped.CatalogSubscriptions != 0 {
		t.Fatalf("subscription was not removed: %+v", stopped)
	}
	platform.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, _, err := platform.ReadFromUDP(make([]byte, 2048)); err == nil {
		t.Fatal("platform got an unexpected notification after cancellation")
	}

	diagnosticRequest(t, server, "POST", api+"/stop", http.StatusAccepted, nil)
	unregister, _ := receiveSimulatorSIP(t, platform, "REGISTER ", time.Second)
	sendSimulatorSIP(t, platform, addr, unregister.response("200 OK", ""))
	waitSimulator(t, server, api, "stopped")
}

func TestGB28181CatalogSubscriptionUsesRegisteredTCPFlow(t *testing.T) {
	platform := openSimulatorTCP(t)
	server := testDiagnosticServer(t, t.TempDir())
	const api = "/api/simulators/gb28181"
	const device = "34020000001320000001"
	const channel = "34020000001320000002"
	const platformID = "34020000002000000001"
	body := fmt.Appendf(nil, `{"platform_host":"127.0.0.1","platform_port":%d,"platform_id":%q,"device_id":%q,"channel_id":%q,"sip_transport":"tcp"}`,
		platform.Addr().(*net.TCPAddr).Port, platformID, device, channel)
	diagnosticRequest(t, server, "POST", api+"/start", http.StatusAccepted, body)
	platform.SetDeadline(time.Now().Add(time.Second))
	conn, err := platform.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	reg := readSIPStream(t, conn, reader, "REGISTER ", time.Second)
	sendSIPStream(t, conn, reg.response("200 OK", "Expires: 600\r\n"))
	waitSimulator(t, server, api, "registered")

	remote := "sip:" + platformID + "@" + platform.Addr().String() + ";transport=tcp"
	local := "sip:" + device + "@" + conn.RemoteAddr().String() + ";transport=tcp"
	query := "<Query><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>" + channel + "</DeviceID></Query>"
	subscribe := fmt.Sprintf("SUBSCRIBE %s SIP/2.0\r\nVia: SIP/2.0/TCP %s;branch=z9hG4bKcatalog-tcp\r\n"+
		"From: <%s>;tag=platform\r\nTo: <%s>\r\nCall-ID: catalog-tcp\r\nCSeq: 1 SUBSCRIBE\r\nMax-Forwards: 70\r\n"+
		"Contact: <%s>\r\nEvent: Catalog;id=1\r\nExpires: 60\r\nContent-Type: Application/MANSCDP+xml\r\nContent-Length: %d\r\n\r\n%s",
		local, platform.Addr(), remote, local, remote, len(query), query)
	sendSIPStream(t, conn, subscribe)
	ack := readSIPStream(t, conn, reader, "SIP/2.0 200 ", time.Second)
	initial := readSIPStream(t, conn, reader, "NOTIFY ", time.Second)
	if ack.headers["call-id"] != "catalog-tcp" || initial.headers["call-id"] != "catalog-tcp" ||
		!strings.Contains(initial.headers["via"], "SIP/2.0/TCP") || !strings.Contains(initial.body, "<DeviceID>"+channel+"</DeviceID>") {
		t.Fatalf("subscription did not use the existing TCP flow: %+v", initial)
	}
	sendSIPStream(t, conn, initial.response("200 OK", ""))
	diagnosticRequest(t, server, "POST", api+"/channel-state", http.StatusOK,
		fmt.Appendf(nil, `{"device_id":%q,"channel_id":%q,"online":false}`, device, channel))
	update := readSIPStream(t, conn, reader, "NOTIFY ", time.Second)
	if !strings.Contains(update.body, "<Event>OFF</Event>") {
		t.Fatalf("TCP subscription did not report offline: %+v", update)
	}
	sendSIPStream(t, conn, update.response("200 OK", ""))
	diagnosticRequest(t, server, "POST", api+"/stop", http.StatusAccepted, nil)
	unregister := readSIPStream(t, conn, reader, "REGISTER ", time.Second)
	sendSIPStream(t, conn, unregister.response("200 OK", ""))
	waitSimulator(t, server, api, "stopped")
}
