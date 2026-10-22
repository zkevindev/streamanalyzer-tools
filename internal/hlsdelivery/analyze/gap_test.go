package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeclaredGapsAreSkipped(t *testing.T) {
	for _, command := range []string{"analyze", "monitor"} {
		for _, onlyGaps := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/all_gaps=%v", command, onlyGaps), func(t *testing.T) {
				var gapRequests, mediaRequests atomic.Int32
				playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n"
				if !onlyGaps {
					playlist += "#EXTINF:4,\na.ts\n"
				}
				playlist += "#EXTINF:4,\n#EXT-X-GAP\ngap.ts\n"
				if !onlyGaps {
					playlist += "#EXTINF:4,\nb.ts\n"
				}
				if command == "analyze" {
					playlist += "#EXT-X-ENDLIST\n"
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/index.m3u8":
						_, _ = fmt.Fprint(w, playlist)
					case "/gap.ts":
						gapRequests.Add(1)
						http.NotFound(w, r)
					default:
						mediaRequests.Add(1)
						w.Header().Set("X-Cache", "HIT")
						_, _ = fmt.Fprint(w, "segment")
					}
				}))
				defer srv.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				wantFetched, wantResults := 2, 3
				if onlyGaps {
					wantFetched, wantResults = 0, 1
				}
				observed := 0
				opt := Options{Segments: wantResults, Duration: time.Second, ErrorThreshold: 1,
					OnSegment: func(SegmentResult) {
						observed++
						if command == "monitor" && observed == wantResults {
							cancel()
						}
					},
				}
				analyze := Analyze
				if command == "monitor" {
					analyze = Monitor
				}
				rep, err := analyze(ctx, srv.URL+"/index.m3u8", opt)
				if err != nil {
					t.Fatal(err)
				}
				if gapRequests.Load() != 0 || int(mediaRequests.Load()) != wantFetched {
					t.Fatalf("requests: gap=%d media=%d", gapRequests.Load(), mediaRequests.Load())
				}
				if rep.ExitCode != 0 || rep.Summary.SegmentsFailed != 0 || rep.Summary.ErrorRatePct != 0 ||
					rep.Summary.SegmentsFetched != wantFetched || rep.Summary.SegmentsSkipped != 1 {
					t.Fatalf("exit=%d summary=%+v", rep.ExitCode, rep.Summary)
				}
				if rep.Summary.ErrorBreakdown != (ErrorBreakdown{}) {
					t.Fatalf("gap counted as request failure: %+v", rep.Summary.ErrorBreakdown)
				}
				if !onlyGaps && (rep.Summary.CacheHitRatePct == nil || *rep.Summary.CacheHitRatePct != 100) {
					t.Fatalf("gap changed cache hit rate: %+v", rep.Summary)
				}
				raw, err := json.Marshal(ReportValue(rep))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(raw), `"gap":true`) || !strings.Contains(string(raw), `"segments_skipped":1`) {
					t.Fatalf("gap metadata missing from JSON: %s", raw)
				}
			})
		}
	}
}
