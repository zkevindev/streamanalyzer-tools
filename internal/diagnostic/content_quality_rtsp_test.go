package diagnostic

import (
	"path/filepath"
	"testing"
)

func TestRTSPContentQualityInputReusesVerifiedRTPReconstruction(t *testing.T) {
	ffmpeg := rtspG711TestTool(t)
	ffprobe, err := investigationMediaTool("ffprobe", filepath.Dir(ffmpeg))
	if err != nil {
		t.Skip("ffprobe missing")
	}
	for _, name := range []string{"h264", "pcma"} {
		t.Run(name, func(t *testing.T) {
			dir, report, _ := investigationRTSPCaptureFixture(t, name)
			input, err := contentQualityInput(t.Context(), dir, t.TempDir(), report)
			if err != nil {
				t.Fatal(err)
			}
			probe, err := investigationMediaInspect(t.Context(), ffprobe, input)
			if err != nil {
				t.Fatal(err)
			}
			if name == "h264" && !probe.hasVideo || name == "pcma" && !probe.hasAudio || probe.durationMS <= 0 {
				t.Fatalf("RTSP reconstruction did not produce analyzable media: %+v", probe)
			}
		})
	}
}

func TestRTSPContentQualityDoesNotRequirePlayerRecording(t *testing.T) {
	ffmpeg := rtspG711TestTool(t)
	ffprobe, err := investigationMediaTool("ffprobe", filepath.Dir(ffmpeg))
	if err != nil {
		t.Skip("ffprobe missing")
	}
	dir, report, _ := investigationRTSPCaptureFixture(t, "h264")
	report.Player = nil
	report.Manifest.Player = nil
	report.RTSP.Preview = nil
	input, err := contentQualityInput(t.Context(), dir, t.TempDir(), report)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := investigationMediaInspect(t.Context(), ffprobe, input)
	if err != nil || !probe.hasVideo || probe.durationMS <= 0 {
		t.Fatalf("playerless RTSP content was not reconstructed: %+v %v", probe, err)
	}
}
