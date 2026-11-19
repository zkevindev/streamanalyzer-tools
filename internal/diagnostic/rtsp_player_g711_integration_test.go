package diagnostic

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A real native client receives original RTP while optional local tools fail.
// Slow qualification must never consume the one-second source observation.
func TestRTSPG711NativeToolFailuresKeepCaptureAndExactReplay(t *testing.T) {
	tool := rtspG711TestTool(t)
	var probeOutput, probeMetadata rtspG711ProbeBuffer
	prepareProbe := func(t *testing.T) {
		t.Helper()
		if probeOutput.Len() != 0 {
			return
		}
		// Runtime failures require a successful qualification first. Produce its
		// real AAC/CRC bytes before observation, so a cold FFmpeg startup cannot
		// turn these cases into the separate slow-qualification queue-limit case.
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		command := exec.CommandContext(ctx, tool, rtspG711Args(rtspG711Config{codec: "pcmu", rate: 8000, channels: 1})...)
		command.WaitDelay = 250 * time.Millisecond
		command.Stdin = bytes.NewReader(make([]byte, 320))
		command.Stdout, command.Stderr = &probeOutput, &probeMetadata
		err := command.Run()
		cancel()
		if err != nil || probeOutput.over || probeMetadata.over || probeOutput.Len() == 0 || probeMetadata.Len() == 0 {
			t.Fatalf("could not prepare real qualification output: %v", err)
		}
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	octal := func(data []byte) string {
		var encoded strings.Builder
		for _, value := range data {
			fmt.Fprintf(&encoded, "\\%03o", value)
		}
		return encoded.String()
	}
	for _, mode := range []string{"missing", "slow", "runtime", "partial"} {
		t.Run(mode, func(t *testing.T) {
			if runtime.GOOS == "windows" && mode != "missing" {
				t.Skip("POSIX encoder wrapper regression")
			}
			var lookup func() (string, error)
			var traceDir string
			switch mode {
			case "missing":
				lookup = func() (string, error) { return "", os.ErrNotExist }
			default:
				dir := t.TempDir()
				traceDir = dir
				wrapper := filepath.Join(dir, "ffmpeg")
				marker := filepath.Join(dir, "probe")
				script := "#!/bin/sh\n"
				if mode == "slow" {
					script += "sleep 2\nexit 1\n"
				} else {
					prepareProbe(t)
					// The normal production qualifier still parses ADTS and verifies
					// each frame CRC. Builtins avoid introducing additional process
					// startup into this already-qualified runtime-failure fixture.
					script += fmt.Sprintf("if [ ! -e %s ]; then : >%s; read -r probe_input || :; : >%s; printf %s; printf %s >&2; : >%s; exit 0; fi\n: >%s\n", quote(marker), quote(filepath.Join(dir, "probeBegin")), quote(marker), quote(octal(probeOutput.Bytes())), quote(octal(probeMetadata.Bytes())), quote(filepath.Join(dir, "probeEnd")), quote(filepath.Join(dir, "runtimeBegin")))
					if mode == "partial" {
						script += fmt.Sprintf("head -c 3200 | %s \"$@\"\n", quote(tool))
					} else {
						script += "exit 1\n"
					}
				}
				if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				lookup = func() (string, error) { return wrapper, nil }
			}
			packets := make([]rtspIntegrationPacket, 70)
			for index := range packets {
				packets[index] = rtspIntegrationPacket{raw: rtspIntegrationRTP(0, uint16(index), uint32(index*160), 7, rtspG711Tone("pcmu", index*160, 160), true), delay: 20 * time.Millisecond}
			}
			fixture := newRTSPIntegrationFixture(t, rtspIntegrationConfig{sdp: rtspIntegrationSDP + "m=audio 0 RTP/AVP 0\r\na=control:trackID=0\r\n", packets: packets})
			player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
			defer player.Close()
			dir := t.TempDir()
			target, _ := parseProtocolURL(fixture.address(), ProtocolRTSP)
			sampleStart := time.Now()
			report, err := sampleRTSPTransportWithPreviewLookup(withPlayerSession(context.Background(), player), dir, target, 1, ProtocolRTSP, "tcp", nil, lookup)
			if err != nil {
				t.Fatal(err)
			}
			preview := report.RTSP.Preview
			if fixture.plays.Load() != 1 || report.RTSP.RTPPackets < 35 || report.Manifest.Observation.ElapsedMS > 1250 || report.Manifest.Observation.EndReason != "duration_limit" || preview == nil || preview.G711 == nil {
				t.Fatalf("optional tool consumed or broke raw observation: plays=%d RTP=%d elapsed=%d preview=%+v", fixture.plays.Load(), report.RTSP.RTPPackets, report.Manifest.Observation.ElapsedMS, preview)
			}
			want := "ffmpeg_failed"
			if mode == "missing" {
				want = "ffmpeg_unavailable"
			}
			if mode == "slow" {
				want = "g711_queue_limit"
			}
			clock := func(value *int64) string {
				if value == nil {
					return "unset"
				}
				return fmt.Sprint(*value)
			}
			trace := map[string]int64{}
			for _, name := range []string{"probeBegin", "probeEnd", "runtimeBegin"} {
				trace[name] = -1
				if info, err := os.Stat(filepath.Join(traceDir, name)); traceDir != "" && err == nil {
					trace[name] = info.ModTime().Sub(sampleStart).Milliseconds()
				}
			}
			t.Logf("tool_check_ms=%s startup_ms=%s wrapper_offsets_ms=%v input_packets=%d output_aus=%d", clock(preview.G711.ToolCheckMS), clock(preview.G711.StartupMS), trace, preview.G711.InputPackets, preview.G711.OutputAUs)
			if preview.G711.ReasonCode != want || preview.G711.State == "ready" || preview.ReasonCode == "network_error" || preview.AudioAUs != preview.G711.OutputAUs {
				t.Fatalf("wrong local failure or output counts: %+v nested=%+v tool_check_ms=%s startup_ms=%s wrapper_offsets_ms=%v", preview, preview.G711, clock(preview.G711.ToolCheckMS), clock(preview.G711.StartupMS), trace)
			}
			if mode == "partial" && preview.G711.OutputAUs == 0 {
				t.Fatalf("fixture failed to exercise retained partial output: %+v", preview.G711)
			}
			assertRTSPG711ExactBundle(t, dir, report)
		})
	}
}

func assertRTSPG711ExactBundle(t *testing.T, dir string, report Report) {
	t.Helper()
	if projected := modelRTSP(report.RTSP); projected.Preview == nil || !reflect.DeepEqual(projected.Preview, report.RTSP.Preview) {
		t.Fatal("actual local failure rejected from bounded model projection")
	}
	var bundle bytes.Buffer
	if err := writeBundle(&bundle, dir, report); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	again, err := readBundle(destination, bytes.NewReader(bundle.Bytes()))
	if err != nil || !reflect.DeepEqual(report, again) {
		t.Fatalf("own report does not import exactly: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, rtspStreamName))
	after, _ := os.ReadFile(filepath.Join(destination, rtspStreamName))
	if !bytes.Equal(before, after) {
		t.Fatal("raw RTP archive changed")
	}
}

func TestRTSPG711UnsupportedDeclaredConfigurationRemainsReviewable(t *testing.T) {
	for _, config := range []string{"192001/9", "2147483647/2147483647"} {
		t.Run(config, func(t *testing.T) {
			fixture := newRTSPIntegrationFixture(t, rtspIntegrationConfig{sdp: rtspIntegrationSDP + "m=audio 0 RTP/AVP 97\r\na=rtpmap:97 PCMA/" + config + "\r\na=control:trackID=0\r\n", closeAfterPlay: true})
			service := rtspIntegrationOpen(t)
			run, err := service.Start(SampleRequest{URL: fixture.address(), Protocol: ProtocolRTSP, Seconds: 1, Playback: true})
			if err != nil {
				t.Fatal(err)
			}
			detail := rtspIntegrationAwait(t, service, run.ID)
			p := detail.Report.RTSP.Preview
			if p == nil || p.G711 == nil || p.G711.ReasonCode != "g711_configuration_unsupported" || p.G711.State != "unavailable" || p.AudioCodec != "pcma" {
				t.Fatalf("unsupported actual declaration was hidden or unimportable: %+v", p)
			}
			rtspIntegrationRoundTrip(t, service, detail)
		})
	}
}
