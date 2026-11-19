'use strict';

const api = '/api/simulators/media';
const el = id => document.getElementById(id);
const names = {
  normal: '正常', long_gop: '12 秒关键帧间隔', dts_backwards: '视频 DTS 回退',
  continuity: '视频 PID 连续计数缺口', pcr_jump: 'PCR 时钟跳变',
};
const protocols = {'http-flv': 'HTTP-FLV', 'http-ts': 'HTTP MPEG-TS'};
let status;
let busy = false;
let generation = 0;

function fillFaults() {
  const options = el('protocol').value === 'http-flv'
    ? ['normal', 'long_gop', 'dts_backwards'] : ['normal', 'continuity', 'pcr_jump'];
  el('fault').replaceChildren(...options.map(key => new Option(names[key], key)));
}

function notice(message) {
  el('notice').textContent = message;
  el('notice').hidden = !message;
}

function render() {
  const active = status?.active;
  el('settings').disabled = busy || !status || active;
  el('start').disabled = el('settings').disabled;
  el('stop').disabled = busy || !active;
  if (!status) return;
  el('state').textContent = active ? '运行中' : '未运行';
  el('state').dataset.status = active ? 'done' : '';
  el('requests').textContent = status.requests;
  el('faults').textContent = status.fault_hits;
  el('info').hidden = !status.id;
  el('scenario').textContent = status.id ? protocols[status.protocol] + ' · ' + names[status.fault] : '';
  el('duration').textContent = status.id ? (status.duration_ms / 1000).toFixed(1) + ' 秒' : '';
  el('last-fault').textContent = status.last_fault ? new Date(status.last_fault).toLocaleTimeString('zh-CN') : '—';
  el('url').textContent = active ? new URL(status.url, location.origin).href : '—';
  const open = el('open');
  open.hidden = !active;
  if (active) open.href = '/diagnostics?' + new URLSearchParams({media_simulator: status.id, media_protocol: status.protocol});
}

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
  const body = await response.json();
  if (!response.ok) throw new Error(body.error || '无法操作媒体模拟源。');
  return body;
}

async function change(path, config) {
  generation++;
  busy = true;
  notice('');
  render();
  try { status = await request(path, config); } catch (error) { notice(error.message); }
  busy = false;
  render();
}

async function poll() {
  const current = generation;
  if (!busy) {
    try {
      const state = await request();
      if (current === generation && !busy) {
        status = state;
        render();
      }
    } catch (error) {
      if (current === generation && !busy) notice(error.message);
    }
  }
  window.setTimeout(poll, 1000);
}

el('protocol').addEventListener('change', fillFaults);
el('form').addEventListener('submit', event => {
  event.preventDefault();
  void change('/start', {protocol: el('protocol').value, fault: el('fault').value});
});
el('stop').addEventListener('click', () => void change('/stop'));
fillFaults();
void poll();
