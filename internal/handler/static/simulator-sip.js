'use strict';

const api = '/api/simulators/sip';
const el = id => document.getElementById(id);
const target = el('sip-target');
const audio = el('audio-player');
const registration = {
  idle: '未注册', registering: '正在注册', registered: '已注册', retrying: '正在重新注册',
  stopping: '正在停止', stopped: '已注销', failed: '注册失败',
};
const callState = {
  dialing: '正在呼出', ringing: '振铃中', answering: '建立通话中',
  incall: '通话中', ending: '正在结束', ended: '已结束', failed: '通话失败',
};
let status;
let busy = false;
let generation = 0;
let pollError = false;
let clipCall = '';
let renderedEvents = '';

async function request(path = '', options = {}) {
  const response = await fetch(api + path, { cache: 'no-store', ...options });
  const result = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(result.error || 'SIP 请求失败。');
  return result;
}

function notice(message = '') {
  el('notice').hidden = !message;
  el('notice').textContent = message;
}

function updateButtons() {
  const calling = status?.call?.active;
  const canDial = !busy && status?.state === 'registered' && !calling;
  const incoming = calling && status.call.direction === 'inbound' && status.call.state === 'ringing';
  el('simulator-fields').disabled = busy || !status || status.active;
  el('register').disabled = el('simulator-fields').disabled;
  el('unregister').disabled = busy || !status?.active || status.state === 'stopping';
  target.disabled = !canDial;
  el('dial').disabled = !canDial;
  el('answer').disabled = busy || status?.state === 'stopping' || !incoming;
  el('hangup').disabled = busy || status?.state === 'stopping' || !calling || status.call.state === 'ending';
  el('hangup').textContent = incoming ? '拒接'
    : calling && status.call.direction === 'outbound' && ['dialing', 'ringing'].includes(status.call.state) ? '取消呼叫' : '挂断';
}

function setError(id, message) {
  el(id).hidden = !message;
  el(id).textContent = message || '';
}

function render(next) {
  status = next;
  const state = el('registration-state');
  state.textContent = registration[status.state] || status.state;
  state.dataset.status = { registered: 'done', failed: 'failed', retrying: 'partial', registering: 'running' }[status.state] || '';
  el('sip-addresses').hidden = !status.local_address;
  el('signal-local').textContent = status.local_address || '';
  el('signal-peer').textContent = status.registrar || '';
  setError('registration-error', status.error);

  const call = status.call;
  el('call-state').textContent = call ? callState[call.state] || call.state : '空闲';
  el('call-state').dataset.status = call?.state === 'incall' ? 'done' : call?.active ? 'running' : '';
  el('call-metrics').hidden = el('media-addresses').hidden = !call;
  el('call-direction').textContent = call?.direction === 'inbound' ? '呼入' : '呼出';
  el('call-peer').textContent = call?.peer || '—';
  el('audio-codec').textContent = call?.codec ? call.codec + ' · 8 kHz' : '—';
  el('media-local').textContent = call?.media_local || '—';
  el('media-remote').textContent = call?.media_remote || '—';
  el('packets-sent').textContent = (call?.packets_sent || 0).toLocaleString('zh-CN');
  el('packets-received').textContent = (call?.packets_received || 0).toLocaleString('zh-CN');
  setError('call-error', call?.error);
  el('received-audio').hidden = !call?.packets_received;
  if (clipCall !== (call?.call_id || '')) {
    audio.removeAttribute('src');
    audio.load();
    audio.hidden = true;
    clipCall = call?.call_id || '';
  }
  renderEvents(status.events || []);
  updateButtons();
}

function renderEvents(entries) {
  const signature = JSON.stringify(entries);
  if (signature === renderedEvents) return;
  renderedEvents = signature;
  el('no-events').hidden = entries.length > 0;
  el('sip-events').replaceChildren(...[...entries].reverse().map(entry => {
    const li = document.createElement('li');
    const row = document.createElement('div');
    row.className = 'event-row';
    const time = document.createElement('time');
    time.className = 'event-time';
    time.dateTime = entry.time;
    time.textContent = new Date(entry.time).toLocaleTimeString('zh-CN', { hour12: false });
    const direction = document.createElement('span');
    direction.className = 'status event-direction';
    direction.dataset.direction = entry.direction;
    direction.textContent = { in: '收到', out: '发送', system: '事件' }[entry.direction] || '';
    const summary = document.createElement('span');
    summary.className = 'event-summary';
    summary.textContent = entry.message;
    row.append(time, direction, summary);
    li.append(row);
    return li;
  }));
}

async function poll() {
  const current = generation;
  if (!busy) {
    try {
      const next = await request();
      if (current === generation && !busy) {
        if (pollError) notice();
        pollError = false;
        render(next);
      }
    } catch (error) {
      if (current === generation && !busy) {
        pollError = true;
        notice(error.message);
      }
    }
  }
  window.setTimeout(poll, status?.active ? 750 : 1500);
}

async function change(path, data) {
  generation++;
  busy = true;
  pollError = false;
  notice();
  updateButtons();
  try {
    const next = await request(path, {
      method: 'POST',
      ...(data && { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) }),
    });
    if (path === '/start') el('sip-password').value = '';
    render(next);
  } catch (error) {
    notice(error.message);
  } finally {
    busy = false;
    updateButtons();
  }
}

el('registration-form').addEventListener('submit', event => {
  event.preventDefault();
  const config = Object.fromEntries(new FormData(event.currentTarget));
  config.port = Number(config.port);
  config.local_port = Number(config.local_port);
  void change('/start', config);
});
el('call-form').addEventListener('submit', event => {
  event.preventDefault();
  void change('/dial', { target: target.value });
});
el('unregister').addEventListener('click', () => { void change('/stop'); });
el('answer').addEventListener('click', () => { void change('/answer'); });
el('hangup').addEventListener('click', () => { void change('/hangup'); });
el('hear-audio').addEventListener('click', () => {
  audio.hidden = false;
  audio.src = api + '/received.wav?t=' + Date.now();
  audio.play().catch(() => { notice('无法播放接收音频。'); });
});
void poll();
