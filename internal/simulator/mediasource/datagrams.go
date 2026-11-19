package mediasource

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pion/rtp"
	"golang.org/x/net/ipv4"
)

var ErrSenderInactive = errors.New("请先配置 UDP 或 RTP MPEG-TS 发送源")

func (c Config) datagram() bool { return c.Protocol == "udp-ts" || c.Protocol == "rtp-ts" }

func (c *Config) destination() (netip.AddrPort, *net.Interface, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(c.TargetIP))
	if err != nil || !ip.Is4() || (!ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() && !ip.IsLoopback() && !ip.IsMulticast()) || c.TargetPort < 1 || c.TargetPort > 65535 {
		return netip.AddrPort{}, nil, fmt.Errorf("%w，需填写有效的 IPv4 目的地址和端口", ErrInvalid)
	}
	c.TargetIP = ip.String()
	c.Interface = strings.TrimSpace(c.Interface)
	if c.Interface == "" {
		return netip.AddrPortFrom(ip, uint16(c.TargetPort)), nil, nil
	}
	if !ip.IsMulticast() {
		return netip.AddrPort{}, nil, fmt.Errorf("%w，网卡只用于组播发送", ErrInvalid)
	}
	nic, err := net.InterfaceByName(c.Interface)
	if err != nil || nic.Flags&net.FlagUp == 0 || nic.Flags&(net.FlagMulticast|net.FlagLoopback) == 0 {
		return netip.AddrPort{}, nil, fmt.Errorf("%w，所选网卡无法发送 IPv4 组播", ErrInvalid)
	}
	return netip.AddrPortFrom(ip, uint16(c.TargetPort)), nic, nil
}

func (c Config) captureURL(dst netip.AddrPort) string {
	scheme, _, _ := strings.Cut(c.Protocol, "-")
	addr := dst.String()
	if !dst.Addr().IsMulticast() {
		addr = net.JoinHostPort("0.0.0.0", strconv.Itoa(c.TargetPort))
	}
	u := url.URL{Scheme: scheme, Host: addr}
	if c.Interface != "" {
		u.RawQuery = url.Values{"interface": {c.Interface}}.Encode()
	}
	return u.String()
}

func (m *Manager) Send() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.status.Active || !m.status.Config.datagram() {
		return Status{}, ErrSenderInactive
	}
	if m.status.Sending {
		return Status{}, ErrActive
	}
	cfg := m.status.Config
	target, nic, err := cfg.destination()
	if err != nil {
		return Status{}, err
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return Status{}, err
	}
	if target.Addr().IsMulticast() {
		pc := ipv4.NewPacketConn(conn)
		if nic != nil {
			err = pc.SetMulticastInterface(nic)
		}
		if err == nil {
			err = pc.SetMulticastLoopback(true)
		}
		if err == nil {
			err = pc.SetMulticastTTL(1)
		}
		if err != nil {
			conn.Close()
			return Status{}, err
		}
	}
	m.status.SentDatagrams, m.status.FaultHits, m.status.LastFault, m.status.SendError = 0, 0, nil, ""
	m.status.Sending = true
	ctx, id, plan := m.ctx, m.status.ID, m.stream.chunks
	go m.send(ctx, conn, id, target, cfg, plan)
	return m.status, nil
}

func prepareDatagrams(plan []chunk, cfg Config) []chunk {
	if cfg.Protocol == "udp-ts" {
		return plan
	}
	wire := make([]chunk, len(plan))
	ssrc := rand.Uint32()
	for i, ch := range plan {
		// The packet is always a fixed RTP header and a bounded TS payload.
		packet := rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 33, SequenceNumber: uint16(400 + i), SSRC: ssrc, Timestamp: uint32(ch.at.Milliseconds() * 90)}, Payload: ch.data}
		raw, _ := packet.Marshal()
		wire[i] = chunk{at: ch.at, data: raw, fault: ch.fault}
	}
	for i := 1; i+1 < len(wire); i++ {
		if wire[i].at < 2*time.Second {
			continue
		}
		switch cfg.Fault {
		case "rtp_drop":
			wire[i].data = nil
			wire[i].fault = true
		case "rtp_duplicate":
			copyPacket := chunk{at: wire[i].at, data: wire[i].data, fault: true}
			wire = append(wire[:i+1], append([]chunk{copyPacket}, wire[i+1:]...)...)
		case "rtp_reorder":
			wire[i].data, wire[i+1].data = wire[i+1].data, wire[i].data
			wire[i].fault = true
		}
		break
	}
	return wire
}

func (m *Manager) send(ctx context.Context, conn *net.UDPConn, id string, target netip.AddrPort, cfg Config, plan []chunk) {
	defer conn.Close()
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.status.ID == id {
			m.status.Sending = false
		}
	}()
	start := time.Now()
	for _, ch := range prepareDatagrams(plan, cfg) {
		if !waitForChunk(ctx, ctx, ch.at-time.Since(start)) {
			return
		}
		var writeErr error
		if len(ch.data) > 0 {
			conn.SetWriteDeadline(time.Now().Add(time.Second))
			_, writeErr = conn.WriteToUDPAddrPort(ch.data, target)
		}
		m.mu.Lock()
		if m.status.ID == id && m.status.Active {
			switch {
			case writeErr != nil:
				m.status.SendError = writeErr.Error()
			case len(ch.data) > 0:
				m.status.SentDatagrams++
			}
			if ch.fault && writeErr == nil {
				m.status.FaultHits++
				now := time.Now()
				m.status.LastFault = &now
			}
		}
		m.mu.Unlock()
		if writeErr != nil {
			return
		}
	}
}
