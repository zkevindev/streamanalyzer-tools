package diagnostic

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type automaticTestModel struct {
	calls   atomic.Int32
	cases   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (m *automaticTestModel) Identity() ModelIdentity {
	return ModelIdentity{Provider: ProviderOpenAICompatible, Model: "automatic-test"}
}
func (m *automaticTestModel) Explain(ctx context.Context, report Report) (Explanation, error) {
	m.calls.Add(1)
	if report.modelScope == "case" {
		m.cases.Add(1)
	}
	if m.started != nil {
		close(m.started)
		<-m.release
	}
	return Explanation{Provider: ProviderOpenAICompatible, Model: "automatic-test", Summary: "已统一核对采样与重点异常证据"}, nil
}
func (m *automaticTestModel) SupportsImages() bool { return true }
func (m *automaticTestModel) ExplainWithImages(ctx context.Context, report Report, images []ModelImage) (Explanation, error) {
	if err := validateModelImagesForReport(report, images); err != nil {
		return Explanation{}, err
	}
	return m.Explain(ctx, report)
}
func automaticServiceFixture(t *testing.T, model Explainer, media bool) (*Service, string, string) {
	t.Helper()
	service, id, base := openStallServiceTestRun(t, model, media)
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	report := *detail.Report
	if err := writeAutomaticRequest(service.runDir(id), detail.CreatedAt); err != nil {
		t.Fatal(err)
	}
	service.persistence.Lock()
	err = persistAutomaticInvestigation(service.runDir(id), &report, initialAutomaticInvestigation(report, detail.CreatedAt))
	service.persistence.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return service, id, base
}
func awaitAutomaticService(t *testing.T, service *Service, id string) Detail {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last Detail
	for time.Now().Before(deadline) {
		detail, err := service.Detail(id)
		if err != nil {
			t.Fatal(err)
		}
		last = detail
		if detail.Report != nil && detail.Report.AutomaticInvestigation != nil && automaticTerminal(detail.Report.AutomaticInvestigation.State) && !detail.AutomaticRunning {
			return detail
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("automatic job stuck: %+v", last.Report.AutomaticInvestigation)
	return Detail{}
}
func TestAutomaticServiceMediaOnlyOneReportAndDuplicateNotifications(t *testing.T) {
	model := &automaticTestModel{}
	service, id, _ := automaticServiceFixture(t, model, true)
	for range 8 {
		go service.maybeStartAutomaticInvestigation(id)
	}
	detail := awaitAutomaticService(t, service, id)
	state := detail.Report.AutomaticInvestigation
	if state.State != "complete" || state.ModelState != "complete" || len(state.Selected) != 1 || state.Selected[0].CaseID != "S001" || state.SummaryReportSHA256 == "" || model.calls.Load() != 1 || model.cases.Load() != 0 {
		t.Fatalf("automatic workflow repeated or skipped whole summary: %+v calls%d cases%d", state, model.calls.Load(), model.cases.Load())
	}
	item := detail.Report.StallInvestigations[0]
	if item.ModelState != "not_requested" || item.Explanation != nil || len(item.Media.Assets) != 4 {
		t.Fatalf("case called model or lost media: %+v", item)
	}
	for range 5 {
		service.maybeStartAutomaticInvestigation(id)
	}
	time.Sleep(30 * time.Millisecond)
	if model.calls.Load() != 1 {
		t.Fatal("terminal completion notification reran model")
	}
	var bundle bytes.Buffer
	if err := service.WriteBundle(id, &bundle); err != nil {
		t.Fatal(err)
	}
	replay, err := readBundle(t.TempDir(), bytes.NewReader(bundle.Bytes()))
	if err != nil || !reflect.DeepEqual(*detail.Report, replay) {
		t.Fatalf("automatic result failed exact offline import: %v", err)
	}
	// A later manual edit invalidates the cached whole report and its automatic
	// completed-summary claim in the same commit, without losing the assets.
	item.ModelError = "手动更新案例状态"
	if err := service.saveStallItem(id, item); err != nil {
		t.Fatal(err)
	}
	changed, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Explanation != nil || changed.Report.AutomaticInvestigation.ModelState != "changed" || changed.Report.AutomaticInvestigation.SummaryReportSHA256 != "" {
		t.Fatal("manual edit left completed summary proof")
	}
	bundle.Reset()
	if err := service.WriteBundle(id, &bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := readBundle(t.TempDir(), bytes.NewReader(bundle.Bytes())); err != nil {
		t.Fatalf("manual post-auto update broke bundle: %v", err)
	}
}
func TestAutomaticServiceUnconfiguredRetryReusesMedia(t *testing.T) {
	service, id, _ := automaticServiceFixture(t, nil, true)
	service.maybeStartAutomaticInvestigation(id)
	first := awaitAutomaticService(t, service, id)
	if first.Report.AutomaticInvestigation.State != "unconfigured" || len(first.Report.StallInvestigations) != 1 {
		t.Fatal(first.Report.AutomaticInvestigation)
	}
	var times []time.Time
	for _, asset := range first.Report.StallInvestigations[0].Media.Assets {
		info, err := os.Stat(filepath.Join(service.runDir(id), asset.Name))
		if err != nil {
			t.Fatal(err)
		}
		times = append(times, info.ModTime())
	}
	model := &automaticTestModel{}
	service.SetModel(model)
	if _, err := service.RetryAutomaticInvestigation(id); err != nil {
		t.Fatal(err)
	}
	second := awaitAutomaticService(t, service, id)
	if second.Report.AutomaticInvestigation.ModelState != "complete" || len(second.Report.StallInvestigations) != 1 || model.calls.Load() != 1 || model.cases.Load() != 0 {
		t.Fatal("retry did not reuse case/one global call")
	}
	for index, asset := range second.Report.StallInvestigations[0].Media.Assets {
		info, err := os.Stat(filepath.Join(service.runDir(id), asset.Name))
		if err != nil || !info.ModTime().Equal(times[index]) {
			t.Fatal("retry extracted existing asset again", err)
		}
	}
}
func TestAutomaticServiceWaitTimeoutStopCapacityAndBoundRetry(t *testing.T) {
	for _, mode := range []string{"timeout", "stop", "capacity", "bound_retry"} {
		t.Run(mode, func(t *testing.T) {
			model := &automaticTestModel{}
			service, id, _ := automaticServiceFixture(t, model, false)
			if mode == "capacity" {
				service.mu.Lock()
				for index := 0; index < maxAutomaticJobs; index++ {
					_, cancel := context.WithCancel(t.Context())
					service.automaticJobs[string(rune('a'+index))] = &automaticInvestigationJob{cancel: cancel, wake: make(chan struct{}, 1)}
				}
				service.mu.Unlock()
				t.Cleanup(func() {
					service.mu.Lock()
					for key, job := range service.automaticJobs {
						job.cancel()
						delete(service.automaticJobs, key)
					}
					service.mu.Unlock()
				})
				service.maybeStartAutomaticInvestigation(id)
				detail := awaitAutomaticService(t, service, id)
				if detail.Report.AutomaticInvestigation.ReasonCode != "worker_limit" || detail.Report.AutomaticInvestigation.ModelState != "not_requested" {
					t.Fatal(detail.Report.AutomaticInvestigation)
				}
				return
			}
			record, err := readPlayerRecording(service.runDir(id))
			if err != nil {
				t.Fatal(err)
			}
			record.Complete = false
			record.Revision++
			if err := writePlayerRecording(service.runDir(id), record); err != nil {
				t.Fatal(err)
			}
			service.persistence.Lock()
			pendingReport, attachErr := readJSONFile[Report](service.runDir(id), reportName)
			if attachErr == nil {
				attachErr = attachPlayerArtifact(service.runDir(id), &pendingReport)
			}
			if attachErr == nil {
				attachErr = persistReport(service.runDir(id), pendingReport)
			}
			service.persistence.Unlock()
			if attachErr != nil {
				t.Fatal(attachErr)
			}
			if mode == "bound_retry" {
				service.persistence.Lock()
				report, err := readJSONFile[Report](service.runDir(id), reportName)
				if err == nil {
					state := *report.AutomaticInvestigation
					state.PlayerRevision = report.Player.Revision
					state.PlayerArtifact = report.Manifest.Player
					state.State = "interrupted"
					err = persistAutomaticInvestigation(service.runDir(id), &report, state)
				}
				service.persistence.Unlock()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := service.RetryAutomaticInvestigation(id); !errors.Is(err, ErrReportChanged) {
					t.Fatal("bound incomplete retry became waiting", err)
				}
				return
			}
			service.automaticPlayerWait = 30 * time.Millisecond
			service.maybeStartAutomaticInvestigation(id)
			if mode == "stop" {
				if err := service.StopAutomaticInvestigation(id); err != nil {
					t.Fatal(err)
				}
			}
			detail := awaitAutomaticService(t, service, id)
			want := "player_missing"
			if mode == "stop" {
				want = "canceled"
			}
			state := detail.Report.AutomaticInvestigation
			if state.ReasonCode != want || model.calls.Load() != 0 || state.CompletedAt == nil || state.CompletedAt.After(state.UpdatedAt) {
				t.Fatalf("waiting lifecycle invalid: %+v", state)
			}
		})
	}
}
func TestAutomaticServiceRecoveryPreservesReplayAndInterruptsLocalOnly(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "imported"}[replay], func(t *testing.T) {
			service, id, base := automaticServiceFixture(t, nil, false)
			before, err := service.Detail(id)
			if err != nil {
				t.Fatal(err)
			}
			artifact := *before.Report.Manifest.AutomaticInvestigation
			if replay {
				if _, err := service.db.Exec("UPDATE runs SET mode='replay' WHERE id=?", id); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(service.runDir(id), automaticRequestName)); err != nil {
					t.Fatal(err)
				}
			}
			if err := service.Close(); err != nil {
				t.Fatal(err)
			}
			model := &automaticTestModel{}
			reopened, err := Open(base, model)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			detail, err := reopened.Detail(id)
			if err != nil {
				t.Fatal(err)
			}
			state := detail.Report.AutomaticInvestigation
			if replay {
				if state.State != "waiting_player" || *detail.Report.Manifest.AutomaticInvestigation != artifact {
					t.Fatal("imported pending state rewritten")
				}
			} else if state.State != "interrupted" || state.OriginalState != "waiting_player" || state.CompletedAt.After(state.UpdatedAt) {
				t.Fatal("local pending not safely interrupted")
			}
			if detail.AutomaticRunning || model.calls.Load() != 0 {
				t.Fatal("open resumed automatic model/network work")
			}
		})
	}
}
func TestAutomaticServiceModelChangeAndPartialMedia(t *testing.T) {
	service, id, _ := automaticServiceFixture(t, nil, false)
	model := &automaticTestModel{started: make(chan struct{}), release: make(chan struct{})}
	service.SetModel(model)
	service.maybeStartAutomaticInvestigation(id)
	select {
	case <-model.started:
	case <-time.After(10 * time.Second):
		t.Fatal("summary did not start after partial media")
	}
	service.SetModel(&automaticTestModel{})
	close(model.release)
	detail := awaitAutomaticService(t, service, id)
	if detail.Report.AutomaticInvestigation.ModelState != "changed" || detail.Report.AutomaticInvestigation.ReasonCode != "model_changed" || detail.Explanation != nil || model.calls.Load() != 1 || model.cases.Load() != 0 {
		t.Fatal("model change saved stale automatic summary")
	}
}

