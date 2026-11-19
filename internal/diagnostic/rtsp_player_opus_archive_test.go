package diagnostic

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestRTSPOpusPublishedCapabilityOneBundleRemainsExact(t *testing.T) {
	data, err := os.ReadFile("testdata/rtsp-v4-cap1-opus-unsupported.zip")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var expected Report
	for _, entry := range archive.File {
		if entry.Name != reportName {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewDecoder(reader).Decode(&expected)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if expected.RuleVersion != "rtsp/4" || expected.RTSP == nil || expected.RTSP.Preview == nil || expected.RTSP.Preview.CodecCapabilitiesVersion != 1 || expected.RTSP.Preview.AudioReason != "codec_unsupported" || expected.RTSP.Preview.AudioAUs != 0 {
		t.Fatal("invalid published capability-one fixture")
	}
	actual, err := readBundle(t.TempDir(), bytes.NewReader(data))
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("published capability-one Opus unsupported report changed: %v", err)
	}
}

func TestRTSPOpusCapabilityVersionPreservesHistoricalPreview(t *testing.T) {
	tracks := []RTSPTrackMetrics{{ID: 1, Kind: "audio", Codec: "opus"}}
	valid := RTSPPreviewReport{Version: 2, CodecCapabilitiesVersion: 2, State: "ready", ClockAlignment: "receiver_rtp", AudioTrackID: 1, AudioCodec: "opus", AudioOutputCodec: "opus", AudioTransform: "native_remux", AudioAUs: 3}
	if !validRTSPPreview(&valid, tracks) {
		t.Fatal("native Opus report rejected")
	}
	for name, mutate := range map[string]func(*RTSPPreviewReport){
		"old_capability": func(p *RTSPPreviewReport) { p.CodecCapabilitiesVersion = 1 },
		"old_preview": func(p *RTSPPreviewReport) {
			p.Version = 1
			p.CodecCapabilitiesVersion = 0
			p.AudioOutputCodec = ""
			p.AudioTransform = ""
		},
		"different_output":   func(p *RTSPPreviewReport) { p.AudioOutputCodec = "aac" },
		"invented_transcode": func(p *RTSPPreviewReport) { p.AudioTransform = "ffmpeg_transcode" },
		"unknown_capability": func(p *RTSPPreviewReport) { p.CodecCapabilitiesVersion = rtspPreviewCodecCapabilitiesVersion + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			value := valid
			mutate(&value)
			if validRTSPPreview(&value, tracks) || modelRTSP(&RTSPMetrics{Preview: &value, Tracks: tracks}).Preview != nil {
				t.Fatal("unproven Opus pipeline entered an older capability or model")
			}
		})
	}
	// The old capability is an immutable observation, including its old
	// unsupported Opus result. Reading it with newer code must not reinterpret it.
	historical := RTSPPreviewReport{Version: 2, CodecCapabilitiesVersion: 1, State: "unsupported", ReasonCode: "codec_unsupported", ClockAlignment: "receiver_rtp", VideoReason: "no_track", AudioReason: "codec_unsupported"}
	before, _ := json.Marshal(historical)
	if !validRTSPPreview(&historical, tracks) {
		t.Fatal("historical unsupported result rejected")
	}
	projected := modelRTSP(&RTSPMetrics{Preview: &historical, Tracks: tracks})
	after, _ := json.Marshal(projected.Preview)
	if string(before) != string(after) {
		t.Fatal("historical pipeline was upgraded during projection")
	}
	if rtspPreviewReasonLabelFor(&historical, "codec_unsupported") != "本版支持 H.264 / H.265 / AAC-LC；其他编码预览暂不支持" {
		t.Fatal("historical capability finding text changed")
	}
	withNewReason := historical
	withNewReason.AudioReason = "opus_configuration_unsupported"
	if validRTSPPreview(&withNewReason, tracks) {
		t.Fatal("historical capability accepted a future Opus reason")
	}
}
