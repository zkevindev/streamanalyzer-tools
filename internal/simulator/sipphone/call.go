package sipphone

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

const ringTimeout = 45 * time.Second

type CallStatus struct {
	SessionTimer    *SessionTimerStatus `json:"session_timer,omitempty"`
	RTPPath         RTPPathStatus       `json:"rtp_path"`
	PRACK           *PRACKStatus        `json:"prack,omitempty"`
	MediaDirection  string              `json:"media_direction,omitempty"`
	PreviousRemote  string              `json:"previous_remote,omitempty"`
	MediaChanges    int                 `json:"media_changes,omitempty"`
	EarlyMedia      bool                `json:"early_media"`
	EarlySend       RTPStats            `json:"early_send_rtp,omitzero"`
	EarlyReceive    RTPStats            `json:"early_receive_rtp,omitzero"`
	AudioReady      bool                `json:"audio_ready"`
	DTMFKeys        string              `json:"dtmf_keys,omitempty"`
	DigitEvents     []DigitEvent        `json:"digit_events,omitempty"`
	Scenario        *ScenarioReport     `json:"scenario,omitempty"`
	AudioFault      string              `json:"audio_fault"`
	SendRTP         RTPStats            `json:"send_rtp"`
	ReceiveRTP      RTPStats            `json:"receive_rtp"`
	Active          bool                `json:"active"`
	State           string              `json:"state"`
	Direction       string              `json:"direction"`
	Peer            string              `json:"peer"`
	ID              string              `json:"call_id,omitempty"`
	Codec           string              `json:"codec,omitempty"`
	MediaLocal      string              `json:"media_local"`
	MediaRemote     string              `json:"media_remote,omitempty"`
	PacketsSent     uint64              `json:"packets_sent"`
	PacketsReceived uint64              `json:"packets_received"`
	Error           string              `json:"error,omitempty"`
}

type phoneCall struct {
	timerWake     chan struct{}
	pendingPRACK  *inboundPRACK
	pendingMedia  *mediaChange
	mediaUpdates  chan *mediaChange
	peerInviteSeq uint32
	sdpVersion    uint32
	dtmfSend      uint16
	status        CallStatus
	audio         *audioSession
	sdpID         uint32
	answer        chan struct{}
	hangup        chan struct{}
	done          chan struct{}
	cancelInvite  context.CancelFunc

	// Dialog identity and pointers are published under agent.mu.
	dialogID string
	caller   *sipgo.DialogClientSession
	callee   *sipgo.DialogServerSession
}

func (a *agent) newCall(direction, peer, state string) (*phoneCall, error) {
	audio, err := openAudio(a.local.Addr(), a.cfg.RTPReturn, a.cfg.SimulateNATPort)
	if err != nil {
		return nil, fmt.Errorf("绑定 RTP 音频端口失败：%w", err)
	}
	return &phoneCall{timerWake: make(chan struct{}, 1), audio: audio, sdpID: rand.Uint32(), mediaUpdates: make(chan *mediaChange, 1), answer: make(chan struct{}), hangup: make(chan struct{}), done: make(chan struct{}),
		status: CallStatus{Active: true, Direction: direction, Peer: peer, State: state, MediaLocal: audio.local.String()}}, nil
}

func (a *agent) finishCall(c *phoneCall, err error) {
	c.audio.close()
	a.mu.Lock()
	defer a.mu.Unlock()
	c.status.Active = false
	c.status.State = "ended"
	if err != nil {
		c.status.State, c.status.Error = "failed", err.Error()
	}
	close(c.done)
}

func (a *agent) callAction(answer bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.call
	if a.ctx.Err() != nil || c == nil || !c.status.Active || c.status.State == "ending" {
		return ErrConflict
	}
	if answer {
		if c.status.Direction != "inbound" || c.status.State != "ringing" {
			return ErrConflict
		}
		c.status.State = "answering"
		close(c.answer)
		return nil
	}
	c.status.State = "ending"
	close(c.hangup)
	if c.cancelInvite != nil {
		c.cancelInvite()
	}
	return nil
}

func (a *agent) target(value string) (sip.Uri, error) {
	value = strings.TrimSpace(value)
	if userPattern.MatchString(value) {
		return sip.Uri{Scheme: "sip", User: value, Host: a.cfg.Domain}, nil
	}
	var uri sip.Uri
	if len(value) <= 384 && !strings.ContainsAny(value, "\r\n\x00") && sip.ParseUri(value, &uri) == nil && uri.Scheme == "sip" &&
		userPattern.MatchString(uri.User) && hostPattern.MatchString(uri.Host) && uri.Port >= 0 && uri.Port <= 65535 &&
		uri.Password == "" && len(uri.Headers) == 0 && len(uri.UriParams) == 0 {
		return uri, nil
	}
	return sip.Uri{}, fmt.Errorf("%w，请输入分机号或 sip:用户@主机", ErrConfig)
}

