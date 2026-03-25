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
	"streamanalyzer/internal/offline"
	"streamanalyzer/internal/models"
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
	if req.Mode != models.OfflineModeRaw && req.Mode != models.OfflineModePCAP {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode"})
		return
	}
	if sp := c.PostForm("server_port"); sp != "" {
		if v, err := strconv.Atoi(sp); err == nil && v > 0 && v <= 65535 {
			req.ServerPort = uint16(v)
		}
	}
	// Offline parsing currently assumes the input raw contains full RTMP handshake.
	// The required length depends on capture direction:
	// - C0+C1+C2 or S0+S1+S2 (both are 3073 bytes).
	req.SkipBytes = rtmpraw.DefaultHandshakeSkipBytes

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
  <title>Offline RTMP Analyzer</title>
  <style>
    *{box-sizing:border-box} body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f5f5f5;padding:20px}
    .container{max-width:1100px;margin:0 auto;background:#fff;padding:20px;border-radius:8px;box-shadow:0 1px 3px rgba(0,0,0,.1)}
    .row{display:flex;gap:12px;flex-wrap:wrap;align-items:flex-end;margin-bottom:12px}
    label{display:block;font-size:12px;color:#666;margin-bottom:6px}
    input,select{padding:8px 10px;border:1px solid #ddd;border-radius:6px;min-width:220px}
    button{background:#2563eb;color:#fff;border:0;padding:9px 14px;border-radius:6px;cursor:pointer}
    button:disabled{background:#9ca3af}
    table{width:100%;border-collapse:collapse;font-size:13px;margin-top:16px}
    th,td{padding:10px;border-bottom:1px solid #eee;text-align:left;vertical-align:top}
    th{background:#f9fafb;font-size:12px;color:#666}
    .muted{color:#6b7280;font-size:12px}
    .section{margin-top:16px}
    .section h3{margin:0 0 8px 0;font-size:14px}
    .kv{display:grid;grid-template-columns:160px 1fr;gap:8px 12px;font-size:13px}
    .kv div{padding:6px 8px;border:1px solid #eee;border-radius:6px}
    .kv .k{background:#f9fafb;color:#6b7280}
    .pill{display:inline-block;padding:2px 8px;border-radius:999px;font-size:12px;border:1px solid #e5e7eb;background:#f9fafb}
    .pill.running{border-color:#bfdbfe;background:#eff6ff;color:#1d4ed8}
    .pill.done{border-color:#bbf7d0;background:#f0fdf4;color:#166534}
    .pill.failed{border-color:#fecaca;background:#fef2f2;color:#991b1b}
    .actions a{margin-right:10px;display:inline-block;color:#2563eb;text-decoration:none;font-size:13px}
    .actions a:hover{text-decoration:underline}
    .small{font-size:12px}
  </style>
</head>
<body>
  <div class="container">
    <div class="row" style="justify-content:space-between;align-items:center">
      <div>
        <div style="font-size:18px;font-weight:600">离线文件解析</div>
        <div class="muted">上传 raw 或 pcap/pcapng，解析并生成 push/pull 子目录产物</div>
      </div>
      <div><a href="/" class="muted">返回首页</a></div>
    </div>

    <div class="row">
      <div>
        <label>模式</label>
        <select id="mode">
          <option value="pcap">pcap/pcapng</option>
          <option value="raw">raw（单方向，含握手）</option>
        </select>
      </div>
      <div>
        <label>serverPort（pcap）</label>
        <input id="serverPort" type="number" value="1935" />
      </div>
      <div>
        <label>文件</label>
        <input id="file" type="file" />
      </div>
      <button id="btn" onclick="upload()">开始解析</button>
      <div id="runHint" class="muted" style="min-width:240px"></div>
    </div>
    <div class="muted" style="margin-top:-2px;margin-bottom:10px;font-size:12px">
      提示：离线解析要求输入包含完整 RTMP 握手（C0+C1+C2 或 S0+S1+S2，合计 3073 字节）。若缺失握手，可能会解析失败。
    </div>

    <table>
      <thead>
        <tr>
          <th>ID</th><th>mode</th><th>status</th><th>created</th><th>error</th><th>查看</th>
        </tr>
      </thead>
      <tbody id="tbody"></tbody>
    </table>

    <div id="detail" style="margin-top:16px;display:none">
      <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap">
        <div style="font-weight:600">任务详情</div>
        <div class="actions" id="taskActions"></div>
      </div>

      <div class="section">
        <h3>解析信息</h3>
        <div class="kv" id="taskKV"></div>
      </div>

      <div class="section">
        <h3>Raw 解析结果（单方向）</h3>
        <div class="muted" style="margin:6px 0" id="rawHint">仅 raw 模式展示</div>
        <table class="small">
          <thead>
            <tr>
              <th>flow</th><th>client → server</th><th>SYN</th><th>pkts</th><th>payload</th><th>dump</th><th>codec</th><th>产物</th><th>err</th>
            </tr>
          </thead>
          <tbody id="flowRaw"></tbody>
        </table>
      </div>

      <div class="section" id="pushSection">
        <h3>推流列表（push）</h3>
        <table class="small">
          <thead>
            <tr>
              <th>flow</th><th>client → server</th><th>SYN</th><th>pkts</th><th>payload</th><th>dump</th><th>codec</th><th>产物</th><th>err</th>
            </tr>
          </thead>
          <tbody id="flowPush"></tbody>
        </table>
      </div>

      <div class="section" id="pullSection">
        <h3>拉流列表（pull）</h3>
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

  <script>
    let currentId = null;
    function setRunHint(msg){
      const el = document.getElementById('runHint');
      if(!el) return;
      el.textContent = msg || '';
    }
    function sleep(ms){ return new Promise(r=>setTimeout(r, ms)); }
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
      // poll task until done/failed, max ~10 minutes
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
      tb.innerHTML = '';
      (data.tasks||[]).sort((a,b)=>String(b.created_at).localeCompare(String(a.created_at))).forEach(t=>{
        const tr = document.createElement('tr');
        if(String(t.status).toLowerCase() === 'running'){
          tr.style.background = '#eff6ff';
        }
        tr.innerHTML =
          '<td><code>'+t.id+'</code></td>'+
          '<td>'+t.mode+'</td>'+
          '<td>'+t.status+'</td>'+
          '<td>'+t.created_at+'</td>'+
          '<td class=\"muted\">'+(t.error||'')+'</td>';
        const tdBtn = document.createElement('td');
        const btn = document.createElement('button');
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
    function statusPill(s){
      const span = document.createElement('span');
      span.className = 'pill ' + String(s||'');
      span.textContent = s || '-';
      return span;
    }
    function kvLine(k, v){
      const kk = document.createElement('div'); kk.className='k'; kk.textContent = k;
      const vv = document.createElement('div'); vv.textContent = (v===undefined || v===null || v==='') ? '-' : String(v);
      return [kk, vv];
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
        const rawRel = toRel(taskId, f.raw_path);
        const vRel = toRel(taskId, f.video_path);
        const aRel = toRel(taskId, f.audio_path);
        let linkCount = 0;
        if(rawRel){ if(linkCount++) addSep(links); addLink(links, base+rawRel, 'raw'); }
        if(vRel){ if(linkCount++) addSep(links); addLink(links, base+vRel, 'video'); }
        if(aRel){ if(linkCount++) addSep(links); addLink(links, base+aRel, 'audio'); }

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
    async function show(id){
      currentId = id;
      document.getElementById('detail').style.display = 'block';
      const res = await fetch('/api/offline/tasks/'+id);
      const data = await res.json();
      const task = data.task || {};
      const summary = data.summary || {};
      const flows = (summary.flows || []);

      // actions (artifacts)
      const act = document.getElementById('taskActions');
      act.innerHTML = '';
      const base = '/api/offline/tasks/'+id+'/files/';
      const inputRel = toRel(id, task.input_path);
      const summaryRel = toRel(id, task.summary_path);
      if(inputRel) addLink(act, base+inputRel, '下载 input');
      addLink(act, base+'summary.txt', '下载 summary.txt');
      if(summaryRel) addLink(act, base+summaryRel, '下载 summary.json');

      // kv
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
      const rawSection = document.getElementById('flowRaw')?.closest('.section');
      const pushSection = document.getElementById('pushSection');
      const pullSection = document.getElementById('pullSection');
      const rawHint = document.getElementById('rawHint');

      const push = flows.filter(f=>String(f.direction).toLowerCase()==='push');
      const pull = flows.filter(f=>String(f.direction).toLowerCase()==='pull');
      const rawFlows = flows.filter(f=>String(f.direction).toLowerCase()==='raw');

      // mode-specific visibility
      if(mode === 'raw'){
        if(rawSection) rawSection.style.display = '';
        if(rawHint) rawHint.style.display = 'none';
        if(pushSection) pushSection.style.display = 'none';
        if(pullSection) pullSection.style.display = 'none';
      }else{
        if(rawSection) rawSection.style.display = 'none';
        if(pushSection) pushSection.style.display = '';
        if(pullSection) pullSection.style.display = '';
      }

      renderFlows(id, rawFlows, 'flowRaw', false);
      renderFlows(id, push, 'flowPush', false);
      renderFlows(id, pull, 'flowPull', false);
    }
    setInterval(loadTasks, 3000);
    loadTasks();
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
            <div style="display:flex; gap:12px; align-items:center;">
                <a href="/" class="nav-link">入口</a>
                <a href="/realtime" class="nav-link" style="font-weight:600;">实时分析</a>
                <a href="/offline" class="nav-link">离线分析(上传文件)</a>
                <a href="/history" class="nav-link">历史记录</a>
            </div>
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
