package diagnostic

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

func schedulingClone(t *testing.T, value PlayerRecording) PlayerRecording {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result PlayerRecording
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func schedulingTestRecording() PlayerRecording {
	return PlayerRecording{Version: 3, Revision: 1, StartedAt: time.Now().UTC(), Source: "same_capture", ElapsedMS: 1000,
		Events: []PlayerEvent{{Kind: "start"}, {AtMS: 100, Kind: "playing"}}, Samples: []PlayerSample{},
		Scheduling: &PlayerSchedulingRecording{LongTaskSupport: "supported", ObservationStartMS: 0, ObservationEndMS: 1000, LongTasks: []PlayerSchedulingInterval{{StartMS: 100, EndMS: 150}}, TimerDelays: []PlayerSchedulingInterval{{StartMS: 200, EndMS: 300}}},
	}
}

func TestPlayerSchedulingActiveIntervalsExcludeUserActionsStartupStopAndEOF(t *testing.T) {
	record := schedulingTestRecording()
	record.ElapsedMS, record.Complete = 10000, true
	record.Events = []PlayerEvent{{Kind: "start"}, {AtMS: 1000, Kind: "playing"}, {AtMS: 2000, Kind: "pause"}, {AtMS: 3000, Kind: "playing"}, {AtMS: 4000, Kind: "seeking"}, {AtMS: 4500, Kind: "seeked"}, {AtMS: 5000, Kind: "playing"}, {AtMS: 6000, Kind: "hidden"}, {AtMS: 6500, Kind: "visible"}, {AtMS: 7000, Kind: "playing"}, {AtMS: 7200, Kind: "waiting"}, {AtMS: 8200, Kind: "playing"}, {AtMS: 9000, Kind: "stop"}, {AtMS: 9500, Kind: "playing"}}
	record.Scheduling = &PlayerSchedulingRecording{LongTaskSupport: "supported", ObservationStartMS: 500, ObservationEndMS: 9800, DroppedLongTasks: 2, DroppedTimerDelays: 3,
		LongTasks:   []PlayerSchedulingInterval{{600, 800}, {900, 1100}, {1900, 2100}, {2500, 3500}, {3900, 5100}, {5900, 7100}, {8200, 8600}, {9100, 9300}},
		TimerDelays: []PlayerSchedulingInterval{{1500, 2500}, {3500, 4500}, {5500, 6500}, {7500, 9500}},
	}
	if err := validatePlayerRecording(record, record.StartedAt); err != nil {
		t.Fatal(err)
	}
	observation := Observation{StartedAt: record.StartedAt.Add(-200 * time.Millisecond), ElapsedMS: 8500, EndReason: "eof"}
	before, _ := json.Marshal(record)
	player := analyzePlayer(record, observation)
	value := player.Scheduling
	wantActive := []PlayerSchedulingInterval{{1200, 2200}, {3200, 4200}, {5200, 6200}, {7200, 8500}}
	wantTasks := []PlayerSchedulingInterval{{1200, 1300}, {2100, 2200}, {3200, 3700}, {4100, 4200}, {5200, 5300}, {6100, 6200}, {7200, 7300}, {8400, 8500}}
	if value == nil || value.ActiveCoverageMS != 4300 || !reflect.DeepEqual(value.ActiveIntervals, wantActive) || value.ObservationStartMS != 700 || value.ObservationEndMS != 10000 {
		t.Fatalf("incorrect active scheduling coverage/alignment: %+v", value)
	}
	if value.LongTasks == nil || value.LongTasks.Count != 8 || value.LongTasks.TotalMS != 1200 || value.LongTasks.MaxMS != 500 || !reflect.DeepEqual(value.LongTasks.Intervals, wantTasks) {
		t.Fatalf("inactive portions leaked into native task metrics: %+v", value.LongTasks)
	}
	if value.TimerDelays == nil || value.TimerDelays.Count != 4 || value.TimerDelays.TotalMS != 2300 || value.TimerDelays.MaxMS != 800 || value.DroppedLongTasks != 2 || value.DroppedTimerDelays != 3 {
		t.Fatalf("timer clipping/truncation incorrect: %+v", value)
	}
	without := record
	without.Scheduling = nil
	legacy := analyzePlayer(without, observation)
	if player.StallCount != 1 || player.StallCount != legacy.StallCount || player.TotalStallMS != legacy.TotalStallMS || !reflect.DeepEqual(player.BufferSamples, legacy.BufferSamples) {
		t.Fatal("scheduling observations synthesized stalls or interpolated buffer evidence")
	}
	after, _ := json.Marshal(record)
	if !bytes.Equal(before, after) {
		t.Fatal("analysis mutated immutable raw scheduling evidence")
	}
	value.LongTasks.Intervals[0].StartMS++
	if record.Scheduling.LongTasks[1].StartMS != 900 {
		t.Fatal("summary intervals alias the raw recording")
	}
	unknownEOF := analyzePlayer(record, Observation{StartedAt: observation.StartedAt, ElapsedMS: 1})
	if unknownEOF.Scheduling.ActiveCoverageMS != 5000 {
		t.Fatal("unknown/pending EOF was treated as a capture cutoff")
	}
}

func TestPlayerSchedulingUnsupportedObservedZeroAndNoCoverageStayDistinct(t *testing.T) {
	for _, test := range []struct {
		name, support            string
		noPlaying, emptyCoverage bool
		native, timer            bool
	}{
		{"supported observed zero", "supported", false, false, true, true},
		{"unsupported native but observed timer", "unsupported", false, false, false, true},
		{"never playing", "supported", true, false, false, false},
		{"empty observation coverage", "supported", false, true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := schedulingTestRecording()
			record.Scheduling.LongTaskSupport = test.support
			record.Scheduling.LongTasks, record.Scheduling.TimerDelays = nil, nil
			if test.noPlaying {
				record.Events = []PlayerEvent{{Kind: "start"}}
			}
			if test.emptyCoverage {
				record.Scheduling.ObservationEndMS = 0
			}
			value := analyzePlayer(record, Observation{StartedAt: record.StartedAt}).Scheduling
			if value == nil || (value.LongTasks != nil) != test.native || (value.TimerDelays != nil) != test.timer {
				t.Fatalf("unknown native metrics were conflated with measured zero: %+v", value)
			}
			for _, metrics := range []*PlayerSchedulingMetrics{value.LongTasks, value.TimerDelays} {
				if metrics != nil && (metrics.Count != 0 || metrics.TotalMS != 0 || metrics.MaxMS != 0 || metrics.Intervals == nil) {
					t.Fatalf("observed zero lacks explicit empty metrics: %+v", metrics)
				}
			}
		})
	}
}

