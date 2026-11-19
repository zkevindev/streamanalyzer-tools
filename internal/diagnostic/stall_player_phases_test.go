package diagnostic

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func phaseAssessmentFixture() StallInvestigation {
	item := networkAssessmentFixture()
	item.Correlation.PlayerRequests[0].Hops = []PlayerNetworkHop{{Number: 1, StartMS: 1800, EndMS: 3000, HTTPStatus: 200, Stages: []CaptureStage{{Name: "response_first_byte", StartMS: 1800, EndMS: 2800}, {Name: "receive", StartMS: 2800, EndMS: 3000}}}}
	for _, finding := range playerPhaseFindings(item.Correlation.PlayerRequests, item.Stall) {
		finding.ID = "S003-E004"
		item.Correlation.Findings = append(item.Correlation.Findings, finding)
	}
	return item
}

func TestPlaybackPhaseEvidenceNarrowsChecksWithoutClaimingCause(t *testing.T) {
	item := phaseAssessmentFixture()
	result := assessStall(item)
	phase := assessmentDirection(result, "playback_request_phases")
	if phase == nil || phase.Status != "observed" || !reflect.DeepEqual(phase.EvidenceIDs, []string{"S003-E004"}) {
		t.Fatalf("invalid phase direction: %+v", result)
	}
	actual := assessmentDirection(result, "actual_playback_request")
	if actual == nil || actual.Status != "candidate" || !strings.Contains(strings.Join(actual.NextChecks, " "), "源端处理") {
		t.Fatalf("phase checks missing from actual path: %+v", actual)
	}
	assertAssessmentReferences(t, item, result)
	for _, version := range []string{"stall-assessment/1", "stall-assessment/2", "stall-assessment/3"} {
		if _, err := recomputeStallAssessment(item, version); !errors.Is(err, ErrInvalid) {
			t.Fatal("phase evidence was silently evaluated with older rules")
		}
	}
}

func TestPlaybackPhaseWindowAndCancellationAreConservative(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*PlayerNetworkRequest)
		want   int
	}{
		{"within selected stall", func(*PlayerNetworkRequest) {}, 1},
		{"phase before selected", func(request *PlayerNetworkRequest) {
			request.Hops[0].Stages = []CaptureStage{{Name: "connect", StartMS: 1000, EndMS: 1700}}
		}, 0},
		{"phase below threshold", func(request *PlayerNetworkRequest) {
			request.Hops[0].Stages = []CaptureStage{{Name: "connect", StartMS: 2100, EndMS: 2599}}
		}, 0},
		{"cancel is retained status", func(request *PlayerNetworkRequest) {
			request.Outcome = "canceled"
			request.FailureStage = "receive"
			request.FailureCode = "canceled"
		}, 0},
		{"incomplete no failure conclusion", func(request *PlayerNetworkRequest) { request.Outcome = "incomplete" }, 0},
		{"known receive failure", func(request *PlayerNetworkRequest) {
			request.Outcome = "transport_error"
			request.FailureStage = "receive"
			request.FailureCode = "transport_error"
			request.Hops[0].Stages = []CaptureStage{{Name: "receive", StartMS: 2000, EndMS: 3000}}
		}, 1},
		{"terminal hop omitted does not blame earlier DNS", func(request *PlayerNetworkRequest) {
			request.Outcome = "transport_error"
			request.FailureStage = "dns"
			request.FailureCode = "dns_error"
			request.DroppedHops = 1
			request.Hops = []PlayerNetworkHop{{Number: 1, StartMS: 2000, EndMS: 2100, HTTPStatus: 302, Stages: []CaptureStage{{Name: "dns", StartMS: 2000, EndMS: 2100}}}}
		}, 0},
		{"failure before stall despite later handler", func(request *PlayerNetworkRequest) {
			request.Outcome = "transport_error"
			request.FailureStage = "dns"
			request.FailureCode = "dns_error"
			request.Hops[0].Stages = []CaptureStage{{Name: "dns", StartMS: 1700, EndMS: 1800}}
			request.Hops[0].EndMS = 1800
			request.Hops[0].FailureStage = "dns"
			request.Hops[0].FailureCode = "dns_error"
		}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := phaseAssessmentFixture()
			test.change(&item.Correlation.PlayerRequests[0])
			if got := playerPhaseFindings(item.Correlation.PlayerRequests, item.Stall); len(got) != test.want {
				t.Fatalf("got %+v, want %d", got, test.want)
			}
		})
	}
}

