package onvif

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	soapNS      = "http://www.w3.org/2003/05/soap-envelope"
	deviceNS    = "http://www.onvif.org/ver10/device/wsdl"
	mediaNS     = "http://www.onvif.org/ver10/media/wsdl"
	ptzNS       = "http://www.onvif.org/ver20/ptz/wsdl"
	schemaNS    = "http://www.onvif.org/ver10/schema"
	errorNS     = "http://www.onvif.org/ver10/error"
	addressNS04 = "http://schemas.xmlsoap.org/ws/2004/08/addressing"
	addressNS05 = "http://www.w3.org/2005/08/addressing"
)

type elementText struct {
	XMLName xml.Name
	Text    string     `xml:",chardata"`
	Attrs   []xml.Attr `xml:",any,attr"`
}

type soapHeader struct {
	Security  soapSecurity `xml:"http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd Security"`
	MessageID elementText
	Action    elementText
}

type operation struct {
	XMLName            xml.Name
	Attrs              []xml.Attr `xml:",any,attr"`
	ProfileToken       string
	ConfigurationToken string
	IncludeCapability  bool
	NodeToken          string
	PresetToken        string
	PresetName         string
	Position           ptzVector
	Translation        ptzVector
	Velocity           ptzVector
	Timeout            string
	PanTilt            *bool
	Zoom               *bool
	Category           []string
	StreamSetup        struct {
		Stream    string
		Transport struct {
			Protocol string
			Tunnel   *struct{}
		}
	}
	Types             elementText
	Scopes            elementText
	EndpointReference struct {
		Address string
	}
}

type soapEnvelope struct {
	XMLName xml.Name   `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
	Attrs   []xml.Attr `xml:",any,attr"`
	Header  soapHeader `xml:"http://www.w3.org/2003/05/soap-envelope Header"`
	Body    struct {
		Attrs      []xml.Attr  `xml:",any,attr"`
		Operations []operation `xml:",any"`
	} `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
}

func readEnvelope(body []byte) (soapEnvelope, error) {
	var request soapEnvelope
	decoder := xml.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	if len(request.Body.Operations) != 1 {
		return request, fmt.Errorf("SOAP Body 应包含一条请求")
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return request, nil
		}
		if err != nil {
			return request, err
		}
		switch value := token.(type) {
		case xml.Comment:
			continue
		case xml.CharData:
			if strings.TrimSpace(string(value)) == "" {
				continue
			}
		}
		return request, fmt.Errorf("SOAP Envelope 后有多余内容")
	}
}

func xmlText(text string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(text))
	return escaped.String()
}

func soapMessage(namespaces, header, body string) string {
	if header != "" {
		header = "<s:Header>" + header + "</s:Header>"
	}
	return xml.Header + `<s:Envelope xmlns:s="` + soapNS + `"` + namespaces + ">" +
		header + "<s:Body>" + body + "</s:Body></s:Envelope>"
}

func addressedReply(request soapHeader, action string) (string, string) {
	ns := request.Action.XMLName.Space
	if ns == "" {
		ns = request.MessageID.XMLName.Space
	}
	anonymous := ns + "/anonymous"
	switch ns {
	case addressNS04:
		anonymous = ns + "/role/anonymous"
	case addressNS05:
	default:
		return "", ""
	}
	header := "<a:Action>" + action + "</a:Action><a:To>" + anonymous + "</a:To>"
	if id := strings.TrimSpace(request.MessageID.Text); id != "" {
		header += "<a:RelatesTo>" + xmlText(id) + "</a:RelatesTo>"
	}
	return ns, header
}

type soapFault struct {
	code    string
	message string
}

func invalidToken(message string) *soapFault {
	return &soapFault{code: "InvalidArgVal", message: message}
}

func unsupportedAction() *soapFault {
	return &soapFault{code: "ActionNotSupported", message: "ONVIF action is not supported"}
}

func faultBody(fault *soapFault) string {
	return `<s:Fault><s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:` + fault.code +
		`</s:Value></s:Subcode></s:Code><s:Reason><s:Text xml:lang="en">` + xmlText(fault.message) + "</s:Text></s:Reason></s:Fault>"
}

func (c *camera) serveSOAP(namespace string, reply func(operation) (string, *soapFault)) http.HandlerFunc {
	const maxSOAPBytes = 32 << 10
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSOAPBytes))
		if err != nil {
			http.Error(w, "Unable to read SOAP request", http.StatusBadRequest)
			return
		}
		request, err := readEnvelope(body)
		if err != nil {
			http.Error(w, "Invalid SOAP envelope", http.StatusBadRequest)
			return
		}
		c.soapRequests.Add(1)
		op := request.Body.Operations[0]
		content, fault := "", unsupportedAction()
		status := http.StatusOK
		if c.status.SOAPDigest && !(namespace == deviceNS && op.XMLName.Space == deviceNS && op.XMLName.Local == "GetSystemDateAndTime") {
			if ok, clock := c.auth.verify(request.Header.Security.Token, c.clock()); !ok {
				detail := "UsernameToken PasswordDigest required"
				c.authRejected.Add(1)
				if clock {
					detail = "UsernameToken Created is outside the device clock window"
					c.clockRejected.Add(1)
				}
				fault = &soapFault{code: "NotAuthorized", message: detail}
				status = http.StatusUnauthorized
			}
		}
		if status == http.StatusOK && op.XMLName.Space == namespace {
			content, fault = reply(op)
		}
		action := namespace + "/" + op.XMLName.Local + "Response"
		if fault != nil {
			content = faultBody(fault)
			if status == http.StatusOK {
				status = http.StatusBadRequest
			}
			// WS-Addressing uses a dedicated fault action.
			action = ""
		} else {
			prefix := "tds"
			switch namespace {
			case ptzNS:
				prefix = "tptz"
			case mediaNS:
				prefix = "trt"
			}
			name := prefix + ":" + op.XMLName.Local + "Response"
			content = "<" + name + ">" + content + "</" + name + ">"
		}
		if action == "" {
			if ns := request.Header.Action.XMLName.Space; ns == addressNS04 {
				action = addressNS04 + "/fault"
			} else {
				action = addressNS05 + "/fault"
			}
		}
		ns, header := addressedReply(request.Header, action)
		namespaces := ` xmlns:tds="` + deviceNS + `" xmlns:trt="` + mediaNS + `" xmlns:tptz="` + ptzNS + `" xmlns:tt="` + schemaNS + `" xmlns:ter="` + errorNS + `"`
		if ns != "" {
			namespaces += ` xmlns:a="` + ns + `"`
		}
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, soapMessage(namespaces, header, content))
	}
}
