package diagnostic

import "testing"

func TestCrossLayerCorrelationAlignsEvidenceWithoutRootCauseClaims(t *testing.T) {
	first := int64(1000)
	source := Artifact{Bytes: 100, SHA256: "source"}
	quality := newContentQualityReport(source)
	quality.State = "complete"
	quality.BlackIntervals = []ContentQualityInterval{{StartMS: 2000, EndMS: 4000, DurationMS: 2000}}
	report := Report{Manifest: Manifest{Stream: source}, Metrics: Metrics{FirstVideoMS: &first},
		Findings:    []Finding{assessmentFinding("F001", "video_arrival_gap")},
		Player:      &PlayerReport{Stalls: []PlayerStall{{StartMS: 3500, EndMS: 4000, DurationMS: 500}}},
		RTMPControl: &RTMPControlReport{Messages: []RTMPControlMessage{{ArrivalMS: 3600, Level: "error"}}},
		Throughput:  &ThroughputReport{AverageMediaKbps: 100, Samples: []ThroughputSample{{Second: 3, DurationMS: 1000, NetworkKbps: 0}}}}
	report.Findings[0].StartMS, report.Findings[0].EndMS = 3200, 3400
	applyContentCorrelations(report, &quality)
	if quality.CorrelationAlignment != "first_media_estimate" || len(quality.Correlations) != 1 {
		t.Fatalf("missing correlation: %+v", quality)
	}
	correlation := quality.Correlations[0]
	if correlation.AlignedStartMS != 3000 || correlation.AlignedEndMS != 5000 || len(correlation.NetworkFindingIDs) != 1 ||
		len(correlation.PlayerStalls) != 1 || len(correlation.RTMPControls) != 1 || len(correlation.LowThroughputSecs) != 1 || correlation.Status != "candidate" {
		t.Fatalf("incorrect cross-layer correlation: %+v", correlation)
	}
	stored := Report{ContentQuality: &quality}
	if err := decodeStoredContentQuality(stored, &report); err != nil {
		t.Fatal(err)
	}
}

func TestContentWithoutDetectedAnomalyStillGuidesPlayerStall(t *testing.T) {
	quality := newContentQualityReport(Artifact{})
	quality.State = "complete"
	report := Report{Player: &PlayerReport{Stalls: []PlayerStall{{StartMS: 1000, EndMS: 2000, DurationMS: 1000}}}}
	applyContentCorrelations(report, &quality)
	if len(quality.Correlations) != 1 || quality.Correlations[0].ContentKind != "none_detected" || len(quality.Correlations[0].PlayerStalls) != 1 {
		t.Fatalf("missing no-content-anomaly direction: %+v", quality.Correlations)
	}
}
