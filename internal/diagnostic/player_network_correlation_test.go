package diagnostic

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func networkAssessmentFixture() StallInvestigation {
	item := assessmentFixture()
	item.Correlation.Source = "independent_pull"
	item.Correlation.BufferSamples = []PlayerBufferSample{assessmentSample(2000, 100, 100, 0, 0), assessmentSample(2500, 20, 100, 0, 0), assessmentSample(3000, 0, 100, 0, 0)}
	request := PlayerNetworkRequest{Number: 7, ResourceID: "r3.ts", Kind: "media", Method: "GET", StartMS: 1800, EndMS: 3000, HTTPStatus: 200, Bytes: 8000, Outcome: "complete"}
	item.Correlation.PlayerRequests = []PlayerNetworkRequest{request}
	number := request.Number
	item.Correlation.Findings = append(item.Correlation.Findings, Finding{ID: "S003-E003", Code: "stall_player_request_slow", StartMS: request.StartMS, EndMS: request.EndMS,
		Evidence: []Event{{Kind: "player_request", Sequence: &number}}})
	return item
}

func TestPlayerNetworkAssessmentRequiresActualPathAndNearbyUnderflow(t *testing.T) {
	for _, test := range []struct {
		name   string
		modify func(*StallInvestigation)
		want   bool
	}{
		{"actual playback slow with underflow", func(*StallInvestigation) {}, true},
		{"sampler gap alone", func(item *StallInvestigation) {
			item.Correlation.PlayerRequests = nil
			item.Correlation.Findings = item.Correlation.Findings[:2]
			assessmentGap(item, 1800, 3000)
		}, false},
		{"fast playback request", func(item *StallInvestigation) { item.Correlation.PlayerRequests[0].StartMS = 2800 }, false},
		{"canceled request", func(item *StallInvestigation) { item.Correlation.PlayerRequests[0].Outcome = "canceled" }, false},
		{"unfinished request", func(item *StallInvestigation) { item.Correlation.PlayerRequests[0].Outcome = "incomplete" }, false},
		{"playlist request", func(item *StallInvestigation) { item.Correlation.PlayerRequests[0].Kind = "playlist" }, false},
		{"head request", func(item *StallInvestigation) { item.Correlation.PlayerRequests[0].Method = "HEAD" }, false},
		{"outside selected stall", func(item *StallInvestigation) {
			item.Correlation.PlayerRequests[0].StartMS = 500
			item.Correlation.PlayerRequests[0].EndMS = 1500
		}, false},
		{"full buffer", func(item *StallInvestigation) {
			for i := range item.Correlation.BufferSamples {
				item.Correlation.BufferSamples[i].BufferMS = 2000
			}
		}, false},
		{"known hidden interval", func(item *StallInvestigation) { item.Correlation.BufferSamples[1].Hidden = true }, false},
		{"distant underflow in long stall", func(item *StallInvestigation) {
			item.Stall.EndMS, item.Stall.DurationMS = 12000, 10000
			item.Correlation.BufferSamples = []PlayerBufferSample{assessmentSample(2000, 2000, 100, 0, 0), assessmentSample(11000, 2000, 100, 0, 0), assessmentSample(12000, 0, 100, 0, 0)}
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := networkAssessmentFixture()
			test.modify(&item)
			result := assessStall(item)
			direction := assessmentDirection(result, "actual_playback_request")
			if (direction != nil) != test.want {
				t.Fatalf("direction=%+v, want candidate=%v", direction, test.want)
			}
			if assessmentDirection(result, "source_or_transport") != nil {
				t.Fatal("independent sampler promoted to same-capture evidence")
			}
			if direction != nil && (direction.Status != "candidate" || !reflect.DeepEqual(direction.EvidenceIDs, []string{"S003-E003"})) {
				t.Fatalf("invalid evidence scope: %+v", direction)
			}
			assertAssessmentReferences(t, item, result)
		})
	}
}

func TestPlayerNetworkFailuresAndUnknownFirstBody(t *testing.T) {
	for _, outcome := range []string{"http_error", "transport_error", "timeout"} {
		t.Run(outcome, func(t *testing.T) {
			item := networkAssessmentFixture()
			item.Correlation.PlayerRequests[0].Outcome = outcome
			item.Correlation.PlayerRequests[0].FirstBodyMS = nil
			item.Correlation.Findings[2].Code = "stall_player_request_failed"
			result := assessStall(item)
			if assessmentDirection(result, "actual_playback_request") == nil {
				t.Fatalf("failed request was discarded: %+v", result)
			}
			if item.Correlation.PlayerRequests[0].FirstBodyMS != nil {
				t.Fatal("unknown first body was changed to zero")
			}
		})
	}
}

