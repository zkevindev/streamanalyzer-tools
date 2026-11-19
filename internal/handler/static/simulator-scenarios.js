'use strict';

const el = id => document.getElementById(id);
const gb = '/api/simulators/gb28181';
const platform = gb + '/platform';
const onvif = '/api/simulators/onvif';
const hls = '/api/simulators/hls';
const runs = '/api/diagnostics/runs';
const ids = {
  platform_id: '34020000002000000001',
  device_id: '34020000001320000001',
  channel_id: '34020000001320000002',
};
const definitions = new Map([...el('scenario').options].map(option => [option.value, option.textContent]));
const storagePrefix = 'streamanalyzer.simulator.scenarios.v1.';
let busy = false;

function load(key) {
  try {
    const parsed = JSON.parse(localStorage.getItem(storagePrefix + key) || '[]');
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

let presets = load('presets');
let history = load('runs');

function persist(key, value) {
  try {
    localStorage.setItem(storagePrefix + key, JSON.stringify(value));
  } catch {
    notice('浏览器无法持久保存记录，可在当前页面导出 JSON。');
  }
}

function validate(data) {
  const name = typeof data?.name === 'string' ? data.name.trim() : '';
  if (data?.version !== 1 || !name || name.length > 80 || !definitions.has(data.scenario)) {
    throw new Error('场景预设格式无效。');
  }
  return {version: 1, name, scenario: data.scenario};
}

function formPreset() {
  return validate({version: 1, name: el('name').value, scenario: el('scenario').value});
}

function notice(message = '') {
  el('notice').textContent = message;
  el('notice').hidden = !message;
}

function progress(text) {
  el('progress').textContent = text;
}

function refreshPresets() {
  el('preset').replaceChildren(new Option('选择预设', ''), ...presets.map((preset, i) => new Option(preset.name, i)));
}

function savePreset(preset) {
  presets = [preset, ...presets.filter(item => item.name !== preset.name)].slice(0, 20);
  persist('presets', presets);
  refreshPresets();
  el('preset').value = '0';
}

function download(name, value) {
  const href = URL.createObjectURL(new Blob([JSON.stringify(value, null, 2)], {type: 'application/json'}));
  const link = document.createElement('a');
  link.href = href;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(href), 1000);
}

function renderHistory(scenario) {
  const list = el('run-history');
  list.hidden = !history.length;
  el('empty-history').hidden = Boolean(history.length);
  list.replaceChildren(...history.map(entry => {
    const row = document.createElement('li');
    const time = document.createElement('time');
    time.textContent = new Date(entry.started_at).toLocaleString('zh-CN');
    const detail = document.createElement('span');
    detail.textContent = entry.preset.name + ' · ' + (entry.passed ? '通过' : '失败') + ' · ' + entry.detail;
    const exportRun = document.createElement('button');
    exportRun.className = 'secondary';
    exportRun.type = 'button';
    exportRun.textContent = '导出结果';
    exportRun.addEventListener('click', () => download('simulator-result.json', entry));
    detail.append(' ', exportRun);
    if (entry.diagnostic_id) {
      const report = document.createElement('a');
      report.href = runs + '/' + encodeURIComponent(entry.diagnostic_id) + '/report.html';
      report.textContent = ' 播放报告';
      detail.append(' ', report);
    }
    row.append(time, detail);
    return row;
  }));
  const comparison = el('comparison');
  const pair = history.filter(entry => entry.preset.scenario === scenario).slice(0, 2);
  comparison.hidden = pair.length < 2;
  if (pair.length === 2) {
    const outcome = entry => (entry.passed ? '通过' : '失败') + '（' + entry.elapsed_ms + ' ms）';
    comparison.textContent = definitions.get(scenario) + ' · 上次 ' + outcome(pair[1]) + ' · 本次 ' + outcome(pair[0]);
  }
}

async function request(path, config, token) {
  const options = {cache: 'no-store', signal: AbortSignal.timeout(5000)};
  if (config !== undefined) {
    options.method = 'POST';
    if (config !== null) {
      options.headers = {'Content-Type': 'application/json'};
      options.body = JSON.stringify(config);
    }
  }
  if (token) options.headers['X-StreamAnalyzer-Player'] = token;
  const response = await fetch(path, options);
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || '请求失败（HTTP ' + response.status + '）');
  return data;
}

