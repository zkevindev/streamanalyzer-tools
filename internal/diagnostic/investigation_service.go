package diagnostic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func investigationRevision(report Report) string {
	if report.Manifest.StallEvidence == nil {
		return ""
	}
	return report.Manifest.StallEvidence.SHA256
}

func investigationByID(report *Report, id string) *StallInvestigation {
	for i := range report.StallInvestigations {
		if report.StallInvestigations[i].ID == id {
			return &report.StallInvestigations[i]
		}
	}
	return nil
}

func (s *Service) InvestigateStall(id string, request StallInvestigationRequest) (StallInvestigation, error) {
	s.persistence.Lock()
	defer s.persistence.Unlock()
	detail, err := s.detail(id)
	if err != nil {
		return StallInvestigation{}, err
	}
	if detail.Report == nil || detail.Status == "running" {
		return StallInvestigation{}, ErrPending
	}
	report := *detail.Report
	kind, index, err := normalizePlayerIntervalRequest(request)
	if err != nil {
		return StallInvestigation{}, err
	}
	interval, err := resolvePlayerInterval(report, kind, index)
	if err != nil {
		return StallInvestigation{}, err
	}
	record, err := readPlayerRecording(s.runDir(id))
	if err != nil {
		return StallInvestigation{}, err
	}
	if !record.Complete {
		return StallInvestigation{}, fmt.Errorf("%w: 播放器记录尚未保存完成", ErrPending)
	}
	if record.Revision != report.Player.Revision || report.Manifest.Player == nil || !sourceArtifactMatches(s.runDir(id), playerName, *report.Manifest.Player, maxPlayerBytes) {
		return StallInvestigation{}, ErrReportChanged
	}
	var item *StallInvestigation
	for i := range report.StallInvestigations {
		candidate := &report.StallInvestigations[i]
		candidateKind, err := normalizePlayerIntervalKind(candidate.IntervalKind)
		if err != nil {
			return StallInvestigation{}, err
		}
		if candidateKind == kind && candidate.StallIndex == index {
			if candidate.PlayerRevision != report.Player.Revision {
				return StallInvestigation{}, ErrReportChanged
			}
			if !request.Retry || candidate.Status == "queued" || candidate.Status == "running" || len(candidate.Media.Assets) > 0 {
				return *candidate, nil
			}
			item = candidate
			break
		}
	}
	if item == nil {
		if len(report.StallInvestigations) >= maxStallInvestigations {
			return StallInvestigation{}, fmt.Errorf("%w: 每份报告最多补查 %d 个异常区间", ErrInvalid, maxStallInvestigations)
		}
		now := time.Now().UTC()
		storedKind := kind
		if kind == IntervalBuffering {
			storedKind = ""
		}
		report.StallInvestigations = append(report.StallInvestigations, StallInvestigation{
			ID: fmt.Sprintf("S%03d", len(report.StallInvestigations)+1), StallIndex: index, IntervalKind: storedKind,
			PlayerRevision: report.Player.Revision, Stall: interval, CreatedAt: now,
		})
		item = &report.StallInvestigations[len(report.StallInvestigations)-1]
	}
	previousState := *item
	item.Status, item.ModelState, item.ModelError, item.Explanation = "queued", "pending", "", nil
	item.UpdatedAt = time.Now().UTC()
	item.Media = InvestigationMedia{Status: "unavailable", Reason: "等待提取源流片段和截图", Assets: []DerivedArtifact{}}
	s.mu.Lock()
	if s.closed || len(s.investigationJobs) >= 16 {
		s.mu.Unlock()
		return StallInvestigation{}, ErrBusy
	}
	key := id + "/" + item.ID
	if _, active := s.investigationJobs[key]; active {
		s.mu.Unlock()
		return previousState, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.investigationJobs[key] = cancel
	s.workers.Add(1)
	if err := persistStallReport(s.runDir(id), &report); err != nil {
		delete(s.investigationJobs, key)
		cancel()
		s.workers.Done()
		s.mu.Unlock()
		return StallInvestigation{}, err
	}
	s.mu.Unlock()
	result := *item
	go s.runStallInvestigation(ctx, id, result, report, key, cancel)
	return result, nil
}

func (s *Service) runStallInvestigation(ctx context.Context, runID string, item StallInvestigation, report Report, key string, cancel context.CancelFunc) {
	defer s.workers.Done()
	defer func() { cancel(); s.mu.Lock(); delete(s.investigationJobs, key); s.mu.Unlock() }()
	select {
	case s.investigationSlots <- struct{}{}:
		defer func() { <-s.investigationSlots }()
	case <-ctx.Done():
		item.Status = "canceled"
		item.ModelState = "failed"
		item.ModelError = "补查已停止"
		_ = s.saveStallItem(runID, item)
		return
	}
	item.Status = "running"
	if err := s.saveStallItem(runID, item); err != nil {
		return
	}
	if !sourceArtifactMatches(s.runDir(runID), dumpName(report.Manifest.Protocol), report.Manifest.Stream, maxStreamBytes) ||
		!sourceArtifactMatches(s.runDir(runID), timelineName, report.Manifest.Timeline, maxTimeline) {
		item.Status, item.ModelState, item.ModelError = "failed", "failed", "原始流或时间线未通过 SHA-256 校验，无法补查"
		item.Media.Reason = "原始采样校验失败"
		_ = s.saveStallItem(runID, item)
		return
	}
	correlation, err := correlatePlayerInterval(s.runDir(runID), report, item.ID, item.IntervalKind, item.StallIndex)
	if err != nil {
		item.Status, item.ModelState, item.ModelError = "failed", "failed", "无法从原始流和时间线重建补查证据"
		item.Media.Reason = "时序证据重建失败，已保留原始数据"
		_ = s.saveStallItem(runID, item)
		return
	}
	item.Correlation = correlation
	item.Assessment = assessStall(item)
	if err := s.saveStallItem(runID, item); err != nil {
		return
	}
	if ctx.Err() == nil {
		item.Media = extractStallMedia(ctx, s.runDir(runID), report, item)
	}
	if ctx.Err() != nil {
		item.Status, item.ModelState, item.ModelError = "canceled", "failed", "补查已停止"
		_ = s.saveStallItem(runID, item)
		return
	}
	if err := s.saveStallItem(runID, item); err != nil {
		return
	}
	s.investigationModel.Lock()
	explanation, selection, modelErr := s.explainStall(ctx, runID, item.ID)
	s.investigationModel.Unlock()
	_, _ = s.finishStallModel(ctx, runID, item, explanation, selection, modelErr)
}

func stallModelFailure(err error) (string, string) {
	switch {
	case errors.Is(err, ErrUnconfigured):
		return "unconfigured", "尚未配置大模型，关联证据和媒体产物已保留"
	case errors.Is(err, ErrModelChanged):
		return "changed", "模型配置已变更，可使用当前模型重新解读"
	case errors.Is(err, ErrBusy):
		return "failed", "模型正在处理其他请求，请稍后重试"
	case errors.Is(err, ErrReportChanged):
		return "failed", "补查证据已更新，请重新解读"
	case errors.Is(err, context.Canceled):
		return "failed", "模型解读已停止"
	default:
		return "failed", "模型解读未完成，请检查模型连接后重试；关联证据已保留"
	}
}

func (s *Service) saveStallItem(runID string, item StallInvestigation) error {
	s.persistence.Lock()
	defer s.persistence.Unlock()
	report, err := readJSONFile[Report](s.runDir(runID), reportName)
	if err != nil {
		return err
	}
	previous := investigationByID(&report, item.ID)
	if previous == nil || report.Player == nil || item.PlayerRevision != report.Player.Revision {
		return ErrReportChanged
	}
	item.UpdatedAt = time.Now().UTC()
	*previous = item
	return persistStallReport(s.runDir(runID), &report)
}

// The scoped report gives every suggestion only this stall's evidence IDs.
func (s *Service) explainStall(ctx context.Context, runID, caseID string) (Explanation, *modelSelection, error) {
	detail, err := s.Detail(runID)
	if err != nil {
		return Explanation{}, nil, err
	}
	if detail.Report == nil {
		return Explanation{}, nil, ErrPending
	}
	item := investigationByID(detail.Report, caseID)
	if item == nil {
		return Explanation{}, nil, ErrNotFound
	}
	if len(item.Correlation.Findings) == 0 {
		return Explanation{}, nil, ErrPending
	}
	s.modelMu.Lock()
	model := s.model
	if model == nil {
		s.modelMu.Unlock()
		return Explanation{}, nil, ErrUnconfigured
	}
	if model.cancel != nil {
		s.modelMu.Unlock()
		return Explanation{}, nil, ErrBusy
	}
	ctx, cancel := context.WithCancel(ctx)
	model.cancel = cancel
	s.modelMu.Unlock()
	defer func() { cancel(); s.modelMu.Lock(); model.cancel = nil; s.modelMu.Unlock() }()
	scoped := *detail.Report
	scoped.Findings = append([]Finding(nil), item.Correlation.Findings...)
	if item.Assessment != nil {
		scoped.Findings = append(scoped.Findings, item.Assessment.Findings...)
	}
	scoped.StallInvestigations = []StallInvestigation{*item}
	// The selected case already includes its actual playback requests. Keep
	// unrelated requests from the rest of the run out of a scoped model call.
	scoped.PlayerNetwork = nil
	player := *scoped.Player
	player.Scheduling = item.Correlation.Scheduling
	player.Presentation = item.Correlation.Presentation
	if item.IntervalKind == IntervalPresentationGap {
		player.Stalls = []PlayerStall{}
		for _, stall := range scoped.Player.Stalls {
			if min(stall.EndMS, item.Stall.EndMS) > max(stall.StartMS, item.Stall.StartMS) {
				player.Stalls = append(player.Stalls, stall)
			}
		}
		player.StallCount, player.TotalStallMS, player.MaxStallMS = len(player.Stalls), 0, 0
		for _, stall := range player.Stalls {
			player.TotalStallMS += stall.DurationMS
			player.MaxStallMS = max(player.MaxStallMS, stall.DurationMS)
		}
	} else {
		player.Stalls = []PlayerStall{item.Stall}
		player.StallCount = 1
		player.TotalStallMS, player.MaxStallMS = item.Stall.DurationMS, item.Stall.DurationMS
	}
	player.StallRatio = 0
	if player.ActiveMS > 0 {
		player.StallRatio = float64(player.TotalStallMS) / float64(player.ActiveMS)
	}
	scoped.Player = &player
	explanation, err := model.explainer.Explain(ctx, scoped)
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.model != model {
		return Explanation{}, model, ErrModelChanged
	}
	if err != nil {
		return Explanation{}, model, err
	}
	if !validExplanation(explanation, scoped.Findings) {
		return Explanation{}, model, ErrProvider
	}
	return explanation, model, nil
}

func (s *Service) finishStallModel(ctx context.Context, runID string, item StallInvestigation, explanation Explanation, selection *modelSelection, modelErr error) (StallInvestigation, error) {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if selection != nil && s.model != selection {
		modelErr = ErrModelChanged
	}
	if modelErr == nil {
		item.Explanation, item.ModelState, item.ModelError = &explanation, "complete", ""
	} else {
		item.ModelState, item.ModelError = stallModelFailure(modelErr)
		item.Explanation = nil
	}
	item.Status = "complete"
	if item.Media.Status != "ready" || item.ModelState != "complete" {
		item.Status = "partial"
	}
	if ctx.Err() != nil {
		item.Status = "canceled"
	}
	if err := s.saveStallItem(runID, item); err != nil {
		return StallInvestigation{}, err
	}
	return item, nil
}

func (s *Service) ExplainStall(ctx context.Context, runID, caseID string) (StallInvestigation, error) {
	s.mu.Lock()
	key := runID + "/" + caseID
	_, active := s.investigationJobs[key]
	if s.closed || active {
		s.mu.Unlock()
		return StallInvestigation{}, ErrBusy
	}
	ctx, cancel := context.WithCancel(ctx)
	s.investigationJobs[key] = cancel
	s.workers.Add(1)
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.investigationJobs, key); s.mu.Unlock(); s.workers.Done() }()
	detail, err := s.Detail(runID)
	if err != nil {
		return StallInvestigation{}, err
	}
	if detail.Report == nil {
		return StallInvestigation{}, ErrPending
	}
	previous := investigationByID(detail.Report, caseID)
	if previous == nil {
		return StallInvestigation{}, ErrNotFound
	}
	item := *previous
	if len(item.Correlation.Findings) == 0 {
		return StallInvestigation{}, ErrPending
	}
	if item.Assessment == nil {
		item.Assessment = assessStall(item)
	}
	item.Status, item.ModelState, item.ModelError, item.Explanation = "running", "pending", "", nil
	if err := s.saveStallItem(runID, item); err != nil {
		return StallInvestigation{}, err
	}
	explanation, selection, modelErr := s.explainStall(ctx, runID, caseID)
	return s.finishStallModel(ctx, runID, item, explanation, selection, modelErr)
}

