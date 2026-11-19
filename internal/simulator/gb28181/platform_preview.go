package gb28181

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtp"
)

func (m *PlatformManager) Preview(cfg PreviewConfig) error {
	if cfg.WindowMS == 0 {
		cfg.WindowMS = 4000
	}
	if !digits20.MatchString(cfg.ChannelID) || cfg.WindowMS < 500 || cfg.WindowMS > 15000 {
		return fmt.Errorf("%w，填写 20 位通道编号及 500–15000 毫秒的收流窗口", ErrPlatformConfig)
	}
	m.mu.Lock()
	p := m.run
	m.mu.Unlock()
	if p == nil {
		return ErrPlatformPeer
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx.Err() != nil || !time.Now().Before(p.expires) {
		return ErrPlatformPeer
	}
	if p.busy {
		return ErrActive
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: p.local.Addr().AsSlice()})
	if err != nil {
		return err
	}
	report := &PlatformPreview{Start: time.Now(), SSRC: 1 + rand.Uint32N(999999999), State: "inviting",
		CallID: sip.GenerateTagN(24), WindowMS: cfg.WindowMS, RTPAddress: conn.LocalAddr().String(), ChannelID: cfg.ChannelID}
	p.preview, p.busy = report, true
	peer := p.peer
	p.workers.Go(func() {
		defer conn.Close()
		defer func() { p.mu.Lock(); p.busy = false; p.mu.Unlock() }()
		p.runPreview(cfg, report, conn, peer)
	})
	return nil
}

func (p *platform) mark(report *PlatformPreview, stage, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	report.Timeline = append(report.Timeline, SessionMark{Stage: stage, Detail: detail, OffsetMS: float64(time.Since(report.Start)) / float64(time.Millisecond)})
}

func (p *platform) finish(report *PlatformPreview, state, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	report.State, report.Error = state, detail
}

