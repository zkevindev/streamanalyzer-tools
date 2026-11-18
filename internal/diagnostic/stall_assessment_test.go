package diagnostic

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func assessmentNumber(value int64) *int64 { return &value }

func assessmentSample(at, buffer, total, dropped, age int64) PlayerBufferSample {
	return PlayerBufferSample{OffsetMS: at, BufferMS: buffer, ReadyState: 3, Video: &PlayerVideoSample{
		QualitySource: "playback_quality", TotalFrames: assessmentNumber(total), DroppedFrames: assessmentNumber(dropped), FrameAgeMS: assessmentNumber(age),
	}}
}

func assessmentFixture() StallInvestigation {
	return StallInvestigation{ID: "S003", StallIndex: 2, Stall: PlayerStall{StartMS: 2000, EndMS: 3000, DurationMS: 1000}, Correlation: StallCorrelation{
		Source: "same_capture", WindowStartMS: 0, WindowEndMS: 6000,
		BufferSamples: []PlayerBufferSample{assessmentSample(2000, 1500, 100, 2, 0), assessmentSample(2500, 1500, 115, 2, 0), assessmentSample(3000, 1500, 130, 2, 0)},
		Keyframes:     []Event{{Kind: "video", ArrivalMS: 1500, Keyframe: true}},
		Findings: []Finding{
			{ID: "S003-E001", Code: "stall_selected", StartMS: 2000, EndMS: 3000},
			{ID: "S003-E002", Code: "stall_buffer_window", StartMS: 0, EndMS: 6000},
		},
	}}
}

func assessmentGap(item *StallInvestigation, start, end int64) {
	item.Correlation.Gaps = append(item.Correlation.Gaps, ReceptionGap{Kind: "video", StartMS: start, EndMS: end, DurationMS: end - start})
	item.Correlation.Findings = append(item.Correlation.Findings, Finding{ID: "S003-E003", Code: "stall_video_receive_gap", StartMS: start, EndMS: end})
}

func assessmentDirection(result *StallAssessment, code string) *StallDirection {
	for index := range result.Directions {
		if result.Directions[index].Code == code {
			return &result.Directions[index]
		}
	}
	return nil
}

func assertAssessmentReferences(t *testing.T, item StallInvestigation, result *StallAssessment) {
	t.Helper()
	ids := map[string]bool{}
	for _, findings := range [][]Finding{item.Correlation.Findings, result.Findings} {
		for _, finding := range findings {
			if finding.ID == "" || ids[finding.ID] {
				t.Fatalf("empty or duplicate evidence ID: %q", finding.ID)
			}
			ids[finding.ID] = true
		}
	}
	for _, finding := range result.Findings {
		if !strings.HasPrefix(finding.ID, item.ID+"-A") {
			t.Fatalf("assessment finding escaped its investigation namespace: %q", finding.ID)
		}
	}
	for _, direction := range result.Directions {
		if len(direction.EvidenceIDs) == 0 {
			t.Fatalf("direction %q has no concrete evidence reference", direction.Code)
		}
		for _, id := range direction.EvidenceIDs {
			if !ids[id] {
				t.Fatalf("direction %q references nonexistent evidence %q", direction.Code, id)
			}
		}
	}
}

