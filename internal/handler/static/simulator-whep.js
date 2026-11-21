'use strict';

const api = '/api/simulators/whep';
const el = id => document.getElementById(id);
const video = el('video');
const modeLabels = {normal:'正常播放', ice_timeout:'ICE 探测超时', no_media:'连接后无 RTP', media_stop:'出画后 RTP 停止'};
let source;
let report;
let running;
let busy = false;
let generation = 0;

async function request(path, options = {}) {
  const result = await fetch(path, {cache:'no-store', ...options});
  if (!result.ok) {
    const body = await result.json().catch(() => ({}));
    throw new Error(body.error || 'WHEP 请求失败（HTTP ' + result.status + '）');
  }
  return result.json();
}

function notice(message = '') {
  el('notice').hidden = !message;
  el('notice').textContent = message;
}

function renderSource() {
  const active = source?.active;
  el('mode').disabled = el('start').disabled = !source || active || busy || Boolean(running);
  el('stop').disabled = !active || busy || Boolean(running);
  el('probe').disabled = !active || busy || Boolean(running);
  el('cancel').disabled = !running;
  el('export').disabled = !report || Boolean(running);
  el('source-state').textContent = active ? modeLabels[source.mode] : source ? '未运行' : '正在读取';
  el('source-state').dataset.status = active ? 'done' : '';
  el('endpoint-info').hidden = !active;
  el('endpoint').textContent = active ? new URL(source.endpoint, location.origin).href : '';
  el('sessions').textContent = source?.sessions ?? 0;
  const diagLink = el('diagnose-link');
  if (diagLink && active && source.endpoint) {
    const m = source.endpoint.match(/\/stream\/([a-f0-9-]{36})$/);
    diagLink.href = m ? '/diagnostics?whep_simulator=' + m[1] : '/diagnostics';
  }
}

function offset(value) {
  return value == null ? '—' : value + ' ms';
}

function renderReport() {
  if (!report) return;
  el('metrics').hidden = false;
  el('probe-state').textContent = report.result || '正在观测';
  el('probe-state').dataset.status = report.result ? report.passed ? 'done' : 'partial' : 'running';
  el('http').textContent = [report.http_status && 'HTTP ' + report.http_status, report.http_ms != null && offset(report.http_ms),
    report.answer_applied_ms != null && 'SDP 已应用 · ' + report.codec].filter(Boolean).join(' · ') || '—';
  el('ice').textContent = [report.ice_state, report.ice_connected_ms != null && offset(report.ice_connected_ms)].filter(Boolean).join(' · ') || '—';
  el('dtls').textContent = [report.dtls_state, report.dtls_connected_ms != null && offset(report.dtls_connected_ms)].filter(Boolean).join(' · ') || '—';
  el('rtp').textContent = offset(report.first_rtp_ms) + ' / ' + offset(report.last_rtp_observed_ms) + ' · ' + report.rtp_packets + ' 包';
  el('frame').textContent = report.first_frame_ms == null ? '—' : offset(report.first_frame_ms) + ' · ' + report.width + ' × ' + report.height;
}

async function changeSource(path, body) {
  notice();
  ++generation;
  busy = true;
  renderSource();
  try {
    source = await request(api + path, {method:'POST', ...(body && {headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)})});
  } catch (error) {
    notice(error.message);
  } finally {
    busy = false;
    renderSource();
  }
}

function waitGathering(pc, signal) {
  if (pc.iceGatheringState === 'complete') return Promise.resolve();
  return new Promise((resolve, reject) => {
    const finish = error => {
      clearTimeout(timer);
      pc.removeEventListener('icegatheringstatechange', onState);
      signal.removeEventListener('abort', onAbort);
      if (error) reject(error); else resolve();
    };
    const onState = () => { if (pc.iceGatheringState === 'complete') finish(); };
    const onAbort = () => finish(signal.reason);
    const timer = setTimeout(() => finish(new Error('本机 ICE 候选收集超时')), 5000);
    pc.addEventListener('icegatheringstatechange', onState);
    signal.addEventListener('abort', onAbort, {once:true});
    if (signal.aborted) onAbort();
    else onState();
  });
}

