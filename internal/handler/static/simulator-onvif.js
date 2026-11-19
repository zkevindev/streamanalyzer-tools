'use strict';

const api = '/api/simulators/onvif';
const el = id => document.getElementById(id);
const form = el('onvif-form');
const network = el('bind-ip');
let active = false;
let loaded = false;
let busy = false;
let generation = 0;
let statusError = false;

async function request(path = '', options = {}) {
  const response = await fetch(api + path, { cache: 'no-store', ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || 'ONVIF 请求失败。');
  return body;
}

function notice(message = '') {
  el('notice').textContent = message;
  el('notice').hidden = !message;
}

let motionActive = false;

function updateButtons() {
  el('simulator-fields').disabled = busy || active || !loaded;
  el('start').disabled = busy || active || !loaded || !network.value;
  el('motion').disabled = busy || !active;
  el('stop').disabled = busy || !active;
}

function render(status) {
  active = status.active;
  const state = el('state');
  const label = active ? '运行中' : '未运行';
  if (state.textContent !== label) state.textContent = label;
  state.dataset.status = active ? 'done' : '';
  el('discovery-replies').textContent = status.discovery_replies;
  el('soap-requests').textContent = status.soap_requests;
  el('rtsp-plays').textContent = status.rtsp_plays;
  el('endpoints').hidden = !active;
  el('device-url').textContent = status.device_url || '';
  el('media-url').textContent = status.media_url || '';
  el('ptz-url').textContent = status.ptz_url || '';
  el('event-url').textContent = status.event_url || '';
  motionActive = Boolean(status.events?.motion);
  el('motion-control').hidden = !active;
  el('motion').textContent = motionActive ? '结束移动侦测' : '触发移动侦测';
  const events = status.events || {};
  el('event-metrics').textContent = [events.subscriptions + ' 个订阅', events.pull_requests + ' 次拉取',
    events.delivered + ' 条通知', events.dropped ? events.dropped + ' 条队列溢出' : ''].filter(Boolean).join(' · ');
  el('snapshot-hint').hidden = !active;
  el('rtsp-auth').textContent = status.rtsp_digest ? 'Digest' : '无鉴权';
  el('soap-auth').textContent = status.soap_digest ? [
    'UsernameToken Digest', '拒绝 ' + status.soap_auth_rejected + ' 次',
    '时钟偏差拒绝 ' + status.soap_clock_rejected + ' 次', '设备偏差 ' + status.clock_offset_seconds + ' 秒',
  ].join(' · ') : '无鉴权';
  el('transport-observation').textContent = 'UDP PLAY ' + status.udp_plays + ' 次 · TCP PLAY ' + status.tcp_plays + ' 次' +
    (status.silent_udp ? ' · UDP 无 RTP' : '');
  const gaps = status.video_gaps || [];
  el('gap-observation-row').hidden = !gaps.length;
  el('gap-observation').textContent = gaps.map((gap, index) => (index ? '子码流' : '主码流') + ' ' +
    (gap.end ? '已恢复（' + (Date.parse(gap.end) - Date.parse(gap.begin)) + ' ms）' : gap.begin ? '停包中' : '等待 TCP 点播')).join(' · ');
  for (const [index, endpoint, detail, snapshot] of [[0, 'rtsp-url', 'main-profile-detail', 'main-snapshot'], [1, 'rtsp-sub-url', 'sub-profile-detail', 'sub-snapshot']]) {
    const profile = status.profiles?.[index];
    el(endpoint).textContent = profile?.stream_url || '';
    if (profile?.snapshot_url) el(snapshot).href = profile.snapshot_url;
    else el(snapshot).removeAttribute('href');
    el(detail).textContent = profile ? '· ' + (profile.filename || '测试画面') + ' · ' +
      profile.width + ' × ' + profile.height + ' / ' + profile.fps + ' fps' : '';
  }
  updateButtons();
}

async function poll() {
  const current = generation;
  if (!busy) {
    try {
      const status = await request();
      if (current === generation && !busy) {
        if (statusError) notice();
        statusError = false;
        render(status);
      }
    } catch (error) {
      if (current === generation && !busy) {
        statusError = true;
        notice(error.message);
      }
    }
  }
  window.setTimeout(poll, 1000);
}

async function configure() {
  try {
    const addresses = await request('/interfaces');
    network.replaceChildren(...addresses.map(address => new Option(address.ip + ' · ' + address.interface, address.ip)));
    if (addresses.some(address => address.ip === '127.0.0.1')) network.value = '127.0.0.1';
    if (!addresses.length) notice('没有可用于 ONVIF 的 IPv4 网卡。');
    loaded = true;
    updateButtons();
  } catch (error) {
    notice(error.message);
  }
}

async function change(path, body) {
  generation++;
  busy = true;
  statusError = false;
  notice();
  updateButtons();
  try {
    const options = {method: 'POST'};
    if (body instanceof FormData) options.body = body;
    else if (body) {
      options.headers = {'Content-Type': 'application/json'};
      options.body = JSON.stringify(body);
    }
    if (path === '/start-file') el('state').textContent = '正在准备视频';
    render(await request(path, options));
    if (body) {
      el('rtsp-password').value = '';
      el('soap-password').value = '';
    }
  } catch (error) {
    if (path === '/start-file') el('state').textContent = '未运行';
    notice(error.message);
  } finally {
    busy = false;
    updateButtons();
  }
}

form.addEventListener('submit', event => {
  event.preventDefault();
  const config = Object.fromEntries(new FormData(form));
  config.http_port = Number(config.http_port);
  config.rtsp_port = Number(config.rtsp_port);
  config.clock_offset_seconds = Number(config.clock_offset_seconds);
  config.silent_udp = el('silent-udp').checked;
  config.video_gap_ms = el('video-gap').checked ? 5000 : 0;
  const files = [['main', el('main-video').files[0]], ['sub', el('sub-video').files[0]]].filter(([, file]) => file);
  if (!files.length) {
    void change('/start', config);
    return;
  }
  if (files.some(([, file]) => file.size > 64 * 1024 * 1024)) {
    notice('每路本地视频最多 64 MiB。');
    return;
  }
  const body = new FormData();
  body.append('config', JSON.stringify(config));
  for (const [name, file] of files) body.append(name, file);
  void change('/start-file', body);
});
el('stop').addEventListener('click', () => { void change('/stop'); });
el('motion').addEventListener('click', () => { void change('/motion', {active: !motionActive}); });
void configure();
void poll();
