package gb28181

import (
	"context"
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

type sipFlow struct {
	local      netip.AddrPort
	client     *sipgo.Client
	ctx        context.Context
	registered chan struct{}
}

func (d *device) serveSIPFlow() (bool, error) {
	ctx, cancel := context.WithCancel(d.ctx)
	defer cancel()
	flow := &sipFlow{ctx: ctx, registered: make(chan struct{})}

	options := []sipgo.UserAgentOption{
		sipgo.WithUserAgent("Stream Analyzer"), sipgo.WithUserAgentHostname(d.cfg.Domain),
	}
	if d.cfg.SIPTransport == "tcp" {
		options = append(options,
			sipgo.WithUserAgentTransactionLayerOptions(sip.WithTransactionLayerTerminateOnConnClose()),
			sipgo.WithUserAgentTransportLayerOptions(
				sip.WithTransportLayerTransports(sip.TransportsConfig{TCP: &sip.TransportTCP{
					WriteTimeout: 3 * time.Second,
					// The simulator owns connection establishment. A transaction
					// must not silently dial a new flow after the current one closes.
					DialerCreate: func(net.Addr) net.Dialer {
						return net.Dialer{Control: refuseSIPRedial}
					},
				}}),
				func(layer *sip.TransportLayer) {
					// Observe framed messages before transaction handlers can modify them.
					layer.OnMessage(func(msg sip.Message) {
						d.logPacket("in", []byte(msg.String()))
					})
				},
			))
	}
	ua, err := sipgo.NewUA(options...)
	if err != nil {
		return false, err
	}
	defer ua.Close()
	server, err := sipgo.NewServer(ua)
	if err != nil {
		return false, err
	}
	d.routes(server, flow)

	var start func() error
	var stop func() error
	var ready <-chan struct{}
	var disconnected <-chan struct{}
	if d.cfg.SIPTransport == "tcp" {
		dialer := net.Dialer{Timeout: 5 * time.Second,
			LocalAddr: &net.TCPAddr{IP: net.ParseIP(d.cfg.LocalIP), Port: d.cfg.LocalPort}}
		conn, err := dialer.DialContext(ctx, "tcp4", d.platform.String())
		if err != nil {
			return false, fmt.Errorf("连接 SIP TCP 平台 %s 失败：%w", d.platform, err)
		}
		wire := &tcpSIPWire{TCPConn: conn.(*net.TCPConn), device: d, ready: make(chan struct{}), done: make(chan struct{})}
		start = func() error { return server.ServeTCP(wire) }
		stop, ready, disconnected = wire.Close, wire.ready, wire.done
		flow.local = wire.LocalAddr().(*net.TCPAddr).AddrPort()
	} else {
		wire, err := d.openSIPUDP()
		if err != nil {
			return false, err
		}
		start = func() error { return server.ServeUDP(wire) }
		stop, ready = wire.Close, wire.ready
		flow.local = wire.LocalAddr().(*net.UDPAddr).AddrPort()
	}
	flow.local = netip.AddrPortFrom(flow.local.Addr().Unmap(), flow.local.Port())
	clientOptions := []sipgo.ClientOption{
		sipgo.WithClientAddr(flow.local.String()), sipgo.WithClientConnectionAddr(flow.local.String()),
	}
	if d.cfg.SIPTransport == "udp" {
		clientOptions = append(clientOptions, sipgo.WithClientNAT())
	}
	flow.client, err = sipgo.NewClient(ua, clientOptions...)
	if err != nil {
		stop()
		return false, err
	}

	served := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = start()
		close(served)
		cancel()
	}()
	defer func() {
		stop()
		<-served
	}()
	select {
	case <-ready:
	case <-served:
		return false, fmt.Errorf("SIP %s 信令初始化失败：%v", strings.ToUpper(d.cfg.SIPTransport), serveErr)
	case <-d.ctx.Done():
		return false, d.ctx.Err()
	}
	d.mu.Lock()
	d.status.LocalAddress = flow.local.String()
	d.status.Platform = d.platform.String()
	d.mu.Unlock()
	transport := strings.ToUpper(d.cfg.SIPTransport)
	d.log("system", fmt.Sprintf("本机 SIP %s %s → %s", transport, flow.local, d.platform), "")

	expires, err := d.register(flow.ctx, flow, defaultExpires)
	if err != nil {
		return false, err
	}
	d.state("registered", "")
	close(flow.registered)
	d.log("system", "设备已注册", "")
	err = d.keepRegistered(flow, expires)
	cancel()
	d.closeMedia()
	d.workers.Wait()
	if d.ctx.Err() != nil {
		select {
		case <-disconnected:
		case <-served:
		default:
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer stopCancel()
			if _, err := d.register(stopCtx, flow, 0); err != nil {
				d.log("system", err.Error(), "")
			} else {
				d.log("system", "设备已注销", "")
			}
		}
		return true, nil
	}
	if errors.Is(err, context.Canceled) {
		return true, fmt.Errorf("SIP %s 连接已断开", transport)
	}
	return true, err
}

func (d *device) openSIPUDP() (*udpSIPWire, error) {
	localIP := net.ParseIP(d.cfg.LocalIP)
	platform := net.UDPAddrFromAddrPort(d.platform)
	if localIP == nil {
		probe, err := net.DialUDP("udp4", nil, platform)
		if err != nil {
			return nil, fmt.Errorf("无法确定到平台的本机地址：%w", err)
		}
		localIP = probe.LocalAddr().(*net.UDPAddr).IP
		probe.Close()
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: localIP, Port: d.cfg.LocalPort})
	if err != nil {
		return nil, fmt.Errorf("绑定本机 SIP 地址失败：%w", err)
	}
	return &udpSIPWire{UDPConn: conn, device: d, platform: platform.IP, ready: make(chan struct{})}, nil
}

func (f *sipFlow) awaitRegistration() {
	select {
	case <-f.registered:
		return
	default:
	}
	// A platform can send its first query immediately after REGISTER 200 OK.
	select {
	case <-f.registered:
	case <-f.ctx.Done():
	case <-time.After(time.Second):
	}
}

func refuseSIPRedial(_, _ string, _ syscall.RawConn) error {
	return net.ErrClosed
}

// tcpSIPWire supplies the device's already-dialed stream to sipgo's TCP server.
// This puts both SIP clients and server transactions on the same connection.
type tcpSIPWire struct {
	*net.TCPConn
	device   *device
	accepted bool
	ready    chan struct{}
	done     chan struct{}
	once     sync.Once
}

func (w *tcpSIPWire) Addr() net.Addr { return w.LocalAddr() }

func (w *tcpSIPWire) Accept() (net.Conn, error) {
	if !w.accepted {
		w.accepted = true
		return w, nil
	}
	// The server has installed the connection in its pool before accepting again.
	close(w.ready)
	<-w.done
	return nil, net.ErrClosed
}

func (w *tcpSIPWire) Read(p []byte) (int, error) {
	n, err := w.TCPConn.Read(p)
	if err != nil {
		w.once.Do(func() { close(w.done) })
	}
	return n, err
}

func (w *tcpSIPWire) Write(p []byte) (int, error) {
	n, err := w.TCPConn.Write(p)
	if err != nil {
		w.Close()
	} else if n == len(p) && n > 4 {
		w.device.logPacket("out", p)
	}
	return n, err
}

func (w *tcpSIPWire) Close() error {
	w.once.Do(func() { close(w.done) })
	return w.TCPConn.Close()
}
