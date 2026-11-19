package onvif

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ptzCapabilities = `<tptz:Capabilities MoveStatus="true" StatusPosition="true"/>`
	maxPresets      = 16
	positionSpace   = "http://www.onvif.org/ver10/tptz/"
	absolutePanTilt = positionSpace + "PanTiltSpaces/PositionGenericSpace"
	absoluteZoom    = positionSpace + "ZoomSpaces/PositionGenericSpace"
	relativePanTilt = positionSpace + "PanTiltSpaces/TranslationGenericSpace"
	relativeZoom    = positionSpace + "ZoomSpaces/TranslationGenericSpace"
	velocityPanTilt = positionSpace + "PanTiltSpaces/VelocityGenericSpace"
	velocityZoom    = positionSpace + "ZoomSpaces/VelocityGenericSpace"
)

type ptzPose struct{ pan, tilt, zoom float64 }

type ptzAxis struct {
	X     float64 `xml:"x,attr"`
	Y     float64 `xml:"y,attr"`
	Space string  `xml:"space,attr"`
}

type ptzVector struct {
	PanTilt *ptzAxis
	Zoom    *ptzAxis
}

func (v ptzVector) pose(p ptzPose, panSpace, zoomSpace string, zoomMin float64) (ptzPose, bool) {
	within := func(value, min float64) bool { return value >= min && value <= 1 }
	if a := v.PanTilt; a != nil {
		if (a.Space != "" && a.Space != panSpace) || !within(a.X, -1) || !within(a.Y, -1) {
			return p, false
		}
		p.pan, p.tilt = a.X, a.Y
	}
	if a := v.Zoom; a != nil {
		if (a.Space != "" && a.Space != zoomSpace) || !within(a.X, zoomMin) {
			return p, false
		}
		p.zoom = a.X
	}
	return p, v.PanTilt != nil || v.Zoom != nil
}

func (p ptzPose) moved(delta ptzPose, seconds float64) ptzPose {
	clamp := func(value, low float64) float64 { return min(1, max(low, value)) }
	return ptzPose{
		pan:  clamp(p.pan+delta.pan*seconds, -1),
		tilt: clamp(p.tilt+delta.tilt*seconds, -1),
		zoom: clamp(p.zoom+delta.zoom*seconds, 0),
	}
}

type ptzPreset struct {
	token, name string
	position    ptzPose
}

type ptzState struct {
	position ptzPose
	velocity ptzPose
	updated  time.Time
	stopAt   time.Time
	sequence uint64
	presets  []ptzPreset
}

func (p *ptzState) advance() {
	if p.velocity == (ptzPose{}) {
		return
	}
	now := time.Now()
	if now.After(p.stopAt) {
		now = p.stopAt
	}
	p.position = p.position.moved(p.velocity, now.Sub(p.updated).Seconds())
	p.updated = now
	if !now.Before(p.stopAt) {
		p.velocity = ptzPose{}
	}
}

func (p *profileVideo) ptzConfig(tag string) string {
	return fmt.Sprintf(`<%s token="ptz-config-%s"><tt:Name>%s PTZ</tt:Name><tt:UseCount>1</tt:UseCount><tt:NodeToken>ptz-node-%s</tt:NodeToken>
<tt:DefaultAbsolutePantTiltPositionSpace>%s</tt:DefaultAbsolutePantTiltPositionSpace><tt:DefaultAbsoluteZoomPositionSpace>%s</tt:DefaultAbsoluteZoomPositionSpace>
<tt:DefaultRelativePanTiltTranslationSpace>%s</tt:DefaultRelativePanTiltTranslationSpace><tt:DefaultRelativeZoomTranslationSpace>%s</tt:DefaultRelativeZoomTranslationSpace>
<tt:DefaultContinuousPanTiltVelocitySpace>%s</tt:DefaultContinuousPanTiltVelocitySpace><tt:DefaultContinuousZoomVelocitySpace>%s</tt:DefaultContinuousZoomVelocitySpace>
<tt:DefaultPTZTimeout>PT2S</tt:DefaultPTZTimeout></%s>`, tag, p.number, p.Name, p.number, absolutePanTilt, absoluteZoom,
		relativePanTilt, relativeZoom, velocityPanTilt, velocityZoom, tag)
}

