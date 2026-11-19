package diagnostic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func automaticBundleFixture(t *testing.T, phase string) (*Service, string, AutomaticInvestigation, []byte) {
	t.Helper()
	service, id, item := openImageStallTestRun(t)
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	report := *detail.Report
	now := time.Now().UTC()
	state := AutomaticInvestigation{Version: 1, Policy: automaticPolicy, State: phase, SourceArtifact: report.Manifest.Stream, TimelineArtifact: report.Manifest.Timeline, Selected: []AutomaticSelection{}, ModelState: "pending", CreatedAt: now, UpdatedAt: now}
	if phase != "waiting_player" && phase != "interrupted" {
		plan := planAutomaticInvestigation(report)
		state.PlayerArtifact, state.PlayerRevision = report.Manifest.Player, report.Player.Revision
		state.Selected, state.CandidateCount = plan.Selected, plan.CandidateCount
		if len(state.Selected) != 1 || state.Selected[0].IntervalIndex != item.StallIndex {
			t.Fatal("unexpected automatic test interval plan")
		}
		state.Selected[0].CaseID = item.ID
		item.ModelState, item.Explanation, item.ModelError = "not_requested", nil, "自动补查仅提取证据，未请求单次模型解读；由整份报告统一总结。"
		item.Status = "complete"
		if phase == "investigating" {
			item.Status = "running"
		}
		if err := service.saveStallItem(id, item); err != nil {
			t.Fatal(err)
		}
		detail, _ = service.Detail(id)
		report = *detail.Report
	}
	if phase == "complete" {
		service.SetModel(testExplainer{identity: ModelIdentity{Provider: ProviderDeepSeek, Model: "automatic-text"}, explain: func(context.Context) (string, error) {
			return "自动补查后按整体时序核对源端与缓冲", nil
		}})
		explanation, err := service.RefreshExplanation(t.Context(), id)
		if err != nil || explanation.ReportInputEvidence == nil {
			t.Fatalf("automatic fixture global summary failed: %v", err)
		}
		state.ModelState, state.SummaryReportSHA256 = "complete", explanation.ReportInputEvidence.ReportSHA256
	}
	if phase == "unconfigured" {
		state.ModelState, state.ReasonCode = "unconfigured", "model_unconfigured"
	}
	if phase == "interrupted" {
		state.ModelState, state.OriginalState, state.ReasonCode = "failed", "waiting_player", "interrupted"
	}
	if phase == "complete" || phase == "unconfigured" || phase == "interrupted" {
		state.CompletedAt = &now
	}
	if err := persistAutomaticInvestigation(service.runDir(id), &report, state); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal(err)
	}
	return service, id, state, archive.Bytes()
}

