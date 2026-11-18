package gb28181

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"
	"time"

	"github.com/emiago/sipgo/sip"
)

//go:generate ffmpeg -hide_banner -loglevel error -y -f lavfi -i testsrc2=size=320x180:rate=15 -t 2 -c:v libx264 -preset medium -crf 28 -pix_fmt yuv420p -x264-params aud=1:repeat-headers=1:keyint=30:scenecut=0:bframes=0 -f h264 test-pattern.h264
//go:embed test-pattern.h264
var testPattern []byte

const testFPS = 15

var (
	audPrefix  = []byte{0, 0, 0, 1, 9}
	testFrames = bytes.Split(testPattern, audPrefix)[1:]
)

type mediaSession struct {
	offer     mediaOffer
	evidence  *sessionEvidence
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
	if req.Recipient.User != d.cfg.DeviceID && req.Recipient.User != d.cfg.ChannelID {
		d.respond(req, tx, 404, "Not Found")
		return
	}
	offer, err := parseOffer(req.Body())
	if err != nil {
		reason := "点播协商失败：" + err.Error()
		d.mu.Lock()
		if d.status.State == "registered" && flow.ctx.Err() == nil && d.media == nil {
			d.newEvidence(req.CallID().Value(), "").mark("failed", reason, time.Now(), true)
			d.status.MediaState, d.status.MediaError = "failed", reason
			d.status.MediaTransport, d.status.MediaLocal, d.status.MediaTarget, d.status.RTPPackets = "", "", "", 0
			d.status.VideoFrames, d.status.AudioPackets = 0, 0
		}
		d.mu.Unlock()
		d.log("system", reason, "")
		d.respond(req, tx, 488, "Not Acceptable Here")
		return
	}

	flow.awaitRegistration()
	d.mu.Lock()
	if d.status.State != "registered" || flow.ctx.Err() != nil {
		d.mu.Unlock()
		d.respond(req, tx, 480, "Temporarily Unavailable")
		return
	}
	callID := req.CallID().Value()
	remoteTag, _ := req.From().Params.Get("tag")
	if active := d.media; active != nil {
		d.mu.Unlock()
		if callID == active.callID && remoteTag == active.remoteTag && req.CSeq().SeqNo == active.cseq {
			_ = tx.Respond(active.answer.Clone())
		} else {
			d.respond(req, tx, 486, "Busy Here")
		}
		return
	}

	session := &mediaSession{offer: offer, evidence: d.newEvidence(callID, offer.transport), localIP: flow.local.Addr().AsSlice(), callID: callID, remoteTag: remoteTag,
		cseq: req.CSeq().SeqNo, acked: make(chan struct{})}
	d.status.MediaTransport = offer.transport
	d.status.VideoFrames, d.status.AudioPackets = 0, 0
	d.status.MediaLocal, d.status.MediaTarget, d.status.MediaError, d.status.RTPPackets = "", "", "", 0
	if offer.transport != mediaTCPPassive {
		d.status.MediaTarget = offer.target.String()
	}
	port, err := session.prepare(session.localIP)
	if err != nil {
		reason := "无法绑定本机媒体端口：" + err.Error()
		session.evidence.mark("failed", reason, time.Now(), true)
		d.status.MediaState, d.status.MediaError = "failed", reason
		d.mu.Unlock()
		d.log("system", reason, "")
		d.respond(req, tx, 500, "Server Internal Error")
		return
	}
	ip := flow.local.Addr().String()
	if offer.transport != mediaTCPActive {
		d.status.MediaLocal = net.JoinHostPort(ip, strconv.Itoa(port))
	}
	answer := sip.NewResponseFromRequest(req, 200, "OK", session.sdpAnswer(req.Recipient.User, ip, port))
	contact := d.contact(flow)
	contact.Address.User = req.Recipient.User
	answer.AppendHeader(contact)
	answer.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	session.localTag, _ = answer.To().Params.Get("tag")
	session.ctx, session.cancel = context.WithCancel(flow.ctx)
	session.answer = answer
	d.media = session
	d.status.MediaState = "waiting_ack"
	d.workers.Add(1)
	d.mu.Unlock()

	if err := tx.Respond(answer.Clone()); err != nil {
		defer d.workers.Done()
		d.failMedia(session, "waiting_ack", "点播应答发送失败："+err.Error())
		return
	}
	// Keep the server transaction available for ACKs and 2xx retransmissions.
	// sipgo already runs each incoming request in its own goroutine.
	d.waitForAck(session, tx)
}

