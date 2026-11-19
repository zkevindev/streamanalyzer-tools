package diagnostic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func presentationRecording() PlayerRecording {
	return PlayerRecording{Version: 4, Revision: 1, StartedAt: time.Now().UTC(), Source: "same_capture", ElapsedMS: 10000, Complete: true, Events: []PlayerEvent{{Kind: "start"}, {AtMS: 100, Kind: "playing"}, {AtMS: 9000, Kind: "stop"}}, Presentation: &PlayerPresentationRecording{Support: "supported", ObservationStartMS: 0, ObservationEndMS: 10000, ReadyAtMS: assessmentNumber(100), Gaps: []PlayerPresentationGap{{StartMS: 1000, EndMS: 2500, ExpectedFrameMS: 33}}, DroppedGaps: 0}}
}

func TestPlayerPresentationWholeGapsExcludeInactiveAndEOFWithoutInventingStalls(t *testing.T) {
	record := presentationRecording()
	record.ElapsedMS = 12000
	record.Events = []PlayerEvent{{Kind: "start"}, {AtMS: 1000, Kind: "playing"}, {AtMS: 2000, Kind: "waiting"}, {AtMS: 2600, Kind: "playing"}, {AtMS: 3000, Kind: "pause"}, {AtMS: 4000, Kind: "playing"}, {AtMS: 5000, Kind: "seeking"}, {AtMS: 5500, Kind: "seeked"}, {AtMS: 6000, Kind: "playing"}, {AtMS: 8000, Kind: "hidden"}, {AtMS: 8500, Kind: "visible"}, {AtMS: 9000, Kind: "playing"}, {AtMS: 11000, Kind: "stop"}}
	record.Presentation = &PlayerPresentationRecording{Support: "supported", ObservationEndMS: 12000, ReadyAtMS: assessmentNumber(500), Gaps: []PlayerPresentationGap{{500, 1500, 33}, {1800, 2800, 33}, {2800, 4200, 33}, {4500, 6500, 33}, {6500, 7600, 33}, {7800, 9200, 33}, {9500, 10500, 33}}, DroppedGaps: 3}
	if err := validatePlayerRecording(record, record.StartedAt); err != nil {
		t.Fatal(err)
	}
	observation := Observation{StartedAt: record.StartedAt.Add(200 * time.Millisecond), ElapsedMS: 10000, EndReason: "eof"}
	player := analyzePlayer(record, observation)
	value := player.Presentation
	if value == nil || value.ReadyAtMS == nil || *value.ReadyAtMS != 300 || value.ActiveCoverageMS != 6200 || !reflect.DeepEqual(value.ActiveIntervals, []PlayerSchedulingInterval{{800, 2800}, {3800, 4800}, {5800, 7800}, {8800, 10000}}) {
		t.Fatalf("coverage/clock alignment incorrect: %+v", value)
	}
	if value.Count == nil || *value.Count != 2 || *value.TotalMS != 2100 || *value.MaxMS != 1100 || len(value.Gaps) != 2 || value.Gaps[0].StartMS != 1600 || value.Gaps[0].EndMS != 2600 || value.Gaps[0].DurationMS != 1000 || value.Gaps[0].BufferingOverlapMS != 600 || value.Gaps[1].BufferingOverlapMS != 0 || value.DroppedGaps != 3 {
		t.Fatalf("whole-gap filter or buffering overlap incorrect: %+v", value)
	}
	without := record
	without.Presentation = nil
	legacy := analyzePlayer(without, observation)
	if player.StallCount != 1 || !reflect.DeepEqual(player.Stalls, legacy.Stalls) || player.TotalStallMS != legacy.TotalStallMS {
		t.Fatal("presentation gaps changed real buffering stall metrics")
	}
	if got := analyzePlayer(record, Observation{StartedAt: observation.StartedAt}).Presentation; got.Count == nil || *got.Count != 3 {
		t.Fatal("unknown pending EOF clipped a complete active callback gap")
	}
	if got := analyzePlayer(record, Observation{StartedAt: record.StartedAt, EndReason: "eof", ElapsedMS: 0}).Presentation; got.Count != nil || got.ActiveCoverageMS != 0 {
		t.Fatal("known zero-length EOF manufactured observed-zero presentation metrics")
	}
	value.Gaps[0].StartMS++
	if record.Presentation.Gaps[1].StartMS != 1800 {
		t.Fatal("summary aliases immutable raw gaps")
	}
}

