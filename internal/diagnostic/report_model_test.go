package diagnostic

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

type reportModelTestProvider struct {
	mode        string
	unsupported bool
	vision      func(context.Context, Report, []ModelImage) (Explanation, error)
	text        func(context.Context, Report) (Explanation, error)
}

func (m *reportModelTestProvider) Identity() ModelIdentity {
	return ModelIdentity{Provider: ProviderOpenAICompatible, Model: "report-image-test"}
}
func (m *reportModelTestProvider) ImageInputMode() string {
	if m.mode == "text" {
		return "text"
	}
	return "auto"
}
func (m *reportModelTestProvider) SupportsImages() bool { return !m.unsupported }
func (m *reportModelTestProvider) Explain(ctx context.Context, report Report) (Explanation, error) {
	if m.text == nil {
		return Explanation{}, ErrProvider
	}
	return m.text(ctx, report)
}
func (m *reportModelTestProvider) ExplainWithImages(ctx context.Context, report Report, images []ModelImage) (Explanation, error) {
	if m.vision == nil {
		return Explanation{}, ErrProvider
	}
	return m.vision(ctx, report, images)
}
func reportModelAnswer(id string) Explanation {
	return Explanation{Provider: ProviderOpenAICompatible, Model: "report-image-test", Summary: "整份报告按观测与优先级排查", Hypotheses: []Hypothesis{{Claim: "先核对对应时间与源画面", EvidenceIDs: []string{id}, NextCheck: "从观测开始补充验证", Priority: "P1"}}}
}
func reportModelFixture(t *testing.T) (*Service, string, Report) {
	t.Helper()
	service, id, base := openImageStallTestRun(t)
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	report := *detail.Report
	report.Findings = append(report.Findings, Finding{ID: "GLOBAL", Code: "global", Severity: "info", Message: "全局数字证据"})
	template := report.StallInvestigations[0]
	report.StallInvestigations = nil
	for index := 1; index <= 5; index++ {
		item := template
		item.ID = []string{"S001", "S002", "S003", "S004", "S005"}[index-1]
		item.Stall.StartMS = int64(index * 1000)
		item.Stall.DurationMS = []int64{1500, 5000, 3000, 4000, 9000}[index-1]
		item.Stall.EndMS = item.Stall.StartMS + item.Stall.DurationMS
		severity := []string{"info", "error", "warn", "error", "error"}[index-1]
		item.Assessment = &StallAssessment{Version: stallAssessmentVersion, Findings: []Finding{{ID: item.ID + "-A001", Severity: severity, Code: "case", Message: "案例数字观测"}}}
		item.Explanation = &Explanation{Model: "historical", Summary: "这段历史模型文字不能再作为输入"}
		item.Media = InvestigationMedia{Status: "ready", Assets: []DerivedArtifact{}}
		if err := os.MkdirAll(filepath.Join(service.runDir(id), "stalls", item.ID), 0700); err != nil {
			t.Fatal(err)
		}
		for imageIndex, name := range []string{"before.jpg", "during.jpg", "after.jpg"} {
			picture := image.NewRGBA(image.Rect(0, 0, 8, 8))
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					picture.Set(x, y, color.RGBA{R: uint8(20 * index), G: uint8(50 + imageIndex*60), B: 170, A: 255})
				}
			}
			var encoded bytes.Buffer
			if err := jpeg.Encode(&encoded, picture, nil); err != nil {
				t.Fatal(err)
			}
			name = "stalls/" + item.ID + "/" + name
			artifact := investigationMediaSave(t, service.runDir(id), name, encoded.Bytes())
			at := int64(imageIndex * 300)
			item.Media.Assets = append(item.Media.Assets, DerivedArtifact{Name: name, Kind: "screenshot", Artifact: artifact, MediaTimeMS: &at})
		}
		if index == 5 {
			item.PlayerRevision--
		}
		report.StallInvestigations = append(report.StallInvestigations, item)
		report.Findings = append(report.Findings, item.Assessment.Findings...)
	}
	if err := persistReport(service.runDir(id), report); err != nil {
		t.Fatal(err)
	}
	_ = base
	return service, id, report
}
func TestReportModelSelectionRanksCasesPreservesIDsAndDeduplicates(t *testing.T) {
	service, id, report := reportModelFixture(t)
	images, cases, omitted, err := selectReportModelImages(service.runDir(id), report)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cases, []ReportModelCaseReference{{"S002", 1}, {"S004", 1}, {"S003", 1}, {"S001", 1}}) || omitted != 1 {
		t.Fatalf("wrong numeric case rank/omissions: %+v %d", cases, omitted)
	}
	for index, expected := range []string{"S002-I002", "S004-I002", "S003-I002"} {
		if images[index].Reference.ID != expected {
			t.Fatalf("representatives renumbered: %+v", modelImageReferences(images))
		}
	}
	// A duplicate representative may use another available picture from that
	// case, preserving its original case-local image ID.
	target := &report.StallInvestigations[3].Media.Assets[1]
	source := report.StallInvestigations[1].Media.Assets[1]
	data, err := os.ReadFile(filepath.Join(service.runDir(id), source.Name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service.runDir(id), target.Name), data, 0600); err != nil {
		t.Fatal(err)
	}
	target.Artifact = source.Artifact
	images, _, _, err = selectReportModelImages(service.runDir(id), report)
	if err != nil {
		t.Fatal(err)
	}
	if images[1].Reference.ID != "S004-I001" {
		t.Fatalf("duplicate SHA used instead of another case picture: %+v", modelImageReferences(images))
	}
	// With one case, its remaining before/after slots fill the global budget.
	report.StallInvestigations = report.StallInvestigations[1:2]
	images, _, omitted, err = selectReportModelImages(service.runDir(id), report)
	if err != nil || len(images) != 3 || omitted != 0 {
		t.Fatal(images, omitted, err)
	}
	if images[0].Reference.ID != "S002-I002" || images[1].Reference.ID != "S002-I001" || images[2].Reference.ID != "S002-I003" {
		t.Fatal("before/after fill changed IDs")
	}
}
func TestReportModelSummaryVisionCachePolicyAndScope(t *testing.T) {
	service, id, report := reportModelFixture(t)
	var imageCalls, textCalls atomic.Int32
	image := func(_ context.Context, received Report, images []ModelImage) (Explanation, error) {
		imageCalls.Add(1)
		if received.modelScope != "report" || len(received.StallInvestigations) != 4 || len(images) != 3 {
			t.Fatal("full current-case timelines missing")
		}
		for _, item := range received.StallInvestigations {
			if item.ID == "S005" || item.Explanation != nil {
				t.Fatal("stale case or previous AI explanation leaked")
			}
		}
		for _, finding := range received.Findings {
			if strings.HasPrefix(finding.ID, "S005-") {
				t.Fatal("stale finding entered model")
			}
		}
		answer := reportModelAnswer(images[0].Reference.ID)
		answer.InputEvidence = &ModelInputEvidence{Version: 99}
		answer.ReportInputEvidence = &ReportModelInputEvidence{Version: 99}
		return answer, nil
	}
	text := func(_ context.Context, received Report) (Explanation, error) {
		textCalls.Add(1)
		for _, finding := range received.Findings {
			if strings.Contains(finding.ID, "-I") {
				t.Fatal("text input contains image findings")
			}
		}
		return reportModelAnswer("GLOBAL"), nil
	}
	service.SetModel(&reportModelTestProvider{vision: image, text: text})
	first, err := service.Explain(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	audit := first.ReportInputEvidence
	if first.InputEvidence != nil || audit == nil || audit.Version != 1 || audit.Mode != "vision" || len(audit.Cases) != 4 || len(audit.Images) != 3 || audit.OmittedImageCases != 1 || len(audit.ReportSHA256) != 64 {
		t.Fatalf("trusted report audit missing: %+v", first)
	}
	if _, err := service.Explain(t.Context(), id); err != nil || imageCalls.Load() != 1 {
		t.Fatal("valid cache repeated image request", err)
	}
	service.SetModel(&reportModelTestProvider{mode: "text", vision: image, text: text})
	disabled, err := service.Explain(t.Context(), id)
	if err != nil || disabled.ReportInputEvidence.FallbackCode != "disabled" || textCalls.Load() != 1 {
		t.Fatal("same-model policy change reused vision cache", err)
	}
	service.SetModel(&reportModelTestProvider{vision: image, text: text})
	if _, err := service.Explain(t.Context(), id); err != nil || imageCalls.Load() != 2 {
		t.Fatal("auto policy reused text cache", err)
	}
	// AI case text is not a numeric input. A changed source timing is.
	report.StallInvestigations[0].Explanation.Summary = "changed case text"
	if err := persistReport(service.runDir(id), report); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Explain(t.Context(), id); err != nil || imageCalls.Load() != 2 {
		t.Fatal("case AI text alone invalidated numeric scope", err)
	}
	report.Metrics.VideoFrames++
	if err := persistReport(service.runDir(id), report); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Explain(t.Context(), id); err != nil || imageCalls.Load() != 3 {
		t.Fatal("changed numeric report reused old summary", err)
	}
}
func TestReportModelSummaryFallbackAndInvalidCases(t *testing.T) {
	for _, mode := range []string{"unsupported", "provider_text_only", "no_images", "invalid_images", "auth"} {
		t.Run(mode, func(t *testing.T) {
			service, id, report := reportModelFixture(t)
			var visionCalls, textCalls int
			model := &reportModelTestProvider{vision: func(context.Context, Report, []ModelImage) (Explanation, error) {
				visionCalls++
				if mode == "auth" {
					return Explanation{}, ErrProvider
				}
				return Explanation{}, ErrModelImagesUnsupported
			}, text: func(_ context.Context, received Report) (Explanation, error) {
				textCalls++
				if len(received.StallInvestigations) != 4 {
					t.Fatal("fallback lost case timing")
				}
				for _, finding := range received.Findings {
					if strings.Contains(finding.ID, "-I") {
						t.Fatal("fallback falsely retained images")
					}
				}
				return reportModelAnswer("GLOBAL"), nil
			}}
			switch mode {
			case "provider_text_only":
				model.unsupported = true
			case "no_images":
				for index := range report.StallInvestigations {
					report.StallInvestigations[index].Media.Assets = nil
				}
			case "invalid_images":
				for _, item := range report.StallInvestigations {
					if err := os.WriteFile(filepath.Join(service.runDir(id), item.Media.Assets[0].Name), []byte("invalid"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := persistReport(service.runDir(id), report); err != nil {
				t.Fatal(err)
			}
			service.SetModel(model)
			answer, err := service.Explain(t.Context(), id)
			if mode == "auth" {
				if !errors.Is(err, ErrProvider) || textCalls != 0 || visionCalls != 1 {
					t.Fatal("ordinary vision error retried", err)
				}
				return
			}
			if err != nil || answer.ReportInputEvidence == nil || answer.ReportInputEvidence.Mode != "text" || textCalls != 1 {
				t.Fatalf("text fallback failed: %+v %v", answer, err)
			}
			audit := answer.ReportInputEvidence
			expected := mode
			if mode == "unsupported" {
				expected = "images_unsupported"
			}
			if mode == "invalid_images" {
				expected = "images_invalid"
			}
			if audit.FallbackCode != expected || len(audit.Images) != 0 {
				t.Fatal(audit)
			}
			if mode == "unsupported" {
				if visionCalls != 1 || len(audit.AttemptedImages) != 3 || audit.OmittedImageCases != 1 {
					t.Fatal("actual attempted images lost")
				}
			} else if visionCalls != 0 || len(audit.AttemptedImages) != 0 || audit.OmittedImageCases != 0 {
				t.Fatal("unsent images marked as transmitted")
			}
		})
	}
}
func TestReportModelSummaryRejectsChangedPixelsAndNumericScopeInFlight(t *testing.T) {
	for _, change := range []string{"pixels", "timing", "model", "cancel"} {
		t.Run(change, func(t *testing.T) {
			service, id, report := reportModelFixture(t)
			started, release := make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			service.SetModel(&reportModelTestProvider{vision: func(_ context.Context, _ Report, images []ModelImage) (Explanation, error) {
				close(started)
				<-release
				return reportModelAnswer(images[0].Reference.ID), nil
			}})
			done := make(chan error, 1)
			go func() { _, err := service.Explain(ctx, id); done <- err }()
			<-started
			expected := ErrReportChanged
			switch change {
			case "pixels":
				if err := os.WriteFile(filepath.Join(service.runDir(id), report.StallInvestigations[1].Media.Assets[1].Name), []byte("changed after load"), 0600); err != nil {
					t.Fatal(err)
				}
			case "timing":
				report.StallInvestigations[0].Correlation.Notes = append(report.StallInvestigations[0].Correlation.Notes, "new numeric scope note")
				if err := persistReport(service.runDir(id), report); err != nil {
					t.Fatal(err)
				}
			case "model":
				service.SetModel(&reportModelTestProvider{mode: "text"})
				expected = ErrModelChanged
			case "cancel":
				cancel()
				expected = context.Canceled
			}
			close(release)
			if err := <-done; !errors.Is(err, expected) {
				t.Fatalf("stale summary saved: %v want%v", err, expected)
			}
			detail, err := service.Detail(id)
			if err != nil || detail.Explanation != nil {
				t.Fatal("stale/canceled summary persisted", err)
			}
		})
	}
}

func TestReportModelSummaryCacheRechecksHashesAndSkipsInvalidCaseImages(t *testing.T) {
	service, id, report := reportModelFixture(t)
	calls := 0
	service.SetModel(&reportModelTestProvider{vision: func(_ context.Context, received Report, images []ModelImage) (Explanation, error) {
		calls++
		if len(received.StallInvestigations) != 4 {
			t.Fatal("case with invalid images lost numeric timeline")
		}
		return reportModelAnswer(images[0].Reference.ID), nil
	}})
	first, err := service.Explain(t.Context(), id)
	if err != nil || first.ReportInputEvidence.Images[0].CaseID != "S002" {
		t.Fatal(err)
	}
	// Only a nonselected before image changes; the case's complete persisted
	// image set is now invalid, so the old selection is not reusable.
	path := filepath.Join(service.runDir(id), report.StallInvestigations[1].Media.Assets[0].Name)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed pixels"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := service.Explain(t.Context(), id)
	if err != nil || calls != 2 || second.ReportInputEvidence.Images[0].CaseID != "S004" || second.ReportInputEvidence.OmittedImageCases != 1 {
		t.Fatalf("invalid case image reused cached summary: %+v %v calls%d", second, err, calls)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	third, err := service.Explain(t.Context(), id)
	if err != nil || calls != 3 || third.ReportInputEvidence.Images[0].CaseID != "S002" {
		t.Fatal("restored high-priority case did not reenter canonical selection", err)
	}
}
func TestReportModelSelectionAvoidsDuplicatePixelBudget(t *testing.T) {
	service, id, report := reportModelFixture(t)
	report.StallInvestigations = report.StallInvestigations[1:2]
	item := &report.StallInvestigations[0]
	source := item.Media.Assets[1]
	data, err := os.ReadFile(filepath.Join(service.runDir(id), source.Name))
	if err != nil {
		t.Fatal(err)
	}
	for index := range item.Media.Assets {
		if err := os.WriteFile(filepath.Join(service.runDir(id), item.Media.Assets[index].Name), data, 0600); err != nil {
			t.Fatal(err)
		}
		item.Media.Assets[index].Artifact = source.Artifact
	}
	images, cases, omitted, err := selectReportModelImages(service.runDir(id), report)
	if err != nil || len(images) != 1 || images[0].Reference.ID != "S002-I002" || len(cases) != 1 || omitted != 0 {
		t.Fatalf("identical pixels consumed budget: %+v omitted%d %v", modelImageReferences(images), omitted, err)
	}
}
