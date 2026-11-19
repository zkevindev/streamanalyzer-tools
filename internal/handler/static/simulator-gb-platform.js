'use strict';

const api = '/api/simulators/gb28181/platform';
const el = id => document.getElementById(id);
const outcomes = {
  record_info: '正在查询录像目录',
  record_info_failed: '录像目录查询失败',
  no_recordings: '指定时段无录像',
  recording_ended: '设备通知录像播放结束',
  inviting: '等待 INVITE 应答',
  receiving: '正在收流',
  invite_failed: '点播协商失败',
  no_ack: '未发送 ACK',
  connecting: '正在建立 TCP 媒体连接',
  no_media_connection: '未建立 TCP 媒体连接',
  media_negotiation: 'TCP 媒体协商失败',
  no_rtp: '未收到 RTP', media_timeout: '活动会话 RTP 超时',
  no_keyframe: '收到 RTP，未解析到关键帧',
  passed: '收到并解析出首个关键帧',
  interrupted: '已触发 SIP 中断',
  stopped: '观测已停止',
};
const stages = {
  record_info: '发送 RecordInfo', record_info_result: '录像目录条数', recording_info: '回放控制应答',
  recording_info_failed: '回放控制未获应答', recording_ended: '收到设备播放结束通知',
  invite: 'INVITE', invite_ok: 'INVITE 200 OK', ack: 'ACK',
  ack_withheld: '暂缓 ACK', cleanup_ack: '清理会话时补发 ACK',
  media_connect: '尝试建立媒体连接', media_connected: '媒体就绪', first_rtp: '首个 RTP',
  media_timeout: '正在点播的会话 RTP 超时', fault_media_silent: '停止发送 RTP',
  fault_injected: '触发 SIP 故障', first_keyframe: '首个关键帧', bye: 'BYE', ended: '会话结束', failed: '失败',
};
let pending = false;
let generation = 0;
let status = null;

async function request(path = '', config) {
  const options = {cache: 'no-store'};
  if (path) {
    options.method = 'POST';
    if (config) {
      options.headers = {'Content-Type': 'application/json'};
      options.body = JSON.stringify(config);
    }
  }
  const response = await fetch(api + path, options);
  const json = await response.json();
  if (!response.ok) throw new Error(json.error || '平台请求失败');
  return json;
}

function notice(message) {
  el('notice').textContent = message;
  el('notice').hidden = !message;
}

function applyRecoveryOptions() {
  const choice = el('recovery-mode');
  for (const option of choice.options) {
    option.disabled = Boolean(option.dataset.transport && option.dataset.transport !== status?.sip_transport);
  }
  if (choice.selectedOptions[0]?.disabled) choice.value = '';
  const recording = el('recording-mode').checked;
  choice.disabled = recording;
  el('skip-ack').disabled = Boolean(choice.value) || recording;
  el('skip-connect').disabled = Boolean(choice.value) || recording;
  el('recording-range').hidden = !recording;
  el('recording-start').required = el('recording-end').required = recording;
  el('preview-submit').textContent = recording ? '检索并回放' : '点播并收流';
  const media = choice.value === 'media_rtp_timeout';
  el('window').min = media ? '8000' : '500';
  if (media && Number(el('window').value) < 8000) el('window').value = '8500';
  el('recovery-hint').hidden = recording || (!media && choice.value !== 'udp_keepalive_timeout');
  el('recovery-hint').textContent = media ? '收到首个关键帧后，匹配的 RTP 停止 3.5 秒即触发重新点播。' :
    'UDP 场景使用 5 秒设备心跳，最多等待 30 秒重新注册。';
}

function selectedDevice() {
  return status?.devices?.find(device => device.device_id === el('preview-device').value) || status;
}

