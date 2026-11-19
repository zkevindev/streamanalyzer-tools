package diagnostic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPresentationWindowKeepsWholeEndpointsAndSumsOverlap(t *testing.T) {
	count, total, largest := 2, int64(6000), int64(4000)
	source := &PlayerPresentationReport{Support: "supported", ObservationStartMS: 0, ObservationEndMS: 10000, ActiveCoverageMS: 10000, ActiveIntervals: []PlayerSchedulingInterval{{0, 10000}}, Count: &count, TotalMS: &total, MaxMS: &largest,
		Gaps: []PlayerPresentationGapReport{{StartMS: 1000, EndMS: 5000, DurationMS: 4000, ExpectedFrameMS: 40}, {StartMS: 7000, EndMS: 9000, DurationMS: 2000, ExpectedFrameMS: 40}}}
	before, _ := json.Marshal(source)
	window := presentationWindow(source, 3000, 8000)
	if window.Count == nil || *window.Count != 2 || *window.TotalMS != 3000 || *window.MaxMS != 2000 || window.ActiveCoverageMS != 5000 || window.Gaps[0].StartMS != 1000 || window.Gaps[1].EndMS != 9000 {
		t.Fatalf("invalid presentation window: %+v", window)
	}
	after, _ := json.Marshal(source)
	if !bytes.Equal(before, after) {
		t.Fatal("window mutated source callback intervals")
	}
	unknown := presentationWindow(&PlayerPresentationReport{Support: "unsupported", ObservationStartMS: 0, ObservationEndMS: 1000}, 0, 1000)
	if unknown.Count != nil {
		t.Fatal("unsupported callbacks became measured zero")
	}
}

