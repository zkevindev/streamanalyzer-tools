package diagnostic

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func videoTestNumber(value int64) *int64 { return &value }
func videoTestSample(at, total, dropped, presented, gap int64) PlayerSample {
	return PlayerSample{AtMS: at, ReadyState: 4, Video: &PlayerVideoSample{QualitySource: "playback_quality", TotalFrames: videoTestNumber(total), DroppedFrames: videoTestNumber(dropped), PresentedFrames: videoTestNumber(presented), FrameAgeMS: videoTestNumber(0), MaxFrameGapMS: videoTestNumber(gap)}}
}

func TestPlayerVideoDeltasRespectContinuousPlaybackWindows(t *testing.T) {
	start := time.Now().UTC()
	record := PlayerRecording{Version: 2, Revision: 1, StartedAt: start, ElapsedMS: 4000, Complete: true, Source: "same_capture",
		Events: []PlayerEvent{{AtMS: 100, Kind: "playing"}, {AtMS: 1000, Kind: "pause"}, {AtMS: 1500, Kind: "playing"}, {AtMS: 2000, Kind: "hidden"}, {AtMS: 2500, Kind: "visible"}, {AtMS: 2600, Kind: "playing"}, {AtMS: 3100, Kind: "seeking"}, {AtMS: 3200, Kind: "seeked"}, {AtMS: 3300, Kind: "playing"}, {AtMS: 3500, Kind: "stop"}},
		Samples: []PlayerSample{
			videoTestSample(0, 0, 0, 0, 0), videoTestSample(100, 10, 1, 8, 20), videoTestSample(300, 20, 2, 17, 30), videoTestSample(900, 30, 3, 26, 40),
			videoTestSample(1000, 90, 9, 81, 999), videoTestSample(1400, 95, 9, 85, 999), videoTestSample(1500, 100, 10, 90, 50), videoTestSample(1700, 110, 11, 99, 60),
			videoTestSample(2000, 500, 50, 450, 1999), videoTestSample(2300, 550, 55, 495, 1999), videoTestSample(2500, 555, 55, 500, 1999),
			videoTestSample(2600, 1000, 100, 900, 70), videoTestSample(2800, 1020, 102, 918, 80),
			videoTestSample(3100, 1500, 150, 1350, 3000), videoTestSample(3200, 1600, 160, 1440, 3000), videoTestSample(3300, 2000, 200, 1800, 90), videoTestSample(3400, 2005, 201, 1804, 95),
			videoTestSample(3500, 9000, 900, 8100, 3400), videoTestSample(3600, 9999, 999, 9000, 3500),
		}}
	if err := validatePlayerRecording(record, start); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(record)
	got := analyzePlayer(record, Observation{StartedAt: start})
	video := got.Video
	if video == nil || video.QualitySource != "playback_quality" || video.TotalFrames == nil || *video.TotalFrames != 55 || video.DroppedFrames == nil || *video.DroppedFrames != 6 ||
		video.PresentedFrames == nil || *video.PresentedFrames != 49 || video.MaxFrameGapMS == nil || *video.MaxFrameGapMS != 95 || video.CounterResets != 0 || video.SampleCount != 9 ||
		video.DroppedRatio == nil || math.Abs(*video.DroppedRatio-6.0/55) > 1e-9 {
		t.Fatalf("inactive intervals entered video counters: %+v", video)
	}
	if len(got.BufferSamples) != len(record.Samples) || got.BufferSamples[1].Video == nil || got.BufferSamples[1].Video == record.Samples[1].Video {
		t.Fatal("report did not independently preserve raw video evidence")
	}
	*got.BufferSamples[1].Video.TotalFrames = 999
	after, _ := json.Marshal(record)
	if !bytes.Equal(before, after) {
		t.Fatal("report video metadata mutated source recording")
	}
}

