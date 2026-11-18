package diagnostic

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestPlayerStallsExcludeStartupUserActionsAndCaptureEOF(t *testing.T) {
	start := time.Now().UTC()
	record := PlayerRecording{Version: 1, Revision: 1, StartedAt: start, ElapsedMS: 5000, Complete: true, Source: "same_capture", Engine: "mpegtsjs",
		Events: []PlayerEvent{
			{AtMS: 0, Kind: "start"}, {AtMS: 10, Kind: "waiting"},
			{AtMS: 80, Kind: "first_frame", Method: "video_frame_callback"}, {AtMS: 100, Kind: "playing"},
			{AtMS: 200, Kind: "waiting"}, {AtMS: 400, Kind: "playing"},
			{AtMS: 500, Kind: "waiting"}, {AtMS: 1200, Kind: "playing"},
			{AtMS: 1500, Kind: "pause"}, {AtMS: 1600, Kind: "waiting"}, {AtMS: 2000, Kind: "playing"},
			{AtMS: 2100, Kind: "seeking"}, {AtMS: 2200, Kind: "waiting"}, {AtMS: 2400, Kind: "seeked"}, {AtMS: 2500, Kind: "playing"},
			{AtMS: 2700, Kind: "hidden"}, {AtMS: 2800, Kind: "waiting"}, {AtMS: 3000, Kind: "visible"}, {AtMS: 3200, Kind: "playing"},
			{AtMS: 3500, Kind: "waiting"}, {AtMS: 4500, Kind: "stop"},
			{AtMS: 4600, Kind: "playing"}, {AtMS: 4700, Kind: "waiting"},
		}, Samples: []PlayerSample{
			{AtMS: 50, BufferMS: 0}, {AtMS: 100, BufferMS: 2000}, {AtMS: 600, BufferMS: 200},
			{AtMS: 1600, BufferMS: 0, Paused: true}, {AtMS: 2300, BufferMS: 0, Seeking: true},
			{AtMS: 2800, BufferMS: 0, Hidden: true}, {AtMS: 4700, BufferMS: 0},
		}}
	if err := validatePlayerRecording(record, start); err != nil {
		t.Fatal(err)
	}
	got := analyzePlayer(record, Observation{StartedAt: start.Add(100 * time.Millisecond)})
	wantStalls := []PlayerStall{{StartMS: 400, EndMS: 1100, DurationMS: 700}, {StartMS: 3400, EndMS: 4400, DurationMS: 1000}}
	if got.State != "complete" || got.StartOffsetMS != -100 || got.StallCount != 2 || got.TotalStallMS != 1700 ||
		got.MaxStallMS != 1000 || got.ActiveMS != 3000 || math.Abs(got.StallRatio-1700.0/3000) > 1e-9 || !reflect.DeepEqual(got.Stalls, wantStalls) {
		t.Fatalf("incorrect player intervals: %+v", got)
	}
	if got.FirstScreenMS == nil || *got.FirstScreenMS != 80 || got.FirstScreenMethod != "video_frame_callback" ||
		got.FirstPlayingMS == nil || *got.FirstPlayingMS != 100 || got.MinBufferMS == nil || *got.MinBufferMS != 200 || got.MaxBufferMS != 2000 {
		t.Fatalf("incorrect rendering or active buffer metrics: %+v", got)
	}
	if len(got.BufferSamples) != len(record.Samples) || got.BufferSamples[0].OffsetMS != -50 {
		t.Fatal("buffer curve lost raw samples or clock alignment")
	}
}

func TestPlayerLateFirstFrameAndRecoveredAutoplay(t *testing.T) {
	record := PlayerRecording{Revision: 1, Complete: true, ElapsedMS: 2000, Events: []PlayerEvent{
		{AtMS: 0, Kind: "autoplay_blocked"}, {AtMS: 100, Kind: "stop"},
		{AtMS: 150, Kind: "first_frame", Method: "loadeddata_estimate"}, {AtMS: 200, Kind: "playing"}, {AtMS: 250, Kind: "waiting"},
	}}
	got := analyzePlayer(record, Observation{})
	if got.State != "complete" || got.FirstScreenMS == nil || *got.FirstScreenMS != 150 || got.FirstScreenMethod != "loadeddata_estimate" ||
		got.FirstPlayingMS == nil || got.ActiveMS != 0 || got.StallCount != 0 {
		t.Fatalf("capture EOF skewed first-render or stall data: %+v", got)
	}
}

