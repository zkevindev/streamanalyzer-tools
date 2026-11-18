'use strict';

const api = '/api/simulators/gb28181';
const el = (id) => document.getElementById(id);
const form = el('simulator-form');
const events = el('events');
const states = {
  idle: '未启动', registering: '正在注册', registered: '已注册',
  retrying: '正在重注册', stopping: '正在停止', stopped: '已停止', failed: '注册失败',
};
const mediaStates = { idle: '等待点播', waiting_ack: '等待 ACK', starting_source: '正在读取视频', streaming: '正在推流', failed: '点播失败' };
const mediaTransports = { udp: 'UDP', 'tcp-active': 'TCP（设备主动连接）', 'tcp-passive': 'TCP（设备被动监听）' };
const audioLabels = { g711a: 'G.711 A-law', g711u: 'G.711 μ-law', none: '仅视频' };
const videoFile = el('video-file');
const directions = { in: '收到', out: '发送', system: '事件' };
const numberFields = ['platform_port', 'local_port', 'heartbeat_seconds'];

let active = false;
let loaded = false;
let busy = false;
let generation = 0;
let pollError = false;

async function request(path = '', body) {
  const options = { cache: 'no-store' };
  if (path) {
    options.method = 'POST';
    if (body instanceof FormData) {
      options.body = body;
    } else if (body) {
      options.headers = { 'Content-Type': 'application/json' };
      options.body = JSON.stringify(body);
    }
  }
  const response = await fetch(api + path, options);
  const result = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(result.error || '模拟器请求失败。');
  return result;
}

function notice(message = '') {
  el('notice').hidden = !message;
  el('notice').textContent = message;
}

function updateButtons(state = el('state').dataset.state) {
  el('simulator-fields').disabled = busy || active || !loaded;
  el('audio-codec').disabled = videoFile.files.length === 0;
  el('clear-video').hidden = videoFile.files.length === 0 || active;
  el('start').disabled = busy || active || !loaded;
  el('stop').disabled = busy || !active || state === 'stopping';
}

function render(status) {
  active = status.active;
  loaded = true;
  const badge = el('state');
  const label = states[status.state] || status.state;
  if (badge.textContent !== label) badge.textContent = label;
  badge.dataset.state = status.state;
  badge.dataset.status = { registered: 'done', failed: 'failed', retrying: 'partial', registering: 'running' }[status.state] || '';
  el('local-sip-label').textContent = status.sip_transport
    ? `本机 SIP · ${status.sip_transport.toUpperCase()}` : '本机 SIP';
  el('local-address').textContent = status.local_address || '—';
  el('platform-address').textContent = status.platform || '—';
  let mediaLabel = mediaStates[status.media_state] || '—';
  if (status.media_state === 'idle' && status.state !== 'registered') mediaLabel = '—';
  if (status.media_state === 'connecting') {
    mediaLabel = status.media_transport === 'tcp-passive' ? '等待平台连入' : '正在连接平台';
  }
  el('media-state').textContent = mediaLabel;
  el('rtp-packets').textContent = status.rtp_packets.toLocaleString('zh-CN');
  el('device-info').hidden = !status.device_id;
  el('device-info').textContent = status.device_id ? `设备 ${status.device_id} · 通道 ${status.channel_id}` : '';
  renderSource(status);
  const route = [
    mediaTransports[status.media_transport],
    status.media_local && `本机 ${status.media_local}`,
    status.media_target && `对端 ${status.media_target}`,
  ].filter(Boolean).join(' · ');
  el('media-route').hidden = !route;
  el('media-route').textContent = route;
  el('media-error').hidden = !status.media_error;
  if (el('media-error').textContent !== (status.media_error || '')) el('media-error').textContent = status.media_error || '';
  el('state-error').hidden = !status.error;
  if (el('state-error').textContent !== (status.error || '')) el('state-error').textContent = status.error || '';
  updateButtons();
  renderEvents(status.events);
}

function renderSource(status) {
  const source = status.media_source || {};
  const output = source.video_mode === 'copy' ? 'H.264 直拷'
    : source.video_mode === 'transcode' ? `${source.video_codec?.toUpperCase() || '源视频'} 转 H.264` : 'H.264';
  const filename = source.filename || '内置测试画面';
  el('media-source').hidden = !source.video_mode;
  el('media-source').textContent = `${filename} · ${output} · ${audioLabels[source.audio_codec] || ''}`;
  const video = status.video_frames || 0;
  const audio = status.audio_packets || 0;
  el('media-units').hidden = !video && !audio;
  el('media-units').textContent = `已发送视频 ${video.toLocaleString('zh-CN')} 帧 · 音频 ${audio.toLocaleString('zh-CN')} 包`;
}

