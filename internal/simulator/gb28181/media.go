package gb28181

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"strconv"
	"time"

	"streamanalyzer/internal/simulator/testpattern"

	"github.com/emiago/sipgo/sip"
)

type mediaSession struct {
	channelID string
	status    *MediaStatus
	offer     mediaOffer
	evidence  *sessionEvidence
	flow      *sipFlow
	playback  *playbackSession
	localIP   net.IP
	conn      net.Conn
	listener  *net.TCPListener
	callID    string
	localTag  string
	remoteTag string
	cseq      uint32
	answer    *sip.Response
	acked     chan struct{}
	started   bool
	ctx       context.Context
	cancel    context.CancelFunc
}

func (d *device) onInvite(flow *sipFlow, req *sip.Request, tx sip.ServerTransaction) {
	if req.CallID() == nil || req.CSeq() == nil || req.From() == nil || req.To() == nil {
		d.respond(req, tx, 400, "Bad Request")
		return
	}
	channelID := d.channelFor(req.Recipient.User)
	if channelID == "" {
		d.respond(req, tx, 404, "Not Found")
		return
	}
	status := d.channelStatus[channelID]
	offer, err := parseOffer(req.Body())
	if err != nil {
		reason := "点播协商失败：" + err.Error()
		d.mu.Lock()
		if d.status.State == "registered" && flow.ctx.Err() == nil && !d.offline[channelID] && d.media[channelID] == nil {
			d.newEvidence(channelID, req.CallID().Value(), "").mark("failed", reason, time.Now(), true)
			*status = MediaStatus{MediaState: "failed", MediaError: reason}
		}
		d.mu.Unlock()
		d.log("system", reason, "")
		d.respond(req, tx, 488, "Not Acceptable Here")
		return
	}

	flow.awaitRegistration()
	d.mu.Lock()
	if d.status.State != "registered" || flow.ctx.Err() != nil || d.offline[channelID] {
		d.mu.Unlock()
		d.respond(req, tx, 480, "Temporarily Unavailable")
		return
	}
	callID := req.CallID().Value()
	remoteTag, _ := req.From().Params.Get("tag")
	if active := d.media[channelID]; active != nil && active.callID != callID && status.MediaState == "playback_done" {
		d.closeMediaLocked(active)
	}
	if active := d.media[channelID]; active != nil {
		d.mu.Unlock()
		if callID == active.callID && remoteTag == active.remoteTag && req.CSeq().SeqNo == active.cseq {
			_ = tx.Respond(active.answer.Clone())
		} else {
			d.respond(req, tx, 486, "Busy Here")
		}
		return
	}

	select {
	case d.fleet.mediaSlots <- struct{}{}:
	default:
		d.mu.Unlock()
		d.log("system", "当前批次已达并发媒体会话上限", "")
		d.respond(req, tx, 503, "Service Unavailable")
		return
	}
	session := &mediaSession{channelID: channelID, status: status, offer: offer, flow: flow,
		evidence: d.newEvidence(channelID, callID, offer.transport), localIP: flow.local.Addr().AsSlice(),
		callID: callID, remoteTag: remoteTag, cseq: req.CSeq().SeqNo, acked: make(chan struct{})}
	*status = MediaStatus{MediaTransport: offer.transport}
	if offer.transport != mediaTCPPassive {
		status.MediaTarget = offer.target.String()
	}
	if offer.mode == "Playback" {
		span, err := d.file.playbackRange(&session.offer)
		session.evidence.Mode = "playback"
		if err != nil {
			reason := "录像回放协商失败：" + err.Error()
			session.evidence.mark("failed", reason, time.Now(), true)
			status.MediaState, status.MediaError = "failed", reason
			<-d.fleet.mediaSlots
			d.mu.Unlock()
			d.respond(req, tx, 488, "Not Acceptable Here")
			return
		}
		session.playback = &playbackSession{window: span, commands: make(chan playbackCommand, 8)}
	}
	port, err := session.prepare(session.localIP)
	if err != nil {
		reason := "无法绑定本机媒体端口：" + err.Error()
		session.evidence.mark("failed", reason, time.Now(), true)
		status.MediaState, status.MediaError = "failed", reason
		<-d.fleet.mediaSlots
		d.mu.Unlock()
		d.log("system", reason, "")
		d.respond(req, tx, 500, "Server Internal Error")
		return
	}
	ip := flow.local.Addr().String()
	if offer.transport != mediaTCPActive {
		status.MediaLocal = net.JoinHostPort(ip, strconv.Itoa(port))
	}
	answer := sip.NewResponseFromRequest(req, 200, "OK", session.sdpAnswer(req.Recipient.User, ip, port))
	contact := d.contact(flow)
	contact.Address.User = req.Recipient.User
	answer.AppendHeader(contact)
	answer.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	session.localTag, _ = answer.To().Params.Get("tag")
	session.evidence.localTag = session.localTag
	session.ctx, session.cancel = context.WithCancel(flow.ctx)
	session.answer = answer
	d.media[channelID] = session
	status.MediaState = "waiting_ack"
	d.workers.Add(1)
	d.mu.Unlock()

	// The INVITE worker owns the slot until a matching ACK starts the stream.
	defer d.workers.Done()
	defer func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if !session.started {
			<-d.fleet.mediaSlots
		}
	}()
	if err := tx.Respond(answer.Clone()); err != nil {
		d.failMedia(session, "waiting_ack", "点播应答发送失败："+err.Error())
		return
	}
	// Keep the server transaction available for ACKs and 2xx retransmissions.
	// sipgo already runs each incoming request in its own goroutine.
	d.waitForAck(session, tx)
}

