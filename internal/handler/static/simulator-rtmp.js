'use strict';
const api = '/api/simulators/rtmp';
const el = id => document.getElementById(id);
const stages = {tls: 'TLS', handshake: 'RTMP 握手', app_connect: '应用连接', play: '播放协商', receive: '媒体接收', publish: '发布成功', publisher_media: '收到发布媒体', publisher_keyframe: '关键帧可转发', publisher_disconnected: '推流端断开'};
let source;
let busy = false;
let generation = 0;

function updateFields() {
  el('keyframe-field').hidden = el('fault').value !== 'publish_relay';
  el('keyframe-delay').disabled = el('keyframe-field').hidden;
}

function render() {
  updateFields();
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
  const ingest = source.ingest;
  el('ingest').hidden = !ingest;
  if (!ingest) return;
  el('ingest-server').textContent = (source.url || '').replace(/\/test$/, '');
  el('ingest-status').textContent = (ingest.publishing ? '正在接收推流' : '等待推流') + ' · ' + ingest.viewers + ' 路拉流';
  el('publication-list').replaceChildren(...(ingest.sessions || []).slice().reverse().map(session => {
    const row = document.createElement('li');
    const start = new Date(session.published_at);
    const time = document.createElement('time');
    time.textContent = start.toLocaleTimeString('zh-CN');
    const facts = document.createElement('span');
    const elapsed = (at, label) => at ? label + ' ' + (Date.parse(at) - start.getTime()) + ' ms' : '';
    facts.textContent = ['第 ' + session.number + ' 次发布',
      elapsed(session.first_media_at, '首媒体'), elapsed(session.first_keyframe_at, '首个上游关键帧'),
      elapsed(session.relay_keyframe_at, '首个可转发关键帧'),
      session.withheld_keyframes && '延迟期间跳过关键帧 ' + session.withheld_keyframes + ' 个',
      elapsed(session.disconnected_at, '断开')].filter(Boolean).join(' · ');
    row.append(time, facts);
    return row;
  }));
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
el('fault').addEventListener('change', updateFields);
el('form').addEventListener('submit', event => {
  event.preventDefault();
  void change('/start', {fault: el('fault').value, port: Number(el('port').value),
    delay_keyframe_ms: el('fault').value === 'publish_relay' ? Number(el('keyframe-delay').value) : 0});
});
el('stop').addEventListener('click', () => void change('/stop'));
void poll();
