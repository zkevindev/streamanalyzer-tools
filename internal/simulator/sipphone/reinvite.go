package sipphone

import (
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

type mediaChange struct {
	seq   uint32
	media audioParameters
	acked chan struct{}
}

func (c *phoneCall) matchesDialog(req *sip.Request) bool {
	id, err := sip.DialogIDFromRequestUAC(req)
	if c.callee != nil {
		id, err = sip.DialogIDFromRequestUAS(req)
	}
	return err == nil && c.dialogID != "" && id == c.dialogID
}

func (a *agent) reinvite(req *sip.Request, tx sip.ServerTransaction) {
	if req.Contact() == nil {
		respond(req, tx, 400, "Bad Request")
		return
	}
	offer, selected, media, err := negotiateSDPAudio(req.Body(), true, true)
	a.mu.Lock()
	c := a.call
	if c == nil || c.status.State != "incall" || !c.matchesDialog(req) {
		a.mu.Unlock()
		respond(req, tx, 481, "Call/Transaction Does Not Exist")
		return
	}
	if c.pendingMedia != nil {
		a.mu.Unlock()
		respond(req, tx, 491, "Request Pending")
		return
	}
	seq := req.CSeq().SeqNo
	if seq <= c.peerInviteSeq {
		a.mu.Unlock()
		respond(req, tx, 500, "CSeq Out of Order")
		return
	}
	if err != nil {
		a.mu.Unlock()
		respond(req, tx, 488, "Not Acceptable Here")
		return
	}
	change := &mediaChange{seq: seq, media: media, acked: make(chan struct{})}
	c.pendingMedia, c.peerInviteSeq = change, seq
	// The session ID is stable, while every answer has a new SDP version.
	c.sdpVersion++
	answer := makeAnswerVersion(c.audio.advertised, c.sdpID, c.sdpVersion, offer, selected, media)
	a.mu.Unlock()

	res := sip.NewResponseFromRequest(req, 200, "OK", answer)
	uri := sip.Uri{Scheme: "sip", User: a.cfg.User, Host: a.local.Addr().String(), Port: int(a.local.Port())}
	res.AppendHeader(&sip.ContactHeader{Address: uri})
	res.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	defer func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if c.pendingMedia == change {
			select {
			case <-change.acked:
			default:
				c.pendingMedia = nil
			}
		}
	}()
	if tx.Respond(res.Clone()) != nil {
		return
	}

	interval := 500 * time.Millisecond
	retry := time.NewTimer(interval)
	defer retry.Stop()
	deadline := time.NewTimer(32 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-change.acked:
			return
		case ack := <-tx.Acks():
			if ack != nil {
				a.ack(ack, tx)
			}
		case <-retry.C:
			if tx.Respond(res.Clone()) != nil {
				return
			}
			interval = min(2*interval, 4*time.Second)
			retry.Reset(interval)
		case <-deadline.C:
			return
		case <-c.done:
			return
		case <-a.ctx.Done():
			return
		case <-tx.Done():
			return
		}
	}
}

func (a *agent) setCallMedia(c *phoneCall, media audioParameters) {
	c.status.Codec = media.codec.name
	c.status.MediaRemote = ""
	if media.remote.Addr().IsValid() && !media.remote.Addr().IsUnspecified() {
		c.status.MediaRemote = media.remote.String()
	}
	switch {
	case media.send && media.recv:
		c.status.MediaDirection = "sendrecv"
	case media.send:
		c.status.MediaDirection = "sendonly"
	case media.recv:
		c.status.MediaDirection = "recvonly"
	default:
		c.status.MediaDirection = "inactive"
	}
	c.dtmfSend, c.status.DTMFKeys = 0, ""
	if media.send && media.dtmfPT != 0 {
		c.dtmfSend = media.dtmfEvents
		var keys strings.Builder
		for i := range dtmfDigits {
			if c.dtmfSend&(1<<i) != 0 {
				keys.WriteByte(dtmfDigits[i])
			}
		}
		c.status.DTMFKeys = keys.String()
	}
}