func ptzSpaces() string {
	var out strings.Builder
	for _, item := range []struct {
		tag, uri string
		min      int
		panTilt  bool
	}{
		{"AbsolutePanTiltPositionSpace", absolutePanTilt, -1, true}, {"AbsoluteZoomPositionSpace", absoluteZoom, 0, false},
		{"RelativePanTiltTranslationSpace", relativePanTilt, -1, true}, {"RelativeZoomTranslationSpace", relativeZoom, -1, false},
		{"ContinuousPanTiltVelocitySpace", velocityPanTilt, -1, true}, {"ContinuousZoomVelocitySpace", velocityZoom, -1, false},
	} {
		fmt.Fprintf(&out, "<tt:%s><tt:URI>%s</tt:URI><tt:XRange><tt:Min>%d</tt:Min><tt:Max>1</tt:Max></tt:XRange>", item.tag, item.uri, item.min)
		if item.panTilt {
			out.WriteString("<tt:YRange><tt:Min>-1</tt:Min><tt:Max>1</tt:Max></tt:YRange>")
		}
		fmt.Fprintf(&out, "</tt:%s>", item.tag)
	}
	return out.String()
}

func (p *profileVideo) ptzNode(tag string) string {
	return fmt.Sprintf(`<tptz:%s token="ptz-node-%s" FixedHomePosition="false"><tt:Name>%s PTZ</tt:Name><tt:SupportedPTZSpaces>%s</tt:SupportedPTZSpaces><tt:MaximumNumberOfPresets>%d</tt:MaximumNumberOfPresets><tt:HomeSupported>false</tt:HomeSupported></tptz:%s>`,
		tag, p.number, p.Name, ptzSpaces(), maxPresets, tag)
}

func (p ptzPose) xml(tag string) string {
	return fmt.Sprintf(`<tt:%s><tt:PanTilt x="%g" y="%g" space="%s"/><tt:Zoom x="%g" space="%s"/></tt:%s>`,
		tag, p.pan, p.tilt, absolutePanTilt, p.zoom, absoluteZoom, tag)
}

func (c *camera) ptzReply(op operation) (string, *soapFault) {
	name := op.XMLName.Local
	var body strings.Builder
	for _, p := range c.profiles {
		switch name {
		case "GetServiceCapabilities":
			return ptzCapabilities, nil
		case "GetNodes":
			body.WriteString(p.ptzNode("PTZNode"))
		case "GetNode":
			if op.NodeToken == "ptz-node-"+p.number {
				return p.ptzNode("PTZNode"), nil
			}
		case "GetConfigurations":
			body.WriteString(p.ptzConfig("tptz:PTZConfiguration"))
		case "GetConfiguration", "GetConfigurationOptions":
			if op.ConfigurationToken != "ptz-config-"+p.number {
				continue
			}
			if name == "GetConfiguration" {
				return p.ptzConfig("tptz:PTZConfiguration"), nil
			}
			return "<tptz:PTZConfigurationOptions><tt:Spaces>" + ptzSpaces() + "</tt:Spaces><tt:PTZTimeout><tt:Min>PT0.1S</tt:Min><tt:Max>PT30S</tt:Max></tt:PTZTimeout></tptz:PTZConfigurationOptions>", nil
		case "GetCompatibleConfigurations":
			if op.ProfileToken == p.Token {
				return p.ptzConfig("tptz:PTZConfiguration"), nil
			}
		default:
			if op.ProfileToken == p.Token {
				c.ptzMu.Lock()
				defer c.ptzMu.Unlock()
				p.ptz.advance()
				return p.ptz.reply(op)
			}
		}
	}
	if body.Len() == 0 {
		return "", invalidToken("Unknown PTZ node, configuration or profile token")
	}
	return body.String(), nil
}

