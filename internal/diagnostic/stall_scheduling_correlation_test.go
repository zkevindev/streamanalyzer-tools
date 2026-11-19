package diagnostic

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func schedulingAssessmentFixture() StallInvestigation {
	item := assessmentFixture()
	item.Correlation.BufferSamples = nil // Scheduling must not fill missing buffer measurements.
	item.Correlation.Scheduling = &PlayerSchedulingReport{LongTaskSupport: "supported", ObservationStartMS: 1000, ObservationEndMS: 4000,
		ActiveCoverageMS: 3000, ActiveIntervals: []PlayerSchedulingInterval{{1000, 4000}},
		LongTasks:   &PlayerSchedulingMetrics{Count: 1, TotalMS: 800, MaxMS: 800, Intervals: []PlayerSchedulingInterval{{2100, 2900}}},
		TimerDelays: &PlayerSchedulingMetrics{Intervals: []PlayerSchedulingInterval{}}}
	for _, finding := range schedulingCaseFindings(item.Correlation.Scheduling, item.Stall) {
		finding.ID = "S003-E003"
		item.Correlation.Findings = append(item.Correlation.Findings, finding)
	}
	return item
}

func TestStallSchedulingSeparatesObservationsFromCandidateAndMissingBuffer(t *testing.T) {
	item := schedulingAssessmentFixture()
	result := assessStall(item)
	if direction := assessmentDirection(result, "page_long_tasks"); direction == nil || direction.Status != "observed" {
		t.Fatalf("missing long-task observation: %+v", result)
	}
	if direction := assessmentDirection(result, "page_scheduling"); direction == nil || direction.Status != "candidate" {
		t.Fatalf("missing scheduling direction: %+v", result)
	}
	if assessmentDirection(result, "buffer_underflow") != nil || assessmentDirection(result, "local_playback_or_schedule") != nil || len(item.Correlation.BufferSamples) != 0 {
		t.Fatal("scheduling synthesized buffer or rendering observations")
	}
	if !strings.Contains(strings.Join(result.Limitations, " "), "缺测缓冲不作插值") {
		t.Fatal("missing scheduling evidence limit")
	}
	assertAssessmentReferences(t, item, result)
	for _, version := range []string{"stall-assessment/1", "stall-assessment/2"} {
		if _, err := recomputeStallAssessment(item, version); !errors.Is(err, ErrInvalid) {
			t.Fatal("new evidence was silently downgraded to legacy rules")
		}
	}
}

func TestStallSchedulingUsesSelectedContinuousOverlap(t *testing.T) {
	for _, test := range []struct {
		name                           string
		intervals                      []PlayerSchedulingInterval
		wantObservation, wantCandidate bool
	}{
		{"only before selected", []PlayerSchedulingInterval{{1000, 1900}}, false, false},
		{"only after selected", []PlayerSchedulingInterval{{3100, 3900}}, false, false},
		{"small overlap", []PlayerSchedulingInterval{{1900, 2200}}, true, false},
		{"separate short fragments", []PlayerSchedulingInterval{{2000, 2300}, {2500, 2800}}, true, false},
		{"long overlap", []PlayerSchedulingInterval{{1900, 2600}}, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := schedulingAssessmentFixture()
			item.Correlation.Findings = item.Correlation.Findings[:2]
			item.Correlation.Scheduling.LongTasks = &PlayerSchedulingMetrics{Intervals: test.intervals}
			for _, finding := range schedulingCaseFindings(item.Correlation.Scheduling, item.Stall) {
				finding.ID = "S003-E003"
				item.Correlation.Findings = append(item.Correlation.Findings, finding)
			}
			result := assessStall(item)
			if (assessmentDirection(result, "page_long_tasks") != nil) != test.wantObservation || (assessmentDirection(result, "page_scheduling") != nil) != test.wantCandidate {
				t.Fatalf("incorrect selected scheduling interval: %+v", result)
			}
		})
	}
}