func (d *device) waitForAck(session *mediaSession, tx sip.ServerTransaction) {
	deadline := time.NewTimer(32 * time.Second)
	defer deadline.Stop()
	interval := 500 * time.Millisecond
	retry := time.NewTimer(interval)
	defer retry.Stop()
	for {
		select {
		// A 2xx ACK normally has a separate Via branch. Some platforms
		// reuse the INVITE branch and deliver it to the same transaction.
		case ack := <-tx.Acks():
			if ack != nil {
				d.onAck(ack, nil)
			}
		case <-session.acked:
			return
		case <-session.ctx.Done():
			return
		case <-retry.C:
			if err := tx.Respond(session.answer.Clone()); err != nil {
				d.failMedia(session, "waiting_ack", "点播应答重发失败："+err.Error())
				return
			}
			interval = min(2*interval, 4*time.Second)
			retry.Reset(interval)
		case <-deadline.C:
			d.failMedia(session, "waiting_ack", "未收到点播 ACK，已释放媒体端口")
			return
		case <-tx.Done():
			d.failMedia(session, "waiting_ack", "点播事务结束前未收到 ACK")
			return
		}
	}
}

func (d *device) failMedia(session *mediaSession, phase, reason string) {
	d.mu.Lock()
	failed := d.media[session.channelID] == session && session.status.MediaState == phase && session.ctx.Err() == nil
	if failed {
		session.evidence.mark("failed", reason, time.Now(), true)
		d.closeMediaLocked(session)
		session.status.MediaState, session.status.MediaError = "failed", reason
	}
	d.mu.Unlock()
	if failed {
		d.log("system", reason, "")
	}
}

func (s *mediaSession) matches(req *sip.Request) bool {
	if req.CallID() == nil || req.From() == nil || req.To() == nil || req.CallID().Value() != s.callID {
		return false
	}
	from, _ := req.From().Params.Get("tag")
	to, _ := req.To().Params.Get("tag")
	return from == s.remoteTag && to == s.localTag
}

// dialog is called with d.mu held.
func (d *device) dialog(req *sip.Request) *mediaSession {
	for _, session := range d.media {
		if session.matches(req) {
			return session
		}
	}
	return nil
}

func (d *device) onAck(req *sip.Request, _ sip.ServerTransaction) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.dialog(req)
	if s == nil || s.started || req.CSeq() == nil || req.CSeq().SeqNo != s.cseq ||
		s.ctx.Err() != nil || d.status.State != "registered" {
		return
	}
	s.started = true
	close(s.acked)
	if s.offer.transport == mediaUDP {
		s.status.MediaState = "streaming"
	} else {
		s.status.MediaState = "connecting"
	}
	d.workers.Add(1)
	go d.stream(s)
}

func (d *device) onBye(req *sip.Request, tx sip.ServerTransaction) {
	if d.readBroadcastBye(req, tx) {
		return
	}
	d.mu.Lock()
	session := d.dialog(req)
	if session != nil {
		d.closeMediaLocked(session)
	}
	d.mu.Unlock()
	if session == nil {
		d.respond(req, tx, 481, "Call/Transaction Does Not Exist")
		return
	}
	d.respond(req, tx, 200, "OK")
	d.log("system", "平台已结束点播 "+session.channelID, "")
}

func (d *device) closeMedia() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if b := d.broadcast; b != nil {
		b.cancel()
	}
	for _, session := range d.media {
		d.closeMediaLocked(session)
	}
}

func (d *device) closeMediaLocked(session *mediaSession) {
	session.evidence.mark("ended", "", time.Now(), true)
	session.close()
	delete(d.media, session.channelID)
	session.status.MediaState = "idle"
}

