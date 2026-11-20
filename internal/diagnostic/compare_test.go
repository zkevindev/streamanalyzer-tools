package diagnostic

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func comparisonDetail(id string, report Report) Detail {
	return Detail{Run: Run{ID: id, Protocol: report.Manifest.Protocol, Origin: report.Manifest.Observation.Origin, Status: "done", CreatedAt: time.Now()}, Report: &report}
}

func comparisonMetricByKey(t *testing.T, comparison ReportComparison, key string) ComparisonMetric {
	t.Helper()
	for _, metric := range comparison.Metrics {
		if metric.Key == key {
			return metric
		}
	}
	t.Fatalf("missing comparison metric %s", key)
	return ComparisonMetric{}
}

func TestReportComparisonDistinguishesImprovementAndUnobserved(t *testing.T) {
	beforeTTFB, afterTTFB := int64(200), int64(80)
	before := Report{RuleVersion: "http-flv/2", Manifest: Manifest{Protocol: ProtocolHTTPFLV, Observation: Observation{Origin: "https://a.example", ElapsedMS: 10_000, TTFBMS: &beforeTTFB}}, Metrics: Metrics{VideoFrames: 1}, Findings: []Finding{assessmentFinding("F001", "old_rule")}}
	after := Report{RuleVersion: "http-flv/2", Manifest: Manifest{Protocol: ProtocolHTTPFLV, Observation: Observation{Origin: "https://a.example", ElapsedMS: 10_000, TTFBMS: &afterTTFB}}, Metrics: Metrics{VideoFrames: 2, MaxVideoGapMS: 500}, Findings: []Finding{assessmentFinding("F001", "new_rule")}}
	comparison := compareReports(comparisonDetail("current", after), comparisonDetail("baseline", before))
	metric := comparisonMetricByKey(t, comparison, "ttfb_ms")
	if metric.State != "improved" || metric.Delta == nil || *metric.Delta != -120 {
		t.Fatalf("incorrect TTFB comparison: %+v", metric)
	}
	gap := comparisonMetricByKey(t, comparison, "max_video_gap_ms")
	if gap.State != "unobserved" || gap.Baseline != nil || gap.Delta != nil {
		t.Fatalf("missing baseline became zero: %+v", gap)
	}
	if !slices.Equal(comparison.NewFindings, []string{"new_rule"}) || !slices.Equal(comparison.ResolvedFindings, []string{"old_rule"}) {
		t.Fatalf("incorrect finding changes: %+v", comparison)
	}
}

func TestReportComparisonWarnsAboutDifferentConditions(t *testing.T) {
	before := Report{RuleVersion: "http-flv/1", Manifest: Manifest{Protocol: ProtocolHTTPFLV, Observation: Observation{Origin: "https://a.example", ElapsedMS: 10_000}}, Metrics: Metrics{VideoCodecs: []string{"h264"}}}
	after := Report{RuleVersion: "http-flv/2", Manifest: Manifest{Protocol: ProtocolHTTPFLV, Observation: Observation{Origin: "https://b.example", ElapsedMS: 30_000}}, Metrics: Metrics{VideoCodecs: []string{"h265"}}, Player: &PlayerReport{StallCount: 0}}
	comparison := compareReports(comparisonDetail("current", after), comparisonDetail("baseline", before))
	joined := strings.Join(comparison.Warnings, " ")
	for _, expected := range []string{"来源不同", "规则版本不同", "编码集合不同", "超过 10%"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing comparison warning %q: %s", expected, joined)
		}
	}
	stalls := comparisonMetricByKey(t, comparison, "stall_count")
	if stalls.State != "unobserved" || stalls.Baseline != nil || stalls.Current == nil || *stalls.Current != 0 {
		t.Fatalf("observed zero and missing baseline were conflated: %+v", stalls)
	}
}

func TestProtocolSpecificComparisonMetrics(t *testing.T) {
	before := Report{Manifest: Manifest{Protocol: ProtocolRTSP, Observation: Observation{ElapsedMS: 10_000}}, RTSP: &RTSPMetrics{Tracks: []RTSPTrackMetrics{{MissingSequencePackets: 8, OutOfOrderPackets: 3}}}}
	after := Report{Manifest: Manifest{Protocol: ProtocolRTSP, Observation: Observation{ElapsedMS: 10_000}}, RTSP: &RTSPMetrics{Tracks: []RTSPTrackMetrics{{MissingSequencePackets: 2, OutOfOrderPackets: 3}}}}
	comparison := compareReports(comparisonDetail("current", after), comparisonDetail("baseline", before))
	if metric := comparisonMetricByKey(t, comparison, "rtsp_missing_packets"); metric.State != "improved" || *metric.Delta != -6 {
		t.Fatalf("incorrect RTSP comparison: %+v", metric)
	}
	if metric := comparisonMetricByKey(t, comparison, "rtsp_out_of_order"); metric.State != "unchanged" {
		t.Fatalf("incorrect RTSP out-of-order comparison: %+v", metric)
	}
}