function render() {
  applyRecoveryOptions();
  const active = Boolean(status?.active);
  const deviceSelect = el('preview-device');
  const selected = deviceSelect.value;
  const devices = status?.devices || [];
  const options = devices.map(device => new Option(device.device_id +
    (device.peer ? ' · 在线' : ' · 离线') + (device.busy ? ' · 观测中' : ''), device.device_id));
  if (options.length !== deviceSelect.options.length ||
      options.some((option, index) => option.text !== deviceSelect.options[index].text)) {
    deviceSelect.replaceChildren(...options);
    if (devices.some(device => device.device_id === selected)) deviceSelect.value = selected;
  }
  deviceSelect.disabled = !devices.length;
  const deviceStatus = selectedDevice();
  const preview = deviceStatus?.preview;
  el('platform-config').disabled = pending || active || !status;
  el('start').disabled = el('platform-config').disabled;
  el('stop').disabled = pending || !active;
  el('preview-config').disabled = pending || !active || !deviceStatus?.peer || deviceStatus.busy;
  el('address').textContent = status?.address ? status.address + ' · SIP ' + status.sip_transport.toUpperCase() : '—';
  el('peer').textContent = devices.length ? devices.filter(device => device.peer).length + ' / ' + devices.length + ' · ' + (deviceStatus?.peer || '所选设备离线') : '—';
  const badge = el('result');
  const recovery = deviceStatus?.recovery;
  const recoveryLabels = {waiting_media: '等待首次关键帧', interrupted: '等待设备重新注册', registered: '核对重新注册后的目录',
    previewing: '正在重新点播', passed: '恢复点播成功', failed: '恢复失败', stopped: '观测已停止'};
  const label = recovery ? (recovery.mode === 'media_rtp_timeout' && recovery.state === 'interrupted' ? '核对当前在线目录' : recoveryLabels[recovery.state]) : preview ? outcomes[preview.state] : '等待点播';
  if (badge.textContent !== label) badge.textContent = label;
  badge.dataset.status = (recovery?.state || preview?.state) === 'passed' ? 'done' : (recovery?.error || preview?.error ? 'failed' : '');
  const recoveryDetails = recovery ? [recovery.before.call_id && '原会话 ' + recovery.before.call_id,
    recovery.ignored_keepalives > 0 && '忽略 ' + recovery.ignored_keepalives + ' 次心跳',
    recovery.previous_session?.timeline?.some(mark => mark.stage === 'ended') && '旧会话已释放',
    recovery.keepalive_at && Date.parse(recovery.keepalive_at) > Date.parse(recovery.last_rtp_at) && '停流后收到并应答心跳',
    recovery.catalog_at && '已收到恢复后的在线目录',
    recovery.recovery_ms != null && '恢复到关键帧 ' + recovery.recovery_ms + ' ms', recovery.error].filter(Boolean) : [];
  el('recovery-result').hidden = !recoveryDetails.length;
  el('recovery-result').textContent = recoveryDetails.join(' · ');
  const recording = preview?.recording;
  el('recording-controls').hidden = !recording || preview.state !== 'receiving';
  el('recording-controls').disabled = pending;
  el('recording-summary').hidden = recording?.total == null;
  el('recording-summary').textContent = recording?.total == null ? '' :
    '录像目录 ' + (recording.records?.length || 0) + ' / ' + recording.total + ' 段' +
    (recording.records?.length > 24 ? ' · 显示前 24 段，完整列表见导出证据' : '');
  const recordingList = el('recording-list');
  recordingList.hidden = !recording?.records?.length;
  recordingList.replaceChildren(...(recording?.records || []).slice(0, 24).map(span => {
    const item = document.createElement('li');
    item.textContent = new Date(span.start).toLocaleString() + ' → ' + new Date(span.end).toLocaleString();
    return item;
  }));
  el('stats').hidden = !preview;
  el('rtp').textContent = preview?.media.packets || 0;
  el('ps').textContent = preview?.media.ps_units || 0;
  el('keys').textContent = preview?.media.keyframes || 0;
  el('detail').textContent = preview?.error || '';
  el('detail').hidden = !preview?.error;
  el('dialog-id').hidden = !preview;
  el('dialog-id').textContent = preview ? 'Call-ID ' + preview.call_id + ' · SSRC ' + preview.ssrc + ' · RTP ' + preview.rtp_address : '';
  el('export').hidden = !devices.some(device => device.preview);
  const marks = (preview?.timeline || []).map(mark => ({...mark, side: '平台'}));
  const device = deviceStatus?.device_session;
  if (preview && device) {
    const shift = Date.parse(device.start) - Date.parse(preview.start);
    marks.push(...device.timeline.map(mark => ({...mark, offset_ms: mark.offset_ms + shift, side: '设备'})));
  }
  el('timeline').hidden = !marks.length;
  el('timeline').replaceChildren(...marks.sort((a, b) => a.offset_ms - b.offset_ms).map(mark => {
    const row = document.createElement('li');
    const timestamp = document.createElement('time');
    timestamp.textContent = '+' + Math.max(0, mark.offset_ms).toFixed(1) + ' ms';
    const label = document.createElement('span');
    label.textContent = mark.side + ' · ' + (stages[mark.stage] || mark.stage) + (mark.detail ? ' · ' + mark.detail : '');
    row.append(timestamp, label);
    return row;
  }));
}

