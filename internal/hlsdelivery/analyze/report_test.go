package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnalysisJSONIncludesPlaylist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".m3u8") {
			_, _ = fmt.Fprint(w, "segment")
			return
		}
		playlist := "#EXTM3U\n#EXT-X-VERSION:6\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:17\n#EXT-X-INDEPENDENT-SEGMENTS\n#EXT-X-KEY:METHOD=AES-128,URI=key.key\n#EXTINF:4,\nsegment.ts\n"
		if r.URL.Path == "/vod.m3u8" {
			playlist += "#EXT-X-ENDLIST\n"
		}
		_, _ = fmt.Fprint(w, playlist)
	}))
	defer srv.Close()
	for _, command := range []string{"analyze", "monitor", "compare"} {
		t.Run(command, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opt := Options{Duration: time.Second, Segments: 1}
			var value any
			if command == "compare" {
				rep, err := Compare(ctx, srv.URL+"/vod.m3u8", srv.URL+"/vod.m3u8", opt)
				if err != nil {
					t.Fatal(err)
				}
				value = CompareValue(rep)
			} else {
				fn, path := Analyze, "/vod.m3u8"
				if command == "monitor" {
					fn, path = Monitor, "/live.m3u8"
					opt.OnSegment = func(SegmentResult) { cancel() }
				}
				rep, err := fn(ctx, srv.URL+path, opt)
				if err != nil {
					t.Fatal(err)
				}
				value = ReportValue(rep)
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			items := []json.RawMessage{raw}
			if command == "compare" {
				var compare struct{ A, B json.RawMessage }
				if err := json.Unmarshal(raw, &compare); err != nil {
					t.Fatal(err)
				}
				items = []json.RawMessage{compare.A, compare.B}
			}
			for _, item := range items {
				var view struct {
					Live     bool          `json:"live"`
					Playlist *PlaylistInfo `json:"playlist"`
				}
				if err := json.Unmarshal(item, &view); err != nil {
					t.Fatal(err)
				}
				want := PlaylistInfo{Version: 6, TargetDurationS: 4, MediaSequence: 17, SegmentCount: 1,
					IndependentSegments: true, Encrypted: true, Live: command == "monitor"}
				if view.Playlist == nil || *view.Playlist != want || view.Live != want.Live {
					t.Fatalf("playlist metadata missing or changed: %s", item)
				}
			}
		})
	}
}
