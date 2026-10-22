package m3u8

import (
	"fmt"
	"testing"
	"time"
)

func TestParseMedia(t *testing.T) {
	text := `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:10
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MAP:URI="init.mp4"
#EXT-X-KEY:METHOD=AES-128,URI="key.key",IV=0x0000000000000000000000000000000A
#EXT-X-PROGRAM-DATE-TIME:2026-10-21T09:00:00.000Z
#EXTINF:4.000,title
#EXT-X-BYTERANGE:1000@200
seg0.cmfv
#EXTINF:4.000,
seg1.cmfv
#EXT-X-DISCONTINUITY
#EXTINF:3.500,
seg2.cmfv
#EXT-X-ENDLIST
`
	pl, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if pl.IsMaster() || !pl.EndList || !pl.IndependentSegments || pl.Version != 7 {
		t.Fatalf("playlist meta: %+v", pl)
	}
	if pl.TargetDuration != 4 || pl.MediaSequence != 10 || pl.Type != "VOD" {
		t.Fatalf("timing meta: %+v", pl)
	}
	if pl.Map == nil || pl.Map.URI != "init.mp4" {
		t.Fatalf("map: %+v", pl.Map)
	}
	if len(pl.Segments) != 3 {
		t.Fatalf("segments: %d", len(pl.Segments))
	}
	s0 := pl.Segments[0]
	if s0.Sequence != 10 || s0.Title != "title" || s0.ByteRange == nil || s0.ByteRange.Offset != 200 || s0.ByteRange.Length != 1000 {
		t.Fatalf("seg0: %+v", s0)
	}
	if s0.Key == nil || s0.Key.Method != "AES-128" || len(s0.Key.IV) != 16 || s0.Key.IV[15] != 0x0A {
		t.Fatalf("key: %+v", s0.Key)
	}
	want := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	if !s0.ProgramDateTime.Equal(want) {
		t.Fatalf("pdt: %s", s0.ProgramDateTime)
	}
	if !pl.Segments[1].HasTime || !pl.Segments[1].ProgramDateTime.Equal(want.Add(4*time.Second)) {
		t.Fatalf("inferred time: %s", pl.Segments[1].ProgramDateTime)
	}
	if pl.Segments[2].HasTime || !pl.Segments[2].Discontinuity || pl.Segments[2].Sequence != 12 {
		t.Fatalf("seg2: %+v", pl.Segments[2])
	}
}

func TestSegmentMetadataBoundAtURI(t *testing.T) {
	for _, afterEXTINF := range []bool{false, true} {
		for _, explicitTime := range []bool{false, true} {
			t.Run(fmt.Sprintf("after_EXTINF=%v/explicit_time=%v", afterEXTINF, explicitTime), func(t *testing.T) {
				text := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-KEY:METHOD=AES-128,URI=old.key\n#EXT-X-MAP:URI=old.mp4\n" +
					"#EXT-X-PROGRAM-DATE-TIME:2026-10-21T09:00:00Z\n#EXTINF:4,first\na.m4s\n"
				tags := "#EXT-X-DISCONTINUITY\n#EXT-X-KEY:METHOD=AES-128,URI=new.key\n#EXT-X-MAP:URI=new.mp4\n"
				if explicitTime {
					// Explicit time belongs to this segment regardless of its order relative to discontinuity.
					tags = "#EXT-X-PROGRAM-DATE-TIME:2026-10-21T10:00:00Z\n" + tags
				}
				if afterEXTINF {
					text += "#EXTINF:4,changed\n" + tags
				} else {
					text += tags + "#EXTINF:4,changed\n"
				}
				text += "b.m4s\n#EXTINF:4,inherited\nc.m4s\n#EXTINF:4,clear key\n#EXT-X-KEY:METHOD=NONE\n#EXT-X-MAP:URI=last.mp4\nd.m4s\n"
				pl, err := Parse(text)
				if err != nil {
					t.Fatal(err)
				}
				if len(pl.Segments) != 4 {
					t.Fatalf("segments=%+v", pl.Segments)
				}
				a, b, c, d := pl.Segments[0], pl.Segments[1], pl.Segments[2], pl.Segments[3]
				if a.Key == nil || a.Key.URI != "old.key" || a.Map == nil || a.Map.URI != "old.mp4" || a.Discontinuity {
					t.Fatalf("earlier segment changed: %+v", a)
				}
				if b.Key == nil || b.Key.URI != "new.key" || b.Map == nil || b.Map.URI != "new.mp4" || !b.Discontinuity {
					t.Fatalf("new metadata not bound to segment URI: %+v", b)
				}
				if c.Key != b.Key || c.Map != b.Map || c.Discontinuity || c.HasTime != explicitTime || b.HasTime != explicitTime {
					t.Fatalf("incorrect metadata inheritance: b=%+v c=%+v", b, c)
				}
				if explicitTime && !c.ProgramDateTime.Equal(b.ProgramDateTime.Add(4*time.Second)) {
					t.Fatalf("explicit program time lost: b=%+v c=%+v", b, c)
				}
				if d.Key != nil || d.Map == nil || d.Map.URI != "last.mp4" || d.Discontinuity {
					t.Fatalf("METHOD=NONE or map change shifted: %+v", d)
				}
				if pl.Map != a.Map {
					t.Fatalf("playlist init differs from first segment: %+v", pl.Map)
				}
			})
		}
	}
}

