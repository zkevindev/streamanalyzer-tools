package analysis

import (
	"testing"

	"streamanalyzer/internal/models"
)

// liveFrames builds a clean 25fps stream with a keyframe every 2s, arriving in
// realtime (arrival advances with DTS).
func liveFrames(seconds int) []Frame {
	var frames []Frame
	base := int64(1_700_000_000_000)
	for i := 0; i < seconds*25; i++ {
		dts := int64(i * 40)
		key := i%50 == 0
		kind := "P"
		if key {
			kind = "I"
		}
		frames = append(frames, Frame{
			IsVideo: true, DTS: dts, PTS: dts, Len: 1000, FrameType: kind,
			IsKey: key, ArrivalMs: base + dts, NALUTypes: "Slice:1",
		})
		// audio every 40ms too, keeps A/V aligned
		frames = append(frames, Frame{
			IsVideo: false, DTS: dts, Len: 100, ArrivalMs: base + dts,
		})
	}
	return frames
}

func secondStats(n int, videoBytes int64) []*models.SecondStat {
	var out []*models.SecondStat
	for i := 0; i < n; i++ {
		out = append(out, &models.SecondStat{Second: i, VideoBytes: videoBytes, AudioBytes: 2500, VideoFPS: 25})
	}
	return out
}

func findAlert(alerts []models.Alert, code string) *models.Alert {
	for i := range alerts {
		if alerts[i].Code == code {
			return &alerts[i]
		}
	}
	return nil
}

func TestHealthyStreamHasNoAlerts(t *testing.T) {
	res := Analyze(liveFrames(10), secondStats(10, 25000))

	for _, a := range res.Health.Alerts {
		if a.Severity == "error" || a.Severity == "warn" {
			t.Errorf("clean stream raised %s: %s (%s)", a.Severity, a.Code, a.Detail)
		}
	}
	if res.Health.Level != "good" {
		t.Errorf("level = %q, want good (summary: %s)", res.Health.Level, res.Health.Summary)
	}
	if res.Health.Score != 100 {
		t.Errorf("score = %d, want 100", res.Health.Score)
	}
	if res.Health.VideoFPSAvg != 25 {
		t.Errorf("fps avg = %v, want 25", res.Health.VideoFPSAvg)
	}
}

func TestDetectsDTSBackwards(t *testing.T) {
	frames := liveFrames(4)
	// Push one video frame's DTS behind its predecessor.
	for i := range frames {
		if frames[i].IsVideo && frames[i].DTS == 2000 {
			frames[i].DTS = 1000
			frames[i].PTS = 1000
			break
		}
	}

	res := Analyze(frames, secondStats(4, 25000))
	a := findAlert(res.Health.Alerts, "dts_backwards")
	if a == nil {
		t.Fatalf("expected dts_backwards alert, got %+v", res.Health.Alerts)
	}
	if a.Severity != "error" {
		t.Errorf("severity = %q, want error", a.Severity)
	}
	if res.Health.Level != "bad" {
		t.Errorf("level = %q, want bad", res.Health.Level)
	}
}

func TestDetectsPTSLessThanDTS(t *testing.T) {
	frames := liveFrames(4)
	for i := range frames {
		if frames[i].IsVideo && frames[i].DTS == 400 {
			frames[i].PTS = frames[i].DTS - 40
			break
		}
	}

	res := Analyze(frames, secondStats(4, 25000))
	if a := findAlert(res.Health.Alerts, "pts_lt_dts"); a == nil {
		t.Fatalf("expected pts_lt_dts alert, got %+v", res.Health.Alerts)
	}
}

func TestDetectsTimestampGap(t *testing.T) {
	frames := liveFrames(4)
	// Shift the tail forward, creating one 5s hole in video DTS.
	for i := range frames {
		if frames[i].IsVideo && frames[i].DTS >= 2000 {
			frames[i].DTS += 5000
			frames[i].PTS += 5000
		}
	}

	res := Analyze(frames, secondStats(4, 25000))
	a := findAlert(res.Health.Alerts, "dts_gap")
	if a == nil {
		t.Fatalf("expected dts_gap alert, got %+v", res.Health.Alerts)
	}
	if a.Count != 1 {
		t.Errorf("gap count = %d, want 1", a.Count)
	}
}

func TestDetectsAVDesync(t *testing.T) {
	frames := liveFrames(6)
	// Drag audio 800ms behind video.
	for i := range frames {
		if !frames[i].IsVideo {
			frames[i].DTS -= 800
		}
	}

	res := Analyze(frames, secondStats(6, 25000))
	a := findAlert(res.Health.Alerts, "av_desync")
	if a == nil {
		t.Fatalf("expected av_desync alert, got %+v", res.Health.Alerts)
	}
	if res.Health.AVSyncMaxMs < avSyncErrorMs {
		t.Errorf("AVSyncMaxMs = %d, want >= %d", res.Health.AVSyncMaxMs, avSyncErrorMs)
	}
	if len(res.AVSync) == 0 {
		t.Error("expected AV sync series to be populated")
	}
}