func rewriteAutomaticBundle(t *testing.T, source []byte, mutate func(map[string]any), raw func([]byte) []byte) []byte {
	t.Helper()
	return networkRewriteBundle(t, source, func(files map[string][]byte) {
		var value map[string]any
		if err := json.Unmarshal(files[automaticInvestigationName], &value); err != nil {
			t.Fatal(err)
		}
		if mutate != nil {
			mutate(value)
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if raw != nil {
			data = raw(data)
		}
		files[automaticInvestigationName] = data
		var manifest Manifest
		if err := json.Unmarshal(files[manifestName], &manifest); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		manifest.AutomaticInvestigation = &Artifact{Bytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}
		files[manifestName], err = json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestAutomaticBundleOfflineExactCompletedAndUnfinishedSnapshots(t *testing.T) {
	for _, phase := range []string{"complete", "unconfigured", "waiting_player", "investigating", "interrupted"} {
		t.Run(phase, func(t *testing.T) {
			service, _, expected, archive := automaticBundleFixture(t, phase)
			service.SetModel(testExplainer{identity: ModelIdentity{Provider: ProviderDeepSeek, Model: "must-not-call"}, explain: func(context.Context) (string, error) {
				t.Error("offline import automatically requested a model")
				return "", ErrProvider
			}})
			t.Setenv("PATH", "")
			imported, err := service.Import(bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			detail, err := service.Detail(imported.ID)
			if err != nil || detail.Mode != "replay" || detail.Report.AutomaticInvestigation == nil {
				t.Fatal("automatic replay metadata missing", err)
			}
			before, _ := json.Marshal(expected)
			after, _ := json.Marshal(detail.Report.AutomaticInvestigation)
			if !bytes.Equal(before, after) {
				t.Fatal("import rewrote the original automatic progress or invented completion")
			}
			if phase == "complete" && detail.Explanation == nil {
				t.Fatal("completed automatic summary omitted")
			}
			if phase == "investigating" && (detail.Report.StallInvestigations[0].Status != "canceled" || detail.Report.StallInvestigations[0].ModelState != "failed") {
				t.Fatal("unfinished case was presented as still running after import")
			}
			var again bytes.Buffer
			if err := service.WriteBundle(imported.ID, &again); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(reportExplanationZIPEntry(t, archive, automaticInvestigationName), reportExplanationZIPEntry(t, again.Bytes(), automaticInvestigationName)) {
				t.Fatal("re-export rewrote the immutable original automatic record")
			}
			if _, err := os.Stat(filepath.Join(service.runDir(imported.ID), automaticRequestName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("import acquired private automatic execution intent")
			}
		})
	}
}

func TestAutomaticBundleRejectsResignedPlanAndSourceTampering(t *testing.T) {
	_, _, _, source := automaticBundleFixture(t, "complete")
	mutations := map[string]func(map[string]any){
		"version":         func(v map[string]any) { v["version"] = 99 },
		"policy":          func(v map[string]any) { v["policy"] = "invented" },
		"source_hash":     func(v map[string]any) { v["source_artifact"].(map[string]any)["sha256"] = strings.Repeat("0", 64) },
		"timeline_hash":   func(v map[string]any) { v["timeline_artifact"].(map[string]any)["sha256"] = strings.Repeat("0", 64) },
		"player_hash":     func(v map[string]any) { v["player_artifact"].(map[string]any)["sha256"] = strings.Repeat("0", 64) },
		"player_revision": func(v map[string]any) { v["player_revision"] = 99 },
		"candidate_count": func(v map[string]any) { v["candidate_count"] = 99 },
		"interval_index":  func(v map[string]any) { v["selected"].([]any)[0].(map[string]any)["interval_index"] = 99 },
		"interval_kind": func(v map[string]any) {
			v["selected"].([]any)[0].(map[string]any)["interval_kind"] = "presentation_gap"
		},
		"interval_length": func(v map[string]any) {
			v["selected"].([]any)[0].(map[string]any)["interval"].(map[string]any)["duration_ms"] = 99
		},
		"wrong_case_id":     func(v map[string]any) { v["selected"].([]any)[0].(map[string]any)["case_id"] = "S008" },
		"missing_case_id":   func(v map[string]any) { delete(v["selected"].([]any)[0].(map[string]any), "case_id") },
		"missing_selection": func(v map[string]any) { v["selected"] = []any{} },
		"summary_hash":      func(v map[string]any) { v["summary_report_sha256"] = strings.Repeat("0", 64) },
		"false_model_state": func(v map[string]any) { v["model_state"] = "pending" },
		"unknown_reason":    func(v map[string]any) { v["reason_code"] = "invented" },
		"null_created":      func(v map[string]any) { v["created_at"] = nil },
		"null_completed":    func(v map[string]any) { v["completed_at"] = nil },
		"casefold_state":    func(v map[string]any) { v["STATE"] = v["state"]; delete(v, "state") },
		"casefold_nested": func(v map[string]any) {
			p := v["player_artifact"].(map[string]any)
			p["SHA256"] = p["sha256"]
			delete(p, "sha256")
		},
		"extra_field": func(v map[string]any) { v["execution_intent"] = true },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			data := rewriteAutomaticBundle(t, source, mutate, nil)
			if _, err := readBundle(t.TempDir(), bytes.NewReader(data)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("re-signed automatic proof tamper accepted: %v", err)
			}
		})
	}
	for name, mutate := range map[string]func([]byte) []byte{
		"duplicate_state": func(data []byte) []byte {
			return []byte(strings.Replace(string(data), `"state":`, `"state":"complete","state":`, 1))
		},
		"trailing_json": func(data []byte) []byte { return append(data, []byte(` {}`)...) },
		"oversized":     func(data []byte) []byte { return append(data, bytes.Repeat([]byte(" "), maxAutomaticBytes)...) },
	} {
		t.Run(name, func(t *testing.T) {
			data := rewriteAutomaticBundle(t, source, nil, mutate)
			if _, err := readBundle(t.TempDir(), bytes.NewReader(data)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("ambiguous or oversized automatic JSON accepted: %v", err)
			}
		})
	}
}

func TestAutomaticBundleManifestAndSummaryGuards(t *testing.T) {
	_, _, _, source := automaticBundleFixture(t, "complete")
	for name, mutate := range map[string]func(map[string][]byte){
		"missing_sidecar": func(files map[string][]byte) { delete(files, automaticInvestigationName) },
		"missing_summary": func(files map[string][]byte) { delete(files, explanationName) },
		"missing_player_provenance": func(files map[string][]byte) {
			var manifest map[string]any
			_ = json.Unmarshal(files[manifestName], &manifest)
			delete(manifest, "player")
			delete(files, playerName)
			files[manifestName], _ = json.Marshal(manifest)
		},
		"private_intent": func(files map[string][]byte) { files[automaticRequestName] = []byte(`{"enabled":true}`) },
		"undeclared_sidecar": func(files map[string][]byte) {
			var manifest map[string]any
			_ = json.Unmarshal(files[manifestName], &manifest)
			delete(manifest, "automatic_investigation")
			files[manifestName], _ = json.Marshal(manifest)
		},
		"legacy_not_requested": func(files map[string][]byte) {
			var manifest map[string]any
			_ = json.Unmarshal(files[manifestName], &manifest)
			delete(manifest, "automatic_investigation")
			delete(files, automaticInvestigationName)
			files[manifestName], _ = json.Marshal(manifest)
		},
		"alias_marker": func(files map[string][]byte) {
			files[manifestName] = []byte(strings.Replace(string(files[manifestName]), `"automatic_investigation":`, `"AUTOMATIC_INVESTIGATION":`, 1))
		},
		"duplicate_marker": func(files map[string][]byte) {
			files[manifestName] = []byte(strings.Replace(string(files[manifestName]), `"automatic_investigation":`, `"automatic_investigation":null,"automatic_investigation":`, 1))
		},
		"null_marker": func(files map[string][]byte) {
			var manifest map[string]any
			_ = json.Unmarshal(files[manifestName], &manifest)
			manifest["automatic_investigation"] = nil
			files[manifestName], _ = json.Marshal(manifest)
		},
		"alias_artifact": func(files map[string][]byte) {
			var manifest map[string]any
			_ = json.Unmarshal(files[manifestName], &manifest)
			marker := manifest["automatic_investigation"].(map[string]any)
			marker["BYTES"] = marker["bytes"]
			delete(marker, "bytes")
			files[manifestName], _ = json.Marshal(manifest)
		},
	} {
		t.Run(name, func(t *testing.T) {
			data := networkRewriteBundle(t, source, mutate)
			if _, err := readBundle(t.TempDir(), bytes.NewReader(data)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("ambiguous marker or missing proof accepted: %v", err)
			}
		})
	}
}

func automaticUnsupportedBundleFixture(t *testing.T) (*Service, string, []byte) {
	t.Helper()
	service, id, _ := openStallServiceTestRun(t, nil, false)
	dir := service.runDir(id)
	detail, _ := service.Detail(id)
	report := *detail.Report
	record, err := readPlayerRecording(dir)
	if err != nil {
		t.Fatal(err)
	}
	record.Revision++
	record.Events = []PlayerEvent{{Kind: "start"}, {AtMS: 1, Kind: "unsupported", Code: "browser_codec_unsupported"}, {AtMS: record.ElapsedMS, Kind: "stop"}}
	record.Samples = []PlayerSample{}
	if err := writePlayerRecording(dir, record); err != nil {
		t.Fatal(err)
	}
	if err := attachPlayerArtifact(dir, &report); err != nil || report.Player.State != "unsupported" {
		t.Fatal("final unsupported recording not preserved", err)
	}
	if err := persistReport(dir, report); err != nil {
		t.Fatal(err)
	}
	service.SetModel(testExplainer{identity: ModelIdentity{Provider: ProviderDeepSeek, Model: "unsupported-text"}, explain: func(context.Context) (string, error) {
		return "播放器不支持当前格式，优先核对浏览器编解码能力", nil
	}})
	explanation, err := service.RefreshExplanation(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := AutomaticInvestigation{Version: 1, Policy: automaticPolicy, State: "complete", ModelState: "complete", ReasonCode: "no_anomalies", SourceArtifact: report.Manifest.Stream, TimelineArtifact: report.Manifest.Timeline, PlayerArtifact: report.Manifest.Player, PlayerRevision: record.Revision, Selected: []AutomaticSelection{}, SummaryReportSHA256: explanation.ReportInputEvidence.ReportSHA256, CreatedAt: now, UpdatedAt: now, CompletedAt: &now}
	if err := persistAutomaticInvestigation(dir, &report, state); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal(err)
	}
	return service, id, archive.Bytes()
}

func TestAutomaticBundleFinalUnsupportedPlayerUsesActualCompleteFlag(t *testing.T) {
	service, _, source := automaticUnsupportedBundleFixture(t)
	imported, err := service.Import(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.Detail(imported.ID)
	if err != nil || detail.Report.Player.State != "unsupported" || detail.Explanation == nil || detail.Explanation.ReportInputEvidence.Mode != "text" {
		t.Fatal("final unsupported player lost its text conclusion", err)
	}
	changed := networkRewriteBundle(t, source, func(files map[string][]byte) {
		var record PlayerRecording
		_ = json.Unmarshal(files[playerName], &record)
		record.Complete = false
		files[playerName], _ = json.Marshal(record)
		playerHash := sha256.Sum256(files[playerName])
		player := Artifact{Bytes: int64(len(files[playerName])), SHA256: hex.EncodeToString(playerHash[:])}
		var state AutomaticInvestigation
		_ = json.Unmarshal(files[automaticInvestigationName], &state)
		state.State, state.ModelState, state.SummaryReportSHA256 = "queued", "pending", ""
		state.PlayerArtifact, state.CompletedAt = &player, nil
		files[automaticInvestigationName], _ = json.Marshal(state)
		autoHash := sha256.Sum256(files[automaticInvestigationName])
		var manifest Manifest
		_ = json.Unmarshal(files[manifestName], &manifest)
		manifest.Player = &player
		manifest.AutomaticInvestigation = &Artifact{Bytes: int64(len(files[automaticInvestigationName])), SHA256: hex.EncodeToString(autoHash[:])}
		files[manifestName], _ = json.Marshal(manifest)
		delete(files, explanationName)
	})
	if _, err := readBundle(t.TempDir(), bytes.NewReader(changed)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported UI state hid an unfinished recording: %v", err)
	}
}

func TestAutomaticBundleUsesCapturedPlayerBytesAfterMutableRecordingChanges(t *testing.T) {
	service, id, _ := automaticUnsupportedBundleFixture(t)
	detail, _ := service.Detail(id)
	dir := service.runDir(id)
	player, err := os.ReadFile(filepath.Join(dir, playerName))
	if err != nil {
		t.Fatal(err)
	}
	automatic, err := readAutomaticInvestigation(dir, *detail.Report.Manifest.AutomaticInvestigation)
	if err != nil {
		t.Fatal(err)
	}
	var record PlayerRecording
	_ = json.Unmarshal(player, &record)
	record.Complete, record.Revision = false, record.Revision+1
	if err := writePlayerRecording(dir, record); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := writeBundleSnapshotAutomatic(&archive, dir, *detail.Report, bytes.NewReader(player), nil, detail.Explanation, automatic, player); err != nil {
		t.Fatal("export read a newer mutable player instead of the captured bytes", err)
	}
	if _, err := readBundle(t.TempDir(), bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticBundleManualEvidenceChangeRetainsExportableAudit(t *testing.T) {
	service, id, _, _ := automaticBundleFixture(t, "complete")
	detail, _ := service.Detail(id)
	item := detail.Report.StallInvestigations[0]
	item.Media.Reason = "手动补充核对源流截图"
	if err := service.saveStallItem(id, item); err != nil {
		t.Fatal(err)
	}
	detail, _ = service.Detail(id)
	state := detail.Report.AutomaticInvestigation
	if state.State != "partial" || state.ModelState != "changed" || state.ReasonCode != "evidence_changed" || state.SummaryReportSHA256 != "" || detail.Explanation != nil {
		t.Fatal("manual evidence change retained an automatic success claim")
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal("manual evidence change made the archive unexportable", err)
	}
	if _, err := readBundle(t.TempDir(), bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticBundleProgressSnapshotSurvivesConcurrentAdvance(t *testing.T) {
	service, id, expected, _ := automaticBundleFixture(t, "waiting_player")
	writer := &blockingPlayerDownload{started: make(chan struct{}), resume: make(chan struct{})}
	download := make(chan error, 1)
	go func() { download <- service.WriteBundle(id, writer) }()
	select {
	case <-writer.started:
	case <-time.After(3 * time.Second):
		close(writer.resume)
		t.Fatal("automatic evidence download did not start")
	}
	detail, _ := service.Detail(id)
	report := *detail.Report
	next := expected
	now := time.Now().UTC()
	next.State, next.ModelState, next.ReasonCode = "unconfigured", "unconfigured", "model_unconfigured"
	next.PlayerArtifact, next.PlayerRevision = report.Manifest.Player, report.Player.Revision
	plan := planAutomaticInvestigation(report)
	next.CandidateCount, next.Selected = plan.CandidateCount, plan.Selected
	next.Selected[0].CaseID = report.StallInvestigations[0].ID
	next.CompletedAt, next.UpdatedAt = &now, now
	advanced := make(chan error, 1)
	go func() {
		service.persistence.Lock()
		defer service.persistence.Unlock()
		advanced <- persistAutomaticInvestigation(service.runDir(id), &report, next)
	}()
	select {
	case err := <-advanced:
		if err != nil {
			close(writer.resume)
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		close(writer.resume)
		t.Fatal("slow export blocked automatic progress persistence")
	}
	close(writer.resume)
	if err := <-download; err != nil {
		t.Fatal(err)
	}
	var captured AutomaticInvestigation
	_ = json.Unmarshal(reportExplanationZIPEntry(t, writer.Bytes(), automaticInvestigationName), &captured)
	before, _ := json.Marshal(expected)
	after, _ := json.Marshal(captured)
	if !bytes.Equal(before, after) {
		t.Fatal("download mixed old evidence with later automatic progress")
	}
	if _, err := readBundle(t.TempDir(), bytes.NewReader(writer.Bytes())); err != nil {
		t.Fatal(err)
	}
}
