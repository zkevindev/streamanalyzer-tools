package diagnostic

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestStallAssessmentLegacyUpgradeWithoutModelAndScopedCitations(t *testing.T) {
	service, id, _ := openStallServiceTestRun(t, nil, false)
	dir := service.runDir(id)
	record, err := readPlayerRecording(dir)
	if err != nil {
		t.Fatal(err)
	}
	record.Version = 2
	record.Revision++
	record.Samples = nil
	for index, at := range []int64{100, 3500, 4000, 4500, 5000, 8500} {
		record.Samples = append(record.Samples, PlayerSample{AtMS: at, CurrentMS: 500, BufferMS: 2000, ReadyState: 3,
			Video: &PlayerVideoSample{QualitySource: "playback_quality", TotalFrames: videoTestNumber(int64(index * 20)), DroppedFrames: videoTestNumber(int64(index)), FrameAgeMS: videoTestNumber(min(at, 1200))}})
	}
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
	correlation, err := correlateStall(dir, report, "S001", 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	report.StallInvestigations = []StallInvestigation{{ID: "S001", StallIndex: 1, PlayerRevision: record.Revision,
		Stall: report.Player.Stalls[0], CreatedAt: now, UpdatedAt: now, Status: "partial", ModelState: "unconfigured",
		Correlation: correlation, Media: InvestigationMedia{Status: "unavailable", Assets: []DerivedArtifact{}}}}
	if err := persistStallReport(dir, &report); err != nil {
		t.Fatal(err)
	}
	upgraded, err := service.ExplainStall(context.Background(), id, "S001")
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Assessment == nil || upgraded.ModelState != "unconfigured" || assessmentDirection(upgraded.Assessment, "local_playback_or_schedule") == nil || len(upgraded.Assessment.Findings) != 2 {
		t.Fatalf("local assessment was not generated without a model: %+v", upgraded)
	}
	assessment := upgraded.Assessment
	service.SetModel(stallTestExplainer{name: "assessment-model", call: func(_ context.Context, scoped Report) (Explanation, error) {
		if len(scoped.StallInvestigations) != 1 || !reflect.DeepEqual(scoped.StallInvestigations[0].Assessment, assessment) {
			t.Fatal("model did not receive the selected assessment")
		}
		input, err := modelInput(scoped)
		if err != nil || !bytes.Contains(input, []byte(`"assessment":{"version":"`+stallAssessmentVersion+`"`)) || !bytes.Contains(input, []byte(`"image_pixels_provided":false`)) {
			t.Fatalf("assessment missing from numeric model input: %s %v", input, err)
		}
		findingID := assessment.Findings[0].ID
		if !validExplanation(Explanation{Summary: "核对本机播放", Hypotheses: []Hypothesis{{Claim: "核对调度", EvidenceIDs: []string{findingID}, NextCheck: "比较播放器"}}}, scoped.Findings) {
			t.Fatal("assessment finding was absent from the scoped citation set")
		}
		return Explanation{Provider: ProviderOpenAICompatible, Model: "assessment-model", Summary: "核对本机播放", Hypotheses: []Hypothesis{{Claim: "核对调度", EvidenceIDs: []string{findingID}, NextCheck: "比较播放器"}}}, nil
	}})
	withModel, err := service.ExplainStall(context.Background(), id, "S001")
	if err != nil || withModel.ModelState != "complete" || withModel.Explanation == nil {
		t.Fatalf("assessment citation was rejected: %+v %v", withModel, err)
	}
	saved, err := service.Detail(id)
	if err != nil {
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
	imported, err := service.Detail(replay.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(saved.Report.StallInvestigations[0])
	after, _ := json.Marshal(imported.Report.StallInvestigations[0])
	if !bytes.Equal(before, after) {
		t.Fatalf("offline replay changed the assessment or its model citations\nbefore: %s\nafter: %s", before, after)
	}
}
