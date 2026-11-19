package appserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/mediafile"
	"streamanalyzer/internal/simulator/onvif"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/google/uuid"
	"github.com/pion/rtp"
	"golang.org/x/net/ipv4"
)

const onvifAPI = "/api/simulators/onvif"
const simulatorSOAPEnvelope = "http://www.w3.org/2003/05/soap-envelope"
const simulatorDeviceNamespace = "http://www.onvif.org/ver10/device/wsdl"
const simulatorMediaNamespace = "http://www.onvif.org/ver10/media/wsdl"

func TestONVIFSimulatorDiscoveryMediaAndRTSP(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	gb := diagnosticRequest(t, server, http.MethodGet, "/simulators", http.StatusOK, nil)
	page := diagnosticRequest(t, server, http.MethodGet, "/simulators/onvif", http.StatusOK, nil)
	if !strings.Contains(string(gb), `href="/simulators/onvif"`) || !strings.Contains(string(page), `href="/simulators"`) {
		t.Fatal("both protocol pages must link to each other")
	}
	interfaces := decodeDiagnostic[[]onvif.Address](t, diagnosticRequest(t, server, http.MethodGet, onvifAPI+"/interfaces", http.StatusOK, nil))
	nicName := ""
	for _, addr := range interfaces {
		if addr.IP == "127.0.0.1" {
			nicName = addr.Interface
			break
		}
	}
	if nicName == "" {
		t.Fatal("IPv4 loopback missing from interface choices")
	}
	loopback, err := net.InterfaceByName(nicName)
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, onvifAPI+"/start", http.StatusBadRequest, []byte(`{"bind_ip":"0.0.0.0"}`))
	occupied := openSimulatorTCP(t)
	conflict, err := json.Marshal(onvif.Config{RTSPPort: occupied.Addr().(*net.TCPAddr).Port})
	if err != nil {
		t.Fatal(err)
	}
	diagnosticRequest(t, server, http.MethodPost, onvifAPI+"/start", http.StatusServiceUnavailable, conflict)

	name := "Cam & Video <测试> " + uuid.NewString()
	config, err := json.Marshal(onvif.Config{BindIP: "127.0.0.1", Name: name})
	if err != nil {
		t.Fatal(err)
	}
	started := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, server, http.MethodPost, onvifAPI+"/start", http.StatusCreated, config))
	if !started.Active || started.Interface != nicName || started.DiscoveryAddress != "239.255.255.250:3702" ||
		!strings.HasPrefix(started.StreamURL, "rtsp://127.0.0.1:") {
		t.Fatalf("wrong ONVIF state: %+v", started)
	}
	diagnosticRequest(t, server, http.MethodPost, onvifAPI+"/start", http.StatusConflict, config)

	const discover = "http://schemas.xmlsoap.org/ws/2005/04/discovery"
	const addressing = "http://schemas.xmlsoap.org/ws/2004/08/addressing"
	var scope strings.Builder
	_ = xml.EscapeText(&scope, []byte("onvif://www.onvif.org/name/"+url.PathEscape(name)))
	probe := func(types string) string {
		return "<d:Probe><d:Types>" + types + "</d:Types><d:Scopes>onvif://www.onvif.org/type " + scope.String() + "</d:Scopes></d:Probe>"
	}
	discoverAt := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 3702}
	sendDiscovery := func(op, content string, wait time.Duration) (string, error) {
		t.Helper()
		conn := openSimulatorUDP(t)
		pc := ipv4.NewPacketConn(conn)
		defer conn.Close()
		if err := pc.SetMulticastInterface(loopback); err != nil {
			t.Fatal(err)
		}
		if err := pc.SetMulticastLoopback(true); err != nil {
			t.Fatal(err)
		}
		soap := fmt.Sprintf(`<s:Envelope xmlns:s="%s" xmlns:a="%s" xmlns:d="%s" xmlns:tns="http://www.onvif.org/ver10/network/wsdl">
<s:Header><a:Action>%s/%s</a:Action><a:MessageID>urn:uuid:simulator-discovery-test</a:MessageID></s:Header><s:Body>%s</s:Body></s:Envelope>`,
			simulatorSOAPEnvelope, addressing, discover, discover, op, content)
		if _, err := conn.WriteToUDP([]byte(soap), discoverAt); err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(wait))
		buf := make([]byte, 8192)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return "", err
			}
			match := string(buf[:n])
			if !strings.Contains(match, started.EndpointID) {
				continue
			}
			if !from.IP.Equal(net.IPv4(127, 0, 0, 1)) || from.Port != 3702 {
				t.Fatalf("discovery replied from another endpoint: %s", from)
			}
			return match, nil
		}
	}
	match, err := sendDiscovery("Probe", probe("tns:NetworkVideoTransmitter"), 2*time.Second)
	if err != nil {
		t.Fatalf("multicast ONVIF Probe did not find the simulator: %v", err)
	}
	if !strings.Contains(match, "ProbeMatches") || !strings.Contains(match, started.DeviceURL) ||
		!strings.Contains(match, started.EndpointID) || !strings.Contains(match, "<a:RelatesTo>urn:uuid:simulator-discovery-test</a:RelatesTo>") {
		t.Fatalf("wrong ProbeMatches: %s", match)
	}
	match, err = sendDiscovery("Resolve", "<d:Resolve><a:EndpointReference><a:Address>"+started.EndpointID+"</a:Address></a:EndpointReference></d:Resolve>", time.Second)
	if err != nil || !strings.Contains(match, "ResolveMatches") || !strings.Contains(match, started.DeviceURL) {
		t.Fatalf("wrong ResolveMatches: %s (%v)", match, err)
	}
	if notFound, err := sendDiscovery("Probe", probe("tns:Recorder"), 150*time.Millisecond); err == nil {
		t.Fatalf("simulator responded to an incompatible device type: %s", notFound)
	}

	client := &http.Client{Timeout: 2 * time.Second}
	info := queryONVIF(t, client, started.DeviceURL, simulatorDeviceNamespace, "GetDeviceInformation", "", http.StatusOK)
	if !strings.Contains(info, "Cam &amp; Video &lt;测试&gt;") {
		t.Fatalf("device name missing or not escaped: %s", info)
	}
	services := queryONVIF(t, client, started.DeviceURL, simulatorDeviceNamespace, "GetServices", "<o:IncludeCapability>true</o:IncludeCapability>", http.StatusOK)
	if !strings.Contains(services, started.MediaURL) || !strings.Contains(services, simulatorMediaNamespace) {
		t.Fatalf("media service was not advertised: %s", services)
	}
	media := queryONVIF(t, client, started.DeviceURL, simulatorDeviceNamespace, "GetCapabilities", "<o:Category>Media</o:Category>", http.StatusOK)
	if !strings.Contains(media, started.MediaURL) || !strings.Contains(media, "<tt:RTP_RTSP_TCP>true</tt:RTP_RTSP_TCP>") ||
		strings.Contains(media, started.DeviceURL) {
		t.Fatalf("wrong media capabilities: %s", media)
	}
	profiles := queryONVIF(t, client, started.MediaURL, simulatorMediaNamespace, "GetProfiles", "", http.StatusOK)
	var profile struct {
		Body struct {
			Response struct {
				Profiles []struct {
					Token                     string `xml:"token,attr"`
					VideoEncoderConfiguration struct {
						Encoding   string
						Resolution struct{ Width, Height int }
					}
				}
			} `xml:"GetProfilesResponse"`
		}
	}
	if err := xml.Unmarshal([]byte(profiles), &profile); err != nil {
		t.Fatal(err)
	}
	if len(profile.Body.Response.Profiles) != 2 {
		t.Fatalf("expected two profiles: %s", profiles)
	}
	advertised := profile.Body.Response.Profiles[0]
	encoder := advertised.VideoEncoderConfiguration
	if advertised.Token == "" || encoder.Encoding != "H264" || encoder.Resolution.Width != 320 || encoder.Resolution.Height != 180 {
		t.Fatalf("wrong media profile: %s", profiles)
	}
	configs := queryONVIF(t, client, started.MediaURL, simulatorMediaNamespace, "GetVideoEncoderConfigurations", "", http.StatusOK)
	if !strings.Contains(configs, "<tt:FrameRateLimit>15</tt:FrameRateLimit>") {
		t.Fatalf("wrong video encoder configuration: %s", configs)
	}
	fault := queryONVIF(t, client, started.MediaURL, simulatorMediaNamespace, "GetStreamUri", "<o:ProfileToken>bad</o:ProfileToken>", http.StatusBadRequest)
	if !strings.Contains(fault, "ter:InvalidArgVal") {
		t.Fatalf("unknown profile should return a SOAP fault: %s", fault)
	}
	uriBody := queryONVIF(t, client, started.MediaURL, simulatorMediaNamespace, "GetStreamUri",
		"<o:StreamSetup><tt:Stream>RTP-Unicast</tt:Stream><tt:Transport><tt:Protocol>RTSP</tt:Protocol></tt:Transport></o:StreamSetup>"+
			"<o:ProfileToken>"+advertised.Token+"</o:ProfileToken>", http.StatusOK)
	_, uri, _ := strings.Cut(uriBody, "<tt:Uri>")
	uri, _, _ = strings.Cut(uri, "</tt:Uri>")
	if uri != started.StreamURL {
		t.Fatalf("unexpected GetStreamUri value: %s", uriBody)
	}
	for _, protocol := range []gortsplib.Protocol{gortsplib.ProtocolTCP, gortsplib.ProtocolUDP} {
		t.Run(protocol.String(), func(t *testing.T) { receiveONVIFVideo(t, uri, protocol, 320, 180, 6000) })
	}
	stopped := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, server, http.MethodPost, onvifAPI+"/stop", http.StatusOK, nil))
	if stopped.Active || stopped.DiscoveryReplies != 2 || stopped.SOAPRequests < 7 || stopped.RTSPPlays != 2 {
		t.Fatalf("wrong final ONVIF stats: %+v", stopped)
	}
	address, err := base.ParseURL(uri)
	if err != nil {
		t.Fatal(err)
	}
	if conn, err := net.DialTimeout("tcp4", address.Host, time.Second); err == nil {
		conn.Close()
		t.Fatal("RTSP socket remained open after stop")
	}
	if response, err := client.Get(started.DeviceURL); err == nil {
		response.Body.Close()
		t.Fatal("ONVIF device socket remained open after stop")
	}
}