func TestPresentationGapCanBeInvestigatedWithoutWaitingAndReplayed(t *testing.T) {
	model := stallTestExplainer{name: "presentation-model", call: func(_ context.Context, report Report) (Explanation, error) {
		if report.Player.StallCount != 0 || len(report.Player.Stalls) != 0 || len(report.StallInvestigations) != 1 || report.StallInvestigations[0].IntervalKind != IntervalPresentationGap {
			t.Error("callback interval synthesized a buffering stall")
			return Explanation{}, ErrProvider
		}
		if report.Player.Presentation == nil || report.Player.Presentation.Count == nil || *report.Player.Presentation.Count != 1 {
			t.Error("selected presentation metrics absent")
			return Explanation{}, ErrProvider
		}
		for _, finding := range report.Findings {
			if finding.Code == "presentation_gap_selected" {
				return Explanation{Provider: ProviderOpenAICompatible, Model: "presentation-model", Summary: "核对回调间隔与页面调度", Hypotheses: []Hypothesis{{Claim: "对照帧率与调度", EvidenceIDs: []string{finding.ID}, NextCheck: "录制客户端媒体与性能日志"}}}, nil
			}
		}
		t.Error("model missing selected callback-gap evidence")
		return Explanation{}, ErrProvider
	}}
	service, id, _ := openStallServiceTestRun(t, model, false)
	dir := service.runDir(id)
	record, err := readPlayerRecording(dir)
	if err != nil {
		t.Fatal(err)
	}
	record.Version = 4
	record.Revision++
	record.Events = []PlayerEvent{{Kind: "start"}, {AtMS: 100, Kind: "playing"}, {AtMS: 120, Kind: "first_frame", Method: "video_frame_callback"}, {AtMS: 9000, Kind: "stop"}}
	ready := int64(500)
	record.Presentation = &PlayerPresentationRecording{Support: "supported", ObservationStartMS: 0, ObservationEndMS: 9000, ReadyAtMS: &ready, Gaps: []PlayerPresentationGap{{StartMS: 3500, EndMS: 5000, ExpectedFrameMS: 40}}}
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
	if report.Player.StallCount != 0 || len(report.Player.Presentation.Gaps) != 1 {
		t.Fatal("gap was lost or counted as buffering")
	}
	if err := persistReport(dir, report); err != nil {
		t.Fatal(err)
	}
	created, err := service.InvestigateStall(id, StallInvestigationRequest{IntervalKind: IntervalPresentationGap, IntervalIndex: 1})
	if err != nil {
		t.Fatal(err)
	}
	item := awaitStallTestItem(t, service, id)
	if item.ID != created.ID || item.IntervalKind != IntervalPresentationGap || item.ModelState != "complete" || assessmentDirection(item.Assessment, "presentation_callback_gap") == nil {
		t.Fatalf("presentation investigation failed: %+v", item)
	}
	if strings.Contains(item.Assessment.Conclusion, "画面已经冻结") {
		t.Fatal("callback interval claimed image-content proof")
	}
	for _, version := range []string{"stall-assessment/1", "stall-assessment/2", "stall-assessment/3", "stall-assessment/4"} {
		if _, err := recomputeStallAssessment(item, version); !errors.Is(err, ErrInvalid) {
			t.Fatal("callback evidence downgraded to a legacy rule")
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
		t.Fatal("callback investigation changed during offline replay")
	}
	for _, modify := range []func(*investigationLedger){func(ledger *investigationLedger) { ledger.Items[0].IntervalKind = IntervalBuffering }, func(ledger *investigationLedger) { ledger.Items[0].Correlation.Presentation.Gaps[0].ExpectedFrameMS++ }} {
		changed := assessmentRewriteBundle(t, bundle.Bytes(), modify)
		if _, err := service.Import(bytes.NewReader(changed)); !errors.Is(err, ErrInvalid) {
			t.Fatal("resigned interval kind/presentation evidence tampering accepted")
		}
	}
}

func TestAssessmentV4ArchiveReplaysAfterPresentationUpgrade(t *testing.T) {
	service, id, _ := openStallServiceTestRun(t, nil, false)
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	report := *detail.Report
	correlation, err := correlateStall(service.runDir(id), report, "S001", 1)
	if err != nil {
		t.Fatal(err)
	}
	item := StallInvestigation{ID: "S001", StallIndex: 1, PlayerRevision: report.Player.Revision, Status: "partial", ModelState: "unconfigured", CreatedAt: report.Manifest.Observation.StartedAt, UpdatedAt: report.Manifest.Observation.StartedAt, Stall: report.Player.Stalls[0], Correlation: correlation, Media: InvestigationMedia{Status: "unavailable", Assets: []DerivedArtifact{}}}
	item.Assessment = assessStallV4(item)
	report.StallInvestigations = []StallInvestigation{item}
	if err := persistStallReport(service.runDir(id), &report); err != nil {
		t.Fatal(err)
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
	if err != nil || !reflect.DeepEqual(got.Report.StallInvestigations[0], item) {
		t.Fatalf("legacy assessment4 changed: %v", err)
	}
}

func TestPresentationModelIsBoundedAndKeepsMeasuredZero(t *testing.T) {
	zero := 0
	duration := int64(0)
	unknown := modelPresentation(&PlayerPresentationReport{Support: "unsupported"})
	if unknown.Count != nil {
		t.Fatal("unsupported became zero")
	}
	measured := modelPresentation(&PlayerPresentationReport{Support: "supported", Count: &zero, TotalMS: &duration, MaxMS: &duration})
	if measured.Count == nil || *measured.Count != 0 {
		t.Fatal("measured zero lost")
	}
	source := &PlayerPresentationReport{Support: "supported"}
	for i := int64(0); i < 40; i++ {
		source.Gaps = append(source.Gaps, PlayerPresentationGapReport{StartMS: i * 2000, EndMS: i*2000 + 1000, DurationMS: 1000})
	}
	model := modelPresentation(source)
	if len(model.Gaps) != 16 || model.ModelOmittedGaps != 24 || len(source.Gaps) != 40 {
		t.Fatal("unbounded or mutated presentation model summary")
	}
}

func TestCallbackAndBufferingOverlapRemainsOneRealBufferingStall(t *testing.T) {
	buffer := PlayerStall{StartMS: 2000, EndMS: 4000, DurationMS: 2000}
	report := Report{Player: &PlayerReport{StallCount: 1, Stalls: []PlayerStall{buffer}, Presentation: &PlayerPresentationReport{Gaps: []PlayerPresentationGapReport{{StartMS: 1500, EndMS: 4500, DurationMS: 3000, ExpectedFrameMS: 40, BufferingOverlapMS: 2000}}}}}
	overlap, evidence := presentationBufferingOverlapEvidence(report, buffer)
	if overlap != 2000 || len(evidence) != 2 || evidence[0].Kind != "player_stall" || evidence[1].Kind != "player_presentation_gap" || report.Player.StallCount != 1 {
		t.Fatalf("incorrect overlap evidence: %d %+v", overlap, evidence)
	}
	if none, _ := presentationBufferingOverlapEvidence(report, PlayerStall{StartMS: 5000, EndMS: 6000, DurationMS: 1000}); none != 0 {
		t.Fatal("unrelated intervals were associated")
	}
}
