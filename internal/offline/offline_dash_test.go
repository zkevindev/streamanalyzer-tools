package offline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"streamanalyzer/internal/models"
)

func TestOfflineDASHDetectAndParse(t *testing.T) {
	dir := t.TempDir()
	mpdPath := filepath.Join(dir, "sample.mpd")
	content := `<?xml version="1.0" encoding="UTF-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT60S" minBufferTime="PT2S">
  <Period id="0">
    <AdaptationSet contentType="video" mimeType="video/mp4" frameRate="30000/1001">
      <SegmentTemplate timescale="1000" initialization="init-$RepresentationID$.m4s" media="seg-$RepresentationID$-$Time$.m4s" startNumber="1">
        <SegmentTimeline>
          <S t="0" d="2000" r="1"/>
          <S t="6000" d="2000"/>
        </SegmentTimeline>
      </SegmentTemplate>
      <Representation id="v1" bandwidth="2500000" width="1920" height="1080" codecs="avc1.640028"/>
    </AdaptationSet>
    <AdaptationSet contentType="audio" mimeType="audio/mp4">
      <Representation id="a1" bandwidth="128000" codecs="mp4a.40.2" audioSamplingRate="48000"/>
    </AdaptationSet>
  </Period>
</MPD>`
	if err := os.WriteFile(mpdPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	mode, reason := DetectMode(mpdPath, "sample.mpd")
	if mode != models.OfflineModeDASH {
		t.Fatalf("expected OfflineModeDASH, got %q (%s)", mode, reason)
	}

	mgr := NewManager(filepath.Join(dir, "data"))
	task := &models.OfflineTask{
		ID:        "offline_dash_test",
		Mode:      models.OfflineModeDASH,
		InputName: "sample.mpd",
		InputPath: mpdPath,
	}
	taskDir := filepath.Join(dir, "data", "offline", task.ID)
	summary := &models.OfflineSummary{TaskID: task.ID, Mode: models.OfflineModeDASH}
	if err := mgr.runDASH(task, taskDir, summary); err != nil {
		t.Fatalf("runDASH failed: %v", err)
	}

	if len(summary.Flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(summary.Flows))
	}
	flow := summary.Flows[0]
	if flow.VideoCodec != "h264" || flow.AudioCodec != "aac" {
		t.Fatalf("unexpected codecs: v=%q a=%q", flow.VideoCodec, flow.AudioCodec)
	}
	if flow.Profile == nil || flow.Profile.Width != 1920 || flow.Profile.Height != 1080 || flow.Profile.DurationMS != 60000 {
		t.Fatalf("unexpected profile: %+v", flow.Profile)
	}
	if len(flow.ArtifactPaths) != 1 {
		t.Fatalf("expected mpd_report.txt artifact, got %+v", flow.ArtifactPaths)
	}
	reportBytes, err := os.ReadFile(flow.ArtifactPaths[0])
	if err != nil {
		t.Fatalf("failed to read mpd_report.txt: %v", err)
	}
	report := string(reportBytes)
	if !strings.Contains(report, "MPEG-DASH MPD Manifest Report") || !strings.Contains(report, "[GAP +2000ms]") {
		t.Fatalf("unexpected mpd_report.txt content:\n%s", report)
	}
}
