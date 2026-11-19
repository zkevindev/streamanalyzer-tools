package diagnostic

import (
	"fmt"
	"net"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
)

type rtspReplayUDP struct {
	sockets     [2]*net.UDPConn
	targets     [2]*net.UDPAddr
	serverPorts [2]int
}

func (p *replayRTSPPeer) setupUDP(id int, transport headers.Transport) (*rtspReplayUDP, base.StatusCode) {
	ports := transport.ClientPorts
	if transport.Protocol != headers.TransportProtocolUDP || ports == nil || ports[0] <= 0 || ports[1] > 65535 ||
		ports[1] != ports[0]+1 || transport.InterleavedIDs != nil || transport.Mode != nil && *transport.Mode != headers.TransportModePlay {
		return nil, base.StatusUnsupportedTransport
	}
	// Destinations are derived exclusively from the loopback control peer.
	ip := p.wire.RemoteAddr().(*net.TCPAddr).IP
	if !ip.IsLoopback() || transport.Destination2 != nil && !net.ParseIP(*transport.Destination2).Equal(ip) {
		return nil, base.StatusUnsupportedTransport
	}
	for previous, used := range p.datagrams {
		if previous != id && ports[0] <= used.targets[1].Port && used.targets[0].Port <= ports[1] {
			return nil, base.StatusUnsupportedTransport
		}
	}
	pair, err := openRTSPReplayPair(ip, *ports)
	if err != nil {
		return nil, base.StatusServiceUnavailable
	}
	if old := p.datagrams[id]; old != nil {
		old.close()
	}
	p.datagrams[id] = pair
	return pair, base.StatusOK
}

func openRTSPReplayPair(ip net.IP, remote [2]int) (*rtspReplayUDP, error) {
	bind := net.IPv4(127, 0, 0, 1)
	for range 24 {
		first, err := net.ListenUDP("udp4", &net.UDPAddr{IP: bind})
		if err != nil {
			return nil, err
		}
		port := first.LocalAddr().(*net.UDPAddr).Port
		second, err := net.ListenUDP("udp4", &net.UDPAddr{IP: bind, Port: port ^ 1})
		if err != nil {
			first.Close()
			continue
		}
		if port&1 != 0 {
			first, second = second, first
		}
		return &rtspReplayUDP{sockets: [2]*net.UDPConn{first, second}, serverPorts: [2]int{port &^ 1, port | 1}, targets: [2]*net.UDPAddr{
			{IP: ip, Port: remote[0]}, {IP: ip, Port: remote[1]},
		}}, nil
	}
	return nil, fmt.Errorf("无法分配本机 RTSP UDP 端口对")
}

func (pair *rtspReplayUDP) send(data []byte, rtcp bool) error {
	index := 0
	if rtcp {
		index = 1
	}
	socket := pair.sockets[index]
	_ = socket.SetWriteDeadline(time.Now().Add(time.Second))
	_, err := socket.WriteToUDP(data, pair.targets[index])
	return err
}

func (pair *rtspReplayUDP) close() {
	for _, socket := range pair.sockets {
		socket.Close()
	}
}