async function play(source, signal, record) {
  if (!globalThis.RTCPeerConnection || !video.requestVideoFrameCallback) {
    throw new Error('当前浏览器需要支持 WebRTC 和逐帧呈现回调。');
  }
  const started = performance.now();
  const elapsed = () => Math.round(performance.now() - started);
  const pc = new RTCPeerConnection({iceServers:[]});
  const transceiver = pc.addTransceiver('video', {direction:'recvonly'});
  let resource;
  let callback;
  let transport;
  const event = (stage, detail) => {
    const row = {stage, detail, at_ms:elapsed()};
    record.events.push(row);
    const item = document.createElement('li');
    const time = document.createElement('time');
    time.textContent = offset(row.at_ms);
    const text = document.createElement('span');
    text.textContent = stage + ' · ' + detail;
    item.append(time, text);
    el('timeline').append(item);
    return row.at_ms;
  };
  const observeICE = () => {
    if (record.ice_state === pc.iceConnectionState) return;
    record.ice_state = pc.iceConnectionState;
    const at = event('ICE', record.ice_state);
    if (['connected', 'completed'].includes(record.ice_state)) record.ice_connected_ms ??= at;
    renderReport();
  };
  const observeDTLS = () => {
    if (!transport || record.dtls_state === transport.state) return;
    record.dtls_state = transport.state;
    const at = event('DTLS', record.dtls_state);
    if (record.dtls_state === 'connected') record.dtls_connected_ms ??= at;
    renderReport();
  };
  pc.addEventListener('iceconnectionstatechange', observeICE);
  pc.addEventListener('track', ({track}) => {
    video.srcObject = new MediaStream([track]);
    void video.play().catch(() => {});
  });
  try {
    video.srcObject = null;
    video.hidden = false;
    callback = video.requestVideoFrameCallback((now, frame) => {
      record.first_frame_ms = Math.round(now - started);
      record.width = frame.width;
      record.height = frame.height;
      event('播放器', '呈现首帧');
      renderReport();
    });
    await pc.setLocalDescription(await pc.createOffer());
    await waitGathering(pc, signal);
    record.offer_ready_ms = event('SDP', 'Offer 已就绪');
    const httpStart = performance.now();
    const response = await fetch(source.endpoint, {
      method:'POST', signal, cache:'no-store',
      headers:{'Content-Type':'application/sdp', Accept:'application/sdp'}, body:pc.localDescription.sdp,
    });
    record.http_status = response.status;
    record.http_ms = Math.round(performance.now() - httpStart);
    event('HTTP', String(response.status));
    if (response.status !== 201 || !response.headers.get('Content-Type')?.startsWith('application/sdp')) {
      throw new Error('WHEP 未返回 HTTP 201 和 SDP Answer。');
    }
    const path = response.headers.get('Location');
    if (!path?.startsWith(api + '/sessions/')) throw new Error('WHEP 未返回本机会话地址。');
    resource = path;
    const answer = await response.text();
    await pc.setRemoteDescription({type:'answer', sdp:answer});
    record.codec = answer.match(/a=rtpmap:\d+ (H264\/90000)/i)?.[1] || '';
    record.answer_applied_ms = event('SDP', 'Answer 已应用');
    observeICE();
    transport = transceiver.receiver.transport;
    transport?.addEventListener('statechange', observeDTLS);
    observeDTLS();
    renderReport();
    const deadline = performance.now() + (source.mode === 'ice_timeout' ? 5400 : 6900);
    while (performance.now() < deadline) {
      signal.throwIfAborted();
      observeICE();
      observeDTLS();
      const stats = await transceiver.receiver.getStats();
      for (const sample of stats.values()) {
        if (sample.type !== 'inbound-rtp' || !sample.packetsReceived) continue;
        if (record.first_rtp_ms == null) record.first_rtp_ms = event('RTP', '首次从浏览器统计观测到视频 RTP');
        if (sample.packetsReceived > record.rtp_packets) record.last_rtp_observed_ms = elapsed();
        record.rtp_packets = sample.packetsReceived;
        record.frames_decoded = sample.framesDecoded || 0;
      }
      renderReport();
      if (source.mode === 'normal' && record.first_frame_ms != null && record.first_rtp_ms != null) break;
      await new Promise(resolve => setTimeout(resolve, 125));
    }
    record.source = await request(resource, {signal});
    record.observed_ms = elapsed();
    const connected = record.ice_connected_ms != null && record.dtls_connected_ms != null;
    const rendered = record.rtp_packets > 0 && record.frames_decoded > 0 && record.first_frame_ms != null;
    if (source.mode === 'normal') {
      record.passed = connected && rendered;
      record.result = record.passed ? '已呈现画面' : '未完整观测到播放';
    } else if (source.mode === 'ice_timeout') {
      record.passed = record.ice_connected_ms == null && !record.rtp_packets && record.source.ice_packets_dropped > 0;
      record.result = record.passed ? '已复现 ICE 探测超时' : '未复现 ICE 探测超时';
    } else if (source.mode === 'no_media') {
      record.passed = connected && !record.rtp_packets && !record.source.rtp_packets && record.first_frame_ms == null;
      record.result = record.passed ? '已连接，观测窗口内无 RTP' : '未复现连接后无 RTP';
    } else {
      record.passed = connected && rendered && Boolean(record.source.media_stopped_at) &&
        record.observed_ms - record.last_rtp_observed_ms >= 800;
      record.result = record.passed ? '已出画，随后 RTP 停止' : '未确认出画后 RTP 停止';
    }
  } finally {
    if (callback != null) video.cancelVideoFrameCallback(callback);
    transport?.removeEventListener('statechange', observeDTLS);
    pc.removeEventListener('iceconnectionstatechange', observeICE);
    pc.close();
    if (resource) void fetch(resource, {method:'DELETE'}).catch(() => {});
  }
}

el('source-form').addEventListener('submit', event => {
  event.preventDefault();
  void changeSource('/start', {mode:el('mode').value});
});
el('stop').addEventListener('click', () => { void changeSource('/stop'); });
el('probe').addEventListener('click', async () => {
  notice();
  el('timeline').replaceChildren();
  report = {version:1, mode:source.mode, started_at:new Date().toISOString(), events:[], rtp_packets:0, passed:false};
  running = new AbortController();
  renderSource();
  renderReport();
  try {
    await play(source, running.signal, report);
  } catch (error) {
    report.result = running.signal.aborted ? '观测已结束' : '播放诊断失败';
    if (!running.signal.aborted) {
      report.error = error.message;
      notice(error.message);
    }
  } finally {
    running = null;
    renderSource();
    renderReport();
  }
});
el('cancel').addEventListener('click', () => { running?.abort(); });
el('export').addEventListener('click', () => {
  const url = URL.createObjectURL(new Blob([JSON.stringify(report, null, 2)], {type:'application/json'}));
  const link = document.createElement('a');
  link.href = url;
  link.download = 'whep-playback.json';
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
});

async function poll() {
  const current = generation;
  if (!busy) {
    try {
      const latest = await request(api);
      if (current === generation && !busy) { source = latest; renderSource(); }
    } catch (error) {
      if (current === generation && !busy && !report) notice(error.message);
    }
  }
  setTimeout(poll, 1000);
}
void poll();