func (a *agent) dial(target string) error {
	recipient, err := a.target(target)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx.Err() != nil || a.status.State != "registered" || a.call != nil && a.call.status.Active {
		return ErrConflict
	}
	c, err := a.newCall("outbound", recipient.String(), "dialing")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, ringTimeout)
	c.cancelInvite = cancel
	a.call = c
	a.workers.Go(func() {
		defer cancel()
		a.finishCall(c, a.outgoing(ctx, c, recipient))
	})
	return nil
}

func (a *agent) outgoing(ctx context.Context, c *phoneCall, recipient sip.Uri) error {
	req := sip.NewRequest(sip.INVITE, recipient)
	// A preloaded loose route keeps CANCEL and non-2xx ACK on the same
	// next hop as INVITE even when the logical SIP domain is different.
	proxy := sip.Uri{Scheme: "sip", Host: a.registrar.Addr().String(), Port: int(a.registrar.Port())}
	proxy.UriParams.Add("lr", "")
	req.AppendHeader(&sip.RouteHeader{Address: proxy})
	req.SetTransport("UDP")
	req.SetBody(makeOffer(c.audio.advertised, c.sdpID))
	// Keep the SIP address of record in From; Contact contains the local socket.
	from := &sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: a.cfg.User, Host: a.cfg.Domain}}
	from.Params.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: recipient})
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	if a.cfg.SessionTimer != "" {
		req.AppendHeader(sip.NewHeader("Supported", "100rel, timer"))
		req.AppendHeader(sip.NewHeader("Min-SE", fmt.Sprint(a.cfg.SessionSeconds)))
		req.AppendHeader((sessionOffer{seconds: a.cfg.SessionSeconds, role: "uas"}).header())
	} else {
		req.AppendHeader(sip.NewHeader("Supported", "100rel"))
	}

	client, err := sipgo.NewClient(a.client.UserAgent, sipgo.WithClientAddr(a.local.String()), sipgo.WithClientConnectionAddr(a.local.String()), sipgo.WithClientNAT())
	if err != nil {
		return errors.New("无法初始化 SIP 呼出客户端")
	}
	client.TxRequester = &prackDialogTransport{client: a.client}
	ua := a.dialogs
	ua.Client = client
	dialog, err := ua.WriteInvite(ctx, req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		return errors.New("无法发送 SIP INVITE")
	}
	defer dialog.Close()
	a.mu.Lock()
	c.status.ID = req.CallID().Value()
	a.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	seenReliable := make(map[string]uint32)
	var prackErr error
	var earlyMedia audioParameters
	var stopEarly func()
	err = dialog.WaitAnswer(ctx, sipgo.AnswerOptions{Username: a.cfg.Username, Password: a.cfg.Password,
		OnResponse: func(res *sip.Response) error {

			if res.StatusCode == 180 || res.StatusCode == 183 {
				var params audioParameters
				if res.StatusCode == 183 && stopEarly == nil && len(res.Body()) > 0 {
					_, _, params, _ = negotiateAudio(res.Body(), false)
				}
				a.mu.Lock()
				if c.status.State == "dialing" {
					c.status.State = "ringing"
				}
				if params.remote.IsValid() && c.status.State == "ringing" {
					earlyMedia = params
					c.status.EarlyMedia = true
					if params.recv {
						params.send = false
						stopEarly = c.audio.run(ctx, params, true)
					}
				}
				a.mu.Unlock()
			}
			if prackErr == nil && res.IsProvisional() && res.StatusCode != 100 && headerOption(res, "Require", "100rel") {
				prackErr = a.sendPRACK(ctx, c, dialog, res, seenReliable)
				if prackErr != nil {
					cancel()
				}
			}
			return nil
		},
	})
	if stopEarly != nil {
		stopEarly()
	}
	if err != nil && (dialog.InviteResponse == nil || !dialog.InviteResponse.IsSuccess()) {
		if prackErr != nil {
			return prackErr
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("呼出等待接听超时")
		}
		var response *sipgo.ErrDialogResponse
		if errors.As(err, &response) {
			return fmt.Errorf("呼叫被拒绝（SIP %d）", response.Res.StatusCode)
		}
		return errors.New("SIP INVITE 未完成")
	}

	// When CANCEL crosses a successful INVITE, acknowledge the final response
	// and end the resulting dialog with BYE.
	id, err := sip.DialogIDFromResponse(dialog.InviteResponse)
	if err != nil || dialog.InviteResponse.Contact() == nil {
		return errors.New("SIP INVITE 应答缺少有效的对话标识或 Contact")
	}
	a.mu.Lock()
	c.dialogID, c.caller, c.cancelInvite = id, dialog, nil
	if c.status.State != "ending" {
		c.status.State = "answering"
	}
	a.mu.Unlock()
	if err := dialog.Ack(context.Background()); err != nil {
		return errors.New("无法发送 SIP ACK")
	}
	if prackErr != nil {
		_ = a.sendBye(dialog.Bye)
		return prackErr
	}
	media := earlyMedia
	if !media.remote.IsValid() || len(dialog.InviteResponse.Body()) != 0 {
		_, _, media, err = negotiateAudio(dialog.InviteResponse.Body(), false)
		if err != nil {
			_ = a.sendBye(dialog.Bye)
			return err
		}
	}
	var timer sessionOffer
	if h := dialog.InviteResponse.GetHeader("Session-Expires"); h != nil && a.cfg.SessionTimer != "" {
		timer, _ = parseSessionOffer(h.Value())
		if timer.seconds < a.cfg.SessionSeconds {
			_ = a.sendBye(dialog.Bye)
			return errors.New("SIP Session-Expires 应答无效或短于 Min-SE")
		}
	}
	return a.connected(c, dialog.Context(), media, dialog.Bye, timer)
}

