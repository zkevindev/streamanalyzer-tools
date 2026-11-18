package diagnostic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type stallTestExplainer struct {
	call func(context.Context, Report) (Explanation, error)
	name string
}

func (m stallTestExplainer) Identity() ModelIdentity {
	return ModelIdentity{Provider: ProviderOpenAICompatible, Model: m.name}
}
func (m stallTestExplainer) Explain(ctx context.Context, report Report) (Explanation, error) {
	return m.call(ctx, report)
}

func openStallServiceTestRun(t *testing.T, model Explainer, realMedia bool) (*Service, string, string) {
	t.Helper()
	base := t.TempDir()
	service, err := Open(base, model)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	run := newRun("sample", ProtocolHTTPFLV, "http://offline.example")
	run.Status, run.ReportReady = "done", true
	dir := service.runDir(run.ID)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var data []byte
	if realMedia {
		_, _, path := investigationMediaFixture(t)
		data, err = os.ReadFile(path)
	} else {
		fixture, _ := stallTestFLV(t, false)
		data, err = os.ReadFile(filepath.Join(fixture, streamName))
	}
	if err != nil {
		t.Fatal(err)
	}
	parser, err := openFLV(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	var trace bytes.Buffer
	var lastArrival int64
	for {
		event, err := parser.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		event.ArrivalMS = max(lastArrival, event.DTS)
		lastArrival = event.ArrivalMS
		events = append(events, event)
		if err := json.NewEncoder(&trace).Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	manifest := Manifest{Version: multiProtocolSchemaVersion, Protocol: ProtocolHTTPFLV,
		Stream: investigationMediaSave(t, dir, streamName, data), Timeline: investigationMediaSave(t, dir, timelineName, trace.Bytes()),
		Observation: Observation{Origin: run.Origin, StartedAt: run.CreatedAt, ElapsedMS: 10000, RequestedSeconds: 10, EndReason: "eof"}}
	report := analyze(manifest, events)
	record := PlayerRecording{Version: 1, Revision: 1, StartedAt: run.CreatedAt, ElapsedMS: 9000, Complete: true, Engine: "mpegtsjs", Source: "same_capture",
		Events:  []PlayerEvent{{Kind: "start"}, {AtMS: 100, Kind: "playing"}, {AtMS: 120, Kind: "first_frame", Method: "video_frame_callback"}, {AtMS: 3500, Kind: "waiting"}, {AtMS: 5000, Kind: "playing"}, {AtMS: 9000, Kind: "stop"}},
		Samples: []PlayerSample{{AtMS: 100, CurrentMS: 0, BufferMS: 1000, ReadyState: 4}, {AtMS: 3500, CurrentMS: 500, BufferMS: 0, ReadyState: 2}, {AtMS: 5000, CurrentMS: 500, BufferMS: 3000, ReadyState: 4}, {AtMS: 8500, CurrentMS: 4000, BufferMS: 300, ReadyState: 3}}}
	if err := writePlayerRecording(dir, record); err != nil {
		t.Fatal(err)
	}
	if err := attachPlayerArtifact(dir, &report); err != nil {
		t.Fatal(err)
	}
	if err := persistReport(dir, report); err != nil {
		t.Fatal(err)
	}
	if err := service.insertRun(run); err != nil {
		t.Fatal(err)
	}
	return service, run.ID, base
}

func awaitStallTestItem(t *testing.T, service *Service, id string) StallInvestigation {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		detail, err := service.Detail(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(detail.Report.StallInvestigations) > 0 {
			item := detail.Report.StallInvestigations[0]
			if item.Status != "queued" && item.Status != "running" {
				return item
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("stall investigation did not finish")
	return StallInvestigation{}
}

func TestStallInvestigationServiceRealAssetsAndOfflineReplay(t *testing.T) {
	model := stallTestExplainer{name: "evidence-model", call: func(ctx context.Context, report Report) (Explanation, error) {
		if len(report.StallInvestigations) != 1 || len(report.Findings) == 0 || !strings.HasPrefix(report.Findings[0].ID, "S001-E") {
			t.Error("model did not receive selected stall evidence")
		}
		data, _ := modelInput(report)
		if !bytes.Contains(data, []byte(`"image_pixels_provided":false`)) || bytes.Contains(data, []byte("data:image")) {
			t.Error("model incorrectly received image pixels")
		}
		return Explanation{Provider: ProviderOpenAICompatible, Model: "evidence-model", Summary: "核对卡顿窗口的接收间隔与缓冲变化", Hypotheses: []Hypothesis{{Claim: "对照源端和当前采样点", EvidenceIDs: []string{report.Findings[0].ID}, NextCheck: "在相同时间段对比源端采样"}}}, nil
	}}
	service, id, _ := openStallServiceTestRun(t, model, true)
	diagnosticDetail, _ := service.Detail(id)
	if _, err := correlateStall(service.runDir(id), *diagnosticDetail.Report, "S001", 1); err != nil {
		t.Fatalf("fixture correlation: %v", err)
	}
	if _, err := service.InvestigateStall(id, StallInvestigationRequest{StallIndex: 0}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid stall accepted: %v", err)
	}
	created, err := service.InvestigateStall(id, StallInvestigationRequest{StallIndex: 1})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "S001" || created.Status != "queued" {
		t.Fatalf("incorrect initial state: %+v", created)
	}
	duplicate, err := service.InvestigateStall(id, StallInvestigationRequest{StallIndex: 1})
	if err != nil || duplicate.ID != created.ID {
		t.Fatalf("creation not idempotent: %+v %v", duplicate, err)
	}
	item := awaitStallTestItem(t, service, id)
	if item.Status != "complete" || item.Media.Status != "ready" || len(item.Media.Assets) != 4 || item.Explanation == nil || item.ModelState != "complete" {
		t.Fatalf("incomplete evidence: %+v", item)
	}
	for _, asset := range item.Media.Assets {
		if err := validateStallAssetFile(service.runDir(id), asset); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Report.Manifest.Version != investigationSchemaVersion || detail.Report.Manifest.StallEvidence == nil {
		t.Fatal("report did not declare supplemental evidence")
	}
	var html bytes.Buffer
	if err := RenderHTMLWithAssets(&html, *detail.Report, nil, "", service.StallImages(id, *detail.Report)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(html.Bytes(), []byte("data:image/jpeg;base64,")) || !bytes.Contains(html.Bytes(), []byte(item.Explanation.Summary)) {
		t.Fatal("standalone report omitted images or advice")
	}
	var bundle bytes.Buffer
	if err := service.WriteBundle(id, &bundle); err != nil {
		t.Fatal(err)
	}
	replay, err := service.Import(bytes.NewReader(bundle.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Detail(replay.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(detail.Report, got.Report) {
		a, _ := json.Marshal(detail.Report)
		b, _ := json.Marshal(got.Report)
		t.Fatalf("offline stall evidence changed\nbefore:%s\nafter:%s", a, b)
	}
	for _, asset := range item.Media.Assets {
		file, err := service.OpenStallAsset(replay.ID, item.ID, filepath.Base(asset.Name))
		if err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
}

func TestStallInvestigationCancelAndRecovery(t *testing.T) {
	service, id, base := openStallServiceTestRun(t, nil, false)
	service.investigationSlots <- struct{}{}
	service.investigationSlots <- struct{}{}
	item, err := service.InvestigateStall(id, StallInvestigationRequest{StallIndex: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.StopStall(id, item.ID); err != nil {
		t.Fatal(err)
	}
	if got := awaitStallTestItem(t, service, id); got.Status != "canceled" {
		t.Fatalf("cancel not recorded: %+v", got)
	}
	<-service.investigationSlots
	<-service.investigationSlots
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	detail.Report.StallInvestigations[0].Status = "running"
	detail.Report.StallInvestigations[0].ModelState = "pending"
	if err := persistStallReport(service.runDir(id), detail.Report); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := reopened.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Report.StallInvestigations[0].Status != "canceled" {
		t.Fatal("restart left a permanently running task")
	}
}

func TestStallInvestigationRejectsModifiedOriginalAndModelChange(t *testing.T) {
	service, id, _ := openStallServiceTestRun(t, nil, false)
	if err := os.WriteFile(filepath.Join(service.runDir(id), streamName), []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InvestigateStall(id, StallInvestigationRequest{StallIndex: 1}); err != nil {
		t.Fatal(err)
	}
	if got := awaitStallTestItem(t, service, id); got.Status != "failed" || !strings.Contains(got.ModelError, "SHA-256") {
		t.Fatalf("changed source accepted: %+v", got)
	}
	service2, id2, _ := openStallServiceTestRun(t, nil, false)
	if _, err := service2.InvestigateStall(id2, StallInvestigationRequest{StallIndex: 1}); err != nil {
		t.Fatal(err)
	}
	awaitStallTestItem(t, service2, id2)
	started := make(chan struct{})
	service2.SetModel(stallTestExplainer{name: "old", call: func(ctx context.Context, _ Report) (Explanation, error) {
		close(started)
		<-ctx.Done()
		return Explanation{Summary: "stale"}, ctx.Err()
	}})
	result := make(chan StallInvestigation, 1)
	go func() { item, _ := service2.ExplainStall(t.Context(), id2, "S001"); result <- item }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("model not started")
	}
	service2.SetModel(stallTestExplainer{name: "new", call: func(context.Context, Report) (Explanation, error) {
		return Explanation{Provider: ProviderOpenAICompatible, Model: "new", Summary: "current"}, nil
	}})
	select {
	case item := <-result:
		if item.Explanation != nil || item.ModelState != "changed" {
			t.Fatalf("stale model response saved: %+v", item)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("model switch did not cancel")
	}
	item, err := service2.ExplainStall(t.Context(), id2, "S001")
	if err != nil || item.Explanation == nil || item.Explanation.Summary != "current" {
		t.Fatalf("current model retry failed: %+v %v", item, err)
	}
}

func TestStallInvestigationUnavailableMediaRetryAndPendingImport(t *testing.T) {
	service, id, _ := openStallServiceTestRun(t, nil, true)
	detail, _ := service.Detail(id)
	correlation, err := correlateStall(service.runDir(id), *detail.Report, "S001", 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	detail.Report.StallInvestigations = []StallInvestigation{{ID: "S001", StallIndex: 1, PlayerRevision: detail.Report.Player.Revision, Status: "partial", CreatedAt: now, UpdatedAt: now,
		Stall: detail.Report.Player.Stalls[0], Correlation: correlation, Media: InvestigationMedia{Status: "unavailable", Reason: "未找到 FFmpeg", Assets: []DerivedArtifact{}}, ModelState: "unconfigured"}}
	if err := persistStallReport(service.runDir(id), detail.Report); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal(err)
	}
	imported, err := service.Import(bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.InvestigateStall(imported.ID, StallInvestigationRequest{StallIndex: 1, Retry: true}); err != nil {
		t.Fatal(err)
	}
	item := awaitStallTestItem(t, service, imported.ID)
	if item.Media.Status != "ready" || len(item.Media.Assets) != 4 || item.ID != "S001" {
		t.Fatalf("offline media retry failed: %+v", item)
	}
	queuedService, queuedID, _ := openStallServiceTestRun(t, nil, false)
	queuedService.investigationSlots <- struct{}{}
	queuedService.investigationSlots <- struct{}{}
	defer func() { <-queuedService.investigationSlots; <-queuedService.investigationSlots }()
	if _, err := queuedService.InvestigateStall(queuedID, StallInvestigationRequest{StallIndex: 1}); err != nil {
		t.Fatal(err)
	}
	var pending bytes.Buffer
	if err := queuedService.WriteBundle(queuedID, &pending); err != nil {
		t.Fatal(err)
	}
	replay, err := queuedService.Import(bytes.NewReader(pending.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := queuedService.Detail(replay.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Report.StallInvestigations[0].Status != "canceled" {
		t.Fatal("import left an unfinished task permanently running")
	}
	if err := queuedService.StopStall(queuedID, "S001"); err != nil {
		t.Fatal(err)
	}
	awaitStallTestItem(t, queuedService, queuedID)
}

func TestStallInvestigationInvalidatesInflightGlobalExplanation(t *testing.T) {
	started, resume := make(chan struct{}), make(chan struct{})
	var startedOnce sync.Once
	model := stallTestExplainer{name: "blocked", call: func(ctx context.Context, report Report) (Explanation, error) {
		startedOnce.Do(func() { close(started) })
		select {
		case <-resume:
			return Explanation{Provider: ProviderOpenAICompatible, Model: "blocked", Summary: "old evidence"}, nil
		case <-ctx.Done():
			return Explanation{}, ctx.Err()
		}
	}}
	service, id, _ := openStallServiceTestRun(t, model, false)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := service.Explain(ctx, id); result <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := service.InvestigateStall(id, StallInvestigationRequest{StallIndex: 1}); err != nil {
		close(resume)
		t.Fatal(err)
	}
	close(resume)
	select {
	case err := <-result:
		if !errors.Is(err, ErrReportChanged) {
			t.Fatalf("old global model result saved after evidence changed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	awaitStallTestItem(t, service, id)
}