func TestDetectsStall(t *testing.T) {
	frames := liveFrames(6)
	// Freeze arrival for 4s midway: timestamps fine, data simply stopped coming.
	for i := range frames {
		if frames[i].ArrivalMs >= 1_700_000_003_000 {
			frames[i].ArrivalMs += 4000
		}
	}

	res := Analyze(frames, secondStats(6, 25000))
	if a := findAlert(res.Health.Alerts, "stream_stall"); a == nil {
		t.Fatalf("expected stream_stall alert, got %+v", res.Health.Alerts)
	}
}

func TestNonRealtimeSkipsJitterChecks(t *testing.T) {
	frames := liveFrames(6)
	// Everything arrives at once, as when reading a file at full speed.
	for i := range frames {
		frames[i].ArrivalMs = 1_700_000_000_000
	}

	res := Analyze(frames, secondStats(6, 25000))
	if a := findAlert(res.Health.Alerts, "not_realtime"); a == nil {
		t.Fatalf("expected not_realtime info, got %+v", res.Health.Alerts)
	}
	if findAlert(res.Health.Alerts, "jitter_high") != nil {
		t.Error("jitter must not be judged for non-realtime pacing")
	}
	if findAlert(res.Health.Alerts, "stream_stall") != nil {
		t.Error("stall must not be judged for non-realtime pacing")
	}
	if len(res.Jitter) != 0 {
		t.Error("jitter series should be empty for non-realtime pacing")
	}
}

func TestDetectsKeyframeGapAndReportsGOP(t *testing.T) {
	var frames []Frame
	base := int64(1_700_000_000_000)
	for i := 0; i < 25*15; i++ {
		dts := int64(i * 40)
		key := i == 0 // single keyframe at the very start
		kind := "P"
		if key {
			kind = "I"
		}
		frames = append(frames, Frame{
			IsVideo: true, DTS: dts, PTS: dts, Len: 1000,
			FrameType: kind, IsKey: key, ArrivalMs: base + dts,
		})
	}

	res := Analyze(frames, secondStats(15, 25000))
	if a := findAlert(res.Health.Alerts, "no_keyframe"); a == nil {
		t.Fatalf("expected no_keyframe alert, got %+v", res.Health.Alerts)
	}
}

func TestFrameKindsAndBFrameNotice(t *testing.T) {
	frames := liveFrames(4)
	for i := range frames {
		if frames[i].IsVideo && frames[i].FrameType == "P" && frames[i].DTS%200 == 40 {
			frames[i].FrameType = "B"
			frames[i].Len = 300
		}
	}

	res := Analyze(frames, secondStats(4, 25000))

	var kinds []string
	for _, k := range res.FrameKind {
		kinds = append(kinds, k.Kind)
		if k.Count == 0 || k.AvgBytes <= 0 {
			t.Errorf("frame kind %s has empty stats: %+v", k.Kind, k)
		}
	}
	if len(kinds) < 3 {
		t.Errorf("frame kinds = %v, want I/P/B", kinds)
	}
	if findAlert(res.Health.Alerts, "b_frames") == nil {
		t.Error("expected b_frames info alert")
	}
}

func TestBitrateSpikeDetected(t *testing.T) {
	stats := secondStats(10, 25000)
	stats[5].VideoBytes = 400000 // sudden 16x second

	res := Analyze(liveFrames(10), stats)
	if a := findAlert(res.Health.Alerts, "bitrate_spike"); a == nil {
		t.Fatalf("expected bitrate_spike alert, got %+v", res.Health.Alerts)
	}
	if res.Health.BitratePeakRatio <= bitratePeakWarn {
		t.Errorf("peak ratio = %v, want > %v", res.Health.BitratePeakRatio, bitratePeakWarn)
	}
}

func TestNALUStatsAggregated(t *testing.T) {
	frames := []Frame{
		{IsVideo: true, DTS: 0, PTS: 0, Len: 900, FrameType: "I", IsKey: true, NALUTypes: "SPS:1;PPS:1;IDR:2"},
		{IsVideo: true, DTS: 40, PTS: 40, Len: 100, FrameType: "P", NALUTypes: "Slice:1"},
		{IsVideo: true, DTS: 80, PTS: 80, Len: 100, FrameType: "P", NALUTypes: "Slice:1;SEI:1"},
	}

	res := Analyze(frames, secondStats(1, 1100))
	got := map[string]int{}
	for _, n := range res.NALU {
		got[n.Name] = n.Count
	}
	for name, want := range map[string]int{"SPS": 1, "PPS": 1, "IDR": 2, "Slice": 2, "SEI": 1} {
		if got[name] != want {
			t.Errorf("NALU %s = %d, want %d (all: %+v)", name, got[name], want, res.NALU)
		}
	}
}

func TestEmptyInput(t *testing.T) {
	res := Analyze(nil, nil)
	if res == nil || res.Health == nil {
		t.Fatal("expected a health result for empty input")
	}
	if res.Health.Summary == "" {
		t.Error("expected a summary explaining there is no data")
	}
}
