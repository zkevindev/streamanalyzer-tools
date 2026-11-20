package diagnostic

import "testing"

func TestAnalysisProfilesChangeRulesAndRemainExplicit(t *testing.T) {
	events := []Event{{Kind: "video", Keyframe: true, ArrivalMS: 1500, Size: 100}, {Kind: "video", Keyframe: true, ArrivalMS: 4000, Size: 100}}
	for _, test := range []struct {
		profile           string
		wantSlow, wantGap bool
	}{
		{profile: "standard"}, {profile: "low-latency", wantSlow: true, wantGap: true}, {profile: "camera"},
	} {
		thresholds, err := resolveAnalysisThresholds(test.profile, nil)
		if err != nil {
			t.Fatal(err)
		}
		manifest := Manifest{Protocol: ProtocolHTTPFLV, AnalysisProfile: test.profile, AnalysisThresholds: thresholds,
			Observation: Observation{ElapsedMS: 5000, EndReason: "duration_limit"}}
		report := analyze(manifest, events)
		var slow, gap bool
		for _, finding := range report.Findings {
			slow = slow || finding.Code == "slow_first_video"
			gap = gap || finding.Code == "video_arrival_gap"
		}
		if slow != test.wantSlow || gap != test.wantGap || report.Manifest.AnalysisThresholds != thresholds {
			t.Fatalf("profile %s produced slow=%v gap=%v: %+v", test.profile, slow, gap, report.Findings)
		}
		if !validManifestThresholds(report.Manifest) {
			t.Fatal("resolved profile was not replayable")
		}
	}
}

func TestCustomThresholdValidation(t *testing.T) {
	custom := AnalysisThresholds{FirstFrameWarnMS: 500, VideoGapWarnMS: 600, DTSGapWarnMS: 700, GOPWarnMS: 800}
	resolved, err := resolveAnalysisThresholds("custom", &custom)
	if err != nil || resolved != custom {
		t.Fatalf("custom thresholds rejected: %+v %v", resolved, err)
	}
	custom.FirstFrameWarnMS = 0
	if _, err := resolveAnalysisThresholds("custom", &custom); err == nil {
		t.Fatal("accepted incomplete custom thresholds")
	}
}
