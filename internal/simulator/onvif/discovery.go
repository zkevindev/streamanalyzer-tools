package onvif

import (
	"encoding/xml"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/ipv4"
)

const (
	discoveryNS05     = "http://schemas.xmlsoap.org/ws/2005/04/discovery"
	discoveryNS09     = "http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01"
	discoveryDeviceNS = "http://www.onvif.org/ver10/network/wsdl"
	discoveryPort     = 3702
)

type discoveryDialect struct {
	ns         string
	addressing string
	anonymous  string
	matchRule  string
}

type discoveryServer struct {
	listener       *net.UDPConn
	group          *net.UDPAddr
	ip             net.IP
	interfaceIndex int
	instance       uint32
	sequence       atomic.Uint32
}

func openDiscovery(ip net.IP, nic *net.Interface) (*discoveryServer, error) {
	group := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: discoveryPort}
	listener, err := net.ListenMulticastUDP("udp4", nic, group)
	if err != nil {
		return nil, err
	}
	return &discoveryServer{listener: listener, group: group, ip: ip, interfaceIndex: nic.Index}, nil
}

func (d *discoveryServer) close() {
	d.listener.Close()
}

func (d *discoveryServer) serve(c *camera) {
	listener := ipv4.NewPacketConn(d.listener)
	// Some systems don't provide packet interface metadata on UDP sockets.
	// In that case the multicast membership is still scoped to the chosen NIC.
	_ = listener.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst, true)
	// Read the complete datagram on platforms that return an error on truncation.
	buf := make([]byte, 65535)
	outgoing := &ipv4.ControlMessage{Src: d.ip, IfIndex: d.interfaceIndex}
	for {
		n, control, src, err := listener.ReadFrom(buf)
		if err != nil {
			return
		}
		if n > 8<<10 {
			continue
		}
		if control != nil {
			if control.IfIndex != 0 && control.IfIndex != d.interfaceIndex {
				continue
			}
			if control.Dst != nil && !control.Dst.Equal(d.group.IP) && !control.Dst.Equal(d.ip) {
				continue
			}
		}
		response := d.match(c, buf[:n])
		if response != "" {
			_ = d.listener.SetWriteDeadline(time.Now().Add(time.Second))
			if _, err := listener.WriteTo([]byte(response), outgoing, src); err == nil {
				c.discoveryReplies.Add(1)
			}
		}
	}
}

func (d *discoveryServer) announce(c *camera, op string) time.Time {
	listener := ipv4.NewPacketConn(d.listener)
	if err := listener.SetMulticastInterface(c.nic); err != nil {
		return time.Time{}
	}
	if err := listener.SetMulticastLoopback(true); err != nil {
		return time.Time{}
	}
	if err := listener.SetMulticastTTL(1); err != nil {
		return time.Time{}
	}

	// ONVIF clients use the WS-Discovery 2005 dialect for multicast.
	header := "<a:Action>" + discoveryNS05 + "/" + op + "</a:Action><a:MessageID>urn:uuid:" +
		uuid.NewString() + "</a:MessageID><a:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</a:To>" + d.appSequence()
	body := "<d:" + op + ">" + c.discoveryEndpoint(op == "Hello") + "</d:" + op + ">"
	message := soapMessage(discoveryNamespaces(addressNS04, discoveryNS05), header, body)
	_ = d.listener.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := listener.WriteTo([]byte(message), &ipv4.ControlMessage{Src: d.ip, IfIndex: d.interfaceIndex}, d.group); err != nil {
		return time.Time{}
	}
	return time.Now()
}

func (d *discoveryServer) appSequence() string {
	return fmt.Sprintf(`<d:AppSequence InstanceId="%d" MessageNumber="%d"/>`, d.instance, d.sequence.Add(1))
}

func (c *camera) discoveryEndpoint(metadata bool) string {
	address := "<a:EndpointReference><a:Address>" + c.status.EndpointID + "</a:Address></a:EndpointReference>"
	if metadata {
		address += "<d:Types>dn:NetworkVideoTransmitter tds:Device</d:Types><d:Scopes>" + xmlText(strings.Join(c.scopes(), " ")) +
			"</d:Scopes><d:XAddrs>" + c.status.DeviceURL + "</d:XAddrs><d:MetadataVersion>1</d:MetadataVersion>"
	}
	return address
}

