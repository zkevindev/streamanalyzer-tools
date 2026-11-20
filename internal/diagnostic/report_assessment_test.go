package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func assessmentFinding(id, code string) Finding {
	return Finding{ID: id, Code: code, Severity: "warn", Message: code, Occurrences: 1}
}

func TestReportAssessmentPrioritizesActionableEvidence(t *testing.T) {
	tests := []struct {
		name       string
		report     Report
		conclusion string
		direction  string
		check      string
	}{
		{name: "HTTP authorization", report: Report{Manifest: Manifest{Observation: Observation{FailureCode: "http_status"}}, Findings: []Finding{assessmentFinding("F001", "http_status"), assessmentFinding("F002", "no_media")}}, conclusion: "连接或协议协商失败", direction: "connection_or_negotiation", check: "播放鉴权"},
		{name: "DNS", report: Report{Manifest: Manifest{Observation: Observation{FailureCode: "dns_error"}}, Findings: []Finding{assessmentFinding("F001", "connection_dns_error")}}, conclusion: "连接或协议协商失败", direction: "connection_or_negotiation", check: "DNS"},
		{name: "PLAY success without media", report: Report{Findings: []Finding{assessmentFinding("F001", "rtsp_no_media")}}, conclusion: "未取得完整媒体", direction: "no_media", check: "UDP"},
		{name: "slow first frame", report: Report{Findings: []Finding{assessmentFinding("F001", "slow_first_video")}}, conclusion: "起播偏慢", direction: "slow_start", check: "关键帧"},
		{name: "receive gap is not root cause", report: Report{Findings: []Finding{assessmentFinding("F001", "video_arrival_gap")}}, conclusion: "连续性异常", direction: "source_or_transport", check: "另一接收点"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assessment := assessReport(test.report)
			if !strings.Contains(assessment.Conclusion, test.conclusion) {
				t.Fatalf("unexpected conclusion: %s", assessment.Conclusion)
			}
			var direction *StallDirection
			for i := range assessment.Directions {
				if assessment.Directions[i].Code == test.direction {
					direction = &assessment.Directions[i]
				}
			}
			if direction == nil || len(direction.EvidenceIDs) == 0 || !strings.Contains(strings.Join(direction.NextChecks, " "), test.check) {
				t.Fatalf("missing actionable direction: %+v", assessment.Directions)
			}
			if err := validReportAssessment(*assessment, test.report); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReportAssessmentDoesNotInventHealthOrEvidence(t *testing.T) {
	report := Report{Findings: []Finding{}}
	assessment := assessReport(report)
	if !strings.Contains(assessment.Conclusion, "不等同") || len(assessment.Directions) != 0 {
		t.Fatalf("empty rules became a health claim: %+v", assessment)
	}
	tampered := *assessment
	tampered.Directions = []StallDirection{{Code: "invented", Status: "observed", EvidenceIDs: []string{"F999"}}}
	if err := validReportAssessment(tampered, report); err == nil {
		t.Fatal("accepted fabricated assessment evidence")
	}
}

func TestReportAssessmentHTMLUsesFindingLinks(t *testing.T) {
	report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, Findings: []Finding{assessmentFinding("F001", "video_arrival_gap")}}
	report.Assessment = assessReport(report)
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"整报告分层排查", "优先核对媒体供给", `href="#F001"`, "report-assessment/1"} {
		if !strings.Contains(output.String(), text) {
			t.Fatalf("report assessment HTML missing %q", text)
		}
	}
}
