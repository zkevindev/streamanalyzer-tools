package appserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/onvifdiscovery"
	"streamanalyzer/internal/simulator/onvif"
)

func TestONVIFDiscoveryAPIToNativeCaptureAndReplay(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	const user = "onvif-api-camera"
	const password = "onvif-api-private-password"
	// Different test processes share loopback multicast. Give this device an
	// independent identity so another fixture cannot impersonate its endpoint.
	camera := &onvif.Manager{}
	t.Cleanup(camera.Close)
	started, err := camera.Start(onvif.Config{BindIP: "127.0.0.1", Name: fmt.Sprintf("Discovery API %d", os.Getpid()), SOAPUsername: user, SOAPPassword: password, RTSPUsername: user, RTSPPassword: password, ClockOffset: 600})
	if err != nil {
		t.Fatal(err)
	}
	const api = "/api/diagnostics/onvif"
	interfaces := decodeDiagnostic[[]onvifdiscovery.Interface](t, diagnosticRequest(t, server, http.MethodGet, api+"/interfaces", http.StatusOK, nil))
	if len(interfaces) == 0 {
		t.Fatal("no discovery interface choices")
	}
	job := decodeDiagnostic[onvifdiscovery.Discovery](t, diagnosticRequest(t, server, http.MethodPost, api+"/discoveries", http.StatusAccepted, []byte(`{"bind_ip":"127.0.0.1","seconds":1}`)))
	deadline := time.Now().Add(4 * time.Second)
	for job.State == "running" && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
		job = decodeDiagnostic[onvifdiscovery.Discovery](t, diagnosticRequest(t, server, http.MethodGet, api+"/discoveries/"+job.ID, http.StatusOK, nil))
	}
	var device onvifdiscovery.Device
	for _, value := range job.Devices {
		if value.EndpointID == started.EndpointID {
			device = value
			break
		}
	}
	if job.State != "complete" || device.ID == "" {
		t.Fatalf("API search failed to discover real ONVIF simulator: %+v", job)
	}
	request, _ := json.Marshal(onvifdiscovery.ProfilesRequest{DiscoveryID: job.ID, DeviceID: device.ID, BindIP: "127.0.0.1", Credentials: onvifdiscovery.Credentials{Username: user, Password: password}})
	profileBytes := diagnosticRequest(t, server, http.MethodPost, api+"/profiles", http.StatusOK, request)
	if bytes.Contains(profileBytes, []byte(password)) || bytes.Contains(profileBytes, []byte(user)) {
		t.Fatal("ONVIF credentials returned in profile API")
	}
	profiles := decodeDiagnostic[onvifdiscovery.ProfilesResult](t, profileBytes)
	if len(profiles.Profiles) != 2 || profiles.ClockStatus != "device_utc" {
		t.Fatalf("incorrect profiles via SOAP: %+v", profiles)
	}
	request, _ = json.Marshal(onvifdiscovery.StreamRequest{SelectionID: profiles.SelectionID, ProfileToken: profiles.Profiles[0].Token, Transport: "tcp", Credentials: onvifdiscovery.Credentials{Username: user, Password: password}})
	stream := decodeDiagnostic[onvifdiscovery.StreamResult](t, diagnosticRequest(t, server, http.MethodPost, api+"/stream", http.StatusOK, request))
	u, err := url.Parse(stream.URL)
	if err != nil || u.User != nil {
		t.Fatal("stream selection returned embedded credentials")
	}
	// Only this explicit user-side action copies ONVIF credentials to RTSP.
	u.User = url.UserPassword(user, password)
	request, _ = json.Marshal(diagnostic.SampleRequest{URL: u.String(), Protocol: diagnostic.ProtocolRTSP, Seconds: 1})
	run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, "/api/diagnostics/runs", http.StatusAccepted, request))
	var detail diagnostic.Detail
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		detail = decodeDiagnostic[diagnostic.Detail](t, diagnosticRequest(t, server, http.MethodGet, "/api/diagnostics/runs/"+run.ID, http.StatusOK, nil))
		if detail.Report != nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if detail.Report == nil || detail.Report.RTSP == nil || detail.Report.RTSP.RTPPackets == 0 || detail.Run.Status != "done" {
		t.Fatalf("discovered stream did not reach native diagnostics: %+v", detail)
	}
	evidence := diagnosticRequest(t, server, http.MethodGet, "/api/diagnostics/runs/"+run.ID+"/evidence.zip", http.StatusOK, nil)
	response, err := http.Post(server.URL()+"/api/diagnostics/import", "application/zip", bytes.NewReader(evidence))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("import failed: %d %s", response.StatusCode, body)
	}
	replay := decodeDiagnostic[diagnostic.Run](t, body)
	replayed := decodeDiagnostic[diagnostic.Detail](t, diagnosticRequest(t, server, http.MethodGet, "/api/diagnostics/runs/"+replay.ID, http.StatusOK, nil))
	if !reflect.DeepEqual(replayed.Report, detail.Report) {
		t.Fatal("discovery-origin native report changed after offline import")
	}
	encoded, _ := json.Marshal(replayed.Report)
	if strings.Contains(string(encoded), password) || strings.Contains(string(encoded), user) {
		t.Fatal("discovery credentials leaked into persisted native report")
	}
	diagnosticRequest(t, server, http.MethodPost, api+"/profiles", http.StatusBadRequest, []byte(`{"unknown":true}`))
	foreign, _ := http.NewRequest(http.MethodPost, server.URL()+api+"/discoveries", strings.NewReader(`{"bind_ip":"127.0.0.1"}`))
	foreign.Header.Set("Origin", "https://untrusted.example")
	foreign.Header.Set("Content-Type", "application/json")
	denied, err := http.DefaultClient.Do(foreign)
	if err != nil {
		t.Fatal(err)
	}
	denied.Body.Close()
	if denied.StatusCode != http.StatusForbidden {
		t.Fatal("cross-origin discovery request accepted")
	}
}
