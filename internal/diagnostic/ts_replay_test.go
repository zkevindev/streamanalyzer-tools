package diagnostic

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"streamanalyzer/internal/hlsdelivery/media"
)

func tsReplayFixture(t *testing.T, protocol string, chunks ...[]byte) (string, Manifest) {
	t.Helper()
	dir := t.TempDir()
	var raw, timeline bytes.Buffer
	enc := json.NewEncoder(&timeline)
	for i, chunk := range chunks {
		if err := enc.Encode(tsSpan{Offset: int64(raw.Len()), Size: len(chunk), ArrivalMS: int64(i + 1)}); err != nil {
			t.Fatal(err)
		}
		_, _ = raw.Write(chunk)
	}
	for name, body := range map[string][]byte{dumpName(protocol): raw.Bytes(), timelineName: timeline.Bytes()} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, Manifest{Protocol: protocol, Observation: Observation{ElapsedMS: int64(len(chunks)), EndReason: "eof"}, Stream: Artifact{Bytes: int64(raw.Len())}}
}

func tsTestPacket(pid uint16, cc byte, payload bool) []byte {
	packet := bytes.Repeat([]byte{0xff}, tsPacketBytes)
	packet[0], packet[1], packet[2] = 0x47, byte(pid>>8), byte(pid)
	if payload {
		packet[3] = 0x10 | cc
	} else {
		packet[3], packet[4], packet[5] = 0x20|cc, 183, 0
	}
	return packet
}

func testPSIWithCRC(packet []byte) []byte {
	copyPacket := slices.Clone(packet)
	payload := copyPacket[5+int(copyPacket[4]):]
	section := payload[1+int(payload[0]):]
	length := 3 + (int(section[1]&15) << 8) + int(section[2])
	binary.BigEndian.PutUint32(section[length-4:], tsPSICRC(section[:length-4]))
	return copyPacket
}

