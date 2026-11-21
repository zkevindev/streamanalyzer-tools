package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