func TestPlayerSchedulingZeroLengthKnownEOFExcludesCoverageButPendingZeroDoesNot(t *testing.T) {
	record := schedulingTestRecording()
	pending := analyzePlayer(record, Observation{StartedAt: record.StartedAt, ElapsedMS: 0}).Scheduling
	if pending == nil || pending.ActiveCoverageMS != 900 || pending.LongTasks == nil || pending.LongTasks.Count != 1 || pending.TimerDelays == nil || pending.TimerDelays.Count != 1 {
		t.Fatalf("pending capture's unknown end was treated as a known zero-length observation: %+v", pending)
	}
	finished := analyzePlayer(record, Observation{StartedAt: record.StartedAt, ElapsedMS: 0, EndReason: "eof"}).Scheduling
	if finished == nil || finished.ActiveCoverageMS != 0 || len(finished.ActiveIntervals) != 0 || finished.LongTasks != nil || finished.TimerDelays != nil {
		t.Fatalf("known zero-length capture retained scheduling coverage or fabricated observed-zero metrics: %+v", finished)
	}
}

func TestPlayerSchedulingValidationAndLegacyVersions(t *testing.T) {
	base := schedulingTestRecording()
	for _, test := range []struct {
		name   string
		mutate func(*PlayerRecording)
	}{
		{"v1 cannot contain scheduling", func(r *PlayerRecording) { r.Version = 1 }},
		{"v2 cannot contain scheduling", func(r *PlayerRecording) { r.Version = 2 }},
		{"unknown version", func(r *PlayerRecording) { r.Version = 4 }},
		{"unknown support", func(r *PlayerRecording) { r.Scheduling.LongTaskSupport = "estimated" }},
		{"unsupported native events", func(r *PlayerRecording) { r.Scheduling.LongTaskSupport = "unsupported" }},
		{"negative coverage", func(r *PlayerRecording) { r.Scheduling.ObservationStartMS = -1 }},
		{"reversed coverage", func(r *PlayerRecording) { r.Scheduling.ObservationStartMS = 1001 }},
		{"coverage beyond recording", func(r *PlayerRecording) { r.Scheduling.ObservationEndMS++ }},
		{"task below 50ms", func(r *PlayerRecording) { r.Scheduling.LongTasks[0].EndMS = 149 }},
		{"timer below 100ms", func(r *PlayerRecording) { r.Scheduling.TimerDelays[0].EndMS = 299 }},
		{"negative interval", func(r *PlayerRecording) { r.Scheduling.LongTasks[0].StartMS = -100 }},
		{"overflowing end", func(r *PlayerRecording) { r.Scheduling.LongTasks[0].EndMS = math.MinInt64 }},
		{"task after coverage", func(r *PlayerRecording) { r.Scheduling.LongTasks[0].EndMS = 1001 }},
		{"overlapping tasks", func(r *PlayerRecording) {
			r.Scheduling.LongTasks = append(r.Scheduling.LongTasks, PlayerSchedulingInterval{140, 200})
		}},
		{"unsorted timers", func(r *PlayerRecording) {
			r.Scheduling.TimerDelays = append(r.Scheduling.TimerDelays, PlayerSchedulingInterval{0, 100})
		}},
		{"negative dropped counter", func(r *PlayerRecording) { r.Scheduling.DroppedLongTasks = -1 }},
		{"oversized dropped counter", func(r *PlayerRecording) { r.Scheduling.DroppedTimerDelays = 1_000_000_001 }},
		{"native list over bound", func(r *PlayerRecording) {
			r.Scheduling.LongTasks = make([]PlayerSchedulingInterval, maxPlayerSchedulingIntervals+1)
		}},
		{"timer list over bound", func(r *PlayerRecording) {
			r.Scheduling.TimerDelays = make([]PlayerSchedulingInterval, maxPlayerSchedulingIntervals+1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := schedulingClone(t, base)
			test.mutate(&record)
			if err := validatePlayerRecording(record, base.StartedAt); !errors.Is(err, ErrInvalid) {
				t.Fatalf("malformed raw scheduling accepted: %v", err)
			}
		})
	}
	if err := validatePlayerRecording(base, base.StartedAt); err != nil {
		t.Fatalf("exact threshold intervals rejected: %v", err)
	}
	for _, version := range []int{1, 2, 3} {
		record := schedulingClone(t, base)
		record.Version, record.Scheduling = version, nil
		if version >= 2 {
			record.Samples = []PlayerSample{{AtMS: 100, Video: &PlayerVideoSample{QualitySource: "unavailable", FrameAgeMS: videoTestNumber(0)}}}
		}
		if err := validatePlayerRecording(record, base.StartedAt); err != nil {
			t.Fatalf("legacy/version%d video compatibility failed: %v", version, err)
		}
		if analyzePlayer(record, Observation{StartedAt: base.StartedAt}).Scheduling != nil {
			t.Fatal("missing legacy scheduling was synthesized")
		}
	}
}

func TestPlayerSchedulingAppendOnlySaveDecodedSnapshotsAndFinalImmutability(t *testing.T) {
	service, id := openRunForExplanation(t, nil)
	record := registerTestPlayer(t, service, id)
	record.Version = 3
	record.Events = append(record.Events, PlayerEvent{Kind: "playing"})
	record.Scheduling = &PlayerSchedulingRecording{LongTaskSupport: "supported", ObservationEndMS: 100, LongTasks: []PlayerSchedulingInterval{{10, 60}}, TimerDelays: []PlayerSchedulingInterval{{0, 100}}, DroppedLongTasks: 3, DroppedTimerDelays: 2}
	if _, err := service.SavePlayerRecording(id, "test-player-grant", record); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*PlayerSchedulingRecording)
	}{
		{"rewrite native prefix", func(value *PlayerSchedulingRecording) { value.LongTasks[0].StartMS = 5 }},
		{"rewrite timer prefix", func(value *PlayerSchedulingRecording) { value.TimerDelays[0] = PlayerSchedulingInterval{1, 101} }},
		{"delete native prefix", func(value *PlayerSchedulingRecording) { value.LongTasks = nil }},
		{"change support", func(value *PlayerSchedulingRecording) { value.LongTaskSupport = "unsupported"; value.LongTasks = nil }},
		{"change observation start", func(value *PlayerSchedulingRecording) { value.ObservationStartMS = 1; value.TimerDelays = nil }},
		{"shrink coverage", func(value *PlayerSchedulingRecording) { value.ObservationEndMS = 90; value.TimerDelays = nil }},
		{"decrease dropped native", func(value *PlayerSchedulingRecording) { value.DroppedLongTasks-- }},
		{"decrease dropped timer", func(value *PlayerSchedulingRecording) { value.DroppedTimerDelays-- }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := schedulingClone(t, record)
			bad.Revision, bad.ElapsedMS, bad.Scheduling.ObservationEndMS = 2, 200, 200
			test.mutate(bad.Scheduling)
			if err := validatePlayerRecording(bad, record.StartedAt); err != nil {
				t.Fatalf("test did not reach prefix policy: %v", err)
			}
			if _, err := service.SavePlayerRecording(id, "test-player-grant", bad); !errors.Is(err, ErrReportChanged) {
				t.Fatalf("existing scheduling evidence was rewritten: %v", err)
			}
		})
	}
	removed := schedulingClone(t, record)
	removed.Revision, removed.Scheduling = 2, nil
	if _, err := service.SavePlayerRecording(id, "test-player-grant", removed); !errors.Is(err, ErrReportChanged) {
		t.Fatalf("existing scheduling was removed: %v", err)
	}
	record = schedulingClone(t, record)
	record.Revision, record.ElapsedMS, record.Scheduling.ObservationEndMS = 2, 200, 200
	record.Scheduling.LongTasks = append(record.Scheduling.LongTasks, PlayerSchedulingInterval{120, 180})
	record.Scheduling.TimerDelays = append(record.Scheduling.TimerDelays, PlayerSchedulingInterval{100, 200})
	record.Scheduling.DroppedLongTasks++
	if _, err := service.SavePlayerRecording(id, "test-player-grant", record); err != nil {
		t.Fatalf("equal decoded prefixes compared by pointer identity: %v", err)
	}
	if _, err := service.SavePlayerRecording(id, "test-player-grant", schedulingClone(t, record)); err != nil {
		t.Fatalf("identical decoded retry failed: %v", err)
	}
	record.Revision, record.ElapsedMS, record.Scheduling.ObservationEndMS, record.Complete = 3, 300, 300, true
	record.Scheduling.LongTasks = append(record.Scheduling.LongTasks, PlayerSchedulingInterval{220, 270})
	record.Scheduling.TimerDelays = append(record.Scheduling.TimerDelays, PlayerSchedulingInterval{200, 300})
	result, err := service.SavePlayerRecording(id, "test-player-grant", record)
	if err != nil || result.Scheduling.LongTasks.Count != 3 || result.Scheduling.TimerDelays.Count != 3 {
		t.Fatalf("final derived counts incorrect: %+v %v", result, err)
	}
	if _, err := service.SavePlayerRecording(id, "test-player-grant", schedulingClone(t, record)); err != nil {
		t.Fatal(err)
	}
	mutated := schedulingClone(t, record)
	mutated.Scheduling.LongTasks[0].StartMS--
	if err := writePlayerRecording(service.runDir(id), mutated); !errors.Is(err, ErrInvalid) {
		t.Fatalf("same-revision immutable raw artifact changed: %v", err)
	}
	mutated.Revision++
	if _, err := service.SavePlayerRecording(id, "test-player-grant", mutated); !errors.Is(err, ErrReportChanged) {
		t.Fatalf("final scheduling recording accepted further edits: %v", err)
	}
}

