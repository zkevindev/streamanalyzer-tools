'use strict';

const api = '/api/simulators/gb28181/platform';
const el = id => document.getElementById(id);
const outcomes = {
  inviting: '等待 INVITE 应答',
  receiving: '正在收流',
  invite_failed: '点播协商失败',
  no_ack: '未发送 ACK',
  connecting: '正在建立 TCP 媒体连接',
  no_media_connection: '未建立 TCP 媒体连接',
  media_negotiation: 'TCP 媒体协商失败',
  no_rtp: '未收到 RTP',
  no_keyframe: '收到 RTP，未解析到关键帧',
  passed: '收到并解析出首个关键帧',
  interrupted: '已触发 SIP 中断',
  stopped: '观测已停止',
};
const stages = {
  invite: 'INVITE', invite_ok: 'INVITE 200 OK', ack: 'ACK',
  ack_withheld: '暂缓 ACK', cleanup_ack: '清理会话时补发 ACK',
  media_connect: '尝试建立媒体连接', media_connected: '媒体就绪', first_rtp: '首个 RTP',
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
  el('skip-ack').disabled = Boolean(choice.value);
  el('skip-connect').disabled = Boolean(choice.value);
  el('recovery-hint').hidden = choice.value !== 'udp_keepalive_timeout';
}

function render() {
  applyRecoveryOptions();
  const active = Boolean(status?.active);
  const preview = status?.preview;
  el('platform-config').disabled = pending || active || !status;
  el('start').disabled = el('platform-config').disabled;
  el('stop').disabled = pending || !active;
  el('preview-config').disabled = pending || !active || !status?.peer || status.busy;
  el('address').textContent = status?.address ? status.address + ' · SIP ' + status.sip_transport.toUpperCase() : '—';
  el('peer').textContent = status?.peer || '—';
  const badge = el('result');
  const recovery = status?.recovery;
  const recoveryLabels = {waiting_media: '等待首次关键帧', interrupted: '等待设备重新注册', registered: '核对重新注册后的目录',
    previewing: '正在重新点播', passed: '恢复点播成功', failed: '恢复失败', stopped: '观测已停止'};
  const label = recovery ? recoveryLabels[recovery.state] : preview ? outcomes[preview.state] : '等待点播';
  if (badge.textContent !== label) badge.textContent = label;
  badge.dataset.status = (recovery?.state || preview?.state) === 'passed' ? 'done' : (recovery?.error || preview?.error ? 'failed' : '');
  const recoveryDetails = recovery ? [recovery.before.call_id && '原会话 ' + recovery.before.call_id,
    recovery.ignored_keepalives > 0 && '忽略 ' + recovery.ignored_keepalives + ' 次心跳',
    recovery.previous_session?.timeline?.some(mark => mark.stage === 'ended') && '旧会话已释放',
    recovery.catalog_at && '已收到恢复后的在线目录',
    recovery.recovery_ms != null && '恢复到关键帧 ' + recovery.recovery_ms + ' ms', recovery.error].filter(Boolean) : [];
  el('recovery-result').hidden = !recoveryDetails.length;
  el('recovery-result').textContent = recoveryDetails.join(' · ');
  el('stats').hidden = !preview;
  el('rtp').textContent = preview?.media.packets || 0;
  el('ps').textContent = preview?.media.ps_units || 0;
  el('keys').textContent = preview?.media.keyframes || 0;
  el('detail').textContent = preview?.error || '';
  el('detail').hidden = !preview?.error;
  el('dialog-id').hidden = !preview;
  el('dialog-id').textContent = preview ? 'Call-ID ' + preview.call_id + ' · SSRC ' + preview.ssrc + ' · RTP ' + preview.rtp_address : '';
  el('export').hidden = !preview;
  const marks = (preview?.timeline || []).map(mark => ({...mark, side: '平台'}));
  const device = status?.device_session;
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
    status = await request(path, config);
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
  void change('/start', config);
});
el('preview-form').addEventListener('submit', event => {
  event.preventDefault();
  const config = Object.fromEntries(new FormData(event.currentTarget));
  config.window_ms = Number(config.window_ms);
  config.skip_ack = !config.recovery && el('skip-ack').checked;
  config.skip_connect = !config.recovery && config.media_transport === 'tcp-active' && el('skip-connect').checked;
  void change('/preview', config);
});
el('media-transport').addEventListener('change', () => {
  el('skip-connect-control').hidden = el('media-transport').value !== 'tcp-active';
});
el('recovery-mode').addEventListener('change', applyRecoveryOptions);
el('stop').addEventListener('click', () => void change('/stop'));
void poll();