func TestPlayerRecordingValidation(t *testing.T) {
	start := time.Now().UTC()
	base := PlayerRecording{Version: 1, Revision: 1, StartedAt: start, Source: "same_capture", ElapsedMS: 1000}
	for _, change := range []func(*PlayerRecording){
		func(r *PlayerRecording) { r.StartedAt = start.Add(-61 * time.Second) },
		func(r *PlayerRecording) { r.ElapsedMS = maxPlayerElapsedMS + 1 },
		func(r *PlayerRecording) { r.Source = "https://secret.example" },
		func(r *PlayerRecording) { r.Engine = "unknown" },
		func(r *PlayerRecording) { r.Events = []PlayerEvent{{AtMS: -1, Kind: "start"}} },
		func(r *PlayerRecording) {
			r.Events = []PlayerEvent{{AtMS: 100, Kind: "playing"}, {AtMS: 50, Kind: "waiting"}}
		},
		func(r *PlayerRecording) { r.Events = []PlayerEvent{{AtMS: 1001, Kind: "playing"}} },
		func(r *PlayerRecording) { r.Events = []PlayerEvent{{Kind: "first_frame"}} },
		func(r *PlayerRecording) { r.Events = []PlayerEvent{{Kind: "error", Code: "raw URL error"}} },
		func(r *PlayerRecording) { r.Samples = []PlayerSample{{AtMS: 1, BufferMS: -1}} },
		func(r *PlayerRecording) { r.Samples = []PlayerSample{{ReadyState: 5}} },
		func(r *PlayerRecording) { r.Samples = make([]PlayerSample, maxPlayerSamples+1) },
	} {
		r := base
		change(&r)
		if !errors.Is(validatePlayerRecording(r, start), ErrInvalid) {
			t.Fatalf("accepted invalid player recording: %+v", r)
		}
	}
}

func registerTestPlayer(t *testing.T, service *Service, id string) PlayerRecording {
	t.Helper()
	run, err := service.getRun(id)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse("http://example.test/live.flv")
	service.players[id] = &playerSessionState{session: newPlayerSession(context.Background(), ProtocolHTTPFLV, target, service.client), token: "test-player-grant", created: run.CreatedAt}
	if err := persistReport(service.runDir(id), Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV, Observation: Observation{StartedAt: run.CreatedAt}}}); err != nil {
		t.Fatal(err)
	}
	return PlayerRecording{Version: 1, Revision: 1, StartedAt: run.CreatedAt, Source: "same_capture", Engine: "mpegtsjs", ElapsedMS: 100, Events: []PlayerEvent{{Kind: "start"}}}
}

func TestPlayerUpdatesAppendOnlyFinalIdempotentAndInvalidateExplanation(t *testing.T) {
	service, id := openRunForExplanation(t, nil)
	record := registerTestPlayer(t, service, id)
	if _, err := service.SavePlayerRecording(id, "wrong", record); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := service.SavePlayerRecording(id, "test-player-grant", record); err != nil {
		t.Fatal(err)
	}
	before, err := service.Detail(id)
	if err != nil || before.Report.Player.Revision != 1 {
		t.Fatalf("report missing player: %+v %v", before, err)
	}
	if err := writeJSONFile(service.runDir(id), explanationName, Explanation{Summary: "stale"}); err != nil {
		t.Fatal(err)
	}
	changed := record
	changed.Revision = 2
	changed.Events = []PlayerEvent{{Kind: "waiting"}}
	if _, err := service.SavePlayerRecording(id, "test-player-grant", changed); !errors.Is(err, ErrReportChanged) {
		t.Fatalf("rewrote prior event: %v", err)
	}
	record.Revision, record.ElapsedMS, record.Complete = 3, 1000, true
	record.Events = append(record.Events, PlayerEvent{AtMS: 100, Kind: "playing"}, PlayerEvent{AtMS: 200, Kind: "waiting"}, PlayerEvent{AtMS: 900, Kind: "stop"})
	got, err := service.SavePlayerRecording(id, "test-player-grant", record)
	if err != nil || got.StallCount != 1 || got.TotalStallMS != 700 {
		t.Fatalf("final update: %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(service.runDir(id), explanationName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale model explanation retained: %v", err)
	}
	if _, err := service.SavePlayerRecording(id, "test-player-grant", record); err != nil {
		t.Fatalf("final retry: %v", err)
	}
	record.Revision++
	if _, err := service.SavePlayerRecording(id, "test-player-grant", record); !errors.Is(err, ErrReportChanged) {
		t.Fatalf("final report changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(service.runDir(id), playerVersionName(1))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old player snapshot retained")
	}
}