func (a *agent) routes(server *sipgo.Server) {
	server.OnInvite(a.incoming)
	server.OnAck(a.ack)
	server.OnBye(a.bye)
	server.OnRequest(prackMethod, a.prack)
	server.OnRequest(sip.UPDATE, a.updateSession)
	server.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		res.AppendHeader(sip.NewHeader("Allow", "OPTIONS, INVITE, ACK, CANCEL, BYE, PRACK, UPDATE"))
		_ = tx.Respond(res)
	})
	server.OnNoRoute(func(req *sip.Request, tx sip.ServerTransaction) {
		if req.Method == sip.CANCEL {
			respond(req, tx, 481, "Call/Transaction Does Not Exist")
			return
		}
		respond(req, tx, 501, "Not Implemented")
	})
}

func respond(req *sip.Request, tx sip.ServerTransaction, code int, reason string) {
	_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
}

func (a *agent) incoming(req *sip.Request, tx sip.ServerTransaction) {
	if req.From() == nil || req.To() == nil || req.CSeq() == nil || req.CallID() == nil {
		respond(req, tx, 400, "Bad Request")
		return
	}
	if req.Recipient.User != a.cfg.User && req.To().Address.User != a.cfg.User {
		respond(req, tx, 404, "Not Found")
		return
	}
	if tag, _ := req.To().Params.Get("tag"); tag != "" {
		a.reinvite(req, tx)
		return
	}
	timer, ok := a.sessionAnswer(req, tx)
	if !ok {
		return
	}
	offer, selected, media, err := negotiateAudio(req.Body(), true)
	if err != nil {
		respond(req, tx, 488, "Not Acceptable Here")
		return
	}
	// Allow an INVITE that arrives in the same network burst as REGISTER 200.
	select {
	case <-a.registered:
	case <-a.ctx.Done():
	case <-tx.Done():
	case <-time.After(300 * time.Millisecond):
	}
	dialog, err := a.dialogs.ReadInvite(req, tx)
	if err != nil {
		respond(req, tx, 400, "Bad Request")
		return
	}
	defer dialog.Close()
	a.mu.Lock()
	if a.ctx.Err() != nil || a.status.State != "registered" && a.status.State != "retrying" {
		a.mu.Unlock()
		respond(req, tx, 480, "Temporarily Unavailable")
		return
	}
	if a.call != nil && a.call.status.Active {
		a.mu.Unlock()
		respond(req, tx, 486, "Busy Here")
		return
	}
	// Keep the peer identity readable without copying password or URI parameters.
	peer := req.From().Address.User + "@" + req.From().Address.Host
	c, err := a.newCall("inbound", peer, "ringing")
	if err != nil {
		a.mu.Unlock()
		respond(req, tx, 500, "Server Internal Error")
		return
	}
	c.dialogID, c.callee, c.status.ID = dialog.ID, dialog, req.CallID().Value()
	c.peerInviteSeq = req.CSeq().SeqNo
	c.status.Codec, c.status.MediaRemote = media.codec.name, media.remote.String()
	a.call = c
	a.workers.Add(1)
	a.mu.Unlock()
	defer a.workers.Done()

	answer := makeAnswer(c.audio.advertised, c.sdpID, offer, selected, media)
	a.finishCall(c, a.ring(c, tx, dialog, answer, media, timer))
}