func TestStallUnsupportedLongTasksRetainsIndependentTimerEvidence(t *testing.T) {
	item := schedulingAssessmentFixture()
	item.Correlation.Scheduling.LongTaskSupport = "unsupported"
	item.Correlation.Scheduling.LongTasks = nil
	item.Correlation.Scheduling.TimerDelays = &PlayerSchedulingMetrics{Intervals: []PlayerSchedulingInterval{{2000, 2900}}}
	item.Correlation.Findings = item.Correlation.Findings[:2]
	for _, finding := range schedulingCaseFindings(item.Correlation.Scheduling, item.Stall) {
		finding.ID = "S003-E003"
		item.Correlation.Findings = append(item.Correlation.Findings, finding)
	}
	result := assessStall(item)
	if assessmentDirection(result, "page_long_tasks") != nil || assessmentDirection(result, "page_timer_delays") == nil || assessmentDirection(result, "page_scheduling") == nil {
		t.Fatalf("timer delay misclassified as native task: %+v", result)
	}
	if !strings.Contains(strings.Join(result.Limitations, " "), "不支持 Long Tasks API") {
		t.Fatal("unsupported source hidden")
	}
}

func TestSchedulingWindowRecomputesCoverageAndPreservesUnknown(t *testing.T) {
	source := schedulingAssessmentFixture().Correlation.Scheduling
	source.ActiveIntervals = []PlayerSchedulingInterval{{1000, 1800}, {2200, 4000}}
	source.ActiveCoverageMS = 2600
	source.LongTasks = &PlayerSchedulingMetrics{Count: 2, TotalMS: 600, MaxMS: 400, Intervals: []PlayerSchedulingInterval{{1600, 1800}, {2200, 2600}}}
	before := *source
	window := schedulingWindow(source, 1700, 2400)
	if window.ActiveCoverageMS != 300 || window.LongTasks.TotalMS != 300 || window.LongTasks.Count != 2 || window.LongTasks.MaxMS != 200 {
		t.Fatalf("incorrect clipped coverage: %+v %+v", window, window.LongTasks)
	}
	if !reflect.DeepEqual(*source, before) {
		t.Fatal("window mutated global scheduling data")
	}
	gap := schedulingWindow(source, 1800, 2200)
	if gap.ActiveCoverageMS != 0 || gap.LongTasks != nil || gap.TimerDelays != nil {
		t.Fatalf("inactive gap was filled: %+v", gap)
	}
	after := schedulingWindow(source, 9000, 10000)
	if after.ObservationStartMS != 4000 || after.ObservationEndMS != 4000 || after.ActiveCoverageMS != 0 {
		t.Fatalf("unobserved future coverage invented: %+v", after)
	}
}

func TestModelSchedulingIsBoundedAndPreservesZeroUnknown(t *testing.T) {
	source := schedulingAssessmentFixture().Correlation.Scheduling
	source.LongTasks = &PlayerSchedulingMetrics{Count: 32, TotalMS: 3200, MaxMS: 100, Intervals: []PlayerSchedulingInterval{}}
	for n := int64(0); n < 32; n++ {
		source.LongTasks.Intervals = append(source.LongTasks.Intervals, PlayerSchedulingInterval{n * 100, n*100 + 100})
	}
	model := modelScheduling(source)
	if len(model.LongTasks.Intervals) != 16 || model.LongTasks.OmittedIntervals != 16 || model.LongTasks.Count != 32 || model.TimerDelays == nil || model.TimerDelays.Count != 0 {
		t.Fatalf("invalid bounded summary: %+v", model)
	}
	source.LongTasks = nil
	if modelScheduling(source).LongTasks != nil || len(source.TimerDelays.Intervals) != 0 {
		t.Fatal("unknown became zero or metrics mutated")
	}
}

func TestAssessmentV2ArchiveStillReplaysAfterSchedulingUpgrade(t *testing.T) {
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
	item.Assessment = assessStallV2(item)
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
		t.Fatalf("assessment/2 archive changed: %v", err)
	}
}
