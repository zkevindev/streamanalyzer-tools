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
  const scenarioActive = ['waiting', 'injecting', 'recovering'].includes(status?.call?.scenario?.state);
  el('audio-fault').disabled = busy || status?.state === 'stopping' || status?.call?.state !== 'incall' || scenarioActive;
  el('preset-times').hidden = !el('preset').value;
  el('send-dtmf').disabled = busy || status?.state === 'stopping' || status?.call?.state !== 'incall' || !status.call.dtmf_keys;
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
  if (status.active) el('preset').value = status.preset || '';
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
  const direction = call?.direction === 'inbound' ? '呼入' : '呼出';
  el('audio-summary').textContent = call?.codec ? direction + ' · ' + call.codec + ' · 8 kHz' : direction;
  el('early-summary').hidden = !call?.early_media;
  el('early-summary').textContent = call?.early_media ?
    '183 早期媒体 · 发送 ' + (call.early_send_rtp?.packets || 0) + ' 包 · 接收 ' + (call.early_receive_rtp?.packets || 0) + ' 包' : '';
  const keys = call?.dtmf_keys || '';
  const digits = call?.digit_events || [];
  el('dtmf-panel').hidden = !keys && !digits.length;
  const select = el('dtmf-digit');
  if ([...select.options].map(option => option.value).join('') !== keys) {
    select.replaceChildren(...[...keys].map(digit => new Option(digit, digit)));
  }
  el('dtmf-events').textContent = digits.map(event => (event.direction === 'send' ? '发送 ' : '收到 ') + event.digit).join(' · ');
  const scenario = call?.scenario;
  const result = el('scenario-result');
  result.hidden = !scenario;
  if (scenario) {
    const states = {waiting: '正在等待预设触发', injecting: '预设正在暂停发送', recovering: '正在验证发送恢复', passed: '场景通过', failed: '场景失败', canceled: '场景结束'};
    result.textContent = states[scenario.state] + (scenario.reason ? ' · ' + scenario.reason : '') +
      (scenario.finished_at ? ' · 注入丢弃 ' + scenario.dropped + ' 包 · 同期接收 ' + scenario.received + ' 包 · 最长无包 ' + scenario.longest_silence_ms + ' ms' : '');
  }
  el('audio-fault').value = call?.audio_fault || 'off';
  renderRTP('send-rtp', call?.send_rtp);
  renderRTP('receive-rtp', call?.receive_rtp);
  el('call-peer').textContent = call?.peer || '—';
  el('media-local').textContent = call?.media_local || '—';
  el('media-remote').textContent = call?.media_remote || '—';
  setError('call-error', call?.error);
  el('received-audio').hidden = !call?.audio_ready;
  if (clipCall !== (call?.call_id || '')) {
    audio.removeAttribute('src');
    audio.load();
    audio.hidden = true;
    clipCall = call?.call_id || '';
  }
  renderEvents(status.events || []);
  updateButtons();
}

function renderRTP(id, stats) {
  const container = el(id);
  const time = value => value ? new Date(value).toLocaleTimeString('zh-CN', {fractionalSecondDigits: 3}) : '—';
  const values = [
    ['通过 / 注入丢弃', (stats?.packets || 0) + ' / ' + (stats?.fault_drops || 0)],
    ['首包 / 末包', time(stats?.first_at) + ' / ' + time(stats?.last_at)],
    ['序列缺口 / 抖动', (stats?.sequence_gaps || 0) + ' / ' + (stats?.jitter_ms || 0).toFixed(2) + ' ms'],
  ];
  if (!container.children.length) {
    container.replaceChildren(...values.map(([label]) => {
      const row = document.createElement('div');
      const term = document.createElement('dt');
      term.textContent = label;
      row.append(term, document.createElement('dd'));
      return row;
    }));
  }
  values.forEach(([, value], index) => { container.children[index].lastElementChild.textContent = value; });
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
    el('audio-fault').value = status?.call?.audio_fault || 'off';
  } finally {
    busy = false;
    updateButtons();
  }
}

el('preset').addEventListener('change', updateButtons);
el('registration-form').addEventListener('submit', event => {
  event.preventDefault();
  const config = Object.fromEntries(new FormData(event.currentTarget));
  config.port = Number(config.port);
  config.local_port = Number(config.local_port);
  config.early_media = el('offer-early').checked;
  config.preset_delay_ms = Number(config.preset_delay_ms);
  config.preset_duration_ms = Number(config.preset_duration_ms);
  void change('/start', config);
});
el('call-form').addEventListener('submit', event => {
  event.preventDefault();
  void change('/dial', { target: target.value });
});
el('unregister').addEventListener('click', () => { void change('/stop'); });
el('answer').addEventListener('click', () => { void change('/answer'); });
el('hangup').addEventListener('click', () => { void change('/hangup'); });
el('send-dtmf').addEventListener('click', () => { void change('/dtmf', {digit: el('dtmf-digit').value}); });
el('audio-fault').addEventListener('change', event => { void change('/audio-fault', {direction: event.target.value}); });
el('hear-audio').addEventListener('click', () => {
  audio.hidden = false;
  audio.src = api + '/received.wav?t=' + Date.now();
  audio.play().catch(() => { notice('无法播放接收音频。'); });
});
void poll();
