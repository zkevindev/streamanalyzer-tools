'use strict';

(() => {
  const node = (id) => document.getElementById(id);
  const endpoint = '/api/hls-stats/tasks';
  const form = node('hls-form'), mode = node('hls-command');
  const startButton = node('hls-start'), stopButton = node('hls-stop');
  const output = node('hls-report'), results = node('hls-results');
  const status = node('hls-status');
  const activeStates = new Set(['running', 'stopping']);
  const modes = {
    analyze: '采样选中的分片，检查请求耗时、吞吐和缓存。',
    manifest: '读取播放列表与档位声明。',
    monitor: '在任务时限内跟踪序号推进和播放列表刷新。',
    compare: '依次用相同参数采样 A 路和 B 路，展示 B 减 A 的变化。',
  };
  let taskID = '', timer, polling, reportForExport, submitting = false, stoppingID = '';

  const escape = (value) => String(value ?? '—').replace(/[&<>"']/g, (char) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  })[char]);
  const shown = (value, suffix = '') => Number.isFinite(value) ? `${Number(value.toFixed(2))}${suffix}` : '—';
  const bits = (value) => Number.isFinite(value) && value > 0
    ? value >= 1000 ? shown(value / 1000, ' Mbps') : shown(value, ' kbps')
    : '—';
  const metrics = [
    {key:'error_rate_pct', title:'分片错误率', unit:'%', delta:' 个百分点'},
    {key:'avg_ttfb_ms', title:'平均 TTFB', unit:' ms', samples:true},
    {key:'avg_download_ms', title:'平均请求耗时', unit:' ms', samples:true},
    {key:'avg_throughput_kbps', title:'平均吞吐', unit:' kbps', rate:true, samples:true},
    {key:'p95_throughput_kbps', title:'最差 5% 吞吐', unit:' kbps', rate:true, samples:true},
    {key:'cache_hit_rate_pct', title:'缓存命中率', unit:'%', delta:' 个百分点'},
  ];

  function setStatus(message, error = false) {
    status.textContent = message;
    status.classList.toggle('hls-problem', error);
  }
  function setBusy(busy, stopping = false) {
    node('hls-fields').disabled = busy;
    startButton.disabled = busy;
    stopButton.hidden = !busy || !taskID;
    stopButton.disabled = stopping;
  }
  function syncForm() {
    const compare = mode.value === 'compare', manifest = mode.value === 'manifest';
    node('hls-second').hidden = !compare;
    node('hls-url-label').textContent = compare ? 'A 路播放列表' : '播放列表地址';
    node('hls-url-b').disabled = !compare;
    node('hls-url-b').required = compare;
    node('hls-mode-hint').textContent = modes[mode.value];
    for (const field of form.querySelectorAll('[data-segment-option]')) {
      field.hidden = manifest;
      for (const input of field.querySelectorAll('input')) input.disabled = manifest;
    }
    node('hls-variant').disabled = manifest || node('hls-all').checked;
  }
  function taskURL(id) {
    const url = new URL(location.href);
    if (url.pathname === '/diagnostics') {
      url.searchParams.set('protocol', 'hls');
      url.searchParams.set('hls_mode', 'delivery');
    }
    if (id) url.searchParams.set('task', id);
    else url.searchParams.delete('task');
    history.replaceState(null, '', url);
  }
  async function request(path, options) {
    const response = await fetch(endpoint + path, {cache:'no-store', ...options});
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw Object.assign(new Error(data.error || `请求失败（HTTP ${response.status}）`), {status:response.status});
    return data;
  }
  function pollAgain(id, delay = 850) { timer = setTimeout(() => poll(id), delay); }
  async function poll(id) {
    if (id !== taskID) return;
    const controller = new AbortController();
    polling = controller;
    try {
      const data = await request('/' + id, {signal:controller.signal});
      if (taskID !== id || controller.signal.aborted) return;
      if (activeStates.has(data.status)) {
        const stopping = data.status === 'stopping' || stoppingID === id;
        setBusy(true, stopping);
        setStatus(stopping ? '正在停止，已采集的结果会保留。' : '正在读取播放列表和分片…');
        pollAgain(id);
        return;
      }
      stoppingID = '';
      setBusy(false);
      const statusText = data.status === 'canceled' ? '分析已停止' : data.status === 'failed' ? '分析未完成' : '分析完成';
      setStatus(`${statusText}${data.error ? ` · ${data.error}` : ''}`, data.status === 'failed');
      render(data.report);
    } catch (error) {
      if (taskID !== id || controller.signal.aborted) return;
      if (error.status === 404) {
        taskID = '';
        stoppingID = '';
        taskURL('');
        setBusy(false);
        setStatus('该任务已过期，请重新开始分析。');
        return;
      }
      setStatus(`读取任务失败，正在重试。${error.message}`, true);
      pollAgain(id, 1800);
    } finally {
      if (polling === controller) polling = undefined;
    }
  }
  mode.addEventListener('change', syncForm);
  node('hls-all').addEventListener('change', syncForm);
  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    if (submitting || startButton.disabled) return;
    const command = mode.value, all = node('hls-all').checked;
    const body = {
      command, url:node('hls-url').value.trim(), timeout:node('hls-timeout').value.trim(),
    };
    if (command === 'compare') body.url_b = node('hls-url-b').value.trim();
    if (command !== 'manifest') Object.assign(body, {
      segments:Number(node('hls-segments').value), all_variants:all, parse_segments:node('hls-parse').checked,
      duration:node('hls-duration').value.trim(), error_threshold:Number(node('hls-error-threshold').value),
      ...(!all && {variant:Number(node('hls-variant').value)}),
    });
    submitting = true;
    taskID = '';
    taskURL('');
    render();
    setBusy(true);
    setStatus('正在提交分析任务…');
    try {
      const created = await request('', {
        method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body),
      });
      taskID = created.id;
      taskURL(taskID);
      void poll(taskID);
    } catch (error) {
      setBusy(false);
      setStatus(error.message, true);
    } finally {
      submitting = false;
    }
  });
  stopButton.addEventListener('click', async () => {
    const id = taskID;
    if (!id) return;
    stoppingID = id;
    stopButton.disabled = true;
    try {
      await request('/' + id, {method:'DELETE'});
    } catch (error) {
      if (id === taskID) {
        stoppingID = '';
        stopButton.disabled = false;
        setStatus(error.message, true);
      }
    }
  });

  function metricValue(report, metric) {
    const stats = report?.summary;
    if (!stats || (metric.samples && !stats.segments_fetched)) return undefined;
    if (metric.key === 'error_rate_pct' && !stats.segments_fetched && !stats.segments_failed) return undefined;
    return stats[metric.key];
  }
  function formatMetric(value, metric) { return metric.rate ? bits(value) : shown(value, metric.unit); }
  function list(items) {
    return items?.length ? `<ul class="hls-notes">${items.map((item) => `<li>${escape(item)}</li>`).join('')}</ul>` : '';
  }
  function table(label, columns, rows) {
    const titles = columns.map((title) => `<th scope="col">${escape(title)}</th>`).join('');
    return `<div class="hls-table-wrap" role="region" aria-label="${escape(label)}" tabindex="0"><table><thead><tr>${titles}</tr></thead><tbody>${rows}</tbody></table></div>`;
  }
  function row(values) { return `<tr>${values.map((value) => `<td>${escape(value)}</td>`).join('')}</tr>`; }
  function compare(report) {
    const metricRows = metrics.map((metric) => {
      const left = metricValue(report.a, metric), right = metricValue(report.b, metric);
      const delta = Number.isFinite(left) && Number.isFinite(right) ? Number((right - left).toFixed(2)) : undefined;
      const difference = delta === undefined ? '—' : `${delta > 0 ? '+' : ''}${shown(delta, metric.delta || metric.unit)}`;
      return row([metric.title, formatMetric(left, metric), formatMetric(right, metric), difference]);
    }).join('');
    const direct = report.a?.summary && report.b?.summary;
    const comparison = direct ? table('A/B 指标对比', ['指标','A 路','B 路','B − A'], metricRows)
      : '<p class="hls-muted">按下方档位查看各自的指标与分片。</p>';
    return `<section class="hls-panel"><h2>A/B 对比</h2>${list(report.notes)}${comparison}</section>` +
      `<div class="hls-side-by-side"><div>${renderReports(report.a, 'A 路')}</div><div>${renderReports(report.b, 'B 路')}</div></div>`;
  }
  function segmentCategory(segment) {
    if (segment.gap) return 'gap';
    if (segment.error || segment.http_status >= 400) return 'failed';
    if (segment.declared_duration_s > 0 && segment.download_ms > segment.declared_duration_s * 1000) return 'slow';
    return 'normal';
  }
  const categories = {failed:'请求失败', gap:'宣告缺片', slow:'下载用时超出片长', normal:'正常'};
  function segmentTable(segments) {
    if (!segments?.length) return '';
    const counts = {failed:0,gap:0,slow:0};
    const rows = segments.map((segment) => {
      const kind = segmentCategory(segment), observed = kind !== 'gap' && (segment.http_status || segment.download_ms > 0);
      if (kind !== 'normal') counts[kind]++;
      const cells = [
        segment.sequence, segment.http_status, observed ? shown(segment.ttfb_ms,' ms') : '—',
        observed ? shown(segment.download_ms,' ms') : '—', shown(segment.declared_duration_s,' s'),
        bits(segment.throughput_kbps), segment.cache_status, segment.pts_start,
        segment.starts_with_keyframe === undefined ? '—' : segment.starts_with_keyframe ? '是' : '否',
      ];
      const description = [categories[kind], segment.discontinuity && 'EXT-X-DISCONTINUITY', segment.error].filter(Boolean).join(' · ');
      return `<tr data-segment-kind="${kind}">${cells.map((value) => `<td>${escape(value)}</td>`).join('')}<td class="hls-detail">${escape(description)}</td></tr>`;
    }).join('');
    const options = Object.entries(counts).map(([kind, count]) => `<option value="${kind}">${categories[kind]}（${count}）</option>`).join('');
    return `<div class="hls-section-title"><h3>分片证据</h3><select data-segment-filter aria-label="筛选分片">` +
      `<option value="all">全部（${segments.length}）</option>${options}</select></div>` +
      '<p data-segment-empty class="hls-muted" hidden>没有符合筛选条件的分片。</p>' +
      table('分片证据', ['序号','HTTP','TTFB','请求耗时','片长','吞吐','缓存','PTS','首帧关键帧','状态'], rows);
  }
  function reportCard(report, title) {
    const stats = report.summary;
    const counts = stats ? [['成功／失败／跳过', `${stats.segments_fetched}／${stats.segments_failed}／${stats.segments_skipped ?? 0}`]] : [];
    const items = [...counts, ...metrics.map((metric) => [metric.title, formatMetric(metricValue(report, metric), metric)])];
    if (report.playlist_refresh) items.push(['播放列表刷新失败', report.playlist_refresh.failures]);
    if (Number.isFinite(report.live_latency_ms)) items.push(['估算直播延迟', shown(report.live_latency_ms,' ms')]);
    if (stats?.pts_continuity) items.push(['PTS 连续性', stats.pts_continuity]);
    const cards = stats ? `<dl class="hls-metrics">${items.map(([name, value]) => `<div><dt>${name}</dt><dd>${escape(value)}</dd></div>`).join('')}</dl>` : '';
    const playlist = report.playlist || {};
    const playlistInfo = [
      report.type, playlist.live && '直播', Number.isFinite(playlist.segment_count) && `${playlist.segment_count} 个分片`,
      playlist.target_duration_s > 0 && `目标片长 ${playlist.target_duration_s} s`,
      Number.isFinite(playlist.media_sequence) && `序号 ${playlist.media_sequence}`, playlist.encrypted && '已加密',
      report.infrastructure?.cdn_detected, report.infrastructure?.edge_location, report.stale && '直播窗口未推进',
    ].filter(Boolean).join(' · ');
    const tested = report.tested_variant;
    const name = tested ? [tested.resolution, bits(tested.bandwidth_kbps)].filter(Boolean).join(' · ') : '';
    const variants = report.manifest?.variants || report.variants;
    const abr = report.abr;
    const tiers = variants?.length ? '<div class="hls-section-title"><h3>播放列表档位</h3></div>' + table('播放列表档位',
      ['分辨率','标称带宽','编码','地址'], variants.map((v) => row([v.resolution,bits(v.bandwidth_kbps),v.codecs,v.url])).join('')) : '';
    const headroom = abr?.variants?.length ? '<div class="hls-section-title"><h3>多码率余量</h3></div>' + table('多码率余量',
      ['档位','标称带宽','平均吞吐余量','最差 5% 余量','风险'], abr.variants.map((v) => row([
        v.label, bits(v.bandwidth_kbps), shown(v.headroom,'×'), shown(v.p95_headroom,'×'), v.risk,
      ])).join('')) : '';
    const notes = [
      report.playlist_refresh?.last_error, ...(report.recommendations || []),
      ...(report.infrastructure?.cache_warnings || []), ...(report.infrastructure?.playlist_warnings || []),
    ].filter(Boolean);
    return `<section class="hls-panel"><h2>${escape([title, name].filter(Boolean).join(' · '))}</h2>` +
      `<p class="hls-muted">${escape(report.url)}</p>${playlistInfo ? `<p class="hls-muted">${escape(playlistInfo)}</p>` : ''}` +
      cards + list(notes) + tiers + headroom + segmentTable(report.segments) + '</section>';
  }
  function renderReports(report, title = '交付概览') {
    if (!report) return '';
    const entries = report.results?.length ? report.results : [report];
    return entries.map((entry) => reportCard(entry, title)).join('');
  }
  function render(report) {
    reportForExport = report;
    results.hidden = !report;
    output.innerHTML = report ? ('a' in report || 'b' in report ? compare(report) : renderReports(report)) : '';
  }
  output.addEventListener('change', (event) => {
    if (!event.target.matches('[data-segment-filter]')) return;
    const view = event.target.closest('.hls-panel');
    let visible = 0;
    for (const row of view.querySelectorAll('[data-segment-kind]')) {
      row.hidden = event.target.value !== 'all' && row.dataset.segmentKind !== event.target.value;
      if (!row.hidden) visible++;
    }
    view.querySelector('[data-segment-empty]').hidden = visible > 0;
  });
  node('hls-export').addEventListener('click', () => {
    if (!reportForExport) return;
    const link = document.createElement('a');
    link.href = URL.createObjectURL(new Blob([JSON.stringify(reportForExport, null, 2)], {type:'application/json'}));
    link.download = `hls-report-${taskID}.json`;
    link.click();
    setTimeout(() => URL.revokeObjectURL(link.href), 1000);
  });
  window.addEventListener('pagehide', () => { clearTimeout(timer); polling?.abort(); });
  window.addEventListener('pageshow', (event) => { if (event.persisted && taskID) void poll(taskID); });
  syncForm();
  const requested = new URLSearchParams(location.search).get('task');
  if (/^[0-9a-f]{16,32}$/.test(requested || '')) {
    taskID = requested;
    setBusy(true);
    setStatus('正在恢复分析任务…');
    void poll(taskID);
  }
})();
