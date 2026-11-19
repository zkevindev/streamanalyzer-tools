package sipphone

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

const prackMethod sip.RequestMethod = "PRACK"

const prackSampleTimeout = 4 * time.Second

type PRACKStatus struct {
	State         string    `json:"state"`
	RSeq          uint32    `json:"rseq"`
	ProvisionalAt time.Time `json:"provisional_at,omitzero"`
	RequestAt     time.Time `json:"request_at,omitzero"`
	ResponseAt    time.Time `json:"response_at,omitzero"`
	FinalCode     int       `json:"final_code,omitempty"`
}

type inboundPRACK struct {
	rseq      uint32
	inviteSeq uint32
	done      chan struct{}
	cseq      uint32
}

type prackDialogTransport struct {
	client *sipgo.Client
	invite atomic.Uint32
}

// sipgo advances the dialog CSeq for PRACK. ACK and CANCEL still need the
// original INVITE number, including when sipgo retransmits an ACK.
func (t *prackDialogTransport) Request(ctx context.Context, req *sip.Request) (sip.ClientTransaction, error) {
	if req.Method == sip.INVITE {
		t.invite.Store(req.CSeq().SeqNo)
	}
	if (req.Method == sip.ACK || req.Method == sip.CANCEL) && req.CSeq().SeqNo != t.invite.Load() {
		req = req.Clone()
		req.CSeq().SeqNo = t.invite.Load()
	}
	if req.Method == sip.ACK {
		return nil, t.client.TransportLayer().WriteMsg(req)
	}
	return t.client.TransactionLayer().Request(ctx, req)
}

func headerOption(message sip.Message, header, option string) bool {
	for _, value := range message.GetHeaders(header) {
		for token := range strings.SplitSeq(value.Value(), ",") {
			if strings.EqualFold(strings.TrimSpace(token), option) {
				return true
			}
		}
	}
	return false
}

func (a *agent) setPRACK(c *phoneCall, status PRACKStatus) {
	a.mu.Lock()
	c.status.PRACK = &status
	a.mu.Unlock()
}

func (a *agent) sendPRACK(ctx context.Context, c *phoneCall, dialog *sipgo.DialogClientSession, res *sip.Response, seen map[string]uint32) error {
	status := PRACKStatus{State: "invalid", ProvisionalAt: time.Now()}
	header := res.GetHeader("RSeq")
	id, idErr := sip.DialogIDFromResponse(res)
	if header == nil || idErr != nil || res.Contact() == nil || res.CSeq() == nil {
		a.setPRACK(c, status)
		return errors.New("可靠临时应答缺少有效的 RSeq、对话标识或 Contact")
	}
	seq, err := strconv.ParseUint(strings.TrimSpace(header.Value()), 10, 31)
	if err != nil || seq == 0 || res.CSeq().SeqNo != dialog.InviteRequest.CSeq().SeqNo {
		a.setPRACK(c, status)
		return errors.New("可靠临时应答的 RSeq 或 CSeq 无效")
	}
	status.RSeq = uint32(seq)
	if prior := seen[id]; prior != 0 && status.RSeq != prior+1 {
		// SIP transactions retransmit PRACK themselves. Repeated or out-of-order
		// provisional responses must not create a second PRACK transaction.
		return nil
	}
	seen[id] = status.RSeq
	req := sip.NewRequest(prackMethod, *res.Contact().Address.Clone())
	req.SetDestination(a.registrar.String())
	req.AppendHeader(sip.NewHeader("RAck", fmt.Sprintf("%d %d INVITE", seq, res.CSeq().SeqNo)))
	status.State, status.RequestAt = "waiting", time.Now()
	a.setPRACK(c, status)
	wait, cancel := context.WithTimeout(ctx, prackSampleTimeout)
	defer cancel()
	answer, err := dialog.Do(wait, req)
	if err != nil {
		status.State = "failed"
		if errors.Is(wait.Err(), context.DeadlineExceeded) {
			status.State = "timeout"
		} else if ctx.Err() != nil {
			status.State = "canceled"
		}
		a.setPRACK(c, status)
		return fmt.Errorf("SIP PRACK 未收到成功应答（%s）", status.State)
	}
	status.ResponseAt, status.FinalCode = time.Now(), answer.StatusCode
	status.State = "confirmed"
	if !answer.IsSuccess() {
		status.State = "rejected"
	}
	a.setPRACK(c, status)
	if !answer.IsSuccess() {
		return fmt.Errorf("SIP PRACK 被拒绝（SIP %d）", answer.StatusCode)
	}
	return nil
}

