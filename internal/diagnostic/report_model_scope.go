package diagnostic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// The projection contains every numeric case but excludes its AI explanation,
// job timestamps and model state. Source hashes bind the underlying recording;
// the case ledger hash is excluded because it includes those explanations.
func reportModelScopeSHA256(report Report) (string, error) {
	report = canonicalReportModel(report)
	data, err := modelInput(report)
	if err != nil {
		return "", err
	}
	scope := struct {
		Input                 json.RawMessage
		Stream, Timeline      Artifact
		Player, PlayerNetwork *Artifact
	}{Input: data, Stream: report.Manifest.Stream, Timeline: report.Manifest.Timeline, Player: report.Manifest.Player, PlayerNetwork: report.Manifest.PlayerNetwork}
	canonical, err := json.Marshal(scope)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:]), nil
}
func reportModelImageReferencesMatch(report Report, refs []ModelImageReference) bool {
	if len(refs) > MaxModelImages {
		return false
	}
	seenNames, seenHashes := map[string]bool{}, map[string]bool{}
	cases := rankedReportModelCases(report)
	for _, ref := range refs {
		if seenNames[ref.Name] || seenHashes[ref.SHA256] {
			return false
		}
		seenNames[ref.Name] = true
		seenHashes[ref.SHA256] = true
		found := false
		for _, item := range cases {
			if item.ID != ref.CaseID {
				continue
			}
			assets, err := modelImageAssets(item)
			if err != nil {
				return false
			}
			for index, asset := range assets {
				if validModelImageReference(ref, index, item.ID) && ref.Name == asset.Name && ref.Artifact == asset.Artifact && reflect.DeepEqual(ref.MediaTimeMS, asset.MediaTimeMS) {
					found = true
					break
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func validateReportModelInput(dir string, report Report, evidence *ReportModelInputEvidence) error {
	invalid := func() error { return fmt.Errorf("%w: 整份报告模型输入与当前证据不一致", ErrInvalid) }
	if evidence == nil || evidence.Version != 1 || evidence.Selection != reportModelSelection || evidence.PlayerRevision != reportModelPlayerRevision(report) || len(evidence.Cases) > maxStallInvestigations || !slices.Equal(evidence.Cases, reportModelCaseReferences(report)) || evidence.RequestedMode != ImageInputAuto && evidence.RequestedMode != ImageInputText {
		return invalid()
	}
	scope, err := reportModelScopeSHA256(report)
	if err != nil || evidence.ReportSHA256 != scope {
		return invalid()
	}
	if evidence.Mode == "vision" {
		if evidence.RequestedMode != ImageInputAuto || evidence.FallbackCode != "" || len(evidence.Images) == 0 || len(evidence.AttemptedImages) != 0 {
			return invalid()
		}
	} else if evidence.Mode == "text" {
		if len(evidence.Images) != 0 {
			return invalid()
		}
		switch evidence.FallbackCode {
		case "disabled":
			if evidence.RequestedMode != ImageInputText {
				return invalid()
			}
		case "provider_text_only", "model_text_only", "no_images", "images_invalid", "images_unsupported":
			if evidence.RequestedMode != ImageInputAuto {
				return invalid()
			}
		default:
			return invalid()
		}
		if (evidence.FallbackCode == "images_unsupported") != (len(evidence.AttemptedImages) > 0) {
			return invalid()
		}
	} else {
		return invalid()
	}
	refs := evidence.Images
	if evidence.Mode == "text" {
		refs = evidence.AttemptedImages
	}
	if !reportModelImageReferencesMatch(report, refs) {
		return invalid()
	}
	if len(refs) == 0 {
		if evidence.OmittedImageCases != 0 {
			return invalid()
		}
		if evidence.FallbackCode == "disabled" || evidence.FallbackCode == "provider_text_only" || evidence.FallbackCode == "model_text_only" {
			return nil
		}
	}
	images, _, omitted, selectionErr := selectReportModelImages(dir, report)
	if evidence.FallbackCode == "images_invalid" {
		if selectionErr != nil && len(images) == 0 {
			return nil
		}
		return invalid()
	}
	if selectionErr != nil {
		return invalid()
	}
	if evidence.FallbackCode == "no_images" {
		if len(images) == 0 {
			return nil
		}
		return invalid()
	}
	expected := modelImageReferences(images)
	if !sameReportModelImageRefs(refs, expected) || evidence.OmittedImageCases != omitted {
		return invalid()
	}
	return nil
}
func sameReportModelImageRefs(a, b []ModelImageReference) bool {
	left, errA := json.Marshal(a)
	right, errB := json.Marshal(b)
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return errA == nil && errB == nil && bytes.Equal(left, right)
}

// The returned set is complete, so validators can use it directly and text
// fallbacks can never cite pictures from the rejected request.
func reportModelFindings(report Report, evidence *ReportModelInputEvidence) []Finding {
	report = canonicalReportModel(report)
	result := append([]Finding(nil), report.Findings...)
	if evidence != nil && evidence.Mode == "vision" && evidence.Version == 1 && reportModelImageReferencesMatch(report, evidence.Images) {
		result = append(result, modelImageFindings(evidence.Images)...)
	}
	return result
}
func reportExplanationReusable(dir string, report Report, explanation Explanation, model Explainer) bool {
	return explanation.InputEvidence == nil && explanation.ReportInputEvidence != nil && explanation.ReportInputEvidence.RequestedMode == modelImageInput(model) && validateReportModelInput(dir, report, explanation.ReportInputEvidence) == nil && validExplanation(explanation, reportModelFindings(report, explanation.ReportInputEvidence))
}

func canonicalReportModel(report Report) Report {
	report.modelScope = "report"
	report.StallInvestigations = rankedReportModelCases(report)
	for index := range report.StallInvestigations {
		report.StallInvestigations[index].Explanation = nil
	}
	allowed := map[string]bool{}
	for _, item := range report.StallInvestigations {
		allowed[item.ID] = true
	}
	findings := make([]Finding, 0, len(report.Findings))
	for _, finding := range report.Findings {
		caseID, suffix, caseFinding := strings.Cut(finding.ID, "-")
		if caseFinding && investigationMediaID.MatchString(caseID) && (strings.HasPrefix(suffix, "E") || strings.HasPrefix(suffix, "A") || strings.HasPrefix(suffix, "I")) {
			if !allowed[caseID] || strings.HasPrefix(suffix, "I") {
				continue
			}
		}
		findings = append(findings, finding)
	}
	report.Findings = findings
	return report
}
