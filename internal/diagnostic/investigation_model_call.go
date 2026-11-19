package diagnostic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"
)

func (s *Service) callStallModel(ctx context.Context, dir string, model Explainer, report Report, item StallInvestigation) (Explanation, []Finding, error) {
	if ctx.Err() != nil {
		return Explanation{}, report.Findings, ctx.Err()
	}
	audit := &ModelInputEvidence{Version: 1, CaseID: item.ID, PlayerRevision: item.PlayerRevision, RequestedMode: modelImageInput(model), Mode: "text", Images: []ModelImageReference{}}
	text := func(code string, attempted []ModelImageReference) (Explanation, []Finding, error) {
		audit.Mode = "text"
		audit.FallbackCode = code
		audit.Images = []ModelImageReference{}
		audit.AttemptedImages = cloneModelImageReferences(attempted)
		if ctx.Err() != nil {
			return Explanation{}, report.Findings, ctx.Err()
		}
		result, err := model.Explain(ctx, report)
		if ctx.Err() != nil {
			return Explanation{}, report.Findings, ctx.Err()
		}
		if err != nil {
			return Explanation{}, report.Findings, err
		}
		result.InputEvidence = audit
		return result, report.Findings, nil
	}
	if audit.RequestedMode == ImageInputText {
		return text("disabled", nil)
	}
	if capability, ok := model.(ImageCapability); ok && !capability.SupportsImages() {
		return text("provider_text_only", nil)
	}
	vision, ok := model.(ImageExplainer)
	if !ok {
		return text("model_text_only", nil)
	}
	images, err := loadStallModelImages(dir, item)
	if err != nil {
		return text("images_invalid", nil)
	}
	if len(images) == 0 {
		return text("no_images", nil)
	}
	refs := modelImageReferences(images)
	visionReport := report
	visionReport.Findings = append(append([]Finding(nil), report.Findings...), modelImageFindings(refs)...)
	result, err := vision.ExplainWithImages(ctx, visionReport, images)
	if ctx.Err() != nil {
		return Explanation{}, visionReport.Findings, ctx.Err()
	}
	if errors.Is(err, ErrModelImagesUnsupported) {
		return text("images_unsupported", refs)
	}
	if err != nil {
		return Explanation{}, visionReport.Findings, err
	}
	audit.Mode = "vision"
	audit.Images = refs
	result.InputEvidence = audit
	return result, visionReport.Findings, nil
}

// A file changed while the provider was working. Finish the visible job using
// the latest case instead of overwriting its assets or keeping it pending.
func (s *Service) failStaleStallModel(runID, caseID string, revision int) (StallInvestigation, error) {
	s.persistence.Lock()
	defer s.persistence.Unlock()
	report, err := readJSONFile[Report](s.runDir(runID), reportName)
	if err != nil {
		return StallInvestigation{}, err
	}
	item := investigationByID(&report, caseID)
	if item == nil || report.Player == nil || report.Player.Revision != revision || item.PlayerRevision != revision {
		return StallInvestigation{}, ErrReportChanged
	}
	item.Status = "partial"
	item.ModelState, item.ModelError = stallModelFailure(ErrReportChanged)
	item.Explanation = nil
	item.UpdatedAt = time.Now().UTC()
	if err := persistStallReport(s.runDir(runID), &report); err != nil {
		return StallInvestigation{}, err
	}
	return *item, ErrReportChanged
}

func sameStallModelScope(a, b StallInvestigation) bool {
	return a.ID == b.ID && a.PlayerRevision == b.PlayerRevision && a.Stall == b.Stall && a.StallIndex == b.StallIndex && a.IntervalKind == b.IntervalKind && a.CreatedAt.Equal(b.CreatedAt) && sameStallModelJSON(a.Media, b.Media) && sameStallModelJSON(a.Correlation, b.Correlation) && sameStallModelJSON(a.Assessment, b.Assessment)
}

func sameStallModelJSON(a, b any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}
