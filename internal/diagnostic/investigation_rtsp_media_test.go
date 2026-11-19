package diagnostic

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/pion/rtp"
	"streamanalyzer/internal/simulator/testpattern"
)

func investigationRTSPFixture(t *testing.T, name string) (string, []rtspIntegrationPacket) {
	t.Helper()
	var codec format.Format
	var frames [][][]byte
	var times []int64
	var payloads [][]byte
	var durations []int64
	switch name {
	case "h264":
		codec = &format.H264{PayloadTyp: 96, PacketizationMode: 1}
		for i := 0; i < 60; i++ {
			var au h264.AnnexB
			if err := au.Unmarshal(testpattern.AccessUnit(i % testpattern.Len())); err != nil {
				t.Fatal(err)
			}
			frames = append(frames, au)
			times = append(times, int64(i*6000))
		}
	case "h265":
		var vps, sps, pps []byte
		frames, times, vps, sps, pps = hevcPreviewFixture(t)
		codec = &format.H265{PayloadTyp: 96, VPS: vps, SPS: sps, PPS: pps}
	case "aac":
		codec = &format.MPEG4Audio{PayloadTyp: 97, Config: &mpeg4audio.AudioSpecificConfig{Type: mpeg4audio.ObjectTypeAACLC, SampleRate: 48000, ChannelConfig: 1}, SizeLength: 13, IndexLength: 3, IndexDeltaLength: 3}
		data, err := os.ReadFile("testdata/rtsp-stall-fixture/tone.aac")
		if err != nil {
			t.Fatal(err)
		}
		var adts mpeg4audio.ADTSPackets
		if err := adts.Unmarshal(data); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 80; i++ {
			payloads = append(payloads, adts[i%len(adts)].AU)
			durations = append(durations, 1024)
		}
	case "opus":
		codec = &format.Opus{PayloadTyp: 98, ChannelCount: 2}
		data, err := os.ReadFile("testdata/opus-fixture/packets.json")
		if err != nil {
			t.Fatal(err)
		}
		var source []struct {
			Samples int64
			Payload []byte
		}
		if err := json.Unmarshal(data, &source); err != nil {
			t.Fatal(err)
		}
		for repeat := 0; repeat < 8; repeat++ {
			for _, item := range source {
				payloads = append(payloads, item.Payload)
				durations = append(durations, item.Samples)
			}
		}
	case "pcma", "pcmu":
		codec = &format.G711{MULaw: name == "pcmu", SampleRate: 8000, ChannelCount: 1}
		for i := 0; i < 60; i++ {
			payloads = append(payloads, rtspG711Tone(name, i*160, 160))
			durations = append(durations, 160)
		}
	}
	var sdp string
	if name == "h264" {
		sdp = rtspIntegrationVideoSDP()
	} else if name == "h265" {
		sdp = rtspIntegrationSDP + "m=video 0 RTP/AVP 96\r\na=rtpmap:96 H265/90000\r\na=control:trackID=0\r\n"
	} else {
		pt := codec.PayloadType()
		rtpmap := ""
		fmtp := ""
		switch name {
		case "aac":
			rtpmap = "MPEG4-GENERIC/48000/1"
			fmtp = "a=fmtp:97 streamtype=5;profile-level-id=1;mode=AAC-hbr;config=1188;sizelength=13;indexlength=3;indexdeltalength=3\r\n"
		case "opus":
			rtpmap = "opus/48000/2"
		case "pcma":
			pt = 8
			rtpmap = "PCMA/8000/1"
		case "pcmu":
			pt = 0
			rtpmap = "PCMU/8000/1"
		}
		sdp = rtspIntegrationSDP + fmt.Sprintf("m=audio 0 RTP/AVP %d\r\na=rtpmap:%d %s\r\n", pt, pt, rtpmap) + fmtp + "a=control:trackID=0\r\n"
	}
	var output []rtspIntegrationPacket
	sequence := uint16(65520)
	add := func(packet *rtp.Packet, at int64) {
		packet.Version = 2
		packet.SSRC = 7
		packet.SequenceNumber = sequence
		sequence++
		packet.Timestamp = uint32(0xffff0000 + at)
		raw, err := packet.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		delay := time.Millisecond
		if name == "pcma" || name == "pcmu" {
			delay = 20 * time.Millisecond
		}
		output = append(output, rtspIntegrationPacket{raw: raw, delay: delay})
	}
	switch value := codec.(type) {
	case *format.H264:
		encoder, err := value.CreateEncoder()
		if err != nil {
			t.Fatal(err)
		}
		for i, au := range frames {
			packets, err := encoder.Encode(au)
			if err != nil {
				t.Fatal(err)
			}
			for _, packet := range packets {
				add(packet, times[i])
			}
		}
	case *format.H265:
		encoder, err := value.CreateEncoder()
		if err != nil {
			t.Fatal(err)
		}
		for i, au := range frames {
			packets, err := encoder.Encode(au)
			if err != nil {
				t.Fatal(err)
			}
			for _, packet := range packets {
				add(packet, times[i])
			}
		}
	case *format.MPEG4Audio:
		encoder, err := value.CreateEncoder()
		if err != nil {
			t.Fatal(err)
		}
		at := int64(0)
		for i, payload := range payloads {
			packets, err := encoder.Encode([][]byte{payload})
			if err != nil {
				t.Fatal(err)
			}
			for _, packet := range packets {
				add(packet, at)
			}
			at += durations[i]
		}
	default:
		at := int64(0)
		for i, payload := range payloads {
			pt := codec.PayloadType()
			if name == "pcma" {
				pt = 8
			}
			add(&rtp.Packet{Header: rtp.Header{PayloadType: pt, Marker: true}, Payload: payload}, at)
			at += durations[i]
			if name == "opus" && i == 7 {
				at += 4800
			}
		}
	}
	return sdp, output
}
func investigationRTSPCaptureFixture(t *testing.T, name string) (string, Report, StallInvestigation) {
	t.Helper()
	sdp, packets := investigationRTSPFixture(t, name)
	return investigationRTSPCapturePackets(t, sdp, packets)
}
func investigationRTSPCapturePackets(t *testing.T, sdp string, packets []rtspIntegrationPacket) (string, Report, StallInvestigation) {
	t.Helper()
	fixture := newRTSPIntegrationFixture(t, rtspIntegrationConfig{sdp: sdp, packets: packets, closeAfterPlay: true})
	t.Cleanup(func() {
		if plays := fixture.plays.Load(); plays != 1 {
			t.Errorf("offline extraction opened another source PLAY: %d", plays)
		}
	})
	target, _ := parseProtocolURL(fixture.address(), ProtocolRTSP)
	dir := t.TempDir()
	player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
	defer player.Close()
	report, err := sampleRTSP(withPlayerSession(context.Background(), player), dir, target, 3, ProtocolRTSP)
	if err != nil {
		t.Fatal(err)
	}
	if report.RTSP.Preview == nil || report.RTSP.Preview.VideoAUs+report.RTSP.Preview.AudioAUs == 0 {
		t.Fatalf("fixture produced no native preview: %+v", report.RTSP.Preview)
	}
	artifact := investigationMediaSave(t, dir, playerName, []byte("{}"))
	report.Manifest.Player = &artifact
	report.Player = &PlayerReport{Source: "same_capture", Revision: 1, BufferSamples: []PlayerBufferSample{{OffsetMS: 2400, CurrentMS: 1000, ReadyState: 4}}}
	item := StallInvestigation{ID: "S001", PlayerRevision: 1, Stall: PlayerStall{StartMS: 2500, EndMS: 3300, DurationMS: 800}}
	return dir, report, item
}

