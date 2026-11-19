package gb28181

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtp"
)

func (m *PlatformManager) Preview(cfg PreviewConfig) error {
	if cfg.WindowMS == 0 {
		cfg.WindowMS = 4000
	}
	if cfg.MediaTransport == "" {
		cfg.MediaTransport = mediaUDP
	}
	if (cfg.MediaTransport != mediaUDP && cfg.MediaTransport != mediaTCPActive && cfg.MediaTransport != mediaTCPPassive) ||
		(cfg.SkipConnect && cfg.MediaTransport != mediaTCPActive) {
		return fmt.Errorf("%w，选择 UDP、TCP 平台主动或 TCP 平台被动", ErrPlatformConfig)
	}
	if !digits20.MatchString(cfg.ChannelID) || cfg.WindowMS < 500 || cfg.WindowMS > 15000 {
		return fmt.Errorf("%w，填写 20 位通道编号及 500–15000 毫秒的收流窗口", ErrPlatformConfig)
	}
	if cfg.Recording != nil && (cfg.Recording.Start.Unix() <= 0 || cfg.Recording.Start.Unix() >= cfg.Recording.End.Unix() ||
		cfg.Recovery != "" || cfg.SkipACK || cfg.SkipConnect) {
		return fmt.Errorf("%w，录像回放需要起止时间和正常点播配置", ErrPlatformConfig)
	}
	m.mu.Lock()
	p := m.run
	m.mu.Unlock()
	if p == nil {
		return ErrPlatformPeer
	}
	p = p.device(cfg.DeviceID)
	if p == nil {
		return fmt.Errorf("%w，未配置该设备编号", ErrPlatformConfig)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx.Err() != nil || !time.Now().Before(p.expires) {
		return ErrPlatformPeer
	}
	if cfg.Recovery != "" && (cfg.SkipACK || cfg.SkipConnect ||
		(cfg.Recovery != recoveryTCP || p.cfg.SIPTransport != "tcp") &&
			(cfg.Recovery != recoveryUDP || p.cfg.SIPTransport != "udp") &&
			(cfg.Recovery != recoveryMedia || cfg.WindowMS < 8000)) {
		return fmt.Errorf("%w，重连场景需匹配 SIP 传输并正常发送 ACK、建立媒体连接；媒体恢复观测窗口至少为 8000 ms", ErrPlatformConfig)
	}
	if p.busy {
		return ErrActive
	}
	media, err := openPreviewMedia(p.local.Addr(), cfg.MediaTransport)
	if err != nil {
		return err
	}
	report := newPlatformPreview(cfg, media)
	var recovery *PlatformRecovery
	if cfg.Recovery != "" {
		recovery = &PlatformRecovery{Mode: cfg.Recovery, State: "waiting_media", Before: report, registrationBaseline: p.registrations}
		p.ignoredSN = make(map[uint32]struct{})
	}
	var playback *platformPlayback
	if cfg.Recording != nil {
		report.State = "record_info"
		report.SSRC += 1000000000
		report.Recording = &PlatformRecording{PlaybackRange: *cfg.Recording}
		playback = &platformPlayback{report: report, sn: 1 + rand.Uint32N(99999998), records: make(map[string]struct{}), queryDone: make(chan struct{}), ended: make(chan struct{})}
	}
	p.recovery, p.playback = recovery, playback
	p.preview, p.busy = report, true
	peer := p.peer
	p.workers.Go(func() {
		defer func() { p.mu.Lock(); p.busy = false; p.mu.Unlock() }()
		defer media.close()
		if recovery != nil {
			p.runRecovery(cfg, report, media, peer, recovery)
		} else if playback == nil || p.queryRecording(cfg, peer, playback) {
			p.runPreview(cfg, report, media, peer, nil)
		}
	})
	return nil
}

func newPlatformPreview(cfg PreviewConfig, media previewMedia) *PlatformPreview {
	return &PlatformPreview{Start: time.Now(), SSRC: 1 + rand.Uint32N(999999999), State: "inviting",
		CallID: sip.GenerateTagN(24), WindowMS: cfg.WindowMS, RTPAddress: media.address, MediaTransport: cfg.MediaTransport, ChannelID: cfg.ChannelID}
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

func (p *platform) runPreview(cfg PreviewConfig, report *PlatformPreview, media previewMedia, peer netip.AddrPort, onFirstKeyframe func() bool) {
	ip := p.local.Addr().String()
	host := p.cfg.PlatformID[:10]
	recipient := sip.Uri{Scheme: "sip", User: cfg.ChannelID, Host: peer.Addr().String(), Port: int(peer.Port())}
	contact := sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: p.cfg.PlatformID, Host: ip, Port: int(p.local.Port())}}
	req := sip.NewRequest(sip.INVITE, recipient)
	transport := strings.ToUpper(p.cfg.SIPTransport)
	if transport == "TCP" {
		contact.Address.UriParams.Add("transport", "tcp")
	}
	req.SetTransport(transport)
	req.SetDestination(peer.String())
	from := &sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: p.cfg.PlatformID, Host: host}}
	from.Params.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: cfg.ChannelID, Host: host}})
	call := sip.CallIDHeader(report.CallID)
	req.AppendHeader(&call)
	req.AppendHeader(sip.NewHeader("Subject", fmt.Sprintf("%s:%010d,%s:0", cfg.ChannelID, report.SSRC, p.cfg.PlatformID)))
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	protocol, attrs := "RTP/AVP", ""
	if cfg.MediaTransport != mediaUDP {
		protocol = "TCP/RTP/AVP"
		attrs = "a=setup:" + strings.TrimPrefix(string(cfg.MediaTransport), "tcp-") + "\r\na=connection:new\r\n"
	}
	mode, start, end := "Play", int64(0), int64(0)
	var playback *platformPlayback
	if cfg.Recording != nil {
		mode, start, end = "Playback", cfg.Recording.Start.Unix(), cfg.Recording.End.Unix()
		playback = p.playback
		p.finish(report, "inviting", "")
	}
	req.SetBody(fmt.Appendf(nil, "v=0\r\no=%s 0 0 IN IP4 %s\r\ns=%s\r\nc=IN IP4 %s\r\nt=%d %d\r\nm=video %d %s 96\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\n%sy=%010d\r\n",
		p.cfg.PlatformID, ip, mode, ip, start, end, media.port, protocol, attrs, report.SSRC))
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

	if err := dialog.Ack(p.ctx); err != nil {
		p.finish(report, "no_ack", "ACK 发送失败")
		return
	}
	p.mark(report, "ack", "")
	if playback != nil {
		p.mu.Lock()
		playback.dialog = dialog
		p.mu.Unlock()
	}
	interrupted := false
	defer func() {
		if playback != nil {
			playback.control.Lock()
			defer playback.control.Unlock()
			p.mu.Lock()
			playback.dialog, playback.wakeRead = nil, nil
			p.mu.Unlock()
		}
		if !interrupted {
			p.endPreview(report, dialog)
		}
	}()

	answer, answerErr := parseOffer(dialog.InviteResponse.Body())
	mediaIP := peer.Addr()
	if answerErr == nil {
		mediaIP = answer.target.Addr()
	}
	deadline := time.Now().Add(time.Duration(cfg.WindowMS) * time.Millisecond)
	var read func([]byte) (int, string, error)
	var readDeadline func(time.Time) error
	if cfg.MediaTransport == mediaUDP {
		conn := media.udp
		conn.SetReadDeadline(deadline)
		readDeadline = conn.SetReadDeadline
		stop := context.AfterFunc(p.ctx, func() { _ = conn.Close() })
		defer stop()
		read = func(buf []byte) (int, string, error) {
			for {
				n, addr, err := conn.ReadFromUDPAddrPort(buf)
				if err != nil {
					return 0, "", err
				}
				if ip := addr.Addr().Unmap(); ip == mediaIP || ip == peer.Addr() {
					return n, addr.String(), nil
				}
			}
		}
	} else {
		want := "active"
		if cfg.MediaTransport == mediaTCPActive {
			want = "passive"
		}
		if answerErr != nil || answer.setup != want || answer.payload != 96 {
			p.finish(report, "media_negotiation", "TCP 媒体应答须使用互补 setup 角色及 PS/90000")
			return
		}
		p.finish(report, "connecting", "")
		p.mark(report, "media_connect", string(cfg.MediaTransport))
		ctx, cancel := context.WithDeadline(p.ctx, deadline)
		conn, err := p.connectPreview(ctx, media, cfg, answer, peer.Addr())
		cancel()
		if err != nil {
			if p.ctx.Err() != nil {
				p.finish(report, "stopped", "")
			} else {
				p.finish(report, "no_media_connection", err.Error())
			}
			return
		}
		defer conn.Close()
		stop := context.AfterFunc(p.ctx, func() { _ = conn.Close() })
		defer stop()
		conn.SetReadDeadline(deadline)
		readDeadline = conn.SetReadDeadline
		p.mu.Lock()
		report.RTPAddress = conn.LocalAddr().String()
		p.mu.Unlock()
		addr := conn.RemoteAddr().String()
		p.mark(report, "media_connected", addr)
		read = func(buf []byte) (int, string, error) { n, err := readPreviewFrame(conn, buf); return n, addr, err }
	}
	if playback != nil {
		p.mu.Lock()
		playback.wakeRead = func() { _ = readDeadline(time.Now().Add(80 * time.Millisecond)) }
		select {
		case <-playback.ended:
			playback.wakeRead()
		default:
		}
		p.mu.Unlock()
	}
	p.finish(report, "receiving", "")
	buf := make([]byte, 65535)
	var result RTPReview
	var assembly []byte
	var mediaDeadline time.Time
	var seq uint16
	var stamp uint32
	for {
		n, addr, err := read(buf)
		if err != nil {
			var network net.Error
			if !mediaDeadline.IsZero() && mediaDeadline.Before(deadline) && p.ctx.Err() == nil &&
				errors.As(err, &network) && network.Timeout() {
				p.mark(report, "media_timeout", "")
				p.finish(report, "media_timeout", "")
				return
			}
			break
		}
		var packet rtp.Packet
		if packet.Unmarshal(buf[:n]) != nil || packet.Version != 2 || packet.PayloadType != 96 || packet.SSRC != report.SSRC || len(packet.Payload) == 0 {
			continue
		}
		if result.Packets == 0 {
			p.mark(report, "first_rtp", addr)
		}
		if len(assembly) > 0 && (packet.SequenceNumber != seq+1 || packet.Timestamp != stamp || len(assembly)+len(packet.Payload) > 4<<20) {
			assembly = assembly[:0]
			result.PartialUnits++
		}
		seq, stamp = packet.SequenceNumber, packet.Timestamp
		result.Packets++
		assembly = append(assembly, packet.Payload...)
		firstKeyframe := false
		if packet.Marker {
			keys := result.Keyframes
			result.reviewPS(assembly)
			assembly = assembly[:0]
			if keys == 0 && result.Keyframes > 0 {
				p.mark(report, "first_keyframe", "")
				firstKeyframe = true
			}
		}
		if cfg.watchMedia && result.Keyframes > 0 {
			at := time.Now()
			mediaDeadline = at.Add(mediaIdleTimeout)
			_ = readDeadline(earlierTime(mediaDeadline, deadline))
			p.mu.Lock()
			p.recovery.LastRTPAt = at
			p.mu.Unlock()
		}
		p.mu.Lock()
		report.Media = PlatformMedia{Packets: result.Packets, PSUnits: result.PSUnits, PartialUnits: result.PartialUnits, VideoUnits: result.VideoUnits, Keyframes: result.Keyframes}
		p.mu.Unlock()
		if firstKeyframe && onFirstKeyframe != nil && onFirstKeyframe() {
			interrupted = true
			return
		}
	}
	recordingEnded := false
	if playback != nil && result.Packets > 0 {
		// The media TCP socket can close just before the out-of-dialog notification.
		timer := time.NewTimer(350 * time.Millisecond)
		select {
		case <-playback.ended:
			recordingEnded = true
		case <-timer.C:
		case <-p.ctx.Done():
		}
		timer.Stop()
	}
	switch {
	case p.ctx.Err() != nil:
		p.finish(report, "stopped", "")
	case result.Packets == 0:
		p.finish(report, "no_rtp", "ACK 已发送，观测窗口内未收到匹配的 RTP")
	case result.Keyframes == 0:
		p.finish(report, "no_keyframe", "已收到 RTP，观测窗口内未解析出 H.264/H.265 关键帧")
	case recordingEnded:
		p.finish(report, "recording_ended", "")
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
