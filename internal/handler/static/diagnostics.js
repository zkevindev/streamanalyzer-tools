'use strict';

const apiRoot = '/api/diagnostics';
const statusLabels = {
    running: '采样中', done: '已完成', partial: '保留部分证据',
    failed: '采样失败', canceled: '已停止', interrupted: '服务中断',
};
const endLabels = {
    duration_limit: '达到采样时长', eof: '响应结束', canceled: '用户停止',
    size_limit: '达到原始流上限', tag_limit: '达到标签数量上限',
    http_status: 'HTTP 状态异常', request_error: '请求失败',
    invalid_flv: 'FLV 头部异常', truncated_tag: 'FLV 标签提前结束', body_error: '读取响应体中断',
    rtmp_error: 'RTMP 连接或读取失败', hls_error: 'HLS 播放列表不可用',
    udp_bind_error: '无法监听 UDP 地址', udp_read_error: '读取 UDP 数据报失败', timeline_limit: '达到证据时间线上限',
};
const protocols = {
    'http-flv': { label: 'HTTP-FLV', example: 'https://live.example.com/app/stream.flv', dump: '原始 FLV' },
    rtmp: { label: 'RTMP', example: 'rtmp://live.example.com/app/stream', dump: '原始 RTMP' },
    hls: { label: 'HLS', example: 'https://live.example.com/app/playlist.m3u8', dump: '原始 HLS ZIP' },
    'http-ts': { label: 'HTTP MPEG-TS', example: 'https://live.example.com/app/stream.ts', dump: '原始 TS', mpegTS: true },
    'udp-ts': { label: 'UDP MPEG-TS', example: 'udp://127.0.0.1:5000', dump: '原始 UDP 载荷', mpegTS: true, receive: true },
    'rtp-ts': { label: 'RTP MPEG-TS', example: 'rtp://127.0.0.1:5000', dump: '原始 RTP 数据报', mpegTS: true, receive: true },
};
const stageLabels = {
    dns: 'DNS 解析', connect: '建立连接', tls: 'TLS 握手', response_headers: 'HTTP 响应头',
    listen: '建立监听', receive: '接收数据', handshake: 'RTMP 握手', play: 'RTMP 建连与播放协商',
};
const modelProviderLabels = {
    'openai-compatible': 'OpenAI 兼容接口',
    deepseek: 'DeepSeek',
    anthropic: 'Anthropic',
    gemini: 'Google Gemini',
};
const node = (id) => document.getElementById(id);
const severityLabels = { error: '错误', warn: '警告', info: '提示' };
const tagLabels = {
    video: '视频', audio: '音频', invalid_audio: '音频标签异常', invalid_video: '视频标签异常', invalid_tag_size: '标签长度异常',
    hls_playlist: '播放列表', hls_init: '初始化段', hls_segment: '媒体分片', hls_gap: '标记缺失的分片',
    ts_packet: 'TS 包', ts_sync_loss: 'TS 同步丢失', rtp_datagram: 'RTP 数据报',
};
const renditionLabels = { main: '主路', audio: '独立音频' };
let desktop = false;
let currentID = '';
let currentStatus = '';
let activePoll;
let runs = [];
let currentModel = null;
let currentExplanation;
let modelConfigRequest = 0;

async function api(path, options) {
    const response = await fetch(apiRoot + path, options);
    if (!response.ok) {
        const result = await response.json().catch(() => ({}));
        throw new Error(result.error || `请求失败（HTTP ${response.status}）`);
    }
    return response.json();
}

function message(text, error = false) {
    node('notice').hidden = !text;
    node('notice').textContent = text;
    node('notice').classList.toggle('error', error);
}

function element(tag, text, className) {
    const item = document.createElement(tag);
    item.textContent = text;
    if (className) item.className = className;
    return item;
}

function runPath(id) {
    return '/runs/' + encodeURIComponent(id);
}

function protocolLabel(protocol, origin) {
    if (protocol === 'rtmp' && origin?.startsWith('rtmps://')) return 'RTMPS';
    return protocols[protocol || 'http-flv']?.label || protocol;
}

