package diagnostic

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRTSPReplayRejectsContradictoryRawEvidence(t *testing.T) {
	fixture := newRTSPIntegrationFixture(t, rtspIntegrationConfig{sdp: rtspIntegrationMulticodecSDP(), packets: []rtspIntegrationPacket{{raw: rtspIntegrationRTP(96, 1, 0, 100, []byte{0x65}, true)}}, closeAfterPlay: true})
	service := rtspIntegrationOpen(t)
	detail := rtspIntegrationCapture(t, service, fixture.address(), ProtocolRTSP, 1)
	files := rtspIntegrationRoundTrip(t, service, detail)
	timeline, err := os.ReadFile(filepath.Join(service.runDir(detail.ID), timelineName))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"sdp", "channels", "swapped_channels", "crc", "timeline"} {
		t.Run(kind, func(t *testing.T) {
			entries := make(map[string][]byte, len(files))
			for name, data := range files {
				entries[name] = append([]byte(nil), data...)
			}
			if kind == "sdp" {
				entries[rtspSDPName] = bytes.Replace(entries[rtspSDPName], []byte("H264"), []byte("H265"), 1)
			}
			if kind == "channels" || kind == "swapped_channels" {
				var metadata rtspArchiveMetadata
				if err := json.Unmarshal(entries[rtspMetadataName], &metadata); err != nil {
					t.Fatal(err)
				}
				metadata.Channels[0].RTPChannel, metadata.Channels[0].RTCPChannel = 4, 5
				if kind == "swapped_channels" {
					metadata.Channels[0].RTPChannel, metadata.Channels[0].RTCPChannel = 2, 3
					metadata.Channels[1].RTPChannel, metadata.Channels[1].RTCPChannel = 0, 1
				}
				entries[rtspMetadataName], _ = json.Marshal(metadata)
			}
			var buffer bytes.Buffer
			archive := zip.NewWriter(&buffer)
			for _, name := range []string{rtspWireName, rtspSDPName, rtspMetadataName} {
				if kind == "crc" && name == rtspWireName {
					entry, err := archive.CreateRaw(&zip.FileHeader{Name: name, Method: zip.Store, CRC32: 1, CompressedSize64: uint64(len(entries[name])), UncompressedSize64: uint64(len(entries[name]))})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := entry.Write(entries[name]); err != nil {
						t.Fatal(err)
					}
				} else {
					entry, err := archive.Create(name)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := entry.Write(entries[name]); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, rtspStreamName), buffer.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			ledger := timeline
			if kind == "timeline" {
				ledger = []byte("{\"offset\":1,\"size\":1,\"arrival_ms\":0,\"connection_id\":1}\n")
			}
			if err := os.WriteFile(filepath.Join(dir, timelineName), ledger, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := replayRTSP(dir, detail.Report.Manifest); !errors.Is(err, ErrInvalid) {
				t.Fatalf("contradictory %s evidence accepted: %v", kind, err)
			}
		})
	}
}
