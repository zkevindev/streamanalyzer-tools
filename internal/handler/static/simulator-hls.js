'use strict';

const api = '/api/simulators/hls';
const el = id => document.getElementById(id);
const modes = {normal: '正常播放', delay: '分片延迟', not_found: '分片返回 404'};
let current;
let busy = false;
let generation = 0;

function updateFields() {
  const mode = el('mode').value;
  el('simulator-fields').disabled = busy || !current || current.active;
  for (const [input, field, show] of [['segment', 'segment-field', mode !== 'normal'], ['delay-ms', 'delay-field', mode === 'delay']]) {
    el(field).hidden = !show;
    el(input).disabled = !show;
  }
  el('start').disabled = el('simulator-fields').disabled;
  el('stop').disabled = busy || !current?.active;
}

function render(status) {
  current = status;
  el('state').textContent = status.active ? '运行中' : '未运行';
  el('state').dataset.status = status.active ? 'done' : '';
  el('requests').textContent = status.segment_requests;
  el('faults').textContent = status.fault_hits;
  el('source-info').hidden = !status.id;
  el('actions').hidden = !status.active;
  el('scenario').textContent = [modes[status.mode], status.segment && '分片 ' + status.segment, status.delay_ms && status.delay_ms + ' ms'].filter(Boolean).join(' · ');
  el('last-fault').textContent = status.last_fault ? new Date(status.last_fault).toLocaleTimeString('zh-CN', {fractionalSecondDigits: 3}) : '—';
  el('playlist').textContent = status.active ? new URL(status.playlist, location.origin).href : '—';
  if (status.active) el('open-workbench').href = '/diagnostics?hls_simulator=' + encodeURIComponent(status.id);
  updateFields();
}

function notice(message = '') {
  el('notice').textContent = message;
  el('notice').hidden = !message;
}

async function request(path = '', options = {}) {
  const response = await fetch(api + path, {cache: 'no-store', ...options});
  const result = await response.json();
  if (!response.ok) throw Error(result.error || 'HLS 模拟器请求失败。');
  return result;
}

async function poll() {
  const version = generation;
  if (!busy) {
    try {
      const status = await request();
      if (version === generation && !busy) render(status);
    } catch (error) {
      if (version === generation && !busy) notice(error.message);
    }
  }
  window.setTimeout(poll, 1000);
}

async function change(path, config) {
  generation++;
  busy = true;
  notice();
  updateFields();
  try {
    render(await request(path, {
      method: 'POST',
      ...(config && {headers: {'Content-Type': 'application/json'}, body: JSON.stringify(config)}),
    }));
  } catch (error) {
    notice(error.message);
  } finally {
    busy = false;
    updateFields();
  }
}

el('mode').addEventListener('change', updateFields);
el('hls-form').addEventListener('submit', event => {
  event.preventDefault();
  void change('/start', {mode: el('mode').value, segment: Number(el('segment').value), delay_ms: Number(el('delay-ms').value)});
});
el('stop').addEventListener('click', () => { void change('/stop'); });
void poll();