function renderHistory() {
    node('run-count').hidden = runs.length === 0;
    node('run-count').textContent = runs.length;
    node('empty-history').hidden = runs.length > 0;
    node('empty-history').textContent = '暂无排障记录';
    const items = runs.map((run) => {
        const row = element('li', '');
        const protocol = protocolLabel(run.protocol, run.origin);
        const button = element('button', run.origin || protocol);
        button.type = 'button';
        if (run.id === currentID) button.setAttribute('aria-current', 'true');
        const when = new Date(run.created_at).toLocaleString();
        const mode = run.mode === 'replay' ? '导入复盘' : (protocols[run.protocol]?.receive ? '收包' : '拉流');
        const meta = element('span', '', 'run-meta');
        const status = element('span', statusLabels[run.status] || run.status, 'status');
        status.dataset.status = run.status;
        meta.append(status, element('span', protocol), element('span', mode), element('span', when));
        button.append(meta);
        button.addEventListener('click', () => selectRun(run.id));
        row.append(button);
        return row;
    });
    node('runs').replaceChildren(...items);
}

async function refreshHistory() {
    runs = await api('/runs');
    renderHistory();
}

function renderTable(columns, rows) {
    const wrap = element('div', '', 'scroll');
    const table = document.createElement('table');
    const header = table.createTHead().insertRow();
    for (const title of columns) {
        const cell = element('th', title);
        cell.scope = 'col';
        header.append(cell);
    }
    const body = table.createTBody();
    for (const values of rows) {
        const row = body.insertRow();
        for (const value of values) {
            row.insertCell().textContent = value ?? '—';
        }
    }
    wrap.append(table);
    return wrap;
}

function hexLabel(value, width) {
    return value == null ? '—' : `0x${value.toString(16).toUpperCase().padStart(width, '0')}`;
}

function renderEvidence(protocol, events) {
    const kind = (event) => tagLabels[event.kind] || event.kind;
    if (protocol === 'hls') {
        return renderTable(['对象', '流', '分片序号', '到达 (ms)', 'HTTP', '请求耗时 (ms)', '原始包条目', '长度（字节）'],
            events.map((event) => [kind(event), renditionLabels[event.rendition], event.sequence, event.arrival_ms, event.http_status || '—',
                event.duration_ms ?? (event.http_status ? 0 : '—'), event.artifact || '—', event.artifact ? event.size : '—']));
    }
    if (protocols[protocol]?.mpegTS) {
        const rtp = protocol === 'rtp-ts';
        return renderTable(['对象', '到达 (ms)', 'PID', ...(rtp ? ['RTP 序号'] : []), '偏移（字节）', '长度（字节）'],
            events.map((event) => [kind(event), event.arrival_ms, hexLabel(event.pid, 4), ...(rtp ? [event.rtp_sequence] : []), event.offset, event.size]));
    }
    return renderTable([protocol === 'rtmp' ? '消息' : '标签', '到达 (ms)', 'DTS (ms)', 'PTS (ms)', '偏移（字节）', protocol === 'rtmp' ? '消息跨度（字节）' : '长度（字节）'],
        events.map((event) => [kind(event), event.arrival_ms, event.dts_ms, event.pts_ms, event.offset, event.size]));
}

function showFinding(id) {
    const finding = node(id);
    if (!finding) return;
    finding.open = true;
    finding.querySelector('summary').focus({ preventScroll: true });
    finding.scrollIntoView({ block: 'nearest' });
}

function renderTrace(findings, elapsed) {
    const trace = node('trace');
    trace.hidden = findings.length === 0;
    const duration = Math.max(elapsed, 1);
    const percent = (ms) => Math.min(100, Math.max(0, ms * 100 / duration));
    const scale = element('div', '', 'trace-scale');
    for (const time of [0, elapsed / 2, elapsed]) {
        scale.append(element('span', (time / 1000).toFixed(1) + ' s'));
    }
    const rows = findings.map((finding) => {
        const row = element('button', '', 'trace-row');
        row.type = 'button';
        row.setAttribute('aria-label', `${finding.id} ${finding.message}`);
        row.title = finding.message;
        const track = element('span', '', 'trace-track');
        track.setAttribute('aria-hidden', 'true');
        const mark = element('span', '', 'trace-mark');
        const start = percent(finding.start_ms);
        mark.style.left = `min(${start}%, calc(100% - .42rem))`;
        mark.style.width = `${percent(finding.end_ms) - start}%`;
        mark.dataset.severity = finding.severity;
        track.append(mark);
        row.append(element('span', finding.id), track);
        row.addEventListener('click', () => showFinding(finding.id));
        return row;
    });
    trace.replaceChildren(scale, ...rows);
}

