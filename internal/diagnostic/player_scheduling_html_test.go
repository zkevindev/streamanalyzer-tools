package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func schedulingHTMLGroup(t *testing.T, body, kind string) string {
	t.Helper()
	start := strings.Index(body, `<section data-kind="`+kind+`">`)
	if start < 0 {
		t.Fatalf("missing scheduling group %q", kind)
	}
	end := strings.Index(body[start:], "</section>")
	return body[start : start+end]
}

func TestPlayerSchedulingHTMLMetricsAndCaseOverlap(t *testing.T) {
	scheduling := &PlayerSchedulingReport{
		LongTaskSupport: "supported", ObservationStartMS: 0, ObservationEndMS: 5000, ActiveCoverageMS: 4000,
		LongTasks:        &PlayerSchedulingMetrics{Count: 2, TotalMS: 400, MaxMS: 250, Intervals: []PlayerSchedulingInterval{{StartMS: 100, EndMS: 250}, {StartMS: 2000, EndMS: 2250}}},
		TimerDelays:      &PlayerSchedulingMetrics{Count: 1, TotalMS: 125, MaxMS: 125, Intervals: []PlayerSchedulingInterval{{StartMS: 2100, EndMS: 2225}}},
		DroppedLongTasks: 2, DroppedTimerDelays: 1,
	}
	window := &PlayerSchedulingReport{
		LongTaskSupport: "supported", ObservationStartMS: 1000, ObservationEndMS: 4000, ActiveCoverageMS: 3000,
		LongTasks:   &PlayerSchedulingMetrics{Count: 1, TotalMS: 250, MaxMS: 250, Intervals: []PlayerSchedulingInterval{{StartMS: 2000, EndMS: 2250}}},
		TimerDelays: scheduling.TimerDelays,
	}
	finding := Finding{ID: "S001-A001", Message: "页面调度延后与卡顿重合"}
	report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, Player: &PlayerReport{Scheduling: scheduling}, Findings: []Finding{finding},
		StallInvestigations: []StallInvestigation{{ID: "S001", StallIndex: 1, Status: "complete", Stall: PlayerStall{StartMS: 2200, EndMS: 2300, DurationMS: 100}, Correlation: StallCorrelation{Scheduling: window},
			Assessment: &StallAssessment{Directions: []StallDirection{{Status: "candidate", Title: "核对页面调度", EvidenceIDs: []string{finding.ID}, NextChecks: []string{"对照主线程记录"}}}, Findings: []Finding{finding}}}}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, text := range []string{"页面调度观测", "补查窗口页面调度", "支持 Long Tasks API", "前台播放有效覆盖", "0–5000 ms", "1000–4000 ms", "4000 ms", "3000 ms", "区间数", "区间累计时长", "最长区间", "定时器延后（原始延后 ≥100 ms）", "每 250 ms 检查", "裁切后的区间可能更短", "不能单独证明 CPU 瓶颈或解码故障", "与本次卡顿重合 (ms)", "长任务 2 条，定时器延后 1 条", "核对页面调度", `href="#case-S001-A001"`} {
		if !strings.Contains(body, text) {
			t.Errorf("report missing %q", text)
		}
	}
	if !strings.Contains(schedulingHTMLGroup(t, body, "long_task"), "<dd>2</dd>") || !strings.Contains(schedulingHTMLGroup(t, body, "timer_delay"), "<dd>1</dd>") {
		t.Fatal("native long tasks and timer delays were merged or miscounted")
	}
	for _, row := range []string{"<td>2000</td><td>2250</td><td>250</td><td>50</td>", "<td>2100</td><td>2225</td><td>125</td><td>25</td>"} {
		if !strings.Contains(body, row) {
			t.Errorf("selected case lost aligned interval or overlap: %s", row)
		}
	}
	for _, id := range []string{finding.ID, "case-" + finding.ID} {
		if strings.Count(body, `id="`+id+`"`) != 1 {
			t.Errorf("duplicate or missing scheduling evidence anchor %q", id)
		}
	}
}

func TestPlayerSchedulingHTMLSupportZeroCoverageAndLegacy(t *testing.T) {
	for _, tc := range []struct {
		name       string
		scheduling *PlayerSchedulingReport
		want       string
		nativeZero bool
		timerZero  bool
	}{
		{name: "legacy", want: "该记录未采集页面调度数据"},
		{name: "supported zero", scheduling: &PlayerSchedulingReport{LongTaskSupport: "supported", ObservationEndMS: 1000, ActiveCoverageMS: 1000, LongTasks: &PlayerSchedulingMetrics{}, TimerDelays: &PlayerSchedulingMetrics{}}, want: "支持 Long Tasks API", nativeZero: true, timerZero: true},
		{name: "unsupported timer zero", scheduling: &PlayerSchedulingReport{LongTaskSupport: "unsupported", ObservationEndMS: 1000, ActiveCoverageMS: 1000, TimerDelays: &PlayerSchedulingMetrics{}}, want: "长任务未观测不代表没有长任务", timerZero: true},
		{name: "no active coverage", scheduling: &PlayerSchedulingReport{LongTaskSupport: "supported", ObservationEndMS: 1000}, want: "本记录没有可用于统计的前台播放覆盖区间"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, Player: &PlayerReport{Scheduling: tc.scheduling}, StallInvestigations: []StallInvestigation{{ID: "S001"}}}
			var output bytes.Buffer
			if err := RenderHTML(&output, report, nil, ""); err != nil {
				t.Fatal(err)
			}
			body := output.String()
			if !strings.Contains(body, tc.want) || !strings.Contains(body, "该补查未包含页面调度数据") {
				t.Fatal("missing scheduling capability or legacy case note")
			}
			if tc.scheduling == nil {
				return
			}
			for kind, wantZero := range map[string]bool{"long_task": tc.nativeZero, "timer_delay": tc.timerZero} {
				group := schedulingHTMLGroup(t, body, kind)
				if wantZero {
					if !strings.Contains(group, "<dd>0</dd>") || strings.Count(group, "<dd>0 ms</dd>") != 2 || strings.Contains(group, "未观测") {
						t.Errorf("observed zero became unknown in %s", kind)
					}
				} else if strings.Count(group, "未观测") != 3 {
					t.Errorf("missing capability or coverage became measured zero in %s", kind)
				}
			}
		})
	}
}

func TestPlayerSchedulingHTMLBoundsRowsAndKeepsClippedIntervals(t *testing.T) {
	metrics := &PlayerSchedulingMetrics{Count: 202, TotalMS: 2020, MaxMS: 10}
	for index := int64(0); index < 202; index++ {
		metrics.Intervals = append(metrics.Intervals, PlayerSchedulingInterval{StartMS: index * 100, EndMS: index*100 + 10})
	}
	scheduling := &PlayerSchedulingReport{LongTaskSupport: "unsupported", ObservationEndMS: 30000, ActiveCoverageMS: 30000, TimerDelays: metrics}
	report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, Player: &PlayerReport{Scheduling: scheduling}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	if !strings.Contains(body, "19900</td><td>19910") || strings.Contains(body, "20000</td><td>20010") || !strings.Contains(body, "另有 2 个已保存区间") {
		t.Fatal("scheduling interval display cap failed")
	}
	if !strings.Contains(body, "原始延后 ≥100 ms") || !strings.Contains(body, "<dd>202</dd>") || strings.Contains(body, "采集上限外未记录") {
		t.Fatal("clipped timer intervals or display-only omissions were misrepresented")
	}
}