func TestAutomaticServiceStartHooksBothCompletionOrdersAndOptIn(t *testing.T) {
	_, _, source := investigationMediaFixture(t)
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range []string{"player_first", "capture_first", "canceled_before_report"} {
		t.Run(order, func(t *testing.T) {
			gate := make(chan struct{})
			var requests atomic.Int32
			var ended atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				<-gate
				time.Sleep(150 * time.Millisecond)
				w.Header().Set("Content-Type", "video/x-flv")
				_, _ = w.Write(data)
				ended.Store(true)
			}))
			defer server.Close()
			model := &automaticTestModel{}
			service, err := Open(t.TempDir(), model)
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			if _, err := service.Start(SampleRequest{URL: server.URL, AutoInvestigate: true}); !errors.Is(err, ErrInvalid) {
				t.Fatal("automatic request accepted without Playback", err)
			}
			run, err := service.Start(SampleRequest{URL: server.URL, Protocol: ProtocolHTTPFLV, Seconds: 1, Playback: true, AutoInvestigate: true})
			if err != nil {
				t.Fatal(err)
			}
			record := PlayerRecording{Version: 1, Revision: 1, StartedAt: run.CreatedAt, ElapsedMS: 900, Complete: true, Engine: "mpegtsjs", Source: "same_capture", Events: []PlayerEvent{{Kind: "start"}, {AtMS: 10, Kind: "playing"}, {AtMS: 20, Kind: "first_frame", Method: "video_frame_callback"}, {AtMS: 50, Kind: "waiting"}, {AtMS: 650, Kind: "playing"}, {AtMS: 900, Kind: "stop"}}, Samples: []PlayerSample{{AtMS: 30, CurrentMS: 500, BufferMS: 200, ReadyState: 4}, {AtMS: 50, CurrentMS: 500, BufferMS: 0, ReadyState: 2}, {AtMS: 650, CurrentMS: 500, BufferMS: 1000, ReadyState: 4}}}
			if order == "player_first" {
				if _, err := service.SavePlayerRecording(run.ID, run.Player.Token, record); err != nil {
					t.Fatal(err)
				}
			} else {
				session, err := service.PlayerSession(run.ID, run.Player.Token)
				if err != nil {
					t.Fatal(err)
				}
				_, live, stop, err := session.Subscribe()
				if err != nil {
					t.Fatal(err)
				}
				defer stop()
				go func() {
					for range live {
					}
				}()
			}
			if order == "canceled_before_report" {
				if err := service.StopAutomaticInvestigation(run.ID); err != nil {
					t.Fatal(err)
				}
			}
			close(gate)
			deadline := time.Now().Add(5 * time.Second)
			for {
				detail, err := service.Detail(run.ID)
				if err != nil {
					t.Fatal(err)
				}
				if detail.ReportReady {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("capture did not stop")
				}
				time.Sleep(10 * time.Millisecond)
			}
			server.Close()
			if !ended.Load() {
				t.Fatal("source still running before analysis")
			}
			if order != "player_first" {
				if _, err := service.SavePlayerRecording(run.ID, run.Player.Token, record); err != nil {
					t.Fatal(err)
				}
			}
			detail := awaitAutomaticService(t, service, run.ID)
			if order == "canceled_before_report" {
				if detail.Report.AutomaticInvestigation.State != "canceled" || model.calls.Load() != 0 {
					t.Fatal("canceled intent restarted after capture")
				}
			} else if detail.Report.AutomaticInvestigation.ModelState != "complete" || model.calls.Load() != 1 || model.cases.Load() != 0 {
				t.Fatalf("completion order failed: %+v calls%d", detail.Report.AutomaticInvestigation, model.calls.Load())
			}
			if requests.Load() != 1 {
				t.Fatalf("auto analysis pulled source again: %d", requests.Load())
			}
		})
	}
}

