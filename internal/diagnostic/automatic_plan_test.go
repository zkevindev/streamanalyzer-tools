package diagnostic

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAutomaticPlanSelectsDistinctCaptureOverlaps(t *testing.T) {
	report := Report{Manifest: Manifest{Observation: Observation{ElapsedMS: 30_000}}, Player: &PlayerReport{
		Stalls:       []PlayerStall{{StartMS: 1000, EndMS: 5000, DurationMS: 4000}, {StartMS: 6000, EndMS: 9000, DurationMS: 3000}, {StartMS: 29_000, EndMS: 60_000, DurationMS: 31_000}, {StartMS: 31_000, EndMS: 40_000, DurationMS: 9000}},
		Presentation: &PlayerPresentationReport{Gaps: []PlayerPresentationGapReport{{StartMS: 1100, EndMS: 5000, DurationMS: 3900}, {StartMS: 10_000, EndMS: 15_000, DurationMS: 5000}}},
	}}
	before, _ := json.Marshal(report)
	plan := planAutomaticInvestigation(report)
	if plan.CandidateCount != 5 || len(plan.Selected) != 3 {
		t.Fatalf("wrong bounded plan:%+v", plan)
	}
	want := []AutomaticSelection{
		{IntervalKind: IntervalPresentationGap, IntervalIndex: 2, Interval: PlayerStall{StartMS: 10_000, EndMS: 15_000, DurationMS: 5000}},
		{IntervalKind: IntervalBuffering, IntervalIndex: 1, Interval: report.Player.Stalls[0]},
		{IntervalKind: IntervalBuffering, IntervalIndex: 2, Interval: report.Player.Stalls[1]},
	}
	if !reflect.DeepEqual(plan.Selected, want) {
		t.Fatalf("selectedoverlap/redundantgap/tail:%+v", plan.Selected)
	}
	if again := planAutomaticInvestigation(report); !reflect.DeepEqual(again, plan) {
		t.Fatal("selectionisnotdeterministic")
	}
	after, _ := json.Marshal(report)
	if !bytes.Equal(before, after) {
		t.Fatal("plannerrewroteobservedstallcountorintervals")
	}
}

func TestAutomaticPlanBoundariesAndTies(t *testing.T) {
	report := Report{Manifest: Manifest{Observation: Observation{ElapsedMS: 10_000}}, Player: &PlayerReport{
		Stalls:       []PlayerStall{{StartMS: 8000, EndMS: 20_000, DurationMS: 12_000}, {StartMS: 1000, EndMS: 3000, DurationMS: 2000}, {StartMS: 5000, EndMS: 5000}, {StartMS: -1000, EndMS: -100, DurationMS: 900}, {StartMS: 10_000, EndMS: 11_000, DurationMS: 1000}, {StartMS: 3000, EndMS: 4000, DurationMS: 999}},
		Presentation: &PlayerPresentationReport{Gaps: []PlayerPresentationGapReport{{StartMS: 1000, EndMS: 3000, DurationMS: 2000}, {StartMS: 3000, EndMS: 5000, DurationMS: 2000}}},
	}}
	plan := planAutomaticInvestigation(report)
	if plan.CandidateCount != 4 || len(plan.Selected) != 3 || plan.Selected[0].IntervalKind != IntervalBuffering || plan.Selected[0].IntervalIndex != 2 || plan.Selected[1].IntervalIndex != 1 || plan.Selected[2].IntervalKind != IntervalPresentationGap || plan.Selected[2].IntervalIndex != 2 {
		t.Fatalf("unexpected stabletieorboundary:%+v", plan)
	}
	for _, empty := range []Report{{}, {Manifest: report.Manifest}, {Player: report.Player}} {
		plan := planAutomaticInvestigation(empty)
		if plan.CandidateCount != 0 || len(plan.Selected) != 0 {
			t.Fatal("inventedobservationswithoutplayer/capture")
		}
	}
}

func TestAutomaticArtifactPreparesAtomicSnapshotAndPreservesSummaryScope(t *testing.T) {
	dir := t.TempDir()
	report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, Findings: []Finding{}}
	if err := persistReport(dir, report); err != nil {
		t.Fatal(err)
	}
	baseline, err := os.ReadFile(filepath.Join(dir, reportName))
	if err != nil {
		t.Fatal(err)
	}
	scope, err := reportModelScopeSHA256(report)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := AutomaticInvestigation{Version: 1, Policy: automaticPolicy, State: "queued", Selected: []AutomaticSelection{{IntervalKind: IntervalBuffering, IntervalIndex: 1, Interval: PlayerStall{StartMS: 100, EndMS: 700, DurationMS: 600}}}, ModelState: "pending", CreatedAt: now, UpdatedAt: now}
	if err := attachAutomaticInvestigation(dir, &report, state); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(filepath.Join(dir, reportName))
	if !bytes.Equal(baseline, current) {
		t.Fatal("preparingautoartifactpublishedhalfofacasebinding")
	}
	state.Selected[0].CaseID = "S008"
	if report.AutomaticInvestigation.Selected[0].CaseID != "" {
		t.Fatal("mutablecallerstatealiasedpersistedautosnapshot")
	}
	if after, err := reportModelScopeSHA256(report); err != nil || after != scope {
		t.Fatal("statusmetadatachangednumericmodelscope", err)
	}
	report.AutomaticInvestigation.State = "investigating"
	if err := persistAutomaticInvestigation(dir, &report, *report.AutomaticInvestigation); err != nil {
		t.Fatal(err)
	}
	data, err := readAutomaticInvestigation(dir, *report.Manifest.AutomaticInvestigation)
	if err != nil {
		t.Fatal(err)
	}
	var actual AutomaticInvestigation
	if json.Unmarshal(data, &actual) != nil || actual.State != "investigating" {
		t.Fatal("wrongimmutablestatusartifact")
	}
	path := filepath.Join(dir, automaticVersionName(report.Manifest.AutomaticInvestigation.SHA256))
	if err := os.WriteFile(path, append(data, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAutomaticInvestigation(dir, *report.Manifest.AutomaticInvestigation); err == nil {
		t.Fatal("changedautomaticartifactaccepted")
	}
	invalid := *report.Manifest.AutomaticInvestigation
	invalid.SHA256 = "../../outside"
	if _, err := readAutomaticInvestigation(dir, invalid); err == nil {
		t.Fatal("noncanonicalartifactpathaccepted")
	}
}

func TestAutomaticPlanPreservesIntervalAlreadyInProgressAtCaptureStart(t *testing.T) {
	previous := PlayerStall{StartMS: -1000, EndMS: 2000, DurationMS: 3000}
	inside := PlayerStall{StartMS: 3000, EndMS: 5100, DurationMS: 2100}
	report := Report{Manifest: Manifest{Observation: Observation{ElapsedMS: 10_000}}, Player: &PlayerReport{Stalls: []PlayerStall{previous, inside}}}
	plan := planAutomaticInvestigation(report)
	if plan.CandidateCount != 2 || len(plan.Selected) != 2 || plan.Selected[0].Interval != inside || plan.Selected[1].Interval != previous {
		t.Fatalf("capture-overlap ranking changed the original interval: %+v", plan)
	}
}
