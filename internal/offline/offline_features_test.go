package offline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"streamanalyzer/internal/models"
)

func TestDetectModeMagic(t *testing.T) {
	dir := t.TempDir()

	// Real fixtures
	if mode, _ := DetectMode(filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.flv"), "unknown.bin"); mode != models.OfflineModeFLV {
		t.Fatalf("expected flv, got %q", mode)
	}
	if mode, _ := DetectMode(filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.ts"), "unknown.bin"); mode != models.OfflineModeTS {
		t.Fatalf("expected ts, got %q", mode)
	}
	if mode, _ := DetectMode(filepath.Join("..", "simulator", "hlsorigin", "avmedia", "video", "init.mp4"), "unknown.bin"); mode != models.OfflineModeMP4 {
		t.Fatalf("expected mp4, got %q", mode)
	}
	if mode, _ := DetectMode(filepath.Join("..", "simulator", "testpattern", "baseline.h264"), "unknown.bin"); mode != models.OfflineModeAnnexB {
		t.Fatalf("expected annexb, got %q", mode)
	}

	// Synthetic PCAP & PCAPNG magic
	pcapFile := filepath.Join(dir, "capture.bin")
	if err := os.WriteFile(pcapFile, []byte{0xd4, 0xc3, 0xb2, 0xa1, 0x02, 0x00, 0x04, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if mode, _ := DetectMode(pcapFile, "capture.bin"); mode != models.OfflineModePCAP {
		t.Fatalf("expected pcap, got %q", mode)
	}

	pcapngFile := filepath.Join(dir, "ng.bin")
	if err := os.WriteFile(pcapngFile, []byte{0x0a, 0x0d, 0x0d, 0x0a, 0x00, 0x00, 0x00, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if mode, _ := DetectMode(pcapngFile, "ng.bin"); mode != models.OfflineModePCAP {
		t.Fatalf("expected pcapng->pcap, got %q", mode)
	}
}

func TestRunAnnexBFixture(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	summary := &models.OfflineSummary{}
	inPath := filepath.Join("..", "simulator", "testpattern", "baseline.h264")
	if err := mgr.runAnnexB(&models.OfflineTask{InputPath: inPath, InputName: "baseline.h264"}, filepath.Join(dir, "annexb"), summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(summary.Flows))
	}
	f := summary.Flows[0]
	if f.VideoCodec != "h264" || f.NALUCount == 0 || f.Profile == nil {
		t.Fatalf("unexpected flow: %+v", f)
	}
	p := f.Profile
	if p.Width == 0 || p.Height == 0 || p.VideoFrames == 0 || p.Keyframes == 0 {
		t.Fatalf("unexpected annexb profile: %+v", p)
	}
	if len(p.NALUHistogram) == 0 || p.NALUHistogram["SPS"] == 0 || p.NALUHistogram["IDR"] == 0 {
		t.Fatalf("expected NALU histogram with SPS/IDR, got %+v", p.NALUHistogram)
	}
	if len(p.FrameSizeSeries) == 0 {
		t.Fatalf("expected non-empty frame size series")
	}
}

func TestTSScanPacketHealthAndCCErrors(t *testing.T) {
	// 1. Clean fixture
	clean := scanTSPackets(filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.ts"))
	if clean.packets == 0 || clean.syncErrors != 0 || clean.teiErrors != 0 || clean.ccErrors != 0 {
		t.Fatalf("unexpected clean ts scan: %+v", clean)
	}

	// 2. Synthetic TS with CC jump and TEI packet
	dir := t.TempDir()
	badTS := filepath.Join(dir, "bad.ts")
	var buf []byte
	mkPkt := func(pid uint16, cc uint8, tei bool) []byte {
		p := make([]byte, 188)
		p[0] = 0x47
		p[1] = byte((pid >> 8) & 0x1f)
		if tei {
			p[1] |= 0x80
		}
		p[2] = byte(pid & 0xff)
		p[3] = 0x10 | (cc & 0x0f) // payload only
		return p
	}
	buf = append(buf, mkPkt(0x0100, 0, false)...)
	buf = append(buf, mkPkt(0x0100, 1, false)...)
	buf = append(buf, mkPkt(0x0100, 4, false)...) // CC jump 1 -> 4
	buf = append(buf, mkPkt(0x0100, 5, true)...)  // TEI=1
	if err := os.WriteFile(badTS, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	bad := scanTSPackets(badTS)
	if bad.packets != 4 || bad.ccErrors != 1 || bad.teiErrors != 1 {
		t.Fatalf("expected 4 pkts, 1 cc error, 1 tei error; got %+v", bad)
	}
	if bad.pids[0x0100] == nil || bad.pids[0x0100].ccErrors != 1 {
		t.Fatalf("expected pid 0x100 cc error, got %+v", bad.pids[0x0100])
	}
}

func TestAnalyzeSamplesAdvancedMetrics(t *testing.T) {
	samples := []timedSample{
		{media: "video", dts: 0, pts: 0, cts: 0, size: 5000, key: true},
		{media: "video", dts: 40, pts: 120, cts: 80, size: 1200},
		{media: "video", dts: 80, pts: 40, cts: -40, size: 800, b: true},
		{media: "video", dts: 1000, pts: 1000, cts: 0, size: 4800, key: true},
		{media: "audio", dts: 700, pts: 700, size: 300},
		{media: "audio", dts: 1700, pts: 1700, size: 300},
	}
	stats := analyzeSamples(samples)
	if !stats.HasBFrames || stats.BFrames != 1 {
		t.Fatalf("expected B-frame detection, got %+v", stats)
	}
	if stats.VideoDeltaMinMS != 40 || stats.VideoDeltaMaxMS != 920 {
		t.Fatalf("unexpected delta bounds: min=%d max=%d", stats.VideoDeltaMinMS, stats.VideoDeltaMaxMS)
	}
	if !stats.HasAVOffset || stats.AVStartOffsetMS != 700 {
		t.Fatalf("expected av offset 700ms, got has=%v offset=%d", stats.HasAVOffset, stats.AVStartOffsetMS)
	}
	if len(stats.BitrateTimeline) == 0 || len(stats.FrameSizeSeries) != 4 || len(stats.VideoDeltaSeries) != 3 {
		t.Fatalf("expected populated chart series: bitrate=%d size=%d delta=%d",
			len(stats.BitrateTimeline), len(stats.FrameSizeSeries), len(stats.VideoDeltaSeries))
	}
}

func TestManagerLifecycleAutoReparseDeleteAndRecovery(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	// Create auto task with FLV fixture
	task, inputPath, err := mgr.CreateTask(&models.OfflineTaskRequest{Mode: models.OfflineModeAuto}, "clip.flv")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.flv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	prepared, err := mgr.PrepareTask(task.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Mode != models.OfflineModeFLV || prepared.InputSize != int64(len(raw)) || prepared.DetectedBy != "magic:flv" {
		t.Fatalf("unexpected prepared task: %+v", prepared)
	}
	if err := mgr.StartTask(prepared.ID); err != nil {
		t.Fatal(err)
	}

	waitDone := func(id string) *models.OfflineTask {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			cur, err := mgr.GetTask(id)
			if err != nil {
				t.Fatal(err)
			}
			if cur.Status == models.OfflineStatusDone || cur.Status == models.OfflineStatusFailed {
				return cur
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("task %s timed out", id)
		return nil
	}

	done := waitDone(prepared.ID)
	if done.Status != models.OfflineStatusDone {
		t.Fatalf("expected done, got %+v", done)
	}

	// Reparse
	if _, err := mgr.ReparseTask(prepared.ID); err != nil {
		t.Fatal(err)
	}
	reparsed := waitDone(prepared.ID)
	if reparsed.Status != models.OfflineStatusDone {
		t.Fatalf("reparse failed: %+v", reparsed)
	}

	// Simulate interrupted running task on disk and verify LoadExisting recovers it
	staleID := "offline_stale_interrupted"
	staleDir := filepath.Join(dir, "offline", staleID)
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staleBytes, _ := json.Marshal(models.OfflineTask{
		ID:     staleID,
		Mode:   models.OfflineModeFLV,
		Status: models.OfflineStatusRunning,
	})
	if err := os.WriteFile(filepath.Join(staleDir, "task.json"), staleBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	mgr2 := NewManager(dir)
	mgr2.LoadExisting()
	stale, err := mgr2.GetTask(staleID)
	if err != nil || stale.Status != models.OfflineStatusFailed || stale.Error == "" {
		t.Fatalf("expected interrupted task marked failed, got %+v err=%v", stale, err)
	}

	// Delete task
	if err := mgr.DeleteTask(prepared.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.GetTask(prepared.ID); err == nil {
		t.Fatal("expected deleted task not found")
	}
	if _, err := os.Stat(filepath.Join(dir, "offline", prepared.ID)); !os.IsNotExist(err) {
		t.Fatalf("expected task dir removed, stat err=%v", err)
	}
}
