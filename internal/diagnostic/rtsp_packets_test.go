package diagnostic

import (
	"bytes"
	"os"
	"testing"
)

func TestBuildAndValidateRTSPPacketReport(t *testing.T) {
	data, err := os.ReadFile("testdata/rtsp-v1-sequence.zip")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	report, err := readBundle(dir, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	packets, err := buildRTSPPacketReport(dir, report)
	if err != nil {
		t.Fatalf("buildRTSPPacketReport failed: %v", err)
	}
	if packets == nil {
		t.Fatal("expected non-nil RTSPPacketReport")
	}
	if len(packets.Tracks) != 1 {
		t.Fatalf("expected 1 track summary, got %d", len(packets.Tracks))
	}
	track := packets.Tracks[0]
	if track.SSRC == nil || *track.SSRC != 200 {
		t.Fatalf("expected final SSRC 200, got %v", track.SSRC)
	}
	if track.FirstSequence == nil || *track.FirstSequence != 65534 || track.LastSequence == nil || *track.LastSequence != 100 {
		t.Fatalf("unexpected sequence range: %v -> %v", track.FirstSequence, track.LastSequence)
	}
	if track.FirstRTPTimestamp == nil || *track.FirstRTPTimestamp != 0 || track.LastRTPTimestamp == nil || *track.LastRTPTimestamp != 25200 {
		t.Fatalf("unexpected timestamp range: %v -> %v", track.FirstRTPTimestamp, track.LastRTPTimestamp)
	}
	if len(packets.Samples) != 8 {
		t.Fatalf("expected 8 packet samples, got %d", len(packets.Samples))
	}
	anomalies := map[string]int{}
	for _, s := range packets.Samples {
		if s.Anomaly != "" {
			anomalies[s.Anomaly]++
		}
	}
	for _, want := range []string{"seq_gap", "out_of_order", "duplicate", "ssrc_change"} {
		if anomalies[want] == 0 {
			t.Errorf("expected anomaly %q in packet samples, got %+v", want, anomalies)
		}
	}

	stored := report
	stored.RTSPPackets = packets
	validated, err := validateStoredRTSPPackets(dir, stored, report)
	if err != nil || validated == nil {
		t.Fatalf("expected valid stored RTSPPackets, got err=%v", err)
	}

	tampered := *packets
	tampered.Samples = append([]RTSPPacketSample(nil), packets.Samples...)
	tampered.Samples[0].Sequence++
	stored.RTSPPackets = &tampered
	if _, err := validateStoredRTSPPackets(dir, stored, report); err == nil {
		t.Fatal("expected validateStoredRTSPPackets to reject tampered sequence number")
	}
}
