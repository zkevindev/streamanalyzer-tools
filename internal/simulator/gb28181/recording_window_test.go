package gb28181

import (
	"testing"
	"time"
)

func TestRecordingWindowSeekStartsAtNextAvailablePart(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	file := localMedia{duration: 2 * time.Second}
	if err := file.setRecording(Config{RecordingStart: base.Format(time.RFC3339), RecordingCount: 2, RecordingGap: 4}, base); err != nil {
		t.Fatal(err)
	}
	offer := mediaOffer{start: base.Unix(), end: base.Add(8 * time.Second).Unix()}
	window, err := file.playbackRange(&offer)
	if err != nil {
		t.Fatal(err)
	}
	if got := window.selectParts(3*time.Second, 5*time.Second); len(got) != 0 {
		t.Fatalf("a seek entirely in the gap has no footage: %+v", got)
	}
	got := window.selectParts(3*time.Second, 7*time.Second)
	if len(got) != 1 || got[0].recordIndex != 2 || !got[0].start.Equal(base.Add(6*time.Second)) || !got[0].end.Equal(base.Add(7*time.Second)) {
		t.Fatalf("seek across missing footage should use the next recording: %+v", got)
	}
}
