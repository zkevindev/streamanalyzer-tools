'use strict';

const api = '/api/simulators/hls';
const el = id => document.getElementById(id);
const modes = {normal: '正常点播', live: '滚动直播', abr: '双码率 fMP4 直播', audio: '独立音轨分片故障', ll: '低延迟直播 / Part 延迟', delay: '分片延迟', not_found: '分片返回 404'};
let current;
let busy = false;
let generation = 0;

function updateFields() {
  const mode = el('mode').value;
  el('simulator-fields').disabled = busy || !current || current.active;
  for (const [input, field, show] of [['segment', 'segment-field', ['delay', 'not_found'].includes(mode)], ['delay-ms', 'delay-field', mode === 'delay']]) {
    el(field).hidden = !show;
    el(input).disabled = !show;
  }
  el('start').disabled = el('simulator-fields').disabled;
  el('stop').disabled = busy || !current?.active;
  el('freeze').disabled = busy || !current?.active;
  const fault = current?.live_fault;
  const partial = current?.low_latency?.fault;
  el('arm-part').disabled = busy || !current?.active || Boolean(partial && !partial.unblocked_at && Date.now() < Date.parse(partial.release_at) + 6000);
  el('live-fault-fields').disabled = busy || !current?.active || Boolean(fault && !fault.retry_at && !fault.expired);
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
  const live = status.mode === 'live';
  const abr = status.mode === 'abr';
  const audio = status.mode === 'audio';
  const canInject = live || audio;
  const ll = status.low_latency;
  el('part-fault-form').hidden = !ll || !status.active;
  el('ll-observation').hidden = !ll;
  const partial = ll?.fault;
  el('ll-observation').textContent = ll ? [
    '下一个 Part ' + ll.next_sequence + '/' + ll.next_part, '阻塞刷新 ' + ll.blocked_reloads + ' 次',
    ll.last_reload_wait_ms != null && '最近刷新等待 ' + ll.last_reload_wait_ms + ' ms',
    partial && ['故障 Part ' + partial.sequence + '/' + partial.part,
      partial.unblocked_at ? '已返回' : partial.requested_at ? '延迟返回中' : '等待播放请求'].join(' · ')
  ].filter(Boolean).join(' · ') : '';
  if (status.active && ll) el('open-workbench').href += '&hls_ll=1';
  if (status.active && audio) el('open-workbench').href += '&hls_recovery=1';
  if (status.active && abr) el('open-workbench').href += '&hls_abr=1';
  el('open-recovery').hidden = !live;
  if (status.active && live) el('open-recovery').href = el('open-workbench').href + '&hls_recovery=1';
  el('freeze').hidden = !live;
  el('live-fault-form').hidden = !canInject || !status.active;
  const fault = status.live_fault;
  const faultState = !fault ? '' : fault.retry_at ? '已收到重试' : fault.expired ? '观测已结束' : fault.hit_at ? '已注入，等待重试' : '等待目标分片请求';
  el('live-fault-status').hidden = !fault;
  el('live-fault-status').textContent = fault ? (fault.rendition === 'audio' ? '独立音轨 · ' : '') + '媒体序号 ' + fault.sequence + ' · ' + faultState + ' · 匹配请求 ' + fault.attempts + ' 次' : '';
  el('freeze').textContent = status.frozen ? '恢复列表更新' : '冻结播放列表';
  el('live-summary').hidden = !canInject && !abr;
  el('live-summary').textContent = '列表起始序号 ' + status.media_sequence + (status.frozen ? ' · 已冻结' : abr ? ' · 双码率初始化段独立 · 每 6 个分片标记不连续' : audio ? ' · 故障仅作用于独立音轨' : ' · 更新中');
  const samples = status.playlist_samples || [];
  el('playlist-samples').hidden = (!canInject && !abr) || samples.length === 0;
  el('playlist-samples').replaceChildren(...samples.slice(-8).reverse().map(sample => {
    const row = document.createElement('li');
    row.className = 'event-row';
    const time = document.createElement('time');
    time.className = 'event-time';
    time.dateTime = sample.at;
    time.textContent = new Date(sample.at).toLocaleTimeString('zh-CN');
    const code = document.createElement('span');
    code.textContent = '列表';
    const value = document.createElement('span');
    value.textContent = '序号 ' + sample.sequence + (sample.frozen ? ' · 已冻结' : '');
    row.append(time, code, value);
    return row;
  }));
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
el('live-fault-form').addEventListener('submit', event => {
  event.preventDefault();
  void change('/live-fault', {
    kind: el('live-fault-kind').value,
    sequence: Number(el('live-fault-seq').value),
    audience: el('live-fault-audience').value,
  });
});
el('part-fault-form').addEventListener('submit', event => {
  event.preventDefault();
  void change('/part-delay', {delay_ms: Number(el('part-delay-ms').value)});
});
el('freeze').addEventListener('click', () => { void change('/freeze', {frozen: !current.frozen}); });
el('stop').addEventListener('click', () => { void change('/stop'); });
void poll();
