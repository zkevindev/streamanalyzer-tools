package sipphone

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

type SessionTimerStatus struct {
	ByeAt        time.Time `json:"bye_at,omitzero"`
	ByeConfirmed bool      `json:"bye_confirmed,omitempty"`
	Expired      bool      `json:"expired,omitempty"`
	Interval     int       `json:"interval_seconds"`
	Refresher    string    `json:"refresher"`
	State        string    `json:"state"`
	Deadline     time.Time `json:"deadline"`
	LastRefresh  time.Time `json:"last_refresh,omitzero"`
	SIPCode      int       `json:"sip_code,omitempty"`
}

type sessionOffer struct {
	seconds int
	role    string
}

func parseSessionOffer(value string) (sessionOffer, bool) {
	parts := strings.Split(value, ";")
	seconds, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || seconds < 1 || seconds > 86400 {
		return sessionOffer{}, false
	}
	result := sessionOffer{seconds: seconds, role: "uac"}
	for _, part := range parts[1:] {
		key, value, _ := strings.Cut(part, "=")
		if strings.EqualFold(strings.TrimSpace(key), "refresher") {
			result.role = strings.ToLower(strings.TrimSpace(value))
			if result.role != "uac" && result.role != "uas" {
				return sessionOffer{}, false
			}
		}
	}
	return result, true
}

func (o sessionOffer) header() sip.Header {
	return sip.NewHeader("Session-Expires", fmt.Sprintf("%d;refresher=%s", o.seconds, o.role))
}

func (o sessionOffer) local(c *phoneCall) bool {
	return (o.role == "uac") == (c.status.Direction == "outbound")
}

func (a *agent) sessionAnswer(req *sip.Request, tx sip.ServerTransaction) (sessionOffer, bool) {
	if a.cfg.SessionTimer == "" {
		if headerOption(req, "Require", "timer") {
			res := sip.NewResponseFromRequest(req, 420, "Bad Extension", nil)
			res.AppendHeader(sip.NewHeader("Unsupported", "timer"))
			_ = tx.Respond(res)
			return sessionOffer{}, false
		}
		return sessionOffer{}, true
	}
	h := req.GetHeader("Session-Expires")
	if h == nil {
		return sessionOffer{}, true
	}
	offer, ok := parseSessionOffer(h.Value())
	if !ok {
		respond(req, tx, 400, "Invalid Session-Expires")
		return sessionOffer{}, false
	}
	if offer.seconds < a.cfg.SessionSeconds {
		res := sip.NewResponseFromRequest(req, 422, "Session Interval Too Small", nil)
		res.AppendHeader(sip.NewHeader("Min-SE", strconv.Itoa(a.cfg.SessionSeconds)))
		_ = tx.Respond(res)
		return sessionOffer{}, false
	}
	return offer, true
}

func (c *phoneCall) recordSessionRefresh(offer sessionOffer, now time.Time, initial bool) {
	timer := c.status.SessionTimer
	if timer == nil {
		timer = &SessionTimerStatus{State: "negotiated", Refresher: "peer"}
		if offer.local(c) {
			timer.Refresher = "local"
		}
		c.status.SessionTimer = timer
	}
	timer.Interval = offer.seconds
	timer.Deadline = now.Add(time.Duration(offer.seconds) * time.Second)
	if !initial {
		timer.LastRefresh, timer.State, timer.SIPCode = now, "refreshed", 200
	}
	select {
	case c.timerWake <- struct{}{}:
	default:
	}
}

func (a *agent) updateSession(req *sip.Request, tx sip.ServerTransaction) {
	a.mu.Lock()
	c := a.call
	res, offer := a.prepareSessionUpdate(c, req)
	a.mu.Unlock()
	if res == nil || tx.Respond(res) != nil || offer.seconds == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.call == c && c.status.State == "incall" && c.peerInviteSeq == req.CSeq().SeqNo {
		c.recordSessionRefresh(offer, time.Now(), false)
	}
}

