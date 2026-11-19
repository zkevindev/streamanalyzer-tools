package onvifdiscovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/ipv4"
)

const soapNS = "http://www.w3.org/2003/05/soap-envelope"
const deviceNS = "http://www.onvif.org/ver10/device/wsdl"
const mediaNS = "http://www.onvif.org/ver10/media/wsdl"
const schemaNS = "http://www.onvif.org/ver10/schema"

type dialect struct{ discovery, addressing string }

var dialects = []dialect{{"http://schemas.xmlsoap.org/ws/2005/04/discovery", "http://schemas.xmlsoap.org/ws/2004/08/addressing"}, {"http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01", "http://www.w3.org/2005/08/addressing"}}

type probeMatch struct {
	Attrs     []xml.Attr `xml:",any,attr"`
	Reference struct {
		Address string `xml:"Address"`
	} `xml:"EndpointReference"`
	Types struct {
		Text  string     `xml:",chardata"`
		Attrs []xml.Attr `xml:",any,attr"`
	} `xml:"Types"`
	Scopes string `xml:"Scopes"`
	XAddrs string `xml:"XAddrs"`
}

type probeEnvelope struct {
	XMLName xml.Name   `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Attrs   []xml.Attr `xml:",any,attr"`
	Header  struct {
		Action struct {
			XMLName xml.Name
			Text    string `xml:",chardata"`
		}
		RelatesTo struct {
			XMLName xml.Name
			Text    string `xml:",chardata"`
		}
	}
	Body struct {
		Attrs   []xml.Attr `xml:",any,attr"`
		Matches struct {
			XMLName xml.Name
			Attrs   []xml.Attr   `xml:",any,attr"`
			Items   []probeMatch `xml:"ProbeMatch"`
		} `xml:"ProbeMatches"`
	}
}

func decodeXML(data []byte, value any) error {
	if len(data) > 1<<20 || bytes.Contains(bytes.ToUpper(data), []byte("<!DOCTYPE")) {
		return invalid("设备返回的 XML 超出范围或格式不受支持")
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(value); err != nil {
		return invalid("设备返回的 XML 无法解析")
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return invalid("设备返回的 XML 无法解析")
		}
		switch v := token.(type) {
		case xml.Comment:
			continue
		case xml.CharData:
			if strings.TrimSpace(string(v)) == "" {
				continue
			}
		}
		return invalid("设备返回了多余的 XML 内容")
	}
}

func probeMessage(version dialect, id string) []byte {
	to := "urn:schemas-xmlsoap-org:ws:2005:04:discovery"
	if version.discovery == dialects[1].discovery {
		to = "urn:docs-oasis-open-org:ws-dd:ns:discovery:2009:01"
	}
	return []byte(fmt.Sprintf(`<s:Envelope xmlns:s="%s" xmlns:a="%s" xmlns:d="%s"><s:Header><a:Action>%s/Probe</a:Action><a:MessageID>%s</a:MessageID><a:To>%s</a:To></s:Header><s:Body><d:Probe/></s:Body></s:Envelope>`, soapNS, version.addressing, version.discovery, version.discovery, id, to))
}

func onvifTypes(value string, levels ...[]xml.Attr) bool {
	for _, name := range strings.Fields(value) {
		prefix, local, found := strings.Cut(name, ":")
		if !found {
			continue
		}
		ns := ""
		for _, attrs := range levels {
			for _, attr := range attrs {
				if attr.Name.Space == "xmlns" && attr.Name.Local == prefix {
					ns = attr.Value
					break
				}
			}
			if ns != "" {
				break
			}
		}
		if ns == deviceNS && local == "Device" || ns == "http://www.onvif.org/ver10/network/wsdl" && (local == "NetworkVideoTransmitter" || local == "NetworkVideoStorage") {
			return true
		}
	}
	return false
}

func parseProbe(data []byte, issued map[string]dialect, peer net.IP, nic Interface) ([]Device, map[string][]string) {
	devices := []Device{}
	addresses := make(map[string][]string)
	_, prefix, _ := net.ParseCIDR(nic.Prefix)
	if len(data) > 32<<10 || !unicast(peer) || prefix == nil || !prefix.Contains(peer) {
		return devices, addresses
	}
	var envelope probeEnvelope
	if decodeXML(data, &envelope) != nil {
		return devices, addresses
	}
	version, ok := issued[strings.TrimSpace(envelope.Header.RelatesTo.Text)]
	if !ok || envelope.Header.RelatesTo.XMLName.Space != version.addressing || envelope.Header.Action.XMLName.Space != version.addressing || strings.TrimSpace(envelope.Header.Action.Text) != version.discovery+"/ProbeMatches" || envelope.Body.Matches.XMLName.Space != version.discovery {
		return devices, addresses
	}
	for index, match := range envelope.Body.Matches.Items {
		if index >= 32 {
			break
		}
		endpoint := strings.TrimSpace(match.Reference.Address)
		if endpoint == "" || len(endpoint) > 512 || len(match.Scopes) > 8192 || len(match.XAddrs) > 16<<10 {
			continue
		}
		// An empty Probe covers cameras and NVRs whose advertised device types
		// differ. Restrict results to devices advertising an ONVIF scope/type.
		if !strings.Contains(match.Scopes, "onvif://www.onvif.org/") && !onvifTypes(match.Types.Text, match.Types.Attrs, match.Attrs, envelope.Body.Matches.Attrs, envelope.Body.Attrs, envelope.Attrs) {
			continue
		}
		var urls []string
		for _, candidate := range strings.Fields(match.XAddrs) {
			if len(urls) == 4 {
				break
			}
			value, err := serviceURL(candidate)
			if err != nil {
				continue
			}
			if ip := net.ParseIP(value.Hostname()); ip != nil && !ip.Equal(peer) {
				continue
			}
			urls = append(urls, value.String())
		}
		if len(urls) == 0 {
			continue
		}
		hash := sha256.Sum256([]byte(peer.String() + "\x00" + endpoint))
		id := hex.EncodeToString(hash[:16])
		name := ""
		for _, scope := range strings.Fields(match.Scopes) {
			if strings.HasPrefix(scope, "onvif://www.onvif.org/name/") {
				decoded, err := url.PathUnescape(strings.TrimPrefix(scope, "onvif://www.onvif.org/name/"))
				if err == nil && len(decoded) <= 256 && !strings.ContainsAny(decoded, "\x00\r\n") {
					name = decoded
				}
				break
			}
		}
		types := strings.Fields(match.Types.Text)
		if len(types) > 8 {
			types = types[:8]
		}
		for i := range types {
			if len(types[i]) > 128 {
				types[i] = "unknown"
			}
		}
		devices = append(devices, Device{ID: id, EndpointID: endpoint, PeerIP: peer.String(), Name: name, ServiceURL: urls[0], Types: types})
		addresses[id] = urls
	}
	return devices, addresses
}

func (m *Manager) discover(ctx context.Context, state *scanState) error {
	nic, err := net.InterfaceByIndex(state.result.Interface.Index)
	if err != nil {
		return &Error{Code: "interface_error", Message: "所选网卡已不可用"}
	}
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(state.result.Interface.IP), Port: 0})
	if err != nil {
		return &Error{Code: "bind_error", Message: "无法绑定所选网卡的 UDP 发现端口"}
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { connection.Close() })
	defer stop()
	packet := ipv4.NewPacketConn(connection)
	if packet.SetMulticastInterface(nic) != nil || packet.SetMulticastTTL(1) != nil {
		return &Error{Code: "interface_error", Message: "所选网卡无法发送局域网组播"}
	}
	_ = packet.SetMulticastLoopback(true)
	_ = packet.SetControlMessage(ipv4.FlagInterface, true)
	issued := map[string]dialect{}
	for _, version := range dialects {
		id := "urn:uuid:" + uuid.NewString()
		issued[id] = version
		_ = connection.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err := connection.WriteToUDP(probeMessage(version, id), &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 3702}); err != nil {
			return &Error{Code: "discovery_error", Message: "无法在所选网卡发送 WS-Discovery Probe"}
		}
	}
	buffer := make([]byte, 65535)
	packets, totalBytes := 0, 0
	for {
		n, control, source, err := packet.ReadFrom(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return &Error{Code: "discovery_error", Message: "读取 WS-Discovery 回复失败"}
		}
		from, ok := source.(*net.UDPAddr)
		if !ok {
			continue
		}
		if control != nil && control.IfIndex != 0 && control.IfIndex != nic.Index {
			continue
		}
		packets++
		totalBytes += n
		if packets > 4096 || totalBytes > 16<<20 {
			m.mu.Lock()
			state.result.ResponseLimitReached = true
			m.mu.Unlock()
			return nil
		}
		devices, addresses := parseProbe(buffer[:n], issued, from.IP, state.result.Interface)
		m.mu.Lock()
		if len(devices) == 0 {
			state.result.IgnoredResponses++
		}
		for _, device := range devices {
			if _, seen := state.addresses[device.ID]; seen {
				continue
			}
			if len(state.result.Devices) == maxDevices {
				state.result.DroppedDevices++
				continue
			}
			state.result.Devices = append(state.result.Devices, device)
			state.addresses[device.ID] = addresses[device.ID]
		}
		m.mu.Unlock()
	}
}