const time = (ms) => ms == null ? '—' : `${ms} ms`;
const bytes = (size) => `${size.toLocaleString()} 字节`;

function renderMetrics(id, values) {
    const list = node(id);
    list.hidden = values.length === 0;
    list.replaceChildren(...values.map(([label, value]) => {
        const item = element('div', '');
        item.append(element('dt', label), element('dd', value ?? '—'));
        return item;
    }));
}

function renderSamplingTiming(observation) {
    const stages = observation.stages || [];
    const values = [];
    if (observation.first_payload_ms != null) values.push(['首个载荷到达', time(observation.first_payload_ms)]);
    if (observation.failure_stage) values.push(['故障阶段', stageLabels[observation.failure_stage] || observation.failure_stage]);
    if (observation.failure_code) values.push(['故障代码', observation.failure_code]);
    node('sampling-timing').hidden = stages.length === 0 && values.length === 0;
    renderMetrics('timing-metrics', values);
    const rows = stages.map((stage) => [stageLabels[stage.name] || stage.name, stage.start_ms, stage.end_ms, stage.end_ms - stage.start_ms]);
    node('timing-table').replaceChildren(...(rows.length ? [renderTable(['阶段', '开始偏移 (ms)', '结束偏移 (ms)', '耗时 (ms)'], rows)] : []));
}

function renderTransport(protocol, ts) {
    node('transport').hidden = !ts;
    if (!ts) return;
    const values = [
        ['TS 包数', ts.packets], ['TS 包大小', ts.packet_size ? bytes(ts.packet_size) : '—'],
        ['连续计数错误', ts.continuity_errors], ['传输错误标记 (TEI)', ts.transport_errors],
        ['重复 TS 包', ts.duplicates], ['格式错误 TS 包', ts.malformed_packets],
        ['同步丢失', bytes(ts.sync_loss_bytes ?? 0)],
        ['PAT / PMT 段数', `${ts.pat_sections ?? 0} / ${ts.pmt_sections ?? 0}`], ['PCR PID 数', ts.pcr_pids],
        ['PSI 解析错误', ts.psi_errors ?? 0],
    ];
    if (ts.trailing_bytes) values.push(['尾部不完整数据', bytes(ts.trailing_bytes)]);
    if (ts.max_pcr_gap_ms) values.push(['最大 PCR 间隔', time(ts.max_pcr_gap_ms)]);
    if (protocol === 'rtp-ts') {
        values.push(['RTP 数据报', ts.rtp_datagrams ?? 0], ['估计缺失 RTP 包', ts.rtp_missing ?? 0],
            ['乱序 RTP 包', ts.rtp_out_of_order ?? 0], ['重复 RTP 包', ts.rtp_duplicates ?? 0],
            ['格式错误 RTP 数据报', ts.rtp_invalid ?? 0]);
    }
    renderMetrics('transport-metrics', values);
    const pids = ts.pids || [];
    node('ts-pids').hidden = pids.length === 0;
    if (!pids.length) {
        node('pid-table').replaceChildren();
        return;
    }
    node('pid-summary').textContent = ts.pid_count > pids.length ? `PID 明细（显示 ${pids.length} / ${ts.pid_count}）` : 'PID 明细';
    const table = renderTable(['PID', '编码', '流类型', 'TS 包数', 'TEI', '连续计数错误', '重复包', 'PES 起始数', 'PCR / PTS / DTS 计数'],
        pids.map((pid) => [hexLabel(pid.pid, 4), pid.codec || '—', hexLabel(pid.stream_type, 2), pid.packets,
            pid.transport_errors, pid.continuity_errors, pid.duplicates, pid.pes_starts,
            `${pid.pcr_count ?? 0} / ${pid.pts_count ?? 0} / ${pid.dts_count ?? 0}`]));
    table.tabIndex = 0;
    table.setAttribute('role', 'region');
    table.setAttribute('aria-label', 'PID 明细');
    node('pid-table').replaceChildren(table);
}