async function waitFor(test, limit, error) {
  const deadline = performance.now() + limit;
  for (;;) {
    const result = await test();
    if (result) return result;
    if (performance.now() >= deadline) throw new Error(error);
    await new Promise(resolve => setTimeout(resolve, 150));
  }
}

async function stopOwned(path, field, value) {
  if (value && (await request(path))[field] === value) await request(path + '/stop', null);
}

async function runGB(expected, entry) {
  let bridge;
  let device;
  const recovery = {tcp_recovery: 'sip_tcp_disconnect', udp_recovery: 'udp_keepalive_timeout'}[expected];
  const sipTransport = expected === 'tcp_recovery' ? 'tcp' : 'udp';
  try {
    progress('启动测试平台和设备…');
    bridge = await request(platform + '/start', {...ids, sip_transport: sipTransport});
    const [host, port] = bridge.address.split(':');
    device = await request(gb + '/start', {
      ...ids, platform_host: host, platform_port: Number(port), sip_transport: sipTransport,
      ...(recovery && {heartbeat_seconds: 5}),
      ...(expected === 'no_rtp' ? {faults: {first_delay_ms: 3000}} : {}),
    });
    await waitFor(async () => {
      const state = await request(gb);
      if (state.state === 'failed') throw new Error(state.error || '设备注册失败');
      return state.state === 'registered' && (await request(platform)).peer;
    }, 6000, '设备未在观测窗口内完成注册。');
    progress(recovery ? '首次出画后注入 SIP 故障，等待重注册和恢复出画…' : '点播并观察 ACK、RTP 和关键帧…');
    await request(platform + '/preview', {
      channel_id: ids.channel_id, window_ms: 2300, skip_ack: expected === 'no_ack', ...(recovery && {recovery}),
    });
    const observed = await waitFor(async () => {
      const state = await request(platform);
      return !state.busy && state.preview ? state : null;
    }, recovery ? 45000 : 9000, '平台点播观测超时。');
    entry.evidence = observed;
    if (recovery) {
      const report = observed.recovery;
      const oldEnded = report?.previous_session?.timeline.some(mark => mark.stage === 'ended');
      entry.passed = Boolean(report?.state === 'passed' && oldEnded && report.before.call_id !== observed.preview.call_id &&
        observed.device_session?.call_id === observed.preview.call_id);
      entry.detail = entry.passed ? '重注册、目录和新点播均通过 · 首个关键帧恢复 ' + report.recovery_ms + ' ms' :
        (report?.error || '恢复证据缺少旧会话释放或新会话');
      return;
    }
    entry.passed = observed.preview.state === expected && observed.device_session?.call_id === observed.preview.call_id;
    entry.detail = '预期 ' + expected + '，实际 ' + observed.preview.state;
  } finally {
    if (device) await stopOwned(gb, 'platform', bridge.address).catch(() => {});
    if (device) {
      await waitFor(async () => !(await request(gb)).active, 3500, '设备关闭超时。').catch(() => {});
    }
    if (bridge) await stopOwned(platform, 'address', bridge.address).catch(() => {});
  }
}

function frameState(video, player) {
  const quality = video.getVideoPlaybackQuality?.();
  return {
    time: video.currentTime,
    presented: player.presentedFrames ?? (quality ? Math.max(0, quality.totalVideoFrames - quality.droppedVideoFrames) : 0),
  };
}

async function sampleRTSPReconnect(source, video, first) {
  const startedAt = Date.now();
  const startedAtPerformanceMS = performance.now();
  const run = await request(runs, {protocol: 'rtsp', url: source.stream_url, transport: 'tcp', seconds: first ? 9 : 5, playback: true});
  const path = runs + '/' + run.id;
  let player;
  let frameAt = 0;
  try {
    if (!run.player) throw new Error('未返回 RTSP 预览配置。');
    player = new PlayerDiagnostics(video, run.player, {
      startedAt, startedAtPerformanceMS,
      onChange(event) { if (event.kind === 'first_frame') frameAt = Date.now(); },
    });
    player.start();
    const detail = await waitFor(async () => {
      const sample = await request(path);
      return (sample.status !== 'running' || !first && frameAt) ? sample : null;
    }, first ? 9000 : 6500, 'RTSP 会话观测超时。');
    await request(path + '/player', player.finish(), run.player.token);
    player = null;
    if (detail.status === 'running') await request(path + '/stop', null);
    const completed = detail.report ? detail : await waitFor(async () => {
      const state = await request(path);
      return state.report ? state : null;
    }, 4000, 'RTSP 采样报告生成超时。');
    return {id: run.id, frame_at: frameAt, end_reason: completed.report.manifest.observation.end_reason,
      requests: (completed.report.rtsp?.requests || []).map(({method, status_code}) => ({method, status_code}))};
  } finally {
    if (player) await request(path + '/player', player.finish(), run.player.token).catch(() => {});
    const latest = await request(path).catch(() => null);
    if (latest?.status === 'running') await request(path + '/stop', null).catch(() => {});
  }
}

