package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func presentationHTMLInt(value int) *int { return &value }

func presentationHTMLSection(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `<section class="player-presentation-report">`)
	if start < 0 {
		t.Fatal("missing presentation callback report")
	}
	end := strings.Index(body[start:], "</section>")
	return body[start : start+end]
}

func TestPlayerPresentationHTMLWithoutBufferingStalls(t *testing.T) {
	zero, ready, total, maximum := int64(0), int64(100), int64(3500), int64(2000)
	presentation := &PlayerPresentationReport{Support: "supported", ObservationEndMS: 8000, ReadyAtMS: &ready, ActiveCoverageMS: 7000, Count: presentationHTMLInt(2), TotalMS: &total, MaxMS: &maximum, DroppedGaps: 3,
		Gaps: []PlayerPresentationGapReport{{StartMS: 1000, EndMS: 3000, DurationMS: 2000, ExpectedFrameMS: 40}, {StartMS: 5000, EndMS: 6500, DurationMS: 1500, ExpectedFrameMS: 400}}}
	report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, Player: &PlayerReport{State: "complete", FirstPlayingMS: &zero, Presentation: presentation}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	panel := presentationHTMLSection(t, body)
	for _, text := range []string{"呈帧回调间隔", "支持视频帧回调", "回调基线就绪偏移", "100 ms", "前台播放有效覆盖", "7000 ms", "间隔次数</dt><dd>2", "3500 ms", "2000 ms", "至少 3 个有效回调媒体时间增量", "1000 ms 与 3 倍间隔基线", "不能单凭该记录确认画面冻结或解码故障", "不叠加到缓冲卡顿统计", "另有 3 条呈帧回调间隔记录未保存"} {
		if !strings.Contains(panel, text) {
			t.Errorf("presentation report missing %q", text)
		}
	}
	if !strings.Contains(body, "卡顿次数</dt><dd>0</dd>") || !strings.Contains(panel, "<td>5000</td><td>6500</td><td>1500</td><td>400</td><td>0</td>") {
		t.Fatal("presentation gaps were hidden, added to buffering stalls, or measured zero overlap was lost")
	}
}

func TestPlayerPresentationHTMLCaseKindsAndWholeIntervals(t *testing.T) {
	ready, fullTotal, maximum, windowTotal := int64(100), int64(3500), int64(2000), int64(3000)
	presentation := &PlayerPresentationReport{Support: "supported", ObservationEndMS: 8000, ReadyAtMS: &ready, ActiveCoverageMS: 7000, Count: presentationHTMLInt(2), TotalMS: &fullTotal, MaxMS: &maximum,
		Gaps: []PlayerPresentationGapReport{{StartMS: 1000, EndMS: 3000, DurationMS: 2000, ExpectedFrameMS: 40}, {StartMS: 5000, EndMS: 6500, DurationMS: 1500, ExpectedFrameMS: 400, BufferingOverlapMS: 200}}}
	window := *presentation
	window.ObservationEndMS, window.ActiveCoverageMS, window.TotalMS = 6000, 5000, &windowTotal
	bufferingFinding := Finding{ID: "S001-A001", Message: "缓冲卡顿证据"}
	gapFinding := Finding{ID: "S002-A001", Message: "回调间隔证据"}
	buffering := StallInvestigation{ID: "S001", StallIndex: 1, Status: "complete", Stall: PlayerStall{StartMS: 6000, EndMS: 6200, DurationMS: 200}, Correlation: StallCorrelation{WindowStartMS: 3000, WindowEndMS: 8000}, Assessment: &StallAssessment{Findings: []Finding{bufferingFinding}}}
	gap := StallInvestigation{ID: "S002", IntervalKind: "presentation_gap", StallIndex: 1, Status: "complete", Stall: PlayerStall{StartMS: 1000, EndMS: 3000, DurationMS: 2000},
		Correlation: StallCorrelation{WindowEndMS: 6000, Presentation: &window, BufferSamples: []PlayerBufferSample{{OffsetMS: 1000, BufferMS: 3000}}, Scheduling: &PlayerSchedulingReport{LongTaskSupport: "unsupported", ActiveCoverageMS: 1000, TimerDelays: &PlayerSchedulingMetrics{Count: 1, TotalMS: 100, MaxMS: 100, Intervals: []PlayerSchedulingInterval{{StartMS: 2100, EndMS: 2200}}}}},
		Assessment:  &StallAssessment{Findings: []Finding{gapFinding}}, Explanation: &Explanation{Hypotheses: []Hypothesis{{EvidenceIDs: []string{gapFinding.ID}}}}}
	report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, Player: &PlayerReport{Presentation: presentation, Stalls: []PlayerStall{buffering.Stall}, StallCount: 1}, Findings: []Finding{bufferingFinding, gapFinding}, StallInvestigations: []StallInvestigation{buffering, gap}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, text := range []string{"播放异常证据补查", "第 1 次卡顿（缓冲）", "第 1 个呈帧回调间隔", "呈帧回调间隔</dt>", "阴影为呈帧回调间隔", "不代表已确认画面冻结", "同期源流片段和截图供内容对照，不能重现或证明本机画面冻结", "与呈帧回调间隔重合 (ms)", "窗口内重合累计时长", "最长重合时长", "起止可能超出窗口", "与本次补查区间重合 (ms)", `href="#case-S002-A001"`} {
		if !strings.Contains(body, text) {
			t.Errorf("case report missing %q", text)
		}
	}
	start := strings.Index(body, `id="investigation-S002"`)
	caseBody := body[start : start+strings.Index(body[start:], "</article>")]
	panel := presentationHTMLSection(t, caseBody)
	if !strings.Contains(panel, "窗口内重合累计时长</dt><dd>3000 ms") || !strings.Contains(panel, "<td>5000</td><td>6500</td><td>1500</td><td>400</td><td>200</td><td>0</td>") {
		t.Fatal("case window aggregates were confused with full callback intervals or selected interval overlap")
	}
	for _, id := range []string{"S001-A001", "S002-A001", "case-S001-A001", "case-S002-A001"} {
		if strings.Count(body, `id="`+id+`"`) != 1 {
			t.Errorf("missing or duplicate cross-kind evidence anchor %s", id)
		}
	}
}

