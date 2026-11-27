'use strict';
const api = '/api/simulators/rtsp';
const el = id => document.getElementById(id);
const stages = {
  tls: 'TLS 握手',
  auth: 'Digest / Basic 鉴权',
  describe: 'DESCRIBE',
  redirect: '302 重定向',
  setup: 'SETUP',
  play: 'PLAY',
  announce: 'ANNOUNCE 推流协商',
  record: 'RECORD 接收推流',
  receive: 'RTP 媒体发送',
};
let source;
let busy = false;
let generation = 0;

function updateFields() {
  const fault = el('fault').value;
  const needAuth = fault === 'auth_required';
  const needDrop = fault === 'rtp_drop';
  el('auth-fields').hidden = !needAuth;
  el('username').disabled = !needAuth;
  el('password').disabled = !needAuth;
  el('drop-field').hidden = !needDrop;
  el('drop-every').disabled = !needDrop;
}

function render() {
  updateFields();
  const active = Boolean(source?.active);
  el('config').disabled = el('start').disabled = busy || !source || active;
  el('stop').disabled = busy || !active;
  if (!source) return;
  el('state').textContent = active ? '运行中' : '未运行';
  el('state').dataset.status = active ? 'done' : '';
  el('url').textContent = active ? source.url : '—';
  el('stage').textContent = stages[source.last_stage] || '—';
  el('connections').textContent = source.connections || 0;
  el('describes').textContent = source.describes || 0;
  el('plays').textContent = `${source.tcp_plays || 0} / ${source.udp_plays || 0}`;
  el('hits').textContent = `${source.fault_hits || 0} / ${source.dropped_packets || 0}`;

  const hasRedirect = active && Boolean(source.redirect_target);
  el('redirect-row').hidden = !hasRedirect;
  el('redirect-target').textContent = hasRedirect ? source.redirect_target : '—';

  const isRelay = active && source.fault === 'publish_relay';
  el('publish-row').hidden = !isRelay;
  el('relay-hint').hidden = !isRelay;
  if (isRelay) {
    el('publish-status').textContent = `${source.publishing ? '正在接收推流' : '等待推流'} · ${source.viewers || 0} 路拉流`;
  }
  el('open').hidden = !active;
}

async function request(path = '', value) {
  const options = { cache: 'no-store' };
  if (path) options.method = 'POST';
  if (value) {
    options.headers = { 'Content-Type': 'application/json' };
    options.body = JSON.stringify(value);
  }
  const response = await fetch(api + path, options);
  const json = await response.json();
  if (!response.ok) throw new Error(json.error || 'RTSP 模拟器请求失败');
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
  try {
    source = await request(path, config);
  } catch (error) {
    notice(error.message);
  }
  busy = false;
  render();
}

async function poll() {
  const active = generation;
  if (!busy) {
    try {
      const next = await request();
      if (active === generation && !busy) {
        source = next;
        render();
      }
    } catch (error) {
      if (active === generation && !busy) notice(error.message);
    }
  }
  setTimeout(poll, 1000);
}

el('fault').addEventListener('change', updateFields);
el('form').addEventListener('submit', event => {
  event.preventDefault();
  const fault = el('fault').value;
  const payload = {
    fault,
    port: Number(el('port').value || 0),
  };
  if (fault === 'auth_required') {
    payload.username = el('username').value.trim();
    payload.password = el('password').value;
  }
  if (fault === 'rtp_drop') {
    payload.drop_every = Number(el('drop-every').value || 10);
  }
  void change('/start', payload);
});
el('stop').addEventListener('click', () => void change('/stop'));
void poll();