func TestPlayerPresentationUnsupportedWarmupObservedZeroAndNoCoverage(t *testing.T) {
	for _, test := range []struct {
		name   string
		modify func(*PlayerRecording)
		known  bool
	}{
		{"observed zero after cadence ready", func(record *PlayerRecording) { record.Presentation.Gaps = nil }, true},
		{"unsupported callback API", func(record *PlayerRecording) {
			record.Presentation.Support = "unsupported"
			record.Presentation.ReadyAtMS = nil
			record.Presentation.Gaps = nil
		}, false},
		{"startup warmup still unknown", func(record *PlayerRecording) { record.Presentation.ReadyAtMS = nil; record.Presentation.Gaps = nil }, false},
		{"never actively playing", func(record *PlayerRecording) { record.Events = []PlayerEvent{{Kind: "start"}} }, false},
		{"cadence ready only at observation end", func(record *PlayerRecording) {
			record.Presentation.ReadyAtMS = assessmentNumber(10000)
			record.Presentation.Gaps = nil
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := presentationRecording()
			test.modify(&record)
			if err := validatePlayerRecording(record, record.StartedAt); err != nil {
				t.Fatal(err)
			}
			value := analyzePlayer(record, Observation{StartedAt: record.StartedAt}).Presentation
			if (value.Count != nil) != test.known || (value.TotalMS != nil) != test.known || (value.MaxMS != nil) != test.known {
				t.Fatalf("unknown versus observed-zero presentation metrics conflated: %+v", value)
			}
			if test.known && (*value.Count != 0 || *value.TotalMS != 0 || *value.MaxMS != 0 || value.Gaps == nil) {
				t.Fatal("observed zero lost explicit empty metrics")
			}
		})
	}
}

func TestPlayerPresentationValidationCadenceThresholdsVersionsAndAppendOnly(t *testing.T) {
	base := presentationRecording()
	for _, test := range []struct {
		name   string
		modify func(*PlayerRecording)
	}{
		{"old raw version", func(record *PlayerRecording) { record.Version = 3 }},
		{"unknown version", func(record *PlayerRecording) { record.Version = 5 }},
		{"unknown support", func(record *PlayerRecording) { record.Presentation.Support = "estimated" }},
		{"unsupported ready point", func(record *PlayerRecording) { record.Presentation.Support = "unsupported" }},
		{"unknown cadence with gap", func(record *PlayerRecording) { record.Presentation.ReadyAtMS = nil }},
		{"ready before coverage", func(record *PlayerRecording) { record.Presentation.ReadyAtMS = assessmentNumber(-1) }},
		{"ready after coverage", func(record *PlayerRecording) { record.Presentation.ReadyAtMS = assessmentNumber(10001) }},
		{"gap before ready", func(record *PlayerRecording) { record.Presentation.ReadyAtMS = assessmentNumber(1100) }},
		{"coverage beyond elapsed", func(record *PlayerRecording) { record.Presentation.ObservationEndMS++ }},
		{"negative coverage", func(record *PlayerRecording) { record.Presentation.ObservationStartMS = -1 }},
		{"zero frame cadence", func(record *PlayerRecording) { record.Presentation.Gaps[0].ExpectedFrameMS = 0 }},
		{"cadence beyond bound", func(record *PlayerRecording) { record.Presentation.Gaps[0].ExpectedFrameMS = 5001 }},
		{"gap below absolute threshold", func(record *PlayerRecording) { record.Presentation.Gaps[0].EndMS = 1999 }},
		{"gap below three cadence periods", func(record *PlayerRecording) { record.Presentation.Gaps[0].ExpectedFrameMS = 501 }},
		{"overlapping gaps", func(record *PlayerRecording) {
			record.Presentation.Gaps = append(record.Presentation.Gaps, PlayerPresentationGap{2000, 3500, 33})
		}},
		{"overflowing end", func(record *PlayerRecording) { record.Presentation.Gaps[0].EndMS = math.MinInt64 }},
		{"gap beyond observation", func(record *PlayerRecording) { record.Presentation.Gaps[0].EndMS = 10001 }},
		{"too many gaps", func(record *PlayerRecording) { record.Presentation.Gaps = make([]PlayerPresentationGap, 129) }},
		{"negative drops", func(record *PlayerRecording) { record.Presentation.DroppedGaps = -1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := schedulingClone(t, base)
			test.modify(&record)
			if err := validatePlayerRecording(record, base.StartedAt); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid presentation data accepted: %v", err)
			}
		})
	}
	for _, cadence := range []int64{33, 500} {
		record := schedulingClone(t, base)
		record.Presentation.Gaps[0].ExpectedFrameMS = cadence
		if err := validatePlayerRecording(record, base.StartedAt); err != nil {
			t.Fatalf("exact threshold rejected: %v", err)
		}
	}
	for _, version := range []int{1, 2, 3, 4} {
		record := schedulingClone(t, base)
		record.Version, record.Presentation = version, nil
		if version >= 2 {
			record.Samples = []PlayerSample{{AtMS: 100, Video: &PlayerVideoSample{QualitySource: "unavailable"}}}
		}
		if version >= 3 {
			record.Scheduling = &PlayerSchedulingRecording{LongTaskSupport: "unsupported", ObservationEndMS: 10000}
		}
		if err := validatePlayerRecording(record, base.StartedAt); err != nil {
			t.Fatalf("legacy/video/scheduling compatibility failed in v%d: %v", version, err)
		}
		if analyzePlayer(record, Observation{StartedAt: base.StartedAt}).Presentation != nil {
			t.Fatal("legacy recording manufactured presentation metrics")
		}
	}
	for _, change := range []func(*PlayerPresentationRecording){func(value *PlayerPresentationRecording) { value.ReadyAtMS = nil; value.Gaps = nil }, func(value *PlayerPresentationRecording) { *value.ReadyAtMS++ }, func(value *PlayerPresentationRecording) { value.Gaps[0].StartMS++ }, func(value *PlayerPresentationRecording) { value.Support = "unsupported" }, func(value *PlayerPresentationRecording) { value.ObservationEndMS-- }, func(value *PlayerPresentationRecording) { value.DroppedGaps-- }} {
		next := schedulingClone(t, base)
		change(next.Presentation)
		if playerPresentationExtends(base.Presentation, next.Presentation) {
			t.Fatal("existing presentation prefix or cadence point was rewritten")
		}
	}
	unknown := schedulingClone(t, base)
	unknown.Presentation.ReadyAtMS, unknown.Presentation.Gaps = nil, nil
	if !playerPresentationExtends(unknown.Presentation, base.Presentation) || !playerPresentationExtends(base.Presentation, schedulingClone(t, base).Presentation) {
		t.Fatal("first cadence readiness or value-equal decoded prefix was rejected")
	}
}

