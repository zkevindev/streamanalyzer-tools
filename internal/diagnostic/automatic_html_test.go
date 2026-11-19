package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func TestAutomaticHTMLShowsSelectionMediaAndSummarySeparately(t *testing.T) {
	report := Report{Manifest: Manifest{Protocol: ProtocolRTSP, Observation: Observation{ElapsedMS: 8000}}, AutomaticInvestigation: &AutomaticInvestigation{Version: 1, Policy: automaticPolicy, State: "partial", PlayerRevision: 2, CandidateCount: 4, ModelState: "complete", Reason: `<img src=x onerror="alert(1)">`, Selected: []AutomaticSelection{
		{IntervalKind: IntervalBuffering, IntervalIndex: 1, Interval: PlayerStall{StartMS: 1000, EndMS: 5000, DurationMS: 4000}, CaseID: "S001"},
		{IntervalKind: IntervalPresentationGap, IntervalIndex: 2, Interval: PlayerStall{StartMS: 6000, EndMS: 9000, DurationMS: 3000}, CaseID: "S002"},
	}}, StallInvestigations: []StallInvestigation{{ID: "S001", Status: "complete", ModelState: "not_requested", Media: InvestigationMedia{Status: "ready"}}, {ID: "S002", Status: "partial", ModelState: "not_requested", Media: InvestigationMedia{Status: "partial"}}}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, &Explanation{Summary: "统一核对两类区间"}, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, value := range []string{"自动补查与整体汇总", "记录阶段 · 保留部分结果", "候选同期异常区间</dt><dd>4", "自动选择</dt><dd>2 个 / 最多 3 个", "整体汇总状态</dt><dd>已生成", "6000–9000", "<td>2000</td>", "片段与截图已提取", "媒体产物部分可用", `href="#investigation-S001"`, `href="#report-model-heading"`, "未单独解读", "候选数与选择数不是新的卡顿次数", "打开报告或导入证据包不会启动自动任务", "&lt;img"} {
		if !strings.Contains(body, value) {
			t.Errorf("automatic report missing %q", value)
		}
	}
	if strings.Contains(body, `<img src=x`) || strings.Contains(body, "模型建议生成失败") {
		t.Fatal("controlled automatic task notes or media-only model status are misleading")
	}
	if strings.Count(body, `id="report-model-heading"`) != 1 || strings.Count(body, `id="automatic-investigation"`) != 1 {
		t.Fatal("automatic report anchors are duplicated")
	}
}

func TestAutomaticHTMLPendingUnknownAndCompletedZero(t *testing.T) {
	for _, state := range []string{"waiting_player", "queued", "interrupted", "failed", "canceled"} {
		report := Report{AutomaticInvestigation: &AutomaticInvestigation{State: state, PlayerRevision: 1, ModelState: "pending"}}
		view := automaticInvestigationView(report, nil)
		if view.PlanKnown {
			t.Fatalf("%s invented a completed zero-candidate selection", state)
		}
	}
	report := Report{AutomaticInvestigation: &AutomaticInvestigation{State: "complete", PlayerRevision: 1, CandidateCount: 0, ModelState: "complete"}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, &Explanation{Summary: "使用已有数值汇总"}, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	if !strings.Contains(body, "候选同期异常区间</dt><dd>0") || !strings.Contains(body, "未选出可补查的同期异常") || strings.Contains(body, "无卡顿") || strings.Contains(body, "流媒体健康") {
		t.Fatal("observed zero automatic candidates became an unsupported health conclusion")
	}
	report.AutomaticInvestigation = nil
	output.Reset()
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "自动补查与整体汇总") {
		t.Fatal("legacy report acquired an invented automatic task")
	}
}
