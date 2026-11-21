package offline

import (
	"testing"

	"streamanalyzer/internal/models"
)

func TestAnalyzeSamplesTimeline(t *testing.T) {
	samples := []timedSample{
		{media: "video", dts: 0, size: 1000, key: true},
		{media: "video", dts: 40, size: 100, key: false},
		{media: "video", dts: 80, size: 100, key: false},
		{media: "video", dts: 2000, size: 800, key: true},
		{media: "audio", dts: 0, size: 50},
		{media: "audio", dts: 2000, size: 50},
	}
	stats := analyzeSamples(samples)
	if stats.VideoFrames != 4 || stats.AudioFrames != 2 || stats.Keyframes != 2 {
		t.Fatalf("counts: %+v", stats)
	}
	if stats.DurationMS != 2000 {
		t.Fatalf("duration %d", stats.DurationMS)
	}
	if stats.FrameRate != 1.5 {
		t.Fatalf("fps %v", stats.FrameRate)
	}
	if stats.GOPMinMS != 2000 || stats.GOPMaxMS != 2000 || len(stats.GOPIntervalsMS) != 1 {
		t.Fatalf("gop: %+v", stats)
	}
	if stats.LargeGaps != 1 || stats.MaxGapMS != 1920 {
		t.Fatalf("gaps: %+v", stats)
	}
	if len(stats.Warnings) == 0 {
		t.Fatal("expected a gap warning")
	}
}

func TestAnalyzeSamplesDTSRegression(t *testing.T) {
	stats := analyzeSamples([]timedSample{
		{media: "video", dts: 100, size: 10, key: true},
		{media: "video", dts: 40, size: 10},
	})
	if stats.DTSRegressions != 1 {
		t.Fatalf("regressions %d", stats.DTSRegressions)
	}
}

func TestAttachMediaProfileCapsFrames(t *testing.T) {
	frames := make([]models.OfflineFrameDetail, maxStoredFrames+5)
	for i := range frames {
		frames[i] = models.OfflineFrameDetail{MediaType: "video", DTS: int64(i * 40), FrameLen: 10, FrameType: "P"}
	}
	frames[0].FrameType = "I"
	var flow models.OfflineFlowResult
	attachMediaProfile(&flow, frames, models.OfflineMediaProfile{VideoCodec: "h264", Width: 320, Height: 240})
	if len(flow.FrameDetails) != maxStoredFrames || flow.Profile == nil || !flow.Profile.FramesTruncated {
		t.Fatalf("cap: stored=%d profile=%+v", len(flow.FrameDetails), flow.Profile)
	}
	if flow.Profile.FrameTotal != len(frames) || flow.Profile.Width != 320 || flow.VideoCodec != "h264" {
		t.Fatalf("profile: %+v codec=%s", flow.Profile, flow.VideoCodec)
	}
}
