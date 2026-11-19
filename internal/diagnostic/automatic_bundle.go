package diagnostic

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

func canonicalAutomaticJSON(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if !canonicalReportJSONValue(decoder, reflect.TypeFor[AutomaticInvestigation]()) {
		return false
	}
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

// Only the new optional marker is tightened here. Historical manifest fields
// retain their established compatibility behavior.
func validAutomaticManifestShape(data []byte, manifest Manifest) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	present := false
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		var value json.RawMessage
		if err != nil || !ok || decoder.Decode(&value) != nil {
			return false
		}
		if !strings.EqualFold(name, "automatic_investigation") {
			continue
		}
		if present || name != "automatic_investigation" || manifest.AutomaticInvestigation == nil || manifest.Player == nil {
			return false
		}
		present = true
		artifact := manifest.AutomaticInvestigation
		if artifact.Bytes <= 0 || artifact.Bytes > maxAutomaticBytes || len(artifact.SHA256) != 64 {
			return false
		}
		hash, err := hex.DecodeString(artifact.SHA256)
		if err != nil || hex.EncodeToString(hash) != artifact.SHA256 {
			return false
		}
		fields := json.NewDecoder(bytes.NewReader(value))
		if !canonicalReportJSONValue(fields, reflect.TypeFor[Artifact]()) {
			return false
		}
	}
	return present == (manifest.AutomaticInvestigation != nil)
}

func importedAutomaticInvestigation(dir string, report *Report, entries map[string]*zip.File) error {
	artifact := report.Manifest.AutomaticInvestigation
	entry := entries[automaticInvestigationName]
	if artifact == nil {
		if entry != nil {
			return fmt.Errorf("%w: 自动补查记录未在清单中声明", ErrInvalid)
		}
		return nil
	}
	if entry == nil || artifact.Bytes <= 0 || artifact.Bytes > maxAutomaticBytes {
		return fmt.Errorf("%w: 自动补查记录缺失或超限", ErrInvalid)
	}
	if err := extractArtifact(dir, entry, *artifact); err != nil {
		return err
	}
	reader, err := entry.Open()
	if err != nil {
		return fmt.Errorf("%w: 自动补查记录不可读", ErrInvalid)
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, maxAutomaticBytes+1))
	reader.Close()
	if readErr != nil {
		return fmt.Errorf("%w: 自动补查记录不可读", ErrInvalid)
	}
	state, err := decodeAutomaticBundle(dir, *report, data)
	if err != nil {
		return err
	}
	// The original stage remains an immutable observation. Imported runs are
	// replay-only and never schedule its unfinished work automatically.
	if err := writeImmutableEvidence(dir, automaticVersionName(artifact.SHA256), data); err != nil {
		return err
	}
	report.AutomaticInvestigation = &state
	return persistReport(dir, *report)
}

func decodeAutomaticBundle(dir string, report Report, data []byte, playerSnapshot ...[]byte) (AutomaticInvestigation, error) {
	var state AutomaticInvestigation
	if len(data) == 0 || len(data) > maxAutomaticBytes || !canonicalAutomaticJSON(data) || json.Unmarshal(data, &state) != nil {
		return state, fmt.Errorf("%w: 自动补查记录格式无效", ErrInvalid)
	}
	if err := validateAutomaticInvestigation(dir, report, state, playerSnapshot...); err != nil {
		return state, err
	}
	return state, nil
}

func validateAutomaticSnapshot(dir string, report Report, data []byte, playerSnapshot ...[]byte) error {
	if report.Manifest.AutomaticInvestigation == nil {
		if len(data) != 0 || report.AutomaticInvestigation != nil {
			return fmt.Errorf("%w: 自动补查快照缺少清单", ErrInvalid)
		}
		return nil
	}
	hash := sha256.Sum256(data)
	if int64(len(data)) != report.Manifest.AutomaticInvestigation.Bytes || hex.EncodeToString(hash[:]) != report.Manifest.AutomaticInvestigation.SHA256 {
		return fmt.Errorf("%w: 自动补查快照未通过哈希校验", ErrInvalid)
	}
	state, err := decodeAutomaticBundle(dir, report, data, playerSnapshot...)
	if err != nil {
		return err
	}
	if report.AutomaticInvestigation == nil || !sameStallModelJSON(state, *report.AutomaticInvestigation) {
		return fmt.Errorf("%w: 自动补查快照与报告不一致", ErrInvalid)
	}
	return nil
}

