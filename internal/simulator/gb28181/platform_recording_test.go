package gb28181

import (
	"bytes"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPlatformRecordingLookupAndControl(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("recording needs FFmpeg")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("recording needs ffprobe")
	}
	path := filepath.Join(t.TempDir(), "recording.mp4")
	if output, err := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x96:rate=20",
		"-t", "2", "-c:v", "libx264", "-g", "10", "-bf", "0", path).CombinedOutput(); err != nil {
		t.Fatalf("generate recording: %v %s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, transport := range []string{"udp", "tcp"} {
		t.Run(transport, func(t *testing.T) {
			const platformID = "34020000002000000001"
			const deviceID = "34020000001320000001"
			const channel = "34020000001320000002"
			start := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
			p := &PlatformManager{}
			t.Cleanup(p.Close)
			if err := p.Start(PlatformConfig{PlatformID: platformID, DeviceID: deviceID, SIPTransport: transport}); err != nil {
				t.Fatal(err)
			}
			addr := netip.MustParseAddrPort(p.Status().Address)
			devices := &Manager{}
			t.Cleanup(devices.Close)
			if err := devices.StartFile(t.Context(), Config{PlatformHost: addr.Addr().String(), PlatformPort: int(addr.Port()),
				PlatformID: platformID, DeviceID: deviceID, ChannelID: channel, SIPTransport: transport,
				RecordingStart: start.Format(time.RFC3339), RecordingCount: 2, RecordingGap: 2}, "recording.mp4", bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
			wait := func(match func(PlatformStatus) bool) PlatformStatus {
				t.Helper()
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					status := p.Status()
					if match(status) {
						return status
					}
					time.Sleep(20 * time.Millisecond)
				}
				t.Fatalf("unexpected status: %+v, preview: %+v", p.Status(), p.Status().Preview)
				return PlatformStatus{}
			}
			wait(func(s PlatformStatus) bool { return s.Peer != "" && devices.Status().State == "registered" })
			empty := PlaybackRange{Start: start.Add(2 * time.Second), End: start.Add(4 * time.Second)}
			if err := p.Preview(PreviewConfig{ChannelID: channel, Recording: &empty, WindowMS: 4500}); err != nil {
				t.Fatal(err)
			}
			status := wait(func(s PlatformStatus) bool { return !s.Busy })
			if status.Preview.State != "no_recordings" || status.Preview.Recording.Total == nil || *status.Preview.Recording.Total != 0 {
				t.Fatalf("unexpected empty lookup: %+v", status.Preview)
			}
			full := PlaybackRange{Start: start, End: start.Add(6 * time.Second)}
			if err := p.Preview(PreviewConfig{ChannelID: channel, Recording: &full, WindowMS: 7000}); err != nil {
				t.Fatal(err)
			}
			wait(func(s PlatformStatus) bool { return !s.Busy || s.Preview.Media.Keyframes > 0 })
			if err := p.ControlPlayback(PlaybackControl{Method: "PAUSE"}); err != nil {
				t.Fatal(err)
			}
			offset := 4.0
			if err := p.ControlPlayback(PlaybackControl{Method: "PLAY", Offset: &offset}); err != nil {
				t.Fatal(err)
			}
			status = wait(func(s PlatformStatus) bool { return !s.Busy })
			if status.Preview.State != "recording_ended" || len(status.Preview.Recording.Records) != 2 ||
				status.Preview.Media.Keyframes < 2 {
				t.Fatalf("unexpected result: %+v recording %+v", status.Preview, status.Preview.Recording)
			}
		})
	}
}
