package onvif

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"streamanalyzer/internal/simulator/testpattern"
)

const (
	videoSourceToken   = "source-1"
	sourceConfigToken  = "source-config-1"
	encoderConfigToken = "encoder-config-1"
	deviceCapabilities = `<tds:Capabilities><tds:Network DiscoveryResolve="true" DiscoveryBye="false"/><tds:Security/><tds:System/></tds:Capabilities>`
	mediaCapabilities  = `<trt:Capabilities SnapshotUri="false"><trt:StreamingCapabilities RTPMulticast="false" RTP_TCP="false" RTP_RTSP_TCP="true"/></trt:Capabilities>`
)

func (c *camera) scopes() []string {
	return []string{
		"onvif://www.onvif.org/type/video_encoder",
		"onvif://www.onvif.org/Profile/Streaming",
		"onvif://www.onvif.org/name/" + url.PathEscape(c.status.Name),
	}
}

func (c *camera) deviceReply(op operation) (string, *soapFault) {
	switch op.XMLName.Local {
	case "GetServices":
		return serviceEntry(deviceNS, c.status.DeviceURL, deviceCapabilities, op.IncludeCapability) +
			serviceEntry(mediaNS, c.status.MediaURL, mediaCapabilities, op.IncludeCapability), nil
	case "GetCapabilities":
		var parts strings.Builder
		if len(op.Category) == 0 || slices.Contains(op.Category, "All") || slices.Contains(op.Category, "Device") {
			parts.WriteString("<tt:Device><tt:XAddr>" + c.status.DeviceURL + "</tt:XAddr></tt:Device>")
		}
		if len(op.Category) == 0 || slices.Contains(op.Category, "All") || slices.Contains(op.Category, "Media") {
			parts.WriteString("<tt:Media><tt:XAddr>" + c.status.MediaURL + "</tt:XAddr>" +
				"<tt:StreamingCapabilities><tt:RTPMulticast>false</tt:RTPMulticast><tt:RTP_TCP>false</tt:RTP_TCP>" +
				"<tt:RTP_RTSP_TCP>true</tt:RTP_RTSP_TCP></tt:StreamingCapabilities></tt:Media>")
		}
		return "<tds:Capabilities>" + parts.String() + "</tds:Capabilities>", nil
	case "GetDeviceInformation":
		serial := strings.TrimPrefix(c.status.EndpointID, "urn:uuid:")
		return "<tds:Manufacturer>Stream Analyzer</tds:Manufacturer><tds:Model>" + xmlText(c.status.Name) + "</tds:Model>" +
			"<tds:FirmwareVersion>1.0</tds:FirmwareVersion><tds:SerialNumber>" + serial + "</tds:SerialNumber><tds:HardwareId>Simulator</tds:HardwareId>", nil
	case "GetScopes":
		var body strings.Builder
		for _, scope := range c.scopes() {
			body.WriteString("<tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef><tt:ScopeItem>" + xmlText(scope) + "</tt:ScopeItem></tds:Scopes>")
		}
		return body.String(), nil
	case "GetSystemDateAndTime":
		now := time.Now().UTC()
		clock := fmt.Sprintf("<tt:Time><tt:Hour>%d</tt:Hour><tt:Minute>%d</tt:Minute><tt:Second>%d</tt:Second></tt:Time>"+
			"<tt:Date><tt:Year>%d</tt:Year><tt:Month>%d</tt:Month><tt:Day>%d</tt:Day></tt:Date>",
			now.Hour(), now.Minute(), now.Second(), now.Year(), now.Month(), now.Day())
		return "<tds:SystemDateAndTime><tt:DateTimeType>Manual</tt:DateTimeType><tt:DaylightSavings>false</tt:DaylightSavings>" +
			"<tt:TimeZone><tt:TZ>UTC0</tt:TZ></tt:TimeZone><tt:UTCDateTime>" + clock + "</tt:UTCDateTime><tt:LocalDateTime>" + clock + "</tt:LocalDateTime></tds:SystemDateAndTime>", nil
	case "GetServiceCapabilities":
		return deviceCapabilities, nil
	default:
		return "", unsupportedAction()
	}
}

func serviceEntry(namespace, address, capabilities string, include bool) string {
	if include {
		capabilities = "<tds:Capabilities>" + capabilities + "</tds:Capabilities>"
	} else {
		capabilities = ""
	}
	return "<tds:Service><tds:Namespace>" + namespace + "</tds:Namespace><tds:XAddr>" + address + "</tds:XAddr>" + capabilities +
		"<tds:Version><tt:Major>2</tt:Major><tt:Minor>0</tt:Minor></tds:Version></tds:Service>"
}

