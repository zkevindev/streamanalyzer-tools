package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"streamanalyzer/internal/models"
	"streamanalyzer/internal/offline"
)

func TestOfflineNewAPIRoutesLocalInspectBoxesAndLocalhostGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr := offline.NewManager(t.TempDir())
	h := NewHandler(mgr)
	r := gin.New()
	h.RegisterRoutes(r)

	// Build a real fMP4 file (init.mp4 + seg-01.m4s)
	initBytes, err := os.ReadFile(filepath.Join("..", "simulator", "hlsorigin", "avmedia", "video", "init.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	segBytes, err := os.ReadFile(filepath.Join("..", "simulator", "hlsorigin", "avmedia", "video", "seg-01.m4s"))
	if err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(t.TempDir(), "sample.mp4")
	if err := os.WriteFile(localPath, append(initBytes, segBytes...), 0o644); err != nil {
		t.Fatal(err)
	}

	bodyBytes, _ := json.Marshal(map[string]any{
		"path": localPath,
		"mode": "auto",
	})

	// 1. Remote IP must be rejected by localDiagnostics middleware
	reqRemote := httptest.NewRequest(http.MethodPost, "/api/offline/tasks/local", bytes.NewReader(bodyBytes))
	reqRemote.Header.Set("Content-Type", "application/json")
	reqRemote.RemoteAddr = "198.51.100.25:54321"
	wRemote := httptest.NewRecorder()
	r.ServeHTTP(wRemote, reqRemote)
	if wRemote.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-localhost /tasks/local, got %d: %s", wRemote.Code, wRemote.Body.String())
	}

	// 2. Localhost creates zero-copy task
	reqLocal := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/offline/tasks/local", bytes.NewReader(bodyBytes))
	reqLocal.Header.Set("Content-Type", "application/json")
	reqLocal.RemoteAddr = "127.0.0.1:54321"
	wLocal := httptest.NewRecorder()
	r.ServeHTTP(wLocal, reqLocal)
	if wLocal.Code != http.StatusOK {
		t.Fatalf("expected 200 from /tasks/local, got %d: %s", wLocal.Code, wLocal.Body.String())
	}
	var task models.OfflineTask
	if err := json.Unmarshal(wLocal.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.SourceType != "local" || task.Mode != models.OfflineModeMP4 {
		t.Fatalf("unexpected task response: %+v", task)
	}

	// Wait for completion
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		got, err := mgr.GetTask(task.ID)
		if err == nil && got.Status != models.OfflineStatusRunning {
			if got.Status != models.OfflineStatusDone {
				t.Fatalf("task failed: %s", got.Error)
			}
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	// 3. GET /api/offline/tasks/:id/inspect/frame
	wInsp := httptest.NewRecorder()
	r.ServeHTTP(wInsp, httptest.NewRequest(http.MethodGet, "/api/offline/tasks/"+task.ID+"/inspect/frame?flow_idx=0&frame_idx=0&hex_limit=256", nil))
	if wInsp.Code != http.StatusOK {
		t.Fatalf("expected 200 from /inspect/frame, got %d: %s", wInsp.Code, wInsp.Body.String())
	}
	var inspResp offline.FrameInspectResponse
	if err := json.Unmarshal(wInsp.Body.Bytes(), &inspResp); err != nil {
		t.Fatal(err)
	}
	if len(inspResp.Inspection.NALUs) == 0 || len(inspResp.Inspection.Hex.Lines) == 0 {
		t.Fatalf("unexpected inspect response: %+v", inspResp.Inspection)
	}

	// 4. GET /api/offline/tasks/:id/mp4/boxes
	wBox := httptest.NewRecorder()
	r.ServeHTTP(wBox, httptest.NewRequest(http.MethodGet, "/api/offline/tasks/"+task.ID+"/mp4/boxes", nil))
	if wBox.Code != http.StatusOK {
		t.Fatalf("expected 200 from /mp4/boxes, got %d: %s", wBox.Code, wBox.Body.String())
	}
	if !bytes.Contains(wBox.Body.Bytes(), []byte(`"moof"`)) || !bytes.Contains(wBox.Body.Bytes(), []byte(`"ftyp"`)) {
		t.Fatalf("expected ftyp and moof in /mp4/boxes response: %s", wBox.Body.String())
	}

	// 5. POST /api/offline/tasks/:id/cancel on finished task returns 400 cleanly
	wCancel := httptest.NewRecorder()
	r.ServeHTTP(wCancel, httptest.NewRequest(http.MethodPost, "/api/offline/tasks/"+task.ID+"/cancel", nil))
	if wCancel.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 canceling non-running task, got %d: %s", wCancel.Code, wCancel.Body.String())
	}
}

func TestOfflineCompareReportAndFFmpegAPIRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr := offline.NewManager(t.TempDir())
	h := NewHandler(mgr)
	r := gin.New()
	h.RegisterRoutes(r)

	tsPath := filepath.Join("..", "simulator", "mediasource", "fixtures", "regular.ts")
	initBytes, err := os.ReadFile(filepath.Join("..", "simulator", "hlsorigin", "avmedia", "video", "init.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	segBytes, err := os.ReadFile(filepath.Join("..", "simulator", "hlsorigin", "avmedia", "video", "seg-01.m4s"))
	if err != nil {
		t.Fatal(err)
	}
	mp4Path := filepath.Join(t.TempDir(), "sample_b.mp4")
	if err := os.WriteFile(mp4Path, append(initBytes, segBytes...), 0o644); err != nil {
		t.Fatal(err)
	}

	createLocal := func(p, mode string) models.OfflineTask {
		body, _ := json.Marshal(map[string]any{"path": p, "mode": mode})
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/offline/tasks/local", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:54321"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("create local failed (%d): %s", w.Code, w.Body.String())
		}
		var task models.OfflineTask
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			got, err := mgr.GetTask(task.ID)
			if err == nil && got.Status != models.OfflineStatusRunning {
				if got.Status != models.OfflineStatusDone {
					t.Fatalf("task %s failed: %s", task.ID, got.Error)
				}
				return *got
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("task %s timed out", task.ID)
		return task
	}

	taskA := createLocal(tsPath, "ts")
	taskB := createLocal(mp4Path, "mp4")

	// 1. GET /api/offline/compare
	wCmp := httptest.NewRecorder()
	r.ServeHTTP(wCmp, httptest.NewRequest(http.MethodGet, "/api/offline/compare?task_a="+taskA.ID+"&task_b="+taskB.ID, nil))
	if wCmp.Code != http.StatusOK {
		t.Fatalf("expected 200 from /compare, got %d: %s", wCmp.Code, wCmp.Body.String())
	}
	var cmpResp models.OfflineCompareResult
	if err := json.Unmarshal(wCmp.Body.Bytes(), &cmpResp); err != nil {
		t.Fatal(err)
	}
	if len(cmpResp.Rows) < 5 || cmpResp.ChangedCount == 0 {
		t.Fatalf("unexpected compare result: %+v", cmpResp)
	}

	// 2. GET /api/offline/tasks/:id/report?format=html and format=md
	wHTML := httptest.NewRecorder()
	r.ServeHTTP(wHTML, httptest.NewRequest(http.MethodGet, "/api/offline/tasks/"+taskA.ID+"/report?flow=0&format=html", nil))
	if wHTML.Code != http.StatusOK || !strings.Contains(wHTML.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected 200 text/html report, got %d %s", wHTML.Code, wHTML.Header().Get("Content-Type"))
	}
	wMD := httptest.NewRecorder()
	r.ServeHTTP(wMD, httptest.NewRequest(http.MethodGet, "/api/offline/tasks/"+taskA.ID+"/report?flow=0&format=md", nil))
	if wMD.Code != http.StatusOK || !strings.Contains(wMD.Header().Get("Content-Type"), "text/markdown") {
		t.Fatalf("expected 200 text/markdown report, got %d %s", wMD.Code, wMD.Header().Get("Content-Type"))
	}

	// 3. FFmpeg endpoints (thumbnail, decode-audit, clip) if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		wThumb := httptest.NewRecorder()
		r.ServeHTTP(wThumb, httptest.NewRequest(http.MethodGet, "/api/offline/tasks/"+taskA.ID+"/thumbnail?flow=0&time_ms=0", nil))
		if wThumb.Code != http.StatusOK || wThumb.Header().Get("Content-Type") != "image/jpeg" {
			t.Fatalf("expected 200 image/jpeg from /thumbnail, got %d (%s): %s", wThumb.Code, wThumb.Header().Get("Content-Type"), wThumb.Body.String())
		}

		wAudit := httptest.NewRecorder()
		r.ServeHTTP(wAudit, httptest.NewRequest(http.MethodPost, "/api/offline/tasks/"+taskA.ID+"/decode-audit?flow=0", nil))
		if wAudit.Code != http.StatusOK {
			t.Fatalf("expected 200 from /decode-audit, got %d: %s", wAudit.Code, wAudit.Body.String())
		}

		clipBody, _ := json.Marshal(map[string]any{"flow": 0, "start_sec": 0, "end_sec": 1})
		reqClip := httptest.NewRequest(http.MethodPost, "/api/offline/tasks/"+taskA.ID+"/clip", bytes.NewReader(clipBody))
		reqClip.Header.Set("Content-Type", "application/json")
		wClip := httptest.NewRecorder()
		r.ServeHTTP(wClip, reqClip)
		if wClip.Code != http.StatusOK {
			t.Fatalf("expected 200 from /clip, got %d: %s", wClip.Code, wClip.Body.String())
		}
		var clipResp struct {
			DownloadURL string `json:"download_url"`
			SizeBytes   int64  `json:"size_bytes"`
		}
		if err := json.Unmarshal(wClip.Body.Bytes(), &clipResp); err != nil {
			t.Fatal(err)
		}
		if clipResp.SizeBytes <= 0 || !strings.HasPrefix(clipResp.DownloadURL, "/api/offline/tasks/") {
			t.Fatalf("unexpected clip response: %+v", clipResp)
		}
		// Verify clip download URL actually serves the clip file
		wDl := httptest.NewRecorder()
		r.ServeHTTP(wDl, httptest.NewRequest(http.MethodGet, clipResp.DownloadURL, nil))
		if wDl.Code != http.StatusOK || int64(wDl.Body.Len()) != clipResp.SizeBytes {
			t.Fatalf("clip download failed: code=%d len=%d want=%d", wDl.Code, wDl.Body.Len(), clipResp.SizeBytes)
		}
	}
}
