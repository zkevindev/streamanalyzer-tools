package gb28181

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/zaf/g711"
)

type BroadcastStatus struct {
	OutputID string `json:"output_id"`
	State    string `json:"state"`
	SourceID string `json:"source_id,omitempty"`
	CallID   string `json:"call_id,omitempty"`
	Codec    string `json:"codec,omitempty"`
	Local    string `json:"local,omitempty"`
	Remote   string `json:"remote,omitempty"`
	Packets  uint64 `json:"packets"`
	Peak     int    `json:"peak_pcm"`
	Error    string `json:"error,omitempty"`
}

type broadcastSession struct {
	flow     *sipFlow
	ctx      context.Context
	cancel   context.CancelFunc
	dialog   *sipgo.DialogClientSession
	dialogID string
}

func (d *device) voiceOutputItem() string {
	return fmt.Sprintf("<Item><DeviceID>%s</DeviceID><Name>Test Speaker</Name><Manufacturer>Stream Analyzer</Manufacturer>"+
		"<Model>Simulator</Model><Owner>Simulator</Owner><CivilCode>%s</CivilCode><Address>Laboratory</Address>"+
		"<Parental>0</Parental><ParentID>%s</ParentID><SafetyWay>0</SafetyWay><RegisterWay>1</RegisterWay><Secrecy>0</Secrecy><Status>ON</Status></Item>",
		d.outputID, d.outputID[:6], d.cfg.DeviceID)
}

func (d *device) onBroadcast(flow *sipFlow, req *sip.Request, tx sip.ServerTransaction, query simulatorQuery) {
	if d.outputID == "" {
		d.respond(req, tx, 501, "Not Implemented")
		return
	}
	if !digits20.MatchString(query.SourceID) || !digits20.MatchString(query.TargetID) {
		d.respond(req, tx, 400, "Bad Request")
		return
	}
	if query.TargetID != d.outputID && d.channelFor(query.TargetID) == "" {
		d.respond(req, tx, 404, "Not Found")
		return
	}
	flow.awaitRegistration()
	d.mu.Lock()
	if flow.ctx.Err() != nil || d.flow != flow || d.status.State != "registered" || d.offline[query.TargetID] {
		d.mu.Unlock()
		d.respond(req, tx, 480, "Temporarily Unavailable")
		return
	}
	if d.broadcast != nil {
		d.mu.Unlock()
		d.respond(req, tx, 486, "Busy Here")
		return
	}
	select {
	case d.fleet.mediaSlots <- struct{}{}:
	default:
		d.mu.Unlock()
		d.respond(req, tx, 503, "Service Unavailable")
		return
	}
	ctx, cancel := context.WithCancel(flow.ctx)
	b := &broadcastSession{flow: flow, ctx: ctx, cancel: cancel}
	d.broadcast = b
	d.voice = BroadcastStatus{OutputID: d.outputID, State: "negotiating", SourceID: query.SourceID}
	d.workers.Add(1)
	d.mu.Unlock()

	defer d.workers.Done()
	defer func() { <-d.fleet.mediaSlots }()
	defer cancel()
	d.respond(req, tx, 200, "OK")
	err := d.runBroadcast(b, query)
	d.mu.Lock()
	d.broadcast = nil
	d.voice.State = "ended"
	if err != nil && ctx.Err() == nil {
		d.voice.State, d.voice.Error = "failed", err.Error()
	}
	d.mu.Unlock()
}