async function runRTSPReconnect(entry) {
  const video = el('scenario-player');
  const attempts = [];
  let source;
  try {
    progress('启动摄像头并等待正常出画…');
    source = await request(onvif + '/start', {bind_ip: '127.0.0.1', rtsp_disconnect_ms: 3000});
    video.hidden = false;
    entry.evidence = {attempts};
    const before = await sampleRTSPReconnect(source, video, true);
    attempts.push(before);
    const state = await request(onvif);
    const outage = state.rtsp_outages?.find(item => item.profile === source.profiles[0].token);
    if (!outage?.begin || outage.closed_sessions < 1 || !before.frame_at || before.frame_at > Date.parse(outage.begin) ||
      !['eof', 'rtsp_error'].includes(before.end_reason)) {
      throw new Error('未观察到正常出画后会话断开。');
    }
    progress('旧会话已断开，正在重新建立 RTSP 会话…');
    const recovered = await waitFor(async () => {
      if (attempts.length >= 12) throw new Error('已达到 RTSP 重连次数上限。');
      const next = await sampleRTSPReconnect(source, video, false);
      attempts.push(next);
      if (next.frame_at) return next;
      await new Promise(resolve => setTimeout(resolve, 350));
      return null;
    }, 15000, '恢复窗口内未重新出画。');
    const finalState = await request(onvif);
    entry.evidence.outage = finalState.rtsp_outages?.find(item => item.profile === source.profiles[0].token);
    entry.evidence.reconnect_attempts = attempts.length - 1;
    entry.evidence.recovery_ms = recovered.frame_at - Date.parse(outage.begin);
    entry.diagnostic_id = recovered.id;
    const reestablished = ['DESCRIBE', 'SETUP', 'PLAY'].every(method =>
      recovered.requests.some(item => item.method === method && item.status_code === 200));
    entry.passed = Boolean(reestablished && entry.evidence.outage?.first_reconnected_play && entry.evidence.recovery_ms >= 0);
    entry.detail = entry.passed ? '新会话已出画 · 重连 ' + entry.evidence.reconnect_attempts + ' 次 · 恢复 ' +
      entry.evidence.recovery_ms + ' ms' : '报告缺少新会话的控制或呈帧证据';
  } finally {
    if (source) await stopOwned(onvif, 'device_url', source.device_url).catch(() => {});
    video.hidden = true;
  }
}

