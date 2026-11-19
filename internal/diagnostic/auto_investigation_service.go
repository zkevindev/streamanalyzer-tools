package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const maxAutomaticJobs = 4
const automaticCaseModelNote = "自动补查仅提取证据，未请求单次模型解读；由整份报告统一总结。"

type automaticInvestigationJob struct {
	cancel      context.CancelFunc
	wake        chan struct{}
	interrupted bool
}
type automaticRequest struct {
	Version     int       `json:"version"`
	RequestedAt time.Time `json:"requested_at"`
	Canceled    bool      `json:"canceled,omitempty"`
}

func markAutomaticCaseModel(item *StallInvestigation) {
	item.ModelState = "not_requested"
	item.ModelError = automaticCaseModelNote
	item.Explanation = nil
}

// Caller holds persistence. The next stall-ledger commit publishes both the
// newly-created case and its automatic selection reference atomically.
func attachAutomaticCase(dir string, report *Report, item StallInvestigation) error {
	if report.AutomaticInvestigation == nil {
		return ErrInvalid
	}
	state := *report.AutomaticInvestigation
	state.Selected = append([]AutomaticSelection(nil), state.Selected...)
	kind, err := normalizePlayerIntervalKind(item.IntervalKind)
	if err != nil {
		return err
	}
	found := false
	for index := range state.Selected {
		selection := &state.Selected[index]
		if selection.IntervalKind == kind && selection.IntervalIndex == item.StallIndex && selection.Interval == item.Stall {
			selection.CaseID = item.ID
			found = true
		}
	}
	if !found {
		return ErrInvalid
	}
	state.UpdatedAt = time.Now().UTC()
	return attachAutomaticInvestigation(dir, report, state)
}
func writeAutomaticRequest(dir string, at time.Time) error {
	return writeJSONFile(dir, automaticRequestName, automaticRequest{Version: 1, RequestedAt: at})
}
func automaticRequested(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, automaticRequestName))
	if err != nil || len(data) > maxAutomaticBytes {
		return false
	}
	var request automaticRequest
	return json.Unmarshal(data, &request) == nil && request.Version == 1 && !request.RequestedAt.IsZero() && !request.Canceled
}
func initialAutomaticInvestigation(report Report, at time.Time) AutomaticInvestigation {
	return AutomaticInvestigation{Version: 1, Policy: automaticPolicy, State: "waiting_player", SourceArtifact: report.Manifest.Stream, TimelineArtifact: report.Manifest.Timeline, Selected: []AutomaticSelection{}, ModelState: "pending", CreatedAt: at, UpdatedAt: time.Now().UTC()}
}
func automaticTerminal(state string) bool {
	switch state {
	case "complete", "partial", "unconfigured", "failed", "canceled", "interrupted":
		return true
	}
	return false
}

