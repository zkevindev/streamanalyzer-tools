'use strict';

const api = '/api/simulators/gb28181';
const el = (id) => document.getElementById(id);
const form = el('simulator-form');
const events = el('events');
const states = {
  idle: '未启动', queued: '等待注册', registering: '正在注册', registered: '已注册',
  retrying: '正在重注册', stopping: '正在停止', stopped: '已停止', failed: '注册失败',
};
const mediaStates = { idle: '等待点播', waiting_ack: '等待 ACK', starting_source: '正在读取视频', delaying: '延迟起播', paused: '录像已暂停', playback_done: '录像回放结束', streaming: '正在推流', failed: '点播失败' };
const mediaTransports = { udp: 'UDP', 'tcp-active': 'TCP（设备主动连接）', 'tcp-passive': 'TCP（设备被动监听）' };
const audioLabels = { g711a: 'G.711 A-law', g711u: 'G.711 μ-law', none: '仅视频' };
const videoFile = el('video-file');
const directions = { in: '收到', out: '发送', system: '事件' };
const numberFields = ['platform_port', 'local_port', 'heartbeat_seconds', 'device_count', 'channels_per_device', 'register_interval_ms'];
const devicePicker = el('device-picker');
const channelPicker = el('channel-picker');

const sessionStages = {
  invite: '收到 INVITE', invite_ok: '发送 INVITE 200 OK', ack: '收到 ACK',
  media_connected: '媒体就绪', first_rtp: '发出首个 RTP 包', first_keyframe: '发完首个视频关键帧',
  bye: '收到 BYE', ended: '会话结束', failed: '会话失败',
  fault_delay: '注入起播延迟', fault_loss: '注入 UDP 丢包', fault_reorder: '注入 UDP 乱序',
  playback_pause: '暂停回放', playback_play: '继续回放', playback_seek: '定位录像', playback_done: '录像回放结束',
  fault_timestamp: '注入时间戳跳变', fault_mute: '中断音频', fault_udp_only: '跳过 UDP 包故障',
};
let liveSessions = [];
let active = false;
let loaded = false;
let busy = false;
let generation = 0;
let pollError = false;

async function request(path = '', body, query = '') {
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
  const response = await fetch(api + path + query, options);
  const result = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(result.error || '模拟器请求失败。');
    error.status = response.status;
    throw error;
  }
  return result;
}

function notice(message = '') {
  el('notice').hidden = !message;
  el('notice').textContent = message;
}

let selectedStatus;

function updateButtons() {
  el('simulator-fields').disabled = busy || active || !loaded;
  el('audio-codec').disabled = videoFile.files.length === 0;
  el('recording-config').hidden = videoFile.files.length === 0;
  el('clear-video').hidden = videoFile.files.length === 0 || active;
  el('start').disabled = busy || active || !loaded;
  el('stop').disabled = busy || !active;
  el('switch-channel-state').disabled = busy || !active || selectedStatus?.state !== 'registered' ||
    selectedStatus?.device_id !== devicePicker.value || selectedStatus?.channel_id !== channelPicker.value;
}

function selectionQuery(includeChannel = true) {
  const params = new URLSearchParams();
  if (devicePicker.value) params.set('device_id', devicePicker.value);
  if (includeChannel && channelPicker.value) params.set('channel_id', channelPicker.value);
  const query = params.toString();
  return query ? '?' + query : '';
}

function renderPicker(select, entries, selected) {
  if ([...select.options].map(option => option.value).join(',') !== entries.map(entry => entry.id).join(',')) {
    select.replaceChildren(...entries.map(entry => new Option(entry.label, entry.id)));
  } else {
    entries.forEach((entry, index) => {
      if (select.options[index].text !== entry.label) select.options[index].text = entry.label;
    });
  }
  select.value = selected || '';
  select.disabled = entries.length < 2;
}

function renderTargets(status) {
  const devices = status.devices || [];
  const channels = status.channels || [];
  el('simulator-targets').hidden = devices.length < 2 && channels.length < 2;
  const registered = devices.filter(device => device.state === 'registered').length;
  const playing = devices.reduce((total, device) => total + device.active_media, 0);
  el('fleet-summary').textContent = `已注册 ${registered} / ${devices.length} 台 · ${playing} 路媒体会话`;
  renderPicker(devicePicker, devices.map(device => ({
    id: device.device_id,
    label: `${device.device_id} · ${states[device.state] || device.state}`,
  })), status.device_id);
  renderPicker(channelPicker, channels.map(channel => ({
    id: channel.channel_id,
    label: `${channel.channel_id} · ${channel.online === false ? '离线' : (mediaStates[channel.media_state] || '正在连接')}`,
  })), status.channel_id);
  el('export-trace').href = api + '/trace.json' + selectionQuery(false);
}

