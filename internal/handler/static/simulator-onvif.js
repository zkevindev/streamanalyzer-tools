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

function updateButtons() {
  el('simulator-fields').disabled = busy || active || !loaded;
  el('start').disabled = busy || active || !loaded || !network.value;
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
  el('rtsp-url').textContent = status.stream_url || '';
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
    render(await request(path, {
      method: 'POST',
      ...(body && { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
    }));
  } catch (error) {
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
  void change('/start', config);
});
el('stop').addEventListener('click', () => { void change('/stop'); });
void configure();
void poll();