func automaticIncompletePlayer(t *testing.T, service *Service, id string) {
	t.Helper()
	record, err := readPlayerRecording(service.runDir(id))
	if err != nil {
		t.Fatal(err)
	}
	record.Revision++
	record.Complete = false
	if err := writePlayerRecording(service.runDir(id), record); err != nil {
		t.Fatal(err)
	}
	service.persistence.Lock()
	report, err := readJSONFile[Report](service.runDir(id), reportName)
	if err == nil {
		err = attachPlayerArtifact(service.runDir(id), &report)
	}
	if err == nil {
		err = persistReport(service.runDir(id), report)
	}
	service.persistence.Unlock()
	if err != nil {
		t.Fatal(err)
	}
}
func TestAutomaticServiceBusyPartialMediaAndClose(t *testing.T) {
	for _, mode := range []string{"busy", "partial_media", "close"} {
		t.Run(mode, func(t *testing.T) {
			model := &automaticTestModel{}
			service, id, base := automaticServiceFixture(t, model, false)
			if mode == "close" {
				automaticIncompletePlayer(t, service, id)
				service.maybeStartAutomaticInvestigation(id)
				if err := service.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := Open(base, model)
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				detail, err := reopened.Detail(id)
				if err != nil {
					t.Fatal(err)
				}
				state := detail.Report.AutomaticInvestigation
				if state.State != "interrupted" || state.OriginalState != "waiting_player" || detail.AutomaticRunning || model.calls.Load() != 0 {
					t.Fatal("close resumed or lost interruption state")
				}
				return
			}
			if mode == "busy" {
				service.investigationModel.Lock()
				defer service.investigationModel.Unlock()
			}
			service.maybeStartAutomaticInvestigation(id)
			detail := awaitAutomaticService(t, service, id)
			state := detail.Report.AutomaticInvestigation
			if mode == "busy" {
				if state.ReasonCode != "model_failed" || model.calls.Load() != 0 {
					t.Fatal("busy model spun/retried")
				}
			} else if state.State != "partial" || state.ModelState != "complete" || state.ReasonCode != "media_partial" || model.calls.Load() != 1 || model.cases.Load() != 0 {
				t.Fatalf("partial media blocked numeric global summary: %+v", state)
			}
		})
	}
}
func TestAutomaticServiceImportedExplicitRetryOnly(t *testing.T) {
	service, id, _ := automaticServiceFixture(t, nil, false)
	if _, err := service.db.Exec("UPDATE runs SET mode='replay' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(service.runDir(id), automaticRequestName)); err != nil {
		t.Fatal(err)
	}
	model := &automaticTestModel{}
	service.SetModel(model)
	service.maybeStartAutomaticInvestigation(id)
	time.Sleep(30 * time.Millisecond)
	if model.calls.Load() != 0 {
		t.Fatal("import-like notification started work without opt-in")
	}
	if _, err := service.RetryAutomaticInvestigation(id); err != nil {
		t.Fatal(err)
	}
	detail := awaitAutomaticService(t, service, id)
	if detail.Report.AutomaticInvestigation.ModelState != "complete" || model.calls.Load() != 1 || model.cases.Load() != 0 {
		t.Fatal("explicit local replay retry did not perform one report summary")
	}
}
func TestAutomaticServiceRecoverySkipsUnreadableUnrelatedHistory(t *testing.T) {
	service, id, base := automaticServiceFixture(t, nil, false)
	path := filepath.Join(service.runDir(id), reportName)
	bad := []byte("{damaged historical report")
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	model := &automaticTestModel{}
	reopened, err := Open(base, model)
	if err != nil {
		t.Fatalf("unreadable history prevents service startup: %v", err)
	}
	defer reopened.Close()
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, bad) || model.calls.Load() != 0 {
		t.Fatal("recovery rewrote unreadable history or started model")
	}
}

