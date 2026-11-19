package diagnostic

import (
	"regexp"
	"strings"
)

type modelInputEvidenceHTML struct {
	Audit     *ModelInputEvidence
	Used      []modelInputImageHTML
	Attempted []modelInputImageHTML
}
type modelInputImageHTML struct {
	ModelImageReference
	Role, Href, AnchorID string
}

var modelInputHTMLImageID = regexp.MustCompile(`^S[0-9]{3}-I00[1-3]$`)

func modelInputEvidenceView(item StallInvestigation, backURL string) *modelInputEvidenceHTML {
	if item.Explanation == nil || item.Explanation.InputEvidence == nil {
		return nil
	}
	audit := item.Explanation.InputEvidence
	view := &modelInputEvidenceHTML{Audit: audit}
	anchors := map[string]bool{}
	for _, finding := range item.Correlation.Findings {
		anchors[finding.ID] = true
	}
	if item.Assessment != nil {
		for _, finding := range item.Assessment.Findings {
			anchors[finding.ID] = true
		}
	}
	images := func(references []ModelImageReference, used bool) []modelInputImageHTML {
		var result []modelInputImageHTML
		for _, reference := range references[:min(3, len(references))] {
			row := modelInputImageHTML{ModelImageReference: reference, Role: "异常附近源流截图"}
			if strings.HasSuffix(reference.Name, "/before.jpg") {
				row.Role = "异常前源流截图"
			} else if strings.HasSuffix(reference.Name, "/after.jpg") {
				row.Role = "异常后源流截图"
			}
			if reference.CaseID == item.ID && modelInputHTMLImageID.MatchString(reference.ID) && strings.HasPrefix(reference.ID, item.ID+"-") {
				for _, asset := range item.Media.Assets {
					if asset.Kind == "screenshot" && asset.Name == reference.Name && asset.Bytes == reference.Bytes && asset.SHA256 == reference.SHA256 && validateStallAsset(asset, item.ID) {
						row.Href = investigationAssetHTMLHref(reference.Name, backURL)
						if used && audit.Mode == "vision" && !anchors[reference.ID] {
							row.AnchorID = "case-" + reference.ID
							anchors[reference.ID] = true
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

func modelInputUsesVision(explanation *Explanation) bool {
	return explanation != nil && explanation.InputEvidence != nil && explanation.InputEvidence.Mode == "vision"
}

func modelInputFallbackLabel(code string) string {
	switch code {
	case "disabled":
		return "已选择仅文本"
	case "no_images":
		return "没有可用截图"
	case "images_invalid":
		return "截图未通过完整性校验"
	case "provider_text_only":
		return "当前厂商使用文本接口"
	case "model_text_only":
		return "当前型号仅支持文本"
	case "images_unsupported":
		return "图文请求不支持，已回退文本"
	default:
		return "未记录可识别的回退原因"
	}
}