func TestPlayerSchedulingObservedFindingsDoNotClaimRootCauseOrDuplicateOnRebuild(t *testing.T) {
	record := schedulingTestRecording()
	player := analyzePlayer(record, Observation{StartedAt: record.StartedAt})
	report := Report{Player: player, Findings: []Finding{}}
	addPlayerFindings(&report)
	if len(report.Findings) != 2 || !slices.ContainsFunc(report.Findings, func(f Finding) bool {
		return f.Code == "player_long_tasks_observed" && f.Occurrences == 1 && len(f.Evidence) == 1 && f.Evidence[0].DurationMS == 50
	}) || !slices.ContainsFunc(report.Findings, func(f Finding) bool { return f.Code == "player_timer_delays_observed" }) || report.Player.StallCount != 0 {
		t.Fatalf("global observed scheduling evidence incorrect: %+v", report.Findings)
	}
	before, _ := json.Marshal(report.Findings)
	addPlayerFindings(&report)
	after, _ := json.Marshal(report.Findings)
	if !bytes.Equal(before, after) {
		t.Fatal("rebuilding scheduling findings duplicated or renumbered unchanged evidence")
	}
	for _, finding := range report.Findings {
		if finding.Severity != "info" || finding.ID == "" || finding.Evidence[0].Artifact != playerName {
			t.Fatalf("scheduling observation was emitted as an unreferenced causal claim: %+v", finding)
		}
	}
}