func (d *device) runBroadcast(b *broadcastSession, query simulatorQuery) error {
	flow := b.flow
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: flow.local.Addr().AsSlice()})
	if err != nil {
		_ = d.sendXML(flow, queryResponse("Broadcast", query.SN, d.outputID, "<Result>ERROR</Result>"))
		return fmt.Errorf("绑定广播 RTP 端口失败：%w", err)
	}
	defer conn.Close()

	if err := d.sendXML(flow, queryResponse("Broadcast", query.SN, d.outputID, "<Result>OK</Result>")); err != nil {
		return fmt.Errorf("广播应答未获确认：%w", err)
	}

	ip := flow.local.Addr()
	port := conn.LocalAddr().(*net.UDPAddr).Port
	ssrc := fmt.Sprintf("%010d", rand.Uint32N(1_000_000_000))
	offer := fmt.Appendf(nil, "v=0\r\no=%s 0 0 IN IP4 %s\r\ns=Broadcast\r\nc=IN IP4 %s\r\nt=0 0\r\n"+
		"m=audio %d RTP/AVP 8 0\r\na=recvonly\r\na=rtpmap:8 PCMA/8000\r\na=rtpmap:0 PCMU/8000\r\ny=%s\r\nf=v/////a/1/8/1\r\n",
		d.outputID, ip, ip, port, ssrc)
	invite := d.request(sip.INVITE, offer)
	invite.From().Params.Add("tag", sip.GenerateTagN(16))
	invite.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	invite.AppendHeader(sip.NewHeader("Subject", d.outputID+":"+ssrc+","+query.SourceID+":0"))
	ua := sipgo.DialogUA{Client: flow.client, ContactHDR: *d.contact(flow), RewriteContact: true}
	ctx, cancel := context.WithCancelCause(context.Background())
	stop := context.AfterFunc(b.ctx, func() { cancel(sipgo.WaitAnswerForceCancelErr) })
	timer := time.AfterFunc(6*time.Second, func() { cancel(sipgo.WaitAnswerForceCancelErr) })
	dialog, err := ua.WriteInvite(ctx, invite)
	if err == nil {
		defer dialog.Close()
		err = dialog.WaitAnswer(ctx, sipgo.AnswerOptions{Username: d.cfg.Username, Password: d.cfg.Password})
	}
	timer.Stop()
	stop()
	cancel(nil)
	if err != nil {
		return fmt.Errorf("广播 INVITE 未建立：%w", err)
	}
	if dialog.InviteResponse.Contact() == nil {
		return fmt.Errorf("广播 INVITE 应答缺少 Contact")
	}
	d.mu.Lock()
	b.dialog, b.dialogID = dialog, dialog.ID
	d.voice.CallID, d.voice.Local = invite.CallID().Value(), conn.LocalAddr().String()
	d.mu.Unlock()

	if err := dialog.Ack(b.ctx); err != nil {
		return fmt.Errorf("广播 ACK 发送失败：%w", err)
	}
	defer func() {
		if dialog.Context().Err() == nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = dialog.Bye(ctx)
		}
	}()

	media, err := broadcastAnswer(dialog.InviteResponse.Body())
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.voice.State, d.voice.Remote = "receiving", media.remote.String()
	d.mu.Unlock()

	readCtx, stopRead := context.WithCancel(b.ctx)
	defer stopRead()
	stopDialog := context.AfterFunc(dialog.Context(), stopRead)
	defer stopDialog()
	closeOnCancel := context.AfterFunc(readCtx, func() { _ = conn.Close() })
	defer closeOnCancel()
	return d.receiveBroadcast(readCtx, conn, media)
}

func (d *device) readBroadcastBye(req *sip.Request, tx sip.ServerTransaction) bool {
	id, err := sip.DialogIDFromRequestUAC(req)
	if err != nil || req.CSeq() == nil {
		return false
	}
	d.mu.Lock()
	b := d.broadcast
	var dialog *sipgo.DialogClientSession
	if b != nil && id == b.dialogID {
		dialog = b.dialog
	}
	d.mu.Unlock()
	if dialog == nil {
		return false
	}
	_ = dialog.ReadBye(req, tx)
	return true
}

type broadcastMedia struct {
	remote netip.AddrPort
	pcma   bool
	pcmu   bool
}

