package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/appserver"
	"streamanalyzer/internal/simulator/gb28181"
)

func TestDesktopSavesSimulatorTrace(t *testing.T) {
	occupied, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.LocalAddr().(*net.UDPAddr).Port
	cfg := &appserver.Config{}
	cfg.Server.DataDir = t.TempDir()
	server, err := appserver.StartWithModel(t.Context(), "127.0.0.1:0", cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })

	api := server.URL() + "/api/simulators/gb28181"
	start := gb28181.Config{
		PlatformHost: "127.0.0.1", PlatformPort: port, PlatformID: "34020000002000000001",
		LocalIP: "127.0.0.1", LocalPort: port, DeviceID: "34020000001320000001", ChannelID: "34020000001320000002",
	}
	input, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(api+"/start", "application/json", strings.NewReader(string(input)))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("simulator start returned %s", response.Status)
	}

	path := filepath.Join(t.TempDir(), "saved-trace.json")
	shell := Shell{App: &App{server: server}, SaveFile: func(filename string) (string, error) {
		if filename != "gb28181-trace.json" {
			t.Fatalf("unexpected suggested filename: %s", filename)
		}
		return path, nil
	}}
	for deadline := time.Now().Add(time.Second); ; time.Sleep(10 * time.Millisecond) {
		req := httptest.NewRequest(http.MethodPost, "/save-artifact", strings.NewReader("{\"name\":\"gb28181-trace.json\"}"))
		res := httptest.NewRecorder()
		shell.ServeHTTP(res, req)
		if res.Code == http.StatusOK {
			break
		}
		if res.Code != http.StatusBadGateway || time.Now().After(deadline) {
			t.Fatalf("trace download returned %d: %s", res.Code, res.Body.String())
		}
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var trace gb28181.Status
	if err := json.Unmarshal(contents, &trace); err != nil || len(trace.Events) == 0 ||
		!strings.Contains(trace.Events[0].Summary, "绑定本机 SIP 地址失败") {
		t.Fatalf("saved artifact is missing the simulator failure: %+v (%v)", trace, err)
	}
}
