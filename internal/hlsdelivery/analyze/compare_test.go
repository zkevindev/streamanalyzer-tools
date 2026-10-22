package analyze

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompareSharesDuration(t *testing.T) {
	var rightRequests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/b/") {
			rightRequests.Add(1)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, ".m3u8"):
			_, _ = fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nfast.ts\n#EXTINF:4,\nslow.ts\n#EXT-X-ENDLIST\n")
		case strings.HasSuffix(r.URL.Path, "/slow.ts"):
			<-r.Context().Done()
		default:
			_, _ = fmt.Fprint(w, "segment")
		}
	}))
	defer srv.Close()
	for _, tc := range []struct {
		name     string
		duration time.Duration
		parent   time.Duration
	}{
		{"task duration", 300 * time.Millisecond, 0},
		{"earlier parent deadline", time.Second, 150 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ctx context.Context
			if tc.parent > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), tc.parent)
				defer cancel()
			}
			rightRequests.Store(0)
			start := time.Now()
			rep, err := Compare(ctx, srv.URL+"/a/index.m3u8", srv.URL+"/b/index.m3u8", Options{
				Duration: tc.duration,
				Timeout:  2 * time.Second,
				Segments: 2,
			})
			elapsed := time.Since(start)
			if !errors.Is(err, context.DeadlineExceeded) || rep != nil {
				t.Fatalf("expired comparison: report=%+v, err=%v", rep, err)
			}
			if got := rightRequests.Load(); got != 0 {
				t.Fatalf("stream B received %d requests after the shared deadline", got)
			}
			limit := tc.duration
			if tc.parent > 0 {
				limit = tc.parent
			}
			if elapsed > limit+limit/2 {
				t.Fatalf("comparison took %v for shared budget %v", elapsed, limit)
			}
		})
	}
}