func (a *agent) ring(c *phoneCall, tx sip.ServerTransaction, dialog *sipgo.DialogServerSession, answer []byte, media audioParameters, session sessionOffer) error {
	if err := dialog.Respond(180, "Ringing", nil); err != nil {
		if dialog.Context().Err() == nil {
			return errors.New("无法发送 SIP 振铃响应")
		}
		return nil
	}
	var stopEarly func()
	early := a.cfg.EarlyMedia && media.send
	reliable := headerOption(dialog.InviteRequest, "Require", "100rel") ||
		a.cfg.ReliableEarly && headerOption(dialog.InviteRequest, "Supported", "100rel")
	var earlyBody []byte
	var earlyHeaders []sip.Header
	if early {
		earlyBody = answer
		earlyHeaders = []sip.Header{sip.NewHeader("Content-Type", "application/sdp")}
	}
	if reliable {
		confirmed, err := a.awaitPRACK(c, tx, dialog, earlyBody, earlyHeaders)
		if err != nil || !confirmed {
			return err
		}
	} else if early {
		if err := dialog.Respond(183, "Session Progress", earlyBody, earlyHeaders...); err != nil {
			return errors.New("无法发送早期媒体响应")
		}
	}
	if early {
		a.mu.Lock()
		c.status.EarlyMedia = true
		a.mu.Unlock()
		stopEarly = c.audio.run(a.ctx, media, true)
		defer stopEarly()
	}
	timer := time.NewTimer(ringTimeout)
	defer timer.Stop()
	select {
	case <-dialog.Context().Done():
		return nil
	case <-tx.Done():
		return nil
	case <-c.hangup:
		respond(dialog.InviteRequest, tx, 603, "Decline")
		return nil
	case <-a.ctx.Done():
		respond(dialog.InviteRequest, tx, 480, "Temporarily Unavailable")
		return nil
	case <-timer.C:
		respond(dialog.InviteRequest, tx, 480, "Temporarily Unavailable")
		return errors.New("呼入等待接听超时")
	case <-c.answer:
	}
	if stopEarly != nil {
		stopEarly()
	}
	if dialog.Context().Err() != nil {
		return nil
	}

	// Handle ACKs with either a separate branch or the same branch as INVITE.
	// Bound the wait even if the answering transaction gets no further packets.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		timeout := time.NewTimer(32 * time.Second)
		defer timeout.Stop()
		for {
			select {
			case ack := <-tx.Acks():
				if ack != nil {
					a.ack(ack, tx)
				}
			case <-stop:
				return
			case <-tx.Done():
				return
			case <-a.ctx.Done():
				tx.Terminate()
				return
			case <-timeout.C:
				tx.Terminate()
				return
			}
		}
	}()
	var err error
	if session.seconds > 0 {
		err = dialog.Respond(200, "OK", answer, sip.NewHeader("Content-Type", "application/sdp"), session.header(), sip.NewHeader("Require", "timer"))
	} else {
		err = dialog.RespondSDP(answer)
	}
	close(stop)
	<-done
	if err != nil || dialog.LoadState() != sip.DialogStateConfirmed {
		if dialog.Context().Err() != nil || a.ctx.Err() != nil {
			return nil
		}
		return errors.New("接听未完成，未收到有效 SIP ACK")
	}
	return a.connected(c, dialog.Context(), media, dialog.Bye, session)
}

func (a *agent) ack(req *sip.Request, tx sip.ServerTransaction) {
	if req.CSeq() == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.call
	if c == nil || !c.status.Active || !c.matchesDialog(req) {
		return
	}
	if change := c.pendingMedia; change != nil && change.seq == req.CSeq().SeqNo {
		select {
		case <-change.acked:
		default:
			c.mediaUpdates <- change
			close(change.acked)
		}
		return
	}
	if c.callee != nil && c.callee.LoadState() == sip.DialogStateEstablished {
		_ = c.callee.ReadAck(req, tx)
	}
}