func TestONVIFLocalSubProfileAndRTSPDigest(t *testing.T) {
	if _, err := mediafile.FindTool("ffmpeg", ""); err != nil {
		t.Skip(err)
	}
	source, err := os.ReadFile("../simulator/hlsorigin/media/segment-01.ts")
	if err != nil {
		t.Fatal(err)
	}
	server := testDiagnosticServer(t, t.TempDir())
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("config", `{"rtsp_username":"cam","rtsp_password":"only-digest-42"}`); err != nil {
		t.Fatal(err)
	}
	file, err := form.CreateFormFile("sub", "local-test.ts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(source); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Post(server.URL()+onvifAPI+"/start-file", form.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	result, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusCreated {
		t.Fatalf("upload did not start ONVIF: %s (%v)", result, err)
	}
	started := decodeDiagnostic[onvif.Status](t, result)
	if !started.RTSPDigest || len(started.Profiles) != 2 || started.Profiles[0].Filename != "" ||
		started.Profiles[1].Filename != "local-test.ts" || bytes.Contains(result, []byte("only-digest-42")) {
		t.Fatalf("wrong profile/source state: %s", result)
	}
	for _, profile := range started.Profiles {
		soap := queryONVIF(t, client, started.MediaURL, simulatorMediaNamespace, "GetStreamUri",
			"<o:ProfileToken>"+profile.Token+"</o:ProfileToken>", http.StatusOK)
		if !strings.Contains(soap, "<tt:Uri>"+profile.StreamURL+"</tt:Uri>") || strings.Contains(soap, "only-digest-42") {
			t.Fatalf("wrong stream URI for %s: %s", profile.Token, soap)
		}
	}
	url, err := base.ParseURL(started.Profiles[1].StreamURL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp4", url.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(time.Second))
	fmt.Fprintf(conn, "DESCRIBE %s RTSP/1.0\r\nCSeq: 1\r\nAccept: application/sdp\r\n\r\n", url)
	reader := bufio.NewReader(conn)
	var challenge strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
		challenge.WriteString(line)
	}
	conn.Close()
	if !strings.Contains(challenge.String(), "401 Unauthorized") ||
		!strings.Contains(challenge.String(), "WWW-Authenticate: Digest ") || strings.Contains(challenge.String(), "Basic ") {
		t.Fatalf("expected an RTSP Digest challenge: %s", challenge.String())
	}
	for _, profile := range started.Profiles {
		authenticated := strings.Replace(profile.StreamURL, "rtsp://", "rtsp://cam:only-digest-42@", 1)
		receiveONVIFVideo(t, authenticated, gortsplib.ProtocolTCP, profile.Width, profile.Height, uint32(90000/profile.FPS))
	}
	diagnosticRequest(t, server, http.MethodPost, onvifAPI+"/stop", http.StatusOK, nil)
}