// Both capture and final-player completion may call this. Only a live capture
// marker authorizes the implicit trigger; import/reopen never calls it.
func (s *Service) maybeStartAutomaticInvestigation(id string) {
	s.mu.Lock()
	if job := s.automaticJobs[id]; job != nil {
		select {
		case job.wake <- struct{}{}:
		default:
		}
		s.mu.Unlock()
		return
	}
	closed := s.closed
	s.mu.Unlock()
	if closed || !automaticRequested(s.runDir(id)) {
		return
	}
	_, err := s.startAutomaticInvestigation(id, false)
	if errors.Is(err, ErrBusy) {
		_ = s.updateAutomaticInvestigation(id, func(state *AutomaticInvestigation) {
			s.mu.Lock()
			running, closed := s.automaticJobs[id] != nil, s.closed
			s.mu.Unlock()
			if !running && !closed && !automaticTerminal(state.State) {
				finishAutomaticState(state, "failed", "not_requested", "worker_limit", "自动补查队列已满，现有采样已保留，可稍后重试")
			}
		})
	}
}
func (s *Service) startAutomaticInvestigation(id string, retry bool) (AutomaticInvestigation, error) {
	s.persistence.Lock()
	defer s.persistence.Unlock()
	detail, err := s.detail(id)
	if err != nil {
		return AutomaticInvestigation{}, err
	}
	if detail.Report == nil || detail.Status == "running" {
		return AutomaticInvestigation{}, ErrPending
	}
	report := *detail.Report
	if report.AutomaticInvestigation == nil {
		return AutomaticInvestigation{}, ErrNotFound
	}
	state := *report.AutomaticInvestigation
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return state, ErrBusy
	}
	if job := s.automaticJobs[id]; job != nil {
		select {
		case job.wake <- struct{}{}:
		default:
		}
		return state, ErrBusy
	}
	if !retry && automaticTerminal(state.State) {
		return state, nil
	}
	if s.active[id] != nil {
		return state, ErrPending
	}
	if len(s.automaticJobs) >= maxAutomaticJobs {
		return state, ErrBusy
	}
	record, recordErr := readPlayerRecording(s.runDir(id))
	if retry && state.PlayerRevision > 0 && (recordErr != nil || !record.Complete || report.Player == nil || report.Manifest.Player == nil || record.Revision != report.Player.Revision) {
		return state, ErrReportChanged
	}
	if retry {
		if err := writeAutomaticRequest(s.runDir(id), time.Now().UTC()); err != nil {
			return state, err
		}
		state.OriginalState = ""
		state.CompletedAt = nil
		state.Reason = ""
		state.ReasonCode = ""
		state.SummaryReportSHA256 = ""
		state.ModelState = "pending"
	}
	if recordErr == nil && record.Complete && report.Player != nil && report.Manifest.Player != nil && record.Revision == report.Player.Revision {
		state = automaticPlannedState(report, state)
	} else {
		state.State = "waiting_player"
	}
	state.UpdatedAt = time.Now().UTC()
	if err := persistAutomaticInvestigation(s.runDir(id), &report, state); err != nil {
		return state, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	job := &automaticInvestigationJob{cancel: cancel, wake: make(chan struct{}, 1)}
	s.automaticJobs[id] = job
	s.workers.Add(1)
	go s.runAutomaticInvestigation(ctx, id, job)
	return state, nil
}
func automaticPlannedState(report Report, state AutomaticInvestigation) AutomaticInvestigation {
	plan := planAutomaticInvestigation(report)
	state.State = "queued"
	state.SourceArtifact = report.Manifest.Stream
	state.TimelineArtifact = report.Manifest.Timeline
	state.PlayerRevision = report.Player.Revision
	artifact := *report.Manifest.Player
	state.PlayerArtifact = &artifact
	state.CandidateCount = plan.CandidateCount
	state.Selected = plan.Selected
	state.ModelState = "pending"
	for index := range state.Selected {
		selection := &state.Selected[index]
		for _, item := range report.StallInvestigations {
			kind, err := normalizePlayerIntervalKind(item.IntervalKind)
			if err == nil && kind == selection.IntervalKind && item.StallIndex == selection.IntervalIndex && item.PlayerRevision == state.PlayerRevision && item.Stall == selection.Interval {
				selection.CaseID = item.ID
				break
			}
		}
	}
	return state
}
func (s *Service) RetryAutomaticInvestigation(id string) (AutomaticInvestigation, error) {
	return s.startAutomaticInvestigation(id, true)
}
func (s *Service) StopAutomaticInvestigation(id string) error {
	s.persistence.Lock()
	defer s.persistence.Unlock()
	s.mu.Lock()
	job := s.automaticJobs[id]
	if job != nil {
		job.cancel()
	}
	s.mu.Unlock()
	if job != nil {
		return nil
	}
	run, err := s.getRun(id)
	if err != nil {
		return err
	}
	dir := s.runDir(id)
	report, err := readJSONFile[Report](dir, reportName)
	if errors.Is(err, os.ErrNotExist) && run.Mode == "sample" {
		data, readErr := os.ReadFile(filepath.Join(dir, automaticRequestName))
		var request automaticRequest
		if readErr != nil || json.Unmarshal(data, &request) != nil || request.Version != 1 {
			return ErrNotFound
		}
		request.Canceled = true
		return writeJSONFile(dir, automaticRequestName, request)
	}
	if err != nil {
		return err
	}
	if report.AutomaticInvestigation == nil {
		return ErrNotFound
	}
	state := *report.AutomaticInvestigation
	if !automaticTerminal(state.State) {
		finishAutomaticState(&state, "canceled", "canceled", "canceled", "自动补查已停止")
		state.UpdatedAt = time.Now().UTC()
		return persistAutomaticInvestigation(dir, &report, state)
	}
	return nil
}
func automaticRequestCanceled(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, automaticRequestName))
	if err != nil || len(data) > maxAutomaticBytes {
		return false
	}
	var request automaticRequest
	return json.Unmarshal(data, &request) == nil && request.Canceled
}