func validateAutomaticInvestigation(dir string, report Report, state AutomaticInvestigation, playerSnapshot ...[]byte) error {
	invalid := func() error { return fmt.Errorf("%w: 自动补查范围、状态或证据绑定无效", ErrInvalid) }
	if state.Version != 1 || state.Policy != automaticPolicy || report.Manifest.Player == nil || state.SourceArtifact != report.Manifest.Stream || state.TimelineArtifact != report.Manifest.Timeline || state.CreatedAt.IsZero() || state.UpdatedAt.Before(state.CreatedAt) || len(state.Reason) > 1024 || len(state.ReasonCode) > 128 || len(state.Selected) > maxAutomaticInvestigations {
		return invalid()
	}
	terminal := true
	switch state.State {
	case "waiting_player", "queued", "investigating", "summarizing":
		terminal = false
	case "complete", "partial", "unconfigured", "failed", "canceled", "interrupted":
	default:
		return invalid()
	}
	if terminal != (state.CompletedAt != nil) || state.CompletedAt != nil && (state.CompletedAt.Before(state.CreatedAt) || state.CompletedAt.After(state.UpdatedAt)) {
		return invalid()
	}
	if state.OriginalState != "" {
		if state.State != "interrupted" || state.OriginalState != "waiting_player" && state.OriginalState != "queued" && state.OriginalState != "investigating" && state.OriginalState != "summarizing" {
			return invalid()
		}
	}
	switch state.ReasonCode {
	case "", "player_missing", "case_limit", "media_partial", "model_unconfigured", "model_failed", "model_changed", "evidence_changed", "canceled", "interrupted", "no_anomalies", "worker_limit":
	default:
		return invalid()
	}
	switch state.ModelState {
	case "pending", "not_requested", "complete", "unconfigured", "failed", "changed", "canceled":
	default:
		return invalid()
	}
	if !terminal && state.ModelState != "pending" || state.State == "unconfigured" && state.ModelState != "unconfigured" || state.State == "canceled" && state.ModelState != "canceled" || state.ModelState == "complete" && state.State != "complete" && state.State != "partial" || state.ModelState != "complete" && state.SummaryReportSHA256 != "" {
		return invalid()
	}
	if state.State == "complete" && state.ModelState != "complete" && !(state.ModelState == "not_requested" && state.ReasonCode == "no_anomalies" && len(state.Selected) == 0) {
		return invalid()
	}
	if state.State == "complete" && state.ReasonCode != "" && state.ReasonCode != "no_anomalies" || state.ReasonCode == "no_anomalies" && state.CandidateCount != 0 {
		return invalid()
	}
	if state.PlayerRevision == 0 {
		if state.PlayerArtifact != nil || state.CandidateCount != 0 || len(state.Selected) != 0 || state.State != "waiting_player" && state.State != "canceled" && state.State != "failed" && state.State != "interrupted" {
			return invalid()
		}
	} else {
		if state.PlayerRevision < 1 || report.Player == nil || report.Manifest.Player == nil || state.PlayerArtifact == nil || state.PlayerRevision != report.Player.Revision || *state.PlayerArtifact != *report.Manifest.Player || state.State == "waiting_player" {
			return invalid()
		}
		var player []byte
		if len(playerSnapshot) > 0 {
			player = playerSnapshot[0]
		} else {
			// Import has already extracted and verified this bounded artifact.
			// Live export always supplies bytes captured under persistence.RLock.
			player, _ = os.ReadFile(filepath.Join(dir, playerName))
		}
		hash := sha256.Sum256(player)
		var record PlayerRecording
		if len(player) > maxPlayerBytes || int64(len(player)) != state.PlayerArtifact.Bytes || hex.EncodeToString(hash[:]) != state.PlayerArtifact.SHA256 || json.Unmarshal(player, &record) != nil || !record.Complete || record.Revision != state.PlayerRevision || validatePlayerRecording(record, report.Manifest.Observation.StartedAt) != nil {
			return invalid()
		}
		plan := planAutomaticInvestigation(report)
		if state.CandidateCount != plan.CandidateCount || len(state.Selected) != len(plan.Selected) {
			return invalid()
		}
		for index, selection := range state.Selected {
			expected := plan.Selected[index]
			if selection.IntervalKind != expected.IntervalKind || selection.IntervalIndex != expected.IntervalIndex || selection.Interval != expected.Interval {
				return invalid()
			}
		}
	}
	linked := map[string]bool{}
	for _, selection := range state.Selected {
		if selection.CaseID == "" {
			if state.State == "complete" || state.State == "summarizing" && state.ReasonCode != "case_limit" && state.ReasonCode != "media_partial" {
				return invalid()
			}
			continue
		}
		item := investigationByID(&report, selection.CaseID)
		if item == nil || linked[item.ID] || item.PlayerRevision != state.PlayerRevision || item.StallIndex != selection.IntervalIndex || item.Stall != selection.Interval {
			return invalid()
		}
		kind, err := normalizePlayerIntervalKind(item.IntervalKind)
		if err != nil || kind != selection.IntervalKind {
			return invalid()
		}
		linked[item.ID] = true
		if state.State == "complete" && item.Media.Status != "ready" {
			return invalid()
		}
	}
	for _, item := range report.StallInvestigations {
		if item.ModelState == "not_requested" && (!linked[item.ID] || item.Explanation != nil) {
			return invalid()
		}
	}
	if state.ModelState == "complete" {
		hash, err := reportModelScopeSHA256(report)
		if err != nil || state.SummaryReportSHA256 != hash {
			return invalid()
		}
	}
	return nil
}

func validateAutomaticSummary(report Report, explanation *Explanation) error {
	state := report.AutomaticInvestigation
	if state == nil || state.ModelState != "complete" {
		return nil
	}
	if explanation == nil || explanation.ReportInputEvidence == nil || state.SummaryReportSHA256 != explanation.ReportInputEvidence.ReportSHA256 {
		return fmt.Errorf("%w: 自动汇总缺少对应的整体模型解读", ErrInvalid)
	}
	return nil
}

func validateImportedAutomaticSummary(dir string, report Report) error {
	if report.AutomaticInvestigation == nil || report.AutomaticInvestigation.ModelState != "complete" {
		return nil
	}
	explanation, err := readReportBundleExplanation(dir, report)
	if err != nil {
		return err
	}
	return validateAutomaticSummary(report, explanation)
}