func queryONVIF(t *testing.T, client *http.Client, address, namespace, op, payload string, status int) string {
	t.Helper()
	request := fmt.Sprintf(`<s:Envelope xmlns:s="%s" xmlns:o="%s" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><o:%s>%s</o:%s></s:Body></s:Envelope>`,
		simulatorSOAPEnvelope, namespace, op, payload, op)
	response, err := client.Post(address, "application/soap+xml", strings.NewReader(request))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/soap+xml") {
		t.Fatalf("%s returned HTTP %d: %s", op, response.StatusCode, body)
	}
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("malformed SOAP response for %s: %v", op, err)
		}
	}
	return string(body)
}

func receiveONVIFVideo(t *testing.T, uri string, protocol gortsplib.Protocol, width, height int, step uint32) {
	t.Helper()
	u, err := base.ParseURL(uri)
	if err != nil {
		t.Fatal(err)
	}
	client := &gortsplib.Client{Scheme: u.Scheme, Host: u.Host, Protocol: &protocol, ReadTimeout: 4 * time.Second, WriteTimeout: 2 * time.Second}
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	desc, _, err := client.Describe(u)
	if err != nil {
		t.Fatal(err)
	}
	var video *format.H264
	media := desc.FindFormat(&video)
	if media == nil || len(video.SPS) == 0 || len(video.PPS) == 0 {
		t.Fatal("RTSP SDP missing H.264 SPS/PPS")
	}
	var sps h264.SPS
	if err := sps.Unmarshal(video.SPS); err != nil || sps.Width() != width || sps.Height() != height {
		t.Fatalf("RTSP SPS does not match ONVIF resolution: %d x %d (%v)", sps.Width(), sps.Height(), err)
	}
	decoder, err := video.CreateDecoder()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetupAll(desc.BaseURL, desc.Medias); err != nil {
		t.Fatal(err)
	}
	received := make(chan uint32, 8)
	synced := false
	client.OnPacketRTP(media, video, func(packet *rtp.Packet) {
		accessUnit, err := decoder.Decode(packet)
		if err != nil {
			return
		}
		if h264.IsRandomAccess(accessUnit) {
			synced = true
		}
		if synced {
			select {
			case received <- packet.Timestamp:
			default:
			}
		}
	})
	if _, err := client.Play(nil); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(4 * time.Second)
	var previous uint32
	for i := range 3 {
		select {
		case timestamp := <-received:
			if i > 0 && timestamp-previous != step {
				t.Fatalf("unexpected RTP timestamp delta %d", timestamp-previous)
			}
			previous = timestamp
		case <-timeout:
			t.Fatalf("no decodable H.264 access units over RTSP %s", protocol)
		}
	}
}
