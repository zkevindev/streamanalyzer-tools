package diagnostic

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestParseContentQualityLog(t *testing.T) {
	log := `[blackdetect @ 0x1] black_start:4 black_end:6 black_duration:2
[freezedetect @ 0x1] freeze_start: 0.5
[freezedetect @ 0x1] freeze_end: 4.5 | freeze_duration: 4
[silencedetect @ 0x1] silence_start: 4
[silencedetect @ 0x1] silence_end: 6 | silence_duration: 2`
	black, freeze, silence := parseContentQualityLog(log, nil, nil, nil)
	if len(black) != 1 || black[0].StartMS != 4000 || black[0].DurationMS != 2000 ||
		len(freeze) != 1 || freeze[0].StartMS != 500 || freeze[0].EndMS != 4500 ||
		len(silence) != 1 || silence[0].StartMS != 4000 || silence[0].EndMS != 6000 {
		t.Fatalf("incorrect filter intervals: %+v %+v %+v", black, freeze, silence)
	}
}

func TestContentQualityDetectsBlackFreezeSilenceAndAVTiming(t *testing.T) {
	ffmpeg, err := investigationMediaTool("ffmpeg", "")
	if err != nil {
		t.Skip("FFmpeg is unavailable")
	}
	if _, err := investigationMediaTool("ffprobe", filepath.Dir(ffmpeg)); err != nil {
		t.Skip("FFprobe is unavailable")
	}
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	run := newRun("sample", ProtocolHTTPFLV, "https://example.test")
	run.Status, run.ReportReady = "done", true
	if err := os.Mkdir(service.runDir(run.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := service.insertRun(run); err != nil {
		t.Fatal(err)
	}
	input := "testsrc2=size=160x90:rate=10:duration=10"
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", input,
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=10",
		"-vf", "drawbox=x=0:y=0:w=iw:h=ih:color=red:t=fill:enable='lt(t,3)',drawbox=x=0:y=0:w=iw:h=ih:color=black:t=fill:enable='between(t,6,8)'", "-af", "volume='if(between(t,6,8),0,1)':eval=frame",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "10", "-c:a", "aac", "-shortest", "-y", filepath.Join(service.runDir(run.ID), streamName)}
	if output, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("generate content fixture: %v: %s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(service.runDir(run.ID), streamName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(service.runDir(run.ID), streamName)); err != nil {
		t.Fatal(err)
	}
	stream := shareTestArtifact(t, service.runDir(run.ID), streamName, data)
	report := Report{RuleVersion: "http-flv/2", Manifest: Manifest{Version: multiProtocolSchemaVersion, Protocol: ProtocolHTTPFLV,
		Observation: Observation{Origin: run.Origin, StartedAt: run.CreatedAt, RequestedSeconds: 10, ElapsedMS: 10_000, EndReason: "eof"}, Stream: stream}, Findings: []Finding{}}
	if err := persistReport(service.runDir(run.ID), report); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	quality, err := service.AnalyzeContentQuality(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if quality.State != "complete" || !quality.HasVideo || !quality.HasAudio || len(quality.BlackIntervals) == 0 || len(quality.FreezeIntervals) == 0 || len(quality.SilenceIntervals) == 0 {
		t.Fatalf("content quality filters did not detect fixture: %+v", quality)
	}
	if quality.AVSync.State != "complete" || quality.AVSync.SamplePairs == 0 || quality.AVSync.DriftMS == nil {
		t.Fatalf("AV timing was not measured: %+v", quality.AVSync)
	}
	cached, err := service.AnalyzeContentQuality(ctx, run.ID)
	if err != nil || !cached.CreatedAt.Equal(quality.CreatedAt) {
		t.Fatalf("saved analysis was not reused: %+v %v", cached, err)
	}
	detail, err := service.Detail(run.ID)
	if err != nil || detail.Report.ContentQuality == nil || detail.Report.ContentQuality.SourceArtifact != stream {
		t.Fatalf("content quality was not persisted with its source hash: %+v %v", detail.Report.ContentQuality, err)
	}
}

func TestStoredContentQualityRejectsWrongSource(t *testing.T) {
	source := Artifact{Bytes: 1, SHA256: "abc"}
	quality := newContentQualityReport(source)
	quality.CreatedAt = time.Now()
	report := Report{Manifest: Manifest{Stream: source}}
	stored := Report{ContentQuality: &quality}
	if err := decodeStoredContentQuality(stored, &report); err != nil || report.ContentQuality == nil {
		t.Fatal(err)
	}
	other := Report{Manifest: Manifest{Stream: Artifact{Bytes: 2, SHA256: "def"}}}
	if err := decodeStoredContentQuality(stored, &other); err == nil {
		t.Fatal("accepted content quality from another source")
	}
}