func TestStallAssessmentReceiveGapRequiresSameCaptureAndStallOverlap(t *testing.T) {
	for _, test := range []struct {
		name, source string
		start, end   int64
		buffer       int64
		candidate    bool
	}{
		{"same capture overlapping", "same_capture", 1800, 2800, 100, true},
		{"gap before selected stall", "same_capture", 500, 1500, 100, false},
		{"gap after selected stall", "same_capture", 3500, 4500, 100, false},
		{"only brief overlap", "same_capture", 1000, 2100, 100, false},
		{"buffer remains rich", "same_capture", 1800, 2800, 1500, false},
		{"HLS independent connection", "independent_pull", 1800, 2800, 100, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := assessmentFixture()
			item.Correlation.Source = test.source
			item.Correlation.BufferSamples[1].BufferMS = test.buffer
			assessmentGap(&item, test.start, test.end)
			result := assessStall(item)
			direction := assessmentDirection(result, "source_or_transport")
			if (direction != nil) != test.candidate {
				t.Fatalf("supply candidate=%v, want %v: %+v", direction, test.candidate, result)
			}
			if direction != nil && (direction.Status != "candidate" || !slices.Contains(direction.EvidenceIDs, "S003-E003") || !slices.Contains(direction.EvidenceIDs, "S003-E002")) {
				t.Fatalf("supply direction lacks measured receive/buffer evidence: %+v", direction)
			}
			if test.source == "independent_pull" && !strings.Contains(strings.Join(result.Limitations, " "), "独立连接") {
				t.Fatal("HLS independent-connection limit omitted")
			}
			assertAssessmentReferences(t, item, result)
		})
	}
}

func TestStallAssessmentPlaybackCandidateNeedsCurrentFrameDelayAndBufferCoverage(t *testing.T) {
	for _, test := range []struct {
		name      string
		modify    func(*StallInvestigation)
		candidate bool
	}{
		{"rich buffer and current delayed callback", func(item *StallInvestigation) {
			item.Correlation.BufferSamples[2].Video.FrameAgeMS = assessmentNumber(1000)
		}, true},
		{"historical cumulative maximum alone", func(item *StallInvestigation) {
			for index := range item.Correlation.BufferSamples {
				item.Correlation.BufferSamples[index].Video.MaxFrameGapMS = assessmentNumber(2500)
			}
		}, false},
		{"frame callback unsupported", func(item *StallInvestigation) {
			for index := range item.Correlation.BufferSamples {
				item.Correlation.BufferSamples[index].Video.FrameAgeMS = nil
			}
		}, false},
		{"delay below threshold", func(item *StallInvestigation) {
			item.Correlation.BufferSamples[1].Video.FrameAgeMS = assessmentNumber(999)
		}, false},
		{"buffer not continuously rich", func(item *StallInvestigation) {
			item.Correlation.BufferSamples[0].BufferMS = 500
			item.Correlation.BufferSamples[1].Video.FrameAgeMS = assessmentNumber(1500)
		}, false},
		{"isolated delayed sample", func(item *StallInvestigation) {
			item.Correlation.BufferSamples = item.Correlation.BufferSamples[1:2]
			item.Correlation.BufferSamples[0].Video.FrameAgeMS = assessmentNumber(1500)
		}, false},
		{"widely spaced endpoints do not establish buffer coverage", func(item *StallInvestigation) {
			item.Stall.EndMS, item.Stall.DurationMS = 12000, 10000
			item.Correlation.WindowEndMS = 15000
			item.Correlation.BufferSamples = []PlayerBufferSample{assessmentSample(2000, 1500, 100, 2, 0), assessmentSample(12000, 1500, 400, 2, 1500)}
		}, false},
		{"historical callback age before short stall cannot establish delay", func(item *StallInvestigation) {
			item.Stall.EndMS, item.Stall.DurationMS = 2600, 600
			item.Correlation.BufferSamples = []PlayerBufferSample{
				assessmentSample(1900, 1500, 100, 2, 60000),
				assessmentSample(2000, 1500, 100, 2, 60000),
				assessmentSample(2300, 1500, 110, 2, 60000),
				assessmentSample(2600, 1500, 120, 2, 60000),
			}
			item.Correlation.BufferSamples[0].Paused = true
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := assessmentFixture()
			test.modify(&item)
			result := assessStall(item)
			direction := assessmentDirection(result, "local_playback_or_schedule")
			if (direction != nil) != test.candidate {
				t.Fatalf("playback candidate=%v, want %v: %+v", direction, test.candidate, result)
			}
			if direction != nil && direction.Status != "candidate" {
				t.Fatalf("local playback was presented as proven root cause: %+v", direction)
			}
			assertAssessmentReferences(t, item, result)
		})
	}
}

