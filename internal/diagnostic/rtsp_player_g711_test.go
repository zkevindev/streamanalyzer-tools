package diagnostic

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/g711"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"github.com/pion/rtp"
)

func rtspG711TestTool(t *testing.T) string {
	t.Helper()
	tool, err := investigationMediaTool("ffmpeg", "")
	if err != nil {
		t.Skip("FFmpeg is not installed")
	}
	return tool
}
func rtspG711Tone(codec string, start, samples int) []byte {
	pcm := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		value := int16(12000 * math.Sin(2*math.Pi*440*float64(start+i)/8000))
		binary.BigEndian.PutUint16(pcm[i*2:], uint16(value))
	}
	var output []byte
	if codec == "pcma" {
		output, _ = g711.Alaw(pcm).Marshal()
	} else {
		output, _ = g711.Mulaw(pcm).Marshal()
	}
	return output
}

func TestG711RealStreamingTranscodeClockAndDecodedSignal(t *testing.T) {
	tool := rtspG711TestTool(t)
	for _, codec := range []string{"pcma", "pcmu"} {
		t.Run(codec, func(t *testing.T) {
			var mu sync.Mutex
			var timestamps []int64
			var packets mpeg4audio.ADTSPackets
			encoder := newRTSPG711Encoder(rtspG711Config{codec: codec, rate: 8000, channels: 1, tool: tool}, nil, func(at int64, asc, au []byte) bool {
				var config mpeg4audio.AudioSpecificConfig
				if err := config.Unmarshal(asc); err != nil {
					t.Error(err)
				}
				if config.Type != mpeg4audio.ObjectTypeAACLC || config.SampleRate != 48000 || config.ChannelConfig != 1 {
					t.Errorf("invalid AAC output config: %+v", config)
				}
				mu.Lock()
				timestamps = append(timestamps, at)
				packets = append(packets, &mpeg4audio.ADTSPacket{Type: mpeg4audio.ObjectTypeAACLC, SampleRate: 48000, ChannelConfig: 1, AU: bytes.Clone(au)})
				mu.Unlock()
				return true
			}, func(code string) { t.Errorf("unexpected G711 failure: %s", code) })
			if !encoder.available() {
				t.Fatalf("real FFmpeg rejected: %+v", encoder.report)
			}
			for index := 0; index < 50; index++ {
				encoder.enqueue(&rtp.Packet{Header: rtp.Header{SSRC: 42}, Payload: rtspG711Tone(codec, index*160, 160)}, 8000+int64(index*160))
				time.Sleep(20 * time.Millisecond)
			}
			mu.Lock()
			liveOutput := len(packets)
			mu.Unlock()
			if liveOutput < 10 {
				t.Fatalf("encoder buffered until EOF instead of streaming: %d AUs", liveOutput)
			}
			report := encoder.finish()
			if report.State != "ready" || report.InputSamples != 8000 || report.InputPackets != 50 || report.InsertedSilenceSamples != 0 || report.DroppedLateSamples != 0 || report.EncoderPrimingSamples == nil || *report.EncoderPrimingSamples != 1024 || report.StartupMS == nil || report.OutputAUs < 40 {
				t.Fatalf("incorrect real G711 conversion: %+v", report)
			}
			if timestamps[0] != 2978 || timestamps[1] != 3000 {
				t.Fatalf("AAC priming or shared receiver offset was flattened: %v", timestamps[:min(3, len(timestamps))])
			}
			for i := 1; i < len(timestamps); i++ {
				if timestamps[i] < timestamps[i-1] {
					t.Fatal("AAC output timestamp regressed")
				}
			}
			encoded, err := packets.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			decoder := exec.CommandContext(ctx, tool, "-hide_banner", "-loglevel", "error", "-f", "aac", "-i", "pipe:0", "-ac", "1", "-ar", "48000", "-f", "s16le", "pipe:1")
			decoder.Stdin = bytes.NewReader(encoded)
			pcm, err := decoder.Output()
			if err != nil {
				t.Fatal(err)
			}
			// Check decoded signal energy at the original 440 Hz, not merely that
			// the encoder emitted nonempty bytes or a browser accepted an init box.
			start := int(*report.EncoderPrimingSamples) + 4800
			count := 19200
			if len(pcm) < (start+count)*2 {
				t.Fatalf("too little decoded audio: %d", len(pcm))
			}
			var energy, sine, cosine float64
			for i := 0; i < count; i++ {
				sample := float64(int16(binary.LittleEndian.Uint16(pcm[(start+i)*2:])))
				phase := 2 * math.Pi * 440 * float64(i) / 48000
				energy += sample * sample
				sine += sample * math.Sin(phase)
				cosine += sample * math.Cos(phase)
			}
			rms := math.Sqrt(energy / float64(count))
			amplitude := 2 * math.Hypot(sine, cosine) / float64(count)
			fraction := amplitude * amplitude / 2 / (energy / float64(count))
			if rms < 5000 || fraction < 0.85 {
				t.Fatalf("AAC decoded silence or wrong content: RMS=%.1f tone fraction=%.3f", rms, fraction)
			}
		})
	}
}