func TestTSReplayContinuityAndOfflineEvidence(t *testing.T) {
	standard := media.SyntheticMPEGTS(true)
	pat, pmt := testPSIWithCRC(standard[:188]), testPSIWithCRC(standard[188:376])
	first := slices.Clone(standard[376:])
	first[3] |= 14
	te := tsTestPacket(0x101, 1, true)
	te[1] |= 0x80
	discontinuity := tsTestPacket(0x101, 2, false)
	discontinuity[5] = 0x80
	bad := tsTestPacket(0x101, 5, false)
	bad[4] = 255
	payload := bytes.Join([][]byte{
		pat, pmt, first, first, first, tsTestPacket(0x101, 14, false),
		tsTestPacket(0x101, 15, true), tsTestPacket(0x101, 0, true), te,
		tsTestPacket(0x101, 9, true), discontinuity, tsTestPacket(0x101, 4, true), bad,
		[]byte{9, 8, 7}, tsTestPacket(0x101, 5, true), tsTestPacket(0x101, 6, true), tsTestPacket(0x101, 7, true),
	}, nil)
	dir, manifest := tsReplayFixture(t, ProtocolHTTPTS, payload[:191], payload[191:544], payload[544:])
	report, err := replayTS(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	metrics := report.MPEGTS
	if metrics.Packets != 16 || metrics.ContinuityErrors != 2 || metrics.Duplicates != 1 || metrics.MalformedPackets != 1 ||
		metrics.TransportErrors != 1 || metrics.PATSections != 1 || metrics.PMTSections != 1 ||
		metrics.SyncLossBytes != 3 || metrics.PCRPIDs != 1 || !slices.Equal(report.Metrics.VideoCodecs, []string{"h264"}) {
		t.Fatalf("wrong TS metrics: %+v", metrics)
	}
	for _, finding := range report.Findings {
		if finding.Code == "ts_transport_error" {
			if len(finding.Evidence) != 1 || finding.Evidence[0].Offset != 8*188 || finding.Evidence[0].PID == nil ||
				*finding.Evidence[0].PID != 0x101 || finding.Evidence[0].ArrivalMS != 3 {
				t.Fatalf("wrong TEI evidence: %+v", finding.Evidence)
			}
		}
		if finding.Code == "ts_continuity_error" && (finding.Occurrences != 2 || finding.NextCheck == "") {
			t.Fatalf("wrong continuity finding: %+v", finding)
		}
	}
	replayed, err := replayTS(dir, manifest)
	if err != nil || !reflect.DeepEqual(replayed, report) {
		t.Fatalf("offline replay differs: %v", err)
	}
}

func testRTPPacket(sequence uint16, extended bool) []byte {
	header := []byte{0x80, 33, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4}
	binary.BigEndian.PutUint16(header[2:], sequence)
	if extended {
		header[0] = 0xb1 // Padding, extension, and one CSRC.
		header = append(header, 1, 2, 3, 5, 0xbe, 0xde, 0, 1, 6, 7, 8, 9)
	}
	result := append(header, tsTestPacket(tsNullPID, 0, true)...)
	if extended {
		result = append(result, 0, 2)
	}
	return result
}

func TestTSReplayRTPWrapReorderAndGap(t *testing.T) {
	incorrectPT := testRTPPacket(5, false)
	incorrectPT[1] = 96
	chunks := [][]byte{
		testRTPPacket(65534, false), testRTPPacket(0, true), testRTPPacket(65535, false),
		testRTPPacket(65535, false), testRTPPacket(1, false), testRTPPacket(3, false),
		testRTPPacket(4, false), incorrectPT,
	}
	dir, manifest := tsReplayFixture(t, ProtocolRTPTS, chunks...)
	report, err := replayTS(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	metrics := report.MPEGTS
	if metrics.Packets != 6 || metrics.RTPDatagrams != 8 || metrics.RTPMissing != 1 || metrics.RTPOutOfOrder != 1 ||
		metrics.RTPDuplicates != 1 || metrics.RTPInvalid != 1 || metrics.TrailingBytes != 0 {
		t.Fatalf("wrong RTP metrics: %+v", metrics)
	}
	gap := slices.IndexFunc(report.Findings, func(f Finding) bool { return f.Code == "rtp_sequence_gap" })
	if gap < 0 {
		t.Fatal("missing RTP gap finding")
	}
	evidence := report.Findings[gap].Evidence
	offset := 0
	for _, chunk := range chunks[:5] {
		offset += len(chunk)
	}
	if len(evidence) != 2 || evidence[1].RTPSequence == nil || *evidence[1].RTPSequence != 3 || evidence[1].Offset != int64(offset) {
		t.Fatalf("wrong RTP gap evidence: %+v", evidence)
	}
}

func TestTSReplayKeepsEarlyPacketsBeforeResync(t *testing.T) {
	null := tsTestPacket(tsNullPID, 0, true)
	body := bytes.Join([][]byte{null, null, {1, 2, 3}, null, null, null}, nil)
	dir, manifest := tsReplayFixture(t, ProtocolHTTPTS, body)
	report, err := replayTS(dir, manifest)
	if err != nil || report.MPEGTS.Packets != 5 || report.MPEGTS.SyncLossBytes != 3 {
		t.Fatalf("packets before resync were skipped: %+v, %v", report.MPEGTS, err)
	}
}

func TestTSReplayRecognizesSinglePacket204Datagram(t *testing.T) {
	packet := append(tsTestPacket(tsNullPID, 0, true), make([]byte, 16)...)
	dir, manifest := tsReplayFixture(t, ProtocolUDPTS, packet)
	report, err := replayTS(dir, manifest)
	if err != nil || report.MPEGTS.Packets != 1 || report.MPEGTS.PacketSize != 204 || report.MPEGTS.SyncLossBytes != 0 {
		t.Fatalf("RS envelope was misread: %+v, %v", report.MPEGTS, err)
	}
}