func TestPlayerUpdateDiscardsInflightModelExplanation(t *testing.T) {
	started, resume := make(chan struct{}), make(chan struct{})
	model := testExplainer{identity: ModelIdentity{Provider: ProviderOpenAICompatible, Model: "test"}, explain: func(ctx context.Context) (string, error) {
		close(started)
		select {
		case <-resume:
			return "old revision", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	service, id := openRunForExplanation(t, model)
	record := registerTestPlayer(t, service, id)
	if _, err := service.SavePlayerRecording(id, "test-player-grant", record); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := service.Explain(ctx, id); result <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	record.Revision, record.ElapsedMS = 2, 1000
	if _, err := service.SavePlayerRecording(id, "test-player-grant", record); err != nil {
		close(resume)
		t.Fatal(err)
	}
	close(resume)
	select {
	case err := <-result:
		if !errors.Is(err, ErrReportChanged) {
			t.Fatalf("stale model result accepted: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	got, err := service.Detail(id)
	if err != nil || got.Explanation != nil {
		t.Fatalf("stale explanation saved: %+v %v", got.Explanation, err)
	}
}

func TestModelPlayerSummaryExcludesRawSamplesAndEvents(t *testing.T) {
	report := Report{Player: &PlayerReport{State: "complete", Source: "same_capture", FirstScreenMS: new(int64), StallCount: 2, TotalStallMS: 1000,
		Events: []PlayerEvent{{Kind: "start"}}, BufferSamples: []PlayerBufferSample{{OffsetMS: 12345}}, Stalls: []PlayerStall{{StartMS: 10, EndMS: 1010, DurationMS: 1000}}}}
	data, err := modelInput(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"stall_count":2`)) || !bytes.Contains(data, []byte(`"total_stall_ms":1000`)) ||
		bytes.Contains(data, []byte(`buffer_samples`)) || bytes.Contains(data, []byte(`"kind":"start"`)) {
		t.Fatalf("unexpected model player projection: %s", data)
	}
}

type blockingPlayerDownload struct {
	bytes.Buffer
	started, resume chan struct{}
	once            sync.Once
}

func (w *blockingPlayerDownload) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.resume
	return w.Buffer.Write(data)
}

func TestPlayerSaveContinuesDuringSlowEvidenceDownload(t *testing.T) {
	service, id := openRunForExplanation(t, nil)
	record := registerTestPlayer(t, service, id)
	if _, err := service.SavePlayerRecording(id, "test-player-grant", record); err != nil {
		t.Fatal(err)
	}
	dir := service.runDir(id)
	for name, data := range map[string][]byte{streamName: make([]byte, 128<<10), timelineName: {}} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wantPlayer, err := os.ReadFile(filepath.Join(dir, playerName))
	if err != nil {
		t.Fatal(err)
	}
	w := &blockingPlayerDownload{started: make(chan struct{}), resume: make(chan struct{})}
	download := make(chan error, 1)
	go func() { download <- service.WriteBundle(id, w) }()
	select {
	case <-w.started:
	case <-time.After(2 * time.Second):
		close(w.resume)
		t.Fatal("download did not start")
	}
	record.Revision, record.ElapsedMS = 2, 200
	saved := make(chan error, 1)
	go func() { _, err := service.SavePlayerRecording(id, "test-player-grant", record); saved <- err }()
	select {
	case err := <-saved:
		if err != nil {
			close(w.resume)
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(w.resume)
		<-download
		t.Fatal("slow download blocked player recording")
	}
	close(w.resume)
	if err := <-download; err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(w.Bytes()), int64(w.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range archive.File {
		if entry.Name == playerName {
			r, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(r)
			r.Close()
			if err != nil || !bytes.Equal(data, wantPlayer) {
				t.Fatalf("download mixed player revisions: %v", err)
			}
			return
		}
	}
	t.Fatal("download omitted player evidence")
}

func TestImportedPlayerProvenanceMustMatchProtocol(t *testing.T) {
	start := time.Now().UTC()
	for _, protocol := range []string{ProtocolHTTPFLV, ProtocolHLS} {
		dir := t.TempDir()
		source := "independent_pull"
		if protocol == ProtocolHLS {
			source = "same_capture"
		}
		record := PlayerRecording{Version: 1, Revision: 1, StartedAt: start, Source: source}
		if err := writePlayerRecording(dir, record); err != nil {
			t.Fatal(err)
		}
		report := Report{Manifest: Manifest{Protocol: protocol, Observation: Observation{StartedAt: start}}}
		if err := attachPlayerArtifact(dir, &report); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted incorrect %s provenance: %v", protocol, err)
		}
	}
}