function renderReport(report) {
    const observed = report.manifest.observation;
    const protocol = report.manifest.protocol;
    const m = report.metrics;
    const values = [];
    if (protocol === 'hls') {
        const hls = report.hls || {};
        const bandwidth = hls.rendition_bandwidth ? `${(hls.rendition_bandwidth / 1000).toLocaleString()} kb/s` : '—';
        values.push(
            ['采集分片', `${hls.segments_captured ?? 0} · 含 ${hls.audio_segments_captured ?? 0} 个独立音频分片`],
            ['失败 / 跳过分片', `${hls.segments_failed ?? 0} / ${hls.segments_skipped ?? 0}`],
            ['清单快照 / 失败', `${hls.playlists_captured ?? 0} / ${hls.playlist_failures ?? 0}`],
            ['初始化段 / 失败', `${hls.init_segments_captured ?? 0} / ${hls.init_segments_failed ?? 0}`],
            ['加密分片', hls.encrypted_segments ?? 0],
            ['最长分片下载', time(hls.segments_captured ? hls.max_segment_download_ms ?? 0 : null)],
            ['采样变体带宽', bandwidth],
            ['媒体数据', bytes(hls.media_bytes ?? 0)],
        );
    } else if (!protocols[protocol]?.mpegTS) {
        values.push(
            ['首个视频帧', time(m.first_video_ms)],
            ['视频帧 / 音频帧', `${m.video_frames} / ${m.audio_frames}`],
            ['视频最大到达间隔', time(m.video_frames > 1 ? m.max_video_gap_ms : null)],
            ['最大关键帧间隔', time(m.max_gop_ms || null)],
        );
        if (m.first_audio_ms != null) values.push(['首个音频帧', time(m.first_audio_ms)]);
        if (m.first_keyframe_ms != null) values.push(['首个关键帧', time(m.first_keyframe_ms)]);
        if (m.max_audio_gap_ms > 0) values.push(['音频最大到达间隔', time(m.max_audio_gap_ms)]);
    }
    if (observed.ttfb_ms != null) {
        values.push([protocol === 'rtmp' ? 'RTMP 首字节' : '响应首字节', time(observed.ttfb_ms)]);
    }
    if (m.video_codecs?.length) values.push(['视频编码', m.video_codecs.join(' / ')]);
    if (m.audio_codecs?.length) values.push(['音频编码', m.audio_codecs.join(' / ')]);
    values.push([protocols[protocol]?.dump || '原始流', bytes(report.manifest.stream.bytes)]);
    renderMetrics('metrics', values);
    renderSamplingTiming(observed);
    renderTransport(protocol, report.mpegts);
    renderTrace(report.findings, observed.elapsed_ms);
    const findings = report.findings.map((finding) => {
        const item = element('details', '', 'finding');
        item.id = finding.id;
        const summary = element('summary', finding.id);
        const severity = element('span', severityLabels[finding.severity] || finding.severity, 'severity');
        severity.dataset.severity = finding.severity;
        summary.append(severity, finding.message);
        const occurrence = finding.occurrences > 0 ? ` · 共 ${finding.occurrences} 次` : '';
        item.append(summary, element('p', `${(finding.start_ms / 1000).toFixed(3)}–${(finding.end_ms / 1000).toFixed(3)} 秒 · 从开始采样计时${occurrence}`, 'muted'));
        if (finding.next_check) {
            const nextCheck = element('p', '');
            nextCheck.append(element('strong', '建议检查 '), finding.next_check);
            item.append(nextCheck);
        }
        if (finding.evidence?.length) item.append(renderEvidence(protocol, finding.evidence));
        return item;
    });
    node('findings').replaceChildren(...(findings.length ? findings : [element('p', '这次采样暂无规则触发。', 'muted')]));
}

function modelLabel(identity) {
    const provider = identity.provider || 'openai-compatible';
    return [modelProviderLabels[provider] || provider, identity.model].filter(Boolean).join(' · ');
}