func TestRTSPMediaOfflineAllCodecsAndDeterministicProvenance(t *testing.T) {
	ffmpeg := rtspG711TestTool(t)
	ffprobe, err := investigationMediaTool("ffprobe", filepath.Dir(ffmpeg))
	if err != nil {
		t.Skip("ffprobe missing")
	}
	for _, name := range []string{"h264", "h265", "aac", "opus", "pcma", "pcmu"} {
		t.Run(name, func(t *testing.T) {
			dir, report, item := investigationRTSPCaptureFixture(t, name)
			stage := t.TempDir()
			input, err := investigationMediaRTSP(t.Context(), dir, stage, report, item)
			if err != nil {
				t.Fatal(err)
			}
			proof := input.rtsp
			if proof == nil || proof.PacketCount == 0 || proof.RemuxArtifact.Bytes == 0 || proof.AnchorCurrentMS != 1000 || proof.AnchorSampleOffsetMS != 2400 || input.alignment != "receiver_rtp_estimate" {
				t.Fatalf("missing exact inputs: %+v", proof)
			}
			if (name == "h264" || name == "h265") && input.partial {
				t.Fatal("normal unknown-duration final sample was mislabeled packet loss")
			}
			if name == "opus" {
				raw, err := os.ReadFile(input.path)
				if err != nil {
					t.Fatal(err)
				}
				var parts fmp4.Parts
				if err := parts.Unmarshal(raw); err != nil {
					t.Fatal(err)
				}
				if len(parts) < 2 || parts[0].Tracks[0].Samples[0].Duration != 120 || parts[1].Tracks[0].BaseTime-parts[0].Tracks[0].BaseTime != 120 {
					t.Fatal("2.5ms Opus packets lost RTP sample resolution")
				}
				gap := false
				for i := 1; i < len(parts); i++ {
					previous := parts[i-1].Tracks[0]
					if parts[i].Tracks[0].BaseTime > previous.BaseTime+uint64(previous.Samples[0].Duration) {
						gap = true
					}
				}
				if !gap {
					t.Fatal("Opus DTX timestamp gap was flattened")
				}
			}
			for repeat := 0; repeat < 3; repeat++ {
				again, err := recomputeRTSPMediaEvidence(t.Context(), dir, report, item)
				if err != nil || !reflect.DeepEqual(proof, again) {
					t.Fatalf("offline recipe changed: %v\n%+v\n%+v", err, proof, again)
				}
			}
			probe, err := investigationMediaInspect(t.Context(), ffprobe, input.path)
			if err != nil || probe.durationMS <= 500 {
				t.Fatalf("fMP4 not actually decodable: %+v %v", probe, err)
			}
			var pcm investigationMediaLimitedBuffer
			pcm.limit = 1 << 20
			args := []string{"-hide_banner", "-loglevel", "error", "-protocol_whitelist", "file,pipe", "-i", input.path, "-f", "null", "-"}
			if err := investigationMediaRun(t.Context(), ffmpeg, args, nil); err != nil {
				t.Fatalf("actual decoding failed: %v", err)
			}
			// Exercise the complete production extraction path too: the fMP4
			// reconstruction is only an intermediate input, not the shipped asset.
			media := extractStallMedia(t.Context(), dir, report, item)
			video := name == "h264" || name == "h265"
			if video {
				if media.Status != "ready" || len(media.Assets) != 4 {
					t.Fatalf("final video extraction incomplete: %+v", media)
				}
			} else if media.Status != "partial" || len(media.Assets) != 1 || media.Assets[0].Kind != "clip" {
				t.Fatalf("audio-only extraction fabricated images or lost audio: %+v", media)
			}
			if media.RTSP == nil || !reflect.DeepEqual(media.RTSP, proof) {
				t.Fatal("final assets lost source/clock provenance")
			}
			for _, asset := range media.Assets {
				path := filepath.Join(dir, asset.Name)
				if asset.Kind == "screenshot" {
					file, err := os.Open(path)
					if err != nil {
						t.Fatal(err)
					}
					picture, err := jpeg.Decode(file)
					file.Close()
					if err != nil || picture.Bounds().Dx() == 0 || picture.Bounds().Dy() == 0 {
						t.Fatalf("final JPEG does not decode: %s %v", asset.Name, err)
					}
					continue
				}
				var codecJSON investigationMediaLimitedBuffer
				codecJSON.limit = 4096
				if err := investigationMediaRun(t.Context(), ffprobe, []string{"-hide_banner", "-loglevel", "error", "-show_entries", "stream=codec_name,codec_type", "-of", "json", path}, &codecJSON); err != nil {
					t.Fatal(err)
				}
				var streams struct {
					Streams []struct {
						Codec string `json:"codec_name"`
						Kind  string `json:"codec_type"`
					} `json:"streams"`
				}
				if err := json.Unmarshal(codecJSON.Bytes(), &streams); err != nil {
					t.Fatal(err)
				}
				if len(streams.Streams) != 1 || video && (streams.Streams[0].Kind != "video" || streams.Streams[0].Codec != "h264") || !video && (streams.Streams[0].Kind != "audio" || streams.Streams[0].Codec != "aac") {
					t.Fatalf("final asset codecs are wrong: %s", codecJSON.Bytes())
				}
				if video {
					if err := investigationMediaRun(t.Context(), ffmpeg, []string{"-hide_banner", "-loglevel", "error", "-i", path, "-f", "null", "-"}, nil); err != nil {
						t.Fatalf("final H264 MP4 does not decode: %v", err)
					}
				} else {
					var decoded investigationMediaLimitedBuffer
					decoded.limit = 1 << 20
					if err := investigationMediaRun(t.Context(), ffmpeg, []string{"-hide_banner", "-loglevel", "error", "-i", path, "-ac", "1", "-ar", "8000", "-f", "s16le", "pipe:1"}, &decoded); err != nil {
						t.Fatalf("final AAC MP4 does not actually decode: %v", err)
					}
					if decoded.Len() < 8000 {
						t.Fatalf("too little final decoded AAC: %d bytes", decoded.Len())
					}
					var energy float64
					for offset := 0; offset+2 <= decoded.Len(); offset += 2 {
						sample := float64(int16(binary.LittleEndian.Uint16(decoded.Bytes()[offset:])))
						energy += sample * sample
					}
					if math.Sqrt(energy/float64(decoded.Len()/2)) < 100 {
						t.Fatal("final AAC decoded empty/silent fixture instead of captured audio")
					}
				}
			}
			if name == "h265" {
				if proof.PreviewOriginMS >= proof.MediaOriginMS || input.anchorMS != 1000+proof.PreviewOriginMS-proof.MediaOriginMS {
					t.Fatalf("B-frame decode/presentation origin flattened: %+v anchor=%d", proof, input.anchorMS)
				}
			}
			if name == "pcma" || name == "pcmu" {
				if proof.AudioTransform != "g711_to_pcm" {
					t.Fatal(proof)
				}
				if err := investigationMediaRun(t.Context(), ffmpeg, []string{"-hide_banner", "-loglevel", "error", "-i", input.path, "-ac", "1", "-ar", "8000", "-f", "s16le", "pipe:1"}, &pcm); err != nil {
					t.Fatal(err)
				}
				var energy, sine, cosine float64
				count := min(4000, pcm.Len()/2)
				for i := 0; i < count; i++ {
					value := float64(int16(binary.LittleEndian.Uint16(pcm.Bytes()[i*2:])))
					energy += value * value
					sine += value * math.Sin(2*math.Pi*440*float64(i)/8000)
					cosine += value * math.Cos(2*math.Pi*440*float64(i)/8000)
				}
				if count < 4000 || math.Sqrt(energy/float64(count)) < 5000 || 2*(sine*sine+cosine*cosine)/(float64(count)*energy) < 0.9 {
					t.Fatal("G711 reconstruction changed signal content")
				}
			}
		})
	}
}