func (p *ptzState) reply(op operation) (string, *soapFault) {
	switch op.XMLName.Local {
	case "GetStatus":
		panTilt, zoom := "IDLE", "IDLE"
		if p.velocity.pan != 0 || p.velocity.tilt != 0 {
			panTilt = "MOVING"
		}
		if p.velocity.zoom != 0 {
			zoom = "MOVING"
		}
		return fmt.Sprintf("<tptz:PTZStatus>%s<tt:MoveStatus><tt:PanTilt>%s</tt:PanTilt><tt:Zoom>%s</tt:Zoom></tt:MoveStatus><tt:UtcTime>%s</tt:UtcTime></tptz:PTZStatus>",
			p.position.xml("Position"), panTilt, zoom, time.Now().UTC().Format(time.RFC3339Nano)), nil
	case "AbsoluteMove":
		pos, ok := op.Position.pose(p.position, absolutePanTilt, absoluteZoom, 0)
		if !ok {
			return "", invalidToken("Invalid absolute PTZ position")
		}
		p.position, p.velocity = pos, ptzPose{}
	case "RelativeMove":
		delta, ok := op.Translation.pose(ptzPose{}, relativePanTilt, relativeZoom, -1)
		if !ok {
			return "", invalidToken("Invalid relative PTZ translation")
		}
		p.position, p.velocity = p.position.moved(delta, 1), ptzPose{}
	case "ContinuousMove":
		velocity, ok := op.Velocity.pose(p.velocity, velocityPanTilt, velocityZoom, -1)
		timeout, validTimeout := ptzTimeout(op.Timeout)
		if !ok || !validTimeout {
			return "", invalidToken("Invalid PTZ velocity or timeout")
		}
		p.velocity, p.updated = velocity, time.Now()
		p.stopAt = p.updated.Add(timeout)
	case "Stop":
		if op.PanTilt == nil && op.Zoom == nil || op.PanTilt != nil && *op.PanTilt {
			p.velocity.pan, p.velocity.tilt = 0, 0
		}
		if op.PanTilt == nil && op.Zoom == nil || op.Zoom != nil && *op.Zoom {
			p.velocity.zoom = 0
		}
	case "GetPresets":
		var out strings.Builder
		for _, preset := range p.presets {
			fmt.Fprintf(&out, `<tptz:Preset token="%s"><tt:Name>%s</tt:Name>%s</tptz:Preset>`, preset.token, xmlText(preset.name), preset.position.xml("PTZPosition"))
		}
		return out.String(), nil
	case "SetPreset", "GotoPreset", "RemovePreset":
		return p.presetAction(op)
	default:
		return "", unsupportedAction()
	}
	return "", nil
}

func ptzTimeout(raw string) (time.Duration, bool) {
	if raw == "" {
		return 2 * time.Second, true
	}
	if !strings.HasPrefix(raw, "PT") || !strings.HasSuffix(raw, "S") {
		return 0, false
	}
	sec, err := strconv.ParseFloat(raw[2:len(raw)-1], 64)
	return time.Duration(sec * float64(time.Second)), err == nil && !math.IsNaN(sec) && sec >= 0.1 && sec <= 30
}

func (p *ptzState) presetAction(op operation) (string, *soapFault) {
	if op.PresetToken == "" && op.XMLName.Local == "SetPreset" {
		if len(p.presets) == maxPresets {
			return "", invalidToken("PTZ preset limit reached")
		}
		if utf8.RuneCountInString(op.PresetName) > 64 {
			return "", invalidToken("PTZ preset name is too long")
		}
		p.sequence++
		token := "preset-" + strconv.FormatUint(p.sequence, 10)
		name := op.PresetName
		if name == "" {
			name = token
		}
		p.presets = append(p.presets, ptzPreset{token: token, name: name, position: p.position})
		return "<tptz:PresetToken>" + token + "</tptz:PresetToken>", nil
	}
	for i, preset := range p.presets {
		if preset.token != op.PresetToken {
			continue
		}
		switch op.XMLName.Local {
		case "SetPreset":
			if utf8.RuneCountInString(op.PresetName) > 64 {
				return "", invalidToken("PTZ preset name is too long")
			}
			if op.PresetName != "" {
				p.presets[i].name = op.PresetName
			}
			p.presets[i].position = p.position
			return "<tptz:PresetToken>" + preset.token + "</tptz:PresetToken>", nil
		case "GotoPreset":
			p.position, p.velocity = preset.position, ptzPose{}
		case "RemovePreset":
			p.presets = append(p.presets[:i], p.presets[i+1:]...)
		}
		return "", nil
	}
	return "", invalidToken("Unknown PTZ preset token")
}
