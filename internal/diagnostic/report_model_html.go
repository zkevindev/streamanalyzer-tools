package diagnostic

import (
	"regexp"
	"sort"
	"strings"
)

type reportModelInputHTML struct {
	Audit         *ReportModelInputEvidence
	Cases         []reportModelCaseHTML
	Used          []modelInputImageHTML
	Attempted     []modelInputImageHTML
	SelectedCases int
	Omitted       []string
}
type reportModelCaseHTML struct {
	CaseID, Href, ImageLabel string
}

func reportModelInputView(explanation *Explanation, report Report, backURL string) *reportModelInputHTML {
	if explanation == nil || explanation.ReportInputEvidence == nil {
		return nil
	}
	audit := explanation.ReportInputEvidence
	view := &reportModelInputHTML{Audit: audit}
	cases := map[string]StallInvestigation{}
	for _, item := range report.StallInvestigations {
		for _, scope := range audit.Cases {
			if scope.CaseID == item.ID && scope.PlayerRevision == item.PlayerRevision {
				cases[item.ID] = item
			}
		}
	}
	selected := audit.Images
	if audit.Mode != "vision" {
		selected = audit.AttemptedImages
	}
	selectedCases := map[string]bool{}
	for _, image := range selected {
		selectedCases[image.CaseID] = true
	}
	view.SelectedCases = len(selectedCases)
	for _, scope := range audit.Cases {
		row := reportModelCaseHTML{CaseID: scope.CaseID, ImageLabel: "时序"}
		if _, ok := cases[scope.CaseID]; ok {
			row.Href = "#investigation-" + scope.CaseID
		}
		if selectedCases[scope.CaseID] {
			row.ImageLabel = "截图"
			if audit.Mode != "vision" {
				row.ImageLabel = "曾尝试截图"
			}
		} else if len(selected) > 0 {
			view.Omitted = append(view.Omitted, scope.CaseID)
		}
		view.Cases = append(view.Cases, row)
	}
	anchors := map[string]bool{}
	images := func(references []ModelImageReference, final bool) []modelInputImageHTML {
		var result []modelInputImageHTML
		for _, image := range references[:min(3, len(references))] {
			row := modelInputImageHTML{ModelImageReference: image, Role: "异常附近源流截图"}
			if strings.HasSuffix(image.Name, "/before.jpg") {
				row.Role = "异常前源流截图"
			} else if strings.HasSuffix(image.Name, "/after.jpg") {
				row.Role = "异常后源流截图"
			}
			if item, ok := cases[image.CaseID]; ok && modelInputHTMLImageID.MatchString(image.ID) && strings.HasPrefix(image.ID, item.ID+"-") {
				for _, asset := range item.Media.Assets {
					if asset.Kind == "screenshot" && asset.Name == image.Name && asset.Bytes == image.Bytes && asset.SHA256 == image.SHA256 && validateStallAsset(asset, item.ID) {
						row.Href = investigationAssetHTMLHref(asset.Name, backURL)
						if final && audit.Mode == "vision" && !anchors[image.ID] {
							row.AnchorID = "summary-image-" + image.ID
							anchors[image.ID] = true
						}
						break
					}
				}
			}
			result = append(result, row)
		}
		return result
	}
	view.Used, view.Attempted = images(audit.Images, true), images(audit.AttemptedImages, false)
	return view
}

var reportModelImageCitationID = regexp.MustCompile(`^S[0-9]{3}-I[0-9]{3}$`)

func reportModelCitationTarget(id string, input *reportModelInputHTML) string {
	if !reportModelImageCitationID.MatchString(id) {
		return id
	}
	if input == nil || input.Audit.Mode != "vision" {
		return ""
	}
	for _, image := range input.Used {
		if image.ID == id {
			return image.AnchorID
		}
	}
	return ""
}

func reportModelHypotheses(explanation *Explanation) []Hypothesis {
	if explanation == nil {
		return nil
	}
	result := append([]Hypothesis(nil), explanation.Hypotheses...)
	sort.SliceStable(result, func(a, b int) bool {
		return modelPriorityRank(result[a].Priority) < modelPriorityRank(result[b].Priority)
	})
	return result
}

func reportModelPriorityLabel(priority string) string {
	switch priority {
	case "P1":
		return "优先核对"
	case "P2":
		return "随后核对"
	case "P3":
		return "补充核对"
	default:
		return ""
	}
}

func reportModelHasPriority(explanation *Explanation) bool {
	if explanation == nil {
		return false
	}
	for _, hypothesis := range explanation.Hypotheses {
		if reportModelPriorityLabel(hypothesis.Priority) != "" {
			return true
		}
	}
	return false
}
