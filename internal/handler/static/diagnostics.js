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
};
const protocols = {
    'http-flv': { label: 'HTTP-FLV', example: 'https://live.example.com/app/stream.flv', dump: '原始 FLV' },
    rtmp: { label: 'RTMP', example: 'rtmp://live.example.com/app/stream', dump: '原始 RTMP' },
    hls: { label: 'HLS', example: 'https://live.example.com/app/playlist.m3u8', dump: '原始 HLS ZIP' },
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
        const mode = run.mode === 'replay' ? '导入复盘' : '拉流';
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

function renderEvidence(protocol, events) {
    const wrap = element('div', '', 'scroll');
    const table = document.createElement('table');
    const header = table.createTHead().insertRow();
    const columns = protocol === 'hls' ? ['对象', '流', '分片序号', '到达 ms', 'HTTP', '请求耗时 ms', '原始包条目', '长度（字节）'] :
        [protocol === 'rtmp' ? '消息' : '标签', '到达 ms', 'DTS ms', 'PTS ms', '偏移（字节）', protocol === 'rtmp' ? '消息跨度（字节）' : '长度（字节）'];
    for (const title of columns) {
        const cell = element('th', title);
        cell.scope = 'col';
        header.append(cell);
    }
    const body = table.createTBody();
    for (const event of events) {
        const row = body.insertRow();
        const kind = tagLabels[event.kind] || event.kind;
        const values = protocol === 'hls' ? [kind, renditionLabels[event.rendition] || '—', event.sequence ?? '—', event.arrival_ms, event.http_status || '—',
            event.duration_ms ?? (event.http_status ? 0 : '—'), event.artifact || '—', event.artifact ? event.size : '—'] :
            [kind, event.arrival_ms, event.dts_ms, event.pts_ms, event.offset, event.size];
        for (const value of values) {
            row.insertCell().textContent = value;
        }
    }
    wrap.append(table);
    return wrap;
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

function renderReport(report) {
    const observed = report.manifest.observation;
    const protocol = report.manifest.protocol;
    const m = report.metrics;
    const time = (ms) => ms == null ? '—' : `${ms} ms`;
    const bytes = (size) => `${size.toLocaleString()} 字节`;
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
    } else {
        values.push(
            ['首个视频帧', time(m.first_video_ms)],
            ['视频帧 / 音频帧', `${m.video_frames} / ${m.audio_frames}`],
            ['视频最大到达间隔', time(m.video_frames > 1 ? m.max_video_gap_ms : null)],
            ['最大关键帧间隔', time(m.max_gop_ms || null)],
        );
    }
    if (protocol !== 'rtmp' || observed.ttfb_ms != null) {
        values.push([protocol === 'rtmp' ? 'RTMP 首字节' : '响应首字节', time(observed.ttfb_ms)]);
    }
    if (m.video_codecs?.length) values.push(['视频编码', m.video_codecs.join(' / ')]);
    if (m.audio_codecs?.length) values.push(['音频编码', m.audio_codecs.join(' / ')]);
    values.push([protocol === 'hls' ? '原始 HLS ZIP' : '原始流', bytes(report.manifest.stream.bytes)]);
    node('metrics').replaceChildren(...values.map(([label, value]) => {
        const item = element('div', '');
        item.append(element('dt', label), element('dd', value));
        return item;
    }));
    renderTrace(report.findings, observed.elapsed_ms);
    const findings = report.findings.map((finding) => {
        const item = element('details', '', 'finding');
        item.id = finding.id;
        const summary = element('summary', finding.id);
        const severity = element('span', severityLabels[finding.severity] || finding.severity, 'severity');
        severity.dataset.severity = finding.severity;
        summary.append(severity, finding.message);
        item.append(summary, element('p', `${(finding.start_ms / 1000).toFixed(3)}–${(finding.end_ms / 1000).toFixed(3)} 秒 · 从开始采样计时`, 'muted'));
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
    node('url').placeholder = protocols[node('protocol').value].example;
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