function renderModelStatus() {
    const result = currentExplanation;
    const button = node('explain');
    const cached = result && currentModel &&
        (result.provider || 'openai-compatible') === currentModel.provider &&
        (!currentModel.model || result.model === currentModel.model);
    button.hidden = !currentModel || (Boolean(cached) && !desktop);
    button.textContent = result ? '用当前模型重新解读' : '生成解读';
    if (button.disabled) return;
    node('model-status').textContent = currentModel
        ? (button.hidden ? '' : `当前模型 ${modelLabel(currentModel)}。发送脱敏指标与异常证据 ID。`)
        : '模型服务尚未配置。';
}

async function refreshModelConfiguration() {
    const request = ++modelConfigRequest;
    const config = await api('/config', { cache: 'no-store' });
    if (request !== modelConfigRequest) return;
    currentModel = config.model_configured
        ? { provider: config.model_provider || 'openai-compatible', model: config.model_name || '' }
        : null;
    if (!node('report').hidden) renderModelStatus();
}

function renderExplanation(result) {
    currentExplanation = result;
    renderModelStatus();
    const content = node('explanation');
    content.replaceChildren();
    if (!result) return;
    const hypotheses = element('ul', '');
    for (const item of result.hypotheses || []) {
        const row = element('li', '');
        row.append(element('strong', `待验证 · ${item.claim} `));
        for (const id of item.evidence_ids) {
            const link = element('a', id + ' ');
            link.href = '#' + id;
            link.addEventListener('click', (event) => {
                event.preventDefault();
                showFinding(id);
            });
            row.append(link);
        }
        row.append(element('p', item.next_check));
        hypotheses.append(row);
    }
    content.append(element('p', `解读来源 ${modelLabel(result)}`, 'model-source muted'), element('p', result.summary));
    if (hypotheses.childElementCount) content.append(hypotheses);
}

function renderDetail(detail) {
    const protocol = detail.report?.manifest.protocol || detail.protocol || 'http-flv';
    const mode = detail.mode === 'replay' ? '复盘结果' : '采样结果';
    node('result-heading').textContent = `${protocolLabel(protocol, detail.origin)} ${mode}`;
    currentStatus = detail.status;
    node('stop').hidden = currentStatus !== 'running';
    node('report').hidden = !detail.report;
    const observed = detail.report?.manifest.observation;
    const parts = [detail.origin, statusLabels[detail.status] || detail.status];
    if (observed) {
        parts.push(`${(observed.elapsed_ms / 1000).toFixed(2)} 秒`, endLabels[observed.end_reason] || observed.end_reason);
        if (observed.http_status) parts.push('HTTP ' + observed.http_status);
    }
    if (detail.error) parts.push(detail.error);
    const summary = parts.filter(Boolean).join(' · ');
    if (node('summary').textContent !== summary) node('summary').textContent = summary;
    if (!detail.report) return;
    const base = apiRoot + runPath(detail.id);
    for (const [id, suffix] of Object.entries({ html: '/report.html', json: '/report.json', dump: '/dump', bundle: '/evidence.zip' })) {
        node(id).href = base + suffix;
    }
    node('dump').textContent = protocols[protocol]?.dump || '原始流';
    node('dump').download = detail.dump_name || '';
    node('dump').hidden = detail.report.manifest.stream.bytes === 0;
    renderReport(detail.report);
    renderExplanation(detail.explanation);
}

async function refreshDetail(id) {
    try {
        const detail = await api(runPath(id));
        if (id !== currentID) return;
        const previousStatus = currentStatus;
        renderDetail(detail);
        if (detail.status === 'running') {
            activePoll = setTimeout(() => refreshDetail(id), 850);
        } else if (previousStatus === 'running') {
            await refreshHistory();
        }
    } catch (err) {
        if (id === currentID) message(err.message, true);
    }
}

function selectRun(id) {
    clearTimeout(activePoll);
    currentID = id;
    currentStatus = '';
    history.replaceState(null, '', '#' + id);
    node('report').hidden = true;
    node('stop').hidden = true;
    node('summary').textContent = '正在加载记录…';
    renderHistory();
    return refreshDetail(id);
}