function render(status) {
  active = status.active;
  selectedStatus = status;
  loaded = true;
  el('channel-controls').hidden = !status.channel_id;
  el('switch-channel-state').textContent = status.channel_online ? '通道下线' : '通道上线';
  el('catalog-subscriptions').textContent = '目录订阅 ' + (status.catalog_subscriptions || 0);
  renderTargets(status);
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
  el('media-state').textContent = status.channel_id && !status.channel_online ? '通道离线' : mediaLabel;
  el('rtp-packets').textContent = status.rtp_packets.toLocaleString('zh-CN');
  el('device-info').hidden = !status.device_id;
  el('device-info').textContent = status.device_id ? `设备 ${status.device_id} · 通道 ${status.channel_id}` : '';
  renderSource(status);
  renderBroadcast(status.broadcast);
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
  renderSessions(status.sessions || []);
}

function renderTimeline(target, marks) {
  target.hidden = !marks.length;
  target.replaceChildren(...marks.map(mark => {
    const row = document.createElement('li');
    const time = document.createElement('time');
    time.textContent = '+' + Number(mark.offset_ms).toFixed(1) + ' ms';
    const message = document.createElement('span');
    message.textContent = (sessionStages[mark.stage] || mark.stage) + (mark.detail ? ' · ' + mark.detail : '');
    row.append(time, message);
    return row;
  }));
}

function renderSessions(sessions) {
  liveSessions = sessions;
  const select = el('sessions');
  const selected = select.value;
  const ids = sessions.map(session => session.id);
  if ([...select.options].map(option => option.value).join(',') !== ids.join(',')) {
    select.replaceChildren(...sessions.map(session => new Option(session.call_id, session.id)));
    if (!sessions.length) select.append(new Option('等待点播', ''));
    if (ids.includes(selected)) select.value = selected;
  }
  select.disabled = !sessions.length;
  const session = sessions.find(candidate => candidate.id === select.value);
  renderTimeline(el('session-timeline'), session?.timeline || []);
  const capture = el('session-capture');
  capture.hidden = !session;
  capture.textContent = session?.capture_enabled
    ? 'RTP 采样 ' + (session.capture_bytes || 0).toLocaleString('zh-CN') + ' 字节' + (session.capture_truncated ? '（已达上限）' : '')
    : '未启用 RTP 采样';
  if (session?.mode === 'playback') capture.textContent = '录像回放 · ' + capture.textContent;
  const stats = session?.fault_stats;
  if (stats) capture.textContent += ' · 注入丢包 ' + stats.dropped_rtp + ' · 调序 ' + stats.reordered_rtp + ' · 中断音频 ' + stats.muted_audio;
  const download = el('export-evidence');
  download.hidden = !session;
  if (session) download.href = api + '/sessions/' + session.id + '/evidence.zip';
}

el('sessions').addEventListener('change', () => renderSessions(liveSessions));
el('import-evidence').addEventListener('change', async event => {
  const input = event.currentTarget;
  const file = input.files[0];
  if (!file) return;
  input.disabled = true;
  const output = el('evidence-review');
  output.hidden = false;
  output.textContent = '正在校验证据…';
  renderTimeline(el('review-timeline'), []);
  el('review-signaling').hidden = true;
  try {
    const response = await fetch(api + '/evidence/import', { method: 'POST', body: file });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || '导入失败');
    const r = data.rtp;
    output.textContent = [data.session.device_id, data.session.channel_id, data.session.call_id].filter(Boolean).join(' · ') + (r
      ? ' · RTP ' + r.packets + ' 包 · 序列缺口 ' + r.sequence_gaps + ' · 乱序 ' + r.out_of_order + ' · 重复 ' + r.duplicates + ' · 完整 PS ' + r.ps_units + ' · 不完整 PS ' + r.partial_units
      : ' · 信令时间线');
    renderTimeline(el('review-timeline'), data.session.timeline || []);
    el('review-signaling').hidden = !data.signaling?.length;
    el('review-packets').textContent = (data.signaling || [])
      .map(event => '[' + event.direction + '] ' + (event.packet || event.summary)).join('\n\n');
  } catch (error) {
    output.textContent = error.message;
  } finally {
    input.value = '';
    input.disabled = false;
  }
});