func presentationServiceFixture(t *testing.T, gaps []PlayerPresentationGap, noBuffering bool) (*Service, string, PlayerRecording) {
	t.Helper()
	service, id, _ := openStallServiceTestRun(t, nil, false)
	dir := service.runDir(id)
	record, err := readPlayerRecording(dir)
	if err != nil {
		t.Fatal(err)
	}
	record.Version, record.Revision = 4, 2
	record.Presentation = &PlayerPresentationRecording{Support: "supported", ObservationStartMS: 100, ObservationEndMS: 9000, ReadyAtMS: assessmentNumber(100), Gaps: gaps}
	if noBuffering {
		record.Events = slices.DeleteFunc(record.Events, func(event PlayerEvent) bool { return event.Kind == "waiting" })
	}
	if err := writePlayerRecording(dir, record); err != nil {
		t.Fatal(err)
	}
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := attachPlayerArtifact(dir, detail.Report); err != nil {
		t.Fatal(err)
	}
	if err := persistReport(dir, *detail.Report); err != nil {
		t.Fatal(err)
	}
	return service, id, record
}

func TestPlayerPresentationSelectedCasesPreserveRealBufferingScopeAndOfflineReplay(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		t.Run(map[bool]string{false: "presentation only", true: "overlapping real buffering"}[overlap], func(t *testing.T) {
			service, id, record := presentationServiceFixture(t, []PlayerPresentationGap{{3500, 5000, 33}}, !overlap)
			detail, _ := service.Detail(id)
			interval, err := resolvePlayerInterval(*detail.Report, IntervalPresentationGap, 1)
			if err != nil {
				t.Fatal(err)
			}
			correlation, err := correlatePlayerInterval(service.runDir(id), *detail.Report, "S001", IntervalPresentationGap, 1)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			item := StallInvestigation{ID: "S001", IntervalKind: IntervalPresentationGap, StallIndex: 1, PlayerRevision: record.Revision, Stall: interval, Status: "partial", CreatedAt: now, UpdatedAt: now, Correlation: correlation, Media: InvestigationMedia{Status: "unavailable", Assets: []DerivedArtifact{}}, ModelState: "unconfigured"}
			item.Assessment = assessStall(item)
			detail.Report.StallInvestigations = []StallInvestigation{item}
			if err := persistStallReport(service.runDir(id), detail.Report); err != nil {
				t.Fatal(err)
			}
			service.SetModel(stallTestExplainer{name: "presentation-case", call: func(_ context.Context, scoped Report) (Explanation, error) {
				want := 0
				if overlap {
					want = 1
				}
				if scoped.Player.StallCount != want || len(scoped.Player.Stalls) != want || (!overlap && (scoped.Player.TotalStallMS != 0 || scoped.Player.MaxStallMS != 0 || scoped.Player.StallRatio != 0)) || scoped.Player.Presentation == nil || scoped.StallInvestigations[0].IntervalKind != IntervalPresentationGap {
					t.Errorf("presentation selection fabricated buffering or lost selected context: %+v", scoped.Player)
				}
				return Explanation{Provider: ProviderOpenAICompatible, Model: "presentation-case", Summary: "核对呈帧间隔", Hypotheses: []Hypothesis{{Claim: "对照同一时段的呈现和调度", EvidenceIDs: []string{scoped.Findings[0].ID}, NextCheck: "比较播放器时间轴"}}}, nil
			}})
			if _, err := service.ExplainStall(t.Context(), id, "S001"); err != nil {
				t.Fatal(err)
			}
			before, _ := service.Detail(id)
			var archive bytes.Buffer
			if err := service.WriteBundle(id, &archive); err != nil {
				t.Fatal(err)
			}
			service.SetModel(nil)
			imported, err := service.Import(bytes.NewReader(archive.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			after, _ := service.Detail(imported.ID)
			if !reflect.DeepEqual(before.Report, after.Report) {
				t.Fatal("offline import changed presentation selector, correlation, model citations, or raw-derived report")
			}
			raw, err := readPlayerRecording(service.runDir(imported.ID))
			if err != nil || !reflect.DeepEqual(raw, record) {
				t.Fatalf("offline import changed raw v4 callback provenance: %v", err)
			}
			for _, test := range []struct {
				name   string
				modify func(*investigationLedger)
			}{
				{"unknown kind", func(ledger *investigationLedger) { ledger.Items[0].IntervalKind = "decoder_failure" }},
				{"kind relabeled buffering", func(ledger *investigationLedger) { ledger.Items[0].IntervalKind = ""; ledger.Items[0].Stall.StartMS++ }},
				{"invalid selected index", func(ledger *investigationLedger) { ledger.Items[0].StallIndex = 2 }},
				{"tampered selected interval", func(ledger *investigationLedger) { ledger.Items[0].Stall.DurationMS++ }},
				{"duplicate kind and index", func(ledger *investigationLedger) {
					copy := ledger.Items[0]
					copy.ID = "S002"
					ledger.Items = append(ledger.Items, copy)
				}},
			} {
				t.Run(test.name, func(t *testing.T) {
					bad := assessmentRewriteBundle(t, archive.Bytes(), test.modify)
					if _, err := service.Import(bytes.NewReader(bad)); !errors.Is(err, ErrInvalid) {
						t.Fatalf("selector tampering survived a valid checksum: %v", err)
					}
				})
			}
			malformedRaw := networkRewriteBundle(t, archive.Bytes(), func(files map[string][]byte) {
				var value PlayerRecording
				if err := json.Unmarshal(files[playerName], &value); err != nil {
					t.Fatal(err)
				}
				value.Presentation.Gaps[0].ExpectedFrameMS = 0
				files[playerName], _ = json.Marshal(value)
				var manifest Manifest
				json.Unmarshal(files[manifestName], &manifest)
				hash := sha256.Sum256(files[playerName])
				manifest.Player = &Artifact{Bytes: int64(len(files[playerName])), SHA256: hex.EncodeToString(hash[:])}
				files[manifestName], _ = json.Marshal(manifest)
			})
			if _, err := service.Import(bytes.NewReader(malformedRaw)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("checksum-valid malformed raw callback cadence imported: %v", err)
			}
		})
	}
}

func TestPlayerPresentationInvestigationSelectorsAndSharedCaseLimit(t *testing.T) {
	for _, test := range []struct {
		request StallInvestigationRequest
		kind    string
		index   int
		valid   bool
	}{
		{StallInvestigationRequest{StallIndex: 1}, IntervalBuffering, 1, true},
		{StallInvestigationRequest{IntervalIndex: 1}, IntervalBuffering, 1, true},
		{StallInvestigationRequest{IntervalKind: IntervalBuffering, StallIndex: 1, IntervalIndex: 1}, IntervalBuffering, 1, true},
		{StallInvestigationRequest{StallIndex: 1, IntervalIndex: 2}, "", 0, false},
		{StallInvestigationRequest{IntervalKind: IntervalPresentationGap, IntervalIndex: 1}, IntervalPresentationGap, 1, true},
		{StallInvestigationRequest{IntervalKind: IntervalPresentationGap, StallIndex: 1, IntervalIndex: 1}, "", 0, false},
		{StallInvestigationRequest{IntervalKind: IntervalPresentationGap}, "", 0, false},
		{StallInvestigationRequest{IntervalKind: "unknown", IntervalIndex: 1}, "", 0, false},
		{StallInvestigationRequest{StallIndex: -1, IntervalIndex: 1}, "", 0, false},
	} {
		kind, index, err := normalizePlayerIntervalRequest(test.request)
		if (err == nil) != test.valid || kind != test.kind || index != test.index {
			t.Fatalf("selector normalization incorrect: %+v => %s %d %v", test.request, kind, index, err)
		}
	}
	var gaps []PlayerPresentationGap
	for index := int64(0); index < 8; index++ {
		start := 100 + 1100*index
		gaps = append(gaps, PlayerPresentationGap{start, start + 1000, 33})
	}
	service, id, _ := presentationServiceFixture(t, gaps, false)
	service.investigationSlots <- struct{}{}
	service.investigationSlots <- struct{}{}
	defer func() {
		detail, _ := service.Detail(id)
		for _, item := range detail.Report.StallInvestigations {
			service.StopStall(id, item.ID)
		}
		<-service.investigationSlots
		<-service.investigationSlots
	}()
	buffering, err := service.InvestigateStall(id, StallInvestigationRequest{StallIndex: 1})
	if err != nil {
		t.Fatal(err)
	}
	presentation, err := service.InvestigateStall(id, StallInvestigationRequest{IntervalKind: IntervalPresentationGap, IntervalIndex: 1})
	if err != nil || buffering.ID == presentation.ID || buffering.IntervalKind != "" || presentation.IntervalKind != IntervalPresentationGap {
		t.Fatalf("same numeric index across kinds collided: %+v %+v %v", buffering, presentation, err)
	}
	again, err := service.InvestigateStall(id, StallInvestigationRequest{IntervalKind: IntervalBuffering, IntervalIndex: 1})
	if err != nil || again.ID != buffering.ID {
		t.Fatal("legacy buffering and canonical buffering selectors were not idempotent")
	}
	data, _ := json.Marshal(buffering)
	if bytes.Contains(data, []byte("interval_kind")) {
		t.Fatal("legacy buffering item gained a serialized kind field")
	}
	for index := 2; index <= 7; index++ {
		if _, err := service.InvestigateStall(id, StallInvestigationRequest{IntervalKind: IntervalPresentationGap, IntervalIndex: index}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.InvestigateStall(id, StallInvestigationRequest{IntervalKind: IntervalPresentationGap, IntervalIndex: 8}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "最多补查") {
		t.Fatalf("valid ninth selection escaped the combined buffering/presentation case cap: %v", err)
	}
	if _, err := service.InvestigateStall(id, StallInvestigationRequest{IntervalKind: IntervalPresentationGap, IntervalIndex: 99}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unobserved presentation selector accepted: %v", err)
	}
	detail, _ := service.Detail(id)
	if len(detail.Report.StallInvestigations) != 8 {
		t.Fatal("combined case limit changed")
	}
}

func TestPlayerPresentationGlobalFindingsRemainObservedAndStable(t *testing.T) {
	record := presentationRecording()
	report := Report{Player: analyzePlayer(record, Observation{StartedAt: record.StartedAt}), Findings: []Finding{}}
	addPlayerFindings(&report)
	if report.Player.StallCount != 0 || len(report.Findings) != 1 || report.Findings[0].Code != "player_presentation_gaps_observed" || !strings.Contains(report.Findings[0].NextCheck, "不证明") || report.Findings[0].Evidence[0].DurationMS != 1500 {
		t.Fatalf("presentation evidence was promoted to buffering or decoder proof: %+v", report)
	}
	before, _ := json.Marshal(report.Findings)
	addPlayerFindings(&report)
	after, _ := json.Marshal(report.Findings)
	if !bytes.Equal(before, after) {
		t.Fatal("rebuilding findings duplicated or renumbered unchanged presentation evidence")
	}
}

func TestPlayerPresentationSaveReadinessTransitionsValuePrefixesAndFinalImmutability(t *testing.T) {
	service, id := openRunForExplanation(t, nil)
	record := registerTestPlayer(t, service, id)
	record.Version = 4
	record.Events = append(record.Events, PlayerEvent{Kind: "playing"})
	record.Presentation = &PlayerPresentationRecording{Support: "supported", ObservationEndMS: 100, Gaps: []PlayerPresentationGap{}}
	initial, err := service.SavePlayerRecording(id, "test-player-grant", record)
	if err != nil || initial.Presentation == nil || initial.Presentation.Count != nil {
		t.Fatalf("warmup was recorded as observed zero: %+v %v", initial, err)
	}
	record = schedulingClone(t, record)
	record.Revision, record.ElapsedMS, record.Presentation.ObservationEndMS = 2, 200, 200
	record.Presentation.ReadyAtMS = assessmentNumber(100)
	ready, err := service.SavePlayerRecording(id, "test-player-grant", record)
	if err != nil || ready.Presentation.Count == nil || *ready.Presentation.Count != 0 {
		t.Fatalf("first stable cadence did not enable observed-zero metrics: %+v %v", ready, err)
	}
	record.Revision, record.ElapsedMS, record.Presentation.ObservationEndMS = 3, 2000, 2000
	record.Presentation.Gaps = append(record.Presentation.Gaps, PlayerPresentationGap{500, 1500, 33})
	record.Presentation.DroppedGaps = 3
	record.Samples = append(record.Samples, PlayerSample{AtMS: 1000, Video: &PlayerVideoSample{QualitySource: "unavailable"}})
	record.Scheduling = &PlayerSchedulingRecording{LongTaskSupport: "unsupported", ObservationEndMS: 2000, TimerDelays: []PlayerSchedulingInterval{{400, 600}}}
	if _, err := service.SavePlayerRecording(id, "test-player-grant", schedulingClone(t, record)); err != nil {
		t.Fatalf("v4 scheduling/video or decoded presentation prefix rejected: %v", err)
	}
	for _, modify := range []func(*PlayerPresentationRecording){func(value *PlayerPresentationRecording) { *value.ReadyAtMS = 101 }, func(value *PlayerPresentationRecording) { value.Gaps[0].EndMS = 1501 }, func(value *PlayerPresentationRecording) { value.DroppedGaps = 2 }} {
		bad := schedulingClone(t, record)
		bad.Revision, bad.ElapsedMS, bad.Presentation.ObservationEndMS = 4, 2500, 2500
		modify(bad.Presentation)
		_, err := service.SavePlayerRecording(id, "test-player-grant", bad)
		if !errors.Is(err, ErrReportChanged) {
			t.Fatalf("saved callback provenance was rewritten: %v", err)
		}
	}
	record = schedulingClone(t, record)
	record.Revision, record.ElapsedMS, record.Presentation.ObservationEndMS, record.Complete = 4, 2500, 2500, true
	record.Presentation.Gaps = append(record.Presentation.Gaps, PlayerPresentationGap{1500, 2500, 33})
	record.Events = append(record.Events, PlayerEvent{AtMS: 2500, Kind: "stop"})
	final, err := service.SavePlayerRecording(id, "test-player-grant", record)
	if err != nil || final.Presentation.Count == nil || *final.Presentation.Count != 2 || final.StallCount != 0 {
		t.Fatalf("final presentation count fabricated buffering: %+v %v", final, err)
	}
	if _, err := service.SavePlayerRecording(id, "test-player-grant", schedulingClone(t, record)); err != nil {
		t.Fatalf("same final revision decoded retry rejected: %v", err)
	}
	mutated := schedulingClone(t, record)
	mutated.Presentation.Gaps[0].ExpectedFrameMS = 34
	if err := writePlayerRecording(service.runDir(id), mutated); !errors.Is(err, ErrInvalid) {
		t.Fatalf("immutable raw v4 revision changed: %v", err)
	}
	mutated.Revision++
	if _, err := service.SavePlayerRecording(id, "test-player-grant", mutated); !errors.Is(err, ErrReportChanged) {
		t.Fatalf("final v4 recording accepted a later rewrite: %v", err)
	}
}
