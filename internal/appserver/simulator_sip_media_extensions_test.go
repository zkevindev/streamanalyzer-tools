package appserver

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/sipphone"

	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
)

func TestSIPPhoneEarlyMediaAndDTMF(t *testing.T) {
	pbx := openSimulatorUDP(t)
	media := openSimulatorUDP(t)
	server := testDiagnosticServer(t, t.TempDir())
	cfg, err := json.Marshal(sipphone.Config{Host: "127.0.0.1", Port: pbx.LocalAddr().(*net.UDPAddr).Port, User: "2001"})
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/start", http.StatusAccepted, cfg)
	register, endpoint := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
	sendSimulatorSIP(t, pbx, endpoint, register.response("200 OK", ""))
	waitSIPPhone(t, server, "registered", "")
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/dial", http.StatusAccepted, []byte(`{"target":"2002"}`))
	invite, _ := receiveSimulatorSIP(t, pbx, "INVITE ", time.Second)
	if !strings.Contains(invite.body, "a=rtpmap:101 telephone-event/8000") {
		t.Fatalf("offer lacks DTMF: %s", invite.body)
	}
	contact := "Contact: <sip:2002@" + pbx.LocalAddr().String() + ">\r\n"
	answer := strings.Replace(sipTestSDP(media, "0", "PCMU", ""), "RTP/AVP 0\r\n", "RTP/AVP 0 101\r\n", 1) +
		"a=rtpmap:101 telephone-event/8000\r\na=fmtp:101 0-11\r\n"
	sendSimulatorSIP(t, pbx, endpoint, sipTestResponse(invite, "183 Session Progress", contact, answer))
	target := simulatorAudioAddress(t, invite.body)
	wait := func(state string, ready func(*sipphone.CallStatus) bool) *sipphone.CallStatus {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			call := waitSIPPhone(t, server, "registered", state).Call
			if ready(call) {
				return call
			}
			if time.Now().After(deadline) {
				t.Fatalf("unexpected call state: %+v", call)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait("ringing", func(c *sipphone.CallStatus) bool { return c.EarlyMedia })
	sendRTP := func(pt uint8, ts uint32, seq uint16, payload []byte) {
		t.Helper()
		packet := rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: pt, Timestamp: ts, SequenceNumber: seq, SSRC: 4321}, Payload: payload}
		raw, err := packet.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := media.WriteToUDP(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	sendRTP(0, 160, 1, make([]byte, 160))
	call := wait("ringing", func(c *sipphone.CallStatus) bool { return c.EarlyReceive.Packets == 1 })
	if call.ReceiveRTP.Packets != 0 {
		t.Fatalf("early media leaked into established media counters: %+v", call)
	}
	sendSimulatorSIP(t, pbx, endpoint, invite.response("200 OK", contact))
	receiveSimulatorSIP(t, pbx, "ACK ", time.Second)
	call = waitSIPPhone(t, server, "registered", "incall").Call
	if call.Codec != "PCMU" || call.DTMFKeys != "0123456789*#" {
		t.Fatalf("final response did not preserve the early SDP answer: %+v", call)
	}
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/dtmf", http.StatusConflict, []byte(`{"digit":"A"}`))
	diagnosticRequest(t, server, http.MethodPost, sipAPI+"/dtmf", http.StatusAccepted, []byte(`{"digit":"5"}`))
	if err := media.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1500)
	var timestamp uint32
	var final int
	for i := 0; i < 7; {
		n, _, err := media.ReadFromUDP(buf)
		if err != nil {
			t.Fatal(err)
		}
		var packet rtp.Packet
		if err := packet.Unmarshal(buf[:n]); err != nil {
			t.Fatal(err)
		}
		if packet.PayloadType != 101 {
			continue
		}
		if i == 0 {
			timestamp = packet.Timestamp
		}
		if packet.Timestamp != timestamp || packet.Marker != (i == 0) || len(packet.Payload) != 4 || packet.Payload[0] != 5 {
			t.Fatalf("wrong RTP telephone event: %+v", packet)
		}
		if packet.Payload[1]&0x80 != 0 {
			final++
			if binary.BigEndian.Uint16(packet.Payload[2:]) != 800 {
				t.Fatalf("wrong final event duration: %x", packet.Payload)
			}
		}
		i++
	}
	if final != 3 {
		t.Fatalf("got %d final DTMF reports, want 3", final)
	}
	for seq := uint16(2); seq <= 4; seq++ {
		sendRTP(101, 320, seq, []byte{11, 0x8a, 3, 32})
	}
	sendRTP(0, 1120, 5, make([]byte, 160))
	call = wait("incall", func(c *sipphone.CallStatus) bool { return c.ReceiveRTP.Packets == 4 })
	if len(call.DigitEvents) != 2 || call.DigitEvents[0].Digit != "5" || call.DigitEvents[1].Digit != "#" {
		t.Fatalf("wrong sent/received DTMF events: %+v", call.DigitEvents)
	}
	wav := diagnosticRequest(t, server, http.MethodGet, sipAPI+"/received.wav", http.StatusOK, nil)
	if len(wav) != 44+2*320 {
		t.Fatalf("telephone-event bytes leaked into the WAV: %d bytes", len(wav))
	}
}

func simulatorAudioAddress(t *testing.T, text string) *net.UDPAddr {
	t.Helper()
	var session sdp.SessionDescription
	if err := session.Unmarshal([]byte(text)); err != nil {
		t.Fatal(err)
	}
	for _, media := range session.MediaDescriptions {
		if media.MediaName.Media == "audio" {
			return &net.UDPAddr{IP: net.ParseIP(session.ConnectionInformation.Address.Address), Port: media.MediaName.Port.Value}
		}
	}
	t.Fatal("missing audio in SDP")
	return nil
}