function renderBroadcast(broadcast) {
  el('broadcast-status').hidden = !broadcast;
  el('voice-output').textContent = broadcast?.output_id || '';
  const labels = {idle: '等待广播', negotiating: '协商中', receiving: '接收中', ended: '已结束', failed: '失败'};
  el('voice-state').textContent = broadcast ? [
    labels[broadcast.state], broadcast.codec, 'RTP ' + broadcast.packets.toLocaleString('zh-CN') + ' 包',
    broadcast.packets ? 'PCM 峰值 ' + broadcast.peak_pcm.toLocaleString('zh-CN') : '',
    broadcast.source_id ? '音源 ' + broadcast.source_id : '',
  ].filter(Boolean).join(' · ') : '';
  el('voice-error').textContent = broadcast?.error || '';
  el('voice-error').hidden = !broadcast?.error;
}

function renderSource(status) {
  const source = status.media_source || {};
  const output = source.video_mode === 'copy' ? (source.output_codec === 'hevc' ? 'H.265 直拷' : 'H.264 直拷')
    : source.video_mode === 'transcode' ? `${source.video_codec?.toUpperCase() || '源视频'} 转 H.264` : 'H.264';
  const filename = source.filename || '内置测试画面';
  el('media-source').hidden = !source.video_mode;
  el('media-source').textContent = `${filename} · ${output} · ${audioLabels[source.audio_codec] || ''}`;
  const record = status.recording;
  const windowLabel = el('recording-window');
  windowLabel.hidden = !record;
  windowLabel.textContent = record ? '录像可查询 ' + new Date(record.start).toLocaleString('zh-CN') +
    ' 至 ' + new Date(record.end).toLocaleString('zh-CN') : '';
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

async function refresh() {
  const current = generation;
  const query = selectionQuery();
  try {
    const status = await request('', undefined, query).catch(error => {
      if (!query || error.status !== 404) throw error;
      return request();
    });
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

async function poll() {
  if (!busy) await refresh();
  window.setTimeout(poll, active ? 1000 : 2500);
}

function changeTarget() {
  updateButtons();
  generation++;
  void refresh();
}

devicePicker.addEventListener('change', () => {
  channelPicker.value = '';
  changeTarget();
});
channelPicker.addEventListener('change', changeTarget);

async function change(path, body) {
  generation++;
  busy = true;
  pollError = false;
  if (path === '/start-file') el('start').textContent = '正在准备视频…';
  updateButtons();
  notice();
  try {
    const status = await request(path, body, path === '/stop' ? selectionQuery() : '');
    if (path === '/start' || path === '/start-file') {
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

el('switch-channel-state').addEventListener('click', () => {
  void change('/channel-state', {
    device_id: selectedStatus.device_id,
    channel_id: selectedStatus.channel_id,
    online: !selectedStatus.channel_online,
  });
});

form.addEventListener('submit', (event) => {
  event.preventDefault();
  const config = Object.fromEntries(new FormData(form));
  for (const name of numberFields) config[name] = Number(config[name]);
  config.capture_rtp = el('capture-rtp').checked;
  config.voice = el('voice-enabled').checked;
  const faults = {
    first_delay_ms: Number(el('fault-delay').value), start_ms: Number(el('fault-start').value),
    window_ms: Number(el('fault-window').value), timestamp_jump_ms: Number(el('fault-jump').value),
    drop_every: Number(el('fault-drop').value), reorder_every: Number(el('fault-reorder').value),
    mute_audio: el('fault-mute').checked,
  };
  if (faults.first_delay_ms || faults.timestamp_jump_ms || faults.drop_every || faults.reorder_every || faults.mute_audio) {
    config.faults = faults;
  }
  const file = videoFile.files[0];
  if (!file) {
    void change('/start', config);
    return;
  }
  if (file.size > 512 * 1024 * 1024) {
    notice('本地视频超过 512 MiB。');
    return;
  }
  const recordingStart = el('recording-start').value;
  if (recordingStart) config.recording_start = new Date(recordingStart).toISOString();
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
  for (const id of ['export-trace', 'export-evidence']) el(id).addEventListener('click', (event) => {
    if (!desktop) return;
    event.preventDefault();
    if (exporting) return;
    exporting = true;
    feedback.hidden = false;
    feedback.classList.remove('error');
    feedback.textContent = id === 'export-evidence' ? '请选择证据包的保存位置。' : '请选择 SIP 信令的保存位置。';
    const data = id === 'export-evidence'
      ? { id: el('sessions').value, name: 'gb28181-evidence.zip' }
      : { id: devicePicker.value, name: 'gb28181-trace.json' };
    window.parent.postMessage({ kind: 'save-artifact', ...data }, '*');
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
