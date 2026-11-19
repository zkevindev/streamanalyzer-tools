package sipphone

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/pion/sdp/v3"
)

type audioCodec struct {
	name string
	pt   uint8
}

var codecs = []audioCodec{{"PCMU", 0}, {"PCMA", 8}}

type audioParameters struct {
	codec      audioCodec
	remote     netip.AddrPort
	send, recv bool
}

func audioDirection(attrs []sdp.Attribute, fallback string) string {
	for _, a := range attrs {
		switch a.Key {
		case "sendonly", "recvonly", "inactive", "sendrecv":
			return a.Key
		}
	}
	return fallback
}

func negotiateAudio(body []byte, answering bool) (*sdp.SessionDescription, int, audioParameters, error) {
	fail := func(message string) (*sdp.SessionDescription, int, audioParameters, error) {
		return nil, -1, audioParameters{}, errors.New(message)
	}
	var session sdp.SessionDescription
	if len(body) == 0 || len(body) > 16<<10 || session.Unmarshal(body) != nil {
		return fail("无有效的 SIP SDP")
	}
	sessionDirection := audioDirection(session.Attributes, "sendrecv")
	for i, media := range session.MediaDescriptions {
		m := media.MediaName
		if m.Media != "audio" || strings.Join(m.Protos, "/") != "RTP/AVP" || m.Port.Value < 1 || m.Port.Value > 65535 {
			continue
		}
		connection := media.ConnectionInformation
		if connection == nil {
			connection = session.ConnectionInformation
		}
		if connection == nil || connection.Address == nil || connection.NetworkType != "IN" || connection.AddressType != "IP4" {
			continue
		}
		ip, err := netip.ParseAddr(connection.Address.Address)
		if err != nil || !isUnicast(ip) {
			continue
		}
		remoteDirection := audioDirection(media.Attributes, sessionDirection)
		send, recv := remoteDirection != "sendonly" && remoteDirection != "inactive", remoteDirection != "recvonly" && remoteDirection != "inactive"
		if !send && !recv {
			continue
		}
		mapping := make(map[string]string)
		for _, attr := range media.Attributes {
			if attr.Key == "rtpmap" {
				fields := strings.Fields(attr.Value)
				if len(fields) == 2 {
					mapping[fields[0]] = strings.ToUpper(fields[1])
				}
			}
		}
		for _, format := range m.Formats {
			pt, err := strconv.ParseUint(format, 10, 7)
			if err != nil {
				continue
			}
			for _, codec := range codecs {
				if !answering && uint8(pt) != codec.pt {
					continue
				}
				encoding := mapping[format]
				expected := codec.name + "/8000"
				if encoding == expected || encoding == expected+"/1" || encoding == "" && uint8(pt) == codec.pt {
					if uint8(pt) != codec.pt && pt < 96 {
						continue
					}
					return &session, i, audioParameters{
						codec: audioCodec{codec.name, uint8(pt)}, remote: netip.AddrPortFrom(ip, uint16(m.Port.Value)),
						send: send, recv: recv,
					}, nil
				}
			}
		}
	}
	return fail("对端 SDP 未提供可用的 IPv4 G.711 RTP 音频")
}

func sdpBase(local netip.AddrPort, id uint32) string {
	return fmt.Sprintf("v=0\r\no=- %d %d IN IP4 %s\r\ns=Stream Analyzer SIP\r\nc=IN IP4 %s\r\nt=0 0\r\n",
		id, id, local.Addr(), local.Addr())
}

func makeOffer(local netip.AddrPort, id uint32) []byte {
	return fmt.Appendf([]byte(sdpBase(local, id)), "m=audio %d RTP/AVP 0 8\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:8 PCMA/8000\r\na=ptime:20\r\na=sendrecv\r\n", local.Port())
}

func makeAnswer(local netip.AddrPort, id uint32, offer *sdp.SessionDescription, accepted int, audio audioParameters) []byte {
	var result strings.Builder
	result.WriteString(sdpBase(local, id))
	for i, media := range offer.MediaDescriptions {
		if i != accepted {
			m := media.MediaName
			fmt.Fprintf(&result, "m=%s 0 %s %s\r\n", m.Media, strings.Join(m.Protos, "/"), strings.Join(m.Formats, " "))
			continue
		}
		direction := "sendrecv"
		if !audio.recv {
			direction = "sendonly"
		} else if !audio.send {
			direction = "recvonly"
		}
		fmt.Fprintf(&result, "m=audio %d RTP/AVP %d\r\na=rtpmap:%d %s/8000\r\na=ptime:20\r\na=%s\r\n",
			local.Port(), audio.codec.pt, audio.codec.pt, audio.codec.name, direction)
	}
	return []byte(result.String())
}