func TestPlayerVideoCounterResetsAndUnsupportedDifferFromZero(t *testing.T) {
	record := PlayerRecording{Revision: 1, ElapsedMS: 1000, Events: []PlayerEvent{{AtMS: 0, Kind: "playing"}}}
	if got := analyzePlayer(record, Observation{}).Video; got != nil {
		t.Fatal("legacy recording invented video metrics")
	}
	record.Samples = []PlayerSample{{AtMS: 100, Video: &PlayerVideoSample{QualitySource: "unavailable"}}, {AtMS: 500, Video: &PlayerVideoSample{QualitySource: "unavailable"}}}
	got := analyzePlayer(record, Observation{}).Video
	if got == nil || got.QualitySource != "unavailable" || got.TotalFrames != nil || got.DroppedFrames != nil || got.PresentedFrames != nil || got.DroppedRatio != nil || got.MaxFrameGapMS != nil {
		t.Fatalf("unsupported metrics became measured zero: %+v", got)
	}
	record.Samples = []PlayerSample{videoTestSample(100, 0, 0, 0, 0), videoTestSample(500, 0, 0, 0, 0)}
	got = analyzePlayer(record, Observation{}).Video
	if got.TotalFrames == nil || *got.TotalFrames != 0 || got.DroppedFrames == nil || *got.DroppedFrames != 0 || got.DroppedRatio != nil || got.PresentedFrames == nil || *got.PresentedFrames != 0 {
		t.Fatalf("observed zero was lost: %+v", got)
	}
	record.Samples = []PlayerSample{videoTestSample(100, 100, 10, 90, 10), videoTestSample(200, 120, 12, 108, 20), videoTestSample(300, 5, 1, 4, 20), videoTestSample(400, 10, 2, 8, 30)}
	got = analyzePlayer(record, Observation{}).Video
	if got.CounterResets != 1 || got.TotalFrames == nil || *got.TotalFrames != 25 || got.DroppedFrames == nil || *got.DroppedFrames != 3 || got.PresentedFrames == nil || *got.PresentedFrames != 22 {
		t.Fatalf("counter reset produced negative or absolute counts: %+v", got)
	}
	record.Samples = []PlayerSample{videoTestSample(100, 100, 10, 90, 10), videoTestSample(200, 110, 11, 99, 20), videoTestSample(300, 120, 12, 108, 30), videoTestSample(400, 130, 13, 117, 40)}
	record.Samples[2].Video.QualitySource = "webkit"
	record.Samples[3].Video.QualitySource = "webkit"
	got = analyzePlayer(record, Observation{}).Video
	if got.QualitySource != "mixed" || got.TotalFrames == nil || *got.TotalFrames != 20 || got.DroppedFrames == nil || *got.DroppedFrames != 2 || got.PresentedFrames == nil || *got.PresentedFrames != 27 {
		t.Fatalf("incompatible quality sources were compared: %+v", got)
	}
}

func TestPlayerVideoValidationAndRecordingVersions(t *testing.T) {
	start := time.Now().UTC()
	base := PlayerRecording{Version: 2, Revision: 1, StartedAt: start, ElapsedMS: 1000, Source: "same_capture", Samples: []PlayerSample{videoTestSample(100, 10, 1, 8, 50)}}
	if err := validatePlayerRecording(base, start); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PlayerRecording){
		func(r *PlayerRecording) { r.Version = 1 }, func(r *PlayerRecording) { r.Version = 5 },
		func(r *PlayerRecording) { r.Samples[0].Video.QualitySource = "unknown" },
		func(r *PlayerRecording) { r.Samples[0].Video.TotalFrames = videoTestNumber(-1) },
		func(r *PlayerRecording) { r.Samples[0].Video.TotalFrames = videoTestNumber(maxPlayerVideoCounter + 1) },
		func(r *PlayerRecording) { r.Samples[0].Video.DroppedFrames = videoTestNumber(11) },
		func(r *PlayerRecording) { r.Samples[0].Video.DroppedFrames = nil },
		func(r *PlayerRecording) { r.Samples[0].Video.PresentedFrames = videoTestNumber(-1) },
		func(r *PlayerRecording) { r.Samples[0].Video.FrameAgeMS = videoTestNumber(101) },
		func(r *PlayerRecording) { r.Samples[0].Video.MaxFrameGapMS = videoTestNumber(-1) },
		func(r *PlayerRecording) { r.Samples[0].Video.QualitySource = "unavailable" },
	} {
		data, _ := json.Marshal(base)
		var invalid PlayerRecording
		if err := json.Unmarshal(data, &invalid); err != nil {
			t.Fatal(err)
		}
		change(&invalid)
		if !errors.Is(validatePlayerRecording(invalid, start), ErrInvalid) {
			t.Fatalf("invalid imported video counters accepted: %+v", invalid)
		}
	}
	base.Version = 1
	base.Samples[0].Video = nil
	if err := validatePlayerRecording(base, start); err != nil {
		t.Fatalf("legacy recording rejected: %v", err)
	}
	base.Version = 2
	base.Samples[0].Video = &PlayerVideoSample{QualitySource: "unavailable", PresentedFrames: videoTestNumber(1), FrameAgeMS: videoTestNumber(0)}
	if err := validatePlayerRecording(base, start); err != nil {
		t.Fatalf("frame callback without native quality API rejected: %v", err)
	}
}

