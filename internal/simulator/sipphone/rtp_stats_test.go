package sipphone

import (
	"testing"
	"time"

	"github.com/pion/rtp"
)

func TestRTPTelemetryHandlesWrapAndLatePackets(t *testing.T) {
	var stats rtpStats
	start := time.Now()
	for i, seq := range []uint16{65534, 1, 0, 65535, 0} {
		stats.observe(rtp.Header{SSRC: 1, SequenceNumber: seq, Timestamp: uint32(i * 200)}, start.Add(time.Duration(i)*20*time.Millisecond))
		if i == 1 && stats.snapshot().SequenceGaps != 2 {
			t.Fatal("sequence wrap did not expose the two gaps")
		}
	}
	stats.drop()
	got := stats.snapshot()
	if got.SequenceGaps != 0 || got.Packets != 5 || got.FaultDrops != 1 || got.FirstAt != start ||
		got.LastAt != start.Add(80*time.Millisecond) || got.JitterMS <= 0 {
		t.Fatalf("unexpected telemetry: %+v", got)
	}
	stats.observe(rtp.Header{SSRC: 2, SequenceNumber: 300}, start.Add(time.Second))
	if got := stats.snapshot(); got.SequenceGaps != 0 || got.JitterMS != 0 {
		t.Fatalf("new SSRC must start a new sequence and jitter window: %+v", got)
	}
}
