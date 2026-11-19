package hlsorigin

import (
	"strings"
	"testing"
)

func TestLivePlaylistTracksDiscontinuityAcrossFixtureWrap(t *testing.T) {
	withWrap := string(livePlaylist(13))
	if !strings.Contains(withWrap, "#EXT-X-DISCONTINUITY-SEQUENCE:0\n") ||
		!strings.Contains(withWrap, "#EXT-X-DISCONTINUITY\n#EXTINF:2.000,\nlive-13.ts") {
		t.Fatal(withWrap)
	}
	afterWrap := string(livePlaylist(15))
	if !strings.Contains(afterWrap, "#EXT-X-DISCONTINUITY-SEQUENCE:1\n") || strings.Contains(afterWrap, "#EXT-X-DISCONTINUITY\n") {
		t.Fatal(afterWrap)
	}
	if n, ok := liveResourceSequence("live-13.ts", 15); !ok || n != 13 {
		t.Fatalf("rolled segment lookup: %d %t", n, ok)
	}
	if _, ok := liveResourceSequence("live-8.ts", 15); ok {
		t.Fatal("expired segment was served")
	}
}
