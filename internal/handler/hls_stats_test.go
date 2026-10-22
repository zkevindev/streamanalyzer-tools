package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestHLSStatsAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXTINF:4.0,\nseg.ts\n#EXT-X-ENDLIST\n"))
		case "/seg.ts":
			w.Header().Set("X-Cache", "HIT")
			_, _ = w.Write([]byte("segment-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer media.Close()

	h := NewHandler(nil, nil, nil)
	r := gin.New()
	h.RegisterRoutes(r)

	body := `{"command":"analyze","segments":1,"duration":"5s","timeout":"2s","url":"` + media.URL + `/a.m3u8"}`
	req := httptest.NewRequest(http.MethodPost, "/api/hls-stats/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Status != "running" {
		t.Fatalf("created %+v", created)
	}

	deadline := time.Now().Add(5 * time.Second)
	var view hlsStatsView
	for {
		get := httptest.NewRequest(http.MethodGet, "/api/hls-stats/tasks/"+created.ID, nil)
		got := httptest.NewRecorder()
		r.ServeHTTP(got, get)
		if got.Code != http.StatusOK {
			t.Fatalf("get %d %s", got.Code, got.Body.String())
		}
		if err := json.Unmarshal(got.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if view.Status != "running" || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if view.Status != "done" || view.ExitCode != 0 {
		t.Fatalf("view %+v", view)
	}
	raw, err := json.Marshal(view.Report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"segments_fetched":1`) && !strings.Contains(string(raw), `"segments_fetched": 1`) {
		t.Fatalf("report %s", raw)
	}
	var report struct {
		Playlist struct {
			TargetDurationS float64 `json:"target_duration_s"`
			SegmentCount    int     `json:"segment_count"`
		} `json:"playlist"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.Playlist.TargetDurationS != 4 || report.Playlist.SegmentCount != 1 {
		t.Fatalf("HTTP report missing playlist metadata: %s", raw)
	}

	missing := httptest.NewRequest(http.MethodPost, "/api/hls-stats/tasks", strings.NewReader(`{"command":"analyze","duration":"5s","timeout":"2s","url":"`+media.URL+`/missing.m3u8"}`))
	missing.Header.Set("Content-Type", "application/json")
	missRec := httptest.NewRecorder()
	r.ServeHTTP(missRec, missing)
	var missCreated struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(missRec.Body.Bytes(), &missCreated); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		get := httptest.NewRequest(http.MethodGet, "/api/hls-stats/tasks/"+missCreated.ID, nil)
		got := httptest.NewRecorder()
		r.ServeHTTP(got, get)
		if err := json.Unmarshal(got.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if view.Status != "running" || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if view.Status != "failed" || view.ExitCode != 5 {
		t.Fatalf("missing %+v", view)
	}
}

func TestHLSStatsCancellationPreservesPartialReport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, command := range []string{"analyze", "monitor"} {
		t.Run(command, func(t *testing.T) {
			blocked := make(chan struct{})
			media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/index.m3u8":
					_, _ = fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nfast.ts\n#EXTINF:4,\nslow.ts\n")
				case "/slow.ts":
					close(blocked)
					<-r.Context().Done()
				default:
					_, _ = fmt.Fprint(w, "segment")
				}
			}))
			defer media.Close()
			h := NewHandler(nil, nil, nil)
			router := gin.New()
			h.RegisterRoutes(router)
			request := func(method, path, body string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				return rec
			}
			created := request(http.MethodPost, "/api/hls-stats/tasks", fmt.Sprintf(
				`{"command":%q,"url":%q,"segments":2,"duration":"5s","timeout":"4s"}`, command, media.URL+"/index.m3u8"))
			if created.Code != http.StatusOK {
				t.Fatalf("create: %d %s", created.Code, created.Body.String())
			}
			var view hlsStatsView
			if err := json.Unmarshal(created.Body.Bytes(), &view); err != nil {
				t.Fatal(err)
			}
			id := view.ID
			defer h.hlsStats.cancel(id)
			select {
			case <-blocked:
			case <-time.After(3 * time.Second):
				t.Fatal("second segment was never requested")
			}
			path := "/api/hls-stats/tasks/" + id
			stopped := request(http.MethodDelete, path, "")
			if stopped.Code != http.StatusOK || !strings.Contains(stopped.Body.String(), "stopping") {
				t.Fatalf("stop: %d %s", stopped.Code, stopped.Body.String())
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				got := request(http.MethodGet, path, "")
				if err := json.Unmarshal(got.Body.Bytes(), &view); err != nil {
					t.Fatal(err)
				}
				if view.Status != "stopping" && view.Status != "running" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("cancellation did not finish: %+v", view)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if view.Status != "canceled" || view.ExitCode != 1 || view.Error != "任务已取消" || view.Finished.IsZero() || view.Report == nil {
				t.Fatalf("cancellation lost: %+v", view)
			}
			raw, err := json.Marshal(view.Report)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"segments_fetched":1`) {
				t.Fatalf("partial report lost: %s", raw)
			}
			again := request(http.MethodDelete, path, "")
			if !strings.Contains(again.Body.String(), "canceled") {
				t.Fatalf("repeated stop changed terminal state: %s", again.Body.String())
			}
		})
	}
}

func TestHLSStatsCancelWinsFinalTransition(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var store hlsStatsStore
	if err := store.put(&hlsStatsJob{hlsStatsView: hlsStatsView{ID: "job", Status: "running"}, cancel: cancel}); err != nil {
		t.Fatal(err)
	}
	if status, ok := store.cancel("job"); !ok || status != "stopping" || ctx.Err() != context.Canceled {
		t.Fatalf("stop state=%s found=%v context=%v", status, ok, ctx.Err())
	}
	// Model a worker that checked its context just before the stop request arrived.
	partial := map[string]any{"segments_fetched": 1}
	store.finish("job", "done", 0, "", partial)
	view, _ := store.get("job")
	if view.Status != "canceled" || view.ExitCode != 1 || view.Report == nil {
		t.Fatalf("completion overwrote requested cancellation: %+v", view)
	}
}

func TestHLSStatsRejectsFileURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(nil, nil, nil)
	r := gin.New()
	h.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/api/hls-stats/tasks", strings.NewReader(`{"command":"manifest","url":"file:///etc/passwd"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestHLSStatsStoreLimit(t *testing.T) {
	var s hlsStatsStore
	for i := 0; i < hlsStatsMaxJobs; i++ {
		job := &hlsStatsJob{hlsStatsView: hlsStatsView{ID: fmt.Sprintf("r%d", i), Status: "running"}}
		if i == 0 {
			job.Status = "stopping"
		}
		if err := s.put(job); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.put(&hlsStatsJob{hlsStatsView: hlsStatsView{ID: "overflow", Status: "running"}}); err != errHLSStatsFull {
		t.Fatalf("overflow: %v", err)
	}
	if len(s.order) != hlsStatsMaxJobs || len(s.jobs) != hlsStatsMaxJobs {
		t.Fatalf("order %d jobs %d", len(s.order), len(s.jobs))
	}
	s.finish("r10", "done", 0, "", nil)
	if err := s.put(&hlsStatsJob{hlsStatsView: hlsStatsView{ID: "fresh", Status: "running"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.jobs["r10"]; ok {
		t.Fatal("finished job should be evicted")
	}
	if _, ok := s.jobs["r0"]; !ok {
		t.Fatal("oldest running job should stay")
	}
	if _, ok := s.jobs["fresh"]; !ok || len(s.jobs) != hlsStatsMaxJobs {
		t.Fatalf("jobs %d", len(s.jobs))
	}
}
