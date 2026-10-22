package analyze

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/hlsdelivery/m3u8"
)

func TestFetchByteRange(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		contentRange string
		body         string
		wantError    bool
	}{
		{"valid", 206, "bytes 4-7/12", "4567", false},
		{"unknown total", 206, "bytes 4-7/*", "4567", false},
		{"ignored range", 200, "", "0123456789ab", true},
		{"missing content range", 206, "", "4567", true},
		{"wrong offset", 206, "bytes 0-3/12", "0123", true},
		{"wrong end", 206, "bytes 4-8/12", "45678", true},
		{"invalid total", 206, "bytes 4-7/7", "4567", true},
		{"malformed total", 206, "bytes 4-7/12junk", "4567", true},
		{"short body", 206, "bytes 4-7/12", "45", true},
		{"long body", 206, "bytes 4-7/12", "45678", true},
		{"unsatisfiable", 416, "bytes */3", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Range"); got != "bytes=4-7" {
					t.Errorf("Range = %q", got)
				}
				w.Header().Set("Content-Range", tc.contentRange)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			r := newRunner(Options{Timeout: time.Second})
			got := r.get(context.Background(), srv.URL, maxSegment, "", &m3u8.ByteRange{Offset: 4, Length: 4})
			if (got.Err != nil) != tc.wantError {
				t.Fatalf("fetch error = %v, want error %v", got.Err, tc.wantError)
			}
			if !tc.wantError && string(got.Body) != "4567" {
				t.Fatalf("body = %q", got.Body)
			}
			if tc.status == 200 && len(got.Body) != 0 {
				t.Fatalf("ignored range retained full object: %q", got.Body)
			}
		})
	}
}

func TestAnalyzeIgnoredByteRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\n#EXT-X-BYTERANGE:4@4\nshared.ts\n#EXT-X-ENDLIST\n"))
			return
		}
		_, _ = w.Write([]byte("0123456789ab"))
	}))
	defer srv.Close()
	rep, err := Analyze(context.Background(), srv.URL+"/index.m3u8", Options{Segments: 1, ParseSegments: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.SegmentsFailed != 1 || rep.Summary.SegmentsFetched != 0 || len(rep.Segments) != 1 {
		t.Fatalf("summary = %+v, segments = %+v", rep.Summary, rep.Segments)
	}
	seg := rep.Segments[0]
	if seg.Error == "" || seg.ThroughputKbps != 0 || seg.StartsWithKeyframe != nil || seg.Bytes != 0 {
		t.Fatalf("ignored range counted as a successful segment: %+v", seg)
	}
}
