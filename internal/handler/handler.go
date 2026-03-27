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

	r.GET("/", h.HomePage)
	r.GET("/realtime", h.Index)
	r.GET("/history", h.HistoryPage)
	r.GET("/offline", h.OfflinePage)
}

func (h *Handler) HomePage(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, indexHTML)
}

func (h *Handler) CreateOfflineTask(c *gin.Context) {
	if h.offline == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "offline manager not configured"})
		return
	}

	mode := c.PostForm("mode")
	var req models.OfflineTaskRequest
	req.Mode = models.OfflineMode(mode)
	if req.Mode != models.OfflineModeRaw && req.Mode != models.OfflineModePCAP && req.Mode != models.OfflineModeTS && req.Mode != models.OfflineModeFLV {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode"})
		return
	}
	if sp := c.PostForm("server_port"); sp != "" {
		if v, err := strconv.Atoi(sp); err == nil && v > 0 && v <= 65535 {
			req.ServerPort = uint16(v)
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

func (h *Handler) OfflinePage(c *gin.Context) {
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>离线解析 - Stream Analyzer</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@400;500;600;700&display=swap" rel="stylesheet">
  <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
  <style>
    *,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
    :root{--bg:#fafaf9;--surface:#ffffff;--border:#e8e6e1;--text:#1a1918;--text-muted:#78716c;--accent:#d97757;--accent-hover:#c4684a;--accent-light:#fef3ec;--radius:1rem;--shadow:0 2px 12px rgba(0,0,0,.05);--shadow-md:0 4px 20px rgba(0,0,0,.08)}
    body{font-family:'Outfit',-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:var(--bg);color:var(--text);min-h:100dvh;padding:2rem;line-height:1.5}
    .container{max-width:1100px;margin:0 auto}
    .page-header{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:2rem;gap:1rem;flex-wrap:wrap}
    .page-title{display:flex;align-items:center;gap:.75rem}
    .page-icon{width:40px;height:40px;background:var(--accent-light);border-radius:10px;display:flex;align-items:center;justify-content:center;color:var(--accent)}
    .page-icon svg{width:20px;height:20px}
    h1{font-size:1.375rem;font-weight:700;letter-spacing:-.02em}
    .page-desc{color:var(--text-muted);font-size:.875rem;margin-top:.25rem}
    .back-link{display:inline-flex;align-items:center;gap:.375rem;color:var(--text-muted);text-decoration:none;font-size:.875rem;padding:.5rem .875rem;border-radius:8px;transition:all .2s ease}
    .back-link:hover{color:var(--text);background:rgba(0,0,0,.04)}
    .back-link svg{width:14px;height:14px}
    .card{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:1.5rem;box-shadow:var(--shadow);margin-bottom:1.5rem}
    .form-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:1rem;align-items:flex-end}
    .form-group{display:flex;flex-direction:column;gap:.375rem}
    label{font-size:.8125rem;font-weight:500;color:var(--text-muted)}
    input[type="text"],input[type="number"],select{padding:.625rem .875rem;border:1px solid var(--border);border-radius:.5rem;font-size:.875rem;background:var(--surface);transition:border-color .2s ease;width:100%}
    input:focus,select:focus{outline:none;border-color:var(--accent)}
    input[type="file"]{padding:.5rem;border:1px dashed var(--border);border-radius:.5rem;background:#fafaf9;font-size:.875rem}
    .btn{display:inline-flex;align-items:center;gap:.5rem;background:var(--accent);color:#fff;text-decoration:none;padding:.625rem 1.125rem;border-radius:.5rem;font-size:.875rem;font-weight:500;border:none;cursor:pointer;transition:all .2s ease;white-space:nowrap}
    .btn:hover{background:var(--accent-hover);transform:translateY(-1px)}
    .btn:active{transform:scale(.98)}
    .btn:disabled{background:#c0bdb8;cursor:not-allowed;transform:none}
    .btn.secondary{background:#1a1918}
    .btn.secondary:hover{background:#2d2c2a}
    .hint{font-size:.75rem;color:var(--text-muted);margin-top:.25rem}
    .hint.tip{background:#fef9f7;padding:.625rem .875rem;border-radius:.5rem;border-left:3px solid var(--accent);margin-top:.75rem}
    #runHint{min-height:1.5rem;font-size:.8125rem;color:var(--text-muted)}
    .table-wrap{overflow-x:auto;border:1px solid var(--border);border-radius:var(--radius);margin-top:1.5rem}
    table{width:100%;border-collapse:collapse;font-size:.8125rem}
    th,td{padding:.75rem 1rem;text-align:left;border-bottom:1px solid var(--border);vertical-align:top}
    th{background:#fafaf9;font-weight:500;font-size:.75rem;color:var(--text-muted);text-transform:uppercase;letter-spacing:.03em}
    tr:last-child td{border-bottom:none}
    tr:hover td{background:#fafaf9}
    tr.running td{background:#fef9f7}
    .pill{display:inline-block;padding:.25rem .625rem;border-radius:999px;font-size:.6875rem;font-weight:500;border:1px solid}
    .pill.running{border-color:#fcd9c4;background:#fef3ec;color:#c4684a}
    .pill.done{border-color:#bbf7d0;background:#f0fdf4;color:#166534}
    .pill.failed{border-color:#fecaca;background:#fef2f2;color:#991b1b}
    .actions{display:flex;gap:.5rem;flex-wrap:wrap}
    .actions a{color:var(--accent);text-decoration:none;font-size:.8125rem;display:inline-flex;align-items:center;gap:.25rem}
    .actions a:hover{text-decoration:underline}
    .detail{margin-top:1.5rem;display:none}
    .detail-header{display:flex;justify-content:space-between;align-items:center;margin-bottom:1.25rem;flex-wrap:wrap;gap:.75rem}
    .detail-title{font-size:1rem;font-weight:600}
    .section{margin-top:1.5rem}
    .section-title{font-size:.875rem;font-weight:600;margin-bottom:.75rem;color:var(--text)}
    .stats-cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(120px,1fr));gap:.75rem}
    .stat-card{border:1px solid var(--border);border-radius:.625rem;padding:.75rem .875rem;background:#fafaf9}
    .stat-card .k{font-size:.75rem;color:var(--text-muted)}
    .stat-card .v{font-size:1.1rem;font-weight:700;margin-top:.2rem}
    .collapse{border:1px solid var(--border);border-radius:.625rem;background:#fff}
    .collapse summary{cursor:pointer;list-style:none;padding:.75rem .875rem;font-size:.8125rem;color:var(--text);font-weight:600}
    .collapse summary::-webkit-details-marker{display:none}
    .collapse summary::after{content:'▼';float:right;font-size:.7rem;color:var(--text-muted)}
    .collapse[open] summary::after{content:'▲'}
    .chart-grid{display:grid;grid-template-columns:1fr;gap:1rem}
    .chart-box{border:1px solid var(--border);border-radius:.625rem;padding:.5rem;background:#fff}
    .chart-title{font-size:.8125rem;color:var(--text-muted);margin:.25rem .5rem .5rem}
    .chart{height:260px}
    .meta-box{margin-top:.5rem;border:1px solid var(--border);border-radius:.625rem;background:#1f2937;color:#e5e7eb;padding:.75rem;max-height:260px;overflow:auto;font-size:.75rem;white-space:pre-wrap;word-break:break-all}
    .kv{display:grid;grid-template-columns:160px 1fr;gap:.5rem;font-size:.8125rem}
    .kv>div{padding:.5rem .75rem;border:1px solid var(--border);border-radius:.5rem}
    .kv .k{background:#fafaf9;color:var(--text-muted)}
    code{font-family:ui-monospace,monospace;font-size:.8125rem;background:#f3f4f6;padding:.125rem .375rem;border-radius:.25rem}
    .empty-state{text-align:center;padding:3rem;color:var(--text-muted);font-size:.875rem}
    @media(max-width:640px){body{padding:1rem}.form-grid{grid-template-columns:1fr}.page-header{flex-direction:column}.chart-grid{grid-template-columns:1fr}}
  </style>
</head>
<body>
  <div class="container">
    <div class="page-header">
      <div class="page-title">
        <div class="page-icon">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" d="M20.25 6.375c0 2.278-3.694 4.125-8.25 4.125S3.75 8.653 3.75 6.375m16.5 0c0-2.278-3.694-4.125-8.25-4.125S3.75 4.097 3.75 6.375m16.5 0v11.25c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125V6.375m16.5 0v3.75m-16.5-3.75v3.75m16.5 0v3.75C20.25 16.153 16.556 18 12 18s-8.25-1.847-8.25-4.125v-3.75m16.5 0c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125" />
          </svg>
        </div>
        <div>
          <h1>离线文件解析</h1>
          <p class="page-desc">上传 raw 或 pcap/pcapng，解析并生成 push/pull 子目录产物</p>
        </div>
      </div>
      <a href="/" class="back-link">
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" d="M10.5 19.5 3 12m0 0 7.5-7.5M3 12h18" />
        </svg>
        返回首页
      </a>
    </div>

    <div class="card">
      <div class="form-grid">
        <div class="form-group">
          <label>模式</label>
          <select id="mode">
            <option value="pcap">pcap/pcapng</option>
            <option value="raw">raw（单方向，含握手）</option>
            <option value="ts">ts（MPEG-TS）</option>
            <option value="flv">flv（文件）</option>
          </select>
        </div>
        <div class="form-group">
          <label>serverPort（pcap）</label>
          <input id="serverPort" type="number" value="1935" />
        </div>
        <div class="form-group">
          <label>文件</label>
          <input id="file" type="file" />
        </div>
        <button id="btn" class="btn" onclick="upload()">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" width="16" height="16">
            <path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5m-13.5-9L12 3m0 0 4.5 4.5M12 3v13.5" />
          </svg>
          开始解析
        </button>
        <div id="runHint"></div>
      </div>
      <p class="hint tip" id="modeHint">提示：离线解析要求输入包含完整 RTMP 握手（C0+C1+C2 或 S0+S1+S2，合计 3073 字节）。若缺失握手，可能会解析失败。</p>
    </div>

    <div class="card">
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>ID</th><th>mode</th><th>status</th><th>created</th><th>error</th><th>操作</th>
            </tr>
          </thead>
          <tbody id="tbody"></tbody>
        </table>
      </div>
      <div id="emptyState" class="empty-state" style="display:none">暂无解析任务</div>
    </div>

    <div id="detail" class="detail">
      <div class="card">
        <div class="detail-header">
          <div class="detail-title">任务详情</div>
          <div class="actions" id="taskActions"></div>
        </div>

        <div class="section">
          <div class="section-title">解析信息</div>
          <div class="kv" id="taskKV"></div>
        </div>

        <div class="section" id="offlineChartSection" style="display:none">
          <div class="section-title">时序分析</div>
          <div class="chart-grid">
            <div class="chart-box">
              <div class="chart-title">I帧间隔</div>
              <div id="chartIInterval" class="chart"></div>
            </div>
          </div>
        </div>
        <div class="section" id="flvMetadataSection" style="display:none">
          <div class="section-title">FLV Metadata（onMetaData）</div>
          <pre id="flvMetadataBox" class="meta-box">metadata: -</pre>
        </div>

        <div class="section" id="tsStatsSection" style="display:none">
          <div class="section-title">TS 解析统计</div>
          <div class="stats-cards" id="tsStatsCards"></div>
        </div>
        <div class="section" id="tsPidSection" style="display:none">
          <div class="section-title">TS PID 明细</div>
          <details class="collapse" open>
            <summary id="tsPidSummary">PID 明细</summary>
            <div class="table-wrap" style="margin-top:0">
              <table class="small">
                <thead>
                  <tr>
                    <th>PID</th><th>stream_type</th><th>category</th><th>PES</th><th>NALU</th><th>bytes</th><th>输出文件</th>
                  </tr>
                </thead>
                <tbody id="tsPidBody"></tbody>
              </table>
            </div>
          </details>
        </div>

        <div class="section" id="rawSection">
          <div class="section-title">Raw 解析结果（单方向）</div>
          <div class="hint" style="margin:.5rem 0" id="rawHint">仅 raw 模式展示</div>
          <div class="table-wrap">
            <table class="small">
              <thead>
                <tr>
                  <th>flow</th><th>client → server</th><th>SYN</th><th>pkts</th><th>payload</th><th>dump</th><th>codec</th><th>产物</th><th>err</th>
                </tr>
              </thead>
              <tbody id="flowRaw"></tbody>
            </table>
          </div>
        </div>

        <div class="section" id="rawFrameSection" style="display:none">
          <div class="section-title" id="mediaDetailMainTitle">媒体包明细</div>
          <p id="rawFrameModeHint" class="hint tip" style="display:none"></p>

          <div id="tsPesWrap" style="display:none">
            <div class="section-title" style="font-size:.875rem;margin-top:.5rem">TS PES 明细</div>
            <p id="tsPesTruncHint" class="hint tip" style="display:none"></p>
            <details class="collapse" open>
              <summary id="tsPesSummary">PES 包列表</summary>
              <div class="table-wrap" style="margin-top:0;max-height:420px;overflow:auto">
                <table class="small">
                  <thead>
                    <tr>
                      <th>#</th><th>PID</th><th>prog</th><th>stream_type</th><th>cat</th><th>sid</th>
                      <th>PTS(90k)</th><th>DTS(90k)</th><th>len</th><th>NALU#</th><th>NALU 摘要</th>
                    </tr>
                  </thead>
                  <tbody id="tsPesBody"></tbody>
                </table>
              </div>
            </details>
          </div>

          <div id="rawFrameWrap" style="display:none">
            <div class="section-title" style="font-size:.875rem;margin-top:.5rem">Raw 逐帧明细（RTMP 裸流）</div>
            <details id="rawFrameDetails" class="collapse" open>
              <summary id="rawFrameSummary">音视频帧明细</summary>
              <div class="table-wrap" style="margin-top:0;max-height:420px;overflow:auto">
                <table class="small">
                  <thead>
                    <tr>
                      <th>#</th><th>媒体</th><th>DTS</th><th>PTS</th><th>长度</th><th>帧类型</th>
                    </tr>
                  </thead>
                  <tbody id="rawFrameBody"></tbody>
                </table>
              </div>
            </details>
          </div>
        </div>

        <div class="section" id="pushSection">
          <div class="section-title">推流列表（push）</div>
          <div class="table-wrap">
            <table class="small">
              <thead>
                <tr>
                  <th>flow</th><th>client → server</th><th>SYN</th><th>pkts</th><th>payload</th><th>dump</th><th>codec</th><th>产物</th><th>err</th>
                </tr>
              </thead>
              <tbody id="flowPush"></tbody>
            </table>
          </div>
        </div>

        <div class="section" id="pullSection">
          <div class="section-title">拉流列表（pull）</div>
          <div class="table-wrap">
            <table class="small">
              <thead>
                <tr>
                  <th>flow</th><th>client → server</th><th>SYN</th><th>pkts</th><th>payload</th><th>dump</th><th>codec</th><th>产物</th><th>err</th>
                </tr>
              </thead>
              <tbody id="flowPull"></tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  </div>

  <script>
    let currentId = null;
    function setRunHint(msg){
      const el = document.getElementById('runHint');
      if(!el) return;
      el.textContent = msg || '';
    }
    function sleep(ms){ return new Promise(r=>setTimeout(r, ms)); }
    function updateModeHint(){
      const modeEl = document.getElementById('mode');
      const hintEl = document.getElementById('modeHint');
      const serverPortWrap = document.getElementById('serverPort').closest('.form-group');
      if(!modeEl || !hintEl) return;
      const mode = modeEl.value;
      if(serverPortWrap){
        serverPortWrap.style.display = (mode === 'pcap') ? '' : 'none';
      }
      if(mode === 'ts'){
        hintEl.textContent = '提示：TS 模式会解析 PAT/PMT/PES，导出各 PID 对应的 ES 文件，并统计 NALU。';
      }else if(mode === 'flv'){
        hintEl.textContent = '提示：FLV 模式会解析音视频 Tag、metadata，并提取 video.annexb / audio.adts.aac。';
      }else{
        hintEl.textContent = '提示：离线解析要求输入包含完整 RTMP 握手（C0+C1+C2 或 S0+S1+S2，合计 3073 字节）。若缺失握手，可能会解析失败。';
      }
    }
    async function upload(){
      const f = document.getElementById('file').files[0];
      if(!f){ alert('请选择文件'); return; }
      const btn = document.getElementById('btn'); btn.disabled = true;
      setRunHint('上传中…（文件较大时可能需要几十秒）');
      const fd = new FormData();
      fd.append('mode', document.getElementById('mode').value);
      fd.append('server_port', document.getElementById('serverPort').value);
      fd.append('file', f);
      let res;
      try{
        res = await fetch('/api/offline/tasks', { method:'POST', body: fd });
      }catch(e){
        btn.disabled = false;
        setRunHint('');
        alert('请求失败：' + (e && e.message ? e.message : e));
        return;
      }
      if(!res.ok){
        btn.disabled = false;
        setRunHint('');
        alert(await res.text());
        return;
      }
      const task = await res.json();
      btn.disabled = false;
      setRunHint('后端解析中…任务ID：' + (task.id||'-'));
      await loadTasks();
      if(task && task.id){
        await show(task.id);
        await waitUntilDone(task.id);
      }
    }
    async function waitUntilDone(id){
      const deadline = Date.now() + 10*60*1000;
      while(Date.now() < deadline){
        await sleep(1000);
        const res = await fetch('/api/offline/tasks/'+id);
        if(!res.ok) continue;
        const data = await res.json();
        const st = (data.task && data.task.status) ? String(data.task.status) : '';
        if(st === 'done'){
          setRunHint('解析完成：' + id);
          await loadTasks();
          await show(id);
          return;
        }
        if(st === 'failed'){
          const err = (data.task && data.task.error) ? String(data.task.error) : 'unknown';
          setRunHint('解析失败：' + id);
          await loadTasks();
          await show(id);
          alert('解析失败：' + err);
          return;
        }
      }
      setRunHint('仍在解析中（超时停止自动提示）：' + id);
    }
    async function loadTasks(){
      const res = await fetch('/api/offline/tasks');
      const data = await res.json();
      const tb = document.getElementById('tbody');
      const empty = document.getElementById('emptyState');
      tb.innerHTML = '';
      const tasks = (data.tasks||[]).sort((a,b)=>String(b.created_at).localeCompare(String(a.created_at)));
      if(!tasks.length){ empty.style.display='block'; return; }
      empty.style.display='none';
      tasks.forEach(t=>{
        const tr = document.createElement('tr');
        if(String(t.status).toLowerCase() === 'running') tr.classList.add('running');
        tr.innerHTML =
          '<td><code>'+t.id+'</code></td>'+
          '<td>'+t.mode+'</td>'+
          '<td><span class="pill '+String(t.status||'')+'">'+t.status+'</span></td>'+
          '<td>'+t.created_at+'</td>'+
          '<td class="muted">'+(t.error||'')+'</td>';
        const tdBtn = document.createElement('td');
        const btn = document.createElement('button');
        btn.className = 'btn secondary';
        btn.textContent = '查看';
        btn.onclick = ()=>show(t.id);
        tdBtn.appendChild(btn);
        tr.appendChild(tdBtn);
        tb.appendChild(tr);
      });
    }
    function toRel(id, p){
      if(!p) return '';
      const key = '/offline/'+id+'/';
      const idx = p.indexOf(key);
      if(idx < 0) return '';
      return p.slice(idx + key.length).replace(/^\/+/, '');
    }
    function fmtBytes(n){
      if(n === undefined || n === null) return '-';
      const v = Number(n);
      if(!isFinite(v)) return String(n);
      if(v < 1024) return v + ' B';
      if(v < 1024*1024) return (v/1024).toFixed(2) + ' KB';
      if(v < 1024*1024*1024) return (v/1024/1024).toFixed(2) + ' MB';
      return (v/1024/1024/1024).toFixed(2) + ' GB';
    }
    function kvLine(k, v){
      const kk = document.createElement('div'); kk.className='k'; kk.textContent = k;
      const vv = document.createElement('div'); vv.textContent = (v===undefined || v===null || v==='') ? '-' : String(v);
      return [kk, vv];
    }
    function renderTSStats(flows){
      const section = document.getElementById('tsStatsSection');
      const box = document.getElementById('tsStatsCards');
      if(!section || !box) return;
      const sum = (key) => (flows||[]).reduce((acc, f)=>acc + Number(f && f[key] ? f[key] : 0), 0);
      const stats = [
        {k:'PAT', v:sum('pat_count')},
        {k:'PMT', v:sum('pmt_count')},
        {k:'Program', v:sum('program_count')},
        {k:'PES', v:sum('pes_count')},
        {k:'NALU', v:sum('nalu_count')},
        {k:'视频PID', v:sum('video_pid_count')},
        {k:'音频PID', v:sum('audio_pid_count')}
      ];
      box.innerHTML = '';
      stats.forEach(s=>{
        const card = document.createElement('div');
        card.className = 'stat-card';
        const k = document.createElement('div');
        k.className = 'k';
        k.textContent = s.k;
        const v = document.createElement('div');
        v.className = 'v';
        v.textContent = String(s.v);
        card.appendChild(k);
        card.appendChild(v);
        box.appendChild(card);
      });
      section.style.display = '';
    }
    function renderTSPIDs(taskId, flows){
      const section = document.getElementById('tsPidSection');
      const body = document.getElementById('tsPidBody');
      const summary = document.getElementById('tsPidSummary');
      if(!section || !body || !summary) return;
      const all = [];
      (flows||[]).forEach(f=>{
        const items = Array.isArray(f.pid_details) ? f.pid_details : [];
        items.forEach(it=>all.push(it));
      });
      all.sort((a,b)=>Number(a.pid||0)-Number(b.pid||0));
      body.innerHTML = '';
      const base = '/api/offline/tasks/'+taskId+'/files/';
      all.forEach(it=>{
        const tr = document.createElement('tr');
        const rel = toRel(taskId, it.output_path || '');
        const linkCell = document.createElement('td');
        if(rel){
          const a = document.createElement('a');
          a.href = base + rel;
          a.target = '_blank';
          a.textContent = rel;
          linkCell.appendChild(a);
        }else{
          linkCell.textContent = '-';
        }
        tr.innerHTML =
          '<td>'+String(it.pid ?? '-')+'</td>'+
          '<td>'+String(it.stream_type || '-')+'</td>'+
          '<td>'+String(it.category || '-')+'</td>'+
          '<td>'+String(it.pes_count ?? 0)+'</td>'+
          '<td>'+String(it.nalu_count ?? 0)+'</td>'+
          '<td>'+fmtBytes(it.bytes ?? 0)+'</td>';
        tr.appendChild(linkCell);
        body.appendChild(tr);
      });
      summary.textContent = 'PID 明细（共 ' + all.length + ' 项）';
      section.style.display = '';
    }
    function fmt90kTick(valid, base){
      if(!valid) return 'N/A';
      const v = Number(base);
      if(!isFinite(v)) return 'N/A';
      return v + ' (' + (v/90000).toFixed(3) + 's)';
    }
    function naluBriefLine(nalus){
      if(!nalus || !nalus.length) return '-';
      return nalus.slice(0, 10).map(function(n){
        const name = n.type_name ? '(' + n.type_name + ')' : '';
        return (n.codec || '') + ' t=' + String(n.type) + name + (n.key ? '*' : '');
      }).join(', ');
    }
    function renderTSPESDetails(flows){
      const section = document.getElementById('rawFrameSection');
      const tsWrap = document.getElementById('tsPesWrap');
      const rawWrap = document.getElementById('rawFrameWrap');
      const body = document.getElementById('tsPesBody');
      const summary = document.getElementById('tsPesSummary');
      const trunc = document.getElementById('tsPesTruncHint');
      const hint = document.getElementById('rawFrameModeHint');
      const title = document.getElementById('mediaDetailMainTitle');
      if(!section || !tsWrap || !rawWrap || !body || !summary) return;
      if(hint){ hint.style.display = 'none'; hint.textContent = ''; }
      if(title){ title.textContent = '媒体包明细'; }
      rawWrap.style.display = 'none';
      tsWrap.style.display = '';
      const all = [];
      (flows||[]).forEach(function(f){
        const items = Array.isArray(f.pes_details) ? f.pes_details : [];
        items.forEach(function(it){ all.push(it); });
      });
      const total = (flows && flows[0] && flows[0].pes_detail_total != null) ? Number(flows[0].pes_detail_total) : all.length;
      let truncFlag = false;
      (flows||[]).forEach(function(f){
        if(f && f.pes_details_truncated) truncFlag = true;
      });
      if(trunc){
        if(truncFlag){
          trunc.style.display = '';
          trunc.textContent = 'PES 明细已截断：共 ' + (isFinite(total)?total:all.length) + ' 个 PES，页面最多展示前 ' + all.length + ' 条（可在 summary.json 中调高后端上限）。';
        }else{
          trunc.style.display = 'none';
          trunc.textContent = '';
        }
      }
      body.innerHTML = '';
      for(let i=0;i<all.length;i++){
        const it = all[i] || {};
        const tr = document.createElement('tr');
        tr.innerHTML =
          '<td>' + String(it.seq ?? (i+1)) + '</td>' +
          '<td>' + String(it.pid ?? '-') + '</td>' +
          '<td>' + String(it.program_number ?? '-') + '</td>' +
          '<td>' + String(it.stream_type || '-') + '</td>' +
          '<td>' + String(it.category || '-') + '</td>' +
          '<td>0x' + Number(it.stream_id ?? 0).toString(16) + '</td>' +
          '<td>' + fmt90kTick(it.pts_valid, it.pts_base) + '</td>' +
          '<td>' + fmt90kTick(it.dts_valid, it.dts_base) + '</td>' +
          '<td>' + fmtBytes(it.payload_len ?? 0) + '</td>' +
          '<td>' + String(it.nalu_count ?? 0) + '</td>' +
          '<td style="max-width:280px;word-break:break-all;font-size:.75rem">' + naluBriefLine(it.nalus) + '</td>';
        body.appendChild(tr);
      }
      if(all.length === 0){
        const tr = document.createElement('tr');
        tr.innerHTML = '<td colspan="11" style="color:var(--text-muted)">暂无 PES 明细（请重新解析 TS 任务以生成 pes_details）。</td>';
        body.appendChild(tr);
      }
      summary.textContent = 'PES 包列表（展示 ' + all.length + ' / ' + (isFinite(total)?total:all.length) + '）';
      section.style.display = '';
    }
    function renderRawFrames(flows){
      const section = document.getElementById('rawFrameSection');
      const body = document.getElementById('rawFrameBody');
      const summary = document.getElementById('rawFrameSummary');
      const hint = document.getElementById('rawFrameModeHint');
      const details = document.getElementById('rawFrameDetails');
      const tsWrap = document.getElementById('tsPesWrap');
      const rawWrap = document.getElementById('rawFrameWrap');
      const title = document.getElementById('mediaDetailMainTitle');
      if(!section || !body || !summary) return;
      if(hint){ hint.style.display = 'none'; hint.textContent = ''; }
      if(title){ title.textContent = '媒体包明细'; }
      if(tsWrap){ tsWrap.style.display = 'none'; }
      if(rawWrap){ rawWrap.style.display = ''; }
      if(details){ details.style.display = ''; }
      const all = [];
      (flows||[]).forEach(f=>{
        const items = Array.isArray(f.frame_details) ? f.frame_details : [];
        items.forEach(it=>all.push(it));
      });
      body.innerHTML = '';
      if(all.length === 0){
        const tr = document.createElement('tr');
        tr.innerHTML = '<td colspan="6" style="color:var(--text-muted)">暂无逐帧数据（旧任务或未解析出音视频的 raw 文件会为空）。请用「模式：raw」重新上传 RTMP 单方向裸流。</td>';
        body.appendChild(tr);
      }else{
        for(let i=0;i<all.length;i++){
          const it = all[i] || {};
          const tr = document.createElement('tr');
          tr.innerHTML =
            '<td>' + (i+1) + '</td>' +
            '<td>' + String(it.media_type || '-') + '</td>' +
            '<td>' + String(it.dts ?? '-') + '</td>' +
            '<td>' + String(it.pts ?? '-') + '</td>' +
            '<td>' + fmtBytes(it.frame_len ?? 0) + '</td>' +
            '<td>' + String(it.frame_type || '-') + '</td>';
          body.appendChild(tr);
        }
      }
      summary.textContent = '音视频帧明细（共 ' + all.length + ' 帧）';
      section.style.display = '';
    }
    function clearTbody(id){
      const tb = document.getElementById(id);
      tb.innerHTML = '';
      return tb;
    }
    function addLink(container, href, label){
      const a = document.createElement('a');
      a.href = href;
      a.textContent = label;
      a.target = '_blank';
      container.appendChild(a);
      return a;
    }
    function addSep(container){
      const span = document.createElement('span');
      span.textContent = ' | ';
      span.className = 'muted';
      container.appendChild(span);
    }
    function renderFlows(taskId, flows, tbodyId, withDir){
      const tb = clearTbody(tbodyId);
      const base = '/api/offline/tasks/'+taskId+'/files/';
      (flows||[]).forEach(f=>{
        const tr = document.createElement('tr');
        const client = (f.client_ip||'-')+':'+(f.client_port||'-');
        const server = (f.server_ip||'-')+':'+(f.server_port||'-');
        const links = document.createElement('div');
        const isTS = String(f.direction||'').toLowerCase() === 'ts';
        const rawRel = toRel(taskId, f.raw_path);
        const vRel = toRel(taskId, f.video_path);
        const aRel = toRel(taskId, f.audio_path);
        const extras = Array.isArray(f.artifact_paths) ? f.artifact_paths : [];
        let linkCount = 0;
        // TS：产物列只保留 video / audio（与 PID 明细一致，避免 raw 与 artifact 重复）
        if(!isTS && rawRel){ if(linkCount++) addSep(links); addLink(links, base+rawRel, 'raw'); }
        if(vRel){ if(linkCount++) addSep(links); addLink(links, base+vRel, 'video'); }
        if(aRel){ if(linkCount++) addSep(links); addLink(links, base+aRel, 'audio'); }
        if(!isTS){
          for(let i=0;i<extras.length;i++){
            const rel = toRel(taskId, extras[i]);
            if(!rel) continue;
            if(linkCount++) addSep(links);
            addLink(links, base+rel, 'artifact'+(i+1));
          }
        }

        const tdFlow = document.createElement('td'); tdFlow.textContent = 'flow'+String(f.flow_id).padStart(3,'0');
        const tdAddr = document.createElement('td'); tdAddr.textContent = client + ' → ' + server;
        const tdSyn = document.createElement('td'); tdSyn.textContent = (f.has_syn ? ('Y('+ (f.syn_count||0) +')') : 'N');
        const tdPkts = document.createElement('td'); tdPkts.textContent = String(f.tcp_pkt_count ?? '-');
        const tdPayload = document.createElement('td'); tdPayload.textContent = fmtBytes(f.payload_bytes);
        tr.appendChild(tdFlow); tr.appendChild(tdAddr); tr.appendChild(tdSyn); tr.appendChild(tdPkts); tr.appendChild(tdPayload);
        if(withDir){
          const tdDir = document.createElement('td'); tdDir.textContent = f.direction || '-';
          tr.appendChild(tdDir);
        }
        const tdDump = document.createElement('td'); tdDump.textContent = f.dump_raw_dir || '-';
        const tdCodec = document.createElement('td'); tdCodec.textContent = f.video_codec || '-';
        const tdLinks = document.createElement('td'); tdLinks.appendChild(links);
        const tdErr = document.createElement('td'); tdErr.textContent = f.error || '';
        tr.appendChild(tdDump); tr.appendChild(tdCodec); tr.appendChild(tdLinks); tr.appendChild(tdErr);
        tb.appendChild(tr);
      });
    }
    let iIntervalChart = null;
    function destroyOfflineCharts(){
      if(iIntervalChart){
        try{ iIntervalChart.dispose(); }catch(_){}
        iIntervalChart = null;
      }
    }
    function pickTSDTS(item){
      if(!item) return null;
      if(item.dts_valid) return Number(item.dts_base);
      return null;
    }
    function calcOfflineSeries(mode, flows){
      const iIntervals = [];
      if(mode === 'ts'){
        const pes = [];
        (flows || []).forEach(function(f){
          const items = Array.isArray(f.pes_details) ? f.pes_details : [];
          items.forEach(function(it){ pes.push(it); });
        });
        pes.sort(function(a,b){ return Number(a.seq || 0) - Number(b.seq || 0); });
        let lastI = null;
        pes.forEach(function(p){
          const cat = String(p.category || '').toLowerCase();
          const ts = pickTSDTS(p);
          if(ts == null || !isFinite(ts)) return;
          if(cat === 'video'){
            const nalus = Array.isArray(p.nalus) ? p.nalus : [];
            const isI = nalus.some(function(n){ return !!n.key; }) ||
              nalus.some(function(n){ return String(n.type_name || '').toUpperCase() === 'IDR'; });
            if(isI){
              if(lastI != null) iIntervals.push((ts - lastI) / 90); // 90k tick -> ms
              lastI = ts;
            }
          }
        });
      }else{
        const frames = [];
        (flows || []).forEach(function(f){
          const items = Array.isArray(f.frame_details) ? f.frame_details : [];
          items.forEach(function(it){ frames.push(it); });
        });
        let lastI = null;
        frames.forEach(function(fr){
          const media = String(fr.media_type || '').toLowerCase();
          const dts = Number(fr.dts);
          const ts = isFinite(dts) ? dts : null;
          if(ts == null) return;
          if(media === 'video'){
            const ft = String(fr.frame_type || '').toUpperCase();
            if(ft.startsWith('I')){
              if(lastI != null) iIntervals.push(ts - lastI);
              lastI = ts;
            }
          }
        });
      }
      return { iIntervals };
    }
    function renderOfflineCharts(mode, flows){
      const section = document.getElementById('offlineChartSection');
      const iEl = document.getElementById('chartIInterval');
      if(!section || !iEl) return;
      destroyOfflineCharts();
      if(typeof echarts === 'undefined'){
        section.style.display = 'none';
        return;
      }
      const series = calcOfflineSeries(mode, flows);
      iIntervalChart = echarts.init(iEl);
      iIntervalChart.setOption({
        tooltip:{trigger:'axis'},
        xAxis:{type:'category', data:series.iIntervals.map(function(_,i){ return i+1; }), name:'I间隔序号'},
        yAxis:{type:'value', name:'ms'},
        series:[{type:'line', smooth:true, data:series.iIntervals}]
      });
      section.style.display = '';
    }
    function renderFLVMetadata(flows){
      const section = document.getElementById('flvMetadataSection');
      const box = document.getElementById('flvMetadataBox');
      if(!section || !box) return;
      let raw = '';
      (flows || []).forEach(function(f){
        if(!raw && f && f.flv_metadata_json){
          raw = String(f.flv_metadata_json);
        }
      });
      if(!raw){
        box.textContent = 'metadata: -';
        section.style.display = 'none';
        return;
      }
      try{
        box.textContent = JSON.stringify(JSON.parse(raw), null, 2);
      }catch(_){
        box.textContent = raw;
      }
      section.style.display = '';
    }
    async function show(id){
      currentId = id;
      document.getElementById('detail').style.display = 'block';
      const res = await fetch('/api/offline/tasks/'+id);
      const data = await res.json();
      const task = data.task || {};
      const summary = data.summary || {};
      const flows = (summary.flows || []);

      const act = document.getElementById('taskActions');
      act.innerHTML = '';
      const base = '/api/offline/tasks/'+id+'/files/';
      const inputRel = toRel(id, task.input_path);
      const summaryRel = toRel(id, task.summary_path);
      if(inputRel) addLink(act, base+inputRel, '下载 input');
      addLink(act, base+'summary.txt', '下载 summary.txt');
      if(summaryRel) addLink(act, base+summaryRel, '下载 summary.json');

      const kv = document.getElementById('taskKV');
      kv.innerHTML = '';
      const lines = [];
      lines.push(kvLine('task_id', task.id));
      lines.push(kvLine('mode', task.mode));
      lines.push(kvLine('status', task.status));
      lines.push(kvLine('server_port', task.server_port));
      lines.push(kvLine('input_name', task.input_name));
      lines.push(kvLine('created_at', task.created_at));
      lines.push(kvLine('started_at', task.started_at));
      lines.push(kvLine('finished_at', task.finished_at));
      lines.push(kvLine('flows', flows.length));
      if(task.error) lines.push(kvLine('error', task.error));
      lines.flat().forEach(n=>kv.appendChild(n));

      const mode = String(task.mode||'').toLowerCase();
      const rawSection = document.getElementById('rawSection');
      const pushSection = document.getElementById('pushSection');
      const pullSection = document.getElementById('pullSection');
      const rawHint = document.getElementById('rawHint');
      const tsStatsSection = document.getElementById('tsStatsSection');
      const tsPidSection = document.getElementById('tsPidSection');
      const rawFrameSection = document.getElementById('rawFrameSection');
      const offlineChartSection = document.getElementById('offlineChartSection');
      const flvMetadataSection = document.getElementById('flvMetadataSection');

      const push = flows.filter(f=>String(f.direction).toLowerCase()==='push');
      const pull = flows.filter(f=>String(f.direction).toLowerCase()==='pull');
      const rawFlows = flows.filter(f=>String(f.direction).toLowerCase()==='raw' || String(f.direction).toLowerCase()==='flv');
      const tsFlows = flows.filter(f=>String(f.direction).toLowerCase()==='ts');

      if(mode === 'raw' || mode === 'ts' || mode === 'flv'){
        if(rawSection) rawSection.style.display = '';
        if(rawHint) rawHint.style.display = '';
        if(rawHint) rawHint.textContent = mode === 'ts' ? 'TS 模式展示单文件解析结果' : (mode === 'flv' ? 'FLV 模式展示单文件解析结果' : '仅 raw 模式展示');
        if(pushSection) pushSection.style.display = 'none';
        if(pullSection) pullSection.style.display = 'none';
      }else{
        if(rawSection) rawSection.style.display = 'none';
        if(pushSection) pushSection.style.display = '';
        if(pullSection) pullSection.style.display = '';
      }
      if(mode === 'ts'){
        renderTSStats(tsFlows);
        renderTSPIDs(id, tsFlows);
        renderTSPESDetails(tsFlows);
        renderOfflineCharts('ts', tsFlows);
        if(flvMetadataSection) flvMetadataSection.style.display = 'none';
      }else if(mode === 'raw' || mode === 'flv'){
        if(tsStatsSection) tsStatsSection.style.display = 'none';
        if(tsPidSection) tsPidSection.style.display = 'none';
        renderRawFrames(rawFlows);
        renderOfflineCharts(mode, rawFlows);
        if(mode === 'flv'){
          renderFLVMetadata(rawFlows);
        }else if(flvMetadataSection){
          flvMetadataSection.style.display = 'none';
        }
      }else if(tsStatsSection){
        tsStatsSection.style.display = 'none';
        if(tsPidSection) tsPidSection.style.display = 'none';
        if(rawFrameSection) rawFrameSection.style.display = 'none';
        if(offlineChartSection) offlineChartSection.style.display = 'none';
        if(flvMetadataSection) flvMetadataSection.style.display = 'none';
        destroyOfflineCharts();
      }

      renderFlows(id, mode === 'ts' ? tsFlows : rawFlows, 'flowRaw', false);
      renderFlows(id, push, 'flowPush', false);
      renderFlows(id, pull, 'flowPull', false);
    }
    setInterval(loadTasks, 3000);
    loadTasks();
    const modeEl = document.getElementById('mode');
    if(modeEl) modeEl.addEventListener('change', updateModeHint);
    updateModeHint();
  </script>
</body>
</html>`
	c.Header("Content-Type", "text/html")
	c.String(http.StatusOK, html)
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
    <title>实时分析 - Stream Analyzer</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@400;500;600;700&display=swap" rel="stylesheet">
    <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
    <style>
        *,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
        :root{--bg:#fafaf9;--surface:#ffffff;--border:#e8e6e1;--text:#1a1918;--text-muted:#78716c;--accent:#d97757;--accent-hover:#c4684a;--accent-light:#fef3ec;--radius:.875rem;--shadow:0 2px 12px rgba(0,0,0,.05);--shadow-md:0 4px 20px rgba(0,0,0,.08)}
        body{font-family:'Outfit',-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:var(--bg);color:var(--text);min-h:100dvh;padding:1.5rem;line-height:1.5}
        .container{max-width:1200px;margin:0 auto}
        .nav-bar{display:flex;align-items:center;gap:.5rem;margin-bottom:1.5rem;flex-wrap:wrap}
        .nav-item{display:inline-flex;align-items:center;gap:.375rem;padding:.5rem .875rem;border-radius:.5rem;font-size:.8125rem;color:var(--text-muted);text-decoration:none;transition:all .2s ease}
        .nav-item:hover{color:var(--text);background:rgba(0,0,0,.04)}
        .nav-item.active{color:var(--accent);background:var(--accent-light);font-weight:500}
        .card{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:1.5rem;box-shadow:var(--shadow);margin-bottom:1.5rem}
        .card-title{font-size:1rem;font-weight:600;margin-bottom:1rem;display:flex;align-items:center;gap:.5rem}
        .form-grid{display:grid;grid-template-columns:1fr auto auto;gap:1rem;align-items:flex-end}
        .form-group{display:flex;flex-direction:column;gap:.375rem}
        label{font-size:.8125rem;font-weight:500;color:var(--text-muted)}
        input[type="text"],select{padding:.625rem .875rem;border:1px solid var(--border);border-radius:.5rem;font-size:.875rem;background:var(--surface);transition:border-color .2s ease;width:100%}
        input:focus,select:focus{outline:none;border-color:var(--accent)}
        .btn{display:inline-flex;align-items:center;gap:.5rem;background:var(--accent);color:#fff;text-decoration:none;padding:.625rem 1.125rem;border-radius:.5rem;font-size:.875rem;font-weight:500;border:none;cursor:pointer;transition:all .2s ease}
        .btn:hover{background:var(--accent-hover);transform:translateY(-1px)}
        .btn:active{transform:scale(.98)}
        .btn:disabled{background:#c0bdb8;cursor:not-allowed;transform:none}
        .btn.stop{background:#dc2626}.btn.stop:hover{background:#b91c1c}
        .btn.secondary{background:#1a1918}.btn.secondary:hover{background:#2d2c2a}
        .btn.success{background:#059669}.btn.success:hover{background:#047857}
        .table-wrap{overflow-x:auto;border:1px solid var(--border);border-radius:var(--radius)}
        table{width:100%;border-collapse:collapse;font-size:.8125rem}
        th,td{padding:.75rem 1rem;text-align:left;border-bottom:1px solid var(--border);vertical-align:top}
        th{background:#fafaf9;font-weight:500;font-size:.75rem;color:var(--text-muted);text-transform:uppercase;letter-spacing:.03em}
        tr:last-child td{border-bottom:none}
        tr:hover td{background:#fafaf9}
        .task-id{font-family:ui-monospace,monospace;font-size:.8125rem;background:#f3f4f6;padding:.125rem .375rem;border-radius:.25rem}
        .pill{display:inline-block;padding:.25rem .625rem;border-radius:999px;font-size:.6875rem;font-weight:500}
        .pill.running{background:#dcfce7;color:#166534}
        .pill.stopped{background:#fee2e2;color:#991b1b}
        .actions{display:flex;gap:.375rem}.actions button{padding:.375rem .625rem;font-size:.75rem}
        .empty{text-align:center;padding:3rem;color:var(--text-muted);font-size:.875rem}
        .detail{margin-top:1.5rem;display:none}
        .detail-header{display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem;flex-wrap:wrap;gap:.75rem}
        .detail-title{font-size:1rem;font-weight:600}
        .stats-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(100px,1fr));gap:.75rem;background:#fafaf9;padding:1rem;border-radius:.75rem;margin-bottom:1rem}
        .stat-item{text-align:center}
        .stat-value{font-size:1.25rem;font-weight:700;color:var(--text)}
        .stat-label{font-size:.6875rem;color:var(--text-muted);margin-top:.25rem;text-transform:uppercase;letter-spacing:.03em}
        .chart-tabs{display:flex;gap:.5rem;margin-bottom:1rem;flex-wrap:wrap}
        .chart-tabs button{padding:.5rem .875rem;font-size:.8125rem;border:1px solid var(--border);background:var(--surface);border-radius:.5rem;cursor:pointer;transition:all .2s ease}
        .chart-tabs button:hover{border-color:var(--accent);color:var(--accent)}
        .chart-tabs button.active{background:var(--text);color:#fff;border-color:var(--text)}
        .chart-frame{width:100%;height:400px;border:1px solid var(--border);border-radius:var(--radius);margin-bottom:1rem}
        .chart-table{overflow:auto;max-height:250px;border:1px solid var(--border);border-radius:var(--radius)}
        .chart-table table{font-size:.75rem}
        .chart-table th,.chart-table td{padding:.5rem .75rem}
        .chart-table th{background:#fafaf9;position:sticky;top:0}
        .chart-table th.video{color:#2563eb}
        .chart-table th.audio{color:#059669}
        .metadata-box{background:#1a1918;color:#e2e8f0;border-radius:.75rem;padding:1rem;font-size:.75rem;max-height:180px;overflow:auto;margin-top:1rem;white-space:pre-wrap;word-break:break-all}
        @media(max-width:768px){.form-grid{grid-template-columns:1fr}}
        @media(max-width:640px){body{padding:1rem}.stats-grid{grid-template-columns:repeat(3,1fr)}}
    </style>
</head>
<body>
    <div class="container">
        <nav class="nav-bar">
            <a href="/" class="nav-item">入口</a>
            <a href="/realtime" class="nav-item active">实时分析</a>
            <a href="/offline" class="nav-item">离线分析</a>
            <a href="/history" class="nav-item">历史记录</a>
        </nav>

        <div class="card">
            <div class="form-grid">
                <div class="form-group">
                    <label>Stream URL</label>
                    <input type="text" id="streamUrl" placeholder="rtmp://example.com/live/stream 或 http://example.com/live.flv">
                </div>
                <div class="form-group">
                    <label>Type</label>
                    <select id="streamType">
                        <option value="http-flv">HTTP-FLV</option>
                        <option value="rtmp">RTMP</option>
                    </select>
                </div>
                <button class="btn" onclick="startTask()">
                    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M5.25 5.653c0-.856.917-1.398 1.667-.986l11.54 6.347a1.125 1.125 0 0 1 0 1.972l-11.54 6.347a1.125 1.125 0 0 1-1.667-.986V5.653Z" /></svg>
                    开始分析
                </button>
            </div>
        </div>

        <div class="card">
            <div class="card-title">活动任务</div>
            <div class="table-wrap">
                <table>
                    <thead>
                        <tr><th>ID</th><th>URL</th><th>Type</th><th>Status</th><th>Actions</th></tr>
                    </thead>
                    <tbody id="taskBody"></tbody>
                </table>
            </div>
            <div id="emptyState" class="empty" style="display:none">暂无活动任务</div>
        </div>

        <div id="chartSection" class="detail">
            <div class="card">
                <div class="detail-header">
                    <div class="detail-title">分析结果 - <span id="currentTaskTitle"></span></div>
                    <button class="btn secondary" onclick="closeChart()">关闭</button>
                </div>
                <div id="statsGrid" class="stats-grid"></div>
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
                <pre id="metadataBox" class="metadata-box">metadata: -</pre>
                <div id="chartTable" class="chart-table" style="display:none">
                    <table><thead id="tableHead"></thead><tbody id="tableBody"></tbody></table>
                </div>
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
                await fetch('/api/task', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url,type})});
                document.getElementById('streamUrl').value = '';
                loadTasks();
            } catch(e) { alert('创建失败: ' + e.message); }
        }

        async function stopTask(id) {
            if (stoppingTasks.has(id)) return;
            stoppingTasks.add(id);
            try {
                const res = await fetch('/api/task/' + id, {method:'DELETE'});
                if (!res.ok) {const text = await res.text(); throw new Error(text || ('HTTP ' + res.status));}
                if (currentTaskId === id) closeChart();
                await loadTasks();
            } catch (e) {alert('停止任务失败: ' + (e && e.message ? e.message : e));}
            finally {stoppingTasks.delete(id);}
        }

        async function loadTasks() {
            const res = await fetch('/api/task');
            const data = await res.json();
            const tbody = document.getElementById('taskBody');
            const empty = document.getElementById('emptyState');
            tbody.innerHTML = '';
            if (!data.tasks.length) {empty.style.display='block'; return;}
            empty.style.display='none';
            data.tasks.forEach(t => {
                const status = t.status === 'running' ? 'running' : 'stopped';
                const tr = document.createElement('tr');
                tr.innerHTML = '<td><span class="task-id">' + t.id.substring(5,15) + '...</span></td>' +
                    '<td>' + t.url + '</td>' +
                    '<td>' + t.type.toUpperCase() + '</td>' +
                    '<td><span class="pill ' + status + '">' + t.status + '</span></td>' +
                    '<td class="actions"><button class="btn stop" onclick="stopTask(\'' + t.id + '\')">停止</button>' +
                    '<button class="btn" onclick="selectTask(\'' + t.id + '\')">分析</button></td>';
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
            document.getElementById('chartSection').scrollIntoView({behavior:'smooth'});
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
            if (typeof renderClientChart === 'function' && currentData) renderClientChart(currentChartType, currentData, 'mainChart');
        }

        function updateStats(d) {
            function videoCodecName(v) {const m={'2':'H.263','3':'ScreenVideo','4':'VP6','5':'VP6A','6':'ScreenVideo2','7':'H.264/AVC','12':'H.265/HEVC'};return m[String(v)]||(v?('Codec-'+v):'-');}
            function audioCodecName(v) {const m={'2':'MP3','7':'G.711 A-Law','8':'G.711 mu-Law','10':'AAC','11':'Speex','14':'MP3-8k'};return m[String(v)]||(v?('Codec-'+v):'-');}
            const intervals = d.iframe_intervals.filter(v=>v>0);
            const avgInt = intervals.length?Math.round(intervals.reduce((a,b)=>a+b,0)/intervals.length):0;
            const avgGop = d.gop_sizes.length?Math.round(d.gop_sizes.reduce((a,b)=>a+b,0)/d.gop_sizes.length):0;
            const iFrames = d.frame_types.filter(f=>f==='I').length;
            const pFrames = d.frame_types.filter(f=>f==='P').length;
            const bFrames = d.frame_types.filter(f=>f==='B').length;
            const stats = d.second_stats;
            const avgVbr = stats.length?(stats.reduce((a,b)=>a+b.video_bitrate,0)/stats.length).toFixed(1):0;
            const avgAbr = stats.length?(stats.reduce((a,b)=>a+b.audio_bitrate,0)/stats.length).toFixed(1):0;
            const avgVfps = stats.length?Math.round(stats.reduce((a,b)=>a+b.video_fps,0)/stats.length):0;
            const avgAfps = stats.length?Math.round(stats.reduce((a,b)=>a+b.audio_fps,0)/stats.length):0;
            const codecFps = d.video_frame_rate&&d.video_frame_rate>0?d.video_frame_rate.toFixed(3):'-';
            const videoCodec = videoCodecName(d.video_codec);
            const audioCodec = audioCodecName(d.audio_codec);
            const resolution = (d.video_width>0&&d.video_height>0)?(d.video_width+'x'+d.video_height):'-';
            const sampleRate = d.sample_rate>0?d.sample_rate:'-';
            const channels = d.channels>0?d.channels:'-';

            document.getElementById('statsGrid').innerHTML =
                '<div class="stat-item"><div class="stat-value">'+d.frame_types.length+'</div><div class="stat-label">总帧数</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+iFrames+'/'+pFrames+'/'+bFrames+'</div><div class="stat-label">I/P/B帧</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgInt+'</div><div class="stat-label">I帧间隔</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgGop+'</div><div class="stat-label">GOP大小</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgVbr+'</div><div class="stat-label">视频码率</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgAbr+'</div><div class="stat-label">音频码率</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgVfps+'</div><div class="stat-label">视频FPS</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgAfps+'</div><div class="stat-label">音频FPS</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+codecFps+'</div><div class="stat-label">编码帧率</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+videoCodec+'</div><div class="stat-label">视频编码</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+audioCodec+'</div><div class="stat-label">音频编码</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+resolution+'</div><div class="stat-label">分辨率</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+sampleRate+'</div><div class="stat-label">采样率</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+channels+'</div><div class="stat-label">声道数</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+stats.length+'</div><div class="stat-label">时长(s)</div></div>';

            const metadataBox = document.getElementById('metadataBox');
            if (metadataBox) {
                if (d.metadata_json) {
                    try {metadataBox.textContent = JSON.stringify(JSON.parse(d.metadata_json), null, 2);}
                    catch (_) {metadataBox.textContent = d.metadata_json;}
                } else {metadataBox.textContent = 'metadata: -';}
            }
        }

        function showChart(type) {
            currentChartType = type;
            document.querySelectorAll('.chart-tabs button[data-chart]').forEach(function(b){b.classList.toggle('active',b.getAttribute('data-chart')===type);});
            if (currentData && typeof renderClientChart === 'function') renderClientChart(type, currentData, 'mainChart');
            updateChartTable(type);
        }

        function updateChartTable(type) {
            if (!currentData) return;
            const d = currentData;
            const tableDiv = document.getElementById('chartTable');
            const thead = document.getElementById('tableHead');
            const tbody = document.getElementById('tableBody');
            let headers = '', rows = '';
            const stats = d.second_stats || [];
            const maxRows = Number.MAX_SAFE_INTEGER;
            const displayStats = stats;
            const formatTime = (sec) => {
                const d = new Date(Number(sec)*1000);
                if (Number.isNaN(d.getTime())) return '-';
                const pad = (n) => String(n).padStart(2,'0');
                return d.getFullYear()+'-'+pad(d.getMonth()+1)+'-'+pad(d.getDate())+' '+pad(d.getHours())+':'+pad(d.getMinutes())+':'+pad(d.getSeconds());
            };
            const startSecond = displayStats.length>0?Number(displayStats[0].second):0;

            switch(type) {
                case 'timestamp':
                    tableDiv.style.display='block';
                    headers='<tr><th>时间戳</th><th class="video">视频DTS</th><th class="video">视频PTS</th><th class="audio">音频DTS</th></tr>';
                    const vts=d.video_dts||[], vpts=d.video_pts||[], ats=d.audio_dts||[];
                    const allTsSet=new Set([].concat(vts,vpts,ats));
                    const allTs=Array.from(allTsSet).filter(v=>v!==null&&v!==undefined&&v!=='').sort((a,b)=>Number(a)-Number(b)).slice(0,maxRows);
                    const hasNum=(arr,x)=>arr.some(v=>Number(v)===Number(x));
                    for(let i=0;i<allTs.length;i++){const t=allTs[i];rows+='<tr><td>'+t+'</td><td>'+(hasNum(vts,t)?t:'-')+'</td><td>'+(hasNum(vpts,t)?t:'-')+'</td><td>'+(hasNum(ats,t)?t:'-')+'</td></tr>';}
                    break;
                case 'length':
                    tableDiv.style.display='block';
                    headers='<tr><th>#</th><th class="video">视频长度</th><th class="audio">音频长度</th></tr>';
                    const vls=d.video_lens||[], als=d.audio_lens||[];
                    const lsLen=Math.min(Math.max(vls.length,als.length),maxRows);
                    for(let i=0;i<lsLen;i++){rows+='<tr><td>'+(i+1)+'</td><td>'+(vls[i]||'-')+'</td><td>'+(als[i]||'-')+'</td></tr>';}
                    break;
                case 'iframe':
                    tableDiv.style.display='block';
                    headers='<tr><th>#</th><th class="video">I帧间隔</th></tr>';
                    const iframes=(d.iframe_intervals||[]).filter(v=>v>0).slice(0,maxRows);
                    for(let i=0;i<iframes.length;i++){rows+='<tr><td>'+(i+1)+'</td><td>'+iframes[i]+'</td></tr>';}
                    break;
                case 'gop':
                    tableDiv.style.display='block';
                    headers='<tr><th>#</th><th class="video">GOP大小</th></tr>';
                    const gops=(d.gop_sizes||[]).slice(0,maxRows);
                    for(let i=0;i<gops.length;i++){rows+='<tr><td>'+(i+1)+'</td><td>'+gops[i]+'</td></tr>';}
                    break;
                case 'bitrate':
                    tableDiv.style.display='block';
                    headers='<tr><th>时间</th><th class="video">视频码率</th><th class="audio">音频码率</th></tr>';
                    for(let i=0;i<displayStats.length;i++){const s=displayStats[i];const rel=Number(s.second)-startSecond;rows+='<tr><td>T+'+rel+'s<br/>'+formatTime(s.second)+'</td><td>'+s.video_bitrate.toFixed(1)+'</td><td>'+s.audio_bitrate.toFixed(1)+'</td></tr>';}
                    break;
                case 'fps':
                    tableDiv.style.display='block';
                    headers='<tr><th>时间</th><th class="video">视频FPS</th><th class="audio">音频FPS</th></tr>';
                    for(let i=0;i<displayStats.length;i++){const s=displayStats[i];const rel=Number(s.second)-startSecond;rows+='<tr><td>T+'+rel+'s<br/>'+formatTime(s.second)+'</td><td>'+s.video_fps+'</td><td>'+s.audio_fps+'</td></tr>';}
                    break;
                case 'avsync':
                    tableDiv.style.display='block';
                    headers='<tr><th>视频DTS</th><th class="audio">最近音频DTS</th><th>偏差</th></tr>';
                    const avVts=(d.video_dts||[]).map(Number).filter(v=>!Number.isNaN(v));
                    const avAts=(d.audio_dts||[]).map(Number).filter(v=>!Number.isNaN(v));
                    if(avVts.length===0||avAts.length===0){rows='<tr><td colspan="3">暂无可计算的 AV 时间戳数据</td></tr>';break;}
                    let ai=0;
                    for(let i=0;i<Math.min(avVts.length,maxRows);i++){const ts=avVts[i];while(ai+1<avAts.length&&Math.abs(avAts[ai+1]-ts)<=Math.abs(avAts[ai]-ts))ai++;const nearest=avAts[ai];const diff=ts-nearest;rows+='<tr><td>'+ts+'</td><td>'+nearest+'</td><td>'+diff+'</td></tr>';}
                    break;
                default: tableDiv.style.display='none'; return;
            }
            thead.innerHTML=headers; tbody.innerHTML=rows;
        }

        function downloadXLSX() {if(currentTaskId) window.location.href='/api/task/'+currentTaskId+'/xlsx';}

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
    <title>历史记录 - Stream Analyzer</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@400;500;600;700&display=swap" rel="stylesheet">
    <script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
    <style>
        *,*::before,*::after{box-sizing:border-box;margin:0;padding:0}
        :root{--bg:#fafaf9;--surface:#ffffff;--border:#e8e6e1;--text:#1a1918;--text-muted:#78716c;--accent:#d97757;--accent-hover:#c4684a;--accent-light:#fef3ec;--radius:.875rem;--shadow:0 2px 12px rgba(0,0,0,.05);--shadow-md:0 4px 20px rgba(0,0,0,.08)}
        body{font-family:'Outfit',-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:var(--bg);color:var(--text);min-h:100dvh;padding:1.5rem;line-height:1.5}
        .container{max-width:1200px;margin:0 auto}
        .nav-bar{display:flex;align-items:center;gap:.5rem;margin-bottom:1.5rem;flex-wrap:wrap}
        .nav-item{display:inline-flex;align-items:center;gap:.375rem;padding:.5rem .875rem;border-radius:.5rem;font-size:.8125rem;color:var(--text-muted);text-decoration:none;transition:all .2s ease}
        .nav-item:hover{color:var(--text);background:rgba(0,0,0,.04)}
        .nav-item.active{color:var(--accent);background:var(--accent-light);font-weight:500}
        .card{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:1.5rem;box-shadow:var(--shadow);margin-bottom:1.5rem}
        .card-title{font-size:1rem;font-weight:600;margin-bottom:1rem;display:flex;align-items:center;gap:.5rem}
        .back-link{display:inline-flex;align-items:center;gap:.375rem;color:var(--text-muted);text-decoration:none;font-size:.875rem;padding:.5rem .875rem;border-radius:8px;transition:all .2s ease}
        .back-link:hover{color:var(--text);background:rgba(0,0,0,.04)}
        .back-link svg{width:14px;height:14px}
        .table-wrap{overflow-x:auto;border:1px solid var(--border);border-radius:var(--radius)}
        table{width:100%;border-collapse:collapse;font-size:.8125rem}
        th,td{padding:.75rem 1rem;text-align:left;border-bottom:1px solid var(--border);vertical-align:top}
        th{background:#fafaf9;font-weight:500;font-size:.75rem;color:var(--text-muted);text-transform:uppercase;letter-spacing:.03em}
        tr:last-child td{border-bottom:none}
        tr:hover td{background:#fafaf9}
        tr.active td{background:#eff6ff}
        .task-id{font-family:ui-monospace,monospace;font-size:.8125rem;background:#f3f4f6;padding:.125rem .375rem;border-radius:.25rem}
        .pill{display:inline-block;padding:.25rem .625rem;border-radius:999px;font-size:.6875rem;font-weight:500}
        .pill.running{background:#dcfce7;color:#166534}
        .pill.stopped{background:#fee2e2;color:#991b1b}
        .btn{display:inline-flex;align-items:center;gap:.375rem;background:var(--accent);color:#fff;text-decoration:none;padding:.5rem .875rem;border-radius:.5rem;font-size:.8125rem;font-weight:500;border:none;cursor:pointer;transition:all .2s ease}
        .btn:hover{background:var(--accent-hover);transform:translateY(-1px)}
        .btn:active{transform:scale(.98)}
        .btn.stop{background:#dc2626}.btn.stop:hover{background:#b91c1c}
        .btn.secondary{background:#1a1918}.btn.secondary:hover{background:#2d2c2a}
        .btn.success{background:#059669}.btn.success:hover{background:#047857}
        .actions{display:flex;gap:.375rem}.actions button{padding:.375rem .625rem;font-size:.75rem}
        .empty{text-align:center;padding:3rem;color:var(--text-muted);font-size:.875rem}
        .detail{margin-top:1.5rem;display:none}
        .detail-header{display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem;flex-wrap:wrap;gap:.75rem}
        .detail-title{font-size:1rem;font-weight:600}
        .stats-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(100px,1fr));gap:.75rem;background:#fafaf9;padding:1rem;border-radius:.75rem;margin-bottom:1rem}
        .stat-item{text-align:center}
        .stat-value{font-size:1.25rem;font-weight:700;color:var(--text)}
        .stat-label{font-size:.6875rem;color:var(--text-muted);margin-top:.25rem;text-transform:uppercase;letter-spacing:.03em}
        .chart-tabs{display:flex;gap:.5rem;margin-bottom:1rem;flex-wrap:wrap}
        .chart-tabs button{padding:.5rem .875rem;font-size:.8125rem;border:1px solid var(--border);background:var(--surface);border-radius:.5rem;cursor:pointer;transition:all .2s ease}
        .chart-tabs button:hover{border-color:var(--accent);color:var(--accent)}
        .chart-tabs button.active{background:var(--text);color:#fff;border-color:var(--text)}
        .chart-frame{width:100%;height:400px;border:1px solid var(--border);border-radius:var(--radius);margin-bottom:1rem}
        .chart-table{overflow:auto;max-height:250px;border:1px solid var(--border);border-radius:var(--radius)}
        .chart-table table{font-size:.75rem}
        .chart-table th,.chart-table td{padding:.5rem .75rem}
        .chart-table th{background:#fafaf9;position:sticky;top:0}
        .chart-table th.video{color:#2563eb}
        .chart-table th.audio{color:#059669}
        .metadata-box{background:#1a1918;color:#e2e8f0;border-radius:.75rem;padding:1rem;font-size:.75rem;max-height:180px;overflow:auto;margin-top:1rem;white-space:pre-wrap;word-break:break-all}
        @media(max-width:640px){body{padding:1rem}.stats-grid{grid-template-columns:repeat(3,1fr)}}
    </style>
</head>
<body>
    <div class="container">
        <nav class="nav-bar">
            <a href="/" class="nav-item">入口</a>
            <a href="/realtime" class="nav-item">实时分析</a>
            <a href="/offline" class="nav-item">离线分析</a>
            <a href="/history" class="nav-item active">历史记录</a>
        </nav>

        <div class="card">
            <div class="table-wrap">
                <table>
                    <thead>
                        <tr><th>ID</th><th>URL</th><th>Type</th><th>Status</th><th>Actions</th></tr>
                    </thead>
                    <tbody id="taskBody"></tbody>
                </table>
            </div>
            <div id="emptyState" class="empty" style="display:none">No tasks found</div>
        </div>

        <div id="analysisSection" class="detail">
            <div class="card">
                <div class="detail-header">
                    <div class="detail-title">分析详情 - <span id="currentTaskTitle"></span></div>
                    <button class="btn secondary" onclick="closeAnalysis()">关闭</button>
                </div>
                <div id="statsGrid" class="stats-grid"></div>
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
                <pre id="metadataBox" class="metadata-box">metadata: -</pre>
                <div id="chartTable" class="chart-table" style="display:none">
                    <table><thead id="tableHead"></thead><tbody id="tableBody"></tbody></table>
                </div>
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
            if (!data.tasks.length) {empty.style.display='block'; return;}
            empty.style.display='none';
            data.tasks.forEach(t => {
                const status = t.status === 'running' ? 'running' : 'stopped';
                const tr = document.createElement('tr');
                if (currentTaskId === t.id) tr.classList.add('active');
                tr.innerHTML = '<td><span class="task-id">' + t.id.substring(5,15) + '...</span></td>' +
                    '<td>' + t.url + '</td>' +
                    '<td>' + t.type.toUpperCase() + '</td>' +
                    '<td><span class="pill ' + status + '">' + t.status + '</span></td>' +
                    '<td class="actions">' +
                    (t.status === 'running'
                        ? '<button class="btn stop" onclick="stopTask(\'' + t.id + '\')">停止</button>'
                        : '<button class="btn" onclick="analyzeTask(\'' + t.id + '\')">分析</button>' +
                          '<button class="btn success" onclick="downloadXLSXById(\'' + t.id + '\')">XLSX</button>') +
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
                '<div class="stat-item"><div class="stat-value">'+d.frame_types.length+'</div><div class="stat-label">总帧数</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+iFrames+'/'+pFrames+'/'+bFrames+'</div><div class="stat-label">I/P/B帧</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgInt+'</div><div class="stat-label">I帧间隔</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgGop+'</div><div class="stat-label">GOP大小</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgVbr+'</div><div class="stat-label">视频码率</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgAbr+'</div><div class="stat-label">音频码率</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgVfps+'</div><div class="stat-label">视频FPS</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+avgAfps+'</div><div class="stat-label">音频FPS</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+codecFps+'</div><div class="stat-label">编码帧率</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+videoCodec+'</div><div class="stat-label">视频编码</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+audioCodec+'</div><div class="stat-label">音频编码</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+resolution+'</div><div class="stat-label">分辨率</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+sampleRate+'</div><div class="stat-label">采样率</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+channels+'</div><div class="stat-label">声道数</div></div>'+
                '<div class="stat-item"><div class="stat-value">'+stats.length+'</div><div class="stat-label">时长(s)</div></div>';

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
                    tableDiv.style.display='block';
                    headers='<tr><th>时间戳</th><th class="video">视频DTS</th><th class="video">视频PTS</th><th class="audio">音频DTS</th></tr>';
                    const vts=d.video_dts||[], vpts=d.video_pts||[], ats=d.audio_dts||[];
                    const allTsSet=new Set([].concat(vts,vpts,ats));
                    const allTs=Array.from(allTsSet).filter(v=>v!==null&&v!==undefined&&v!=='').sort((a,b)=>Number(a)-Number(b)).slice(0,maxRows);
                    const hasNum=(arr,x)=>arr.some(v=>Number(v)===Number(x));
                    for(let i=0;i<allTs.length;i++){const t=allTs[i];rows+='<tr><td>'+t+'</td><td>'+(hasNum(vts,t)?t:'-')+'</td><td>'+(hasNum(vpts,t)?t:'-')+'</td><td>'+(hasNum(ats,t)?t:'-')+'</td></tr>';}
                    break;
                case 'length':
                    tableDiv.style.display='block';
                    headers='<tr><th>#</th><th class="video">视频长度</th><th class="audio">音频长度</th></tr>';
                    const vls=d.video_lens||[], als=d.audio_lens||[];
                    const lsLen=Math.min(Math.max(vls.length,als.length),maxRows);
                    for(let i=0;i<lsLen;i++){rows+='<tr><td>'+(i+1)+'</td><td>'+(vls[i]||'-')+'</td><td>'+(als[i]||'-')+'</td></tr>';}
                    break;
                case 'iframe':
                    tableDiv.style.display='block';
                    headers='<tr><th>#</th><th class="video">I帧间隔</th></tr>';
                    const iframes=(d.iframe_intervals||[]).filter(v=>v>0).slice(0,maxRows);
                    for(let i=0;i<iframes.length;i++){rows+='<tr><td>'+(i+1)+'</td><td>'+iframes[i]+'</td></tr>';}
                    break;
                case 'gop':
                    tableDiv.style.display='block';
                    headers='<tr><th>#</th><th class="video">GOP大小</th></tr>';
                    const gops=(d.gop_sizes||[]).slice(0,maxRows);
                    for(let i=0;i<gops.length;i++){rows+='<tr><td>'+(i+1)+'</td><td>'+gops[i]+'</td></tr>';}
                    break;
                case 'bitrate':
                    tableDiv.style.display='block';
                    headers='<tr><th>时间</th><th class="video">视频码率</th><th class="audio">音频码率</th></tr>';
                    for(let i=0;i<displayStats.length;i++){const s=displayStats[i];const rel=Number(s.second)-startSecond;rows+='<tr><td>T+'+rel+'s<br/>'+formatTime(s.second)+'</td><td>'+s.video_bitrate.toFixed(1)+'</td><td>'+s.audio_bitrate.toFixed(1)+'</td></tr>';}
                    break;
                case 'fps':
                    tableDiv.style.display='block';
                    headers='<tr><th>时间</th><th class="video">视频FPS</th><th class="audio">音频FPS</th></tr>';
                    for(let i=0;i<displayStats.length;i++){const s=displayStats[i];const rel=Number(s.second)-startSecond;rows+='<tr><td>T+'+rel+'s<br/>'+formatTime(s.second)+'</td><td>'+s.video_fps+'</td><td>'+s.audio_fps+'</td></tr>';}
                    break;
                case 'avsync':
                    tableDiv.style.display='block';
                    headers='<tr><th>视频DTS</th><th class="audio">最近音频DTS</th><th>偏差</th></tr>';
                    const avVts=(d.video_dts||[]).map(Number).filter(v=>!Number.isNaN(v));
                    const avAts=(d.audio_dts||[]).map(Number).filter(v=>!Number.isNaN(v));
                    if(avVts.length===0||avAts.length===0){rows='<tr><td colspan="3">暂无可计算的 AV 时间戳数据</td></tr>';break;}
                    let ai=0;
                    for(let i=0;i<Math.min(avVts.length,maxRows);i++){const ts=avVts[i];while(ai+1<avAts.length&&Math.abs(avAts[ai+1]-ts)<=Math.abs(avAts[ai]-ts))ai++;const nearest=avAts[ai];const diff=ts-nearest;rows+='<tr><td>'+ts+'</td><td>'+nearest+'</td><td>'+diff+'</td></tr>';}
                    break;
                default:
                    tableDiv.style.display='none';
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