func (d *device) stream(session *mediaSession) {
	defer d.workers.Done()
	defer func() { <-d.fleet.mediaSlots }()
	ctx := session.ctx
	conn, err := session.connect(session.localIP, d.platform.Addr().AsSlice())
	if err != nil {
		d.failMedia(session, "connecting", err.Error())
		return
	}
	d.mu.Lock()
	if d.media[session.channelID] != session || ctx.Err() != nil {
		d.mu.Unlock()
		conn.Close()
		return
	}
	session.conn = conn
	session.evidence.mark("media_connected", conn.LocalAddr().String()+" → "+conn.RemoteAddr().String(), time.Now(), true)
	phase := "streaming"
	if d.file != nil {
		phase = "starting_source"
	}
	session.status.MediaState = phase
	session.status.MediaLocal, session.status.MediaTarget = conn.LocalAddr().String(), conn.RemoteAddr().String()
	d.mu.Unlock()

	source := "测试画面"
	videoCodec, audioCodec := "h264", "none"
	if d.cfg.Voice {
		audioCodec = "g711a"
	}
	if d.file != nil {
		source = "本地视频 " + d.file.info.Filename
		videoCodec, audioCodec = d.file.info.OutputCodec, d.file.info.AudioCodec
	}
	d.log("system", fmt.Sprintf("通道 %s 点播%s（%s）→ %s", session.channelID, source, session.offer.transport, conn.RemoteAddr()), "")
	muxer := newProgramMuxer(videoCodec, audioCodec)
	seq := uint16(rand.Uint32())
	rtpBase := rand.Uint32()
	faults := mediaFaults{config: d.cfg.Faults}
	mark := func(stage, detail string) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.media[session.channelID] == session {
			session.evidence.mark(stage, detail, time.Now(), true)
		}
	}
	var stats FaultStats
	if faults.config != nil {
		if session.offer.transport == mediaUDP {
			faults.packets.dropEvery = uint64(faults.config.DropEvery)
			faults.packets.reorderEvery = uint64(faults.config.ReorderEvery)
		} else if faults.config.DropEvery != 0 || faults.config.ReorderEvery != 0 {
			mark("fault_udp_only", "当前媒体为 TCP")
		}
		if faults.config.FirstDelayMS != 0 {
			d.mu.Lock()
			if d.media[session.channelID] == session {
				session.status.MediaState = "delaying"
			}
			d.mu.Unlock()
			if err := faults.delay(ctx, mark); err != nil {
				return
			}
			d.mu.Lock()
			if d.media[session.channelID] == session {
				session.status.MediaState = phase
			}
			d.mu.Unlock()
		}
	}
	firstPacket, firstKey := true, true
	var silenceAt time.Time
	silenced := false
	capture := func(packet []byte) {
		if !firstPacket && !session.evidence.CaptureEnabled {
			return
		}
		d.mu.Lock()
		if d.media[session.channelID] == session {
			session.evidence.capture(packet, firstPacket)
		}
		d.mu.Unlock()
		firstPacket = false
	}
	send := func(unit mediaUnit) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !silenceAt.IsZero() && !time.Now().Before(silenceAt) {
			if !silenced {
				mark("fault_media_silent", "")
				silenced = true
			}
			return nil
		}
		inside, mute := faults.apply(&unit, mark)
		if mute {
			stats.MutedAudio++
			mark("fault_mute", "")
			d.mu.Lock()
			if d.media[session.channelID] == session {
				snapshot := stats
				session.evidence.FaultStats = &snapshot
			}
			d.mu.Unlock()
			return nil
		}
		var rules *packetFaults
		if inside && (faults.packets.dropEvery != 0 || faults.packets.reorderEvery != 0) {
			rules = &faults.packets
		}
		ps := muxer.pack(unit)
		dropped := faults.packets.dropped
		packets, err := sendRTP(conn, session.offer, ps, rtpBase+uint32(unit.dts), &seq, capture, rules)
		if faults.packets.dropped != stats.DroppedRTP {
			stats.DroppedRTP = faults.packets.dropped
			mark("fault_loss", "")
		}
		if faults.packets.reordered != stats.ReorderedRTP {
			stats.ReorderedRTP = faults.packets.reordered
			mark("fault_reorder", "")
		}
		d.mu.Lock()
		if d.media[session.channelID] == session {
			session.status.RTPPackets += packets
			if faults.config != nil {
				snapshot := stats
				session.evidence.FaultStats = &snapshot
			}
			if err == nil && dropped == faults.packets.dropped {
				if unit.stream == videoStreamID {
					session.status.VideoFrames++
					if unit.key && firstKey {
						session.evidence.mark("first_keyframe", fmt.Sprintf("PTS=%d, DTS=%d (90 kHz)", unit.pts, unit.dts), time.Now(), true)
						firstKey = false
						if session.playback == nil && faults.config != nil && faults.config.SilenceAfterMS > 0 &&
							d.silenceUsed.CompareAndSwap(false, true) {
							silenceAt = time.Now().Add(time.Duration(faults.config.SilenceAfterMS) * time.Millisecond)
						}
					}
				} else {
					session.status.AudioPackets++
				}
				phase = "streaming"
				session.status.MediaState = phase
			}
		}
		d.mu.Unlock()
		if err != nil {
			return fmt.Errorf("RTP 发送失败：%w", err)
		}
		return nil
	}
	if session.playback != nil {
		controlMark := func(stage, detail string) {
			d.mu.Lock()
			defer d.mu.Unlock()
			if d.media[session.channelID] != session {
				return
			}
			session.evidence.mark(stage, detail, time.Now(), false)
			switch stage {
			case "playback_pause":
				phase = "paused"
			default:
				phase = "streaming"
			}
			session.status.MediaState = phase
		}
		err = session.playback.run(ctx, d.file, send, controlMark)
		if errors.Is(err, io.EOF) {
			d.finishPlayback(session)
			return
		}
	} else if d.file != nil {
		err = d.file.play(ctx, send)
	} else {
		err = playTestPattern(ctx, d.cfg.Voice, send)
	}
	if err != nil {
		d.failMedia(session, phase, err.Error())
	}
}

