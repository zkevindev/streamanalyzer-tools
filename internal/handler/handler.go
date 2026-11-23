package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"streamanalyzer/internal/models"
	"streamanalyzer/internal/offline"
	"streamanalyzer/internal/rtmpraw"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	offline *offline.Manager
}

func NewHandler(offlineMgr *offline.Manager) *Handler {
	return &Handler{
		offline: offlineMgr,
	}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api")
	{
		offlineAPI := api.Group("/offline")
		{
			offlineAPI.POST("/tasks", h.CreateOfflineTask)
			offlineAPI.POST("/tasks/local", localDiagnostics, h.CreateLocalOfflineTask)
			offlineAPI.GET("/tasks", h.ListOfflineTasks)
			offlineAPI.GET("/compare", h.CompareOfflineTasks)
			offlineAPI.GET("/tasks/:id", h.GetOfflineTask)
			offlineAPI.DELETE("/tasks/:id", h.DeleteOfflineTask)
			offlineAPI.POST("/tasks/:id/reparse", h.ReparseOfflineTask)
			offlineAPI.POST("/tasks/:id/cancel", h.CancelOfflineTask)
			offlineAPI.GET("/tasks/:id/inspect/frame", h.InspectOfflineFrame)
			offlineAPI.GET("/tasks/:id/mp4/boxes", h.InspectOfflineMP4Boxes)
			offlineAPI.GET("/tasks/:id/thumbnail", h.GetOfflineThumbnail)
			offlineAPI.POST("/tasks/:id/decode-audit", h.RunOfflineDecodeAudit)
			offlineAPI.POST("/tasks/:id/clip", h.ExportOfflineClip)
			offlineAPI.GET("/tasks/:id/report", h.DownloadOfflineReport)
			offlineAPI.GET("/tasks/:id/files/*name", h.DownloadOfflineFile)
		}
	}

	r.StaticFS("/static", embeddedStaticHTTPFS())
	r.GET("/", func(c *gin.Context) { serveEmbeddedStaticFile(c, "index.html") })
	r.GET("/realtime", func(c *gin.Context) { c.Redirect(http.StatusFound, "/diagnostics") })
	r.GET("/history", func(c *gin.Context) { serveEmbeddedStaticFile(c, "history.html") })
	r.GET("/offline", func(c *gin.Context) { serveEmbeddedStaticFile(c, "offline.html") })
}