function bindForm(id, submit) {
    node(id).addEventListener('submit', async (event) => {
        event.preventDefault();
        const button = event.currentTarget.querySelector('button');
        button.disabled = true;
        try {
            const run = await submit();
            message('');
            await refreshHistory();
            await selectRun(run.id);
        } catch (err) {
            message(err.message, true);
        } finally {
            button.disabled = false;
        }
    });
}

node('protocol').addEventListener('change', () => {
    const protocol = node('protocol').value;
    const selected = protocols[protocol];
    node('url').placeholder = selected.example;
    node('url-label').textContent = selected.receive ? '监听地址' : '播放地址';
    const hint = node('url-hint');
    hint.hidden = !selected.receive;
    hint.textContent = selected.receive ? `填写本机 IPv4 和端口。组播示例 ${protocol.split('-')[0]}://239.1.1.1:5000?interface=en0` : '';
});
bindForm('sample', () => api('/runs', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ protocol: node('protocol').value, url: node('url').value.trim(), seconds: node('seconds').valueAsNumber }),
}));
bindForm('import', () => {
    message('正在上传证据包并重跑规则…');
    return api('/import', {
        method: 'POST', headers: { 'Content-Type': 'application/zip' }, body: node('archive').files[0],
    });
});
node('stop').addEventListener('click', async () => {
    const id = currentID;
    node('stop').disabled = true;
    try {
        await api(runPath(id) + '/stop', { method: 'POST' });
        if (id === currentID) message('已请求停止，正在保存采样结果。');
    } catch (err) {
        message(err.message, true);
    } finally {
        node('stop').disabled = false;
    }
});
node('explain').addEventListener('click', async () => {
    const id = currentID;
    const button = node('explain');
    const refresh = desktop && currentExplanation ? '?refresh=1' : '';
    button.disabled = true;
    node('model-status').textContent = `正在使用 ${modelLabel(currentModel)} 生成解读…`;
    try {
        const result = await api(runPath(id) + '/explain' + refresh, { method: 'POST' });
        button.disabled = false;
        if (id === currentID) renderExplanation(result);
    } catch (err) {
        if (id === currentID) node('model-status').textContent = err.message;
    } finally {
        button.disabled = false;
        if (id !== currentID && !node('report').hidden) renderModelStatus();
    }
});

if (window.parent !== window) {
    let saving = false;
    const settingsButton = node('model-settings');
    settingsButton.addEventListener('click', () => {
        if (desktop) window.parent.postMessage({ kind: 'open-model-settings' }, '*');
    });
    for (const [id, name] of Object.entries({ json: 'report.json', dump: 'dump', bundle: 'evidence.zip' })) {
        node(id).addEventListener('click', (event) => {
            if (!desktop) return;
            event.preventDefault();
            if (saving) return;
            saving = true;
            message('请选择产物保存位置。');
            window.parent.postMessage({ kind: 'save-artifact', id: currentID, name }, '*');
        });
    }
    window.addEventListener('message', async (event) => {
        if (event.source !== window.parent) return;
        if (event.data?.kind === 'desktop-shell') {
            desktop = true;
            settingsButton.hidden = !event.data.modelSettings;
            if (!node('report').hidden) renderModelStatus();
        }
        if (event.data?.kind === 'model-settings-closed' && !settingsButton.hidden) settingsButton.focus();
        if (event.data?.kind === 'model-settings-updated') {
            try {
                await refreshModelConfiguration();
                message('模型设置已保存。');
            } catch (err) {
                message(`模型设置已保存，刷新模型状态失败。${err.message}`, true);
            }
        }
        if (event.data?.kind === 'artifact-result') {
            saving = false;
            message(event.data.status, event.data.error);
        }
    });
    window.parent.postMessage({ kind: 'workbench-ready' }, '*');
}

async function initialize() {
    try {
        await refreshModelConfiguration();
        await refreshHistory();
        const hash = location.hash.slice(1);
        const id = /^[0-9a-f]{32}$/.test(hash) ? hash : runs[0]?.id;
        if (id) await selectRun(id);
    } catch (err) {
        message(err.message, true);
    }
}
initialize();