func TestPlayerPresentationHTMLUnknownVersusReadyZero(t *testing.T) {
	zero := int64(0)
	for _, tc := range []struct {
		name         string
		presentation *PlayerPresentationReport
		want         string
		zero         bool
	}{
		{name: "legacy", want: "该记录未采集呈帧回调间隔数据"},
		{name: "unsupported", presentation: &PlayerPresentationReport{Support: "unsupported"}, want: "当前环境未提供 requestVideoFrameCallback"},
		{name: "warming up", presentation: &PlayerPresentationReport{Support: "supported", ActiveCoverageMS: 1000}, want: "尚未完成回调基线预热"},
		{name: "no active coverage", presentation: &PlayerPresentationReport{Support: "supported", ReadyAtMS: &zero}, want: "预热后没有可用于统计的连续前台播放覆盖区间"},
		{name: "ready observed zero", presentation: &PlayerPresentationReport{Support: "supported", ReadyAtMS: &zero, ActiveCoverageMS: 1000, Count: presentationHTMLInt(0), TotalMS: &zero, MaxMS: &zero}, want: "有效覆盖内未记录到符合阈值的呈帧回调间隔", zero: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := RenderHTML(&output, Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, Player: &PlayerReport{Presentation: tc.presentation}}, nil, ""); err != nil {
				t.Fatal(err)
			}
			panel := presentationHTMLSection(t, output.String())
			if !strings.Contains(panel, tc.want) {
				t.Fatalf("missing capability/coverage message %q", tc.want)
			}
			if tc.presentation == nil {
				return
			}
			if tc.zero {
				if !strings.Contains(panel, "间隔次数</dt><dd>0</dd>") || !strings.Contains(panel, "回调基线就绪偏移</dt><dd>0 ms</dd>") || strings.Contains(panel, "未观测") {
					t.Fatal("observed zero or ready-at zero was changed to unknown")
				}
			} else if !strings.Contains(panel, "间隔次数</dt><dd>未观测</dd>") || strings.Contains(panel, "有效覆盖内未记录到符合阈值") {
				t.Fatal("unknown presentation data was presented as no observed gaps")
			}
		})
	}
}