func TestStallAssessmentReceiveGapDoesNotExplainDistantBufferUnderflow(t *testing.T) {
	item := assessmentFixture()
	item.Stall.EndMS, item.Stall.DurationMS = 12000, 10000
	item.Correlation.WindowEndMS = 15000
	item.Correlation.BufferSamples = []PlayerBufferSample{
		assessmentSample(2000, 1500, 100, 2, 0),
		assessmentSample(2600, 1500, 118, 2, 0),
		assessmentSample(11000, 1500, 370, 2, 0),
		assessmentSample(12000, 0, 400, 2, 0),
	}
	assessmentGap(&item, 1800, 2600)
	result := assessStall(item)
	if assessmentDirection(result, "buffer_underflow") == nil || assessmentDirection(result, "source_or_transport") != nil {
		t.Fatalf("an earlier receive gap was blamed for distant later buffer depletion: %+v", result)
	}
	assertAssessmentReferences(t, item, result)
}

func TestStallAssessmentCounterResetDoesNotEraseLaterObservedDrops(t *testing.T) {
	item := assessmentFixture()
	item.Correlation.BufferSamples = []PlayerBufferSample{
		assessmentSample(2000, 1500, 100, 5, 0),
		assessmentSample(2500, 1500, 10, 1, 0),
		assessmentSample(3000, 1500, 25, 4, 0),
	}
	result := assessStall(item)
	if assessmentDirection(result, "frame_drops") == nil || len(result.Findings) != 1 || !strings.Contains(result.Findings[0].Message, "增量 15、丢帧增量 3") {
		t.Fatalf("reset interval was subtracted from valid later deltas: %+v", result)
	}
	assertAssessmentReferences(t, item, result)
}

func TestStallAssessmentFrameDropsRequireValidConsecutiveCounterDeltas(t *testing.T) {
	for _, test := range []struct {
		name    string
		modify  func(*PlayerBufferSample, *PlayerBufferSample)
		dropped bool
	}{
		{"positive observed dropped delta", func(first, last *PlayerBufferSample) {
			last.Video.DroppedFrames = assessmentNumber(5)
		}, true},
		{"raw historical drops are not new drops", func(first, last *PlayerBufferSample) {}, false},
		{"observed zero counters", func(first, last *PlayerBufferSample) {
			for _, sample := range []*PlayerBufferSample{first, last} {
				sample.Video.TotalFrames = assessmentNumber(0)
				sample.Video.DroppedFrames = assessmentNumber(0)
			}
		}, false},
		{"unsupported differs from observed zero", func(first, last *PlayerBufferSample) {
			first.Video = &PlayerVideoSample{QualitySource: "unavailable"}
			last.Video = &PlayerVideoSample{QualitySource: "unavailable"}
		}, false},
		{"legacy no video", func(first, last *PlayerBufferSample) {
			first.Video, last.Video = nil, nil
		}, false},
		{"counter reset", func(first, last *PlayerBufferSample) {
			last.Video.TotalFrames = assessmentNumber(10)
			last.Video.DroppedFrames = assessmentNumber(1)
		}, false},
		{"drop delta exceeds total delta", func(first, last *PlayerBufferSample) {
			last.Video.TotalFrames = assessmentNumber(101)
			last.Video.DroppedFrames = assessmentNumber(5)
		}, false},
		{"quality API changes", func(first, last *PlayerBufferSample) {
			last.Video.QualitySource = "webkit"
			last.Video.DroppedFrames = assessmentNumber(5)
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := assessmentFixture()
			item.Correlation.BufferSamples = []PlayerBufferSample{item.Correlation.BufferSamples[0], item.Correlation.BufferSamples[2]}
			test.modify(&item.Correlation.BufferSamples[0], &item.Correlation.BufferSamples[1])
			result := assessStall(item)
			direction := assessmentDirection(result, "frame_drops")
			if (direction != nil) != test.dropped {
				t.Fatalf("observed drops=%v, want %v: %+v", direction, test.dropped, result)
			}
			if direction != nil && direction.Status != "observed" {
				t.Fatalf("drop evidence was presented as causal diagnosis: %+v", direction)
			}
			assertAssessmentReferences(t, item, result)
		})
	}
}

