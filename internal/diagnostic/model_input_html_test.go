package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func modelInputHTMLFixture(t *testing.T) (StallInvestigation, []ModelImageReference) {
	t.Helper()
	zero := int64(0)
	item := StallInvestigation{ID: "S001", StallIndex: 1, PlayerRevision: 2, Status: "complete", Media: InvestigationMedia{Status: "ready"}, ModelState: "complete"}
	var refs []ModelImageReference
	for index, name := range []string{"before.jpg", "during.jpg", "after.jpg"} {
		asset := investigationTestImage("stalls/S001/"+name, investigationTestJPEG(t))
		asset.MediaTimeMS = &zero
		item.Media.Assets = append(item.Media.Assets, asset)
		refs = append(refs, ModelImageReference{ID: "S001-I00" + string(rune('1'+index)), CaseID: item.ID, Name: asset.Name, Artifact: asset.Artifact, MediaTimeMS: &zero})
	}
	item.Explanation = &Explanation{Provider: ProviderOpenAICompatible, Model: "vision-fixture", Summary: "核对截图与时序", InputEvidence: &ModelInputEvidence{Version: 1, CaseID: item.ID, PlayerRevision: 2, RequestedMode: ImageInputAuto, Mode: "vision", Images: refs}, Hypotheses: []Hypothesis{{Claim: "截图内容需要结合接收记录验证", EvidenceIDs: []string{refs[0].ID}, NextCheck: "核对源端发送时刻"}}}
	return item, refs
}

func TestModelInputHTMLVisionImageAnchorsAndFileIdentity(t *testing.T) {
	item, refs := modelInputHTMLFixture(t)
	report := Report{Manifest: Manifest{Protocol: ProtocolRTSP}, StallInvestigations: []StallInvestigation{item}, Findings: modelImageFindings(refs)}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, &Explanation{Model: "global-text", Summary: "整份报告时序汇总"}, "/diagnostics#run123"); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, value := range []string{"模型建议（截图与时序证据）", "本次为图文解读 · 3 张截图与时序文本", "请求设置 自动图文", "播放器记录修订 #2", "媒体时间 0 ms", "本次采用的截图", refs[0].SHA256, "查看本地截图", "/api/diagnostics/runs/run123/stall-assets/S001/before.jpg", "整份报告文本汇总", "不能据此重现或证明本机画面冻结"} {
		if !strings.Contains(body, value) {
			t.Errorf("vision HTML missing %q", value)
		}
	}
	for _, ref := range refs {
		for _, id := range []string{ref.ID, "case-" + ref.ID} {
			if strings.Count(body, `id="`+id+`"`) != 1 {
				t.Errorf("missing or duplicate image anchor %s", id)
			}
		}
	}
	if !strings.Contains(body, `href="#case-S001-I001"`) || strings.Contains(body, "未发送截图像素") || strings.Contains(body, "历史结果未记录") {
		t.Fatal("vision citation or actual input status is misleading")
	}
}

func TestModelInputHTMLAttemptedImagesAreDistinctFromFinalText(t *testing.T) {
	item, refs := modelInputHTMLFixture(t)
	item.Explanation.Hypotheses = nil
	item.Explanation.InputEvidence.Mode = "text"
	item.Explanation.InputEvidence.Images = []ModelImageReference{}
	item.Explanation.InputEvidence.AttemptedImages = refs
	item.Explanation.InputEvidence.FallbackCode = "images_unsupported"
	var output bytes.Buffer
	if err := RenderHTML(&output, Report{StallInvestigations: []StallInvestigation{item}}, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, value := range []string{"本次为时序文本解读", "已尝试发送 3 张截图", "最终建议基于文本", "已尝试发送的截图", "图文请求不支持，已回退文本", `href="stalls/S001/before.jpg"`} {
		if !strings.Contains(body, value) {
			t.Errorf("fallback HTML missing %q", value)
		}
	}
	if strings.Contains(body, "未发送截图像素") || strings.Contains(body, `id="case-S001-I001"`) || strings.Contains(body, "本次采用的截图") {
		t.Fatal("attempted image request became a successful vision citation")
	}
	item.Explanation.InputEvidence.AttemptedImages = nil
	item.Explanation.InputEvidence.FallbackCode = "disabled"
	item.Explanation.InputEvidence.RequestedMode = ImageInputText
	output.Reset()
	if err := RenderHTML(&output, Report{StallInvestigations: []StallInvestigation{item}}, nil, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "本次仅发送时序文本，未发送截图像素") || !strings.Contains(output.String(), "请求设置 仅文本") {
		t.Fatal("explicit text mode lost its observed input status")
	}
}

func TestModelInputHTMLLegacyAndUnmatchedImageReferences(t *testing.T) {
	item, _ := modelInputHTMLFixture(t)
	item.Explanation.InputEvidence = nil
	var output bytes.Buffer
	if err := RenderHTML(&output, Report{StallInvestigations: []StallInvestigation{item}}, &Explanation{Summary: "新的整份报告文本解读"}, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "该历史结果未记录截图输入方式") != 1 {
		t.Fatal("global text summary was treated as missing image audit")
	}
	item, _ = modelInputHTMLFixture(t)
	item.Explanation.InputEvidence.Images[0].Name = "javascript:alert(1)"
	item.Explanation.InputEvidence.Images[1].SHA256 = strings.Repeat("e", 64)
	item.Explanation.InputEvidence.Images[2].CaseID = "S002"
	view := modelInputEvidenceView(item, "")
	for _, row := range view.Used {
		if row.Href != "" || row.AnchorID != "" {
			t.Fatal("unmatched image identity acquired a trusted link or evidence anchor")
		}
	}
}
