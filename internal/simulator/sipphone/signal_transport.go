package sipphone

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

type signalBinding struct {
	ready <-chan struct{}
	serve func(*sipgo.Server) error
	close func() error
}

func (a *agent) openSignal(local netip.Addr) (signalBinding, error) {
	a.signalPhase("connection", "")
	if a.cfg.Transport == "udp" {
		if !local.IsValid() {
			probe, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(a.registrar))
			if err != nil {
				a.signalPhase("connection", "route")
				return signalBinding{}, errors.New("无法确定本机 SIP UDP 地址")
			}
			local = probe.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap()
			probe.Close()
		}
		udp, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(local, uint16(a.cfg.LocalPort))))
		if err != nil {
			a.signalPhase("connection", "bind")
			return signalBinding{}, fmt.Errorf("绑定本机 SIP UDP 地址失败：%w", err)
		}
		a.local = udp.LocalAddr().(*net.UDPAddr).AddrPort()
		wire := &signalSocket{UDPConn: udp, agent: a, ready: make(chan struct{})}
		return signalBinding{ready: wire.ready, close: wire.Close, serve: func(server *sipgo.Server) error { return server.ServeUDP(wire) }}, nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: local.AsSlice(), Port: a.cfg.LocalPort}}
	conn, err := dialer.DialContext(ctx, "tcp4", a.registrar.String())
	if err != nil {
		a.signalPhase("connection", "dial")
		return signalBinding{}, fmt.Errorf("连接 SIP %s 服务器失败：%w", strings.ToUpper(a.cfg.Transport), err)
	}
	a.local = conn.LocalAddr().(*net.TCPAddr).AddrPort()
	if a.cfg.Transport == "tls" {
		a.signalPhase("tls_handshake", "")
		conf := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: a.cfg.TLSServerName, RootCAs: a.cfg.tlsRoots}
		tlsConn := tls.Client(conn, conf)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			var hostname x509.HostnameError
			var authority x509.UnknownAuthorityError
			code := "handshake"
			switch {
			case errors.As(err, &hostname):
				code = "hostname_mismatch"
			case errors.As(err, &authority):
				code = "untrusted_certificate"
			}
			a.signalPhase("tls_handshake", code)
			return signalBinding{}, fmt.Errorf("SIP TLS 握手失败（%s）", code)
		}
		conn = tlsConn
	}
	wire := &sipStreamWire{Conn: conn, agent: a, ready: make(chan struct{}), done: make(chan struct{})}
	return signalBinding{ready: wire.ready, close: wire.Close, serve: func(server *sipgo.Server) error {
		if a.cfg.Transport == "tls" {
			return server.ServeTLS(wire)
		}
		return server.ServeTCP(wire)
	}}, nil
}

func (a *agent) signalPhase(stage, failure string) {
	a.mu.Lock()
	previous := a.status.SignalStage
	a.status.SignalStage, a.status.SignalFailure = stage, failure
	a.mu.Unlock()
	if previous != stage || failure != "" {
		a.event("system", strings.TrimSpace(stage+" "+failure))
	}
}

func (a *agent) streamOptions() []sipgo.UserAgentOption {
	tcp := &sip.TransportTCP{WriteTimeout: 3 * time.Second, DialerCreate: func(net.Addr) net.Dialer {
		return net.Dialer{Control: func(_, _ string, _ syscall.RawConn) error {
			return errors.New("SIP stream connection is closed")
		}}
	}}
	conf := sip.TransportsConfig{TCP: tcp}
	if a.cfg.Transport == "tls" {
		conf = sip.TransportsConfig{TLS: &sip.TransportTLS{TransportTCP: tcp}}
	}
	return []sipgo.UserAgentOption{
		sipgo.WithUserAgentTransactionLayerOptions(sip.WithTransactionLayerTerminateOnConnClose()),
		sipgo.WithUserAgentTransportLayerOptions(sip.WithTransportLayerTransports(conf), func(layer *sip.TransportLayer) {
			layer.OnMessage(func(msg sip.Message) {
				a.observeSignal("in", []byte(msg.String()), msg.Source())
			})
		}),
	}
}

// sipgo accepts an established outbound connection into its reusable stream pool.
type sipStreamWire struct {
	net.Conn
	agent       *agent
	ready, done chan struct{}
	accepted    bool
	once        sync.Once
}

func (w *sipStreamWire) Addr() net.Addr { return w.LocalAddr() }

func (w *sipStreamWire) Accept() (net.Conn, error) {
	if !w.accepted {
		w.accepted = true
		return w, nil
	}
	close(w.ready)
	<-w.done
	return nil, net.ErrClosed
}

func (w *sipStreamWire) Read(data []byte) (int, error) {
	n, err := w.Conn.Read(data)
	if err != nil {
		w.once.Do(func() { close(w.done) })
	}
	return n, err
}

func (w *sipStreamWire) Write(data []byte) (int, error) {
	n, err := w.Conn.Write(data)
	if err != nil {
		_ = w.Close()
	} else if n == len(data) {
		w.agent.observeSignal("out", data, w.RemoteAddr().String())
	}
	return n, err
}

func (w *sipStreamWire) Close() error {
	w.once.Do(func() { close(w.done) })
	return w.Conn.Close()
}