func (a *agent) bye(req *sip.Request, tx sip.ServerTransaction) {
	if req.CSeq() == nil {
		respond(req, tx, 400, "Bad Request")
		return
	}
	a.mu.Lock()
	c := a.call
	var handle func(*sip.Request, sip.ServerTransaction) error
	if c != nil && c.status.Active {
		if c.callee != nil && c.callee.LoadState() >= sip.DialogStateEstablished {
			id, _ := sip.DialogIDFromRequestUAS(req)
			if id == c.dialogID {
				handle = c.callee.ReadBye
			}
		}
		if c.caller != nil {
			id, _ := sip.DialogIDFromRequestUAC(req)
			if id == c.dialogID {
				handle = c.caller.ReadBye
			}
		}
	}
	a.mu.Unlock()
	if handle == nil {
		respond(req, tx, 481, "Call/Transaction Does Not Exist")
		return
	}
	if err := handle(req, tx); err != nil {
		respond(req, tx, 500, "Server Internal Error")
	}
}

func (a *agent) connected(c *phoneCall, dialog context.Context, media audioParameters, bye func(context.Context) error, session sessionOffer) error {
	a.mu.Lock()
	c.status.Codec, c.status.MediaRemote = media.codec.name, media.remote.String()
	if c.status.State == "ending" {
		a.mu.Unlock()
		if dialog.Err() != nil {
			return nil
		}
		return a.sendBye(bye)
	}
	c.status.State = "incall"
	c.sdpVersion = c.sdpID
	a.setCallMedia(c, media)
	if session.seconds > 0 {
		c.recordSessionRefresh(session, time.Now(), true)
	}
	preset := a.cfg.Preset != ""
	if preset {
		c.status.Scenario = &ScenarioReport{State: "waiting"}
	}
	a.mu.Unlock()

	stopAudio := c.audio.run(a.ctx, media, false)
	presetCtx, cancelPreset := context.WithCancel(a.ctx)
	defer cancelPreset()
	var presetWorker sync.WaitGroup
	if preset {
		presetWorker.Go(func() { a.runPreset(presetCtx, c, media) })
	}
	defer func() { stopAudio() }()
	stopPreset := func() {
		cancelPreset()
		presetWorker.Wait()
	}
	defer stopPreset()
	var timer *time.Timer
	var timerC <-chan time.Time
	var attempted time.Time
	if session.seconds > 0 {
		timer = time.NewTimer(time.Hour)
		defer timer.Stop()
	}
	for {
		if timer != nil {
			a.mu.Lock()
			status := *c.status.SessionTimer
			a.mu.Unlock()
			when := status.Deadline
			if status.Refresher == "local" && attempted != status.Deadline {
				when = when.Add(-time.Duration(status.Interval) * time.Second / 2)
			}
			timer.Reset(time.Until(when))
			timerC = timer.C
		}
		select {
		case <-c.timerWake:
			continue
		case <-timerC:
			a.mu.Lock()
			status := c.status.SessionTimer
			if !time.Now().Before(status.Deadline) {
				status.Expired = true
				c.status.State = "ending"
				a.mu.Unlock()
				stopAudio()
				a.mu.Lock()
				status.ByeAt = time.Now()
				a.mu.Unlock()
				byeErr := a.sendBye(bye)
				a.mu.Lock()
				status.ByeConfirmed = byeErr == nil
				a.mu.Unlock()
				return errors.New("SIP 会话计时到期")
			}
			if status.Refresher != "local" || attempted == status.Deadline ||
				time.Until(status.Deadline) > time.Duration(status.Interval)*time.Second/2 {
				a.mu.Unlock()
				continue
			}
			attempted = status.Deadline
			offer := sessionOffer{seconds: status.Interval, role: session.role}
			a.mu.Unlock()
			a.refreshSession(c, dialog, offer)
		case <-dialog.Done():
			return nil
		case <-a.ctx.Done():
			stopAudio()
			return a.sendBye(bye)
		case <-c.hangup:
			stopAudio()
			return a.sendBye(bye)
		case change := <-c.mediaUpdates:
			stopPreset()
			stopAudio()
			a.mu.Lock()
			for len(c.audio.digits) > 0 {
				<-c.audio.digits
			}
			c.status.PreviousRemote = c.status.MediaRemote
			c.status.MediaChanges++
			a.setCallMedia(c, change.media)
			c.pendingMedia = nil
			a.mu.Unlock()
			stopAudio = c.audio.run(a.ctx, change.media, false)
		}
	}
}

func (a *agent) sendBye(bye func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := bye(ctx); err != nil {
		return errors.New("未收到 SIP BYE 确认")
	}
	return nil
}
