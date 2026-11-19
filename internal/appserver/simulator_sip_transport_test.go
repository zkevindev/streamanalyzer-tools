package appserver

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/sipphone"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

func TestSIPStreamTransportRegisterAndCall(t *testing.T) {
	certServer := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := certServer.TLS.Certificates[0]
	certServer.Close()
	root := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}))
	for _, tc := range []struct{ name, transport, ca, serverName, failure string }{
		{name: "tcp", transport: "tcp"},
		{name: "trusted", transport: "tls", ca: root},
		{name: "untrusted", transport: "tls", failure: "untrusted_certificate"},
		{name: "wrong-name", transport: "tls", ca: root, serverName: "incorrect.example", failure: "hostname_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			pbx, err := sipgo.NewUA()
			if err != nil {
				t.Fatal(err)
			}
			defer pbx.Close()
			server, err := sipgo.NewServer(pbx)
			if err != nil {
				t.Fatal(err)
			}
			media := openSimulatorUDP(t)
			registered := make(chan string, 4)
			acked := make(chan string, 4)
			ended := make(chan string, 4)
			server.OnRegister(func(req *sip.Request, tx sip.ServerTransaction) {
				res := sip.NewResponseFromRequest(req, 200, "OK", nil)
				if req.GetHeader("Authorization") == nil && req.GetHeader("Expires").Value() != "0" {
					res = sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
					res.AppendHeader(sip.NewHeader("WWW-Authenticate", `Digest realm="loopback", nonce="sample-nonce", algorithm=MD5, qop="auth"`))
				} else if req.GetHeader("Expires").Value() != "0" {
					registered <- req.Source()
				}
				_ = tx.Respond(res)
			})
			server.OnInvite(func(req *sip.Request, tx sip.ServerTransaction) {
				res := sip.NewResponseFromRequest(req, 200, "OK", []byte(sipTestSDP(media, "0", "PCMU", "")))
				res.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
				res.AppendHeader(sip.NewHeader("Contact", "<sip:2002@"+listener.Addr().String()+";transport="+tc.transport+">"))
				_ = tx.Respond(res)
			})
			server.OnAck(func(req *sip.Request, _ sip.ServerTransaction) { acked <- req.Source() })
			server.OnBye(func(req *sip.Request, tx sip.ServerTransaction) {
				ended <- req.Source()
				_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
			})
			if tc.transport == "tls" {
				go func() {
					_ = server.ServeTLS(tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}}))
				}()
			} else {
				go func() { _ = server.ServeTCP(listener) }()
			}

			app := testDiagnosticServer(t, t.TempDir())
			input, _ := json.Marshal(sipphone.Config{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port,
				User: "2001", Transport: tc.transport, TLSCAPEM: tc.ca, TLSServerName: tc.serverName, Password: "fixture"})
			diagnosticRequest(t, app, "POST", sipAPI+"/start", 202, input)
			if tc.failure != "" {
				state := waitSIPPhone(t, app, "failed", "")
				if state.SignalStage != "tls_handshake" || state.SignalFailure != tc.failure || len(registered) != 0 {
					t.Fatalf("wrong TLS stage: %+v", state)
				}
				return
			}
			state := waitSIPPhone(t, app, "registered", "")
			if state.SignalStage != "registered" || state.Transport != tc.transport || state.LocalAddress == "" {
				t.Fatalf("wrong register stage: %+v", state)
			}
			hadDigest := false
			for _, event := range state.Events {
				hadDigest = hadDigest || event.Message == "digest"
			}
			if !hadDigest {
				t.Fatalf("Digest stage missing: %+v", state.Events)
			}
			source := <-registered
			diagnosticRequest(t, app, "POST", sipAPI+"/dial", 202, []byte(`{"target":"2002"}`))
			waitSIPPhone(t, app, "registered", "incall")
			select {
			case addr := <-acked:
				if addr != source {
					t.Fatalf("ACK used a new connection: %s", addr)
				}
			case <-time.After(time.Second):
				t.Fatal("missing ACK")
			}
			media.SetReadDeadline(time.Now().Add(time.Second))
			if _, _, err := media.ReadFromUDP(make([]byte, 512)); err != nil {
				t.Fatal(err)
			}
			diagnosticRequest(t, app, "POST", sipAPI+"/hangup", 202, nil)
			select {
			case addr := <-ended:
				if addr != source {
					t.Fatalf("BYE used a new connection: %s", addr)
				}
			case <-time.After(time.Second):
				t.Fatal("missing BYE")
			}
			if _, _, err := net.SplitHostPort(state.LocalAddress); err != nil {
				t.Fatal(err)
			}
			waitSIPPhone(t, app, "registered", "ended")
			client, err := sipgo.NewClient(pbx, sipgo.WithClientAddr(listener.Addr().String()))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			var recipient, contact sip.Uri
			if sip.ParseUri("sip:2001@"+source, &recipient) != nil || sip.ParseUri("sip:2002@"+listener.Addr().String(), &contact) != nil {
				t.Fatal("invalid loopback SIP address")
			}
			dialogs := sipgo.DialogUA{Client: client, ContactHDR: sip.ContactHeader{Address: contact}, RewriteContact: true}
			call := sip.NewRequest(sip.INVITE, recipient)
			call.SetTransport(strings.ToUpper(tc.transport))
			call.SetDestination(source)
			call.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
			call.SetBody([]byte(sipTestSDP(media, "0", "PCMU", "")))
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			dialog, err := dialogs.WriteInvite(ctx, call)
			if err != nil {
				t.Fatal(err)
			}
			defer dialog.Close()
			waitSIPPhone(t, app, "registered", "ringing")
			diagnosticRequest(t, app, "POST", sipAPI+"/answer", 202, nil)
			if err := dialog.WaitAnswer(ctx, sipgo.AnswerOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := dialog.Ack(ctx); err != nil {
				t.Fatal(err)
			}
			waitSIPPhone(t, app, "registered", "incall")
			if err := dialog.Bye(ctx); err != nil {
				t.Fatal(err)
			}
			waitSIPPhone(t, app, "registered", "ended")
		})
	}
}