func (c *camera) mediaReply(op operation) (string, *soapFault) {
	switch op.XMLName.Local {
	case "GetServiceCapabilities":
		return mediaCapabilities, nil
	case "GetProfiles":
		return profile("trt:Profiles"), nil
	case "GetProfile":
		if strings.TrimSpace(op.ProfileToken) != profileToken {
			return "", invalidToken("Unknown profile token")
		}
		return profile("trt:Profile"), nil
	case "GetVideoSources":
		return fmt.Sprintf(`<trt:VideoSources token="%s"><tt:Framerate>%d</tt:Framerate><tt:Resolution><tt:Width>%d</tt:Width><tt:Height>%d</tt:Height></tt:Resolution></trt:VideoSources>`,
			videoSourceToken, testpattern.FPS, testpattern.Width, testpattern.Height), nil
	case "GetVideoSourceConfigurations":
		return sourceConfig("trt:Configurations"), nil
	case "GetVideoEncoderConfigurations":
		return encoderConfig("trt:Configurations"), nil
	case "GetVideoSourceConfiguration":
		if strings.TrimSpace(op.ConfigurationToken) != sourceConfigToken {
			return "", invalidToken("Unknown video source configuration token")
		}
		return sourceConfig("trt:Configuration"), nil
	case "GetVideoEncoderConfiguration":
		if strings.TrimSpace(op.ConfigurationToken) != encoderConfigToken {
			return "", invalidToken("Unknown video encoder configuration token")
		}
		return encoderConfig("trt:Configuration"), nil
	case "GetStreamUri":
		if strings.TrimSpace(op.ProfileToken) != profileToken {
			return "", invalidToken("Unknown profile token")
		}
		stream := strings.TrimSpace(op.StreamSetup.Stream)
		protocol := strings.TrimSpace(op.StreamSetup.Transport.Protocol)
		if (stream != "" && stream != "RTP-Unicast") || op.StreamSetup.Transport.Tunnel != nil ||
			(protocol != "" && protocol != "UDP" && protocol != "RTSP" && protocol != "TCP") {
			return "", &soapFault{code: "InvalidArgVal", message: "Only RTSP unicast streaming is available"}
		}
		return "<trt:MediaUri><tt:Uri>" + c.status.StreamURL + "</tt:Uri><tt:InvalidAfterConnect>false</tt:InvalidAfterConnect>" +
			"<tt:InvalidAfterReboot>true</tt:InvalidAfterReboot><tt:Timeout>PT0S</tt:Timeout></trt:MediaUri>", nil
	default:
		return "", unsupportedAction()
	}
}

func profile(tag string) string {
	return fmt.Sprintf(`<%s token="%s" fixed="true"><tt:Name>H.264 Test Pattern</tt:Name>`, tag, profileToken) +
		sourceConfig("tt:VideoSourceConfiguration") + encoderConfig("tt:VideoEncoderConfiguration") + "</" + tag + ">"
}

func sourceConfig(tag string) string {
	return fmt.Sprintf(`<%s token="%s"><tt:Name>Test Pattern Source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>%s</tt:SourceToken><tt:Bounds x="0" y="0" width="%d" height="%d"/></%s>`,
		tag, sourceConfigToken, videoSourceToken, testpattern.Width, testpattern.Height, tag)
}

func encoderConfig(tag string) string {
	return fmt.Sprintf(`<%s token="%s"><tt:Name>H.264 Test Encoder</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>H264</tt:Encoding>
<tt:Resolution><tt:Width>%d</tt:Width><tt:Height>%d</tt:Height></tt:Resolution><tt:Quality>5</tt:Quality>
<tt:RateControl><tt:FrameRateLimit>%d</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>256</tt:BitrateLimit></tt:RateControl>
<tt:H264><tt:GovLength>%d</tt:GovLength><tt:H264Profile>High</tt:H264Profile></tt:H264>
<tt:Multicast><tt:Address><tt:Type>IPv4</tt:Type><tt:IPv4Address>0.0.0.0</tt:IPv4Address></tt:Address><tt:Port>0</tt:Port><tt:TTL>1</tt:TTL><tt:AutoStart>false</tt:AutoStart></tt:Multicast>
<tt:SessionTimeout>PT60S</tt:SessionTimeout></%s>`,
		tag, encoderConfigToken, testpattern.Width, testpattern.Height, testpattern.FPS, testpattern.Len(), tag)
}
