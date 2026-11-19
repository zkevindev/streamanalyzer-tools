package appserver

import (
	"bytes"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/onvif"
)

const simulatorPTZNamespace = "http://www.onvif.org/ver20/ptz/wsdl"

func TestONVIFSnapshotAndPTZ(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	started := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, server, http.MethodPost, onvifAPI+"/start", http.StatusCreated, []byte("{}")))
	client := &http.Client{Timeout: time.Second}
	for _, name := range []string{"GetServices", "GetCapabilities"} {
		services := queryONVIF(t, client, started.DeviceURL, simulatorDeviceNamespace, name, "", http.StatusOK)
		if !strings.Contains(services, started.PTZURL) {
			t.Fatalf("%s did not expose PTZ: %s", name, services)
		}
	}
	profile := started.Profiles[0]
	token := "<o:ProfileToken>" + profile.Token + "</o:ProfileToken>"
	query := func(name, body string) string {
		t.Helper()
		return queryONVIF(t, client, started.PTZURL, simulatorPTZNamespace, name, token+body, http.StatusOK)
	}
	options := queryONVIF(t, client, started.PTZURL, simulatorPTZNamespace, "GetConfigurationOptions", "<o:ConfigurationToken>ptz-config-1</o:ConfigurationToken>", http.StatusOK)
	if !strings.Contains(options, "ContinuousPanTiltVelocitySpace") {
		t.Fatalf("missing PTZ space: %s", options)
	}
	uri := queryONVIF(t, client, started.MediaURL, simulatorMediaNamespace, "GetSnapshotUri", token, http.StatusOK)
	if !strings.Contains(uri, "<tt:Uri>"+profile.SnapshotURL+"</tt:Uri>") {
		t.Fatalf("wrong snapshot URI: %s", uri)
	}
	center := getONVIFSnapshot(t, client, profile)
	sub := getONVIFSnapshot(t, client, started.Profiles[1])
	move := func(pose string) { query("AbsoluteMove", "<o:Position>"+pose+"</o:Position>") }
	move(`<tt:PanTilt x="0.7" y="-0.5"/><tt:Zoom x="0.6"/>`)
	moved := getONVIFSnapshot(t, client, profile)
	if bytes.Equal(center, moved) || !bytes.Equal(sub, getONVIFSnapshot(t, client, started.Profiles[1])) {
		t.Fatal("PTZ did not update only the selected profile snapshot")
	}
	if status := query("GetStatus", ""); !strings.Contains(status, `x="0.7" y="-0.5"`) || !strings.Contains(status, `<tt:Zoom x="0.6"`) {
		t.Fatalf("wrong PTZ position: %s", status)
	}
	preset := query("SetPreset", "<o:PresetName>Check &amp; Restore</o:PresetName>")
	if !strings.Contains(preset, "<tptz:PresetToken>preset-1</tptz:PresetToken>") || !strings.Contains(query("GetPresets", ""), "Check &amp; Restore") {
		t.Fatalf("preset was not stored: %s", preset)
	}
	presetToken := "<o:PresetToken>preset-1</o:PresetToken>"
	move(`<tt:PanTilt x="0" y="0"/><tt:Zoom x="0"/>`)
	if !bytes.Equal(center, getONVIFSnapshot(t, client, profile)) {
		t.Fatal("absolute move did not restore the center snapshot")
	}
	query("RelativeMove", `<o:Translation><tt:Zoom x="0.4"/></o:Translation>`)
	if status := query("GetStatus", ""); !strings.Contains(status, `<tt:Zoom x="0.4"`) {
		t.Fatalf("relative zoom did not update position: %s", status)
	}
	query("ContinuousMove", `<o:Velocity><tt:PanTilt x="-1" y="0"/></o:Velocity><o:Timeout>PT0.1S</o:Timeout>`)
	time.Sleep(130 * time.Millisecond)
	if status := query("GetStatus", ""); !strings.Contains(status, `x="-0.1" y="0"`) || !strings.Contains(status, "<tt:PanTilt>IDLE</tt:PanTilt>") {
		t.Fatalf("continuous move did not advance and stop at timeout: %s", status)
	}
	query("GotoPreset", presetToken)
	if !bytes.Equal(moved, getONVIFSnapshot(t, client, profile)) {
		t.Fatal("goto preset did not restore the stored snapshot")
	}
	query("RemovePreset", presetToken)
	queryONVIF(t, client, started.PTZURL, simulatorPTZNamespace, "GotoPreset", token+presetToken, http.StatusBadRequest)
}

func getONVIFSnapshot(t *testing.T, client *http.Client, profile onvif.Profile) []byte {
	t.Helper()
	res, err := client.Get(profile.SnapshotURL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("snapshot failed: %v (%s)", err, res.Status)
	}
	frame, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil || frame.Width != profile.Width || frame.Height != profile.Height {
		t.Fatalf("unexpected JPEG snapshot: %+v %v", frame, err)
	}
	return data
}
