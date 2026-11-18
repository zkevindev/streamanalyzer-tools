package gb28181

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

type mediaTransport string

const (
	mediaUDP        mediaTransport = "udp"
	mediaTCPActive  mediaTransport = "tcp-active"
	mediaTCPPassive mediaTransport = "tcp-passive"

	mediaConnectTimeout = 5 * time.Second
)

type mediaOffer struct {
	target    netip.AddrPort
	transport mediaTransport
	payload   byte
	ssrc      uint32
	y         string
}

func parseOffer(body []byte) (mediaOffer, error) {
	var offer mediaOffer
	var sessionIP, videoIP string
	var sessionSetup, videoSetup string
	var protocol string
	var port int
	var payloads []string
	maps := make(map[string]string)
	inVideo, inMedia := false, false
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "s":
			if !strings.EqualFold(value, "Play") {
				return offer, errors.New("只接受 s=Play 实时点播")
			}
		case "c":
			if inVideo {
				videoIP = value
			} else if !inMedia {
				sessionIP = value
			}
		case "m":
			inMedia = true
			fields := strings.Fields(value)
			inVideo = len(fields) >= 4 && fields[0] == "video" && payloads == nil
			if inVideo {
				port, _ = strconv.Atoi(fields[1])
				protocol, payloads = strings.ToUpper(fields[2]), fields[3:]
			}
		case "a":
			attribute, data, _ := strings.Cut(value, ":")
			switch {
			case attribute == "setup" && inVideo:
				videoSetup = data
			case attribute == "setup" && !inMedia:
				sessionSetup = data
			case attribute == "rtpmap" && inVideo:
				if f := strings.Fields(data); len(f) == 2 {
					maps[f[0]] = strings.ToLower(f[1])
				}
			}
		case "y":
			offer.y = value
		}
	}

	if payloads == nil {
		return offer, errors.New("SDP 缺少视频媒体描述")
	}
	switch protocol {
	case "RTP/AVP":
		offer.transport = mediaUDP
	case "TCP/RTP/AVP":
		if videoSetup == "" {
			videoSetup = sessionSetup
		}
		switch strings.ToLower(strings.TrimSpace(videoSetup)) {
		case "passive":
			offer.transport = mediaTCPActive
		case "", "active", "actpass":
			// RFC 4145 defaults the offerer to active. Accept actpass as passive.
			offer.transport = mediaTCPPassive
		default:
			return offer, errors.New("SDP 的 TCP setup 角色不支持")
		}
	default:
		return offer, errors.New("SDP 视频传输需使用 RTP/AVP 或 TCP/RTP/AVP")
	}
	if videoIP == "" {
		videoIP = sessionIP
	}
	fields := strings.Fields(videoIP)
	if len(fields) != 3 || fields[0] != "IN" || fields[1] != "IP4" || port < 1 || port > 65535 {
		return offer, errors.New("SDP 缺少有效的 IPv4 视频地址或端口")
	}
	ip, err := netip.ParseAddr(fields[2])
	if err != nil || !ip.Is4() || !unicastIP(ip.AsSlice()) {
		return offer, errors.New("SDP 视频地址需为单播 IPv4 地址")
	}
	offer.target = netip.AddrPortFrom(ip, uint16(port))
	if offer.y == "" {
		offer.y = fmt.Sprintf("%010d", rand.Uint32N(1_000_000_000))
	}
	ssrc, err := strconv.ParseUint(offer.y, 10, 32)
	if err != nil {
		return offer, errors.New("SDP 的 y= SSRC 无效")
	}
	offer.ssrc = uint32(ssrc)
	for _, value := range payloads {
		pt, err := strconv.ParseUint(value, 10, 7)
		if err == nil && (maps[value] == "ps/90000" || value == "96" && maps[value] == "") {
			offer.payload = byte(pt)
			return offer, nil
		}
	}
	return offer, errors.New("SDP 未提供 PS/90000 媒体载荷")
}

func (s *mediaSession) prepare(localIP net.IP) (int, error) {
	switch s.offer.transport {
	case mediaUDP:
		conn, err := net.DialUDP("udp4", &net.UDPAddr{IP: localIP}, net.UDPAddrFromAddrPort(s.offer.target))
		if err != nil {
			return 0, err
		}
		s.conn = conn
		return conn.LocalAddr().(*net.UDPAddr).Port, nil
	case mediaTCPPassive:
		listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: localIP})
		if err != nil {
			return 0, err
		}
		s.listener = listener
		return listener.Addr().(*net.TCPAddr).Port, nil
	case mediaTCPActive:
		// In the active TCP answer, the advertised port is the discard port.
		return 9, nil
	default:
		return 0, fmt.Errorf("未知的媒体传输 %q", s.offer.transport)
	}
}

func (s *mediaSession) sdpAnswer(channel, ip string, port int) []byte {
	protocol, attributes := "RTP/AVP", ""
	switch s.offer.transport {
	case mediaTCPActive:
		protocol, attributes = "TCP/RTP/AVP", "a=setup:active\r\na=connection:new\r\n"
	case mediaTCPPassive:
		protocol, attributes = "TCP/RTP/AVP", "a=setup:passive\r\na=connection:new\r\n"
	}
	return []byte(fmt.Sprintf("v=0\r\no=%s 0 0 IN IP4 %s\r\ns=Play\r\nc=IN IP4 %s\r\nt=0 0\r\nm=video %d %s %d\r\na=sendonly\r\na=rtpmap:%d PS/90000\r\n%sy=%s\r\n",
		channel, ip, ip, port, protocol, s.offer.payload, s.offer.payload, attributes, s.offer.y))
}

func (s *mediaSession) connect(localIP, platformIP net.IP) (net.Conn, error) {
	switch s.offer.transport {
	case mediaUDP:
		return s.conn, nil
	case mediaTCPActive:
		dialer := net.Dialer{Timeout: mediaConnectTimeout, LocalAddr: &net.TCPAddr{IP: localIP}}
		conn, err := dialer.DialContext(s.ctx, "tcp4", s.offer.target.String())
		if err != nil {
			return nil, fmt.Errorf("连接平台媒体端口 %s 失败：%w", s.offer.target, err)
		}
		return conn, nil
	case mediaTCPPassive:
		defer s.listener.Close()
		if err := s.listener.SetDeadline(time.Now().Add(mediaConnectTimeout)); err != nil {
			return nil, fmt.Errorf("设置 TCP 媒体监听超时失败：%w", err)
		}
		offerIP := s.offer.target.Addr().AsSlice()
		for {
			conn, err := s.listener.AcceptTCP()
			if err != nil {
				return nil, fmt.Errorf("等待平台连入媒体端口 %s 失败：%w", s.listener.Addr(), err)
			}
			peerIP := conn.RemoteAddr().(*net.TCPAddr).IP
			if peerIP.Equal(offerIP) || peerIP.Equal(platformIP) {
				return conn, nil
			}
			conn.Close()
		}
	default:
		return nil, fmt.Errorf("未知的媒体传输 %q", s.offer.transport)
	}
}

func (s *mediaSession) close() {
	s.cancel()
	if s.listener != nil {
		s.listener.Close()
	}
	if s.conn != nil {
		s.conn.Close()
	}
}
