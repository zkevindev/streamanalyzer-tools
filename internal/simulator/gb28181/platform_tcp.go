package gb28181

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"

	"github.com/emiago/sipgo"
)

type platformListener struct {
	Close func() error
	serve func(*sipgo.Server) error
}

func (p *platform) listen() (platformListener, netip.AddrPort, error) {
	ip := net.ParseIP(p.cfg.IP)
	if p.cfg.SIPTransport == "tcp" {
		conn, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: ip, Port: p.cfg.Port})
		if err != nil {
			return platformListener{}, netip.AddrPort{}, err
		}
		wire := &platformTCPListener{TCPListener: conn, forget: p.forget}
		return platformListener{wire.Close, func(s *sipgo.Server) error { return s.ServeTCP(wire) }}, ipv4Endpoint(conn.Addr().(*net.TCPAddr).AddrPort()), nil
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: p.cfg.Port})
	if err != nil {
		return platformListener{}, netip.AddrPort{}, err
	}
	return platformListener{conn.Close, func(s *sipgo.Server) error { return s.ServeUDP(conn) }}, ipv4Endpoint(conn.LocalAddr().(*net.UDPAddr).AddrPort()), nil
}

func ipv4Endpoint(a netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(a.Addr().Unmap(), a.Port())
}

type platformTCPListener struct {
	*net.TCPListener
	forget func(netip.AddrPort)
}

func (l *platformTCPListener) Accept() (net.Conn, error) {
	conn, err := l.AcceptTCP()
	if err != nil {
		return nil, err
	}
	peer := ipv4Endpoint(conn.RemoteAddr().(*net.TCPAddr).AddrPort())
	return &platformTCPConn{TCPConn: conn, end: sync.OnceFunc(func() { l.forget(peer) })}, nil
}

type platformTCPConn struct {
	*net.TCPConn
	end func()
}

func (c *platformTCPConn) Close() error {
	c.end()
	return c.TCPConn.Close()
}

type previewMedia struct {
	udp     *net.UDPConn
	tcp     *net.TCPListener
	port    int
	address string
}

func openPreviewMedia(ip netip.Addr, role mediaTransport) (previewMedia, error) {
	switch role {
	case mediaTCPActive:
		return previewMedia{port: 9}, nil
	case mediaTCPPassive:
		tcp, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: ip.AsSlice()})
		if err != nil {
			return previewMedia{}, err
		}
		addr := tcp.Addr().(*net.TCPAddr)
		return previewMedia{tcp: tcp, port: addr.Port, address: addr.String()}, nil
	default:
		udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip.AsSlice()})
		if err != nil {
			return previewMedia{}, err
		}
		addr := udp.LocalAddr().(*net.UDPAddr)
		return previewMedia{udp: udp, port: addr.Port, address: addr.String()}, nil
	}
}

func (m previewMedia) close() {
	if m.udp != nil {
		m.udp.Close()
	}
	if m.tcp != nil {
		m.tcp.Close()
	}
}

func (p *platform) connectPreview(ctx context.Context, m previewMedia, cfg PreviewConfig, answer mediaOffer, peer netip.Addr) (net.Conn, error) {
	if cfg.SkipConnect {
		<-ctx.Done()
		return nil, errors.New("观测窗口内未发起平台侧 TCP 连接")
	}
	if cfg.MediaTransport == mediaTCPActive {
		if answer.target.Addr() != peer {
			return nil, fmt.Errorf("SDP TCP 对端 %s 与已注册设备地址不匹配", answer.target.Addr())
		}
		dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: p.local.Addr().AsSlice()}}
		return dialer.DialContext(ctx, "tcp4", answer.target.String())
	}
	deadline, _ := ctx.Deadline()
	m.tcp.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = m.tcp.Close() })
	defer stop()
	for {
		conn, err := m.tcp.AcceptTCP()
		if err != nil {
			return nil, err
		}
		rs := ipv4Endpoint(conn.RemoteAddr().(*net.TCPAddr).AddrPort()).Addr()
		if rs == peer || rs == answer.target.Addr() {
			return conn, nil
		}
		conn.Close()
	}
}

func readPreviewFrame(conn net.Conn, buf []byte) (int, error) {
	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return 0, err
	}
	n := int(binary.BigEndian.Uint16(header[:]))
	return io.ReadFull(conn, buf[:n])
}