func (h *Handler) CreateOfflineTask(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "offline manager not configured"})
		return
	}

	mode := strings.ToLower(strings.TrimSpace(c.PostForm("mode")))
	if mode == "" {
		mode = string(models.OfflineModeAuto)
	}
	var req models.OfflineTaskRequest
	req.Mode = models.OfflineMode(mode)
	switch req.Mode {
	case models.OfflineModeAuto,
		models.OfflineModeRaw,
		models.OfflineModePCAP,
		models.OfflineModeTS,
		models.OfflineModeFLV,
		models.OfflineModeMP4,
		models.OfflineModeAnnexB,
		models.OfflineModePS,
		models.OfflineModeWebM,
		models.OfflineModeDASH:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode"})
		return
	}

	if sp := strings.TrimSpace(c.PostForm("server_port")); sp != "" {
		if v, err := strconv.Atoi(sp); err == nil && v > 0 && v <= 65535 {
			req.ServerPort = uint16(v)
		}
	}
	if req.Mode == models.OfflineModePCAP && req.ServerPort == 0 {
		req.ServerPort = 1935
	}

	skipExplicit := false
	if sb := strings.TrimSpace(c.PostForm("skip_bytes")); sb != "" {
		v, err := strconv.Atoi(sb)
		if err != nil || v < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid skip_bytes"})
			return
		}
		req.SkipBytes = v
		skipExplicit = true
	} else if req.Mode == models.OfflineModeRaw || req.Mode == models.OfflineModePCAP {
		// Default assumes full RTMP handshake (C0+C1+C2 or S0+S1+S2 = 3073 bytes).
		req.SkipBytes = rtmpraw.DefaultHandshakeSkipBytes
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}

	task, inputPath, err := h.offline.CreateTask(&req, file.Filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := c.SaveUploadedFile(file, inputPath); err != nil {
		_ = h.offline.DeleteTask(task.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	prepared, err := h.offline.PrepareTask(task.ID, skipExplicit)
	if err != nil {
		_ = h.offline.DeleteTask(task.ID)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.offline.StartTask(prepared.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	prepared.Status = models.OfflineStatusRunning
	c.JSON(http.StatusOK, prepared)
}

func (h *Handler) ListOfflineTasks(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusOK, gin.H{"tasks": []*models.OfflineTask{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": h.offline.ListTasks()})
}

func (h *Handler) GetOfflineTask(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
	task, err := h.offline.GetTask(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	var summary *models.OfflineSummary
	if task.SummaryPath != "" {
		if b, err := os.ReadFile(task.SummaryPath); err == nil {
			var s models.OfflineSummary
			if err := json.Unmarshal(b, &s); err == nil {
				summary = &s
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"task": task, "summary": summary})
}

func (h *Handler) DeleteOfflineTask(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
	if err := h.offline.DeleteTask(taskID); err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": taskID})
}

func (h *Handler) ReparseOfflineTask(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
	task, err := h.offline.ReparseTask(taskID)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, task)
}

type localOfflineTaskRequest struct {
	Path       string `json:"path"`
	FilePath   string `json:"file_path"`
	Mode       string `json:"mode"`
	ServerPort uint16 `json:"server_port"`
	SkipBytes  *int   `json:"skip_bytes"`
}

func (h *Handler) CreateLocalOfflineTask(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "offline manager not configured"})
		return
	}
	var body localOfflineTaskRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	localPath := strings.TrimSpace(body.Path)
	if localPath == "" {
		localPath = strings.TrimSpace(body.FilePath)
	}
	if localPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	mode := strings.ToLower(strings.TrimSpace(body.Mode))
	if mode == "" {
		mode = string(models.OfflineModeAuto)
	}
	req := models.OfflineTaskRequest{
		Mode:       models.OfflineMode(mode),
		ServerPort: body.ServerPort,
	}
	switch req.Mode {
	case models.OfflineModeAuto,
		models.OfflineModeRaw,
		models.OfflineModePCAP,
		models.OfflineModeTS,
		models.OfflineModeFLV,
		models.OfflineModeMP4,
		models.OfflineModeAnnexB,
		models.OfflineModePS,
		models.OfflineModeWebM,
		models.OfflineModeDASH:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode"})
		return
	}
	if req.Mode == models.OfflineModePCAP && req.ServerPort == 0 {
		req.ServerPort = 1935
	}

	skipExplicit := false
	if body.SkipBytes != nil {
		if *body.SkipBytes < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid skip_bytes"})
			return
		}
		req.SkipBytes = *body.SkipBytes
		skipExplicit = true
	} else if req.Mode == models.OfflineModeRaw || req.Mode == models.OfflineModePCAP {
		req.SkipBytes = rtmpraw.DefaultHandshakeSkipBytes
	}

	task, err := h.offline.CreateLocalTask(&req, localPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	prepared, err := h.offline.PrepareTask(task.ID, skipExplicit)
	if err != nil {
		_ = h.offline.DeleteTask(task.ID)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.offline.StartTask(prepared.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	prepared.Status = models.OfflineStatusRunning
	c.JSON(http.StatusOK, prepared)
}

func (h *Handler) CancelOfflineTask(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
	if err := h.offline.CancelTask(taskID); err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"canceling": taskID})
}

func (h *Handler) InspectOfflineFrame(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
	flowIdx, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("flow_idx", "0")))
	hexOffset, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("hex_offset", "0")))
	hexLimit, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("hex_limit", "2048")))

	if pesStr := strings.TrimSpace(c.Query("pes_idx")); pesStr != "" {
		pesIdx, err := strconv.Atoi(pesStr)
		if err != nil || pesIdx < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pes_idx"})
			return
		}
		resp, err := h.offline.InspectPES(taskID, flowIdx, pesIdx, hexOffset, hexLimit)
		if err != nil {
			code := http.StatusBadRequest
			if strings.Contains(err.Error(), "not found") {
				code = http.StatusNotFound
			}
			c.JSON(code, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	frameIdx, err := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("frame_idx", "0")))
	if err != nil || frameIdx < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid frame_idx"})
		return
	}
	resp, err := h.offline.InspectFrame(taskID, flowIdx, frameIdx, hexOffset, hexLimit)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) InspectOfflineMP4Boxes(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
	tree, err := h.offline.InspectMP4Boxes(taskID)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tree)
}