async function runHLS(kind, entry) {
  let source;
  let run;
  let player;
  let saved = false;
  const video = el('scenario-player');
  try {
    if (!globalThis.Hls?.isSupported()) throw new Error('此浏览器需要支持 hls.js 的媒体扩展。');
    progress('启动滚动直播并等待播放器出画…');
    source = await request(hls + '/start', {mode: 'live'});
    run = await request(runs, {protocol: 'hls', url: new URL(source.playlist, location.origin).href, seconds: 22, playback: true});
    entry.diagnostic_id = run.id;
    if (!run.player) throw new Error('未返回播放配置。');
    video.hidden = false;
    player = new PlayerDiagnostics(video, run.player, {hlsRecovery: true});
    player.start();
    await waitFor(() => frameState(video, player).presented > 0 && video.currentTime > 0.3, 7000, '未观察到故障前出画。');
    const armed = await request(hls + '/live-fault', {kind});
    entry.evidence = {fault: armed.live_fault};
    progress('等待首次分片请求失败及同分片重试…');
    const retried = await waitFor(async () => {
      const result = await request(hls);
      if (result.id !== source.id) throw new Error('本次直播源已被替换。');
      entry.evidence.fault = result.live_fault;
      return result.live_fault?.retry_at ? result.live_fault : null;
    }, 9000, '未观察到分片重试。');
    const before = frameState(video, player);
    // The 2 s segments start at sequence 1. Wait until frames enter
    // the failed segment, beyond frames already buffered before the retry.
    const targetTime = Math.max(before.time + 0.4, (retried.sequence - 1) * 2 + 0.2);
    const resumed = await waitFor(() => {
      const now = frameState(video, player);
      return now.presented > before.presented && now.time > targetTime ? now : null;
    }, 8500, '重试之后未观察到目标分片继续呈帧。');
    entry.evidence.resumed_at = new Date().toISOString();
    entry.evidence.frames_after_retry = resumed.presented - before.presented;

    const runPath = runs + '/' + run.id;
    await request(runPath + '/player', player.finish(), run.player.token);
    saved = true;
    await request(runPath + '/stop', null);
    const report = await waitFor(async () => {
      const detail = await request(runPath);
      return detail.report?.player_network ? detail.report : null;
    }, 6000, '播放器证据生成超时。');
    const requests = report.player_network.requests || [];
    const failed = requests.find(item => item.kind === 'media' &&
      (kind === 'not_found' ? item.http_status === 404 : item.http_status === 200 && item.bytes > 0 && item.outcome === 'transport_error'));
    const recovered = failed && requests.find(item => item.number > failed.number &&
      item.resource_id === failed.resource_id && item.outcome === 'complete' && item.http_status === 200);
    entry.evidence.requests = [failed, recovered].filter(Boolean);
    entry.passed = Boolean(failed && recovered);
    entry.detail = entry.passed ? '同分片重试后继续呈帧 · ' + retried.attempts + ' 次请求' : '报告中未找到同一分片的失败和恢复';
  } finally {
    if (player && !saved) await request(runs + '/' + run.id + '/player', player.finish(), run.player.token).catch(() => {});
    if (run && !saved) await request(runs + '/' + run.id + '/stop', null).catch(() => {});
    if (source) await stopOwned(hls, 'id', source.id).catch(() => {});
    video.hidden = true;
  }
}

el('scenario-form').addEventListener('submit', async event => {
  event.preventDefault();
  if (busy) return;
  let preset;
  try { preset = formPreset(); } catch (error) { notice(error.message); return; }
  const [family, expected] = preset.scenario.split('-');
  const started = performance.now();
  const entry = {preset, started_at: new Date().toISOString(), passed: false};
  busy = el('config').disabled = true;
  notice();
  try {
    if (family === 'gb') await runGB(expected, entry);
    else if (family === 'rtsp') await runRTSPReconnect(entry);
    else await runHLS(expected, entry);
  } catch (error) {
    entry.detail = error.message;
  } finally {
    entry.elapsed_ms = Math.round(performance.now() - started);
    history = [entry, ...history].slice(0, 12);
    persist('runs', history);
    progress((entry.passed ? '场景通过' : '场景失败') + ' · ' + entry.detail);
    renderHistory(preset.scenario);
    busy = el('config').disabled = false;
  }
});

el('save').addEventListener('click', () => {
  try { savePreset(formPreset()); } catch (error) { notice(error.message); }
});
el('export-preset').addEventListener('click', () => {
  try { download('simulator-preset.json', formPreset()); } catch (error) { notice(error.message); }
});
el('preset').addEventListener('change', event => {
  if (event.currentTarget.value === '') return;
  const preset = presets[Number(event.currentTarget.value)];
  el('name').value = preset.name;
  el('scenario').value = preset.scenario;
  renderHistory(preset.scenario);
});
el('scenario').addEventListener('change', () => renderHistory(el('scenario').value));
el('import-preset').addEventListener('change', async event => {
  const control = event.currentTarget;
  try {
    const file = control.files[0];
    if (!file) return;
    if (file.size > 4096) throw new Error('预设文件最多 4 KiB。');
    const preset = validate(JSON.parse(await file.text()));
    savePreset(preset);
    el('name').value = preset.name;
    el('scenario').value = preset.scenario;
    renderHistory(preset.scenario);
  } catch (error) {
    notice(error.message);
  } finally {
    control.value = '';
  }
});
refreshPresets();
renderHistory(el('scenario').value);