func (d *device) waitForAck(session *mediaSession, tx sip.ServerTransaction) {
	defer d.workers.Done()
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
	failed := d.media == session && d.status.MediaState == phase && session.ctx.Err() == nil
	if failed {
		session.evidence.mark("failed", reason, time.Now(), true)
		d.closeMediaLocked()
		d.status.MediaState, d.status.MediaError = "failed", reason
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

func (d *device) onAck(req *sip.Request, _ sip.ServerTransaction) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.media
	if s == nil || s.started || !s.matches(req) || req.CSeq() == nil || req.CSeq().SeqNo != s.cseq ||
		s.ctx.Err() != nil || d.status.State != "registered" {
		return
	}
	s.started = true
	close(s.acked)
	if s.offer.transport == mediaUDP {
		d.status.MediaState = "streaming"
	} else {
		d.status.MediaState = "connecting"
	}
	d.workers.Add(1)
	go d.stream(s)
}

func (d *device) onBye(req *sip.Request, tx sip.ServerTransaction) {
	d.mu.Lock()
	matched := d.media != nil && d.media.matches(req)
	if matched {
		d.closeMediaLocked()
	}
	d.mu.Unlock()
	if !matched {
		d.respond(req, tx, 481, "Call/Transaction Does Not Exist")
		return
	}
	d.respond(req, tx, 200, "OK")
	d.log("system", "平台已结束点播", "")
}

func (d *device) closeMedia() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closeMediaLocked()
}

func (d *device) closeMediaLocked() {
	if s := d.media; s != nil {
		s.evidence.mark("ended", "", time.Now(), true)
		s.close()
		d.media = nil
		d.status.MediaState = "idle"
	}
}

func (d *device) stream(session *mediaSession) {
	defer d.workers.Done()
	ctx := session.ctx
	conn, err := session.connect(session.localIP, d.platform.Addr().AsSlice())
	if err != nil {
		d.failMedia(session, "connecting", err.Error())
		return
	}
	d.mu.Lock()
	if d.media != session || ctx.Err() != nil {
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
	d.status.MediaState = phase
	d.status.MediaLocal, d.status.MediaTarget = conn.LocalAddr().String(), conn.RemoteAddr().String()
	d.mu.Unlock()

	source := "测试画面"
	audioCodec := "none"
	if d.file != nil {
		source = "本地视频 " + d.file.info.Filename
		audioCodec = d.file.info.AudioCodec
	}
	d.log("system", fmt.Sprintf("点播%s（%s）→ %s", source, session.offer.transport, conn.RemoteAddr()), "")
	muxer := newProgramMuxer(audioCodec)
	seq := uint16(rand.Uint32())
	rtpBase := rand.Uint32()
	faults := mediaFaults{config: d.cfg.Faults}
	mark := func(stage, detail string) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.media == session {
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
			if d.media == session {
				d.status.MediaState = "delaying"
			}
			d.mu.Unlock()
			if err := faults.delay(ctx, mark); err != nil {
				return
			}
			d.mu.Lock()
			if d.media == session {
				d.status.MediaState = phase
			}
			d.mu.Unlock()
		}
	}
	firstPacket, firstKey := true, true
	capture := func(packet []byte) {
		if !firstPacket && !session.evidence.CaptureEnabled {
			return
		}
		d.mu.Lock()
		if d.media == session {
			session.evidence.capture(packet, firstPacket)
		}
		d.mu.Unlock()
		firstPacket = false
	}
	send := func(unit mediaUnit) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		inside, mute := faults.apply(&unit, mark)
		if mute {
			stats.MutedAudio++
			mark("fault_mute", "")
			d.mu.Lock()
			if d.media == session {
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
		if d.media == session {
			d.status.RTPPackets += packets
			if faults.config != nil {
				snapshot := stats
				session.evidence.FaultStats = &snapshot
			}
			if err == nil && dropped == faults.packets.dropped {
				if unit.stream == videoStreamID {
					d.status.VideoFrames++
					if unit.key && firstKey {
						session.evidence.mark("first_keyframe", fmt.Sprintf("PTS=%d, DTS=%d (90 kHz)", unit.pts, unit.dts), time.Now(), true)
						firstKey = false
					}
				} else {
					d.status.AudioPackets++
				}
				phase = "streaming"
				d.status.MediaState = phase
			}
		}
		d.mu.Unlock()
		if err != nil {
			return fmt.Errorf("RTP 发送失败：%w", err)
		}
		return nil
	}
	if d.file != nil {
		err = d.file.play(ctx, send)
	} else {
		err = playTestPattern(ctx, send)
	}
	if err != nil {
		d.failMedia(session, phase, err.Error())
	}
}

func playTestPattern(ctx context.Context, send func(mediaUnit) error) error {
	ticker := time.NewTicker(time.Second / testFPS)
	defer ticker.Stop()
	var pts int64
	var data []byte
	for index := 0; ; index = (index + 1) % len(testFrames) {
		data = append(data[:0], audPrefix...)
		data = append(data, testFrames[index]...)
		if err := send(mediaUnit{stream: videoStreamID, data: data, pts: pts, dts: pts, key: index == 0}); err != nil {
			return err
		}
		pts += 90000 / testFPS
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
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
