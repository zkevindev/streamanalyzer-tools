package analyze

import (
	"bytes"
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

func TestMonitorRequiresEdgeProgramTime(t *testing.T) {
	for _, tc := range []struct {
		name          string
		discontinuity bool
		restoreTime   bool
		wantKnown     bool
	}{
		{"missing after discontinuity", true, false, false},
		{"restored after discontinuity", true, true, true},
		{"propagated to live edge", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now().Add(-10 * time.Second)
			playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-PROGRAM-DATE-TIME:" + start.UTC().Format(time.RFC3339Nano) + "\n#EXTINF:4,\na.ts\n"
			if tc.discontinuity {
				playlist += "#EXT-X-DISCONTINUITY\n"
			}
			if tc.restoreTime {
				playlist += "#EXT-X-PROGRAM-DATE-TIME:" + start.Add(4*time.Second).UTC().Format(time.RFC3339Nano) + "\n"
			}
			playlist += "#EXTINF:4,\nb.ts\n"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".m3u8") {
					_, _ = fmt.Fprint(w, playlist)
					return
				}
				_, _ = fmt.Fprint(w, "segment")
			}))
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var log bytes.Buffer
			fetched := 0
			rep, err := Monitor(ctx, srv.URL+"/live.m3u8", Options{
				Duration: time.Second,
				Log:      &log,
				OnSegment: func(SegmentResult) {
					fetched++
					if fetched == 2 {
						cancel()
					}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if fetched != 2 {
				t.Fatalf("fetched %d segments", fetched)
			}
			if (rep.LiveLatencyMS != nil) != tc.wantKnown || (rep.EdgeLagMS != nil) != tc.wantKnown {
				t.Fatalf("known=%v, viewer=%v edge=%v", tc.wantKnown, rep.LiveLatencyMS, rep.EdgeLagMS)
			}
			view := analyzeJSON(rep)
			for _, field := range []string{"live_latency_ms", "edge_lag_ms"} {
				if _, ok := view[field]; ok != tc.wantKnown {
					t.Fatalf("JSON field %s present=%v, want %v", field, ok, tc.wantKnown)
				}
			}
			if !tc.wantKnown && !strings.Contains(log.String(), "观看延迟未知") {
				t.Fatalf("missing unknown latency log: %s", log.String())
			}
			if tc.wantKnown && (*rep.EdgeLagMS < 1900 || *rep.LiveLatencyMS-*rep.EdgeLagMS != 12000) {
				t.Fatalf("incorrect live-edge latency: viewer=%v edge=%v", *rep.LiveLatencyMS, *rep.EdgeLagMS)
			}
		})
	}
}

func TestMonitorReportsPlaylistRefreshFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		master  bool
		failure string
		recover bool
	}{
		{"media HTTP failure", false, "http", false},
		{"master child HTTP failure", true, "http", false},
		{"media timeout", false, "timeout", false},
		{"malformed playlist", false, "parse", false},
		{"empty playlist", false, "empty", false},
		{"refresh recovers", false, "http", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var refreshes atomic.Int32
			start := time.Now().Add(-2 * time.Second)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/master.m3u8" {
					_, _ = fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000\nmedia.m3u8\n")
					return
				}
				if !strings.HasSuffix(r.URL.Path, ".m3u8") {
					_, _ = fmt.Fprint(w, "segment")
					return
				}
				n := refreshes.Add(1)
				if n > 1 && !(tc.recover && n > 2) {
					switch tc.failure {
					case "http":
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
					case "timeout":
						<-r.Context().Done()
					case "parse":
						_, _ = fmt.Fprint(w, "invalid playlist")
					case "empty":
						_, _ = fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n")
					}
					return
				}
				sequence := 1
				if n > 2 {
					sequence = 2
				}
				pdt := start.Add(time.Duration(sequence-1) * time.Second).UTC().Format(time.RFC3339Nano)
				_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-PROGRAM-DATE-TIME:%s\n#EXTINF:1,\n%d.ts\n", sequence, pdt, sequence)
			}))
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			duration := time.Second
			if tc.recover {
				duration = 3 * time.Second
			}
			path := "/media.m3u8"
			if tc.master {
				path = "/master.m3u8"
			}
			rep, err := Monitor(ctx, srv.URL+path, Options{
				Duration: duration,
				Timeout:  150 * time.Millisecond,
				OnSegment: func(seg SegmentResult) {
					if tc.recover && seg.Sequence == 2 {
						cancel()
					}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			stats := rep.PlaylistRefresh
			if stats == nil || stats.Failures != 1 || stats.LastError == "" {
				t.Fatalf("refresh stats = %+v", stats)
			}
			consecutive, fetched := 1, 1
			if tc.recover {
				consecutive, fetched = 0, 2
			}
			if stats.ConsecutiveFailures != consecutive || rep.Summary.SegmentsFetched != fetched || rep.Summary.SegmentsFailed != 0 {
				t.Fatalf("refresh=%+v summary=%+v", stats, rep.Summary)
			}
			if !tc.recover && (rep.LiveLatencyMS != nil || rep.EdgeLagMS != nil) {
				t.Fatalf("outage published latency from an outdated playlist: %+v", rep)
			}
			warnings := strings.Join(rep.Infrastructure.PlaylistWarnings, "\n")
			if !strings.Contains(warnings, "播放列表刷新失败 1 次") ||
				!strings.Contains(strings.Join(rep.Recommendations, "\n"), stats.LastError) {
				t.Fatalf("refresh failure missing from warnings: %s", warnings)
			}
			if tc.recover && !strings.Contains(warnings, "已恢复") {
				t.Fatalf("recovery missing from warning: %s", warnings)
			}
			raw, err := json.Marshal(ReportValue(rep))
			if err != nil {
				t.Fatal(err)
			}
			var view struct {
				Refresh *PlaylistRefreshStats `json:"playlist_refresh"`
			}
			if err := json.Unmarshal(raw, &view); err != nil {
				t.Fatal(err)
			}
			if view.Refresh == nil || *view.Refresh != *stats {
				t.Fatalf("refresh failure missing from JSON: %s", raw)
			}
		})
	}
}
