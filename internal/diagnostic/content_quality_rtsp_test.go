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

func TestRTSPContentQualityRequiresSameCaptureClock(t *testing.T) {
	report := Report{Manifest: Manifest{Protocol: ProtocolRTSP}}
	if _, err := contentQualityRTSPAnchor(report); err == nil {
		t.Fatal("accepted RTSP source analysis without a same-capture media clock")
	}
}
