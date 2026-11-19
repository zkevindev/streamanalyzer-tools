package diagnostic

import (
	"context"
	"errors"
)

func callReportModel(ctx context.Context, dir string, model Explainer, report Report) (Explanation, error) {
	report = canonicalReportModel(report)
	hash, err := reportModelScopeSHA256(report)
	if err != nil {
		return Explanation{}, err
	}
	audit := &ReportModelInputEvidence{Version: 1, ReportSHA256: hash, PlayerRevision: reportModelPlayerRevision(report), RequestedMode: modelImageInput(model), Mode: "text", Selection: reportModelSelection, Cases: reportModelCaseReferences(report), Images: []ModelImageReference{}}
	text := func(code string, attempted []ModelImageReference, omitted int) (Explanation, error) {
		if ctx.Err() != nil {
			return Explanation{}, ctx.Err()
		}
		audit.Mode = "text"
		audit.FallbackCode = code
		audit.Images = []ModelImageReference{}
		audit.AttemptedImages = cloneModelImageReferences(attempted)
		audit.OmittedImageCases = omitted
		answer, err := model.Explain(ctx, report)
		if ctx.Err() != nil {
			return Explanation{}, ctx.Err()
		}
		if err != nil {
			return Explanation{}, err
		}
		answer.InputEvidence = nil
		answer.ReportInputEvidence = audit
		return answer, nil
	}
	if ctx.Err() != nil {
		return Explanation{}, ctx.Err()
	}
	if audit.RequestedMode == ImageInputText {
		return text("disabled", nil, 0)
	}
	if capability, ok := model.(ImageCapability); ok && !capability.SupportsImages() {
		return text("provider_text_only", nil, 0)
	}
	vision, ok := model.(ImageExplainer)
	if !ok {
		return text("model_text_only", nil, 0)
	}
	images, _, omitted, err := selectReportModelImages(dir, report)
	if err != nil {
		return text("images_invalid", nil, 0)
	}
	if len(images) == 0 {
		return text("no_images", nil, 0)
	}
	refs := modelImageReferences(images)
	audit.Mode = "vision"
	audit.Images = refs
	audit.OmittedImageCases = omitted
	scoped := report
	scoped.Findings = reportModelFindings(report, audit)
	if ctx.Err() != nil {
		return Explanation{}, ctx.Err()
	}
	answer, err := vision.ExplainWithImages(ctx, scoped, images)
	if ctx.Err() != nil {
		return Explanation{}, ctx.Err()
	}
	if errors.Is(err, ErrModelImagesUnsupported) {
		return text("images_unsupported", refs, omitted)
	}
	if err != nil {
		return Explanation{}, err
	}
	answer.InputEvidence = nil
	answer.ReportInputEvidence = audit
	return answer, nil
}