async function change(path, config) {
  generation++;
  pending = true;
  notice('');
  render();
  try {
    status = await request(path, path === '/playback-control' ? {...config, device_id: selectedDevice()?.device_id} : config);
  } catch (error) {
    notice(error.message);
  } finally {
    pending = false;
    render();
  }
}

async function poll() {
  const current = generation;
  try {
    if (!pending) {
      const next = await request();
      if (!pending && current === generation) {
        status = next;
        render();
      }
    }
  } catch (error) {
    if (current === generation && !pending) notice(error.message);
  }
  window.setTimeout(poll, 700);
}

el('start-form').addEventListener('submit', event => {
  event.preventDefault();
  const config = Object.fromEntries(new FormData(event.currentTarget));
  config.port = Number(config.port);
  config.device_ids = el('extra-devices').value.split(/[\s,，]+/).filter(Boolean);
  void change('/start', config);
});
el('preview-form').addEventListener('submit', event => {
  event.preventDefault();
  const config = Object.fromEntries(new FormData(event.currentTarget));
  config.window_ms = Number(config.window_ms);
  config.device_id = selectedDevice()?.device_id;
  if (el('recording-mode').checked) {
    const start = new Date(el('recording-start').value);
    const end = new Date(el('recording-end').value);
    if (!(start < end)) { notice('录像开始时间必须早于结束时间。'); return; }
    config.recording = {start: start.toISOString(), end: end.toISOString()};
    delete config.recovery;
  }
  config.skip_ack = !config.recording && !config.recovery && el('skip-ack').checked;
  config.skip_connect = !config.recording && !config.recovery && config.media_transport === 'tcp-active' && el('skip-connect').checked;
  void change('/preview', config);
});
el('media-transport').addEventListener('change', () => {
  el('skip-connect-control').hidden = el('media-transport').value !== 'tcp-active';
});
el('recovery-mode').addEventListener('change', applyRecoveryOptions);
el('recording-mode').addEventListener('change', applyRecoveryOptions);
el('recording-pause').addEventListener('click', () => void change('/playback-control', {method: 'PAUSE'}));
el('recording-resume').addEventListener('click', () => void change('/playback-control', {method: 'PLAY'}));
el('recording-seek').addEventListener('click', () => {
  const offset = el('recording-offset');
  if (offset.value !== '' && offset.reportValidity()) {
    void change('/playback-control', {method: 'PLAY', offset: Number(offset.value)});
  }
});
el('preview-device').addEventListener('change', render);
el('stop').addEventListener('click', () => void change('/stop'));
void poll();
