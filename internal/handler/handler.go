package handler

import (
	"encoding/json"
	"net/http"
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
	offline  *offline.Manager
	hlsStats hlsStatsStore
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
			offlineAPI.GET("/tasks", h.ListOfflineTasks)
			offlineAPI.GET("/tasks/:id", h.GetOfflineTask)
			offlineAPI.GET("/tasks/:id/files/*name", h.DownloadOfflineFile)
		}

		hlsStats := api.Group("/hls-stats")
		{
			hlsStats.POST("/tasks", h.CreateHLSStatsTask)
			hlsStats.GET("/tasks", localDiagnostics, h.ListHLSStatsTasks)
			hlsStats.GET("/tasks/:id", h.GetHLSStatsTask)
			hlsStats.DELETE("/tasks/:id", h.StopHLSStatsTask)
		}
	}

	r.StaticFS("/static", embeddedStaticHTTPFS())
	r.GET("/", func(c *gin.Context) { serveEmbeddedStaticFile(c, "index.html") })
	r.GET("/realtime", func(c *gin.Context) { c.Redirect(http.StatusFound, "/diagnostics") })
	r.GET("/history", func(c *gin.Context) { serveEmbeddedStaticFile(c, "history.html") })
	r.GET("/offline", func(c *gin.Context) { serveEmbeddedStaticFile(c, "offline.html") })
	r.GET("/hls-stats", func(c *gin.Context) {
		target := "/diagnostics?protocol=hls&hls_mode=delivery"
		if task := c.Query("task"); task != "" {
			target += "&task=" + task
		}
		c.Redirect(http.StatusFound, target)
	})
}

func (h *Handler) CreateOfflineTask(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "offline manager not configured"})
		return
	}

	mode := c.PostForm("mode")
	var req models.OfflineTaskRequest
	req.Mode = models.OfflineMode(mode)
	if req.Mode != models.OfflineModeRaw && req.Mode != models.OfflineModePCAP && req.Mode != models.OfflineModeTS && req.Mode != models.OfflineModeFLV && req.Mode != models.OfflineModeMP4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode"})
		return
	}
	if req.Mode == models.OfflineModePCAP {
		if sp := c.PostForm("server_port"); sp != "" {
			if v, err := strconv.Atoi(sp); err == nil && v > 0 && v <= 65535 {
				req.ServerPort = uint16(v)
			}
		}
	}
	// raw/pcap parsing assumes the input raw contains full RTMP handshake.
	// The required length depends on capture direction:
	// - C0+C1+C2 or S0+S1+S2 (both are 3073 bytes).
	if req.Mode == models.OfflineModeRaw || req.Mode == models.OfflineModePCAP {
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.offline.StartTask(task.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, task)
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

func (h *Handler) DownloadOfflineFile(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "offline manager not configured"})
		return
	}
	taskID := c.Param("id")
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
