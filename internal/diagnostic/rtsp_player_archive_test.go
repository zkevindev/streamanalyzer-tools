package diagnostic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRTSPPreviewLegacyNativeArchiveVersionsRemainUnchanged(t *testing.T) {
	for _, transport := range []string{"tcp", "udp"} {
		t.Run(transport, func(t *testing.T) {
			var report Report
			var err error
			dir := t.TempDir()
			packet := rtspIntegrationRTP(96, 0, 0, 123, []byte{0x65, 0x88, 0x84}, true)
			if transport == "udp" {
				target, _ := rtspUDPTestServer(t, []rtspUDPTestPacket{{data: packet}}, false)
				report, err = sampleRTSPTransport(context.Background(), dir, target, 1, ProtocolRTSP, "udp")
			} else {
				fixture := newRTSPIntegrationFixture(t, rtspIntegrationConfig{packets: []rtspIntegrationPacket{{raw: packet}}, closeAfterPlay: true})
				target := fixture.address()
				parsed, _ := parseProtocolURL(target, ProtocolRTSP)
				report, err = sampleRTSP(context.Background(), dir, parsed, 1, ProtocolRTSP)
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, rtspStreamName))
			if err != nil {
				t.Fatal(err)
			}
			files := rtspIntegrationZIP(t, raw)
			var metadata rtspArchiveMetadata
			if err := json.Unmarshal(files[rtspMetadataName], &metadata); err != nil {
				t.Fatal(err)
			}
			wantVersion, wantEntries, wantRule := 1, 3, "rtsp/1"
			if transport == "udp" {
				wantVersion, wantEntries, wantRule = 2, 5, "rtsp/2"
			}
			if metadata.Version != wantVersion || len(files) != wantEntries || report.RuleVersion != wantRule || report.RTSP.Preview != nil || report.RTSP.RTPPackets != 1 || report.Metrics.VideoFrames != 0 || report.Metrics.FirstVideoMS != nil {
				t.Fatalf("legacy capture/report shape changed: metadata=%+v report=%+v", metadata, report)
			}
			beforeJSON, _ := json.Marshal(report)
			again, err := replayRTSP(dir, report.Manifest)
			afterJSON, _ := json.Marshal(again)
			if err != nil || !reflect.DeepEqual(report, again) || !bytes.Equal(beforeJSON, afterJSON) {
				t.Fatalf("legacy replay changed: %v", err)
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(files[rtspMetadataName], &fields)
			if _, exists := fields["preview"]; exists {
				t.Fatal("legacy archive gained a preview field")
			}
			fields["preview"] = json.RawMessage("null")
			files[rtspMetadataName], _ = json.Marshal(fields)
			rtspUDPTestWriteArchive(t, dir, files)
			if _, err := replayRTSP(dir, report.Manifest); !errors.Is(err, ErrInvalid) {
				t.Fatalf("legacy version accepted new preview field presence: %v", err)
			}
		})
	}
}