func TestG711GapBackfillAndLargeResetAreExplicit(t *testing.T) {
	tool := rtspG711TestTool(t)
	var times []int64
	encoder := newRTSPG711Encoder(rtspG711Config{codec: "pcmu", rate: 8000, channels: 1, tool: tool}, nil, func(at int64, _, _ []byte) bool { times = append(times, at); return true }, nil)
	if !encoder.available() {
		t.Fatal(encoder.report)
	}
	for _, pts := range []int64{0, 160, 480, 480, 640} {
		encoder.enqueue(&rtp.Packet{Header: rtp.Header{SSRC: 1}, Payload: rtspG711Tone("pcmu", int(pts), 160)}, pts)
	}
	report := encoder.finish()
	if report.State != "ready" || report.InputSamples != 640 || report.InsertedSilenceSamples != 160 || report.DroppedLateSamples != 160 || report.TimelineResets != 0 || len(times) == 0 || times[0] != 1978 {
		t.Fatalf("gap was compressed or overlap duplicated: %+v times=%v", report, times)
	}
	encoder = newRTSPG711Encoder(rtspG711Config{codec: "pcmu", rate: 8000, channels: 1, tool: tool}, nil, func(int64, []byte, []byte) bool { return true }, nil)
	encoder.enqueue(&rtp.Packet{Header: rtp.Header{SSRC: 1}, Payload: rtspG711Tone("pcmu", 0, 160)}, 0)
	encoder.enqueue(&rtp.Packet{Header: rtp.Header{SSRC: 1}, Payload: rtspG711Tone("pcmu", 0, 160)}, 16000)
	report = encoder.finish()
	if report.State != "failed" || report.ReasonCode != "g711_timeline_discontinuity" || report.TimelineResets != 1 || report.InsertedSilenceSamples != 0 {
		t.Fatalf("large gap silently synthesized media: %+v", report)
	}
}

func TestG711UnavailableQueueAndRuntimeFailureAreBounded(t *testing.T) {
	missing := newRTSPG711Encoder(rtspG711Config{codec: "pcma", rate: 8000, channels: 1}, func() (string, error) { return "", os.ErrNotExist }, nil, nil)
	if missing.available() {
		t.Fatal("missing FFmpeg advertised playable audio")
	}
	if report := missing.finish(); report.State != "unavailable" || report.ReasonCode != "ffmpeg_unavailable" || report.OutputAUs != 0 {
		t.Fatal(report)
	}
	blocked := &rtspG711Encoder{config: rtspG711Config{codec: "pcmu", rate: 8000, channels: 1}, queue: make(chan rtspG711Input, 1)}
	packet := &rtp.Packet{Payload: make([]byte, 160)}
	start := time.Now()
	for i := 0; i < 1000; i++ {
		blocked.enqueue(packet, int64(i*160))
	}
	if time.Since(start) > time.Second || blocked.report.DroppedPackets != 1 || blocked.report.ReasonCode != "g711_queue_limit" {
		t.Fatalf("native callback blocked or queue unbounded: %+v", blocked.report)
	}
	tool := rtspG711TestTool(t)
	failed := newRTSPG711Encoder(rtspG711Config{codec: "pcma", rate: 8000, channels: 1, tool: tool}, nil, func(int64, []byte, []byte) bool { return true }, nil)
	if !failed.available() {
		t.Fatal(failed.report)
	}
	failed.command.Process.Kill()
	failed.enqueue(packet, 0)
	start = time.Now()
	report := failed.finish()
	if time.Since(start) > 2500*time.Millisecond || report.State != "failed" || report.ReasonCode == "network_error" || failed.command.ProcessState == nil {
		t.Fatalf("failed encoder leaked or masqueraded as network fault: %+v", report)
	}
}