func (a *agent) prepareSessionUpdate(c *phoneCall, req *sip.Request) (*sip.Response, sessionOffer) {
	var empty sessionOffer
	if c == nil || c.status.State != "incall" || !c.matchesDialog(req) {
		return sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil), empty
	}
	if c.status.SessionTimer == nil || c.status.SessionTimer.Refresher != "peer" || len(req.Body()) != 0 {
		return sip.NewResponseFromRequest(req, 488, "Not Acceptable Here", nil), empty
	}
	if req.CSeq() == nil || req.CSeq().SeqNo <= c.peerInviteSeq {
		return sip.NewResponseFromRequest(req, 500, "CSeq Out of Order", nil), empty
	}
	c.peerInviteSeq = req.CSeq().SeqNo
	timer := c.status.SessionTimer
	role := "uac"
	if c.status.Direction == "outbound" {
		role = "uas"
	}
	offer := sessionOffer{seconds: timer.Interval, role: role}
	if h := req.GetHeader("Session-Expires"); h != nil {
		supplied, ok := parseSessionOffer(h.Value())
		if !ok {
			return sip.NewResponseFromRequest(req, 400, "Invalid Session-Expires", nil), empty
		}
		if supplied.seconds < a.cfg.SessionSeconds {
			res := sip.NewResponseFromRequest(req, 422, "Session Interval Too Small", nil)
			res.AppendHeader(sip.NewHeader("Min-SE", strconv.Itoa(a.cfg.SessionSeconds)))
			return res, empty
		}
		offer.seconds = supplied.seconds
	}
	switch a.cfg.SessionTimer {
	case "reject":
		timer.State, timer.SIPCode = "refresh_rejected", 488
		return sip.NewResponseFromRequest(req, 488, "Not Acceptable Here", nil), empty
	case "timeout":
		timer.State, timer.SIPCode = "refresh_unanswered", 0
		return nil, empty
	default:
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		res.AppendHeader((sessionOffer{seconds: offer.seconds, role: "uac"}).header())
		return res, offer
	}
}

// At most one refresh is initiated per negotiated interval. Failed refreshes
// leave the existing expiration deadline in place.
func (a *agent) refreshSession(c *phoneCall, dialog context.Context, offer sessionOffer) {
	a.mu.Lock()
	if a.cfg.SessionTimer != "normal" {
		status := c.status.SessionTimer
		status.State, status.SIPCode = "refresh_withheld", 0
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	recipient := c.remoteContact()
	req := sip.NewRequest(sip.UPDATE, recipient)
	req.AppendHeader((sessionOffer{seconds: offer.seconds, role: "uac"}).header())
	req.AppendHeader(sip.NewHeader("Supported", "timer"))
	ctx, cancel := context.WithTimeout(dialog, time.Second)
	defer cancel()
	var res *sip.Response
	if c.caller != nil {
		res, _ = c.caller.Do(ctx, req)
	} else {
		res, _ = c.callee.Do(ctx, req)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	status := c.status.SessionTimer
	if res == nil {
		status.State, status.SIPCode = "refresh_timeout", 0
		return
	}
	status.SIPCode = res.StatusCode
	if !res.IsSuccess() {
		status.State = "refresh_rejected"
		return
	}
	if h := res.GetHeader("Session-Expires"); h != nil {
		next, ok := parseSessionOffer(h.Value())
		if !ok || next.role != "uac" || next.seconds < a.cfg.SessionSeconds {
			status.State = "refresh_invalid"
			return
		}
		next.role = offer.role
		c.recordSessionRefresh(next, time.Now(), false)
		return
	}
	status.State = "refresh_invalid"
}

func (c *phoneCall) remoteContact() sip.Uri {
	if c.caller != nil {
		return c.caller.InviteResponse.Contact().Address
	}
	if contact := c.callee.InviteRequest.Contact(); contact != nil {
		return contact.Address
	}
	return c.callee.InviteRequest.From().Address
}
