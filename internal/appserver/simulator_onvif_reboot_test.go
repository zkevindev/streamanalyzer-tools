package appserver

import (
	"encoding/json"
	"encoding/xml"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/onvif"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/google/uuid"
)

func TestONVIFDeviceRebootRediscoveryAndRTSP(t *testing.T) {
	app := testDiagnosticServer(t, t.TempDir())
	config, err := json.Marshal(onvif.Config{BindIP: "127.0.0.1", Name: "Reboot " + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	old := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, app, "POST", onvifAPI+"/start", 201, config))
	interfaceInfo, err := net.InterfaceByName(old.Interface)
	if err != nil {
		t.Fatal(err)
	}
	group, err := net.ResolveUDPAddr("udp4", old.DiscoveryAddress)
	if err != nil {
		t.Fatal(err)
	}
	watch, err := net.ListenMulticastUDP("udp4", interfaceInfo, group)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	receiveONVIFVideo(t, old.StreamURL, gortsplib.ProtocolTCP, 320, 180, 6000)
	client := &http.Client{Timeout: time.Second}
	input, _ := json.Marshal(map[string]string{"device_url": old.DeviceURL})
	fresh := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, app, "POST", onvifAPI+"/reboot", 200, input))
	if fresh.DeviceURL == old.DeviceURL || fresh.StreamURL == old.StreamURL || fresh.EndpointID != old.EndpointID ||
		fresh.Reboot == nil || fresh.Reboot.PreviousDeviceURL != old.DeviceURL || fresh.Reboot.PreviousStreamURL != old.StreamURL ||
		fresh.Reboot.ByeSentAt.IsZero() || !fresh.Reboot.HelloSentAt.After(fresh.Reboot.ByeSentAt) {
		t.Fatalf("wrong reboot observation: %+v %+v", fresh, fresh.Reboot)
	}
	// A client ages out its old address on Bye and stores the replacement on Hello.
	cache := map[string]string{old.EndpointID: old.DeviceURL}
	watch.SetReadDeadline(time.Now().Add(2 * time.Second))
	packet := make([]byte, 8192)
	var previousInstance uint32
	for cache[old.EndpointID] != fresh.DeviceURL {
		size, source, err := watch.ReadFromUDP(packet)
		if err != nil {
			t.Fatal("did not rediscover the rebooted device", err)
		}
		var announcement struct {
			Header struct {
				Action      string
				AppSequence struct {
					Instance uint32 `xml:"InstanceId,attr"`
					Number   uint32 `xml:"MessageNumber,attr"`
				}
			}
			Body struct {
				Message struct {
					XMLName  xml.Name
					XAddrs   string
					Endpoint string `xml:"EndpointReference>Address"`
				} `xml:",any"`
			}
		}
		if xml.Unmarshal(packet[:size], &announcement) != nil || announcement.Body.Message.Endpoint != old.EndpointID {
			continue
		}
		op := announcement.Body.Message
		appSequence := announcement.Header.AppSequence
		if source.Port != 3702 || !source.IP.IsLoopback() || op.XMLName.Space+"/"+op.XMLName.Local != announcement.Header.Action || appSequence.Number == 0 {
			t.Fatalf("unexpected discovery announcement: %s from %s", packet[:size], source)
		}
		switch op.XMLName.Local {
		case "Bye":
			previousInstance = appSequence.Instance
			delete(cache, op.Endpoint)
		case "Hello":
			if cache[op.Endpoint] != "" || previousInstance != fresh.Reboot.PreviousInstance ||
				appSequence.Instance <= previousInstance || appSequence.Instance != fresh.Reboot.Instance {
				t.Fatalf("Hello did not follow Bye with a new instance: %s", packet[:size])
			}
			cache[op.Endpoint] = op.XAddrs
		}
	}
	xaddr := cache[old.EndpointID]
	services := queryONVIF(t, client, xaddr, simulatorDeviceNamespace, "GetServices", "", http.StatusOK)
	if !strings.Contains(services, fresh.MediaURL) {
		t.Fatal("rediscovered device did not advertise its current media service")
	}
	stream := queryONVIF(t, client, fresh.MediaURL, simulatorMediaNamespace, "GetStreamUri",
		"<o:ProfileToken>"+old.Profiles[0].Token+"</o:ProfileToken>", http.StatusOK)
	_, uri, _ := strings.Cut(stream, "<tt:Uri>")
	uri, _, _ = strings.Cut(uri, "</tt:Uri>")
	if uri != fresh.StreamURL {
		t.Fatalf("client did not resolve the new RTSP endpoint: %s", stream)
	}
	receiveONVIFVideo(t, uri, gortsplib.ProtocolTCP, 320, 180, 6000)
	for _, former := range []string{old.DeviceURL, old.StreamURL} {
		address, err := url.Parse(former)
		if err != nil {
			t.Fatal(err)
		}
		if socket, err := net.DialTimeout("tcp4", address.Host, time.Second); err == nil {
			socket.Close()
			t.Fatalf("old endpoint %s is still listening", former)
		}
	}
	diagnosticRequest(t, app, "POST", onvifAPI+"/reboot", http.StatusConflict, input)
}