func broadcastAnswer(body []byte) (broadcastMedia, error) {
	var answer broadcastMedia
	var clean strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		// y= and f= are GB extensions beyond RFC 4566.
		if !strings.HasPrefix(line, "y=") && !strings.HasPrefix(line, "f=") {
			clean.WriteString(line + "\n")
		}
	}
	var desc sdp.SessionDescription
	if err := desc.Unmarshal([]byte(clean.String())); err != nil || len(desc.MediaDescriptions) != 1 {
		return answer, fmt.Errorf("广播应答缺少有效的音频 SDP")
	}
	media := desc.MediaDescriptions[0]
	name := media.MediaName
	info := media.ConnectionInformation
	if info == nil {
		info = desc.ConnectionInformation
	}
	if name.Media != "audio" || strings.Join(name.Protos, "/") != "RTP/AVP" || name.Port.Value < 1 || name.Port.Value > 65535 ||
		info == nil || info.Address == nil || info.NetworkType != "IN" || info.AddressType != "IP4" {
		return answer, fmt.Errorf("广播应答需要 IPv4 UDP RTP 音频")
	}
	direction := func(attributes []sdp.Attribute) string {
		for _, attr := range attributes {
			switch attr.Key {
			case "sendonly", "sendrecv", "inactive", "recvonly":
				return attr.Key
			}
		}
		return ""
	}
	flow := direction(media.Attributes)
	if flow == "" {
		flow = direction(desc.Attributes)
	}
	if flow == "recvonly" || flow == "inactive" {
		return answer, fmt.Errorf("广播平台未协商发送方向")
	}

	ip, err := netip.ParseAddr(info.Address.Address)
	if err != nil || !ip.Is4() || !unicastIP(ip.AsSlice()) {
		return answer, fmt.Errorf("广播媒体地址需为单播 IPv4 地址")
	}
	answer.remote = netip.AddrPortFrom(ip, uint16(name.Port.Value))
	for _, format := range name.Formats {
		switch format {
		case "8":
			answer.pcma = true
		case "0":
			answer.pcmu = true
		}
	}
	if !answer.pcma && !answer.pcmu {
		return answer, fmt.Errorf("广播应答未协商 G.711 A-law 或 μ-law")
	}
	return answer, nil
}

func (d *device) receiveBroadcast(ctx context.Context, conn *net.UDPConn, media broadcastMedia) error {
	const idle = 30 * time.Second
	_ = conn.SetReadDeadline(time.Now().Add(idle))
	buf := make([]byte, 2048)
	var locked bool
	var ssrc uint32
	for ctx.Err() == nil {
		n, addr, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("广播 RTP 接收结束：%w", err)
		}
		if addr.Addr().Unmap() != media.remote.Addr() {
			continue
		}
		var packet rtp.Packet
		if packet.Unmarshal(buf[:n]) != nil || packet.Version != 2 || len(packet.Payload) == 0 ||
			locked && ssrc != packet.SSRC {
			continue
		}
		decode, codec := g711.DecodeUlawFrame, "PCMU"
		switch {
		case media.pcma && packet.PayloadType == 8:
			decode, codec = g711.DecodeAlawFrame, "PCMA"
		case media.pcmu && packet.PayloadType == 0:
		default:
			continue
		}
		peak := 0
		for _, code := range packet.Payload {
			pcm := int(decode(code))
			peak = max(peak, pcm, -pcm)
		}
		d.mu.Lock()
		d.voice.Codec = codec
		d.voice.Packets++
		d.voice.Peak = max(d.voice.Peak, peak)
		d.mu.Unlock()
		if !locked {
			d.log("system", "收到首个语音广播 RTP 包", "")
			ssrc, locked = packet.SSRC, true
		}
		_ = conn.SetReadDeadline(time.Now().Add(idle))
	}
	return nil
}

func talkbackTone() []byte {
	// 100 ms at 8 kHz is exactly 44 periods of the 440 Hz test tone.
	tone := make([]byte, 800)
	for i := range tone {
		tone[i] = g711.EncodeAlawFrame(int16(4800 * math.Sin(2*math.Pi*440*float64(i)/8000)))
	}
	return tone
}
