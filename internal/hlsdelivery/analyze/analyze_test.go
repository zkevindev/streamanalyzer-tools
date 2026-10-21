package analyze

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/hlsdelivery/m3u8"
	"streamanalyzer/internal/hlsdelivery/media"
)

func TestAnalyzeMaster(t *testing.T) {
	srv := testHLSServer(t)
	defer srv.Close()
	rep, err := Analyze(context.Background(), srv.URL+"/master.m3u8", Options{
		Timeout:       2 * time.Second,
		Duration:      5 * time.Second,
		Segments:      4,
		ParseSegments: true,
		VariantSet:    true,
		Variant:       -1,
		Headers:       http.Header{"Authorization": []string{"Bearer test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ExitCode != 0 {
		t.Fatalf("exit %d, recs %v", rep.ExitCode, rep.Recommendations)
	}
	if rep.Variant == nil || rep.Variant.Bandwidth != 3000000 || rep.Variant.Resolution != "1280x720" {
		t.Fatalf("variant: %+v", rep.Variant)
	}
	if rep.Summary.SegmentsFetched != 3 || rep.Summary.SegmentsFailed != 1 {
		t.Fatalf("counts: %+v", rep.Summary)
	}
	if rep.Summary.ErrorRatePct != 25 || rep.Summary.ErrorBreakdown.HTTP5xx != 1 {
		t.Fatalf("errors: %+v", rep.Summary)
	}
	if rep.Summary.CacheHitRatePct == nil || *rep.Summary.CacheHitRatePct < 60 || *rep.Summary.CacheHitRatePct >= 80 {
		t.Fatalf("cache: %v", rep.Summary.CacheHitRatePct)
	}
	if rep.Infrastructure.CDNDetected != "Cloudflare" {
		t.Fatalf("cdn: %s", rep.Infrastructure.CDNDetected)
	}
	if rep.ABR == nil || !strings.Contains(rep.ABR.Recommended, "1280x720") {
		t.Fatalf("abr: %+v", rep.ABR)
	}
	var keyframes int
	for _, seg := range rep.Segments {
		if seg.StartsWithKeyframe != nil && *seg.StartsWithKeyframe {
			keyframes++
		}
	}
	if keyframes != 3 {
		t.Fatalf("keyframes %d, segments %+v", keyframes, rep.Segments)
	}
	joined := strings.Join(rep.Recommendations, "\n")
	if !strings.Contains(joined, "命中率") {
		t.Fatalf("recommendations: %s", joined)
	}

	low, err := Analyze(context.Background(), srv.URL+"/master.m3u8", Options{
		Timeout: 2 * time.Second, Duration: 5 * time.Second, Segments: 4,
		VariantSet: true, Variant: 0,
		Headers: http.Header{"Authorization": []string{"Bearer test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if low.Variant == nil || low.Variant.Bandwidth != 800000 || low.Summary.SegmentsFetched != 1 {
		t.Fatalf("low: %+v summary %+v", low.Variant, low.Summary)
	}
}

func TestManifestAndEmpty(t *testing.T) {
	srv := testHLSServer(t)
	defer srv.Close()
	rep, err := Manifest(context.Background(), srv.URL+"/master.m3u8", Options{
		Headers: http.Header{"Authorization": []string{"Bearer test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Variants) != 2 || rep.Playlist.SegmentCount != 0 {
		t.Fatalf("manifest: %+v", rep)
	}
	_, err = Analyze(context.Background(), srv.URL+"/empty.m3u8", Options{
		Headers: http.Header{"Authorization": []string{"Bearer test"}},
	})
	if exitCode(err) != 6 {
		t.Fatalf("empty: %v", err)
	}
	_, err = Analyze(context.Background(), srv.URL+"/missing.m3u8", Options{
		Headers: http.Header{"Authorization": []string{"Bearer test"}},
	})
	if exitCode(err) != 5 {
		t.Fatalf("missing: %v", err)
	}
}

func TestThresholdsAndCompare(t *testing.T) {
	srv := testHLSServer(t)
	defer srv.Close()
	opt := Options{
		Timeout: 2 * time.Second, Duration: 5 * time.Second, Segments: 4,
		ErrorThreshold: 10,
		Headers:        http.Header{"Authorization": []string{"Bearer test"}},
	}
	rep, err := Analyze(context.Background(), srv.URL+"/hi.m3u8", opt)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ExitCode != 2 {
		t.Fatalf("exit %d", rep.ExitCode)
	}
	cmp, err := Compare(context.Background(), srv.URL+"/low.m3u8", srv.URL+"/hi.m3u8", Options{
		Timeout: 2 * time.Second, Duration: 5 * time.Second, Segments: 4,
		Headers: http.Header{"Authorization": []string{"Bearer test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.Notes) < 2 || cmp.A.Summary.SegmentsFetched != 1 {
		t.Fatalf("compare: %+v", cmp.Notes)
	}
}

func TestMonitor(t *testing.T) {
	srv := testHLSServer(t)
	defer srv.Close()
	rep, err := Monitor(context.Background(), srv.URL+"/live.m3u8", Options{
		Timeout: 2 * time.Second, Duration: time.Second, Segments: 5,
		Headers: http.Header{"Authorization": []string{"Bearer test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.SegmentsFetched != 2 {
		t.Fatalf("fetched %d", rep.Summary.SegmentsFetched)
	}
	if rep.LiveLatencyMS == nil || *rep.LiveLatencyMS < 2900 || *rep.LiveLatencyMS > 3100 {
		t.Fatalf("latency %v", rep.LiveLatencyMS)
	}
}

func TestChooseAndABR(t *testing.T) {
	pl := &m3u8.Playlist{Segments: []m3u8.Segment{{Sequence: 0}, {Sequence: 1}, {Sequence: 2}, {Sequence: 3}, {Sequence: 4}}}
	tail := chooseSegments(pl, 2)
	if tail[0].Sequence != 3 || tail[1].Sequence != 4 {
		t.Fatalf("tail: %+v", tail)
	}
	pl.EndList = true
	head := chooseSegments(pl, 2)
	if head[0].Sequence != 0 || head[1].Sequence != 1 {
		t.Fatalf("head: %+v", head)
	}
	abr := buildABR([]m3u8.Variant{
		{Bandwidth: 2493000, Resolution: "1280x720"},
		{Bandwidth: 4712000, Resolution: "1920x1080"},
	}, 57822, 9300)
	if abr.Recommended == "" || !strings.Contains(abr.Recommended, "1920x1080") {
		t.Fatalf("recommended: %+v", abr)
	}
	if !strings.Contains(abr.SafeFallback, "1280x720") {
		t.Fatalf("safe: %+v", abr)
	}
	if abr.Variants[0].Risk != "NONE" || abr.Variants[1].Risk != "MEDIUM" {
		t.Fatalf("risk: %+v", abr.Variants)
	}
}

func TestPTSContinuityFrameGap(t *testing.T) {
	a0, a1 := int64(0), int64(270000)
	b0, b1 := int64(273000), int64(543000)
	segs := []SegmentResult{
		{PTSStart: &a0, PTSEnd: &a1},
		{PTSStart: &b0, PTSEnd: &b1},
	}
	if got := ptsContinuity(segs); got != "OK" {
		t.Fatalf("frame gap: %q", got)
	}
	jump := b0 + 90000*2
	segs[1].PTSStart = &jump
	if got := ptsContinuity(segs); got != "ISSUES" {
		t.Fatalf("jump: %q", got)
	}
	if got := ptsContinuity(nil); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if got := ptsContinuity(segs[:1]); got != "" {
		t.Fatalf("single: %q", got)
	}
	missing := int64(-1)
	unknown := []SegmentResult{{PTSStart: &missing, PTSEnd: &missing}, {PTSStart: &missing, PTSEnd: &missing}}
	if got := ptsContinuity(unknown); got != "" {
		t.Fatalf("no pts: %q", got)
	}
}

func TestSummaryOmitsUnknownCacheHit(t *testing.T) {
	view := summaryView(Summary{})
	if _, ok := view["cache_hit_rate_pct"]; ok {
		t.Fatalf("unknown rate present: %#v", view["cache_hit_rate_pct"])
	}
	if _, ok := view["pts_continuity"]; ok {
		t.Fatalf("unknown continuity present: %#v", view["pts_continuity"])
	}
	rate := 12.5
	view = summaryView(Summary{CacheHitRatePct: &rate, PTSContinuity: "OK"})
	if view["cache_hit_rate_pct"] != 12.5 || view["pts_continuity"] != "OK" {
		t.Fatalf("%#v", view)
	}
}

func TestAnalyzeUnknownCacheHit(t *testing.T) {
	srv := testHLSServer(t)
	defer srv.Close()
	rep, err := Analyze(context.Background(), srv.URL+"/live.m3u8", Options{
		Timeout:    2 * time.Second,
		Duration:   5 * time.Second,
		Segments:   2,
		VariantSet: true,
		Variant:    -1,
		Headers:    http.Header{"Authorization": []string{"Bearer test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.CacheHitRatePct != nil {
		t.Fatalf("cache: %v", *rep.Summary.CacheHitRatePct)
	}
	if rep.Summary.PTSContinuity != "" {
		t.Fatalf("pts: %q", rep.Summary.PTSContinuity)
	}
	view := summaryView(rep.Summary)
	if _, ok := view["cache_hit_rate_pct"]; ok {
		t.Fatalf("view cache: %#v", view["cache_hit_rate_pct"])
	}
}

func TestCDNHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Server", "cloudflare")
	h.Set("Cf-Cache-Status", "HIT")
	h.Set("Age", "12")
	if DetectCDN(h) != "Cloudflare" {
		t.Fatal(DetectCDN(h))
	}
	status, age := CacheInfo(h)
	if status != "HIT" || age == nil || *age != 12 {
		t.Fatalf("%s %v", status, age)
	}
	h.Set("X-Cache", "Miss from cloudfront")
	h.Set("X-Amz-Cf-Pop", "LAX")
	if DetectCDN(h) != "Cloudflare" {
		t.Fatal("server cloudflare should win before x-cache")
	}
	if got := normalizeCache("Hit from cloudfront"); got != "HIT" {
		t.Fatal(got)
	}
	fastly := http.Header{}
	fastly.Set("X-Served-By", "cache-sin-wsap440089-SIN")
	if got := edgeLocation(fastly); got != "sin" {
		t.Fatalf("edge %q", got)
	}
	if got := edgeLocation(h); got != "lax" {
		t.Fatalf("cf pop %q", got)
	}
}

func testHLSServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := media.SyntheticMPEGTS(true)
	ts = append(ts, bytes.Repeat([]byte{0}, 64*1024)...)
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte(`#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360,CODECS="avc1.42e01e"
low.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1280x720,CODECS="avc1.4d401f"
hi.m3u8
`))
	})
	mux.HandleFunc("/hi.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:8
#EXTINF:4.000,
seg0.ts
#EXTINF:4.000,
seg1.ts
#EXTINF:4.000,
seg2.ts
#EXTINF:4.000,
seg3.ts
#EXT-X-ENDLIST
`))
	})
	mux.HandleFunc("/low.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\nlow0.ts\n#EXT-X-ENDLIST\n"))
	})
	mux.HandleFunc("/empty.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:4\n"))
	})
	mux.HandleFunc("/live.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:5\n#EXTINF:1.0,\na.ts\n#EXTINF:1.0,\nb.ts\n"))
	})
	writeSeg := func(status int, cache string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !auth(w, r) {
				return
			}
			if cache != "" {
				w.Header().Set("Cf-Cache-Status", cache)
				w.Header().Set("Cf-Ray", "abc-SJC")
				w.Header().Set("Age", "3")
			}
			w.WriteHeader(status)
			if status < 400 {
				_, _ = w.Write(ts)
			}
		}
	}
	mux.HandleFunc("/seg0.ts", writeSeg(200, "HIT"))
	mux.HandleFunc("/seg1.ts", writeSeg(200, "HIT"))
	mux.HandleFunc("/seg2.ts", writeSeg(500, ""))
	mux.HandleFunc("/seg3.ts", writeSeg(200, "MISS"))
	mux.HandleFunc("/low0.ts", writeSeg(200, "HIT"))
	mux.HandleFunc("/a.ts", writeSeg(200, ""))
	mux.HandleFunc("/b.ts", writeSeg(200, ""))
	return httptest.NewServer(mux)
}

func exitCode(err error) int {
	var ee *ExitError
	if err != nil && errorAs(err, &ee) {
		return ee.Code
	}
	if err != nil {
		return 1
	}
	return 0
}

func errorAs(err error, target **ExitError) bool {
	for err != nil {
		if ee, ok := err.(*ExitError); ok {
			*target = ee
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
