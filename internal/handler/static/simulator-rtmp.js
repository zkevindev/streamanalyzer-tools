'use strict';
const api = '/api/simulators/rtmp';
const el = id => document.getElementById(id);
const stages = {tls: 'TLS', handshake: 'RTMP 握手', app_connect: '应用连接', play: '播放协商', receive: '媒体接收'};
let source;
let busy = false;
let generation = 0;

function render() {
  const active = source?.active;
  el('config').disabled = el('start').disabled = busy || !source || active;
  el('stop').disabled = busy || !active;
  if (!source) return;
  el('state').textContent = active ? '运行中' : '未运行';
  el('state').dataset.status = active ? 'done' : '';
  el('url').textContent = active ? source.url : '—';
  el('stage').textContent = stages[source.last_stage] || '—';
  el('connections').textContent = source.connections;
  el('plays').textContent = source.plays;
  el('hits').textContent = source.fault_hits;
  el('open').hidden = !active;
}

async function request(path = '', value) {
  const options = {cache: 'no-store'};
  if (path) options.method = 'POST';
  if (value) {
    options.headers = {'Content-Type': 'application/json'};
    options.body = JSON.stringify(value);
  }
  const response = await fetch(api + path, options);
  const json = await response.json();
  if (!response.ok) throw new Error(json.error || 'RTMP 模拟器请求失败');
  return json;
}

function notice(text) {
  el('notice').textContent = text;
  el('notice').hidden = !text;
}

async function change(path, config) {
  ++generation;
  busy = true;
  notice('');
  render();
  try { source = await request(path, config); } catch (error) { notice(error.message); }
  busy = false;
  render();
}

async function poll() {
  const active = generation;
  if (!busy) {
    try {
      const next = await request();
      if (active === generation && !busy) { source = next; render(); }
    } catch (error) { if (active === generation && !busy) notice(error.message); }
  }
  setTimeout(poll, 1000);
}
el('form').addEventListener('submit', event => {
  event.preventDefault();
  void change('/start', {fault: el('fault').value, port: Number(el('port').value)});
});
el('stop').addEventListener('click', () => void change('/stop'));
void poll();
