package appserver

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/onvif"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/pion/rtp"
)

func TestONVIFSOAPClockFaultAndRTSPUDPBlackhole(t *testing.T) {
	const username = "soap-test"
	const password = "soap-fixture-password"
	s := testDiagnosticServer(t, t.TempDir())
	started := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, s, http.MethodPost, onvifAPI+"/start", http.StatusCreated,
		[]byte(`{"soap_username":"soap-test","soap_password":"soap-fixture-password","clock_offset_seconds":600,"silent_udp":true}`)))
	client := &http.Client{Timeout: time.Second}
	token := func(secret, nonce string, created time.Time) string {
		text := created.UTC().Format(time.RFC3339)
		digest := sha1.Sum([]byte(nonce + text + secret))
		return fmt.Sprintf(`<s:Header><w:Security xmlns:w="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:u="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"><w:UsernameToken><w:Username>%s</w:Username><w:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</w:Password><w:Nonce>%s</w:Nonce><u:Created>%s</u:Created></w:UsernameToken></w:Security></s:Header>`,
			username, base64.StdEncoding.EncodeToString(digest[:]), base64.StdEncoding.EncodeToString([]byte(nonce)), text)
	}
	profiles := func(headers string, want int) string {
		t.Helper()
		body := fmt.Sprintf(`<s:Envelope xmlns:s="%s" xmlns:o="%s">%s<s:Body><o:GetProfiles/></s:Body></s:Envelope>`, simulatorSOAPEnvelope, simulatorMediaNamespace, headers)
		response, err := client.Post(started.MediaURL, "application/soap+xml", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != want {
			t.Fatalf("expected SOAP HTTP %d, got %d %s %v", want, response.StatusCode, data, err)
		}
		return string(data)
	}
	profiles("", http.StatusUnauthorized)
	profiles(token("different-password", "wrong", time.Now().Add(10*time.Minute)), http.StatusUnauthorized)
	skew := profiles(token(password, "stale", time.Now()), http.StatusUnauthorized)
	if !strings.Contains(skew, "outside the device clock window") {
		t.Fatalf("wrong rejection: %s", skew)
	}
	clock := queryONVIF(t, client, started.DeviceURL, simulatorDeviceNamespace, "GetSystemDateAndTime", "", http.StatusOK)
	if !strings.Contains(clock, "UTCDateTime") {
		t.Fatal("device clock unavailable before authentication")
	}
	synced := token(password, "valid", time.Now().Add(10*time.Minute))
	if got := profiles(synced, http.StatusOK); !strings.Contains(got, "profile-1") {
		t.Fatal(got)
	}
	profiles(synced, http.StatusUnauthorized)
	status := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, s, http.MethodGet, onvifAPI, http.StatusOK, nil))
	if status.SOAPAuthRejected != 4 || status.SOAPClockRejected != 1 || status.ClockOffset != 600 {
		t.Fatalf("wrong SOAP diagnostics: %+v", status)
	}

	u, err := base.ParseURL(started.StreamURL)
	if err != nil {
		t.Fatal(err)
	}
	udp := &gortsplib.Client{Scheme: u.Scheme, Host: u.Host, ReadTimeout: 2 * time.Second, WriteTimeout: time.Second, InitialUDPReadTimeout: 650 * time.Millisecond}
	if err := udp.Start(); err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	desc, _, err := udp.Describe(u)
	if err != nil {
		t.Fatal(err)
	}
	if err := udp.SetupAll(desc.BaseURL, desc.Medias); err != nil {
		t.Fatal(err)
	}
	packets := make(chan struct{}, 1)
	udp.OnPacketRTPAny(func(_ *description.Media, _ format.Format, _ *rtp.Packet) {
		select {
		case packets <- struct{}{}:
		default:
		}
	})
	if _, err := udp.Play(nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-packets:
		t.Fatal("silent UDP stream emitted RTP")
	case <-time.After(400 * time.Millisecond):
	}
	select {
	case <-packets:
	case <-time.After(3 * time.Second):
		t.Fatal("automatic transport fallback did not receive TCP RTP")
	}
	udp.Close()
	status = decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, s, http.MethodGet, onvifAPI, http.StatusOK, nil))
	if status.UDPPlays != 1 || status.TCPPlays != 1 || status.RTSPPlays != 2 || !status.SilentUDP {
		t.Fatalf("wrong transport diagnostics: %+v", status)
	}
}
