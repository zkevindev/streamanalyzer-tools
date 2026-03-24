package handler

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"streamanalyzer/internal/analyzer"
	"streamanalyzer/internal/models"
	"streamanalyzer/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

type Handler struct {
	analyzer *analyzer.StreamAnalyzer
	storage  *storage.CSVStorage
}

func NewHandler(analyzer *analyzer.StreamAnalyzer, storage *storage.CSVStorage) *Handler {
	return &Handler{
		analyzer: analyzer,
		storage:  storage,
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
	}

	r.GET("/", h.Index)
	r.GET("/history", h.HistoryPage)
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

	return chartData, nil
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

func (h *Handler) Index(c *gin.Context) {
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Stream Analyzer</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { 
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #f5f5f5;
            padding: 20px;
            min-height: 100vh;
        }
        .container { 
            max-width: 1200px; 
            margin: 0 auto; 
            background: white;
            padding: 24px;
            border-radius: 8px;
            box-shadow: 0 1px 3px rgba(0,0,0,0.1);
        }
        .header { 
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 24px;
            padding-bottom: 16px;
            border-bottom: 1px solid #e5e5e5;
        }
        h1 { font-size: 22px; color: #333; font-weight: 600; }
        .nav-link { 
            color: #2563eb; 
            text-decoration: none; 
            font-size: 14px;
        }
        .nav-link:hover { text-decoration: underline; }
        h2 { font-size: 16px; color: #333; margin-bottom: 16px; font-weight: 600; }
        .form-row { margin-bottom: 16px; }
        .form-row > .form-group { margin-bottom: 12px; }
        .form-group { flex: 1; }
        .form-group label { 
            display: block; 
            font-size: 13px; 
            color: #666; 
            margin-bottom: 6px; 
        }
        input, select { 
            width: 100%; 
            padding: 8px 12px; 
            border: 1px solid #ddd;
            border-radius: 4px; 
            font-size: 14px;
        }
        input:focus, select:focus { 
            outline: none;
            border-color: #2563eb;
        }
        button { 
            background: #2563eb;
            color: white;
            padding: 8px 16px;
            border: none;
            border-radius: 4px;
            cursor: pointer;
            font-size: 14px;
        }
        button:hover { background: #1d4ed8; }
        button.stop { background: #dc2626; }
        button.stop:hover { background: #b91c1c; }
        button.secondary { background: #6b7280; }
        button.secondary:hover { background: #4b5563; }
        button.success { background: #059669; }
        button.success:hover { background: #047857; }
        button:disabled { background: #9ca3af; cursor: not-allowed; }
        table { 
            width: 100%; 
            border-collapse: collapse;
            font-size: 14px;
        }
        th, td { padding: 12px; text-align: left; border-bottom: 1px solid #e5e5e5; }
        th { background: #f9fafb; color: #666; font-weight: 500; font-size: 12px; }
        tr:hover { background: #f9fafb; }
        .task-id { 
            font-family: monospace;
            background: #f3f4f6;
            padding: 2px 6px;
            border-radius: 3px;
            font-size: 13px;
        }
        .status { 
            display: inline-block;
            padding: 2px 8px;
            border-radius: 3px;
            font-size: 12px;
        }
        .status-running { background: #dcfce7; color: #166534; }
        .status-stopped { background: #fee2e2; color: #991b1b; }
        .actions button { margin-right: 4px; padding: 4px 8px; font-size: 12px; }
        .empty { 
            text-align: center; 
            padding: 40px; 
            color: #9ca3af; 
            font-size: 14px;
        }
        .chart-section { 
            margin-top: 24px;
            padding-top: 24px;
            border-top: 1px solid #e5e5e5;
            display: none;
        }
        .chart-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 16px;
        }
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
            gap: 12px;
            margin-bottom: 16px;
            background: #f9fafb;
            padding: 16px;
            border-radius: 6px;
        }
        .stat-item { text-align: center; }
        .stat-value { 
            font-size: 20px; 
            font-weight: 600; 
            color: #1f2937;
        }
        .stat-label { 
            font-size: 11px; 
            color: #6b7280;
            margin-top: 4px;
        }
        .chart-tabs { 
            display: flex; 
            gap: 8px; 
            margin-bottom: 16px;
            flex-wrap: wrap;
        }
        .chart-tabs button { padding: 6px 12px; font-size: 13px; }
        .chart-tabs button.active { background: #1f2937; }
        .chart-frame { 
            width: 100%; 
            height: 500px; 
            border: 1px solid #e5e5e5;
            border-radius: 4px;
        }
        .chart-table { 
            margin-top: 16px;
            max-height: 300px;
            overflow-y: auto;
            border: 1px solid #e5e5e5;
            border-radius: 4px;
        }
        .chart-table table { 
            width: 100%; 
            border-collapse: collapse; 
            font-size: 12px; 
        }
        .chart-table th, .chart-table td { 
            padding: 8px 12px; 
            text-align: center; 
            border-bottom: 1px solid #e5e5e5; 
        }
        .chart-table th { 
            background: #f9fafb; 
            font-weight: 500; 
            position: sticky;
            top: 0;
        }
        .chart-table th.video { color: #2563eb; }
        .chart-table th.audio { color: #059669; }
        .metadata-box {
            margin-top: 12px;
            background: #0f172a;
            color: #e2e8f0;
            border-radius: 6px;
            padding: 12px;
            max-height: 220px;
            overflow: auto;
            font-size: 12px;
            white-space: pre-wrap;
            word-break: break-all;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Stream Analyzer</h1>
            <a href="/history" class="nav-link">历史记录</a>
        </div>
        
        <div class="form-row">
            <div class="form-group">
                <label>Stream URL</label>
                <input type="text" id="streamUrl" placeholder="rtmp://example.com/live/stream 或 http://example.com/live.flv">
            </div>
            <div class="form-group" style="display: flex; gap: 12px; align-items: flex-end;">
                <div>
                    <label>Type</label>
                    <select id="streamType">
                        <option value="http-flv">HTTP-FLV</option>
                        <option value="rtmp">RTMP</option>
                    </select>
                </div>
                <button onclick="startTask()">开始分析</button>
            </div>
        </div>
        
        <h2>活动任务</h2>
        <table id="taskTable">
            <thead>
                <tr>
                    <th>ID</th>
                    <th>URL</th>
                    <th>Type</th>
                    <th>Status</th>
                    <th>Actions</th>
                </tr>
            </thead>
            <tbody id="taskBody"></tbody>
        </table>
        <div id="emptyState" class="empty" style="display:none;">暂无活动任务</div>
        
        <div id="chartSection" class="chart-section">
            <div class="chart-header">
                <h2>分析结果 - <span id="currentTaskTitle"></span></h2>
                <button class="secondary" onclick="closeChart()">关闭</button>
            </div>
            <div id="statsGrid" class="stats-grid"></div>
            <pre id="metadataBox" class="metadata-box">metadata: -</pre>
            <div class="chart-tabs">
                <button type="button" class="active" data-chart="timestamp" onclick="showChart('timestamp')">DTS</button>
                <button type="button" data-chart="length" onclick="showChart('length')">数据长度</button>
                <button type="button" data-chart="iframe" onclick="showChart('iframe')">I帧间隔</button>
                <button type="button" data-chart="gop" onclick="showChart('gop')">GOP大小</button>
                <button type="button" data-chart="bitrate" onclick="showChart('bitrate')">码率</button>
                <button type="button" data-chart="fps" onclick="showChart('fps')">帧率</button>
                <button type="button" data-chart="avsync" onclick="showChart('avsync')">AV偏差</button>
                <button type="button" class="success" onclick="downloadXLSX()">下载XLSX</button>
            </div>
            <div id="mainChart" class="chart-frame"></div>
            <div id="chartTable" class="chart-table" style="display:none;">
                <table>
                    <thead id="tableHead"></thead>
                    <tbody id="tableBody"></tbody>
                </table>
            </div>
        </div>
    </div>
    
    <script>` + echartsHelpersJS + `</script>
    <script>
        let currentTaskId = null;
        let refreshInterval = null;
        let currentData = null;
        let currentChartType = 'timestamp';
        const stoppingTasks = new Set();
        
        async function startTask() {
            const url = document.getElementById('streamUrl').value;
            const type = document.getElementById('streamType').value;
            if (!url) { alert('请输入流地址'); return; }
            
            try {
                await fetch('/api/task', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ url, type })
                });
                document.getElementById('streamUrl').value = '';
                loadTasks();
            } catch(e) { alert('创建失败: ' + e.message); }
        }
        
        async function stopTask(id) {
            if (stoppingTasks.has(id)) return;
            stoppingTasks.add(id);
            try {
                const res = await fetch('/api/task/' + id, { method: 'DELETE' });
                if (!res.ok) {
                    const text = await res.text();
                    throw new Error(text || ('HTTP ' + res.status));
                }
                if (currentTaskId === id) closeChart();
                await loadTasks();
            } catch (e) {
                alert('停止任务失败: ' + (e && e.message ? e.message : e));
            } finally {
                stoppingTasks.delete(id);
            }
        }
        
        async function loadTasks() {
            const res = await fetch('/api/task');
            const data = await res.json();
            const tbody = document.getElementById('taskBody');
            const empty = document.getElementById('emptyState');
            tbody.innerHTML = '';
            
            if (!data.tasks.length) {
                empty.style.display = 'block';
                return;
            }
            empty.style.display = 'none';
            
            data.tasks.forEach(t => {
                const status = t.status === 'running' ? 'status-running' : 'status-stopped';
                const tr = document.createElement('tr');
                tr.innerHTML = '<td><span class="task-id">' + t.id.substring(5,15) + '...</span></td>' +
                    '<td>' + t.url + '</td>' +
                    '<td>' + t.type.toUpperCase() + '</td>' +
                    '<td><span class="status ' + status + '">' + t.status + '</span></td>' +
                    '<td class="actions">' +
                    '<button class="stop" onclick="stopTask(\'' + t.id + '\')">停止</button>' +
                    '<button onclick="selectTask(\'' + t.id + '\')">分析</button></td>';
                tbody.appendChild(tr);
            });
        }
        
        async function selectTask(id) {
            if (typeof disposeMainChart === 'function') disposeMainChart();
            currentTaskId = id;
            document.getElementById('chartSection').style.display = 'block';
            document.getElementById('currentTaskTitle').textContent = id.substring(5,15) + '...';
            await refreshData();
            showChart('timestamp');
            document.getElementById('chartSection').scrollIntoView({ behavior: 'smooth' });
            
            if (refreshInterval) clearInterval(refreshInterval);
            refreshInterval = setInterval(refreshData, 5000);
        }
        
        function closeChart() {
            if (typeof disposeMainChart === 'function') disposeMainChart();
            document.getElementById('chartSection').style.display = 'none';
            if (refreshInterval) clearInterval(refreshInterval);
            currentTaskId = null;
        }
        
        async function refreshData() {
            if (!currentTaskId) return;
            const res = await fetch('/api/task/' + currentTaskId + '/data');
            const data = await res.json();
            currentData = data;
            updateStats(data);
            if (currentChartType) updateChartTable(currentChartType);
            if (typeof renderClientChart === 'function' && currentData) {
                renderClientChart(currentChartType, currentData, 'mainChart');
            }
        }
        
        function updateStats(d) {
            function videoCodecName(v) {
                const m = {
                    '2': 'H.263',
                    '3': 'ScreenVideo',
                    '4': 'VP6',
                    '5': 'VP6A',
                    '6': 'ScreenVideo2',
                    '7': 'H.264/AVC',
                    '12': 'H.265/HEVC'
                };
                return m[String(v)] || (v ? ('Codec-' + v) : '-');
            }
            function audioCodecName(v) {
                const m = {
                    '2': 'MP3',
                    '7': 'G.711 A-Law',
                    '8': 'G.711 mu-Law',
                    '10': 'AAC',
                    '11': 'Speex',
                    '14': 'MP3-8k'
                };
                return m[String(v)] || (v ? ('Codec-' + v) : '-');
            }
            const intervals = d.iframe_intervals.filter(v => v > 0);
            const avgInt = intervals.length ? Math.round(intervals.reduce((a,b)=>a+b,0)/intervals.length) : 0;
            const avgGop = d.gop_sizes.length ? Math.round(d.gop_sizes.reduce((a,b)=>a+b,0)/d.gop_sizes.length) : 0;
            const iFrames = d.frame_types.filter(f=>f==='I').length;
            const pFrames = d.frame_types.filter(f=>f==='P').length;
            const bFrames = d.frame_types.filter(f=>f==='B').length;
            const stats = d.second_stats;
            const avgVbr = stats.length ? (stats.reduce((a,b)=>a+b.video_bitrate,0)/stats.length).toFixed(1) : 0;
            const avgAbr = stats.length ? (stats.reduce((a,b)=>a+b.audio_bitrate,0)/stats.length).toFixed(1) : 0;
            const avgVfps = stats.length ? Math.round(stats.reduce((a,b)=>a+b.video_fps,0)/stats.length) : 0;
            const avgAfps = stats.length ? Math.round(stats.reduce((a,b)=>a+b.audio_fps,0)/stats.length) : 0;
            const codecFps = d.video_frame_rate && d.video_frame_rate > 0 ? d.video_frame_rate.toFixed(3) : '-';
            const videoCodec = videoCodecName(d.video_codec);
            const audioCodec = audioCodecName(d.audio_codec);
            const resolution = (d.video_width > 0 && d.video_height > 0) ? (d.video_width + 'x' + d.video_height) : '-';
            const sampleRate = d.sample_rate > 0 ? d.sample_rate : '-';
            const channels = d.channels > 0 ? d.channels : '-';
            
            document.getElementById('statsGrid').innerHTML = 
                '<div class="stat-item"><div class="stat-value">' + d.frame_types.length + '</div><div class="stat-label">总帧数</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + iFrames + '/' + pFrames + '/' + bFrames + '</div><div class="stat-label">I/P/B帧</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgInt + '</div><div class="stat-label">I帧间隔(ms)</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgGop + '</div><div class="stat-label">GOP大小</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgVbr + '</div><div class="stat-label">视频码率(Kbps)</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgAbr + '</div><div class="stat-label">音频码率(Kbps)</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgVfps + '</div><div class="stat-label">视频FPS</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgAfps + '</div><div class="stat-label">音频FPS</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + codecFps + '</div><div class="stat-label">编码帧率FPS</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + videoCodec + '</div><div class="stat-label">视频编码</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + audioCodec + '</div><div class="stat-label">音频编码</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + resolution + '</div><div class="stat-label">分辨率</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + sampleRate + '</div><div class="stat-label">采样率</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + channels + '</div><div class="stat-label">声道数</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + stats.length + '</div><div class="stat-label">时长(s)</div></div>';

            const metadataBox = document.getElementById('metadataBox');
            if (metadataBox) {
                if (d.metadata_json) {
                    try {
                        metadataBox.textContent = JSON.stringify(JSON.parse(d.metadata_json), null, 2);
                    } catch (_) {
                        metadataBox.textContent = d.metadata_json;
                    }
                } else {
                    metadataBox.textContent = 'metadata: -';
                }
            }
        }
        
        function showChart(type) {
            currentChartType = type;
            document.querySelectorAll('.chart-tabs button[data-chart]').forEach(function(b) {
                b.classList.toggle('active', b.getAttribute('data-chart') === type);
            });
            if (currentData && typeof renderClientChart === 'function') {
                renderClientChart(type, currentData, 'mainChart');
            }
            updateChartTable(type);
        }
        
        function updateChartTable(type) {
            if (!currentData) return;
            const d = currentData;
            const tableDiv = document.getElementById('chartTable');
            const thead = document.getElementById('tableHead');
            const tbody = document.getElementById('tableBody');
            
            let headers = '';
            let rows = '';
            const stats = d.second_stats || [];
            const maxRows = Number.MAX_SAFE_INTEGER;
            const displayStats = stats;
            const formatTime = (sec) => {
                const d = new Date(Number(sec) * 1000);
                if (Number.isNaN(d.getTime())) return '-';
                const pad = (n) => String(n).padStart(2, '0');
                return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + ' ' +
                    pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
            };
            const startSecond = displayStats.length > 0 ? Number(displayStats[0].second) : 0;
            
            switch(type) {
                case 'timestamp':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>时间戳(ms)</th><th class="video">视频DTS(ms)</th><th class="video">视频PTS(ms)</th><th class="audio">音频DTS(ms)</th></tr>';
                    const vts = d.video_dts || [];
                    const vpts = d.video_pts || [];
                    const ats = d.audio_dts || [];
                    const allTsSet = new Set([].concat(vts, vpts, ats));
                    const allTs = Array.from(allTsSet).filter(v => v !== null && v !== undefined && v !== '').sort((a, b) => Number(a) - Number(b)).slice(0, maxRows);
                    const hasNum = (arr, x) => arr.some(v => Number(v) === Number(x));
                    for (let i = 0; i < allTs.length; i++) {
                        const t = allTs[i];
                        rows += '<tr><td>' + t + '</td><td>' + (hasNum(vts, t) ? t : '-') + '</td><td>' + (hasNum(vpts, t) ? t : '-') + '</td><td>' + (hasNum(ats, t) ? t : '-') + '</td></tr>';
                    }
                    break;
                case 'length':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>序号</th><th class="video">视频长度(bytes)</th><th class="audio">音频长度(bytes)</th></tr>';
                    const vls = d.video_lens || [];
                    const als = d.audio_lens || [];
                    const lsLen = Math.min(Math.max(vls.length, als.length), maxRows);
                    for (let i = 0; i < lsLen; i++) {
                        rows += '<tr><td>' + (i+1) + '</td><td>' + (vls[i] || '-') + '</td><td>' + (als[i] || '-') + '</td></tr>';
                    }
                    break;
                case 'iframe':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>序号</th><th class="video">I帧间隔(ms)</th></tr>';
                    const iframes = (d.iframe_intervals || []).filter(v => v > 0).slice(0, maxRows);
                    for (let i = 0; i < iframes.length; i++) {
                        rows += '<tr><td>' + (i+1) + '</td><td>' + iframes[i] + '</td></tr>';
                    }
                    break;
                case 'gop':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>序号</th><th class="video">GOP大小(帧数)</th></tr>';
                    const gops = (d.gop_sizes || []).slice(0, maxRows);
                    for (let i = 0; i < gops.length; i++) {
                        rows += '<tr><td>' + (i+1) + '</td><td>' + gops[i] + '</td></tr>';
                    }
                    break;
                case 'bitrate':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>时间</th><th class="video">视频码率(Kbps)</th><th class="audio">音频码率(Kbps)</th></tr>';
                    for (let i = 0; i < displayStats.length; i++) {
                        const s = displayStats[i];
                        const rel = Number(s.second) - startSecond;
                        rows += '<tr><td>T+' + rel + 's<br/>' + formatTime(s.second) + '</td><td>' + s.video_bitrate.toFixed(1) + '</td><td>' + s.audio_bitrate.toFixed(1) + '</td></tr>';
                    }
                    break;
                case 'fps':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>时间</th><th class="video">视频FPS</th><th class="audio">音频FPS</th></tr>';
                    for (let i = 0; i < displayStats.length; i++) {
                        const s = displayStats[i];
                        const rel = Number(s.second) - startSecond;
                        rows += '<tr><td>T+' + rel + 's<br/>' + formatTime(s.second) + '</td><td>' + s.video_fps + '</td><td>' + s.audio_fps + '</td></tr>';
                    }
                    break;
                case 'avsync':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>视频DTS(ms)</th><th class="audio">最近音频DTS(ms)</th><th>偏差(ms)</th></tr>';
                    const avVts = (d.video_dts || []).map(Number).filter(v => !Number.isNaN(v));
                    const avAts = (d.audio_dts || []).map(Number).filter(v => !Number.isNaN(v));
                    if (avVts.length === 0 || avAts.length === 0) {
                        rows = '<tr><td colspan="3">暂无可计算的 AV 时间戳数据</td></tr>';
                        break;
                    }
                    let ai = 0;
                    for (let i = 0; i < Math.min(avVts.length, maxRows); i++) {
                        const ts = avVts[i];
                        while (ai + 1 < avAts.length && Math.abs(avAts[ai + 1] - ts) <= Math.abs(avAts[ai] - ts)) ai++;
                        const nearest = avAts[ai];
                        const diff = ts - nearest;
                        rows += '<tr><td>' + ts + '</td><td>' + nearest + '</td><td>' + diff + '</td></tr>';
                    }
                    break;
                default:
                    tableDiv.style.display = 'none';
                    return;
            }
            
            thead.innerHTML = headers;
            tbody.innerHTML = rows;
        }
        
        function downloadXLSX() {
            if (currentTaskId) window.location.href = '/api/task/' + currentTaskId + '/xlsx';
        }
        
        setInterval(loadTasks, 5000);
        loadTasks();
    </script>
</body>
</html>`
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, html)
}

func (h *Handler) HistoryPage(c *gin.Context) {
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Stream Analyzer - History</title>
    <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { 
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #f5f5f5;
            padding: 20px;
            min-height: 100vh;
        }
        .container { 
            max-width: 1200px; 
            margin: 0 auto; 
            background: white;
            padding: 24px;
            border-radius: 8px;
            box-shadow: 0 1px 3px rgba(0,0,0,0.1);
        }
        .header { 
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 24px;
            padding-bottom: 16px;
            border-bottom: 1px solid #e5e5e5;
        }
        h1 { font-size: 22px; color: #333; font-weight: 600; }
        h2 { font-size: 16px; color: #333; margin-bottom: 16px; font-weight: 600; }
        .nav-link { color: #2563eb; text-decoration: none; font-size: 14px; }
        .nav-link:hover { text-decoration: underline; }
        table { width: 100%; border-collapse: collapse; font-size: 14px; }
        th, td { padding: 12px; text-align: left; border-bottom: 1px solid #e5e5e5; }
        th { background: #f9fafb; color: #666; font-weight: 500; font-size: 12px; }
        tr:hover { background: #f9fafb; }
        tr.active { background: #eff6ff; }
        .task-id { font-family: monospace; background: #f3f4f6; padding: 2px 6px; border-radius: 3px; font-size: 13px; }
        .status { display: inline-block; padding: 2px 8px; border-radius: 3px; font-size: 12px; }
        .status-running { background: #dcfce7; color: #166534; }
        .status-stopped { background: #fee2e2; color: #991b1b; }
        button { background: #2563eb; color: white; padding: 6px 12px; border: none; border-radius: 4px; cursor: pointer; font-size: 13px; margin-right: 4px; }
        button:hover { background: #1d4ed8; }
        button.stop { background: #dc2626; }
        button.stop:hover { background: #b91c1c; }
        button.secondary { background: #6b7280; }
        button.secondary:hover { background: #4b5563; }
        button.success { background: #059669; }
        button.success:hover { background: #047857; }
        button.active { background: #1f2937; }
        .empty { text-align: center; padding: 40px; color: #9ca3af; font-size: 14px; }
        .analysis-section { margin-top: 24px; padding-top: 24px; border-top: 1px solid #e5e5e5; display: none; }
        .analysis-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
        .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 12px; margin-bottom: 16px; background: #f9fafb; padding: 16px; border-radius: 6px; }
        .stat-item { text-align: center; }
        .stat-value { font-size: 20px; font-weight: 600; color: #1f2937; }
        .stat-label { font-size: 11px; color: #6b7280; margin-top: 4px; }
        .chart-tabs { display: flex; gap: 8px; margin-bottom: 16px; flex-wrap: wrap; }
        .chart-frame { width: 100%; height: 500px; border: 1px solid #e5e5e5; border-radius: 4px; }
        .chart-table { margin-top: 16px; max-height: 300px; overflow-y: auto; border: 1px solid #e5e5e5; border-radius: 4px; }
        .chart-table table { width: 100%; border-collapse: collapse; font-size: 12px; }
        .chart-table th, .chart-table td { padding: 8px 12px; text-align: center; border-bottom: 1px solid #e5e5e5; }
        .chart-table th { background: #f9fafb; position: sticky; top: 0; }
        .chart-table th.video { color: #2563eb; }
        .chart-table th.audio { color: #059669; }
        .metadata-box {
            margin-top: 12px;
            background: #0f172a;
            color: #e2e8f0;
            border-radius: 6px;
            padding: 12px;
            max-height: 220px;
            overflow: auto;
            font-size: 12px;
            white-space: pre-wrap;
            word-break: break-all;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Stream Analyzer - History</h1>
            <a href="/" class="nav-link">Back</a>
        </div>
        
        <table id="taskTable">
            <thead>
                <tr>
                    <th>ID</th>
                    <th>URL</th>
                    <th>Type</th>
                    <th>Status</th>
                    <th>Actions</th>
                </tr>
            </thead>
            <tbody id="taskBody"></tbody>
        </table>
        <div id="emptyState" class="empty" style="display:none;">No tasks found</div>
        
        <div id="analysisSection" class="analysis-section">
            <div class="analysis-header">
                <h2>Analysis - <span id="currentTaskTitle"></span></h2>
                <button class="secondary" onclick="closeAnalysis()">Close</button>
            </div>
            <div id="statsGrid" class="stats-grid"></div>
            <pre id="metadataBox" class="metadata-box">metadata: -</pre>
            <div class="chart-tabs">
                <button type="button" class="active" data-chart="timestamp" onclick="showChart('timestamp')">DTS</button>
                <button type="button" data-chart="length" onclick="showChart('length')">Length</button>
                <button type="button" data-chart="iframe" onclick="showChart('iframe')">I-Frame</button>
                <button type="button" data-chart="gop" onclick="showChart('gop')">GOP</button>
                <button type="button" data-chart="bitrate" onclick="showChart('bitrate')">Bitrate</button>
                <button type="button" data-chart="fps" onclick="showChart('fps')">FPS</button>
                <button type="button" data-chart="avsync" onclick="showChart('avsync')">AV Sync</button>
                <button type="button" class="success" onclick="downloadXLSX()">XLSX</button>
            </div>
            <div id="mainChart" class="chart-frame"></div>
            <div id="chartTable" class="chart-table" style="display:none;">
                <table>
                    <thead id="tableHead"></thead>
                    <tbody id="tableBody"></tbody>
                </table>
            </div>
        </div>
    </div>
    
    <script>` + echartsHelpersJS + `</script>
    <script>
        let currentTaskId = null;
        let currentData = null;
        let currentChartType = 'timestamp';
        const stoppingTasks = new Set();
        let chartRefreshInterval = null;
        
        async function loadAllTasks() {
            const res = await fetch('/api/task/all');
            const data = await res.json();
            const tbody = document.getElementById('taskBody');
            const empty = document.getElementById('emptyState');
            tbody.innerHTML = '';
            
            if (!data.tasks.length) { empty.style.display = 'block'; return; }
            empty.style.display = 'none';
            
            data.tasks.forEach(t => {
                const status = t.status === 'running' ? 'status-running' : 'status-stopped';
                const tr = document.createElement('tr');
                if (currentTaskId === t.id) tr.classList.add('active');
                tr.innerHTML = '<td><span class="task-id">' + t.id.substring(5,15) + '...</span></td>' +
                    '<td>' + t.url + '</td>' +
                    '<td>' + t.type.toUpperCase() + '</td>' +
                    '<td><span class="status ' + status + '">' + t.status + '</span></td>' +
                    '<td>' +
                    (t.status === 'running' 
                        ? '<button class="stop" onclick="stopTask(\'' + t.id + '\')">Stop</button>'
                        : '<button onclick="analyzeTask(\'' + t.id + '\')">Analyze</button>' +
                          '<button class="success" onclick="downloadXLSXById(\'' + t.id + '\')">XLSX</button>') +
                    '</td>';
                tbody.appendChild(tr);
            });
        }
        
        async function stopTask(id) {
            if (stoppingTasks.has(id)) return;
            stoppingTasks.add(id);
            try {
                const res = await fetch('/api/task/' + id, { method: 'DELETE' });
                if (!res.ok) {
                    const text = await res.text();
                    throw new Error(text || ('HTTP ' + res.status));
                }
                if (currentTaskId === id) closeAnalysis();
                await loadAllTasks();
            } catch (e) {
                alert('Stop failed: ' + (e && e.message ? e.message : e));
            } finally {
                stoppingTasks.delete(id);
            }
        }
        
        async function analyzeTask(id) {
            if (typeof disposeMainChart === 'function') disposeMainChart();
            currentTaskId = id;
            document.getElementById('analysisSection').style.display = 'block';
            document.getElementById('currentTaskTitle').textContent = id.substring(5,15) + '...';
            await loadChartData();
            showChart('timestamp');
            loadAllTasks();
            document.getElementById('analysisSection').scrollIntoView({ behavior: 'smooth' });
            if (chartRefreshInterval) clearInterval(chartRefreshInterval);
            chartRefreshInterval = setInterval(loadChartData, 5000);
        }
        
        function closeAnalysis() {
            if (typeof disposeMainChart === 'function') disposeMainChart();
            document.getElementById('analysisSection').style.display = 'none';
            if (chartRefreshInterval) clearInterval(chartRefreshInterval);
            chartRefreshInterval = null;
            currentTaskId = null;
            loadAllTasks();
        }
        
        async function loadChartData() {
            if (!currentTaskId) return;
            const res = await fetch('/api/task/' + currentTaskId + '/data');
            const d = await res.json();
            currentData = d;
            updateStats(d);
            if (typeof renderClientChart === 'function' && currentData) {
                renderClientChart(currentChartType, currentData, 'mainChart');
            }
            updateChartTable(currentChartType);
        }
        
        function updateStats(d) {
            function videoCodecName(v) {
                const m = {
                    '2': 'H.263',
                    '3': 'ScreenVideo',
                    '4': 'VP6',
                    '5': 'VP6A',
                    '6': 'ScreenVideo2',
                    '7': 'H.264/AVC',
                    '12': 'H.265/HEVC'
                };
                return m[String(v)] || (v ? ('Codec-' + v) : '-');
            }
            function audioCodecName(v) {
                const m = {
                    '2': 'MP3',
                    '7': 'G.711 A-Law',
                    '8': 'G.711 mu-Law',
                    '10': 'AAC',
                    '11': 'Speex',
                    '14': 'MP3-8k'
                };
                return m[String(v)] || (v ? ('Codec-' + v) : '-');
            }
            const intervals = d.iframe_intervals.filter(v => v > 0);
            const avgInt = intervals.length ? Math.round(intervals.reduce((a,b)=>a+b,0)/intervals.length) : 0;
            const avgGop = d.gop_sizes.length ? Math.round(d.gop_sizes.reduce((a,b)=>a+b,0)/d.gop_sizes.length) : 0;
            const iFrames = d.frame_types.filter(f=>f==='I').length;
            const pFrames = d.frame_types.filter(f=>f==='P').length;
            const bFrames = d.frame_types.filter(f=>f==='B').length;
            const stats = d.second_stats;
            const avgVbr = stats.length ? (stats.reduce((a,b)=>a+b.video_bitrate,0)/stats.length).toFixed(1) : 0;
            const avgAbr = stats.length ? (stats.reduce((a,b)=>a+b.audio_bitrate,0)/stats.length).toFixed(1) : 0;
            const avgVfps = stats.length ? Math.round(stats.reduce((a,b)=>a+b.video_fps,0)/stats.length) : 0;
            const avgAfps = stats.length ? Math.round(stats.reduce((a,b)=>a+b.audio_fps,0)/stats.length) : 0;
            const codecFps = d.video_frame_rate && d.video_frame_rate > 0 ? d.video_frame_rate.toFixed(3) : '-';
            const videoCodec = videoCodecName(d.video_codec);
            const audioCodec = audioCodecName(d.audio_codec);
            const resolution = (d.video_width > 0 && d.video_height > 0) ? (d.video_width + 'x' + d.video_height) : '-';
            const sampleRate = d.sample_rate > 0 ? d.sample_rate : '-';
            const channels = d.channels > 0 ? d.channels : '-';
            
            document.getElementById('statsGrid').innerHTML = 
                '<div class="stat-item"><div class="stat-value">' + d.frame_types.length + '</div><div class="stat-label">Frames</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + iFrames + '/' + pFrames + '/' + bFrames + '</div><div class="stat-label">I/P/B</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgInt + '</div><div class="stat-label">I-Interval(ms)</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgGop + '</div><div class="stat-label">GOP Size</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgVbr + '</div><div class="stat-label">Video(Kbps)</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgAbr + '</div><div class="stat-label">Audio(Kbps)</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgVfps + '</div><div class="stat-label">Video FPS</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + avgAfps + '</div><div class="stat-label">Audio FPS</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + codecFps + '</div><div class="stat-label">Codec FPS</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + videoCodec + '</div><div class="stat-label">Video Codec</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + audioCodec + '</div><div class="stat-label">Audio Codec</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + resolution + '</div><div class="stat-label">Resolution</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + sampleRate + '</div><div class="stat-label">Sample Rate</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + channels + '</div><div class="stat-label">Channels</div></div>' +
                '<div class="stat-item"><div class="stat-value">' + stats.length + '</div><div class="stat-label">Duration(s)</div></div>';

            const metadataBox = document.getElementById('metadataBox');
            if (metadataBox) {
                if (d.metadata_json) {
                    try {
                        metadataBox.textContent = JSON.stringify(JSON.parse(d.metadata_json), null, 2);
                    } catch (_) {
                        metadataBox.textContent = d.metadata_json;
                    }
                } else {
                    metadataBox.textContent = 'metadata: -';
                }
            }
        }
        
        function showChart(type) {
            currentChartType = type;
            document.querySelectorAll('.chart-tabs button[data-chart]').forEach(function(b) {
                b.classList.toggle('active', b.getAttribute('data-chart') === type);
            });
            if (currentData && typeof renderClientChart === 'function') {
                renderClientChart(type, currentData, 'mainChart');
            }
            updateChartTable(type);
        }
        
        function updateChartTable(type) {
            if (!currentData) return;
            const d = currentData;
            const tableDiv = document.getElementById('chartTable');
            const thead = document.getElementById('tableHead');
            const tbody = document.getElementById('tableBody');
            let headers = '';
            let rows = '';
            const stats = d.second_stats || [];
            const maxRows = Number.MAX_SAFE_INTEGER;
            const displayStats = stats;
            const formatTime = (sec) => {
                const d = new Date(Number(sec) * 1000);
                if (Number.isNaN(d.getTime())) return '-';
                const pad = (n) => String(n).padStart(2, '0');
                return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + ' ' +
                    pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
            };
            const startSecond = displayStats.length > 0 ? Number(displayStats[0].second) : 0;
            switch(type) {
                case 'timestamp':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>Timestamp(ms)</th><th class="video">Video DTS(ms)</th><th class="video">Video PTS(ms)</th><th class="audio">Audio DTS(ms)</th></tr>';
                    const vts = d.video_dts || [];
                    const vpts = d.video_pts || [];
                    const ats = d.audio_dts || [];
                    const allTsSet = new Set([].concat(vts, vpts, ats));
                    const allTs = Array.from(allTsSet).filter(v => v !== null && v !== undefined && v !== '').sort((a, b) => Number(a) - Number(b)).slice(0, maxRows);
                    const hasNum = (arr, x) => arr.some(v => Number(v) === Number(x));
                    for (let i = 0; i < allTs.length; i++) {
                        const t = allTs[i];
                        rows += '<tr><td>' + t + '</td><td>' + (hasNum(vts, t) ? t : '-') + '</td><td>' + (hasNum(vpts, t) ? t : '-') + '</td><td>' + (hasNum(ats, t) ? t : '-') + '</td></tr>';
                    }
                    break;
                case 'length':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>#</th><th class="video">Video len</th><th class="audio">Audio len</th></tr>';
                    const vls = d.video_lens || [];
                    const als = d.audio_lens || [];
                    const lsLen = Math.min(Math.max(vls.length, als.length), maxRows);
                    for (let i = 0; i < lsLen; i++) {
                        rows += '<tr><td>' + (i+1) + '</td><td>' + (vls[i] || '-') + '</td><td>' + (als[i] || '-') + '</td></tr>';
                    }
                    break;
                case 'iframe':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>#</th><th class="video">I-frame interval(ms)</th></tr>';
                    const iframes = (d.iframe_intervals || []).filter(v => v > 0).slice(0, maxRows);
                    for (let i = 0; i < iframes.length; i++) {
                        rows += '<tr><td>' + (i+1) + '</td><td>' + iframes[i] + '</td></tr>';
                    }
                    break;
                case 'gop':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>#</th><th class="video">GOP size</th></tr>';
                    const gops = (d.gop_sizes || []).slice(0, maxRows);
                    for (let i = 0; i < gops.length; i++) {
                        rows += '<tr><td>' + (i+1) + '</td><td>' + gops[i] + '</td></tr>';
                    }
                    break;
                case 'bitrate':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>Time</th><th class="video">Video Kbps</th><th class="audio">Audio Kbps</th></tr>';
                    for (let i = 0; i < displayStats.length; i++) {
                        const s = displayStats[i];
                        const rel = Number(s.second) - startSecond;
                        rows += '<tr><td>T+' + rel + 's<br/>' + formatTime(s.second) + '</td><td>' + s.video_bitrate.toFixed(1) + '</td><td>' + s.audio_bitrate.toFixed(1) + '</td></tr>';
                    }
                    break;
                case 'fps':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>Time</th><th class="video">Video FPS</th><th class="audio">Audio FPS</th></tr>';
                    for (let i = 0; i < displayStats.length; i++) {
                        const s = displayStats[i];
                        const rel = Number(s.second) - startSecond;
                        rows += '<tr><td>T+' + rel + 's<br/>' + formatTime(s.second) + '</td><td>' + s.video_fps + '</td><td>' + s.audio_fps + '</td></tr>';
                    }
                    break;
                case 'avsync':
                    tableDiv.style.display = 'block';
                    headers = '<tr><th>Video DTS(ms)</th><th class="audio">Nearest Audio DTS(ms)</th><th>Diff(ms)</th></tr>';
                    const avVts = (d.video_dts || []).map(Number).filter(v => !Number.isNaN(v));
                    const avAts = (d.audio_dts || []).map(Number).filter(v => !Number.isNaN(v));
                    if (avVts.length === 0 || avAts.length === 0) {
                        rows = '<tr><td colspan="3">No enough AV timestamps</td></tr>';
                        break;
                    }
                    let ai = 0;
                    for (let i = 0; i < Math.min(avVts.length, maxRows); i++) {
                        const ts = avVts[i];
                        while (ai + 1 < avAts.length && Math.abs(avAts[ai + 1] - ts) <= Math.abs(avAts[ai] - ts)) ai++;
                        const nearest = avAts[ai];
                        const diff = ts - nearest;
                        rows += '<tr><td>' + ts + '</td><td>' + nearest + '</td><td>' + diff + '</td></tr>';
                    }
                    break;
                default:
                    tableDiv.style.display = 'none';
                    return;
            }
            thead.innerHTML = headers;
            tbody.innerHTML = rows;
        }
        
        function downloadXLSX() {
            if (currentTaskId) window.location.href = '/api/task/' + currentTaskId + '/xlsx';
        }
        
        function downloadXLSXById(id) {
            window.location.href = '/api/task/' + id + '/xlsx';
        }
        
        loadAllTasks();
    </script>
</body>
</html>`
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, html)
}
