package diagnostic

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func reportModelHTMLFixture(t *testing.T) (Report, Explanation) {
	t.Helper()
	var report Report
	var references []ModelImageReference
	for index := 1; index <= 4; index++ {
		item, refs := modelInputHTMLFixture(t)
		id := fmt.Sprintf("S%03d", index)
		item.ID = id
		for n := range item.Media.Assets {
			item.Media.Assets[n].Name = strings.ReplaceAll(item.Media.Assets[n].Name, "S001", id)
			item.Media.Assets[n].SHA256 = strings.Repeat(fmt.Sprintf("%x", index), 64)
		}
		for n := range refs {
			refs[n].CaseID = id
			refs[n].ID = strings.ReplaceAll(refs[n].ID, "S001", id)
			refs[n].Name = item.Media.Assets[n].Name
			refs[n].Artifact = item.Media.Assets[n].Artifact
		}
		if index == 1 {
			item.Explanation.InputEvidence.Images = refs
			report.Findings = modelImageFindings(refs)
		} else {
			item.Explanation = nil
		}
		report.StallInvestigations = append(report.StallInvestigations, item)
		if index <= 3 {
			references = append(references, refs[index-1])
		}
	}
	audit := &ReportModelInputEvidence{Version: 1, ReportSHA256: strings.Repeat("f", 64), PlayerRevision: 2, RequestedMode: "auto", Mode: "vision", Selection: "critical_stalls_v1", Images: references, OmittedImageCases: 1}
	for _, item := range report.StallInvestigations {
		audit.Cases = append(audit.Cases, ReportModelCaseReference{CaseID: item.ID, PlayerRevision: item.PlayerRevision})
	}
	return report, Explanation{Model: "report-fixture", Summary: "汇总各案例时序与截图", ReportInputEvidence: audit, Hypotheses: []Hypothesis{{Priority: "P3", Claim: "第三核查顺序", EvidenceIDs: []string{references[2].ID}, NextCheck: "核查三"}, {Priority: "P1", Claim: "第一核查顺序", EvidenceIDs: []string{references[0].ID}, NextCheck: "核查一"}, {Priority: "P2", Claim: "第二核查顺序", EvidenceIDs: []string{references[1].ID}, NextCheck: "核查二"}}}
}

func TestReportModelHTMLScopeImageCitationsAndPriorityOrder(t *testing.T) {
	report, explanation := reportModelHTMLFixture(t)
	var output bytes.Buffer
	if err := RenderHTML(&output, report, &explanation, "/diagnostics#run123"); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, value := range []string{"整份报告汇总（图文解读）", "全部 4 个补查案例的时序，选取 3 个案例 3 张截图", "其余 1 个案例未提供截图", "未选取的案例：S004", "#investigation-S004", "P1 · 优先核对", "下一步：核查一", "媒体时间 0 ms", strings.Repeat("f", 64), "/api/diagnostics/runs/run123/stall-assets/S002/during.jpg"} {
		if !strings.Contains(body, value) {
			t.Errorf("global input HTML missing %q", value)
		}
	}
	if !(strings.Index(body, "第一核查顺序") < strings.Index(body, "第二核查顺序") && strings.Index(body, "第二核查顺序") < strings.Index(body, "第三核查顺序")) {
		t.Fatal("priority next checks are not in P1/P2/P3 order")
	}
	for _, id := range []string{"S001-I001", "case-S001-I001", "summary-image-S001-I001", "summary-image-S002-I002", "summary-image-S003-I003"} {
		if strings.Count(body, `id="`+id+`"`) != 1 {
			t.Errorf("missing or duplicate distinct evidence anchor %s", id)
		}
	}
	if !strings.Contains(body, `href="#summary-image-S001-I001"`) || !strings.Contains(body, `href="#case-S001-I001"`) {
		t.Fatal("global and case image citations do not have distinct valid targets")
	}
	if explanation.Hypotheses[0].Priority != "P3" {
		t.Fatal("rendering reordered the stored explanation")
	}
}

func TestReportModelHTMLTextFallbackAndLegacy(t *testing.T) {
	report, explanation := reportModelHTMLFixture(t)
	audit := explanation.ReportInputEvidence
	audit.AttemptedImages, audit.Images, audit.Mode, audit.FallbackCode = audit.Images, nil, "text", "images_unsupported"
	explanation.Hypotheses = []Hypothesis{{Claim: "不应将未采用截图作为汇总引用", EvidenceIDs: []string{"S001-I001"}, NextCheck: "继续核对文本"}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, &explanation, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	if !strings.Contains(body, "已尝试发送 3 张截图（来自 3 次异常）") || strings.Contains(body, `id="summary-image-S001-I001"`) || strings.Contains(body, `href="#summary-image-S001-I001"`) || !strings.Contains(body, "S001-I001（未匹配本次截图输入）") {
		t.Fatal("rejected images became trusted final summary citations")
	}
	// Scope and case input remain separate: the first case genuinely used its
	// own images before the overall request later fell back to text.
	if !strings.Contains(body, `id="case-S001-I001"`) {
		t.Fatal("overall fallback destroyed a prior case's image evidence")
	}
	audit.AttemptedImages, audit.FallbackCode, audit.RequestedMode = nil, "disabled", "text"
	output.Reset()
	if err := RenderHTML(&output, report, &explanation, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "本次仅发送时序文本，未发送截图像素") {
		t.Fatal("explicit text summary lost its observed input status")
	}
	explanation.ReportInputEvidence = nil
	explanation.Hypotheses = []Hypothesis{{Claim: "旧排序一", NextCheck: "一"}, {Claim: "旧排序二", NextCheck: "二"}}
	output.Reset()
	if err := RenderHTML(&output, report, &explanation, ""); err != nil {
		t.Fatal(err)
	}
	body = output.String()
	if !strings.Contains(body, "该历史汇总未记录截图输入方式") || strings.Contains(body, "整份报告汇总（文本解读）") || strings.Index(body, "旧排序一") > strings.Index(body, "旧排序二") {
		t.Fatal("legacy summary acquired an invented input mode or new ordering")
	}
}

func TestReportModelHTMLRejectsUnselectedAndChangedImages(t *testing.T) {
	report, explanation := reportModelHTMLFixture(t)
	view := reportModelInputView(&explanation, report, "")
	if reportModelCitationTarget("S004-I001", view) != "" || reportModelCitationTarget("S004-I999", view) != "" {
		t.Fatal("nonselected screenshot citation acquired a target")
	}
	explanation.ReportInputEvidence.Images[0].SHA256 = strings.Repeat("0", 64)
	view = reportModelInputView(&explanation, report, "")
	if view.Used[0].Href != "" || view.Used[0].AnchorID != "" || reportModelCitationTarget("S001-I001", view) != "" {
		t.Fatal("changed screenshot identity became trusted summary evidence")
	}
}

func TestReportModelHTMLWithoutPlayerDoesNotInventRevisionZero(t *testing.T) {
	explanation := &Explanation{Summary: "普通采样文本汇总", ReportInputEvidence: &ReportModelInputEvidence{Version: 1, PlayerRevision: 0, RequestedMode: "auto", Mode: "text", FallbackCode: "no_images", Selection: "critical_stalls_v1"}}
	var output bytes.Buffer
	if err := RenderHTML(&output, Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}}, explanation, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	if !strings.Contains(body, "未记录播放器观测") || strings.Contains(body, "播放器记录修订 #0") || !strings.Contains(body, "本次仍采用报告指标与规则证据") {
		t.Fatal("plain sampling acquired an invented player revision")
	}
}
