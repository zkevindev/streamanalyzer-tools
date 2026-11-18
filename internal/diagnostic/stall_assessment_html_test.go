package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func TestStallAssessmentHTMLDirectionsEvidenceAndLimits(t *testing.T) {
	selected := Finding{ID: "S001-E001", Message: "最低可播放缓冲 0 ms", StartMS: 0, EndMS: 3000}
	assessmentFinding := Finding{ID: "S001-A001", Message: "回调年龄样本 0 ms", StartMS: 0, EndMS: 3000, NextCheck: "核对内容帧率"}
	item := StallInvestigation{
		ID: "S001", StallIndex: 1, Status: "complete",
		Correlation: StallCorrelation{Source: "independent_pull", Findings: []Finding{selected}},
		Assessment: &StallAssessment{
			Version: "stall-assessment/1", Conclusion: "缓冲接近耗尽，继续验证媒体供给",
			Directions: []StallDirection{
				{Code: "buffer", Status: "observed", Title: "缓冲接近耗尽", Message: "最低 0 ms", EvidenceIDs: []string{selected.ID}, NextChecks: []string{"对照发送端输出"}},
				{Code: "source", Status: "candidate", Title: "接收链路待验证", Message: "时间相关性不能确定根因", EvidenceIDs: []string{selected.ID, assessmentFinding.ID, "S001-A999"}, NextChecks: []string{"比较源端和另一个接收点"}},
				{Code: "render", Status: "insufficient", Title: "呈帧证据不足", Message: "未观测，不能按零处理", NextChecks: []string{"重新采集帧回调"}},
			},
			// Repeated imported references must not create duplicate inline anchors.
			Findings: []Finding{selected, assessmentFinding, assessmentFinding},
			Limitations: []string{
				"HLS 采样与播放器使用独立连接；采样连接的间隔不能证明播放连接同样中断。",
				`<img src=x onerror="alert(1)">`,
			},
		},
		Explanation: &Explanation{Summary: "结合分层证据继续复核", Hypotheses: []Hypothesis{{Claim: "待验证", EvidenceIDs: []string{assessmentFinding.ID}, NextCheck: "查看现场日志"}}},
	}
	report := Report{Manifest: Manifest{Protocol: ProtocolHLS}, Findings: []Finding{selected, assessmentFinding}, StallInvestigations: []StallInvestigation{item}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, text := range []string{"分层排查方向", item.Assessment.Conclusion, "已观测", "待验证", "证据不足", "最低 0 ms", "未观测，不能按零处理", "建议验证", "比较源端和另一个接收点", "证据限制", item.Assessment.Limitations[0], "分层判断证据", "回调年龄样本 0 ms", "0–3000 ms", "核对内容帧率", "排查规则 stall-assessment/1", "S001-A999（未找到记录）", "&lt;img"} {
		if !strings.Contains(body, text) {
			t.Errorf("report missing %q", text)
		}
	}
	for _, status := range []string{"observed", "candidate", "insufficient"} {
		if !strings.Contains(body, `data-state="`+status+`"`) {
			t.Errorf("missing direction state %q", status)
		}
	}
	for _, id := range []string{selected.ID, assessmentFinding.ID, "case-" + selected.ID, "case-" + assessmentFinding.ID} {
		if strings.Count(body, `id="`+id+`"`) != 1 {
			t.Errorf("duplicate or missing evidence anchor %q", id)
		}
	}
	for _, id := range []string{selected.ID, assessmentFinding.ID} {
		if !strings.Contains(body, `href="#case-`+id+`"`) {
			t.Errorf("missing case evidence link %q", id)
		}
	}
	if strings.Contains(body, `href="#case-S001-A999"`) || strings.Contains(body, `<img src=x`) {
		t.Fatal("unknown evidence or unsafe text became active HTML")
	}

	// Imported case-only reports retain targets for both rule and model references.
	report.Findings = nil
	output.Reset()
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `id="case-S001-A001"`) || !strings.Contains(output.String(), `href="#case-S001-A001"`) {
		t.Fatal("case-only assessment lost its evidence anchor")
	}
}

func TestStallAssessmentHTMLLegacyPendingAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     string
		assessment *StallAssessment
		want       string
	}{
		{name: "legacy", status: "complete", want: "重新生成建议可补充排查分层"},
		{name: "queued", status: "queued", want: "补查完成后展示分层排查方向"},
		{name: "running", status: "running", want: "补查完成后展示分层排查方向"},
		{name: "empty", status: "complete", assessment: &StallAssessment{}, want: "当前证据尚未形成排查方向"},
		{name: "unknown status", status: "complete", assessment: &StallAssessment{Directions: []StallDirection{{Status: "future", Title: "待核对"}}}, want: "待核对"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, StallInvestigations: []StallInvestigation{{ID: "S001", Status: tc.status, Assessment: tc.assessment}}}
			var output bytes.Buffer
			if err := RenderHTML(&output, report, nil, ""); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), tc.want) {
				t.Fatalf("report missing %q", tc.want)
			}
		})
	}
}
