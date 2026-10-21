package handler

import (
	"encoding/json"
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
