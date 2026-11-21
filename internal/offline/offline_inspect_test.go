package offline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/models"
)

func waitTaskStatus(t *testing.T, mgr *Manager, id string, timeout time.Duration) *models.OfflineTask {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		task, err := mgr.GetTask(id)
		if err == nil && task.Status != models.OfflineStatusRunning {
			return task
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("task %s timed out waiting for completion", id)
	return nil
}

func TestLocalTaskZeroCopyAndDeletePreservesOriginalFile(t *testing.T) {
	baseDir := t.TempDir()
	localDir := t.TempDir()

	fixture, err := os.ReadFile(filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.flv"))
	if err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(localDir, "my_big_video.flv")
	if err := os.WriteFile(localPath, fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(baseDir)

	// Reject directory and missing file
	if _, err := mgr.CreateLocalTask(&models.OfflineTaskRequest{Mode: models.OfflineModeAuto}, localDir); err == nil {
		t.Fatalf("expected error for directory path")
	}
	if _, err := mgr.CreateLocalTask(&models.OfflineTaskRequest{Mode: models.OfflineModeAuto}, filepath.Join(localDir, "missing.flv")); err == nil {
		t.Fatalf("expected error for non-existent path")
	}

	task, err := mgr.CreateLocalTask(&models.OfflineTaskRequest{Mode: models.OfflineModeAuto}, localPath)
	if err != nil {
		t.Fatalf("CreateLocalTask failed: %v", err)
	}
	prepared, err := mgr.PrepareTask(task.ID, false)
	if err != nil {
		t.Fatalf("PrepareTask failed: %v", err)
	}
	if prepared.SourceType != "local" || prepared.InputPath != localPath || prepared.Mode != models.OfflineModeFLV {
		t.Fatalf("unexpected local task metadata: %+v", prepared)
	}

	// Ensure original file was NOT copied into data/offline/<taskID>/input.*
	if _, err := os.Stat(filepath.Join(baseDir, task.ID, "input.flv")); !os.IsNotExist(err) {
		t.Fatalf("expected zero-copy without input.flv copy in task dir")
	}

	if err := mgr.StartTask(task.ID); err != nil {
		t.Fatalf("StartTask failed: %v", err)
	}
	done := waitTaskStatus(t, mgr, task.ID, 15*time.Second)
	if done.Status != models.OfflineStatusDone {
		t.Fatalf("expected done, got status=%s err=%s", done.Status, done.Error)
	}
	if done.ProgressPct < 99.9 {
		t.Fatalf("expected progress_pct=100, got %.1f", done.ProgressPct)
	}

	// Delete task: task directory must be deleted, original local file MUST remain untouched!
	if err := mgr.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, task.ID)); !os.IsNotExist(err) {
		t.Fatalf("expected task dir removed after DeleteTask")
	}
	st, err := os.Stat(localPath)
	if err != nil || st.Size() != int64(len(fixture)) {
		t.Fatalf("DeleteTask MUST NOT delete user's original local file! err=%v", err)
	}
}

func TestCancelTaskMarksCanceled(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	inPath := filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.flv")
	task, err := mgr.CreateLocalTask(&models.OfflineTaskRequest{Mode: models.OfflineModeFLV}, inPath)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate pre-canceled context registered on manager and invoke run directly
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mgr.mu.Lock()
	mgr.running[task.ID] = &runningEntry{ctx: ctx, cancel: cancel}
	mgr.mu.Unlock()

	mgr.run(ctx, task.ID)

	got, err := mgr.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.OfflineStatusCanceled {
		t.Fatalf("expected canceled status, got %q (err=%s)", got.Status, got.Error)
	}

	// Verify CancelTask rejects non-running tasks cleanly
	if err := mgr.CancelTask("non-existent"); err == nil {
		t.Fatalf("expected error canceling non-running task")
	}
}

func TestInspectFrameAcrossFormatsFLVAnnexBMP4TS(t *testing.T) {
	baseDir := t.TempDir()
	mgr := NewManager(baseDir)

	// 1. FLV fixture -> inspect first video frame (must decode H.264 NALU / SPS / PPS / IDR + Hex)
	flvPath := filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.flv")
	flvTask, err := mgr.CreateLocalTask(&models.OfflineTaskRequest{Mode: models.OfflineModeFLV}, flvPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.StartTask(flvTask.ID); err != nil {
		t.Fatal(err)
	}
	if st := waitTaskStatus(t, mgr, flvTask.ID, 15*time.Second); st.Status != models.OfflineStatusDone {
		t.Fatalf("flv task failed: %s", st.Error)
	}
	_, flvSum, err := mgr.loadTaskAndSummary(flvTask.ID)
	if err != nil || len(flvSum.Flows) == 0 || len(flvSum.Flows[0].FrameDetails) == 0 {
		t.Fatalf("expected flv frame details, err=%v", err)
	}
	firstVideoIdx := -1
	for i, fd := range flvSum.Flows[0].FrameDetails {
		if fd.MediaType == "video" {
			firstVideoIdx = i
			break
		}
	}
	if firstVideoIdx < 0 {
		t.Fatalf("no video frame found in FLV fixture")
	}
	flvInsp, err := mgr.InspectFrame(flvTask.ID, 0, firstVideoIdx, 0, 512)
	if err != nil {
		t.Fatalf("InspectFrame(FLV) failed: %v", err)
	}
	if len(flvInsp.Inspection.NALUs) == 0 || len(flvInsp.Inspection.Hex.Lines) == 0 {
		t.Fatalf("expected NALUs and hex lines from FLV frame, got %+v", flvInsp.Inspection)
	}

	// 2. Annex B fixture -> inspect first frame (SPS/PPS/IDR)
	annexPath := filepath.Join("..", "simulator", "testpattern", "baseline.h264")
	annexTask, err := mgr.CreateLocalTask(&models.OfflineTaskRequest{Mode: models.OfflineModeAnnexB}, annexPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.StartTask(annexTask.ID); err != nil {
		t.Fatal(err)
	}
	if st := waitTaskStatus(t, mgr, annexTask.ID, 15*time.Second); st.Status != models.OfflineStatusDone {
		t.Fatalf("annexb task failed: %s", st.Error)
	}
	annexInsp, err := mgr.InspectFrame(annexTask.ID, 0, 0, 0, 256)
	if err != nil {
		t.Fatalf("InspectFrame(AnnexB) failed: %v", err)
	}
	if len(annexInsp.Inspection.SPS) == 0 || annexInsp.Inspection.SPS[0].Width == 0 {
		t.Fatalf("expected decoded SPS from Annex B frame 0, got %+v", annexInsp.Inspection)
	}

	// 3. Real fMP4 (init.mp4 + seg-01.m4s) -> inspect frame 0 (AVCC length-prefixed) AND MP4 Box Tree
	initBytes, err := os.ReadFile(filepath.Join("..", "simulator", "hlsorigin", "avmedia", "video", "init.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	segBytes, err := os.ReadFile(filepath.Join("..", "simulator", "hlsorigin", "avmedia", "video", "seg-01.m4s"))
	if err != nil {
		t.Fatal(err)
	}
	fmp4Path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(fmp4Path, append(initBytes, segBytes...), 0o644); err != nil {
		t.Fatal(err)
	}
	mp4Task, err := mgr.CreateLocalTask(&models.OfflineTaskRequest{Mode: models.OfflineModeMP4}, fmp4Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.StartTask(mp4Task.ID); err != nil {
		t.Fatal(err)
	}
	if st := waitTaskStatus(t, mgr, mp4Task.ID, 15*time.Second); st.Status != models.OfflineStatusDone {
		t.Fatalf("mp4 task failed: %s", st.Error)
	}
	mp4Insp, err := mgr.InspectFrame(mp4Task.ID, 0, 0, 0, 512)
	if err != nil {
		t.Fatalf("InspectFrame(fMP4) failed: %v", err)
	}
	if mp4Insp.Inspection.Format != "avcc" || len(mp4Insp.Inspection.NALUs) == 0 {
		t.Fatalf("expected AVCC NALU inspection from fMP4 frame 0, got %+v", mp4Insp.Inspection)
	}
	boxes, err := mgr.InspectMP4Boxes(mp4Task.ID)
	if err != nil {
		t.Fatalf("InspectMP4Boxes failed: %v", err)
	}
	if !boxes.Fragmented || boxes.TotalNodes == 0 {
		t.Fatalf("unexpected MP4 boxes: %+v", boxes)
	}

	// 4. TS fixture -> inspect PES 0
	tsPath := filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.ts")
	tsTask, err := mgr.CreateLocalTask(&models.OfflineTaskRequest{Mode: models.OfflineModeTS}, tsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.StartTask(tsTask.ID); err != nil {
		t.Fatal(err)
	}
	if st := waitTaskStatus(t, mgr, tsTask.ID, 15*time.Second); st.Status != models.OfflineStatusDone {
		t.Fatalf("ts task failed: %s", st.Error)
	}
	tsInsp, err := mgr.InspectPES(tsTask.ID, 0, 0, 0, 256)
	if err != nil {
		t.Fatalf("InspectPES(TS) failed: %v", err)
	}
	if tsInsp.PESIndex == nil || *tsInsp.PESIndex != 0 || len(tsInsp.Inspection.Hex.Lines) == 0 {
		t.Fatalf("unexpected TS PES inspection: %+v", tsInsp)
	}
}

func findCheck(checks []models.OfflineHealthCheck, code string) *models.OfflineHealthCheck {
	for i := range checks {
		if checks[i].Code == code {
			return &checks[i]
		}
	}
	return nil
}

func TestDeepHealthCheckRulesAllFiveCategories(t *testing.T) {
	// Construct samples that deliberately trigger all 5 deep health rules:
	// 1. IDR missing in-band SPS/PPS (both with and without container sequence header)
	// 2. SPS/PPS parameter & resolution switch (1280x720 -> 1920x1080)
	// 3. PTS < DTS, non-B-frame PTS regression, B-frame CTS=0
	// 4. 200ms bitrate spike (>= 3.0x average with avg > 100 kbps)
	// 5. Audio sample rate/channel change, large audio gap (> 500ms), zero-length & oversized audio frame
	samples := []timedSample{
		// Video: huge 200ms burst at t=0..40ms (~200KB in 200ms = ~8000 kbps) over 5s (~350 kbps avg)
		{media: "video", frameIndex: 0, dts: 0, pts: 0, cts: 0, size: 120000, key: true, sliceKnown: true, hasInbandParams: true, paramHash: "h1", videoWidth: 1280, videoHeight: 720},
		{media: "video", frameIndex: 1, dts: 40, pts: 40, cts: 0, size: 80000, sliceKnown: true},
		{media: "video", frameIndex: 2, dts: 80, pts: 30, cts: -50, size: 2000, sliceKnown: true},        // PTS < DTS & non-B PTS regression
		{media: "video", frameIndex: 3, dts: 120, pts: 120, cts: 0, size: 2000, b: true, sliceKnown: true}, // B-frame with CTS=0
		// IDR without in-band SPS/PPS at frameIndex=4
		{media: "video", frameIndex: 4, dts: 1000, pts: 1000, cts: 0, size: 5000, key: true, sliceKnown: true, hasInbandParams: false},
		// Resolution / parameter switch at frameIndex=5
		{media: "video", frameIndex: 5, dts: 2000, pts: 2000, cts: 0, size: 5000, key: true, sliceKnown: true, hasInbandParams: true, paramHash: "h2", videoWidth: 1920, videoHeight: 1080},
		{media: "video", frameIndex: 6, dts: 3000, pts: 3000, cts: 0, size: 2000, sliceKnown: true},
		{media: "video", frameIndex: 7, dts: 4000, pts: 4000, cts: 0, size: 2000, sliceKnown: true},
		{media: "video", frameIndex: 8, dts: 5000, pts: 5000, cts: 0, size: 2000, sliceKnown: true},

		// Audio anomalies:
		{media: "audio", frameIndex: 9, dts: 0, pts: 0, size: 300, audioSampleRate: 44100, audioChannels: 2},
		{media: "audio", frameIndex: 10, dts: 23, pts: 23, size: 0, audioSampleRate: 44100, audioChannels: 2},        // zero-length frame
		{media: "audio", frameIndex: 11, dts: 1500, pts: 1500, size: 70000, audioSampleRate: 48000, audioChannels: 1}, // large gap + SR/ch switch + oversized
	}

	flow1 := &models.OfflineFlowResult{VideoCodec: "h264", AudioCodec: "aac"}
	attachSampleProfileWithContext(flow1, samples, models.OfflineMediaProfile{
		VideoCodec: "h264",
		AudioCodec: "aac",
	}, healthContext{
		Mode:                  models.OfflineModeAnnexB,
		IsHEVC:                false,
		HasContainerSeqHeader: false,
		CheckIDRParams:        true,
	})
	prof := flow1.Profile

	// Rule 1: idr_missing_params (1/3 IDRs missing in-band without container seq header -> warn)
	c1 := findCheck(prof.HealthChecks, "idr_missing_params")
	if c1 == nil || c1.Severity != "warn" || c1.Count != 1 || c1.FirstFrameIndex == nil || *c1.FirstFrameIndex != 4 {
		t.Fatalf("Rule 1 failed: %+v", c1)
	}

	// Rule 2: param_set_change
	c2 := findCheck(prof.HealthChecks, "param_set_change")
	if c2 == nil || c2.Severity != "warn" || c2.Count != 1 || c2.FirstFrameIndex == nil || *c2.FirstFrameIndex != 5 || !strings.Contains(c2.Detail, "1280x720 -> 1920x1080") {
		t.Fatalf("Rule 2 failed: %+v", c2)
	}

	// Rule 3: pts_cts_anomaly
	c3 := findCheck(prof.HealthChecks, "pts_cts_anomaly")
	if c3 == nil || c3.Severity != "error" || c3.FirstFrameIndex == nil || *c3.FirstFrameIndex != 2 {
		t.Fatalf("Rule 3 failed: %+v", c3)
	}

	// Rule 4: bitrate_spike_200ms
	if prof.Peak200msKbps <= 0 || prof.PeakToAvgRatio < 3.0 {
		t.Fatalf("expected 200ms bitrate spike >= 3.0x, got peak=%.1f ratio=%.2f", prof.Peak200msKbps, prof.PeakToAvgRatio)
	}
	c4 := findCheck(prof.HealthChecks, "bitrate_spike_200ms")
	if c4 == nil || c4.Severity != "warn" {
		t.Fatalf("Rule 4 failed: %+v", c4)
	}

	// Rule 5: audio_health (SR/channel switch + audio gap + zero/oversized frame -> error)
	c5 := findCheck(prof.HealthChecks, "audio_health")
	if c5 == nil || c5.Severity != "error" || c5.FirstFrameIndex == nil {
		t.Fatalf("Rule 5 failed: %+v", c5)
	}
	if !strings.Contains(c5.Detail, "44100Hz/2ch -> 48000Hz/1ch") {
		t.Fatalf("expected audio SR/channel switch in detail, got %q", c5.Detail)
	}

	// Verify backward-compatible Warnings slice is also populated
	if len(prof.Warnings) < 4 {
		t.Fatalf("expected backward-compatible Warnings populated, got %+v", prof.Warnings)
	}

	// Verify container-provided sequence header distinguishes out-of-band vs in-band params
	flow2 := &models.OfflineFlowResult{VideoCodec: "h264"}
	attachSampleProfileWithContext(flow2, samples, models.OfflineMediaProfile{
		VideoCodec: "h264",
	}, healthContext{
		Mode:                  models.OfflineModeFLV,
		IsHEVC:                false,
		HasContainerSeqHeader: true,
		CheckIDRParams:        true,
	})
	c1Container := findCheck(flow2.Profile.HealthChecks, "idr_missing_params")
	if c1Container == nil || c1Container.Severity != "ok" || !strings.Contains(c1Container.Detail, "容器") {
		t.Fatalf("expected container-provided seq hdr to note out-of-band params, got %+v", c1Container)
	}
}