func TestRTSPMediaRejectsUnrelatedOrInactiveClock(t *testing.T) {
	dir, report, item := investigationRTSPCaptureFixture(t, "h264")
	for name, mutate := range map[string]func(*Report){
		"independent": func(r *Report) { r.Player.Source = "independent_pull" },
		"stale":       func(r *Report) { r.Player.BufferSamples[0].OffsetMS = 100 },
		"seeking":     func(r *Report) { r.Player.BufferSamples[0].Seeking = true },
		"hidden":      func(r *Report) { r.Player.BufferSamples[0].Hidden = true },
		"paused":      func(r *Report) { r.Player.BufferSamples[0].Paused = true },
		"outside":     func(r *Report) { r.Player.BufferSamples[0].CurrentMS = 100000 },
		"offset_seek": func(r *Report) {
			r.Player.StartOffsetMS = 1000
			r.Player.Events = []PlayerEvent{{AtMS: 1450, Kind: "seeking"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := report
			player := *report.Player
			player.BufferSamples = append([]PlayerBufferSample(nil), player.BufferSamples...)
			copy.Player = &player
			mutate(&copy)
			if _, err := investigationMediaRTSP(t.Context(), dir, t.TempDir(), copy, item); err == nil {
				t.Fatal("unrelated clock generated media")
			}
		})
	}
}

func TestRTSPMediaKeyframesStayNearLateCase(t *testing.T) {
	var events []Event
	for index := 0; index < 80; index++ {
		event := Event{Offset: int64(index), Artifact: rtspWireName, PTS: int64(index) * 2000, DTS: int64(index) * 2000}
		events = investigationRTSPKeyframes(events, event, 133000)
	}
	if len(events) != 16 {
		t.Fatal(len(events))
	}
	before := false
	for _, event := range events {
		if event.PTS < 110000 {
			t.Fatal("early irrelevant keyframe retained")
		}
		if event.PTS == 132000 {
			before = true
		}
	}
	if !before {
		t.Fatal("nearest preceding keyframe was discarded")
	}
}

func TestRTSPMediaLossResyncAndTruncatedTail(t *testing.T) {
	sdp, packets := investigationRTSPFixture(t, "h264")
	// A packet within the first GOP is missing; the second genuine IDR permits
	// recovery. The last FU payload is truncated without inventing a final frame.
	if len(packets) < 40 {
		t.Fatal("small fixture")
	}
	packets = append(packets[:20:20], packets[21:]...)
	packets = packets[:len(packets)-1]
	dir, report, item := investigationRTSPCapturePackets(t, sdp, packets)
	input, err := investigationMediaRTSP(t.Context(), dir, t.TempDir(), report, item)
	if err != nil {
		t.Fatal(err)
	}
	if input.rtsp.VideoAccessUnits < 5 || input.rtsp.VideoAccessUnits >= 59 || input.rtsp.DroppedPackets == 0 || !input.partial {
		t.Fatalf("loss failed to retain safe AUs: %+v", input.rtsp)
	}
	tool := rtspG711TestTool(t)
	if err := investigationMediaRun(t.Context(), tool, []string{"-hide_banner", "-loglevel", "error", "-i", input.path, "-f", "null", "-"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRTSPMediaReceiverClockAndSequenceWrap(t *testing.T) {
	codec := &format.Opus{PayloadTyp: 98, ChannelCount: 2}
	clock := investigationRTSPClock{}
	packet := &rtp.Packet{Header: rtp.Header{SSRC: 7, Timestamp: 0xfffffff0}, Payload: []byte{0x80, 1}}
	first, ok, err := clock.decode(1, codec, packet, 10000)
	if err != nil || !ok || first != 0 {
		t.Fatal(first, ok, err)
	}
	packet.Timestamp += 120
	second, _, err := clock.decode(1, codec, packet, 10000)
	if err != nil || second != 120 {
		t.Fatal("2.5ms Opus or timestamp wrap flattened", second, err)
	}
	var output []uint16
	reorder := investigationRTSPReorder{}
	for _, seq := range []uint16{65534, 0, 65535, 1, 1} {
		if err := reorder.push(&rtp.Packet{Header: rtp.Header{SequenceNumber: seq, SSRC: 7}}, Event{}, func(p *rtp.Packet, _ Event) error { output = append(output, p.SequenceNumber); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := reorder.finish(); err != nil || !reflect.DeepEqual(output, []uint16{65534, 65535, 0, 1}) || reorder.duplicates != 1 {
		t.Fatal(output, reorder, err)
	}
}

func TestRTSPMediaScreenshotUsesActualLowFPSVideoTail(t *testing.T) {
	tool := rtspG711TestTool(t)
	probe, err := investigationMediaTool("ffprobe", filepath.Dir(tool))
	if err != nil {
		t.Skip("ffprobe missing")
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "lowfps.mp4")
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=1/2:duration=7", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=8", "-map", "0:v", "-map", "1:a", "-c:v", "libx264", "-bf", "0", "-threads", "1", "-c:a", "aac", "-y", clip}
	if err := investigationMediaRun(t.Context(), tool, args, nil); err != nil {
		t.Fatal(err)
	}
	positions, err := investigationRTSPFramePositions(t.Context(), probe, clip, []int64{50, 5100, 7900}, 8000)
	if err != nil || !reflect.DeepEqual(positions, []int64{0, 4000, 6000}) {
		t.Fatalf("audio tail was mistaken for a video frame: %v %v", positions, err)
	}
	shifted := filepath.Join(dir, "shifted.mp4")
	if err := investigationMediaRun(t.Context(), tool, []string{"-hide_banner", "-loglevel", "error", "-i", clip, "-map", "0:v", "-c", "copy", "-output_ts_offset", "0.4", "-y", shifted}, nil); err != nil {
		t.Fatal(err)
	}
	shiftedPositions, err := investigationRTSPFramePositions(t.Context(), probe, shifted, []int64{50, 5100, 7900}, 8000)
	if err != nil || !reflect.DeepEqual(shiftedPositions, positions) {
		t.Fatalf("nonzero format origin was double-added: %v != %v, %v", shiftedPositions, positions, err)
	}
	for _, position := range positions {
		path := filepath.Join(dir, fmt.Sprintf("%d.jpg", position))
		args := []string{"-hide_banner", "-loglevel", "error", "-ss", investigationMediaSeconds(position), "-i", clip, "-frames:v", "1", "-an", "-threads", "1", "-y", path}
		if err := investigationMediaRun(t.Context(), tool, args, nil); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("actual selected frame did not decode: %d %v", position, err)
		}
	}
}

func TestRTSPMediaColoredBFramesUsePlayerTimeNotArrival(t *testing.T) {
	tool := rtspG711TestTool(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "colors.flv")
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=red:size=160x90:rate=10:duration=4", "-vf", "drawbox=x=0:y=0:w=iw:h=ih:color=green:t=fill:enable='gte(t,2)'", "-c:v", "libx264", "-bf", "2", "-g", "10", "-threads", "1", "-x264-params", "aud=1:repeat-headers=1", "-f", "flv", "-y", source}
	if err := investigationMediaRun(t.Context(), tool, args, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	codec := &format.H264{PayloadTyp: 96, PacketizationMode: 1}
	encoder, err := codec.CreateEncoder()
	if err != nil {
		t.Fatal(err)
	}
	var params [][]byte
	var packets []rtspIntegrationPacket
	sequence := uint16(1)
	for offset := 13; offset+15 <= len(data); {
		size := int(readUint24(data[offset+1 : offset+4]))
		if size > len(data)-offset-15 {
			t.Fatal("bad generated FLV")
		}
		body := data[offset+11 : offset+11+size]
		dts := int64(readUint24(data[offset+4:offset+7]) | uint32(data[offset+7])<<24)
		if data[offset] == 9 && len(body) > 5 && body[0]&15 == 7 {
			if body[1] == 0 {
				record := body[5:]
				if len(record) < 7 {
					t.Fatal("bad AVC config")
				}
				cursor := 6
				for i := 0; i < int(record[5]&31); i++ {
					n := int(binary.BigEndian.Uint16(record[cursor:]))
					cursor += 2
					params = append(params, append([]byte(nil), record[cursor:cursor+n]...))
					cursor += n
				}
				count := int(record[cursor])
				cursor++
				for i := 0; i < count; i++ {
					n := int(binary.BigEndian.Uint16(record[cursor:]))
					cursor += 2
					params = append(params, append([]byte(nil), record[cursor:cursor+n]...))
					cursor += n
				}
			}
			if body[1] == 1 {
				var au h264.AVCC
				if err := au.Unmarshal(body[5:]); err != nil {
					t.Fatal(err)
				}
				pts := dts + int64(int32(readUint24(body[2:5])<<8)>>8)
				if body[0]>>4 == 1 {
					au = append(append(h264.AVCC{}, params...), au...)
				}
				encoded, err := encoder.Encode(au)
				if err != nil {
					t.Fatal(err)
				}
				for _, packet := range encoded {
					packet.SequenceNumber = sequence
					sequence++
					packet.SSRC = 7
					packet.Timestamp = 37*90000 + uint32(pts*90)
					raw, err := packet.Marshal()
					if err != nil {
						t.Fatal(err)
					}
					packets = append(packets, rtspIntegrationPacket{raw: raw, delay: time.Millisecond})
				}
			}
		}
		offset += size + 15
	}
	captured, report, item := investigationRTSPCapturePackets(t, rtspIntegrationVideoSDP(), packets)
	if report.Manifest.Observation.ElapsedMS >= 1000 {
		t.Fatal("fixture must receive much faster than its media clock")
	}
	item.Stall.StartMS, item.Stall.EndMS = 4500, 5300
	report.Player.BufferSamples[0].OffsetMS = 4400
	for _, point := range []struct {
		current int64
		green   bool
	}{{600, false}, {2600, true}} {
		report.Player.BufferSamples[0].CurrentMS = point.current
		input, err := investigationMediaRTSP(t.Context(), captured, t.TempDir(), report, item)
		if err != nil {
			t.Fatal(err)
		}
		hasCTS := false
		for _, keyframe := range input.rtsp.Keyframes {
			if keyframe.DTS != keyframe.PTS {
				hasCTS = true
			}
		}
		if !hasCTS || input.anchorMS != point.current+input.rtsp.PreviewOriginMS-input.rtsp.MediaOriginMS {
			t.Fatalf("B-frame presentation/decode timing not retained: %+v", input.rtsp)
		}
		var pixel investigationMediaLimitedBuffer
		pixel.limit = 3
		args := []string{"-hide_banner", "-loglevel", "error", "-ss", investigationMediaSeconds(input.anchorMS), "-i", input.path, "-frames:v", "1", "-vf", "scale=1:1", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1"}
		if err := investigationMediaRun(t.Context(), tool, args, &pixel); err != nil || pixel.Len() != 3 {
			t.Fatalf("cannot decode chosen colored frame: %v %x", err, pixel.Bytes())
		}
		color := pixel.Bytes()
		if point.green {
			if color[1] < 80 || color[0] > 40 {
				t.Fatalf("arrival time selected red instead of green: %v anchor=%d", color, input.anchorMS)
			}
		} else {
			if color[0] < 150 || color[1] > 40 {
				t.Fatalf("expected early red frame: %v", color)
			}
		}
	}
}

func TestRTSPMediaUDPFragmentedOriginalDatagrams(t *testing.T) {
	_, source := investigationRTSPFixture(t, "h264")
	packets := make([]rtspUDPTestPacket, len(source))
	for index, value := range source {
		packets[index] = rtspUDPTestPacket{data: value.raw}
	}
	target, connections := rtspUDPTestServer(t, packets, false)
	dir := t.TempDir()
	player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
	defer player.Close()
	report, err := sampleRTSPTransport(withPlayerSession(t.Context(), player), dir, target, 1, ProtocolRTSP, "udp")
	if err != nil {
		t.Fatal(err)
	}
	if report.RTSP.Preview == nil || report.RTSP.Preview.VideoAUs < 30 {
		t.Fatalf("UDP live fixture did not decode fragmented in-band SPS/PPS: %+v", report.RTSP.Preview)
	}
	artifact := investigationMediaSave(t, dir, playerName, []byte("{}"))
	report.Manifest.Player = &artifact
	report.Player = &PlayerReport{Source: "same_capture", Revision: 1, BufferSamples: []PlayerBufferSample{{OffsetMS: 2400, CurrentMS: 1000, ReadyState: 4}}}
	item := StallInvestigation{ID: "S001", PlayerRevision: 1, Stall: PlayerStall{StartMS: 2500, EndMS: 3300, DurationMS: 800}}
	media := extractStallMedia(t.Context(), dir, report, item)
	if media.Status != "ready" || len(media.Assets) != 4 || media.RTSP == nil || media.RTSP.VideoAccessUnits < 30 {
		t.Fatalf("UDP reusable receive buffer corrupted retained depay fragments: %+v", media)
	}
	again, err := recomputeRTSPMediaEvidence(t.Context(), dir, report, item)
	if err != nil || !reflect.DeepEqual(again, media.RTSP) {
		t.Fatalf("UDP reconstruction changed across reads: %v", err)
	}
	if connections.Load() != 1 {
		t.Fatal("offline extraction opened a source connection")
	}
}

func TestRTSPMediaReorderOwnsImmediatePacketPayload(t *testing.T) {
	backing := make([]byte, 3)
	reorder := investigationRTSPReorder{}
	var retained [][]byte
	for index := 0; index < 3; index++ {
		for at := range backing {
			backing[at] = byte(index + 1)
		}
		packet := &rtp.Packet{Header: rtp.Header{SSRC: 7, SequenceNumber: uint16(index)}, Payload: backing}
		if err := reorder.push(packet, Event{}, func(packet *rtp.Packet, _ Event) error { retained = append(retained, packet.Payload); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	for index, payload := range retained {
		if len(payload) != 3 || payload[0] != byte(index+1) {
			t.Fatal("next UDP datagram overwrote retained in-order depay payload")
		}
	}
}

// Optional reproduction entry for a saved customer/real-browser run. Normal CI
// uses the self-contained native UDP fixture above and never requires this path.
func TestRTSPMediaSavedUDPArchive(t *testing.T) {
	source := os.Getenv("STREAMANALYZER_TEST_RTSP_SAVED_RUN")
	if source == "" {
		t.Skip("no external saved run")
	}
	data, err := os.ReadFile(filepath.Join(source, reportName))
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Player == nil || len(report.Player.Stalls) == 0 {
		t.Fatal("saved run has no player stall")
	}
	dir := t.TempDir()
	for _, name := range []string{rtspStreamName, timelineName} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	item := StallInvestigation{ID: "S001", PlayerRevision: report.Player.Revision, Stall: report.Player.Stalls[0]}
	media := extractStallMedia(t.Context(), dir, report, item)
	if media.Status != "ready" || len(media.Assets) != 4 || media.RTSP == nil {
		t.Fatalf("saved UDP source still fails extraction: %+v", media)
	}
	t.Logf("stored UDP reconstructed %d video AUs + %d audio AUs, %d selected packets, all4assets", media.RTSP.VideoAccessUnits, media.RTSP.AudioAccessUnits, media.RTSP.PacketCount)
}