func TestPlayerVideoAppendOnlyEqualityAcrossDecodedSnapshots(t *testing.T) {
	service, id := openRunForExplanation(t, nil)
	record := registerTestPlayer(t, service, id)
	record.Version = 2
	record.Samples = []PlayerSample{videoTestSample(20, 10, 0, 1, 0)}
	if _, err := service.SavePlayerRecording(id, "test-player-grant", record); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(record)
	var next PlayerRecording
	if err := json.Unmarshal(data, &next); err != nil {
		t.Fatal(err)
	}
	if next.Samples[0].Video == record.Samples[0].Video {
		t.Fatal("test did not create independent video pointers")
	}
	next.Revision, next.ElapsedMS = 2, 200
	next.Events = append(next.Events, PlayerEvent{AtMS: 50, Kind: "playing"})
	next.Samples = append(next.Samples, videoTestSample(100, 20, 1, 10, 30))
	if _, err := service.SavePlayerRecording(id, "test-player-grant", next); err != nil {
		t.Fatalf("valid append-only decoded snapshot rejected: %v", err)
	}
	data, _ = json.Marshal(next)
	var changed PlayerRecording
	_ = json.Unmarshal(data, &changed)
	changed.Revision = 3
	*changed.Samples[0].Video.TotalFrames = 11
	if _, err := service.SavePlayerRecording(id, "test-player-grant", changed); !errors.Is(err, ErrReportChanged) {
		t.Fatalf("previous video counters could be rewritten: %v", err)
	}
	next.Revision, next.Complete = 3, true
	if _, err := service.SavePlayerRecording(id, "test-player-grant", next); err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(next)
	var retry PlayerRecording
	_ = json.Unmarshal(data, &retry)
	if _, err := service.SavePlayerRecording(id, "test-player-grant", retry); err != nil {
		t.Fatalf("identical finalized JSON retry rejected: %v", err)
	}
}

func TestPlayerVideoModelInputPreservesNumericSummary(t *testing.T) {
	report := Report{Player: &PlayerReport{State: "complete", Source: "same_capture", Video: &PlayerVideoReport{QualitySource: "playback_quality", TotalFrames: videoTestNumber(100), DroppedFrames: videoTestNumber(5), PresentedFrames: videoTestNumber(90), MaxFrameGapMS: videoTestNumber(250), SampleCount: 10}}}
	data, err := modelInput(report)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Player struct {
			Video *PlayerVideoReport `json:"video"`
		} `json:"player"`
	}
	if json.Unmarshal(data, &got) != nil || got.Player.Video == nil || got.Player.Video.TotalFrames == nil || *got.Player.Video.TotalFrames != 100 || got.Player.Video.DroppedFrames == nil || *got.Player.Video.DroppedFrames != 5 {
		t.Fatalf("model lost numeric video evidence: %s", data)
	}
	if bytes.Contains(data, []byte("data:image")) || bytes.Contains(data, []byte("request_headers")) {
		t.Fatalf("model projection gained raw artifacts: %s", data)
	}
}

func TestPlayerVideoEvidenceBundleReplayPreservesRawCounters(t *testing.T) {
	service, id, _ := openStallServiceTestRun(t, nil, false)
	dir := service.runDir(id)
	record, err := readPlayerRecording(dir)
	if err != nil {
		t.Fatal(err)
	}
	record.Version, record.Revision = 2, 2
	for index := range record.Samples {
		sample := &record.Samples[index]
		sample.Video = &PlayerVideoSample{QualitySource: "playback_quality", TotalFrames: videoTestNumber(int64(100 + index*20)), DroppedFrames: videoTestNumber(int64(index)), PresentedFrames: videoTestNumber(int64(90 + index*18)), FrameAgeMS: videoTestNumber(10), MaxFrameGapMS: videoTestNumber(50)}
	}
	if err := writePlayerRecording(dir, record); err != nil {
		t.Fatal(err)
	}
	detail, err := service.Detail(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := attachPlayerArtifact(dir, detail.Report); err != nil {
		t.Fatal(err)
	}
	if err := persistReport(dir, *detail.Report); err != nil {
		t.Fatal(err)
	}
	if detail.Report.Player.Video == nil || detail.Report.Player.Video.TotalFrames == nil || *detail.Report.Player.Video.TotalFrames != 60 {
		t.Fatalf("new recording lost counters: %+v", detail.Report.Player.Video)
	}
	var bundle bytes.Buffer
	if err := service.WriteBundle(id, &bundle); err != nil {
		t.Fatal(err)
	}
	imported, err := service.Import(bytes.NewReader(bundle.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.Detail(imported.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(detail.Report, replayed.Report) {
		t.Fatal("offline replay changed v2 video evidence")
	}
}
