package diagnostic

import (
	"fmt"
	"time"
)

type SessionGroup struct {
	ID             string    `json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	Status         string    `json:"status"`
	Rolling        bool      `json:"rolling"`
	SegmentSeconds int       `json:"segment_seconds,omitempty"`
	RetainSegments int       `json:"retain_segments,omitempty"`
	Runs           []Run     `json:"runs"`
	Reason         string    `json:"reason,omitempty"`
}

type diagnosticSessionGroup struct {
	id        string
	createdAt time.Time
	runIDs    []string
	status    string
	reason    string
	rolling   bool
	stopped   bool
	retain    int
	seconds   int
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
	result := SessionGroup{ID: copy.id, CreatedAt: copy.createdAt, Status: copy.status, Rolling: copy.rolling, SegmentSeconds: copy.seconds, RetainSegments: copy.retain, Reason: copy.reason, Runs: []Run{}}
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
	s.mu.Lock()
	group := s.sessionGroups[req.SessionGroupID]
	rolling := group != nil && group.rolling && !group.stopped && detail.Status != "canceled"
	s.mu.Unlock()
	retry := rolling || req.SessionAttempt < req.ReconnectAttempts && detail.Status != "canceled" &&
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
		var deleteID string
		if group := s.sessionGroups[req.SessionGroupID]; group != nil {
			group.runIDs = append(group.runIDs, nextRun.ID)
			if group.rolling && len(group.runIDs) > group.retain {
				deleteID = group.runIDs[0]
				group.runIDs = append([]string(nil), group.runIDs[1:]...)
			}
		}
		s.mu.Unlock()
		if deleteID != "" {
			_ = s.Delete(deleteID)
		}
	}()
}

func (s *Service) StopSessionGroup(id string) (SessionGroup, error) {
	s.mu.Lock()
	group := s.sessionGroups[id]
	if group == nil {
		s.mu.Unlock()
		return SessionGroup{}, ErrNotFound
	}
	group.stopped = true
	group.status = "done"
	group.reason = "canceled"
	var current string
	if len(group.runIDs) > 0 {
		current = group.runIDs[len(group.runIDs)-1]
	}
	s.mu.Unlock()
	if current != "" {
		if _, err := s.Stop(current); err != nil {
			return SessionGroup{}, err
		}
	}
	return s.SessionGroup(id)
}

func validateRolling(req *SampleRequest) error {
	if req.SegmentSeconds == 0 {
		if req.RollingSegments != 0 {
			return fmt.Errorf("%w: 设置保留段数时必须同时设置分段时长", ErrInvalid)
		}
		return nil
	}
	if req.SegmentSeconds < 1 || req.SegmentSeconds > maxSeconds || req.RollingSegments < 0 || req.RollingSegments > 12 {
		return fmt.Errorf("%w: 分段时长须为 1–300 秒，保留段数须为 1–12", ErrInvalid)
	}
	if req.Protocol != ProtocolHTTPFLV && req.Protocol != ProtocolRTMP && req.Protocol != ProtocolHLS {
		return fmt.Errorf("%w: 滚动分段目前适用于 HTTP-FLV、RTMP/RTMPS 与 HLS", ErrInvalid)
	}
	if req.ReconnectAttempts != 0 || req.Playback || req.AutoInvestigate {
		return fmt.Errorf("%w: 滚动分段不叠加自动重连或播放器观测", ErrInvalid)
	}
	if req.RollingSegments == 0 {
		req.RollingSegments = 3
	}
	req.Seconds = req.SegmentSeconds
	return nil
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