func (p *platform) runPreview(cfg PreviewConfig, report *PlatformPreview, conn *net.UDPConn, peer netip.AddrPort) {
	ip := p.local.Addr().String()
	host := p.cfg.PlatformID[:10]
	recipient := sip.Uri{Scheme: "sip", User: cfg.ChannelID, Host: peer.Addr().String(), Port: int(peer.Port())}
	contact := sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: p.cfg.PlatformID, Host: ip, Port: int(p.local.Port())}}
	req := sip.NewRequest(sip.INVITE, recipient)
	req.SetTransport("UDP")
	req.SetDestination(peer.String())
	from := &sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: p.cfg.PlatformID, Host: host}}
	from.Params.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: cfg.ChannelID, Host: host}})
	call := sip.CallIDHeader(report.CallID)
	req.AppendHeader(&call)
	req.AppendHeader(sip.NewHeader("Subject", fmt.Sprintf("%s:%010d,%s:0", cfg.ChannelID, report.SSRC, p.cfg.PlatformID)))
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.SetBody(fmt.Appendf(nil, "v=0\r\no=%s 0 0 IN IP4 %s\r\ns=Play\r\nc=IN IP4 %s\r\nt=0 0\r\nm=video %d RTP/AVP 96\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\ny=%010d\r\n",
		p.cfg.PlatformID, ip, ip, conn.LocalAddr().(*net.UDPAddr).Port, report.SSRC))
	ua := sipgo.DialogUA{Client: p.client, ContactHDR: contact, RewriteContact: true}
	p.mark(report, "invite", "")
	ctx, cancel := context.WithTimeoutCause(context.Background(), 5*time.Second, sipgo.WaitAnswerForceCancelErr)
	wait, force := context.WithCancelCause(ctx)
	stopWait := context.AfterFunc(p.ctx, func() { force(sipgo.WaitAnswerForceCancelErr) })
	dialog, err := ua.WriteInvite(wait, req)
	if err == nil {
		defer dialog.Close()
		err = dialog.WaitAnswer(wait, sipgo.AnswerOptions{})
	}
	stopWait()
	force(nil)
	cancel()
	if err != nil {
		if dialog != nil && dialog.InviteResponse != nil {
			p.mu.Lock()
			report.SIPCode = dialog.InviteResponse.StatusCode
			p.mu.Unlock()
		}
		p.finish(report, "invite_failed", "INVITE 未收到可用的成功应答")
		return
	}
	p.mu.Lock()
	report.SIPCode = dialog.InviteResponse.StatusCode
	p.mu.Unlock()
	p.mark(report, "invite_ok", "")
	if dialog.InviteResponse.Contact() == nil {
		p.finish(report, "invite_failed", "INVITE 应答缺少 Contact")
		return
	}

	if cfg.SkipACK {
		timer := time.NewTimer(time.Duration(cfg.WindowMS) * time.Millisecond)
		defer timer.Stop()
		p.mark(report, "ack_withheld", "")
		select {
		case <-timer.C:
		case <-p.ctx.Done():
		}
		if p.ctx.Err() == nil {
			p.finish(report, "no_ack", "观测期间未发送 ACK")
		} else {
			p.finish(report, "stopped", "")
		}
		// Acknowledge after the observation window so the test device can release the dialog.
		if dialog.Ack(context.Background()) == nil {
			p.mark(report, "cleanup_ack", "")
			p.endPreview(report, dialog)
		}
		return
	}

	stop := context.AfterFunc(p.ctx, func() { _ = conn.Close() })
	defer stop()
	if err := dialog.Ack(p.ctx); err != nil {
		p.finish(report, "no_ack", "ACK 发送失败")
		return
	}
	p.mark(report, "ack", "")
	p.finish(report, "receiving", "")
	defer p.endPreview(report, dialog)
	mediaIP := peer.Addr()
	if answer, err := parseOffer(dialog.InviteResponse.Body()); err == nil {
		mediaIP = answer.target.Addr()
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Duration(cfg.WindowMS) * time.Millisecond))
	buf := make([]byte, 65535)
	var result RTPReview
	var assembly []byte
	var seq uint16
	var stamp uint32
	for {
		n, addr, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			break
		}
		if src := addr.Addr().Unmap(); src != mediaIP && src != peer.Addr() {
			continue
		}
		var packet rtp.Packet
		if packet.Unmarshal(buf[:n]) != nil || packet.Version != 2 || packet.PayloadType != 96 || packet.SSRC != report.SSRC || len(packet.Payload) == 0 {
			continue
		}
		if result.Packets == 0 {
			p.mark(report, "first_rtp", addr.String())
		}
		if len(assembly) > 0 && (packet.SequenceNumber != seq+1 || packet.Timestamp != stamp || len(assembly)+len(packet.Payload) > 4<<20) {
			assembly = assembly[:0]
			result.PartialUnits++
		}
		seq, stamp = packet.SequenceNumber, packet.Timestamp
		result.Packets++
		assembly = append(assembly, packet.Payload...)
		if packet.Marker {
			keys := result.Keyframes
			result.reviewPS(assembly)
			assembly = assembly[:0]
			if keys == 0 && result.Keyframes > 0 {
				p.mark(report, "first_keyframe", "")
			}
		}
		p.mu.Lock()
		report.Media = PlatformMedia{Packets: result.Packets, PSUnits: result.PSUnits, PartialUnits: result.PartialUnits, VideoUnits: result.VideoUnits, Keyframes: result.Keyframes}
		p.mu.Unlock()
	}
	switch {
	case p.ctx.Err() != nil:
		p.finish(report, "stopped", "")
	case result.Packets == 0:
		p.finish(report, "no_rtp", "ACK 已发送，观测窗口内未收到匹配的 RTP")
	case result.Keyframes == 0:
		p.finish(report, "no_keyframe", "已收到 RTP，观测窗口内未解析出 H.264/H.265 关键帧")
	default:
		p.finish(report, "passed", "")
	}
}

func (p *platform) endPreview(report *PlatformPreview, dialog *sipgo.DialogClientSession) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := dialog.Bye(ctx); err == nil {
		p.mark(report, "bye", "")
	}
}
