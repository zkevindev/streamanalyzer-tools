package appserver

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/gb28181"

	"github.com/pion/rtp"
	"github.com/zaf/g711"
)

func TestGB28181BroadcastAndTalkback(t *testing.T) {
	const api = "/api/simulators/gb28181"
	const deviceID = "34020000001320000001"
	const channelID = "34020000001320000002"
	const outputID = "34020000001370000001"
	const platformID = "34020000002000000001"
	for _, transport := range []string{"udp", "tcp"} {
		t.Run(transport, func(t *testing.T) {
			server := testDiagnosticServer(t, t.TempDir())
			downstream, upstream := openSimulatorUDP(t), openSimulatorUDP(t)
			var send func(string)
			var receive func(string) simulatorSIP
			var platformAddr, deviceAddr string
			cfg := gb28181.Config{PlatformHost: "127.0.0.1", SIPTransport: transport, PlatformID: platformID, DeviceID: deviceID, ChannelID: channelID, Voice: true}
			if transport == "tcp" {
				listener := openSimulatorTCP(t)
				cfg.PlatformPort = listener.Addr().(*net.TCPAddr).Port
				platformAddr = listener.Addr().String()
				startGBVoice(t, server, api, cfg)
				listener.SetDeadline(time.Now().Add(time.Second))
				conn, err := listener.AcceptTCP()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { conn.Close() })
				deviceAddr = conn.RemoteAddr().String() + ";transport=tcp"
				reader := bufio.NewReader(conn)
				send = func(raw string) { sendSIPStream(t, conn, raw) }
				receive = func(prefix string) simulatorSIP { return readSIPStream(t, conn, reader, prefix, time.Second) }
			} else {
				conn := openSimulatorUDP(t)
				platformAddr = conn.LocalAddr().String()
				cfg.PlatformPort = conn.LocalAddr().(*net.UDPAddr).Port
				startGBVoice(t, server, api, cfg)
				register, endpoint := receiveSimulatorSIP(t, conn, "REGISTER ", time.Second)
				deviceAddr = endpoint.String()
				sendSimulatorSIP(t, conn, endpoint, register.response("200 OK", ""))
				send = func(raw string) { sendSimulatorSIP(t, conn, endpoint, raw) }
				receive = func(prefix string) simulatorSIP {
					msg, _ := receiveSimulatorSIP(t, conn, prefix, time.Second)
					return msg
				}
			}
			if transport == "tcp" {
				send(receive("REGISTER ").response("200 OK", ""))
			}
			waitSimulator(t, server, api, "registered")

			request := func(method, recipient, from, to, id, branch, contentType, body string, seq int) {
				t.Helper()
				headers := ""
				if contentType != "" {
					headers = "Content-Type: " + contentType + "\r\n"
				}
				send(fmt.Sprintf("%s %s SIP/2.0\r\nVia: SIP/2.0/%s %s;rport;branch=z9hG4bK%s\r\n"+
					"From: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %d %s\r\nMax-Forwards: 70\r\n%sContent-Length: %d\r\n\r\n%s",
					method, recipient, strings.ToUpper(transport), platformAddr, branch, from, to, id, seq, method, headers, len(body), body))
			}
			from := "<sip:" + platformID + "@3402000000>;tag=mock-platform"
			deviceURI := "sip:" + deviceID + "@" + deviceAddr
			channelURI := "sip:" + channelID + "@" + deviceAddr
			request("MESSAGE", deviceURI, from, "<"+deviceURI+">", "voice-catalog", "catalog", "Application/MANSCDP+xml",
				"<Query><CmdType>Catalog</CmdType><SN>41</SN><DeviceID>"+deviceID+"</DeviceID></Query>", 1)
			receive("SIP/2.0 200 ")
			for {
				catalog := receive("MESSAGE ")
				send(catalog.response("200 OK", ""))
				if strings.Contains(catalog.body, "<DeviceID>"+outputID+"</DeviceID>") {
					if !strings.Contains(catalog.body, "<ParentID>"+deviceID+"</ParentID>") || !strings.Contains(catalog.body, "<SumNum>2</SumNum>") {
						t.Fatalf("invalid voice output in catalog: %s", catalog.body)
					}
					break
				}
			}

			speakerURI := "sip:" + outputID + "@" + deviceAddr
			notify := "<Notify><CmdType>Broadcast</CmdType><SN>42</SN><SourceID>" + platformID + "</SourceID><TargetID>" + outputID + "</TargetID></Notify>"
			request("MESSAGE", speakerURI, from, "<"+speakerURI+">", "broadcast-message", "broadcast", "Application/MANSCDP+xml", notify, 1)
			receive("SIP/2.0 200 ")
			response := receive("MESSAGE ")
			if !strings.Contains(response.body, "<SN>42</SN>") || !strings.Contains(response.body, "<DeviceID>"+outputID+"</DeviceID><Result>OK</Result>") {
				t.Fatalf("incorrect broadcast response: %s", response.body)
			}
			send(response.response("200 OK", ""))
			broadcast := receive("INVITE ")
			if !strings.Contains(broadcast.headers["subject"], outputID+":") ||
				!strings.HasSuffix(broadcast.headers["subject"], ","+platformID+":0") ||
				!strings.Contains(broadcast.body, "a=recvonly") {
				t.Fatalf("invalid reverse INVITE: %+v", broadcast)
			}
			// GB y= and f= are outside RFC 4566.
			rtpTarget := simulatorAudioAddress(t, strings.SplitN(broadcast.body, "y=", 2)[0])
			pt, codec, encode, decode := uint8(8), "PCMA", g711.EncodeAlawFrame, g711.DecodeAlawFrame
			if transport == "tcp" {
				pt, codec, encode, decode = 0, "PCMU", g711.EncodeUlawFrame, g711.DecodeUlawFrame
			}
			sdp := strings.Replace(sipTestSDP(downstream, fmt.Sprint(pt), codec, ""), "a=sendrecv", "a=sendonly", 1)
			contact := "Contact: <sip:" + platformID + "@" + platformAddr
			if transport == "tcp" {
				contact += ";transport=tcp"
			}
			send(sipTestResponse(broadcast, "200 OK", contact+">\r\n", sdp+"y=0100000001\r\nf=v/////a/1/8/1\r\n"))
			ack := receive("ACK ")
			if ack.headers["call-id"] != broadcast.headers["call-id"] {
				t.Fatalf("ACK for the wrong broadcast: %+v", ack)
			}
			payload, peak := make([]byte, 160), 0
			for i := range payload {
				payload[i] = encode(int16(5200 * math.Sin(2*math.Pi*600*float64(i)/8000)))
				sample := int(decode(payload[i]))
				peak = max(peak, sample, -sample)
			}
			for i := range 3 {
				p := rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: pt, Timestamp: uint32(i * 160), SequenceNumber: uint16(i + 10), SSRC: 100000001}, Payload: payload}
				data, err := p.Marshal()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := downstream.WriteToUDP(data, rtpTarget); err != nil {
					t.Fatal(err)
				}
			}

			video := fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 127.0.0.1\r\ns=Play\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=video %d RTP/AVP 96\r\na=rtpmap:96 PS/90000\r\na=recvonly\r\ny=0100000002\r\n",
				platformID, upstream.LocalAddr().(*net.UDPAddr).Port)
			request("INVITE", channelURI, from, "<"+channelURI+">", "voice-preview", "preview", "application/sdp", video, 1)
			preview := receive("SIP/2.0 200 ")
			request("ACK", channelURI, from, preview.headers["to"], "voice-preview", "preview-ack", "", "", 1)
			checkGBVoiceUplink(t, upstream)
			status := waitSimulator(t, server, api, "registered")
			if status.Broadcast == nil || status.Broadcast.State != "receiving" || status.Broadcast.Codec != codec ||
				status.Broadcast.Packets != 3 || status.Broadcast.Peak != peak || status.AudioPackets == 0 || status.VideoFrames == 0 {
				t.Fatalf("both voice directions should be active: %+v broadcast=%+v", status.MediaStatus, status.Broadcast)
			}

			request("BYE", deviceURI, ack.headers["to"], broadcast.headers["from"], broadcast.headers["call-id"], "broadcast-bye", "", "", 2)
			receive("SIP/2.0 200 ")
			deadline := time.Now().Add(time.Second)
			for {
				status = waitSimulator(t, server, api, "registered")
				if status.Broadcast.State == "ended" {
					if status.MediaState != "streaming" {
						t.Fatalf("ending broadcast stopped the independent media uplink: %+v", status.MediaStatus)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("broadcast did not end after BYE: %+v", status.Broadcast)
				}
				time.Sleep(10 * time.Millisecond)
			}
			request("BYE", channelURI, from, preview.headers["to"], "voice-preview", "preview-bye", "", "", 2)
			receive("SIP/2.0 200 ")
			diagnosticRequest(t, server, http.MethodPost, api+"/stop", http.StatusAccepted, nil)
			send(receive("REGISTER ").response("200 OK", ""))
			waitSimulator(t, server, api, "stopped")
		})
	}
}

