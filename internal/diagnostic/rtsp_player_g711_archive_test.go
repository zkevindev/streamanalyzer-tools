package diagnostic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// This synthetic capture was produced by the unchanged, published Opus
// capability-two server, before the G.711 preview pipeline existed.
func TestG711PublishedCapabilityTwoAudioUnsupportedBundleRemainsExact(t *testing.T) {
	data, err := os.ReadFile("testdata/rtsp-v4-cap2-g711-unsupported.zip")
	if err != nil {
		t.Fatal(err)
	}
	files := rtspIntegrationZIP(t, data)
	var expected Report
	if err := json.Unmarshal(files[reportName], &expected); err != nil {
		t.Fatal(err)
	}
	preview := expected.RTSP.Preview
	if expected.RuleVersion != "rtsp/4" || preview == nil || preview.Version != 2 || preview.CodecCapabilitiesVersion != 2 || preview.G711 != nil || preview.AudioReason != "codec_unsupported" || preview.AudioAUs != 0 || preview.AudioTrackID != 0 {
		t.Fatal("invalid published capability-two fixture")
	}
	actual, err := readBundle(t.TempDir(), bytes.NewReader(data))
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("published unsupported G.711 audio result changed: %v", err)
	}
	before, _ := json.Marshal(expected)
	after, _ := json.Marshal(actual)
	if !bytes.Equal(before, after) {
		t.Fatal("published capability-two report JSON changed")
	}
	projected := modelRTSP(expected.RTSP)
	if projected.Preview == nil || projected.Preview.G711 != nil || projected.Preview.AudioReason != "codec_unsupported" || projected.Preview.CodecCapabilitiesVersion != 2 {
		t.Fatal("new model projection reinterpreted historical G.711 capability")
	}
}

func TestG711HistoricalUnsupportedReasonLabelsRemainExact(t *testing.T) {
	for _, test := range []struct {
		version, capability int
		want                string
	}{
		{1, 0, "首版仅支持 H.264 / AAC-LC"},
		{2, 1, "本版支持 H.264 / H.265 / AAC-LC；其他编码预览暂不支持"},
		{2, 2, "本版支持 H.264 / H.265 / AAC-LC / Opus；其他编码预览暂不支持"},
	} {
		preview := &RTSPPreviewReport{Version: test.version, CodecCapabilitiesVersion: test.capability}
		if got := rtspPreviewReasonLabelFor(preview, "codec_unsupported"); got != test.want {
			t.Fatalf("historical capability %d reason changed: %q", test.capability, got)
		}
	}
}

func TestG711HistoricalArchivesRejectNestedProofEvenWhenNull(t *testing.T) {
	for _, fixture := range []string{"rtsp-v3-h264-preview.zip", "rtsp-v4-cap1-opus-unsupported.zip", "rtsp-v4-cap2-g711-unsupported.zip"} {
		data, err := os.ReadFile("testdata/" + fixture)
		if err != nil {
			t.Fatal(err)
		}
		outer := rtspIntegrationZIP(t, data)
		var expected Report
		if err := json.Unmarshal(outer[reportName], &expected); err != nil {
			t.Fatal(err)
		}
		inner := rtspIntegrationZIP(t, outer[rtspStreamName])
		for _, proof := range []string{"null", `{"version":1,"encoder":"ffmpeg"}`} {
			t.Run(fixture+"/"+proof, func(t *testing.T) {
				entries := make(map[string][]byte, len(inner))
				for name, value := range inner {
					entries[name] = bytes.Clone(value)
				}
				var metadata map[string]json.RawMessage
				if err := json.Unmarshal(entries[rtspMetadataName], &metadata); err != nil {
					t.Fatal(err)
				}
				var preview map[string]json.RawMessage
				if err := json.Unmarshal(metadata["preview"], &preview); err != nil {
					t.Fatal(err)
				}
				preview["g711"] = json.RawMessage(proof)
				metadata["preview"], _ = json.Marshal(preview)
				entries[rtspMetadataName], _ = json.Marshal(metadata)
				dir := t.TempDir()
				rtspUDPTestWriteArchive(t, dir, entries)
				if err := os.WriteFile(filepath.Join(dir, timelineName), outer[timelineName], 0600); err != nil {
					t.Fatal(err)
				}
				// Rebuild a valid ZIP and its artifact hash. Rejection must come
				// from immutable capability proof, rather than corruption/hash.
				stream, err := os.ReadFile(filepath.Join(dir, rtspStreamName))
				if err != nil {
					t.Fatal(err)
				}
				manifest := expected.Manifest
				hash := sha256.Sum256(stream)
				manifest.Stream = Artifact{Bytes: int64(len(stream)), SHA256: hex.EncodeToString(hash[:])}
				if _, err := replayRTSP(dir, manifest); !errors.Is(err, ErrInvalid) {
					t.Fatalf("historical preview accepted a new G.711 field: %v", err)
				}
			})
		}
	}
}