func TestG711MetadataAndADTSValidation(t *testing.T) {
	packet := mpeg4audio.ADTSPackets{&mpeg4audio.ADTSPacket{Type: mpeg4audio.ObjectTypeAACLC, SampleRate: 48000, ChannelConfig: 1, AU: []byte{1, 2, 3}}}
	data, err := packet.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if au, err := readRTSPG711ADTS(bytes.NewReader(data), 1); err != nil || !bytes.Equal(au, []byte{1, 2, 3}) {
		t.Fatalf("valid ADTS rejected: %x %v", au, err)
	}
	data[2] = 0xff
	if _, err := readRTSPG711ADTS(bytes.NewReader(data), 1); err == nil {
		t.Fatal("unsupported output ADTS rate accepted")
	}
	for _, text := range []string{"0,-1024,-1024,1024,4,0x1234\n", "#tb 0: 1/8000\n", "#tb 0: 1/48000\n#codec_id 0: aac\n#sample_rate 0: 48000\n#channel_layout_name 0: mono\n0,NaN,0,1024,4,0x1234\n"} {
		if _, err := newRTSPG711CRCReader(bytes.NewBufferString(text), 1).next(); err == nil || err == io.EOF {
			t.Fatalf("invalid timing metadata accepted: %q", text)
		}
	}
}

func TestG711InheritedPipesAndSinkFailureTerminate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX inherited-pipe wrapper regression")
	}
	tool := rtspG711TestTool(t)
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "ffmpeg")
	marker, childPID := filepath.Join(dir, "started"), filepath.Join(dir, "child")
	script := fmt.Sprintf("#!/bin/sh\nif [ ! -e %q ]; then touch %q; exec %q \"$@\"; fi\ncat >/dev/null\nsleep 10 &\necho $! > %q\nwait\n", marker, marker, tool, childPID)
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		data, _ := os.ReadFile(childPID)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 {
			if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill()
			}
		}
	})
	encoder := newRTSPG711Encoder(rtspG711Config{codec: "pcmu", rate: 8000, channels: 1, tool: wrapper}, nil, func(int64, []byte, []byte) bool { return true }, nil)
	encoder.enqueue(&rtp.Packet{Header: rtp.Header{SSRC: 1}, Payload: rtspG711Tone("pcmu", 0, 160)}, 0)
	start := time.Now()
	report := encoder.finish()
	if time.Since(start) > 2500*time.Millisecond || report.ReasonCode != "ffmpeg_drain_timeout" || encoder.command.ProcessState == nil {
		t.Fatalf("inherited pipe prevented bounded reap: %+v elapsed=%s", report, time.Since(start))
	}
	failed := newRTSPG711Encoder(rtspG711Config{codec: "pcmu", rate: 8000, channels: 1, tool: tool}, nil, func(int64, []byte, []byte) bool { return false }, nil)
	failed.enqueue(&rtp.Packet{Header: rtp.Header{SSRC: 1}, Payload: rtspG711Tone("pcmu", 0, 160)}, 0)
	report = failed.finish()
	if report.ReasonCode != "g711_player_unavailable" || report.OutputAUs != 0 || !validRTSPG711Reason(report.ReasonCode) {
		t.Fatalf("unimportable sink failure: %+v", report)
	}
}