func (s *Service) StopStall(runID, caseID string) error {
	s.mu.Lock()
	cancel := s.investigationJobs[runID+"/"+caseID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		return nil
	}
	detail, err := s.Detail(runID)
	if err != nil {
		return err
	}
	if detail.Report == nil {
		return ErrPending
	}
	if investigationByID(detail.Report, caseID) == nil {
		return ErrNotFound
	}
	return nil
}

func persistStallReport(dir string, report *Report) error {
	ledger := investigationLedger{Version: 1, Items: report.StallInvestigations}
	data, err := json.Marshal(ledger)
	if err != nil {
		return err
	}
	if len(data) > maxInvestigationBytes {
		return fmt.Errorf("%w: 卡顿补查元数据超限", ErrInvalid)
	}
	hash := sha256.Sum256(data)
	artifact := Artifact{Bytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}
	name := investigationVersionName(artifact.SHA256)
	if err := writeImmutableEvidence(dir, name, data); err != nil {
		return err
	}
	report.Manifest.StallEvidence = &artifact
	report.Manifest.Version = max(report.Manifest.Version, investigationSchemaVersion)
	mergeStallFindings(report)
	if err := os.Remove(filepath.Join(dir, explanationName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := persistReport(dir, *report); err != nil {
		return err
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		value := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "stall-evidence-"), ".json")
		_, hashErr := hex.DecodeString(value)
		if len(value) == 64 && hashErr == nil && entry.Name() == investigationVersionName(value) && entry.Name() != name {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	return nil
}

func investigationVersionName(hash string) string { return "stall-evidence-" + hash + ".json" }

func writeImmutableEvidence(dir, name string, data []byte) error {
	path := filepath.Join(dir, name)
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) == string(data) {
			return nil
		}
		return ErrInvalid
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".stall-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(data)
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func mergeStallFindings(report *Report) {
	report.Findings = slices.DeleteFunc(report.Findings, func(f Finding) bool {
		return strings.HasPrefix(f.ID, "S") && (strings.Contains(f.ID, "-E") || strings.Contains(f.ID, "-A"))
	})
	for _, item := range report.StallInvestigations {
		report.Findings = append(report.Findings, item.Correlation.Findings...)
		if item.Assessment != nil {
			report.Findings = append(report.Findings, item.Assessment.Findings...)
		}
	}
}

func (s *Service) recoverInvestigations() error {
	rows, err := s.db.Query("SELECT id FROM runs WHERE report_ready = 1")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, id := range ids {
		report, err := readJSONFile[Report](s.runDir(id), reportName)
		if err != nil {
			slog.Warn("skip unreadable historical investigation report", "run_id", id, "error", err)
			continue
		}
		changed := false
		for i := range report.StallInvestigations {
			item := &report.StallInvestigations[i]
			if item.Status == "queued" || item.Status == "running" {
				item.Status = "canceled"
				item.ModelState = "failed"
				item.ModelError = "程序退出前补查尚未完成，可重新补查或解读"
				item.UpdatedAt = time.Now().UTC()
				changed = true
			}
		}
		if changed {
			if err := persistStallReport(s.runDir(id), &report); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) OpenStallAsset(runID, caseID, filename string) (*os.File, error) {
	detail, err := s.Detail(runID)
	if err != nil {
		return nil, err
	}
	if detail.Report == nil {
		return nil, ErrPending
	}
	item := investigationByID(detail.Report, caseID)
	if item == nil {
		return nil, ErrNotFound
	}
	name := "stalls/" + caseID + "/" + filename
	for _, asset := range item.Media.Assets {
		if asset.Name == name {
			if !validStallAssetName(name) {
				return nil, ErrNotFound
			}
			file, err := os.Open(filepath.Join(s.runDir(runID), filepath.FromSlash(name)))
			if errors.Is(err, os.ErrNotExist) {
				return nil, ErrNotFound
			}
			return file, err
		}
	}
	return nil, ErrNotFound
}

func readStallLedger(dir string, artifact Artifact) ([]byte, error) {
	file, err := os.Open(filepath.Join(dir, investigationVersionName(artifact.SHA256)))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxInvestigationBytes+1))
	if err != nil || len(data) > maxInvestigationBytes || int64(len(data)) != artifact.Bytes {
		return nil, ErrInvalid
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != artifact.SHA256 {
		return nil, ErrInvalid
	}
	return data, nil
}
