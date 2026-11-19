package appserver

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/sipphone"

	"github.com/pion/rtp"
)

func TestSIPPhoneRTPDestinationWithPortMapping(t *testing.T) {
	for _, mode := range []string{"sdp", "source"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			pbx := openSimulatorUDP(t)
			advertised := openSimulatorUDP(t)
			actual := openSimulatorUDP(t)
			server := testDiagnosticServer(t, t.TempDir())
			body, err := json.Marshal(sipphone.Config{Host: "127.0.0.1", Port: pbx.LocalAddr().(*net.UDPAddr).Port,
				User: "2001", RTPReturn: mode, SimulateNATPort: true})
			if err != nil {
				t.Fatal(err)
			}
			diagnosticRequest(t, server, "POST", sipAPI+"/start", http.StatusAccepted, body)
			register, endpoint := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
			sendSimulatorSIP(t, pbx, endpoint, register.response("200 OK", ""))
			waitSIPPhone(t, server, "registered", "")
			diagnosticRequest(t, server, "POST", sipAPI+"/dial", http.StatusAccepted, []byte(`{"target":"2002"}`))
			invite, _ := receiveSimulatorSIP(t, pbx, "INVITE ", time.Second)
			offerAddress := simulatorAudioAddress(t, invite.body)
			contact := "Contact: <sip:2002@" + pbx.LocalAddr().String() + ">\r\n"
			sendSimulatorSIP(t, pbx, endpoint, sipTestResponse(invite, "200 OK", contact, sipTestSDP(advertised, "0", "PCMU", "")))
			receiveSimulatorSIP(t, pbx, "ACK ", time.Second)
			call := waitSIPPhone(t, server, "registered", "incall").Call
			local, err := net.ResolveUDPAddr("udp4", call.MediaLocal)
			if err != nil || local.Port == offerAddress.Port || call.RTPPath.AdvertisedLocal != offerAddress.String() {
				t.Fatalf("simulated endpoint did not advertise a separate port: %+v", call)
			}
			buf := make([]byte, 1500)
			_ = advertised.SetReadDeadline(time.Now().Add(time.Second))
			if _, src, err := advertised.ReadFromUDP(buf); err != nil || src.String() != local.String() {
				t.Fatalf("initial RTP did not use the SDP destination and actual local port: %v %v", src, err)
			}
			send := func(target *net.UDPAddr, seq uint16) {
				t.Helper()
				packet := rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 0, SSRC: 45, SequenceNumber: seq, Timestamp: uint32(seq) * 160}, Payload: make([]byte, 160)}
				raw, err := packet.Marshal()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := actual.WriteToUDP(raw, target); err != nil {
					t.Fatal(err)
				}
			}
			send(offerAddress, 1)
			send(local, 2)
			want := advertised.LocalAddr().String()
			if mode == "source" {
				want = actual.LocalAddr().String()
				_ = actual.SetReadDeadline(time.Now().Add(time.Second))
				if _, from, err := actual.ReadFromUDP(buf); err != nil || from.String() != local.String() {
					t.Fatalf("learned path did not deliver reverse RTP: %v %v", from, err)
				}
			} else {
				_ = actual.SetReadDeadline(time.Now().Add(160 * time.Millisecond))
				if _, _, err := actual.ReadFromUDP(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
					t.Fatalf("SDP route unexpectedly delivered RTP to the NAT port: %v", err)
				}
			}
			deadline := time.Now().Add(time.Second)
			for {
				call = waitSIPPhone(t, server, "registered", "incall").Call
				if call.RTPPath.Destination == want && call.RTPPath.ObservedSource == actual.LocalAddr().String() {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("RTP path evidence missing: %+v", call)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if call.ReceiveRTP.Packets != 1 || call.MediaRemote != advertised.LocalAddr().String() ||
				call.RTPPath.ReturnMode != mode || call.RTPPath.LearnedAt.IsZero() != (mode == "sdp") {
				t.Fatalf("incorrect SDP, observed source, or learned destination: %+v", call)
			}
			diagnosticRequest(t, server, "POST", sipAPI+"/hangup", http.StatusAccepted, nil)
			bye, _ := receiveSimulatorSIP(t, pbx, "BYE ", time.Second)
			sendSimulatorSIP(t, pbx, endpoint, bye.response("200 OK", ""))
			waitSIPPhone(t, server, "registered", "ended")
			diagnosticRequest(t, server, "POST", sipAPI+"/stop", http.StatusAccepted, nil)
			stop, _ := receiveSimulatorSIP(t, pbx, "REGISTER ", time.Second)
			sendSimulatorSIP(t, pbx, endpoint, stop.response("200 OK", ""))
			waitSIPPhone(t, server, "stopped", "ended")
		})
	}
}