func TestPlayerSchedulingRawAndDerivedEvidenceBundleReplayExactAndMalformedImport(t *testing.T) {
	service, id, _ := openStallServiceTestRun(t, nil, false)
	dir := service.runDir(id)
	record, err := readPlayerRecording(dir)
	if err != nil {
		t.Fatal(err)
	}
	record.Version, record.Revision = 3, 2
	record.Scheduling = &PlayerSchedulingRecording{LongTaskSupport: "supported", ObservationStartMS: 100, ObservationEndMS: 9000, LongTasks: []PlayerSchedulingInterval{{3400, 3600}, {4500, 4700}}, TimerDelays: []PlayerSchedulingInterval{{4300, 4600}}}
	for index := range record.Samples {
		record.Samples[index].Video = &PlayerVideoSample{QualitySource: "playback_quality", TotalFrames: videoTestNumber(int64(100 + index*20)), DroppedFrames: videoTestNumber(int64(index))}
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
	if detail.Report.Player.Scheduling == nil || detail.Report.Player.Scheduling.LongTasks.Count != 2 || detail.Report.Player.Scheduling.TimerDelays.Count != 1 || detail.Report.Player.Video == nil {
		t.Fatal("v3 scheduling or v2 video metrics were lost")
	}
	var archive bytes.Buffer
	if err := service.WriteBundle(id, &archive); err != nil {
		t.Fatal(err)
	}
	imported, err := service.Import(bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Detail(imported.ID)
	if err != nil || !reflect.DeepEqual(detail.Report, replay.Report) {
		t.Fatalf("offline replay changed raw-derived v3 scheduling/video evidence: %v", err)
	}
	raw, err := readPlayerRecording(service.runDir(imported.ID))
	if err != nil || !reflect.DeepEqual(raw, record) {
		t.Fatalf("offline replay changed raw scheduling provenance: %v", err)
	}
	// Re-sign an invalid raw record so validation, rather than the checksum,
	// must reject the unsupported native observation.
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, entry := range reader.File {
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name] = data
	}
	record.Scheduling.LongTaskSupport = "unsupported"
	files[playerName], _ = json.Marshal(record)
	var manifest Manifest
	if err := json.Unmarshal(files[manifestName], &manifest); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(files[playerName])
	manifest.Player = &Artifact{Bytes: int64(len(files[playerName])), SHA256: hex.EncodeToString(hash[:])}
	files[manifestName], _ = json.Marshal(manifest)
	var modified bytes.Buffer
	writer := zip.NewWriter(&modified)
	for name, data := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Import(bytes.NewReader(modified.Bytes())); !errors.Is(err, ErrInvalid) {
		t.Fatalf("checksum-valid malformed v3 scheduling import accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, playerVersionName(2))); err != nil {
		t.Fatal("immutable versioned player evidence missing")
	}
}
