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

func TestAnalyzeInitFetchFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failure   string
		parse     bool
		wantFail  int
		wantInits int
		breakdown ErrorBreakdown
	}{
		{"not found", "404", true, 2, 2, ErrorBreakdown{HTTP4xx: 2}},
		{"server error", "503", true, 2, 2, ErrorBreakdown{HTTP5xx: 2}},
		{"timeout", "timeout", true, 2, 2, ErrorBreakdown{Timeouts: 2}},
		{"empty init", "empty", true, 2, 2, ErrorBreakdown{Other: 2}},
		{"ignored init range", "range", true, 2, 2, ErrorBreakdown{Other: 2}},
		{"recovery", "recover", true, 1, 2, ErrorBreakdown{HTTP4xx: 1}},
		{"successful cache", "", true, 0, 1, ErrorBreakdown{}},
		{"delivery without parsing", "404", false, 0, 0, ErrorBreakdown{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var initRequests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/index.m3u8":
					mapTag := "#EXT-X-MAP:URI=init.mp4\n"
					if tc.failure == "range" {
						mapTag = "#EXT-X-MAP:URI=init.mp4,BYTERANGE=4@0\n"
					}
					_, _ = fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n"+mapTag+"#EXTINF:4,\na.m4s\n#EXTINF:4,\nb.m4s\n#EXT-X-ENDLIST\n")
				case "/init.mp4":
					n := initRequests.Add(1)
					switch {
					case tc.failure == "404" || (tc.failure == "recover" && n == 1):
						http.NotFound(w, r)
					case tc.failure == "503":
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
					case tc.failure == "timeout":
						<-r.Context().Done()
					case tc.failure == "empty":
						w.WriteHeader(http.StatusOK)
					default:
						_, _ = w.Write([]byte{0, 0, 0, 8, 'm', 'o', 'o', 'v'})
					}
				default:
					_, _ = w.Write([]byte{0, 0, 0, 8, 'm', 'o', 'o', 'f'})
				}
			}))
			defer srv.Close()
			rep, err := Analyze(context.Background(), srv.URL+"/index.m3u8", Options{
				Segments: 2, ParseSegments: tc.parse, ErrorThreshold: 1,
				Duration: 3 * time.Second, Timeout: 150 * time.Millisecond,
			})
			if err != nil {
				t.Fatal(err)
			}
			if int(initRequests.Load()) != tc.wantInits || rep.Summary.SegmentsFailed != tc.wantFail || rep.Summary.SegmentsFetched != 2-tc.wantFail {
				t.Fatalf("init requests=%d summary=%+v", initRequests.Load(), rep.Summary)
			}
			if rep.Summary.ErrorBreakdown != tc.breakdown || rep.Summary.ErrorRatePct != float64(tc.wantFail)*50 {
				t.Fatalf("incorrect failure classification: %+v", rep.Summary)
			}
			if (rep.ExitCode == 2) != (tc.wantFail > 0) {
				t.Fatalf("error threshold exit=%d", rep.ExitCode)
			}
			for _, seg := range rep.Segments[:tc.wantFail] {
				if !strings.Contains(seg.Error, "初始化段失败") || !strings.Contains(seg.Error, "/init.mp4") ||
					seg.HTTPStatus != 200 || seg.ThroughputKbps != 0 || seg.StartsWithKeyframe != nil || seg.PTSStart != nil {
					t.Fatalf("initialization failure swallowed: %+v", seg)
				}
			}
			if tc.failure == "404" && tc.parse {
				raw, err := json.Marshal(ReportValue(rep))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(raw), `"init_http_status":404`) {
					t.Fatalf("init HTTP status missing from report: %s", raw)
				}
			}
		})
	}
}