func TestAutomaticServiceSummaryFinalizationRejectsPostSaveManualChange(t *testing.T) {
	model := &automaticTestModel{}
	service, id, _ := automaticServiceFixture(t, model, true)
	service.maybeStartAutomaticInvestigation(id)
	detail := awaitAutomaticService(t, service, id)
	if detail.Explanation == nil {
		t.Fatal("missing saved summary")
	}
	answer := *detail.Explanation
	if err := service.updateAutomaticInvestigation(id, func(state *AutomaticInvestigation) {
		state.State = "summarizing"
		state.ModelState = "pending"
		state.SummaryReportSHA256 = ""
		state.CompletedAt = nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	job := &automaticInvestigationJob{cancel: cancel, wake: make(chan struct{}, 1)}
	service.mu.Lock()
	service.automaticJobs[id] = job
	service.mu.Unlock()
	defer func() { service.mu.Lock(); delete(service.automaticJobs, id); service.mu.Unlock() }()
	item := detail.Report.StallInvestigations[0]
	item.ModelError = "统一总结保存之后，用户更新了案例"
	if err := service.saveStallItem(id, item); err != nil {
		t.Fatal(err)
	}
	if err := service.finalizeAutomaticSummary(ctx, id, job, answer, false, "", ""); !errors.Is(err, ErrReportChanged) {
		t.Fatalf("post-save stale answer committed: %v", err)
	}
	current, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	state := current.Report.AutomaticInvestigation
	if state.State != "partial" || state.ModelState != "changed" || state.ReasonCode != "evidence_changed" || state.SummaryReportSHA256 != "" || current.Explanation != nil {
		t.Fatalf("stale automatic completion persisted: %+v", state)
	}
	var bundle bytes.Buffer
	if err := service.WriteBundle(id, &bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := readBundle(t.TempDir(), bytes.NewReader(bundle.Bytes())); err != nil {
		t.Fatalf("post-save change made bundle unusable: %v", err)
	}
	// Cancellation may arrive while completion is waiting for the disk lock.
	if err := service.updateAutomaticInvestigation(id, func(state *AutomaticInvestigation) {
		state.State = "summarizing"
		state.ModelState = "pending"
		state.CompletedAt = nil
	}); err != nil {
		t.Fatal(err)
	}
	service.persistence.Lock()
	done := make(chan error, 1)
	go func() { done <- service.finalizeAutomaticSummary(ctx, id, job, answer, false, "", "") }()
	cancel()
	service.persistence.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel waiting for commit was ignored: %v", err)
	}
}
