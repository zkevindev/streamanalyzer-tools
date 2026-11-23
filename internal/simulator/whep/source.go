package whep

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
)

const route = "/api/simulators/whep"
const maxSessions = 4

var (
	ErrMode         = errors.New("WHEP 模拟场景无效")
	ErrActive       = errors.New("WHEP 模拟源已经启动")
	ErrClosed       = errors.New("WHEP 模拟源已关闭")
	ErrMissing      = errors.New("WHEP 流或会话不存在")
	ErrLimit        = errors.New("WHEP 模拟源的播放会话已满")
	ErrOffer        = errors.New("SDP 需包含一条可接收 H.264 的视频轨")
	ErrNegotiation  = errors.New("本机 WHEP 协商失败")
	ErrUnauthorized = errors.New("WHEP 信令鉴权失败（模拟 401）")
	ErrSDPReject    = errors.New("WHEP 拒绝当前 SDP Offer（模拟 406）")
)

type Status struct {
	Active   bool   `json:"active"`
	Mode     string `json:"mode,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Sessions int    `json:"sessions"`
}

type Manager struct {
	mu       sync.Mutex
	id       string
	mode     string
	sessions map[string]*session
	closed   bool
}

func (m *Manager) Start(mode string) (Status, error) {
	switch mode {
	case "normal", "ice_timeout", "no_media", "media_stop", "rtp_drop", "auth_401", "not_found_404", "sdp_reject_406":
	default:
		return Status{}, ErrMode
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Status{}, ErrClosed
	}
	if m.id != "" {
		return Status{}, ErrActive
	}
	m.id, m.mode = uuid.NewString(), mode
	m.sessions = make(map[string]*session)
	return m.snapshot(), nil
}

func (m *Manager) snapshot() Status {
	status := Status{Active: m.id != "", Mode: m.mode, Sessions: len(m.sessions)}
	if status.Active {
		status.Endpoint = route + "/stream/" + m.id
	}
	return status
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshot()
}

func (m *Manager) Offer(ctx context.Context, sourceID, offer string) (answer, resource string, err error) {
	m.mu.Lock()
	if m.id == "" || m.id != sourceID {
		m.mu.Unlock()
		return "", "", ErrMissing
	}
	switch m.mode {
	case "auth_401":
		m.mu.Unlock()
		return "", "", ErrUnauthorized
	case "not_found_404":
		m.mu.Unlock()
		return "", "", ErrMissing
	case "sdp_reject_406":
		m.mu.Unlock()
		return "", "", ErrSDPReject
	}
	if len(m.sessions) >= maxSessions {
		m.mu.Unlock()
		return "", "", ErrLimit
	}
	id := uuid.NewString()
	peer := newSession(m.mode)
	m.sessions[id] = peer
	m.mu.Unlock()
	release := func() {
		m.mu.Lock()
		if m.sessions[id] == peer {
			delete(m.sessions, id)
		}
		m.mu.Unlock()
		close(peer.done)
	}

	answer, err = peer.negotiate(ctx, offer)
	if err != nil {
		peer.close()
		release()
		return "", "", err
	}
	go func() {
		defer release()
		defer peer.close()
		peer.run()
	}()
	return answer, route + "/sessions/" + id, nil
}

func (m *Manager) Session(id string) (SessionStatus, bool) {
	m.mu.Lock()
	peer := m.sessions[id]
	m.mu.Unlock()
	if peer == nil {
		return SessionStatus{}, false
	}
	return peer.snapshot(), true
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	peer := m.sessions[id]
	if peer != nil {
		peer.cancel()
	}
	m.mu.Unlock()
	if peer == nil {
		return ErrMissing
	}
	<-peer.done
	return nil
}

func (m *Manager) stop(closeManager bool) Status {
	m.mu.Lock()
	m.closed = m.closed || closeManager
	m.id, m.mode = "", ""
	sessions := m.sessions
	m.sessions = nil
	m.mu.Unlock()
	for _, peer := range sessions {
		peer.cancel()
	}
	for _, peer := range sessions {
		<-peer.done
	}
	return Status{}
}

func (m *Manager) Stop() Status { return m.stop(false) }
func (m *Manager) Close()       { m.stop(true) }