func (a *agent) awaitPRACK(c *phoneCall, tx sip.ServerTransaction, dialog *sipgo.DialogServerSession, body []byte, extra []sip.Header) (bool, error) {
	pending := &inboundPRACK{rseq: rand.Uint32N(1<<30) + 1, done: make(chan struct{}), inviteSeq: dialog.InviteRequest.CSeq().SeqNo}
	// Publish the pending sub-transaction before the first 183 can reach the peer.
	a.mu.Lock()
	c.pendingPRACK = pending
	c.status.PRACK = &PRACKStatus{State: "waiting", RSeq: pending.rseq, ProvisionalAt: time.Now()}
	a.mu.Unlock()
	headers := append(extra, sip.NewHeader("Require", "100rel"), sip.NewHeader("RSeq", strconv.FormatUint(uint64(pending.rseq), 10)))
	send := func() error { return dialog.Respond(183, "Session Progress", body, headers...) }
	if err := send(); err != nil {
		return false, errors.New("无法发送可靠 SIP 183")
	}
	delay := sip.T1
	retry := time.NewTimer(delay)
	defer retry.Stop()
	limit := time.NewTimer(prackSampleTimeout)
	defer limit.Stop()
	for {
		select {
		case <-pending.done:
			return true, nil
		case <-retry.C:
			if err := send(); err != nil {
				return false, errors.New("无法重发可靠 SIP 183")
			}
			delay *= 2
			retry.Reset(delay)
		case <-c.hangup:
			respond(dialog.InviteRequest, tx, 603, "Decline")
			return false, nil
		case <-a.ctx.Done():
			respond(dialog.InviteRequest, tx, 480, "Temporarily Unavailable")
			return false, nil
		case <-dialog.Context().Done():
			return false, nil
		case <-tx.Done():
			return false, nil
		case <-limit.C:
			a.mu.Lock()
			if c.status.PRACK.State == "confirmed" {
				a.mu.Unlock()
				return true, nil
			}
			c.pendingPRACK = nil
			status := *c.status.PRACK
			status.State = "timeout"
			c.status.PRACK = &status
			a.mu.Unlock()
			respond(dialog.InviteRequest, tx, 504, "Server Time-out")
			return false, errors.New("等待 SIP PRACK 超时")
		}
	}
}

func (a *agent) prack(req *sip.Request, tx sip.ServerTransaction) {
	rack := req.GetHeader("RAck")
	if rack == nil || req.CSeq() == nil {
		respond(req, tx, 400, "Bad Request")
		return
	}
	fields := strings.Fields(rack.Value())
	if len(fields) != 3 || fields[2] != "INVITE" {
		respond(req, tx, 400, "Bad Request")
		return
	}
	rseq, rerr := strconv.ParseUint(fields[0], 10, 31)
	cseq, cerr := strconv.ParseUint(fields[1], 10, 32)
	if rerr != nil || cerr != nil {
		respond(req, tx, 400, "Bad Request")
		return
	}
	a.mu.Lock()
	c := a.call
	if c == nil || c.callee == nil || c.pendingPRACK == nil || !c.status.Active || !c.matchesDialog(req) ||
		uint64(c.pendingPRACK.inviteSeq) != cseq || uint64(c.pendingPRACK.rseq) != rseq || req.CSeq().SeqNo <= c.pendingPRACK.inviteSeq {
		a.mu.Unlock()
		respond(req, tx, 481, "Call/Transaction Does Not Exist")
		return
	}
	pending := c.pendingPRACK
	if pending.cseq != 0 {
		a.mu.Unlock()
		respond(req, tx, 481, "Call/Transaction Does Not Exist")
		return
	}
	pending.cseq = req.CSeq().SeqNo
	received := time.Now()
	// Respond outside the agent lock because SIP wire logging uses the same lock.
	a.mu.Unlock()
	err := tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	a.mu.Lock()
	defer a.mu.Unlock()
	if c.pendingPRACK != pending {
		return
	}
	status := *c.status.PRACK
	status.RequestAt, status.ResponseAt = received, time.Now()
	status.State, status.FinalCode = "confirmed", 200
	if err != nil {
		status.State, status.FinalCode = "failed", 0
		pending.cseq = 0
	} else {
		close(pending.done)
	}
	c.status.PRACK = &status
}