func (s *Service) updateAutomaticInvestigation(id string, update func(*AutomaticInvestigation)) error {
	s.persistence.Lock()
	defer s.persistence.Unlock()
	report, err := readJSONFile[Report](s.runDir(id), reportName)
	if err != nil {
		return err
	}
	if report.AutomaticInvestigation == nil {
		return ErrNotFound
	}
	state := *report.AutomaticInvestigation
	state.Selected = append([]AutomaticSelection(nil), state.Selected...)
	update(&state)
	state.UpdatedAt = time.Now().UTC()
	return persistAutomaticInvestigation(s.runDir(id), &report, state)
}
func finishAutomaticState(state *AutomaticInvestigation, status, modelState, code, reason string) {
	state.State = status
	state.ModelState = modelState
	state.ReasonCode = code
	state.Reason = reason
	now := time.Now().UTC()
	state.CompletedAt = &now
	state.UpdatedAt = now
}
func (s *Service) automaticSnapshot(id string) (Report, bool, error) {
	s.persistence.RLock()
	defer s.persistence.RUnlock()
	detail, err := s.detail(id)
	if err != nil {
		return Report{}, false, err
	}
	if detail.Report == nil || !detail.ReportReady || detail.Status == "running" {
		return Report{}, false, ErrPending
	}
	report := *detail.Report
	record, err := readPlayerRecording(s.runDir(id))
	if errors.Is(err, os.ErrNotExist) {
		return report, false, nil
	}
	if err != nil {
		return report, false, err
	}
	s.mu.Lock()
	active := s.active[id] != nil
	playerClosed := true
	if player := s.players[id]; player != nil {
		select {
		case <-player.session.Done():
		default:
			playerClosed = false
		}
	}
	s.mu.Unlock()
	ready := !active && playerClosed && record.Complete && report.Player != nil && report.Manifest.Player != nil && record.Revision == report.Player.Revision
	if ready && (!sourceArtifactMatches(s.runDir(id), dumpName(report.Manifest.Protocol), report.Manifest.Stream, maxStreamBytes) || !sourceArtifactMatches(s.runDir(id), timelineName, report.Manifest.Timeline, maxTimeline) || !sourceArtifactMatches(s.runDir(id), playerName, *report.Manifest.Player, maxPlayerBytes)) {
		return report, false, ErrReportChanged
	}
	return report, ready, nil
}
func (s *Service) runAutomaticInvestigation(ctx context.Context, id string, job *automaticInvestigationJob) {
	defer s.workers.Done()
	defer func() {
		job.cancel()
		s.mu.Lock()
		if s.automaticJobs[id] == job {
			delete(s.automaticJobs, id)
		}
		s.mu.Unlock()
	}()
	fail := func(code, reason string) {
		_ = s.updateAutomaticInvestigation(id, func(state *AutomaticInvestigation) { finishAutomaticState(state, "failed", "failed", code, reason) })
	}
	canceled := func() {
		s.mu.Lock()
		interrupted := job.interrupted
		s.mu.Unlock()
		_ = s.updateAutomaticInvestigation(id, func(state *AutomaticInvestigation) {
			if interrupted {
				state.OriginalState = state.State
				finishAutomaticState(state, "interrupted", "canceled", "interrupted", "程序退出前自动补查未完成，未自动续跑")
			} else {
				finishAutomaticState(state, "canceled", "canceled", "canceled", "自动补查已停止")
			}
		})
	}
	wait := s.automaticPlayerWait
	if wait <= 0 {
		wait = 30 * time.Second
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var snapshot Report
	for {
		report, ready, err := s.automaticSnapshot(id)
		if err != nil && !errors.Is(err, ErrPending) {
			fail("evidence_changed", "无法读取最终采样与播放器证据")
			return
		}
		if ready {
			snapshot = report
			break
		}
		select {
		case <-ctx.Done():
			canceled()
			return
		case <-deadline.C:
			fail("player_missing", "采样结束后30秒内未收到完整播放器记录，可保存后重试")
			return
		case <-job.wake:
		case <-ticker.C:
		}
	}
	if snapshot.AutomaticInvestigation == nil {
		fail("evidence_changed", "自动补查记录已改变")
		return
	}
	state := automaticPlannedState(snapshot, *snapshot.AutomaticInvestigation)
	if err := s.updateAutomaticInvestigation(id, func(current *AutomaticInvestigation) {
		created := current.CreatedAt
		*current = state
		current.CreatedAt = created
		current.State = "investigating"
	}); err != nil {
		return
	}
	partial := false
	reasonCode, reason := "", ""
	for _, selection := range state.Selected {
		if ctx.Err() != nil {
			canceled()
			return
		}
		item, err := s.investigateStallMedia(id, StallInvestigationRequest{IntervalKind: selection.IntervalKind, IntervalIndex: selection.IntervalIndex, Retry: true})
		if err != nil {
			partial = true
			if errors.Is(err, ErrInvalid) {
				reasonCode, reason = "case_limit", "补查数量达到上限，保留已有证据并继续整份报告总结"
			} else {
				reasonCode, reason = "media_partial", "部分异常证据无法生成，已保留现有采样"
			}
			continue
		}
		if err := s.updateAutomaticInvestigation(id, func(current *AutomaticInvestigation) {
			for index := range current.Selected {
				if current.Selected[index].IntervalKind == selection.IntervalKind && current.Selected[index].IntervalIndex == selection.IntervalIndex {
					current.Selected[index].CaseID = item.ID
				}
			}
		}); err != nil {
			return
		}
		owned := item.ModelState == "not_requested"
		for {
			s.mu.Lock()
			running := s.investigationJobs[id+"/"+item.ID] != nil
			s.mu.Unlock()
			if !running {
				break
			}
			select {
			case <-ctx.Done():
				if owned {
					_ = s.StopStall(id, item.ID)
				}
				canceled()
				return
			case <-ticker.C:
			}
		}
		detail, err := s.Detail(id)
		if err != nil || detail.Report == nil {
			fail("evidence_changed", "补查证据已改变")
			return
		}
		final := investigationByID(detail.Report, item.ID)
		if final == nil {
			fail("evidence_changed", "异常案例已改变")
			return
		}
		if final.Media.Status != "ready" || final.Status == "failed" || final.Status == "canceled" {
			partial = true
			if reasonCode == "" {
				reasonCode, reason = "media_partial", "部分媒体证据不可用，时序和原始采样仍已保留"
			}
		}
	}
	if ctx.Err() != nil {
		canceled()
		return
	}
	if err := s.updateAutomaticInvestigation(id, func(current *AutomaticInvestigation) {
		current.State = "summarizing"
		current.ModelState = "pending"
		current.ReasonCode = reasonCode
		current.Reason = reason
	}); err != nil {
		return
	}
	if !s.ModelConfigured() {
		_ = s.updateAutomaticInvestigation(id, func(current *AutomaticInvestigation) {
			finishAutomaticState(current, "unconfigured", "unconfigured", "model_unconfigured", "已完成证据补查，尚未配置模型；配置后可重新执行统一总结")
		})
		return
	}
	if !s.investigationModel.TryLock() {
		fail("model_failed", "模型正在处理其他请求，可稍后重新执行自动补查")
		return
	}
	answer, modelErr := s.RefreshExplanation(ctx, id)
	s.investigationModel.Unlock()
	if ctx.Err() != nil {
		canceled()
		return
	}
	if modelErr != nil {
		_ = s.updateAutomaticInvestigation(id, func(current *AutomaticInvestigation) {
			status, modelState, code, message := "partial", "failed", "model_failed", "统一模型总结未完成，可稍后重试"
			if errors.Is(modelErr, ErrUnconfigured) {
				status, modelState, code, message = "unconfigured", "unconfigured", "model_unconfigured", "已保留补查证据，模型尚未配置"
			}
			if errors.Is(modelErr, ErrModelChanged) {
				modelState, code, message = "changed", "model_changed", "总结期间模型配置已变更，可使用当前模型重试"
			}
			if errors.Is(modelErr, ErrReportChanged) {
				code, message = "evidence_changed", "总结期间证据已改变，未保存过期结果"
			}
			finishAutomaticState(current, status, modelState, code, message)
		})
		return
	}
	if answer.ReportInputEvidence == nil {
		fail("model_failed", "统一总结缺少输入证据记录")
		return
	}
	if err := s.finalizeAutomaticSummary(ctx, id, job, answer, partial, reasonCode, reason); err != nil {
		if ctx.Err() != nil {
			canceled()
		}
		return
	}
}
func (s *Service) recoverAutomaticInvestigations() error {
	rows, err := s.db.Query("SELECT id, mode FROM runs WHERE report_ready = 1")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id, mode string
		if err := rows.Scan(&id, &mode); err != nil {
			rows.Close()
			return err
		}
		if mode == "sample" || automaticRequested(s.runDir(id)) {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		report, err := readJSONFile[Report](s.runDir(id), reportName)
		if err != nil {
			slog.Warn("skip unreadable historical automatic investigation report", "run_id", id, "error", err)
			continue
		}
		if report.AutomaticInvestigation != nil && !automaticTerminal(report.AutomaticInvestigation.State) {
			state := *report.AutomaticInvestigation
			state.OriginalState = state.State
			finishAutomaticState(&state, "interrupted", "canceled", "interrupted", "程序退出前自动补查未完成，未自动续跑")
			if err := persistAutomaticInvestigation(s.runDir(id), &report, state); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) interruptAutomaticJobs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.automaticJobs {
		job.interrupted = true
		job.cancel()
	}
}

func invalidateAutomaticSummary(dir string, report *Report) error {
	if report.AutomaticInvestigation == nil {
		return nil
	}
	state := *report.AutomaticInvestigation
	if state.ModelState != "complete" || (state.State != "complete" && state.State != "partial") {
		return nil
	}
	state.State = "partial"
	state.ModelState = "changed"
	state.ReasonCode = "evidence_changed"
	state.Reason = "报告补查已更新，请重新生成整体解读"
	state.SummaryReportSHA256 = ""
	state.UpdatedAt = time.Now().UTC()
	return attachAutomaticInvestigation(dir, report, state)
}

// RefreshExplanation has already persisted its result. Commit automatic
// completion only if that exact result and its entire input scope still exist.
// The final task/ctx check shares the persistence→mu order with stop/start.
func (s *Service) finalizeAutomaticSummary(ctx context.Context, id string, job *automaticInvestigationJob, answer Explanation, partial bool, code, reason string) error {
	s.persistence.Lock()
	defer s.persistence.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	dir := s.runDir(id)
	report, err := readJSONFile[Report](dir, reportName)
	if err != nil {
		return err
	}
	if report.AutomaticInvestigation == nil {
		return ErrReportChanged
	}
	state := *report.AutomaticInvestigation
	current, readErr := readJSONFile[Explanation](dir, explanationName)
	valid := readErr == nil && answer.ReportInputEvidence != nil && current.ReportInputEvidence != nil && sameStallModelJSON(current, answer) && validateReportModelInput(dir, report, answer.ReportInputEvidence) == nil
	if report.Player == nil || report.Manifest.Player == nil || state.SourceArtifact != report.Manifest.Stream || state.TimelineArtifact != report.Manifest.Timeline || state.PlayerRevision != report.Player.Revision || !sameStallModelJSON(state.PlayerArtifact, report.Manifest.Player) {
		valid = false
	}
	if valid {
		expected := automaticPlannedState(report, state)
		valid = state.CandidateCount == expected.CandidateCount && sameStallModelJSON(state.Selected, expected.Selected)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.closed || s.automaticJobs[id] != job {
		return ErrReportChanged
	}
	if state.State != "summarizing" || state.ModelState != "pending" {
		return ErrReportChanged
	}
	if !valid {
		state.SummaryReportSHA256 = ""
		finishAutomaticState(&state, "partial", "changed", "evidence_changed", "总结保存后报告或补查证据已更新，请重新生成整体解读")
		if err := persistAutomaticInvestigation(dir, &report, state); err != nil {
			return err
		}
		return ErrReportChanged
	}
	status := "complete"
	if partial {
		status = "partial"
	}
	if state.CandidateCount == 0 {
		code, reason = "no_anomalies", "本次未发现可补查的同期异常区间；不等同于确认没有播放问题"
	}
	state.SummaryReportSHA256 = answer.ReportInputEvidence.ReportSHA256
	finishAutomaticState(&state, status, "complete", code, reason)
	return persistAutomaticInvestigation(dir, &report, state)
}