func startGBVoice(t *testing.T, server *Server, api string, cfg gb28181.Config) {
	t.Helper()
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, api+"/start", http.StatusAccepted, body)
}

func checkGBVoiceUplink(t *testing.T, conn *net.UDPConn) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1500)
	var samples []byte
	var previous uint32
	for len(samples) < 800 {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			t.Fatal(err)
		}
		var packet rtp.Packet
		if err := packet.Unmarshal(buf[:n]); err != nil {
			t.Fatal(err)
		}
		i := bytes.Index(packet.Payload, []byte{0, 0, 1, 0xc0})
		if i < 0 {
			continue
		}
		pes := packet.Payload[i:]
		if len(pes) < 9 {
			t.Fatalf("truncated PS audio: %x", pes)
		}
		end := int(binary.BigEndian.Uint16(pes[4:6])) + 6
		start := 9 + int(pes[8])
		if end > len(pes) || end-start != 160 || len(samples) > 0 && packet.Timestamp-previous != 1800 {
			t.Fatalf("invalid 20 ms G.711 PS audio packet: %x", pes)
		}
		samples = append(samples, pes[start:end]...)
		previous = packet.Timestamp
	}
	for i, code := range samples {
		want := g711.EncodeAlawFrame(int16(4800 * math.Sin(2*math.Pi*440*float64(i)/8000)))
		if code != want {
			t.Fatalf("uplink PCM differs from the 440 Hz G.711 tone at sample %d: %x != %x", i, code, want)
		}
	}
}
