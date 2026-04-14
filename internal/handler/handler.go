package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"streamanalyzer/internal/analyzer"
	"streamanalyzer/internal/models"
	"streamanalyzer/internal/offline"
	"streamanalyzer/internal/rtmpraw"
	"streamanalyzer/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

type Handler struct {
	analyzer *analyzer.StreamAnalyzer
	storage  *storage.CSVStorage
	offline  *offline.Manager
}

func NewHandler(analyzer *analyzer.StreamAnalyzer, storage *storage.CSVStorage, offlineMgr *offline.Manager) *Handler {
	return &Handler{
		analyzer: analyzer,
		storage:  storage,
		offline:  offlineMgr,
	}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api")
	{
		api.POST("/task", h.CreateTask)
		api.DELETE("/task/:id", h.StopTask)
		api.GET("/task", h.ListTasks)
		api.GET("/task/all", h.ListAllTasks)
		api.GET("/task/:id", h.GetTask)
		api.GET("/task/:id/status", h.GetTaskStatus)
		api.GET("/task/:id/data", h.GetTaskData)
		api.GET("/task/:id/xlsx", h.DownloadXLSX)

		offlineAPI := api.Group("/offline")
		{
			offlineAPI.POST("/tasks", h.CreateOfflineTask)
			offlineAPI.GET("/tasks", h.ListOfflineTasks)
			offlineAPI.GET("/tasks/:id", h.GetOfflineTask)
			offlineAPI.GET("/tasks/:id/files/*name", h.DownloadOfflineFile)
		}
	}

	r.Static("/static", "./internal/handler/static")
	r.GET("/", func(c *gin.Context) { c.File("./internal/handler/static/index.html") })
	r.GET("/realtime", func(c *gin.Context) { c.File("./internal/handler/static/realtime.html") })
	r.GET("/history", func(c *gin.Context) { c.File("./internal/handler/static/history.html") })
	r.GET("/offline", func(c *gin.Context) { c.File("./internal/handler/static/offline.html") })
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

func (h *Handler) CreateTask(c *gin.Context) {
	var req models.TaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := h.analyzer.StartTask(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, task)
}

func (h *Handler) StopTask(c *gin.Context) {
	taskID := c.Param("id")

	if err := h.analyzer.StopTask(taskID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task stopped"})
}

func (h *Handler) ListTasks(c *gin.Context) {
	tasks := h.analyzer.ListTasks()
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

func (h *Handler) ListAllTasks(c *gin.Context) {
	tasks := h.analyzer.ListAllTasks()
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

func (h *Handler) GetTask(c *gin.Context) {
	taskID := c.Param("id")

	task, err := h.analyzer.GetTask(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	xlsxPath := h.storage.GetCSVPath(taskID)
	xlsxExists := false
	if _, err := os.Stat(xlsxPath); err == nil {
		xlsxExists = true
	}

	c.JSON(http.StatusOK, gin.H{
		"task":        task,
		"xlsx_exists": xlsxExists,
		"xlsx_path":   xlsxPath,
	})
}

func (h *Handler) GetTaskStatus(c *gin.Context) {
	taskID := c.Param("id")

	status, err := h.analyzer.GetTaskStatus(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": status})
}

func (h *Handler) GetTaskData(c *gin.Context) {
	taskID := c.Param("id")
	chartData, err := h.readTaskXLSXData(taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, chartData)
}

func (h *Handler) readTaskXLSXData(taskID string) (*models.ChartData, error) {
	// Ensure on-disk snapshot is up-to-date before reading.
	// The analyzer writes rows to in-memory workbook first, then periodic flush.
	h.storage.Flush(taskID)

	xlsxPath := h.storage.GetCSVPath(taskID)

	f, err := excelize.OpenFile(xlsxPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	records, err := f.GetRows("stream")
	if err != nil {
		return nil, err
	}

	chartData := &models.ChartData{
		TaskID: taskID,
	}

	secondStatsMap := make(map[int]*models.SecondStat)

	for i, record := range records {
		if i == 0 {
			continue
		}
		if len(record) < 15 {
			continue
		}

		var dts int64
		if ts, err := strconv.ParseInt(record[0], 10, 64); err == nil {
			dts = ts
		}
		if len(record) > 3 {
			if vw, err := strconv.Atoi(record[3]); err == nil && vw > 0 {
				chartData.VideoWidth = vw
			}
		}
		if len(record) > 4 {
			if vh, err := strconv.Atoi(record[4]); err == nil && vh > 0 {
				chartData.VideoHeight = vh
			}
		}
		if len(record) > 5 && record[5] != "" {
			chartData.VideoCodec = record[5]
		}
		if len(record) > 6 && record[6] != "" {
			chartData.AudioCodec = record[6]
		}
		if len(record) > 7 {
			if sr, err := strconv.Atoi(record[7]); err == nil && sr > 0 {
				chartData.SampleRate = sr
			}
		}
		if len(record) > 8 {
			if ch, err := strconv.Atoi(record[8]); err == nil && ch > 0 {
				chartData.Channels = ch
			}
		}
		var videoLen int64
		if vl, err := strconv.ParseInt(record[1], 10, 64); err == nil {
			videoLen = vl
			chartData.VideoLens = append(chartData.VideoLens, vl)
			if vl > 0 {
				chartData.VideoDTS = append(chartData.VideoDTS, dts)
				if pts, err := strconv.ParseInt(record[9], 10, 64); err == nil {
					chartData.VideoPTS = append(chartData.VideoPTS, pts)
				} else {
					chartData.VideoPTS = append(chartData.VideoPTS, dts)
				}
			}
		}
		var audioLen int64
		if al, err := strconv.ParseInt(record[2], 10, 64); err == nil {
			audioLen = al
			chartData.AudioLens = append(chartData.AudioLens, al)
			if al > 0 {
				chartData.AudioDTS = append(chartData.AudioDTS, dts)
			}
		}
		if videoLen > 0 || audioLen > 0 {
			frameType := record[10]
			chartData.FrameTypes = append(chartData.FrameTypes, frameType)
			if ifInterval, err := strconv.ParseInt(record[12], 10, 64); err == nil {
				chartData.IFrameIntervals = append(chartData.IFrameIntervals, ifInterval)
			}
			if gopSize, err := strconv.Atoi(record[13]); err == nil {
				chartData.GOPSizes = append(chartData.GOPSizes, gopSize)
			}
		}
		if len(record) > 15 {
			if vfr, err := strconv.ParseFloat(record[15], 64); err == nil && vfr > 0 {
				chartData.VideoFrameRate = vfr
			}
		}
		if len(record) > 16 && chartData.MetadataJSON == "" && record[16] != "" {
			chartData.MetadataJSON = record[16]
		}

		if len(record) <= 14 || record[14] == "" {
			// During live write/flush, a partially written row may appear transiently.
			// Skip invalid rows instead of failing the whole API response.
			continue
		}
		recordedAt, err := time.ParseInLocation("2006-01-02 15:04:05", record[14], time.Local)
		if err != nil {
			continue
		}
		second := int(recordedAt.Unix())
		if _, ok := secondStatsMap[second]; !ok {
			secondStatsMap[second] = &models.SecondStat{Second: second}
		}
		stat := secondStatsMap[second]
		if videoLen > 0 {
			stat.VideoBytes += videoLen
			stat.VideoFPS++
		}
		if audioLen > 0 {
			stat.AudioBytes += audioLen
			stat.AudioFPS++
		}
	}

	seconds := make([]int, 0, len(secondStatsMap))
	for sec := range secondStatsMap {
		seconds = append(seconds, sec)
	}
	sort.Ints(seconds)

	for _, sec := range seconds {
		stat := secondStatsMap[sec]
		stat.VideoBitrate = float64(stat.VideoBytes*8) / 1000.0
		stat.AudioBitrate = float64(stat.AudioBytes*8) / 1000.0
		chartData.SecondStats = append(chartData.SecondStats, stat)
	}

	if idx, err := f.GetSheetIndex("hls"); err == nil && idx >= 0 {
		hlsRows, err := f.GetRows("hls")
		if err == nil && len(hlsRows) > 1 {
			for ri := 1; ri < len(hlsRows); ri++ {
				r := hlsRows[ri]
				if len(r) < 19 {
					continue
				}
				seq, _ := strconv.ParseUint(r[1], 10, 64)
				dur, _ := strconv.ParseFloat(r[3], 64)
				sz, _ := strconv.Atoi(r[4])
				seg := models.ChartHLSSegment{
					StreamID:         r[0],
					Seq:              seq,
					URI:              r[2],
					DurationSec:      dur,
					SizeBytes:        sz,
					VideoPTSFirst90k: parseOptionalInt64(r[5]),
					VideoPTSLast90k:  parseOptionalInt64(r[6]),
					VideoDTSFirst90k: parseOptionalInt64(r[7]),
					VideoDTSLast90k:  parseOptionalInt64(r[8]),
					AudioPTSFirst90k: parseOptionalInt64(r[9]),
					AudioPTSLast90k:  parseOptionalInt64(r[10]),
					AudioDTSFirst90k: parseOptionalInt64(r[11]),
					AudioDTSLast90k:  parseOptionalInt64(r[12]),
				}
				if r[13] != "" {
					if d, err := strconv.ParseInt(r[13], 10, 64); err == nil {
						seg.AVDiffPTS90k = d
					}
				}
				seg.AVDiffValid = r[14] == "1"
				seg.PATCount, _ = strconv.Atoi(r[15])
				seg.PMTCount, _ = strconv.Atoi(r[16])
				seg.PESCount, _ = strconv.Atoi(r[17])
				seg.VideoPES, _ = strconv.Atoi(r[18])
				if len(r) > 19 {
					seg.AudioPES, _ = strconv.Atoi(r[19])
				}
				// 新列：av_diff_dts / iframe；旧文件 recorded_at 在 audio_pes 后一列
				if len(r) >= 24 {
					if r[20] != "" {
						if d, err := strconv.ParseInt(r[20], 10, 64); err == nil {
							seg.AVDiffDTS90k = d
						}
					}
					seg.AVDiffDTSValid = r[21] == "1"
					seg.IFrameIntervalsMs = parseSemicolonInt64s(r[22])
				}
				chartData.HLSSegments = append(chartData.HLSSegments, seg)
			}
		}
	}

	return chartData, nil
}

func parseOptionalInt64(s string) int64 {
	if s == "" {
		return -1
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

func parseSemicolonInt64s(s string) []int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (h *Handler) DownloadXLSX(c *gin.Context) {
	taskID := c.Param("id")
	xlsxPath := h.storage.GetCSVPath(taskID)

	if _, err := os.Stat(xlsxPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "XLSX not found"})
		return
	}

	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.xlsx", taskID))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.File(xlsxPath)
}