function renderEvents(items) {
  el('empty-events').hidden = items.length > 0;
  el('export-trace').hidden = items.length === 0;
  const retained = new Set(items.map((item) => String(item.id)));
  for (const row of [...events.children]) {
    if (!retained.has(row.dataset.id)) row.remove();
  }
  const latest = Number(events.firstElementChild?.dataset.id || 0);
  for (const item of items) {
    if (item.id <= latest) continue;
    const row = document.createElement('li');
    row.dataset.id = item.id;
    const timestamp = document.createElement('time');
    timestamp.className = 'event-time';
    timestamp.dateTime = item.time;
    timestamp.textContent = new Date(item.time).toLocaleTimeString('zh-CN', { hour12: false });
    const direction = document.createElement('span');
    direction.className = 'status event-direction';
    direction.dataset.direction = item.direction;
    direction.textContent = directions[item.direction] || item.direction;
    const summary = document.createElement('span');
    summary.className = 'event-summary';
    summary.textContent = item.summary;
    if (item.packet) {
      const details = document.createElement('details');
      const heading = document.createElement('summary');
      const packet = document.createElement('pre');
      packet.className = 'event-packet';
      packet.textContent = item.packet;
      heading.append(timestamp, direction, summary);
      details.append(heading, packet);
      row.append(details);
    } else {
      const heading = document.createElement('div');
      heading.className = 'event-row';
      heading.append(timestamp, direction, summary);
      row.append(heading);
    }
    events.prepend(row);
  }
}

async function poll() {
  const current = generation;
  if (!busy) {
    try {
      const status = await request();
      if (current === generation && !busy) {
        if (pollError) {
          notice();
          pollError = false;
        }
        render(status);
      }
    } catch (error) {
      if (current === generation && !busy) {
        pollError = true;
        notice(error.message);
      }
    }
  }
  window.setTimeout(poll, active ? 1000 : 2500);
}

async function change(path, body) {
  generation++;
  busy = true;
  pollError = false;
  if (path === '/start-file') el('start').textContent = '正在准备视频…';
  updateButtons();
  notice();
  try {
    const status = await request(path, body);
    if (path !== '/stop') {
      events.replaceChildren();
      el('password').value = '';
    }
    render(status);
  } catch (error) {
    notice(error.message);
  } finally {
    busy = false;
    el('start').textContent = '启动模拟设备';
    updateButtons();
  }
}

form.addEventListener('submit', (event) => {
  event.preventDefault();
  const config = Object.fromEntries(new FormData(form));
  for (const name of numberFields) config[name] = Number(config[name]);
  const file = videoFile.files[0];
  if (!file) {
    void change('/start', config);
    return;
  }
  if (file.size > 512 * 1024 * 1024) {
    notice('本地视频超过 512 MiB。');
    return;
  }
  const body = new FormData();
  body.append('config', JSON.stringify(config));
  body.append('video', file);
  void change('/start-file', body);
});
videoFile.addEventListener('change', () => { updateButtons(); });
el('clear-video').addEventListener('click', () => {
  videoFile.value = '';
  updateButtons();
  videoFile.focus();
});
el('stop').addEventListener('click', () => { void change('/stop'); });

if (window.parent !== window) {
  let desktop = false;
  let exporting = false;
  const feedback = el('export-feedback');
  el('export-trace').addEventListener('click', (event) => {
    if (!desktop) return;
    event.preventDefault();
    if (exporting) return;
    exporting = true;
    feedback.hidden = false;
    feedback.classList.remove('error');
    feedback.textContent = '请选择 SIP 信令的保存位置。';
    window.parent.postMessage({ kind: 'save-artifact', name: 'gb28181-trace.json' }, '*');
  });
  window.addEventListener('message', (event) => {
    if (event.source !== window.parent) return;
    if (event.data?.kind === 'desktop-shell') desktop = true;
    if (event.data?.kind === 'artifact-result' && exporting) {
      exporting = false;
      feedback.textContent = event.data.status || '';
      feedback.hidden = !feedback.textContent;
      feedback.classList.toggle('error', Boolean(event.data.error));
    }
  });
  window.parent.postMessage({ kind: 'workbench-ready' }, '*');
}
void poll();