func playTestPattern(ctx context.Context, withTone bool, send func(mediaUnit) error) error {
	send = paceMedia(ctx, 1, send)
	var tone []byte
	if withTone {
		tone = talkbackTone()
	}
	var audioPTS, videoPTS int64
	for index := 0; ; {
		if len(tone) > 0 && audioPTS < videoPTS {
			const samples = 160
			offset := int(audioPTS/(mediaClock/50)%int64(len(tone)/samples)) * samples
			if err := send(mediaUnit{stream: audioStreamID, data: tone[offset : offset+samples], pts: audioPTS, dts: audioPTS}); err != nil {
				return err
			}
			audioPTS += mediaClock / 50
			continue
		}
		data := testpattern.AccessUnit(index)
		if err := send(mediaUnit{stream: videoStreamID, data: data, pts: videoPTS, dts: videoPTS, key: index == 0}); err != nil {
			return err
		}
		index = (index + 1) % testpattern.Len()
		videoPTS += mediaClock / testpattern.FPS
	}
}

func sendRTP(conn net.Conn, offer mediaOffer, ps []byte, stamp uint32, seq *uint16, observe func([]byte), faults *packetFaults) (uint64, error) {
	const maxPayload = 1200
	if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return 0, err
	}
	var buffer [2 + 12 + maxPayload]byte
	datagram := buffer[2:]
	datagram[0] = 0x80
	binary.BigEndian.PutUint32(datagram[4:8], stamp)
	binary.BigEndian.PutUint32(datagram[8:12], offer.ssrc)
	var sent uint64
	var held []byte
	write := func(packet, raw []byte) error {
		if _, err := conn.Write(packet); err != nil {
			return err
		}
		observe(raw)
		sent++
		return nil
	}
	for len(ps) > 0 {
		size := min(maxPayload, len(ps))
		datagram[1] = offer.payload
		if size == len(ps) {
			datagram[1] |= 0x80
		}
		binary.BigEndian.PutUint16(datagram[2:4], *seq)
		copy(datagram[12:], ps[:size])
		packet := datagram[:12+size]
		if offer.transport != mediaUDP {
			binary.BigEndian.PutUint16(buffer[:2], uint16(len(packet)))
			packet = buffer[:2+len(packet)]
		}
		*seq = *seq + 1
		ps = ps[size:]
		if faults != nil {
			faults.index++
			if faults.dropEvery != 0 && faults.index%faults.dropEvery == 0 {
				faults.dropped++
				continue
			}
			if held == nil && len(ps) > 0 && faults.reorderEvery != 0 && faults.index%faults.reorderEvery == 0 {
				held = bytes.Clone(packet)
				continue
			}
		}
		if err := write(packet, datagram[:12+size]); err != nil {
			return sent, err
		}
		if held != nil {
			if err := write(held, held); err != nil {
				return sent, err
			}
			faults.reordered += 2
			held = nil
		}
	}
	if held != nil {
		if err := write(held, held); err != nil {
			return sent, err
		}
	}
	return sent, nil
}