func TestPlaybackPhaseEvidenceIsBoundedAndFailuresPrioritized(t *testing.T) {
	item := phaseAssessmentFixture()
	requests := []PlayerNetworkRequest{}
	for n := 1; n <= 40; n++ {
		request := item.Correlation.PlayerRequests[0]
		request.Number = n
		requests = append(requests, request)
	}
	requests[len(requests)-1].FailureStage = "dns"
	requests[len(requests)-1].FailureCode = "dns_error"
	requests[len(requests)-1].Outcome = "transport_error"
	requests[len(requests)-1].Hops = clonePlayerNetworkHops(requests[len(requests)-1].Hops)
	requests[len(requests)-1].Hops[0].FailureStage = "dns"
	requests[len(requests)-1].Hops[0].FailureCode = "dns_error"
	findings := playerPhaseFindings(requests, item.Stall)
	if len(findings) != 6 || findings[0].Code != "stall_player_phase_failed" || !strings.Contains(findings[0].Message, "#40") {
		t.Fatalf("bounded evidence lost failure: %+v", findings)
	}
}

func TestModelPlaybackPhaseBudgetAndUnknownZero(t *testing.T) {
	no := false
	zero := int64(0)
	requests := []PlayerNetworkRequest{}
	for n := 1; n <= 64; n++ {
		request := PlayerNetworkRequest{Number: n, Outcome: "complete"}
		for h := 1; h <= 8; h++ {
			hop := PlayerNetworkHop{Number: h, TTFBMS: &zero, ConnectionReused: &no}
			for s := 0; s < 16; s++ {
				hop.Stages = append(hop.Stages, CaptureStage{Name: "connect", StartMS: int64(s * 100), EndMS: int64(s*100 + 50)})
			}
			request.Hops = append(request.Hops, hop)
		}
		requests = append(requests, request)
	}
	before, _ := json.Marshal(requests)
	budget := &modelPhaseBudget{hops: 48, stages: 192}
	projected := modelPlaybackRequests(requests, budget)
	var hops, stages, omitted int
	for _, request := range projected {
		omitted += request.ModelOmittedHops
		for _, hop := range request.Hops {
			hops++
			stages += len(hop.Stages)
			if hop.TTFBMS == nil || *hop.TTFBMS != 0 || hop.ConnectionReused == nil || *hop.ConnectionReused {
				t.Fatal("zero/false observed values disappeared")
			}
		}
	}
	if hops > 48 || stages > 192 || omitted == 0 {
		t.Fatalf("unbounded model phases %d/%d", hops, stages)
	}
	after, _ := json.Marshal(requests)
	if !bytes.Equal(before, after) {
		t.Fatal("model projection mutated original phase data")
	}
	unknown := modelPlaybackRequests([]PlayerNetworkRequest{{Number: 1, Hops: []PlayerNetworkHop{{Number: 1}}}}, &modelPhaseBudget{hops: 1, stages: 1})
	if unknown[0].Hops[0].TTFBMS != nil || unknown[0].Hops[0].ConnectionReused != nil {
		t.Fatal("unknown became measured zero/false")
	}
}

func TestAssessmentV3ArchiveReplaysAfterPhaseUpgrade(t *testing.T) {
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
	item.Assessment = assessStallV3(item)
	report.StallInvestigations = []StallInvestigation{item}
	if err := persistStallReport(service.runDir(id), &report); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal(err)
	}
	run, err := service.Import(bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Detail(run.ID)
	if err != nil || !reflect.DeepEqual(got.Report.StallInvestigations[0], item) {
		t.Fatalf("legacy assessment3 changed: %v", err)
	}
}