func (h *Handler) DownloadOfflineFile(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
	if taskID == "" || strings.Contains(taskID, "..") || strings.ContainsAny(taskID, `/\`) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task id"})
		return
	}
	name := c.Param("name")
	name = filepath.Clean(name)
	name = filepath.ToSlash(name)
	name = strings.TrimPrefix(name, "/")
	if name == "" || strings.Contains(name, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file name"})
		return
	}

	base := filepath.Join(h.offline.BaseDir(), "offline", taskID)
	full := filepath.Join(base, name)
	if !strings.HasPrefix(full, base+string(os.PathSeparator)) && full != base {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}
	if _, err := os.Stat(full); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	c.File(full)
}

func (h *Handler) GetOfflineThumbnail(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
	flowIdx, _ := strconv.Atoi(c.DefaultQuery("flow", "0"))
	timeMS, _ := strconv.ParseInt(c.DefaultQuery("time_ms", "0"), 10, 64)
	if timeMS < 0 {
		timeMS = 0
	}

	img, err := h.offline.ExtractFrameThumbnail(c.Request.Context(), taskID, flowIdx, timeMS)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		} else if strings.Contains(err.Error(), "未找到 ffmpeg") {
			code = http.StatusServiceUnavailable
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "image/jpeg", img)
}

func (h *Handler) RunOfflineDecodeAudit(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
	flowIdx, _ := strconv.Atoi(c.DefaultQuery("flow", "0"))
	force := c.DefaultQuery("force", "0") == "1" || strings.EqualFold(c.DefaultQuery("force", ""), "true")

	audit, err := h.offline.RunDecodeAudit(c.Request.Context(), taskID, flowIdx, force)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		} else if strings.Contains(err.Error(), "未找到 ffmpeg") {
			code = http.StatusServiceUnavailable
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, audit)
}

type exportClipRequest struct {
	Flow     int     `json:"flow"`
	StartSec float64 `json:"start_sec"`
	EndSec   float64 `json:"end_sec"`
}

func (h *Handler) ExportOfflineClip(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
	var body exportClipRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	relPath, sizeBytes, err := h.offline.ExportClip(c.Request.Context(), taskID, body.Flow, body.StartSec, body.EndSec)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		} else if strings.Contains(err.Error(), "未找到 ffmpeg") {
			code = http.StatusServiceUnavailable
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	downloadURL := fmt.Sprintf("/api/offline/tasks/%s/files/%s", url.PathEscape(taskID), relPath)
	c.JSON(http.StatusOK, gin.H{
		"rel_path":     relPath,
		"filename":     filepath.Base(relPath),
		"size_bytes":   sizeBytes,
		"download_url": downloadURL,
	})
}

func (h *Handler) CompareOfflineTasks(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskA := strings.TrimSpace(c.Query("task_a"))
	taskB := strings.TrimSpace(c.Query("task_b"))
	if taskA == "" || taskB == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_a and task_b are required"})
		return
	}
	flowA, _ := strconv.Atoi(c.DefaultQuery("flow_a", "0"))
	flowB, _ := strconv.Atoi(c.DefaultQuery("flow_b", "0"))

	res, err := h.offline.CompareTasks(taskA, flowA, taskB, flowB)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) DownloadOfflineReport(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
	flowIdx, _ := strconv.Atoi(c.DefaultQuery("flow", "0"))
	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "html")))

	var (
		data     []byte
		filename string
		ctype    string
		err      error
	)
	switch format {
	case "md", "markdown":
		data, filename, err = h.offline.RenderMarkdownReport(taskID, flowIdx)
		ctype = "text/markdown; charset=utf-8"
	case "html", "":
		data, filename, err = h.offline.RenderHTMLReport(taskID, flowIdx)
		ctype = "text/html; charset=utf-8"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported report format (use html or md)"})
		return
	}
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	if c.DefaultQuery("inline", "0") != "1" {
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	}
	c.Data(http.StatusOK, ctype, data)
}