func TestStallAssessmentExcludedStatesBreakCounterContinuity(t *testing.T) {
	for _, state := range []string{"paused", "seeking", "hidden"} {
		t.Run(state, func(t *testing.T) {
			item := assessmentFixture()
			middle := &item.Correlation.BufferSamples[1]
			middle.BufferMS = 0
			middle.Video.FrameAgeMS = assessmentNumber(2000)
			switch state {
			case "paused":
				middle.Paused = true
			case "seeking":
				middle.Seeking = true
			case "hidden":
				middle.Hidden = true
			}
			item.Correlation.BufferSamples[2].Video.DroppedFrames = assessmentNumber(8)
			result := assessStall(item)
			for _, code := range []string{"frame_drops", "buffer_underflow", "local_playback_or_schedule"} {
				if direction := assessmentDirection(result, code); direction != nil {
					t.Fatalf("%s sample contaminated %s: %+v", state, code, direction)
				}
			}
			assertAssessmentReferences(t, item, result)
			// An eligible empty-buffer point remains an observation, but a gap
			// spanning an excluded playback state cannot establish its cause.
			item.Correlation.BufferSamples[0].BufferMS = 100
			assessmentGap(&item, 1800, 2800)
			result = assessStall(item)
			if assessmentDirection(result, "buffer_underflow") == nil || assessmentDirection(result, "source_or_transport") != nil {
				t.Fatalf("excluded %s state did not constrain supply assessment: %+v", state, result)
			}
			assertAssessmentReferences(t, item, result)
		})
	}
}

func TestStallAssessmentIgnoresSamplesOutsideSelectedStall(t *testing.T) {
	item := assessmentFixture()
	item.Correlation.BufferSamples = append([]PlayerBufferSample{assessmentSample(1000, 0, 20, 10, 1000)}, item.Correlation.BufferSamples...)
	item.Correlation.BufferSamples = append(item.Correlation.BufferSamples, assessmentSample(4000, 0, 500, 100, 3000))
	before, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	result := assessStall(item)
	if len(result.Findings) != 0 || len(result.Directions) != 1 || result.Directions[0].Code != "insufficient_evidence" {
		t.Fatalf("adjacent evidence outside the selected stall caused a diagnosis: %+v", result)
	}
	after, err := json.Marshal(item)
	if err != nil || string(before) != string(after) {
		t.Fatal("assessment mutated the underlying correlation evidence")
	}
	if again := assessStall(item); !reflect.DeepEqual(result, again) {
		t.Fatal("assessment is not reproducible from the same offline evidence")
	}
	assertAssessmentReferences(t, item, result)
}

func TestStallAssessmentLegacyEvidenceRemainsInsufficient(t *testing.T) {
	for _, samples := range [][]PlayerBufferSample{
		nil,
		{{OffsetMS: 2500, BufferMS: 1500, ReadyState: 3}},
		{{OffsetMS: 2000, BufferMS: 1500, ReadyState: 3}, {OffsetMS: 3000, BufferMS: 1500, ReadyState: 3}},
	} {
		item := assessmentFixture()
		item.Correlation.BufferSamples = samples
		item.Correlation.Keyframes = nil
		result := assessStall(item)
		if direction := assessmentDirection(result, "insufficient_evidence"); direction == nil || direction.Status != "insufficient" {
			t.Fatalf("missing renderer/connection evidence was converted into a diagnosis: %+v", result)
		}
		if len(result.Findings) != 0 || !strings.Contains(strings.Join(result.Limitations, " "), "缺少可确认的关键帧") {
			t.Fatalf("legacy missing video or keyframe data was treated as observed zero: %+v", result)
		}
		assertAssessmentReferences(t, item, result)
	}
}