func TestRecordedHealthyOrEmptyPlaybackTraceDoesNotAskToRecaptureIt(t *testing.T) {
	for _, empty := range []bool{false, true} {
		item := networkAssessmentFixture()
		item.Correlation.PlayerNetworkRecorded = true
		item.Correlation.PlayerRequests[0].EndMS = 1850
		if empty {
			item.Correlation.PlayerRequests = nil
		}
		result := assessStall(item)
		if strings.Contains(result.Conclusion, "补采实际播放") || assessmentDirection(result, "actual_playback_request") != nil {
			t.Fatalf("saved playback trace ignored: %+v", result)
		}
	}
}

func TestPlayerRequestWindowBoundsPrioritizeSelectedStall(t *testing.T) {
	stall := PlayerStall{StartMS: 4000, EndMS: 5000, DurationMS: 1000}
	report := Report{PlayerNetwork: &PlayerNetworkReport{DroppedRequests: 5}}
	for number := 1; number <= 40; number++ {
		report.PlayerNetwork.Requests = append(report.PlayerNetwork.Requests, PlayerNetworkRequest{Number: number, StartMS: 1000, EndMS: 3000, Outcome: "http_error"})
	}
	report.PlayerNetwork.Requests = append(report.PlayerNetwork.Requests, PlayerNetworkRequest{Number: 41, StartMS: 4100, EndMS: 4700, Outcome: "complete"})
	window := StallCorrelation{WindowStartMS: 1000, WindowEndMS: 8000}
	correlatePlayerRequests(&window, report, stall)
	if len(window.PlayerRequests) != maxStallRequests || window.PlayerRequests[len(window.PlayerRequests)-1].Number != 41 {
		t.Fatalf("selected playback request was displaced: %+v", window.PlayerRequests)
	}
	if !strings.Contains(strings.Join(window.Notes, " "), "5 次请求") {
		t.Fatal("dropped request limitation omitted")
	}
	if len(report.PlayerNetwork.Requests) != 41 {
		t.Fatal("correlation mutated the source trace")
	}
}

func TestLegacyAssessmentRuleVersionRemainsImportable(t *testing.T) {
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
	item := StallInvestigation{ID: "S001", StallIndex: 1, PlayerRevision: report.Player.Revision, Status: "partial", ModelState: "unconfigured",
		CreatedAt: report.Manifest.Observation.StartedAt, UpdatedAt: report.Manifest.Observation.StartedAt, Stall: report.Player.Stalls[0], Correlation: correlation,
		Media: InvestigationMedia{Status: "unavailable", Assets: []DerivedArtifact{}}}
	item.Assessment = assessStallV1(item)
	report.StallInvestigations = []StallInvestigation{item}
	if err := persistStallReport(service.runDir(id), &report); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal(err)
	}
	replay, err := service.Import(bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	imported, err := service.Detail(replay.ID)
	if err != nil || !reflect.DeepEqual(imported.Report.StallInvestigations[0], item) {
		t.Fatalf("v1 rule archive changed: %v", err)
	}
	if _, err := recomputeStallAssessment(item, "stall-assessment/999"); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown rule version accepted")
	}
}

func TestNetworkModelSummaryIsBoundedAndNumeric(t *testing.T) {
	network := &PlayerNetworkReport{Boundary: "upstream_proxy", Complete: true}
	for i := 1; i <= 100; i++ {
		network.Requests = append(network.Requests, PlayerNetworkRequest{Number: i, ResourceID: "r1.ts", Kind: "media", Method: "GET", EndMS: int64(i), Outcome: "complete"})
	}
	input, err := modelInput(Report{PlayerNetwork: network})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Network modelPlayerNetwork `json:"player_network"`
	}
	if err := json.Unmarshal(input, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Network.Requests) != 64 || decoded.Network.OmittedRequests != 36 || len(network.Requests) != 100 {
		t.Fatalf("unbounded or mutated model input: %+v", decoded.Network)
	}
}
