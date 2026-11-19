'use strict';

const api = '/api/simulators/onvif';
const el = id => document.getElementById(id);
const form = el('onvif-form');
const network = el('bind-ip');
let active = false;
let loaded = false;
let busy = false;
let generation = 0;
let statusError = false;

async function request(path = '', options = {}) {
  const response = await fetch(api + path, { cache: 'no-store', ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || 'ONVIF 请求失败。');
  return body;
}

function notice(message = '') {
  el('notice').textContent = message;
  el('notice').hidden = !message;
}

let motionActive = false;

function updateButtons() {
  el('simulator-fields').disabled = busy || active || !loaded;
  el('start').disabled = busy || active || !loaded || !network.value;
  el('motion').disabled = el('restart-events').disabled = busy || !active;
  el('recorded-motion').disabled = el('missing-motion').disabled = busy || !active;
  el('stop').disabled = busy || !active;
}

function render(status) {
  active = status.active;
  const state = el('state');
  const label = active ? '运行中' : '未运行';
  if (state.textContent !== label) state.textContent = label;
  state.dataset.status = active ? 'done' : '';
  el('discovery-replies').textContent = status.discovery_replies;
  el('soap-requests').textContent = status.soap_requests;
  el('rtsp-plays').textContent = status.rtsp_plays;
  el('endpoints').hidden = !active;
  el('device-url').textContent = status.device_url || '';
  el('media-url').textContent = status.media_url || '';
  el('ptz-url').textContent = status.ptz_url || '';
  el('event-url').textContent = status.event_url || '';
  motionActive = Boolean(status.events?.motion);
  el('motion-control').hidden = !active;
  el('motion').textContent = motionActive ? '结束移动侦测' : '触发移动侦测';
  const events = status.events || {};
  const recovery = events.recovery;
  el('event-recovery').hidden = !recovery;
  el('event-recovery-status').textContent = recovery ? [
    '已失效 ' + recovery.invalidated + ' 个订阅', '旧订阅故障 ' + recovery.old_faults + ' 次',
    recovery.subscribed_at && '重建订阅 ' + (Date.parse(recovery.subscribed_at) - Date.parse(recovery.restarted_at)) + ' ms',
    recovery.first_motion_at && '重新送达移动告警 ' + (Date.parse(recovery.first_motion_at) - Date.parse(recovery.restarted_at)) + ' ms',
  ].filter(Boolean).join(' · ') : '';
  el('event-metrics').textContent = [events.subscriptions + ' 个订阅', events.pull_requests + ' 次拉取',
    events.delivered + ' 条通知', events.dropped ? events.dropped + ' 条队列溢出' : ''].filter(Boolean).join(' · ');
  el('snapshot-hint').hidden = !active;
  el('rtsp-auth').textContent = status.rtsp_digest ? 'Digest' : '无鉴权';
  el('soap-auth').textContent = status.soap_digest ? [
    'UsernameToken Digest', '拒绝 ' + status.soap_auth_rejected + ' 次',
    '时钟偏差拒绝 ' + status.soap_clock_rejected + ' 次', '设备偏差 ' + status.clock_offset_seconds + ' 秒',
  ].join(' · ') : '无鉴权';
  el('transport-observation').textContent = 'UDP PLAY ' + status.udp_plays + ' 次 · TCP PLAY ' + status.tcp_plays + ' 次' +
    (status.udp_source_fault ? ' · RTP ' + status.udp_source_fault.negotiated_port + ' → ' + status.udp_source_fault.alternate_port +
      ' · 已改源发送 ' + status.udp_source_fault.alternate_packets + ' 包' : '') +
    (status.resolution_switch_at ? ' · 已切换分辨率' : '') + (status.silent_udp ? ' · UDP 无 RTP' : '');
  const recording = status.recording;
  el('recording-controls').hidden = !active || !recording;
  const playback = el('recording-playbacks');
  const logs = recording?.playback || [];
  playback.hidden = !logs.length;
  playback.replaceChildren(...logs.slice().reverse().map(trace => {
    const line = document.createElement('li');
    const text = span => new Date(span.from).toISOString() + ' → ' + new Date(span.until).toISOString();
    line.textContent = ['RTSP ' + trace.code, '请求 ' + text(trace.requested),
      trace.returned && '返回 ' + text(trace.returned), trace.parts && '实际录像 ' + trace.parts.length + ' 段',
      trace.keyframes && '送出 ' + trace.keyframes.length + ' 段首关键帧'].filter(Boolean).join(' · ');
    return line;
  }));
  if (recording) {
    const date = value => new Date(value).toLocaleString('zh-CN', {timeZone: 'UTC'});
    el('recording-window').textContent = '录像 ' + date(recording.from) + ' 至 ' + date(recording.until) + ' UTC · ' + recording.stream_url;
    el('recording-status').textContent = '有录像告警 ' + date(recording.recorded_motion) + ' · 无录像告警 ' + date(recording.missing_motion) +
      ' UTC · 成功点播 ' + recording.plays + ' 次 · 无录像时间范围 ' + recording.missing_range + ' 次';
  }
  const outages = status.rtsp_outages || [];
  el('outage-observation-row').hidden = !outages.length;
  el('outage-observation').textContent = outages.map((outage, index) => (index ? '子码流' : '主码流') + ' ' +
    (!outage.begin ? '等待点播' : outage.first_reconnected_play ? '已重新 PLAY' : Date.parse(outage.end) > Date.now() ? '暂不可用' : '等待重新连接') +
    ' · 已关闭 ' + outage.closed_sessions + ' 个会话 · 返回 503 ' + outage.rejected_requests + ' 次').join(' / ');
  const gaps = status.video_gaps || [];
  el('gap-observation-row').hidden = !gaps.length;
  el('gap-observation').textContent = gaps.map((gap, index) => (index ? '子码流' : '主码流') + ' ' +
    (gap.end ? '已恢复（' + (Date.parse(gap.end) - Date.parse(gap.begin)) + ' ms）' : gap.begin ? '停包中' : '等待 TCP 点播')).join(' · ');
  for (const [index, endpoint, detail, snapshot] of [[0, 'rtsp-url', 'main-profile-detail', 'main-snapshot'], [1, 'rtsp-sub-url', 'sub-profile-detail', 'sub-snapshot']]) {
    const profile = status.profiles?.[index];
    el(endpoint).textContent = profile?.stream_url || '';
    if (profile?.snapshot_url) el(snapshot).href = profile.snapshot_url;
    else el(snapshot).removeAttribute('href');
    el(detail).textContent = profile ? '· ' + (profile.filename || '测试画面') + ' · ' +
      profile.width + ' × ' + profile.height + ' / ' + profile.fps + ' fps' : '';
  }
  updateButtons();
}

async function poll() {
  const current = generation;
  if (!busy) {
    try {
      const status = await request();
      if (current === generation && !busy) {
        if (statusError) notice();
        statusError = false;
        render(status);
      }
    } catch (error) {
      if (current === generation && !busy) {
        statusError = true;
        notice(error.message);
      }
    }
  }
  window.setTimeout(poll, 1000);
}

async function configure() {
  try {
    const addresses = await request('/interfaces');
    network.replaceChildren(...addresses.map(address => new Option(address.ip + ' · ' + address.interface, address.ip)));
    if (addresses.some(address => address.ip === '127.0.0.1')) network.value = '127.0.0.1';
    if (!addresses.length) notice('没有可用于 ONVIF 的 IPv4 网卡。');
    loaded = true;
    updateButtons();
  } catch (error) {
    notice(error.message);
  }
}

async function change(path, body) {
  generation++;
  busy = true;
  statusError = false;
  notice();
  updateButtons();
  try {
    const options = {method: 'POST'};
    if (body instanceof FormData) options.body = body;
    else if (body) {
      options.headers = {'Content-Type': 'application/json'};
      options.body = JSON.stringify(body);
    }
    if (path === '/start-file') el('state').textContent = '正在准备视频';
    render(await request(path, options));
    if (body) {
      el('rtsp-password').value = '';
      el('soap-password').value = '';
    }
  } catch (error) {
    if (path === '/start-file') el('state').textContent = '未运行';
    notice(error.message);
  } finally {
    busy = false;
    updateButtons();
  }
}

form.addEventListener('submit', event => {
  event.preventDefault();
  const config = Object.fromEntries(new FormData(form));
  config.http_port = Number(config.http_port);
  config.rtsp_port = Number(config.rtsp_port);
  config.clock_offset_seconds = Number(config.clock_offset_seconds);
  config.silent_udp = el('silent-udp').checked;
  config.recording_fixture = el('recording-fixture').checked || el('recording-gap').checked;
  config.recording_gap_seconds = el('recording-gap').checked ? 8 : 0;
  config.rtsp_disconnect_ms = el('rtsp-outage').checked ? 3000 : 0;
  config.udp_source_fault_ms = el('udp-source-fault').checked ? 2200 : 0;
  config.rtsp_resolution_switch_ms = el('resolution-switch').checked ? 3000 : 0;
  config.rtcp_clock_step_ms = el('rtcp-step').checked ? 350 : 0;
  config.video_gap_ms = el('video-gap').checked ? 5000 : 0;
  const files = [['main', el('main-video').files[0]], ['sub', el('sub-video').files[0]]].filter(([, file]) => file);
  if (!files.length) {
    void change('/start', config);
    return;
  }
  if (files.some(([, file]) => file.size > 64 * 1024 * 1024)) {
    notice('每路本地视频最多 64 MiB。');
    return;
  }
  const body = new FormData();
  body.append('config', JSON.stringify(config));
  for (const [name, file] of files) body.append(name, file);
  void change('/start-file', body);
});
const rtspFaults = ['rtsp-outage', 'video-gap', 'rtcp-step', 'resolution-switch', 'udp-source-fault'];
for (const selected of rtspFaults) {
  el(selected).addEventListener('change', () => {
    if (el(selected).checked) {
      if (selected === 'udp-source-fault') el('silent-udp').checked = false;
      for (const other of rtspFaults.filter(value => value !== selected)) el(other).checked = false;
    }
  });
}
el('stop').addEventListener('click', () => { void change('/stop'); });
el('silent-udp').addEventListener('change', () => { if (el('silent-udp').checked) el('udp-source-fault').checked = false; });
el('recorded-motion').addEventListener('click', () => { void change('/recording-motion', {recorded: true}); });
el('missing-motion').addEventListener('click', () => { void change('/recording-motion', {recorded: false}); });
el('restart-events').addEventListener('click', () => { void change('/restart-events'); });
el('motion').addEventListener('click', () => { void change('/motion', {active: !motionActive}); });
void configure();
void poll();
