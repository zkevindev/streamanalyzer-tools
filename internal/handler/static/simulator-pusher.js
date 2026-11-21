(() => {
  'use strict';

  const API = '/api/simulators/pusher';

  const els = {
    notice: document.getElementById('notice'),
    form: document.getElementById('push-form'),
    protocol: document.getElementById('protocol'),
    sourceKind: document.getElementById('source-kind'),
    targetURL: document.getElementById('target-url'),
    targetHint: document.getElementById('target-hint'),
    rtspOpts: document.getElementById('rtsp-opts'),
    rtspTransport: document.getElementById('rtsp-transport'),
    whipOpts: document.getElementById('whip-opts'),
    bearerToken: document.getElementById('bearer-token'),
    srcUpload: document.getElementById('src-upload'),
    dropzone: document.getElementById('dropzone'),
    uploadFile: document.getElementById('upload-file'),
    uploadLabel: document.getElementById('upload-label'),
    uploadMeta: document.getElementById('upload-meta'),
    srcFile: document.getElementById('src-file'),
    filePath: document.getElementById('file-path'),
    srcPattern: document.getElementById('src-pattern'),
    patternRes: document.getElementById('pattern-res'),
    patternFPS: document.getElementById('pattern-fps'),
    srcBrowser: document.getElementById('src-browser'),
    captureMode: document.getElementById('capture-mode'),
    cameraDevice: document.getElementById('camera-device'),
    localPreview: document.getElementById('local-preview'),
    btnPreview: document.getElementById('btn-preview'),
    btnStopPreview: document.getElementById('btn-stop-preview'),
    loopOpts: document.getElementById('loop-opts'),
    loopEnabled: document.getElementById('loop-enabled'),
    loopCount: document.getElementById('loop-count'),
    videoMode: document.getElementById('video-mode'),
    audioMode: document.getElementById('audio-mode'),
    videoBitrate: document.getElementById('video-bitrate'),
    taskName: document.getElementById('task-name'),
    btnStart: document.getElementById('btn-start'),
    activeSummary: document.getElementById('active-summary'),
    btnStopAll: document.getElementById('btn-stop-all'),
    taskList: document.getElementById('task-list'),
  };

  let selectedFile = null;
  let previewStream = null;
  let serverTasks = [];
  let maxTasks = 16;
  const openLogs = new Set();

  // Local browser WebRTC WHIP sessions: id -> session object
  const whipSessions = new Map();
  // Local browser MediaRecorder -> WebSocket ingest sessions: serverTaskID -> { ws, recorder, stream, ownedStream }
  const ingestSessions = new Map();

  const protoMeta = {
    rtmp: {
      placeholder: 'rtmp://127.0.0.1:1935/live/test',
      hint: '支持 rtmp:// 或 rtmps:// 地址，可直接推向本机 RTMP 模拟器（publish_relay 模式）或任意流媒体服务。',
    },
    rtsp: {
      placeholder: 'rtsp://127.0.0.1:8554/live/test',
      hint: '支持 rtsp:// 或 rtsps:// 推流（ANNOUNCE/RECORD），支持 TCP 或 UDP 传输。',
    },
    whip: {
      placeholder: 'http://127.0.0.1:1985/rtc/v1/whip/?app=live&stream=test',
      hint: '标准 RFC 9725 WHIP 端点（http:// 或 https://），自动经本机后端协商 SDP 避免跨域限制。',
    },
  };

  const statusLabels = {
    starting: '启动中',
    pushing: '推流中',
    stopped: '已停止',
    failed: '推流失败',
  };

  const sourceLabels = {
    upload: '上传录像',
    file: '本机路径',
    pattern: '测试画面',
    browser: '浏览器采集',
  };

  function showNotice(msg) {
    if (!msg) {
      els.notice.hidden = true;
      els.notice.textContent = '';
      return;
    }
    els.notice.textContent = msg;
    els.notice.hidden = false;
  }

  function escapeHTML(str) {
    return String(str ?? '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function formatDuration(ms) {
    const totalSec = Math.max(0, Math.floor((ms || 0) / 1000));
    const h = Math.floor(totalSec / 3600);
    const m = Math.floor((totalSec % 3600) / 60);
    const s = totalSec % 60;
    const pad = (n) => String(n).padStart(2, '0');
    return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`;
  }

  function formatBytes(bytes) {
    const b = Number(bytes || 0);
    if (b < 1024) return `${b} B`;
    if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
    if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(2)} MB`;
    return `${(b / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  }

  function redactURL(raw) {
    try {
      const u = new URL(raw);
      if (u.password) u.password = '***';
      for (const [k] of u.searchParams) {
        if (/token|key|secret|pass|auth/i.test(k)) {
          u.searchParams.set(k, '***');
        }
      }
      return u.toString();
    } catch {
      return raw;
    }
  }

  function syncFormVisibility() {
    const proto = els.protocol.value;
    const src = els.sourceKind.value;

    const pm = protoMeta[proto] || protoMeta.rtmp;
    els.targetURL.placeholder = pm.placeholder;
    els.targetHint.textContent = pm.hint;

    els.rtspOpts.hidden = proto !== 'rtsp';
    els.whipOpts.hidden = proto !== 'whip';

    els.srcUpload.hidden = src !== 'upload';
    els.srcFile.hidden = src !== 'file';
    els.srcPattern.hidden = src !== 'pattern';
    els.srcBrowser.hidden = src !== 'browser';
    els.loopOpts.hidden = src === 'browser';

    // Smart codec defaults
    if (proto === 'whip') {
      if (src === 'pattern' || src === 'browser') {
        els.videoMode.value = 'h264';
      }
      if (els.audioMode.value !== 'none') {
        els.audioMode.value = 'opus';
      }
    } else if (src === 'pattern' || src === 'browser') {
      els.videoMode.value = 'h264';
      if (els.audioMode.value === 'copy' || els.audioMode.value === 'opus') {
        els.audioMode.value = 'aac';
      }
    } else if (els.audioMode.value === 'opus') {
      els.audioMode.value = 'copy';
    }

    if (src === 'browser') {
      refreshCameraDevices();
    }
  }

  async function refreshCameraDevices() {
    if (!navigator.mediaDevices || !navigator.mediaDevices.enumerateDevices) return;
    try {
      const devices = await navigator.mediaDevices.enumerateDevices();
      const cams = devices.filter((d) => d.kind === 'videoinput');
      const current = els.cameraDevice.value;
      els.cameraDevice.innerHTML = '<option value="">系统默认摄像头</option>';
      cams.forEach((cam, idx) => {
        const opt = document.createElement('option');
        opt.value = cam.deviceId;
        opt.textContent = cam.label || `摄像头 #${idx + 1}`;
        els.cameraDevice.appendChild(opt);
      });
      if (current) els.cameraDevice.value = current;
    } catch {
      // Ignore permission issues before getUserMedia
    }
  }

  async function acquireCaptureStream() {
    if (!navigator.mediaDevices) {
      throw new Error('当前浏览器环境不支持媒体采集 API（请在 localhost 或 HTTPS 下打开）');
    }
    const mode = els.captureMode.value;
    if (mode === 'screen') {
      if (!navigator.mediaDevices.getDisplayMedia) {
        throw new Error('当前浏览器不支持共享屏幕采集');
      }
      return await navigator.mediaDevices.getDisplayMedia({
        video: { frameRate: { ideal: 25, max: 30 } },
        audio: els.audioMode.value !== 'none',
      });
    }
    const deviceId = els.cameraDevice.value;
    const videoConstraint = deviceId
      ? { deviceId: { exact: deviceId }, width: { ideal: 1280 }, height: { ideal: 720 } }
      : { width: { ideal: 1280 }, height: { ideal: 720 } };
    const wantAudio = mode !== 'camera_only' && els.audioMode.value !== 'none';
    const stream = await navigator.mediaDevices.getUserMedia({
      video: videoConstraint,
      audio: wantAudio,
    });
    await refreshCameraDevices();
    return stream;
  }

  function stopPreview() {
    if (previewStream) {
      previewStream.getTracks().forEach((tr) => tr.stop());
      previewStream = null;
    }
    els.localPreview.srcObject = null;
    els.localPreview.hidden = true;
    els.btnStopPreview.hidden = true;
    els.btnPreview.textContent = '开启本地预览';
  }

  els.btnPreview.addEventListener('click', async () => {
    showNotice('');
    try {
      stopPreview();
      previewStream = await acquireCaptureStream();
      els.localPreview.srcObject = previewStream;
      els.localPreview.hidden = false;
      els.btnStopPreview.hidden = false;
      els.btnPreview.textContent = '刷新预览';
      previewStream.getVideoTracks().forEach((tr) => {
        tr.addEventListener('ended', () => stopPreview(), { once: true });
      });
    } catch (err) {
      showNotice(`无法开启采集预览：${err.message || err}`);
    }
  });

  els.btnStopPreview.addEventListener('click', stopPreview);

  function setSelectedFile(file) {
    selectedFile = file || null;
    if (selectedFile) {
      els.uploadLabel.textContent = selectedFile.name;
      els.uploadMeta.textContent = `文件大小：${formatBytes(selectedFile.size)}（点击可重新选择）`;
    } else {
      els.uploadLabel.textContent = '点击选择或拖拽录像文件到此处';
      els.uploadMeta.textContent = '支持 MP4 / FLV / TS / MKV / MOV 等常见封装格式';
    }
  }

  els.uploadFile.addEventListener('change', () => {
    setSelectedFile(els.uploadFile.files && els.uploadFile.files[0]);
  });

  els.dropzone.addEventListener('dragover', (e) => {
    e.preventDefault();
    els.dropzone.classList.add('is-dragover');
  });
  els.dropzone.addEventListener('dragleave', () => {
    els.dropzone.classList.remove('is-dragover');
  });
  els.dropzone.addEventListener('drop', (e) => {
    e.preventDefault();
    els.dropzone.classList.remove('is-dragover');
    const file = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
    if (file) {
      setSelectedFile(file);
    }
  });

  els.protocol.addEventListener('change', syncFormVisibility);
  els.sourceKind.addEventListener('change', syncFormVisibility);
  els.loopEnabled.addEventListener('change', () => {
    els.loopCount.disabled = !els.loopEnabled.checked;
  });

  function collectBaseConfig() {
    return {
      name: els.taskName.value.trim(),
      protocol: els.protocol.value,
      target_url: els.targetURL.value.trim(),
      bearer_token: els.bearerToken.value.trim(),
      rtsp_transport: els.rtspTransport.value,
      source_kind: els.sourceKind.value,
      file_path: els.filePath.value.trim(),
      loop: els.loopEnabled.checked,
      loop_count: Math.max(0, parseInt(els.loopCount.value || '0', 10) || 0),
      video_mode: els.videoMode.value,
      audio_mode: els.audioMode.value,
      video_bitrate_kbps: Math.max(100, parseInt(els.videoBitrate.value || '1200', 10) || 1200),
      fps: Math.max(5, parseInt(els.patternFPS.value || '25', 10) || 25),
      resolution: els.patternRes.value,
    };
  }

  function addWHIPLog(sess, msg) {
    const ts = new Date().toTimeString().slice(0, 8);
    sess.recent_logs.push(`${ts} ${msg}`);
    if (sess.recent_logs.length > 25) {
      sess.recent_logs.shift();
    }
  }

  async function waitForICEGathering(pc, timeoutMs = 3500) {
    if (pc.iceGatheringState === 'complete') return;
    await new Promise((resolve) => {
      let done = false;
      const finish = () => {
        if (done) return;
        done = true;
        clearTimeout(timer);
        pc.removeEventListener('icegatheringstatechange', onChange);
        resolve();
      };
      const onChange = () => {
        if (pc.iceGatheringState === 'complete') finish();
      };
      const timer = setTimeout(finish, timeoutMs);
      pc.addEventListener('icegatheringstatechange', onChange);
    });
  }

  async function startBrowserWHIP(cfg) {
    const captureLabel = els.captureMode.value === 'screen' ? '屏幕共享 (WebRTC)' : '摄像头采集 (WebRTC)';
    let stream;
    let ownedStream = true;
    if (previewStream && previewStream.active) {
      // Clone tracks so stopping push won't stop the preview (and vice versa).
      stream = previewStream.clone();
    } else {
      stream = await acquireCaptureStream();
    }

    const id = 'whip-' + Math.random().toString(36).slice(2, 10);
    const sess = {
      id,
      isLocalWHIP: true,
      name: cfg.name || `WHIP · ${captureLabel}`,
      protocol: 'whip',
      target_url: redactURL(cfg.target_url),
      raw_target_url: cfg.target_url,
      bearer_token: cfg.bearer_token,
      source_kind: 'browser',
      source_name: captureLabel,
      loop: false,
      loop_count: 0,
      video_mode: 'webrtc',
      audio_mode: cfg.audio_mode,
      status: 'starting',
      ice_state: 'new',
      frames_sent: 0,
      bytes_sent: 0,
      fps: 0,
      bitrate_kbps: 0,
      elapsed_ms: 0,
      started_at: new Date().toISOString(),
      startedMs: Date.now(),
      finishedMs: 0,
      error: '',
      recent_logs: [],
      pc: null,
      stream,
      ownedStream,
      resourceURL: '',
      statsTimer: null,
      prevBytes: 0,
      prevTime: Date.now(),
    };
    whipSessions.set(id, sess);
    addWHIPLog(sess, `准备通过浏览器 WebRTC 推流到 ${sess.target_url}`);
    renderTasks();

    try {
      const pc = new RTCPeerConnection();
      sess.pc = pc;

      stream.getTracks().forEach((track) => {
        pc.addTrack(track, stream);
        track.addEventListener('ended', () => {
          if (sess.status === 'starting' || sess.status === 'pushing') {
            addWHIPLog(sess, '本地采集轨道已结束，自动停止推流');
            stopBrowserWHIP(id);
          }
        });
      });

      pc.addEventListener('iceconnectionstatechange', () => {
        sess.ice_state = pc.iceConnectionState;
        addWHIPLog(sess, `ICE 状态：${pc.iceConnectionState}`);
        if (pc.iceConnectionState === 'connected' || pc.iceConnectionState === 'completed') {
          if (sess.status === 'starting') sess.status = 'pushing';
        } else if (pc.iceConnectionState === 'failed') {
          sess.status = 'failed';
          sess.error = 'WebRTC ICE 连接失败';
          sess.finishedMs = Date.now();
          cleanupBrowserWHIPMedia(sess);
        }
        renderTasks();
      });

      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
      await waitForICEGathering(pc, 3500);

      addWHIPLog(sess, '发送 WHIP Offer SDP（经本机代理）...');
      const resp = await fetch(`${API}/whip-proxy`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          target_url: cfg.target_url,
          bearer_token: cfg.bearer_token,
          offer_sdp: pc.localDescription.sdp,
        }),
      });
      const data = await resp.json().catch(() => ({}));
      if (!resp.ok) {
        throw new Error(data.error || `WHIP 协商失败 (HTTP ${resp.status})`);
      }
      sess.resourceURL = data.resource_url || '';
      await pc.setRemoteDescription({ type: 'answer', sdp: data.answer_sdp });
      addWHIPLog(sess, `WHIP SDP 协商成功 (HTTP ${data.status_code || 201})`);
      if (sess.status === 'starting') {
        sess.status = 'pushing';
      }

      sess.statsTimer = setInterval(async () => {
        if (!sess.pc || (sess.status !== 'pushing' && sess.status !== 'starting')) return;
        try {
          const stats = await sess.pc.getStats();
          let bytes = 0;
          let frames = 0;
          let fps = 0;
          stats.forEach((report) => {
            if (report.type === 'outbound-rtp') {
              bytes += Number(report.bytesSent || 0);
              if (report.kind === 'video') {
                frames = Number(report.framesEncoded || report.framesSent || frames);
                if (typeof report.framesPerSecond === 'number') {
                  fps = report.framesPerSecond;
                }
              }
            }
          });
          const now = Date.now();
          const dt = Math.max(0.2, (now - sess.prevTime) / 1000);
          if (bytes >= sess.prevBytes) {
            sess.bitrate_kbps = ((bytes - sess.prevBytes) * 8) / 1000 / dt;
          }
          sess.prevBytes = bytes;
          sess.prevTime = now;
          sess.bytes_sent = bytes;
          sess.frames_sent = frames;
          sess.fps = fps;
          sess.elapsed_ms = now - sess.startedMs;
          renderTasks();
        } catch {
          // ignore transient stats errors
        }
      }, 1000);
      renderTasks();
    } catch (err) {
      sess.status = 'failed';
      sess.error = err.message || String(err);
      sess.finishedMs = Date.now();
      addWHIPLog(sess, `失败：${sess.error}`);
      cleanupBrowserWHIPMedia(sess);
      renderTasks();
      throw err;
    }
  }

  function cleanupBrowserWHIPMedia(sess) {
    if (sess.statsTimer) {
      clearInterval(sess.statsTimer);
      sess.statsTimer = null;
    }
    if (sess.pc) {
      try { sess.pc.close(); } catch {}
      sess.pc = null;
    }
    if (sess.ownedStream && sess.stream) {
      sess.stream.getTracks().forEach((tr) => tr.stop());
      sess.stream = null;
    }
  }

  async function stopBrowserWHIP(id) {
    const sess = whipSessions.get(id);
    if (!sess) return;
    if (sess.status === 'starting' || sess.status === 'pushing') {
      sess.status = 'stopped';
      sess.finishedMs = Date.now();
      addWHIPLog(sess, '已手动停止推流');
    }
    cleanupBrowserWHIPMedia(sess);
    if (sess.resourceURL) {
      const resourceURL = sess.resourceURL;
      sess.resourceURL = '';
      try {
        await fetch(`${API}/whip-proxy`, {
          method: 'DELETE',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            resource_url: resourceURL,
            bearer_token: sess.bearer_token,
          }),
        });
      } catch {
        // best-effort teardown
      }
    }
    renderTasks();
  }

  function pickRecorderMimeType() {
    if (typeof MediaRecorder === 'undefined') return '';
    const candidates = [
      'video/webm;codecs=vp8,opus',
      'video/webm;codecs=h264,opus',
      'video/webm;codecs=vp9,opus',
      'video/webm',
      'video/mp4',
    ];
    for (const mime of candidates) {
      if (MediaRecorder.isTypeSupported(mime)) return mime;
    }
    return '';
  }

  async function startBrowserIngest(cfg) {
    if (typeof MediaRecorder === 'undefined') {
      throw new Error('当前浏览器不支持 MediaRecorder，无法转推 RTMP/RTSP');
    }
    let stream;
    if (previewStream && previewStream.active) {
      stream = previewStream.clone();
    } else {
      stream = await acquireCaptureStream();
    }

    const captureLabel = els.captureMode.value === 'screen' ? '屏幕共享' : '摄像头采集';
    cfg.source_name = captureLabel;
    if (!cfg.name) {
      cfg.name = `${cfg.protocol.toUpperCase()} · ${captureLabel}`;
    }

    let task;
    try {
      const resp = await fetch(`${API}/tasks`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(cfg),
      });
      const data = await resp.json().catch(() => ({}));
      if (!resp.ok) {
        throw new Error(data.error || `创建推流任务失败 (HTTP ${resp.status})`);
      }
      task = data;
    } catch (err) {
      stream.getTracks().forEach((tr) => tr.stop());
      throw err;
    }

    const wsProto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsURL = `${wsProto}//${location.host}${API}/tasks/${encodeURIComponent(task.id)}/ingest`;
    const ws = new WebSocket(wsURL);
    ws.binaryType = 'arraybuffer';

    const mimeType = pickRecorderMimeType();
    const recOpts = mimeType ? { mimeType, videoBitsPerSecond: cfg.video_bitrate_kbps * 1000 } : {};
    const recorder = new MediaRecorder(stream, recOpts);

    const entry = { ws, recorder, stream, closed: false };
    ingestSessions.set(task.id, entry);

    const cleanup = () => {
      if (entry.closed) return;
      entry.closed = true;
      ingestSessions.delete(task.id);
      try {
        if (recorder.state !== 'inactive') recorder.stop();
      } catch {}
      try {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send('stop');
          ws.close();
        } else if (ws.readyState === WebSocket.CONNECTING) {
          ws.close();
        }
      } catch {}
      stream.getTracks().forEach((tr) => tr.stop());
    };

    stream.getVideoTracks().forEach((tr) => {
      tr.addEventListener('ended', cleanup, { once: true });
    });

    recorder.ondataavailable = async (ev) => {
      if (!ev.data || ev.data.size === 0) return;
      if (ws.readyState !== WebSocket.OPEN) return;
      // Backpressure guard: drop chunk if socket buffer grows too large (>4 MB)
      if (ws.bufferedAmount > 4 * 1024 * 1024) return;
      try {
        const buf = await ev.data.arrayBuffer();
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(buf);
        }
      } catch {
        // ignore write race
      }
    };

    ws.onopen = () => {
      try {
        recorder.start(250);
      } catch (err) {
        showNotice(`启动浏览器录制失败：${err.message || err}`);
        cleanup();
      }
    };

    ws.onerror = () => {
      cleanup();
    };
    ws.onclose = () => {
      cleanup();
    };
  }

  function cleanupIngestSession(taskID) {
    const entry = ingestSessions.get(taskID);
    if (!entry || entry.closed) return;
    entry.closed = true;
    ingestSessions.delete(taskID);
    try {
      if (entry.recorder && entry.recorder.state !== 'inactive') entry.recorder.stop();
    } catch {}
    try {
      if (entry.ws && (entry.ws.readyState === WebSocket.OPEN || entry.ws.readyState === WebSocket.CONNECTING)) {
        entry.ws.close();
      }
    } catch {}
    if (entry.stream) {
      entry.stream.getTracks().forEach((tr) => tr.stop());
    }
  }

  els.form.addEventListener('submit', async (e) => {
    e.preventDefault();
    showNotice('');

    const cfg = collectBaseConfig();
    if (!cfg.target_url) {
      showNotice('请填写目标推流地址');
      return;
    }

    els.btnStart.disabled = true;
    const origText = els.btnStart.textContent;
    els.btnStart.textContent = '正在启动...';

    try {
      if (cfg.source_kind === 'browser') {
        if (cfg.protocol === 'whip') {
          await startBrowserWHIP(cfg);
        } else {
          await startBrowserIngest(cfg);
        }
      } else if (cfg.source_kind === 'upload') {
        if (!selectedFile) {
          throw new Error('请先选择要上传的录像文件');
        }
        const fd = new FormData();
        // Backend multipart streaming expects config part before file part
        fd.append('config', JSON.stringify(cfg));
        fd.append('file', selectedFile, selectedFile.name);
        const resp = await fetch(`${API}/tasks/upload`, {
          method: 'POST',
          body: fd,
        });
        const data = await resp.json().catch(() => ({}));
        if (!resp.ok) {
          throw new Error(data.error || `上传并启动失败 (HTTP ${resp.status})`);
        }
      } else {
        if (cfg.source_kind === 'file' && !cfg.file_path) {
          throw new Error('请填写本机录像文件路径');
        }
        const resp = await fetch(`${API}/tasks`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(cfg),
        });
        const data = await resp.json().catch(() => ({}));
        if (!resp.ok) {
          throw new Error(data.error || `启动推流失败 (HTTP ${resp.status})`);
        }
      }
      await pollTasks();
    } catch (err) {
      showNotice(err.message || String(err));
    } finally {
      els.btnStart.disabled = false;
      els.btnStart.textContent = origText;
    }
  });

  async function stopTask(id, isLocalWHIP) {
    showNotice('');
    if (isLocalWHIP) {
      await stopBrowserWHIP(id);
      return;
    }
    cleanupIngestSession(id);
    try {
      const resp = await fetch(`${API}/tasks/${encodeURIComponent(id)}/stop`, { method: 'POST' });
      if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || `停止失败 (HTTP ${resp.status})`);
      }
      await pollTasks();
    } catch (err) {
      showNotice(err.message || String(err));
    }
  }

  async function deleteTask(id, isLocalWHIP) {
    showNotice('');
    if (isLocalWHIP) {
      whipSessions.delete(id);
      openLogs.delete(id);
      renderTasks();
      return;
    }
    try {
      const resp = await fetch(`${API}/tasks/${encodeURIComponent(id)}`, { method: 'DELETE' });
      if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || `移除失败 (HTTP ${resp.status})`);
      }
      openLogs.delete(id);
      await pollTasks();
    } catch (err) {
      showNotice(err.message || String(err));
    }
  }

  els.btnStopAll.addEventListener('click', async () => {
    showNotice('');
    els.btnStopAll.disabled = true;
    try {
      for (const id of Array.from(ingestSessions.keys())) {
        cleanupIngestSession(id);
      }
      await Promise.all(Array.from(whipSessions.keys()).map((id) => stopBrowserWHIP(id)));
      await fetch(`${API}/stop-all`, { method: 'POST' });
      await pollTasks();
    } catch (err) {
      showNotice(`停止全部任务时出错：${err.message || err}`);
    }
  });

  function combinedTasks() {
    const locals = Array.from(whipSessions.values()).map((s) => ({
      ...s,
      elapsed_ms: s.finishedMs ? s.finishedMs - s.startedMs : Date.now() - s.startedMs,
    }));
    const all = [...locals, ...serverTasks];
    all.sort((a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime());
    return all;
  }

  function renderTasks() {
    const tasks = combinedTasks();
    const activeCount = tasks.filter((t) => t.status === 'starting' || t.status === 'pushing').length;

    els.activeSummary.textContent = `${activeCount} 路运行中 / 共 ${tasks.length} 路`;
    els.btnStopAll.disabled = activeCount === 0;

    // Clean up stale browser ingest sessions if server task ended
    for (const [id] of ingestSessions) {
      const st = serverTasks.find((t) => t.id === id);
      if (st && (st.status === 'stopped' || st.status === 'failed')) {
        cleanupIngestSession(id);
      }
    }

    if (tasks.length === 0) {
      els.taskList.innerHTML = '<p class="pusher-empty">暂无推流任务。在左侧配置目标地址和推流源即可启动，支持同时运行多路。</p>';
      return;
    }

    const html = tasks.map((t) => {
      const isActive = t.status === 'starting' || t.status === 'pushing';
      const proto = String(t.protocol || '').toLowerCase();
      const protoTag = proto === 'rtsp' && t.rtsp_transport
        ? `RTSP/${String(t.rtsp_transport).toUpperCase()}`
        : proto.toUpperCase();
      const srcTag = sourceLabels[t.source_kind] || t.source_kind;
      const loopTag = t.source_kind === 'browser'
        ? ''
        : (t.loop ? (t.loop_count > 0 ? ` · 循环 ${t.loop_count} 次` : ' · 无限循环') : ' · 单次');
      const icePart = t.ice_state ? ` · ICE: ${escapeHTML(t.ice_state)}` : '';
      const bitrateText = t.bitrate_kbps > 0 ? `${t.bitrate_kbps.toFixed(0)} kbps` : '—';
      const fpsText = t.fps > 0 ? `${t.fps.toFixed(1)} fps` : '—';
      const logs = Array.isArray(t.recent_logs) ? t.recent_logs : [];
      const isOpen = openLogs.has(t.id) ? ' open' : '';

      return `
        <article class="pusher-task" data-id="${escapeHTML(t.id)}" data-local="${t.isLocalWHIP ? '1' : '0'}">
          <div class="pusher-task-head">
            <div class="pusher-task-title">
              <span class="pusher-badge" data-proto="${escapeHTML(proto)}">${escapeHTML(protoTag)}</span>
              <strong>${escapeHTML(t.name)}</strong>
            </div>
            <span class="pusher-status" data-status="${escapeHTML(t.status)}">${escapeHTML(statusLabels[t.status] || t.status)}</span>
          </div>
          <div class="pusher-task-meta">
            <div>目标：<code>${escapeHTML(t.target_url)}</code></div>
            <div>来源：${escapeHTML(srcTag)}（${escapeHTML(t.source_name || '')}）${escapeHTML(loopTag)} · 编码 ${escapeHTML(t.video_mode || '-')}/${escapeHTML(t.audio_mode || '-')}${icePart}</div>
          </div>
          <dl class="pusher-task-stats">
            <div><dt>已推时长</dt><dd>${escapeHTML(formatDuration(t.elapsed_ms))}</dd></div>
            <div><dt>实时码率</dt><dd>${escapeHTML(bitrateText)}</dd></div>
            <div><dt>实时帧率</dt><dd>${escapeHTML(fpsText)}</dd></div>
            <div><dt>发送流量 / 帧数</dt><dd>${escapeHTML(formatBytes(t.bytes_sent))} / ${escapeHTML(String(t.frames_sent || 0))}</dd></div>
          </dl>
          ${t.error ? `<p class="sim-error" style="margin:0;">错误：${escapeHTML(t.error)}</p>` : ''}
          ${logs.length > 0 ? `
            <details class="pusher-task-logs" data-log-id="${escapeHTML(t.id)}"${isOpen}>
              <summary>最近日志（${logs.length} 条）</summary>
              <pre>${escapeHTML(logs.join('\n'))}</pre>
            </details>
          ` : ''}
          <div class="pusher-task-actions">
            ${isActive
              ? `<button type="button" class="secondary" data-action="stop">停止推流</button>`
              : `<button type="button" class="secondary" data-action="delete">移除记录</button>`}
          </div>
        </article>
      `;
    }).join('');

    els.taskList.innerHTML = html;
  }

  els.taskList.addEventListener('toggle', (e) => {
    const det = e.target;
    if (det && det.matches && det.matches('details[data-log-id]')) {
      const id = det.getAttribute('data-log-id');
      if (det.open) openLogs.add(id);
      else openLogs.delete(id);
    }
  }, true);

  els.taskList.addEventListener('click', (e) => {
    const btn = e.target.closest('button[data-action]');
    if (!btn) return;
    const card = btn.closest('.pusher-task');
    if (!card) return;
    const id = card.getAttribute('data-id');
    const isLocal = card.getAttribute('data-local') === '1';
    const action = btn.getAttribute('data-action');
    if (action === 'stop') {
      stopTask(id, isLocal);
    } else if (action === 'delete') {
      deleteTask(id, isLocal);
    }
  });

  async function pollTasks() {
    try {
      const resp = await fetch(`${API}/tasks`, { cache: 'no-store' });
      if (!resp.ok) return;
      const data = await resp.json();
      serverTasks = Array.isArray(data.tasks) ? data.tasks : [];
      if (data.max_tasks) maxTasks = data.max_tasks;
      renderTasks();
    } catch {
      // ignore transient network errors
    }
  }

  window.addEventListener('beforeunload', () => {
    for (const id of Array.from(ingestSessions.keys())) {
      cleanupIngestSession(id);
    }
    for (const sess of whipSessions.values()) {
      cleanupBrowserWHIPMedia(sess);
    }
  });

  syncFormVisibility();
  pollTasks();
  setInterval(pollTasks, 1500);
})();
