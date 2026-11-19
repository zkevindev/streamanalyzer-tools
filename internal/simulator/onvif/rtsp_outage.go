package onvif

import (
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
)

const rtspOutageDelay = 3 * time.Second

type RTSPOutageStatus struct {
	Profile   string    `json:"profile"`
	Begin     time.Time `json:"begin,omitzero"`
	End       time.Time `json:"end,omitzero"`
	FirstPlay time.Time `json:"first_reconnected_play,omitzero"`
	Closed    int       `json:"closed_sessions"`
	Rejected  int       `json:"rejected_requests"`
}

type rtspOutage struct {
	duration time.Duration
	mu       sync.Mutex
	armed    time.Time
	status   RTSPOutageStatus
	sessions map[*gortsplib.ServerSession]*gortsplib.ServerConn
}

func (o *rtspOutage) reject(now time.Time) bool {
	if o.duration == 0 {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.rejectLocked(now)
}

func (o *rtspOutage) rejectLocked(now time.Time) bool {
	if o.status.Begin.IsZero() || !now.Before(o.status.End) {
		return false
	}
	o.status.Rejected++
	return true
}

func (o *rtspOutage) play(now time.Time, session *gortsplib.ServerSession, conn *gortsplib.ServerConn) bool {
	if o.duration == 0 {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.rejectLocked(now) {
		return false
	}
	if o.armed.IsZero() {
		o.armed = now.Add(rtspOutageDelay)
		o.sessions = make(map[*gortsplib.ServerSession]*gortsplib.ServerConn)
	}
	if o.status.Begin.IsZero() {
		o.sessions[session] = conn
	} else if o.status.FirstPlay.IsZero() {
		o.status.FirstPlay = now
	}
	return true
}

func (o *rtspOutage) forget(session *gortsplib.ServerSession) {
	if o.duration != 0 {
		o.mu.Lock()
		delete(o.sessions, session)
		o.mu.Unlock()
	}
}

func (o *rtspOutage) advance(now time.Time) {
	if o.duration == 0 {
		return
	}
	o.mu.Lock()
	if o.armed.IsZero() || now.Before(o.armed) || !o.status.Begin.IsZero() || len(o.sessions) == 0 {
		o.mu.Unlock()
		return
	}
	o.status.Begin, o.status.End, o.status.Closed = now, now.Add(o.duration), len(o.sessions)
	sessions := o.sessions
	o.sessions = nil
	o.mu.Unlock()
	for session, conn := range sessions {
		session.Close()
		conn.Close()
	}
}

func (o *rtspOutage) snapshot(profile string) RTSPOutageStatus {
	o.mu.Lock()
	defer o.mu.Unlock()
	status := o.status
	status.Profile = profile
	return status
}
