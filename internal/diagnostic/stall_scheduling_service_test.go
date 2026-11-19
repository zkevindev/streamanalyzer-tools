package diagnostic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestSchedulingCaseModelAndOfflineReplayRemainScoped(t *testing.T) {
	model := stallTestExplainer{name: "scheduling-model", call: func(_ context.Context, report Report) (Explanation, error) {
		if len(report.StallInvestigations) != 1 || report.Player.Scheduling == nil || !reflect.DeepEqual(report.Player.Scheduling, report.StallInvestigations[0].Correlation.Scheduling) {
			t.Error("case model received unrelated global scheduling data")
			return Explanation{}, ErrProvider
		}
		input, err := modelInput(report)
		if err != nil {
			t.Error(err)
			return Explanation{}, ErrProvider
		}
		var decoded struct {
			Player struct {
				Scheduling *modelPlayerScheduling `json:"scheduling"`
			} `json:"player"`
		}
		if err := json.Unmarshal(input, &decoded); err != nil || decoded.Player.Scheduling == nil || decoded.Player.Scheduling.LongTasks.TotalMS != 2000 {
			t.Errorf("wrong scheduling model metrics: %v", err)
			return Explanation{}, ErrProvider
		}
		for _, finding := range report.Findings {
			if finding.Code == "stall_page_long_tasks" {
				return Explanation{Provider: ProviderOpenAICompatible, Model: "scheduling-model", Summary: "对照主线程与媒体线程", Hypotheses: []Hypothesis{{Claim: "核对页面调度", EvidenceIDs: []string{finding.ID}, NextCheck: "录制浏览器性能"}}}, nil
			}
		}
		t.Error("case model missing scheduling evidence ID")
		return Explanation{}, ErrProvider
	}}
	service, id, _ := openStallServiceTestRun(t, model, false)
	dir := service.runDir(id)
	record, err := readPlayerRecording(dir)
	if err != nil {
		t.Fatal(err)
	}
	record.Version = 3
	record.Revision++
	record.Scheduling = &PlayerSchedulingRecording{LongTaskSupport: "supported", ObservationStartMS: 0, ObservationEndMS: record.ElapsedMS,
		LongTasks: []PlayerSchedulingInterval{{1000, 1800}, {3600, 4800}, {8500, 8800}}, TimerDelays: []PlayerSchedulingInterval{{3650, 4750}}}
	if err := writePlayerRecording(dir, record); err != nil {
		t.Fatal(err)
	}
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	report := *detail.Report
	if err := attachPlayerArtifact(dir, &report); err != nil {
		t.Fatal(err)
	}
	if err := persistReport(dir, report); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InvestigateStall(id, StallInvestigationRequest{StallIndex: 1}); err != nil {
		t.Fatal(err)
	}
	item := awaitStallTestItem(t, service, id)
	if item.ModelState != "complete" || item.Explanation == nil || assessmentDirection(item.Assessment, "page_scheduling") == nil || item.Correlation.Scheduling.LongTasks.Count != 2 {
		t.Fatalf("scheduling case failed: %+v", item)
	}
	// The before-stall task is visible as context but only selected overlap
	// supports the scheduling finding/candidate and the model's cited ID.
	for _, finding := range item.Correlation.Findings {
		if finding.Code == "stall_page_long_tasks" && (finding.StartMS != 3600 || finding.EndMS != 4800) {
			t.Fatal("outside-stall task entered scoped scheduling finding")
		}
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
	saved, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Report, saved.Report) {
		t.Fatal("offline scheduling report changed")
	}
	for _, modify := range []func(*investigationLedger){
		func(ledger *investigationLedger) { ledger.Items[0].Correlation.Scheduling.ActiveCoverageMS++ },
		func(ledger *investigationLedger) { ledger.Items[0].Assessment.Version = "stall-assessment/2" },
	} {
		altered := assessmentRewriteBundle(t, bundle.Bytes(), modify)
		if _, err := service.Import(bytes.NewReader(altered)); !errors.Is(err, ErrInvalid) {
			t.Fatal("modified scheduling correlation/rule version survived valid checksum")
		}
	}
}