func assessmentRewriteBundle(t *testing.T, source []byte, modify func(*investigationLedger)) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, entry := range reader.File {
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(file)
		if err := errors.Join(readErr, file.Close()); err != nil {
			t.Fatal(err)
		}
		files[entry.Name] = data
	}
	var ledger investigationLedger
	if err := json.Unmarshal(files[investigationName], &ledger); err != nil {
		t.Fatal(err)
	}
	modify(&ledger)
	files[investigationName], err = json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(files[manifestName], &manifest); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(files[investigationName])
	manifest.StallEvidence = &Artifact{Bytes: int64(len(files[investigationName])), SHA256: hex.EncodeToString(hash[:])}
	files[manifestName], err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var result bytes.Buffer
	writer := zip.NewWriter(&result)
	for name, data := range files {
		target, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := target.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func TestStallAssessmentBundleReplayRejectsTamperingAndAcceptsLegacyAbsence(t *testing.T) {
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
	item := StallInvestigation{ID: "S001", StallIndex: 1, PlayerRevision: report.Player.Revision,
		Status: "partial", CreatedAt: report.Manifest.Observation.StartedAt, UpdatedAt: report.Manifest.Observation.StartedAt,
		Stall: report.Player.Stalls[0], Correlation: correlation, ModelState: "unconfigured",
		Media: InvestigationMedia{Status: "unavailable", Reason: "No decoded assets in this offline fixture", Assets: []DerivedArtifact{}},
	}
	item.Assessment = assessStall(item)
	assertAssessmentReferences(t, item, item.Assessment)
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
	got, err := service.Detail(replay.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Report, &report) {
		t.Fatal("offline import did not preserve the deterministic assessment and supporting evidence")
	}
	for _, test := range []struct {
		name   string
		modify func(*StallAssessment)
	}{
		{"rewritten conclusion", func(value *StallAssessment) { value.Conclusion = "the network is definitively at fault" }},
		{"candidate promoted to proof", func(value *StallAssessment) { value.Directions[len(value.Directions)-1].Status = "observed" }},
		{"invented evidence reference", func(value *StallAssessment) { value.Directions[0].EvidenceIDs = []string{"S001-E999"} }},
		{"invented assessment finding", func(value *StallAssessment) {
			value.Findings = append(value.Findings, Finding{ID: "S001-A999", Code: "invented_root_cause", Message: "unsupported"})
		}},
		{"unrecognized algorithm version", func(value *StallAssessment) { value.Version = "stall-assessment/999" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			modified := assessmentRewriteBundle(t, archive.Bytes(), func(ledger *investigationLedger) { test.modify(ledger.Items[0].Assessment) })
			if _, err := service.Import(bytes.NewReader(modified)); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "排查分层") {
				t.Fatalf("tampered assessment survived a valid artifact checksum: %v", err)
			}
		})
	}
	legacy := assessmentRewriteBundle(t, archive.Bytes(), func(ledger *investigationLedger) { ledger.Items[0].Assessment = nil })
	legacyRun, err := service.Import(bytes.NewReader(legacy))
	if err != nil {
		t.Fatalf("old evidence without an assessment was rejected: %v", err)
	}
	legacyDetail, err := service.Detail(legacyRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacyItem := legacyDetail.Report.StallInvestigations[0]
	if legacyItem.Assessment != nil || !reflect.DeepEqual(legacyItem.Correlation, item.Correlation) {
		t.Fatal("legacy import silently manufactured an assessment or changed its raw correlation evidence")
	}
}
