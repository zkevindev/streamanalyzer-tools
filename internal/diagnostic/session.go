package diagnostic

import (
	"fmt"
	"time"
)

type SessionGroup struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"`
	Runs      []Run     `json:"runs"`
	Reason    string    `json:"reason,omitempty"`
}

type diagnosticSessionGroup struct {
	id        string
	createdAt time.Time
	runIDs    []string
	status    string
	reason    string
}

func (s *Service) SessionGroup(id string) (SessionGroup, error) {
	s.mu.Lock()
	group := s.sessionGroups[id]
	if group == nil {
		s.mu.Unlock()
		return SessionGroup{}, ErrNotFound
	}
	copy := *group
	copy.runIDs = append([]string(nil), group.runIDs...)
	s.mu.Unlock()
	result := SessionGroup{ID: copy.id, CreatedAt: copy.createdAt, Status: copy.status, Reason: copy.reason, Runs: []Run{}}
	for _, runID := range copy.runIDs {
		run, err := s.getRun(runID)
		if err != nil {
			return SessionGroup{}, err
		}
		run.SessionGroupID = id
		result.Runs = append(result.Runs, run)
	}
	return result, nil
}

func (s *Service) finishSession(req SampleRequest, runID string) {
	if req.SessionGroupID == "" {
		return
	}
	detail, err := s.Detail(runID)
	if err != nil {
		return
	}
	reason := ""
	if detail.Report != nil {
		reason = detail.Report.Manifest.Observation.EndReason
	}
	retry := req.SessionAttempt < req.ReconnectAttempts && detail.Status != "canceled" &&
		(reason == "request_error" || reason == "body_error" || reason == "rtmp_error" || reason == "eof")
	if !retry {
		s.mu.Lock()
		if group := s.sessionGroups[req.SessionGroupID]; group != nil {
			group.status = "done"
			group.reason = reason
		}
		s.mu.Unlock()
		return
	}
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		delay := time.Duration(req.ReconnectDelayMS) * time.Millisecond
		if delay > 0 {
			timer := time.NewTimer(delay)
			<-timer.C
		}
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		next := req
		next.SessionAttempt++
		nextRun, err := s.Start(next)
		if err != nil {
			s.mu.Lock()
			if group := s.sessionGroups[req.SessionGroupID]; group != nil {
				group.status = "done"
				group.reason = "reconnect_start_failed"
			}
			s.mu.Unlock()
			return
		}
		s.mu.Lock()
		if group := s.sessionGroups[req.SessionGroupID]; group != nil {
			group.runIDs = append(group.runIDs, nextRun.ID)
		}
		s.mu.Unlock()
	}()
}

func validateReconnect(req SampleRequest) error {
	if req.ReconnectAttempts < 0 || req.ReconnectAttempts > 3 || req.ReconnectDelayMS < 0 || req.ReconnectDelayMS > 5000 {
		return fmt.Errorf("%w: 重连次数须为 0–3，重连间隔须为 0–5000 ms", ErrInvalid)
	}
	if req.ReconnectAttempts > 0 && req.Protocol != ProtocolHTTPFLV && req.Protocol != ProtocolRTMP {
		return fmt.Errorf("%w: 自动重连目前适用于 HTTP-FLV 与 RTMP/RTMPS", ErrInvalid)
	}
	if req.ReconnectAttempts > 0 && (req.Playback || req.AutoInvestigate) {
		return fmt.Errorf("%w: 重连会话请分别分析各段播放体验，暂不在会话组内自动续接播放器", ErrInvalid)
	}
	return nil
}