func TestParseMaster(t *testing.T) {
	text := `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-STREAM-INF:BANDWIDTH=2493000,RESOLUTION=1280x720,CODECS="avc1.4d401f,mp4a.40.2",FRAME-RATE=30
720.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=4712000,AVERAGE-BANDWIDTH=4500000,RESOLUTION=1920x1080,NAME="1080p"
1080.m3u8
#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=200000,URI="iframe.m3u8",RESOLUTION=1920x1080
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aac",NAME="en",URI="audio.m3u8"
`
	pl, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if !pl.IsMaster() || len(pl.Variants) != 3 {
		t.Fatalf("variants: %+v", pl.Variants)
	}
	if pl.Variants[0].Width != 1280 || pl.Variants[0].Codecs == "" || pl.Variants[0].FrameRate != 30 {
		t.Fatalf("720: %+v", pl.Variants[0])
	}
	if pl.Variants[1].Name != "1080p" || pl.Variants[1].AverageBandwidth != 4500000 {
		t.Fatalf("1080: %+v", pl.Variants[1])
	}
	if !pl.Variants[2].IFrame || pl.Variants[2].URI != "iframe.m3u8" {
		t.Fatalf("iframe: %+v", pl.Variants[2])
	}
	if len(pl.Renditions) != 1 || pl.Renditions[0].URI != "audio.m3u8" {
		t.Fatalf("renditions: %+v", pl.Renditions)
	}
}

func TestParseRejectsHTML(t *testing.T) {
	if _, err := Parse("<html></html>"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseGap(t *testing.T) {
	for _, declaration := range []string{
		"#EXT-X-GAP\n#EXTINF:4,\n",
		"#EXTINF:4,\n#EXT-X-GAP\n",
	} {
		pl, err := Parse("#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:10\n#EXT-X-PROGRAM-DATE-TIME:2026-10-21T09:00:00Z\n" +
			"#EXTINF:4,\na.ts\n" + declaration + "gap.ts\n#EXTINF:4,\nc.ts\n")
		if err != nil {
			t.Fatal(err)
		}
		if len(pl.Segments) != 3 || pl.Segments[0].Gap || !pl.Segments[1].Gap || pl.Segments[2].Gap {
			t.Fatalf("gap markers: %+v", pl.Segments)
		}
		if pl.Segments[1].Sequence != 11 || pl.Segments[2].Sequence != 12 {
			t.Fatalf("gap changed media sequence: %+v", pl.Segments)
		}
		if !pl.Segments[2].ProgramDateTime.Equal(pl.Segments[0].ProgramDateTime.Add(8 * time.Second)) {
			t.Fatalf("gap removed declared time: %+v", pl.Segments)
		}
	}
}