func discoveryNamespaces(addressing, namespace string) string {
	return ` xmlns:a="` + addressing + `" xmlns:d="` + namespace + `" xmlns:dn="` + discoveryDeviceNS + `" xmlns:tds="` + deviceNS + `"`
}

func (d *discoveryServer) match(c *camera, body []byte) string {
	envelope, err := readEnvelope(body)
	if err != nil {
		return ""
	}
	op := envelope.Body.Operations[0]
	var version discoveryDialect
	switch op.XMLName.Space {
	case discoveryNS05:
		version = discoveryDialect{discoveryNS05, addressNS04, addressNS04 + "/role/anonymous", discoveryNS05 + "/rfc2396"}
	case discoveryNS09:
		version = discoveryDialect{discoveryNS09, addressNS05, addressNS05 + "/anonymous", discoveryNS09 + "/rfc3986"}
	default:
		return ""
	}
	id := strings.TrimSpace(envelope.Header.MessageID.Text)
	if id == "" || len(id) > 512 || envelope.Header.MessageID.XMLName.Space != version.addressing {
		return ""
	}
	if action := strings.TrimSpace(envelope.Header.Action.Text); action != "" &&
		(action != version.ns+"/"+op.XMLName.Local || envelope.Header.Action.XMLName.Space != version.addressing) {
		return ""
	}

	scopes := c.scopes()
	var elementName string
	switch op.XMLName.Local {
	case "Probe":
		if !supportedTypes(op.Types, op.Attrs, envelope.Body.Attrs, envelope.Attrs) ||
			!matchingScopes(scopes, op.Scopes, version.matchRule) {
			return ""
		}
		elementName = "ProbeMatch"
	case "Resolve":
		if strings.TrimSpace(op.EndpointReference.Address) != c.status.EndpointID {
			return ""
		}
		elementName = "ResolveMatch"
	default:
		return ""
	}

	action := version.ns + "/" + elementName + "es"
	header := "<a:Action>" + action + "</a:Action><a:MessageID>urn:uuid:" + uuid.NewString() +
		"</a:MessageID><a:RelatesTo>" + xmlText(id) + "</a:RelatesTo><a:To>" + version.anonymous + "</a:To>" +
		d.appSequence()
	content := "<d:" + elementName + "es><d:" + elementName + ">" + c.discoveryEndpoint(true) +
		"</d:" + elementName + "></d:" + elementName + "es>"
	return soapMessage(discoveryNamespaces(version.addressing, version.ns), header, content)
}

func supportedTypes(types elementText, operationAttrs, bodyAttrs, envelopeAttrs []xml.Attr) bool {
	for _, qualified := range strings.Fields(types.Text) {
		prefix, local, hasPrefix := strings.Cut(qualified, ":")
		ns := ""
		if hasPrefix {
			ns = namespaceFor(prefix, types.Attrs, operationAttrs, bodyAttrs, envelopeAttrs)
			if ns == "" {
				return false
			}
		} else {
			local = qualified
		}
		switch local {
		case "NetworkVideoTransmitter":
			if ns != "" && ns != discoveryDeviceNS {
				return false
			}
		case "Device":
			if ns != "" && ns != deviceNS {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func namespaceFor(prefix string, levels ...[]xml.Attr) string {
	for _, attrs := range levels {
		for _, attr := range attrs {
			if attr.Name.Space == "xmlns" && attr.Name.Local == prefix {
				return attr.Value
			}
		}
	}
	return ""
}

func matchingScopes(available []string, query elementText, matchRule string) bool {
	for _, attr := range query.Attrs {
		if attr.Name.Local == "MatchBy" && attr.Value != "" && attr.Value != matchRule {
			return false
		}
	}
	for _, q := range strings.Fields(query.Text) {
		found := false
		q = strings.TrimRight(q, "/")
		for _, scope := range available {
			if scope == q || strings.HasPrefix(scope, q+"/") {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
