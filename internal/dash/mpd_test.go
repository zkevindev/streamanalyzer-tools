package dash

import (
	"strings"
	"testing"
)

const sampleDynamicMPD = `<?xml version="1.0" encoding="UTF-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011"
     type="dynamic"
     profiles="urn:mpeg:dash:profile:isoff-live:2011"
     minBufferTime="PT2S"
     minimumUpdatePeriod="PT4S"
     availabilityStartTime="2026-01-01T00:00:00Z">
  <BaseURL>https://media.example.test/live/</BaseURL>
  <Period id="p0" start="PT0S">
    <AdaptationSet id="1" contentType="video" mimeType="video/mp4" segmentAlignment="true" frameRate="30">
      <ContentProtection schemeIdUri="urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed" value="Widevine"/>
      <SegmentTemplate timescale="1000" initialization="init-$RepresentationID$.m4s" media="seg-$RepresentationID$-$Time$.m4s" startNumber="100">
        <SegmentTimeline>
          <S t="10000" d="2000" r="1"/>
          <S t="16000" d="2000"/>
        </SegmentTimeline>
      </SegmentTemplate>
      <Representation id="v1080" bandwidth="3500000" width="1920" height="1080" codecs="avc1.640028"/>
      <Representation id="v720" bandwidth="1500000" width="1280" height="720" codecs="avc1.4d401f"/>
    </AdaptationSet>
    <AdaptationSet id="2" contentType="audio" mimeType="audio/mp4" lang="zh">
      <SegmentTemplate timescale="48000" duration="96000" initialization="init-$RepresentationID$.m4s" media="seg-$RepresentationID$-$Number%04d$.m4s" startNumber="1"/>
      <Representation id="a128" bandwidth="128000" codecs="mp4a.40.2" audioSamplingRate="48000"/>
    </AdaptationSet>
  </Period>
</MPD>`

func TestLooksLikeMPDAndParse(t *testing.T) {
	raw := []byte(sampleDynamicMPD)
	if !LooksLikeMPD(raw) {
		t.Fatalf("expected LooksLikeMPD to return true")
	}
	if LooksLikeMPD([]byte("not xml at all")) {
		t.Fatalf("expected LooksLikeMPD to return false for plain text")
	}

	mpd, err := ParseMPD(raw)
	if err != nil {
		t.Fatalf("ParseMPD failed: %v", err)
	}
	if mpd.Type != "dynamic" {
		t.Fatalf("unexpected type: %q", mpd.Type)
	}
	if mpd.MinBufferTimeMS != 2000 || mpd.MinimumUpdatePeriodMS != 4000 {
		t.Fatalf("unexpected durations: minBuffer=%d update=%d", mpd.MinBufferTimeMS, mpd.MinimumUpdatePeriodMS)
	}
	if len(mpd.Periods) != 1 || len(mpd.Periods[0].AdaptationSets) != 2 {
		t.Fatalf("unexpected periods/adaptation sets: %+v", mpd.Periods)
	}

	videoAS := mpd.Periods[0].AdaptationSets[0]
	if len(videoAS.DRM) != 1 || videoAS.DRM[0] != "Widevine" {
		t.Fatalf("expected Widevine DRM, got %+v", videoAS.DRM)
	}
	vRep := videoAS.Representations[0]
	docURL := "http://example.test/manifest.mpd"
	initURL := ResolveInitURL(docURL, mpd, mpd.Periods[0], videoAS, vRep)
	if initURL != "https://media.example.test/live/init-v1080.m4s" {
		t.Fatalf("unexpected init URL: %s", initURL)
	}

	segs, gaps := ExpandMediaSegments(docURL, mpd, mpd.Periods[0], videoAS, vRep, 8)
	if len(segs) != 3 {
		t.Fatalf("expected 3 timeline segments, got %d", len(segs))
	}
	if gaps != 1 {
		t.Fatalf("expected 1 timeline gap (14000 -> 16000), got %d", gaps)
	}
	if segs[2].GapMS != 2000 {
		t.Fatalf("expected 2000ms gap on segment 2, got %d", segs[2].GapMS)
	}
	if !strings.HasSuffix(segs[0].URL, "/live/seg-v1080-10000.m4s") {
		t.Fatalf("unexpected segment 0 URL: %s", segs[0].URL)
	}

	audioAS := mpd.Periods[0].AdaptationSets[1]
	aRep := audioAS.Representations[0]
	aSegs, aGaps := ExpandMediaSegments(docURL, mpd, mpd.Periods[0], audioAS, aRep, 3)
	if aGaps != 0 || len(aSegs) != 3 {
		t.Fatalf("unexpected audio segments: len=%d gaps=%d", len(aSegs), aGaps)
	}
	if !strings.HasSuffix(aSegs[0].URL, "/live/seg-a128-0001.m4s") {
		t.Fatalf("unexpected formatted number URL: %s", aSegs[0].URL)
	}
}

func TestParseISO8601DurationMS(t *testing.T) {
	cases := map[string]int64{
		"PT2S":       2000,
		"PT1M30.5S":  90500,
		"PT1H2M3S":   3723000,
		"P1DT0H0M1S": 86401000,
		"invalid":    0,
		"":           0,
	}
	for in, want := range cases {
		if got := ParseISO8601DurationMS(in); got != want {
			t.Errorf("ParseISO8601DurationMS(%q) = %d, want %d", in, got, want)
		}
	}
}