func TestG711NestedMetadataRequiresActualMeasurements(t *testing.T) {
	zero, priming, padding := int64(0), int64(1024), int64(0)
	base := RTSPPreviewReport{Version: 2, CodecCapabilitiesVersion: 3, State: "ready", ClockAlignment: "receiver_rtp", AudioTrackID: 1, AudioCodec: "pcmu", AudioOutputCodec: "aac", AudioTransform: "g711_to_aac", AudioAUs: 2, G711: &RTSPG711PreviewReport{Version: 1, Encoder: "ffmpeg", State: "ready", InputSampleRate: 8000, OutputSampleRate: 48000, Channels: 1, InputPackets: 1, InputSamples: 160, OutputAUs: 2, OutputSamples: 1984, FirstInputPTS: &zero, EncoderPrimingSamples: &priming, EncoderPaddingSamples: &padding, ToolCheckMS: &zero, StartupMS: &zero, DrainMS: &zero}}
	tracks := []RTSPTrackMetrics{{ID: 1, Kind: "audio", Codec: "pcmu", ClockRate: 8000, Channels: 1}}
	if !validRTSPPreview(&base, tracks) {
		t.Fatal("measured baseline rejected")
	}
	for name, set := range map[string]func(*RTSPG711PreviewReport){
		"priming": func(v *RTSPG711PreviewReport) { v.EncoderPrimingSamples = &zero },
		"padding": func(v *RTSPG711PreviewReport) { v.EncoderPaddingSamples = &zero },
		"startup": func(v *RTSPG711PreviewReport) { v.StartupMS = &zero },
	} {
		t.Run("zero output with partial "+name, func(t *testing.T) {
			copy := base
			value := RTSPG711PreviewReport{Version: 1, Encoder: "ffmpeg", State: "no_media", ReasonCode: "audio_no_media", InputSampleRate: 8000, OutputSampleRate: 48000, Channels: 1}
			set(&value)
			copy.State, copy.ReasonCode, copy.AudioAUs, copy.G711 = "failed", "audio_no_media", 0, &value
			if validRTSPPreview(&copy, tracks) {
				t.Fatal("zero-output report accepted partial measured output pointers")
			}
		})
	}
	for name, mutate := range map[string]func(*RTSPG711PreviewReport){
		"zero input packets":      func(v *RTSPG711PreviewReport) { v.InputPackets = 0 },
		"zero input samples":      func(v *RTSPG711PreviewReport) { v.InputSamples = 0; v.FirstInputPTS = nil },
		"zero output samples":     func(v *RTSPG711PreviewReport) { v.OutputSamples = 0 },
		"too many output samples": func(v *RTSPG711PreviewReport) { v.OutputSamples = 2049 },
		"unavailable without reason": func(v *RTSPG711PreviewReport) {
			*v = RTSPG711PreviewReport{Version: 1, Encoder: "ffmpeg", State: "unavailable", InputSampleRate: 8000, OutputSampleRate: 48000, Channels: 1}
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := base
			nested := cloneRTSPG711Report(*base.G711)
			copy.G711 = &nested
			mutate(copy.G711)
			copy.AudioAUs = copy.G711.OutputAUs
			if validRTSPPreview(&copy, tracks) {
				t.Fatal("contradictory counters accepted")
			}
		})
	}
}

func TestG711SampleLimitPreservesImportableCounts(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	encoder := &rtspG711Encoder{config: rtspG711Config{codec: "pcmu", rate: 8000, channels: 1}, ctx: ctx, cancel: cancel, queue: make(chan rtspG711Input, 4), stdin: nopRTSPG711Writer{}}
	limit := int64(maxSeconds+2) * 8000
	encoder.report = RTSPG711PreviewReport{Version: 1, Encoder: "ffmpeg", State: "starting", InputSampleRate: 8000, OutputSampleRate: 48000, Channels: 1, InputPackets: 1, InputSamples: limit - 160}
	for _, pts := range []int64{0, 160} {
		encoder.queue <- rtspG711Input{payload: rtspG711Tone("pcmu", 0, 160), pts: pts, ssrc: 1}
	}
	close(encoder.queue)
	encoder.writeInput()
	value := encoder.report
	preview := &RTSPPreviewReport{Version: 2, CodecCapabilitiesVersion: 3, State: "failed", ReasonCode: value.ReasonCode, ClockAlignment: "receiver_rtp", AudioTrackID: 1, AudioCodec: "pcmu", AudioOutputCodec: "aac", AudioTransform: "g711_to_aac", G711: &value}
	if value.InputSamples != limit || value.InputPackets != 2 || value.DroppedPackets != 1 || value.ReasonCode != "g711_sample_limit" || !validRTSPPreview(preview, []RTSPTrackMetrics{{ID: 1, Kind: "audio", Codec: "pcmu", ClockRate: 8000, Channels: 1}}) {
		t.Fatalf("limit failure invalidates own report: %+v", value)
	}
}

type nopRTSPG711Writer struct{}

func (nopRTSPG711Writer) Write(data []byte) (int, error) { return len(data), nil }
func (nopRTSPG711Writer) Close() error                   { return nil }
