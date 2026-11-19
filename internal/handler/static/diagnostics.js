'use strict';

const apiRoot = '/api/diagnostics';
const statusLabels = {
    running: '采样中', done: '已完成', partial: '保留部分证据',
    failed: '采样失败', canceled: '已停止', interrupted: '服务中断',
};
const endLabels = {
    duration_limit: '达到采样时长', duration: '达到采样时长', eof: '响应结束', canceled: '用户停止',
    size_limit: '达到原始流上限', tag_limit: '达到标签数量上限',
    http_status: 'HTTP 状态异常', request_error: '请求失败',
    invalid_flv: 'FLV 头部异常', truncated_tag: 'FLV 标签提前结束', body_error: '读取响应体中断',
    rtmp_error: 'RTMP 连接或读取失败', rtsp_error: 'RTSP 控制或接收失败', hls_error: 'HLS 播放列表不可用',
    udp_bind_error: '无法监听 UDP 地址', udp_read_error: '读取 UDP 数据报失败', timeline_limit: '达到证据时间线上限',
};
const protocols = {
    'http-flv': { label: 'HTTP-FLV', example: 'https://live.example.com/app/stream.flv', dump: '原始 FLV' },
    rtmp: { label: 'RTMP', example: 'rtmp://live.example.com/app/stream', dump: '原始 RTMP' },
    rtsp: { label: 'RTSP', example: 'rtsp://camera.example/stream', dump: '原始 RTSP/RTP ZIP', nativeRTSP: true },
    rtsps: { label: 'RTSPS', example: 'rtsps://camera.example/stream', dump: '原始 RTSP/RTP ZIP', nativeRTSP: true },
    hls: { label: 'HLS', example: 'https://live.example.com/app/playlist.m3u8', dump: '原始 HLS ZIP' },
    'http-ts': { label: 'HTTP MPEG-TS', example: 'https://live.example.com/app/stream.ts', dump: '原始 TS', mpegTS: true },
    'udp-ts': { label: 'UDP MPEG-TS', example: 'udp://127.0.0.1:5000', dump: '原始 UDP 载荷', mpegTS: true, receive: true },
    'rtp-ts': { label: 'RTP MPEG-TS', example: 'rtp://127.0.0.1:5000', dump: '原始 RTP 数据报', mpegTS: true, receive: true },
};
const stageLabels = {
    dns: 'DNS 解析', connect: 'TCP 建连', tls: 'TLS 握手', request_send: '发送请求',
    response_first_byte: '等待响应首字节', response_headers: 'HTTP 响应头',
    listen: '建立监听', receive: '接收数据', handshake: 'RTMP 握手', app_connect: 'RTMP 应用连接',
    create_stream: 'RTMP 创建流', play: 'RTMP 播放协商',
};
const httpStages = [
    ['dns', 'DNS'], ['connect', 'TCP'], ['tls', 'TLS'], ['request_send', '发送请求'],
    ['response_first_byte', '等待首字节'], ['response_headers', '接收响应头'],
];
const requestKinds = { hls_playlist: '播放列表', hls_init: '初始化段', hls_segment: '媒体分片' };
const mediaArrivalLabels = {
    segment: ['首个视频分片完成', '首个音频分片完成'],
    pes: ['首个视频 PES 起始', '首个音频 PES 起始'],
    rtp: ['首个视频 RTP 到达', '首个音频 RTP 到达'],
};
const rtspStageLabels = { dns: 'DNS 解析', connect: 'TCP 建连', tls: 'TLS 握手', describe: 'RTSP DESCRIBE', setup: 'RTSP SETUP', play: 'RTSP PLAY', options: 'RTSP OPTIONS', teardown: 'RTSP TEARDOWN', rtsp_describe: 'RTSP DESCRIBE', rtsp_setup: 'RTSP SETUP', rtsp_play: 'RTSP PLAY', rtsp_options: 'RTSP OPTIONS', rtsp_teardown: 'RTSP TEARDOWN', receive: '接收 RTSP/RTP 应用字节', rtsp_receive: '接收 RTSP/RTP 应用字节' };
const rtspKindLabels = { video: '视频', audio: '音频', other: '其他' };
const rtspFailureLabels = { timeout: '超时', canceled: '已取消', dns_error: 'DNS 失败', connection_refused: '连接被拒绝', tls_error: 'TLS 失败', network_error: '网络错误', rtsp_error: 'RTSP 错误', rtsp_status: 'RTSP 状态异常', rtsp_auth_error: 'RTSP 认证失败', rtsp_transport_error: 'RTSP 传输协商失败', rtsp_parse_error: 'RTSP 解析失败' };
const rtspBoundaryNote = '首音视频时间表示对应 RTP 包到达本接收点，不是解码首帧。SDP 声明的轨道与编码单独列出，不代表已收到或解码媒体。';
const rtspSequenceNote = '序号缺口和缺失包是接收点估计，使用有限回填窗口，不能直接等同于 TCP 网络丢包。序号跳变/重置属于候选，时间戳回退也可能来自 B 帧或乱序，需结合源端编码、流切换和 RTP 记录验证。';
const rtspPreviewNote = '原生 RTSP/RTP 采样不依赖浏览器预览。当前版本不提供浏览器 RTSP 预览，播放器首屏、缓冲和呈帧指标未采集。';

function rtspTransportLabel(transport) {
    return transport === 'udp' ? 'UDP 单播 RTP/RTCP' : transport === 'tcp_interleaved' ? 'TCP 交织 RTP/RTCP' : transport || '未记录';
}

function rtspEvidenceArtifact(name) {
    return name === 'udp-datagrams.bin' ? name : 'server-wire.bin';
}

function updateRTSPTransport() {
    const protocol = node('protocol').value;
    const native = Boolean(protocols[protocol]?.nativeRTSP);
    const select = node('rtsp-transport');
    node('rtsp-transport-options').hidden = !native;
    select.disabled = !native;
    select.querySelector('option[value="udp"]').disabled = protocol === 'rtsps';
    if (protocol === 'rtsps') select.value = 'tcp';
    if (native) node('url-hint').textContent = '填写摄像头或 NVR 的播放地址，支持 URL 用户信息认证。' +
        (protocol === 'rtsps' ? 'RTSPS 使用 TLS/TCP 并校验服务端证书。' : select.value === 'udp' ? '媒体使用 UDP 单播，控制连接仍使用 TCP；不会自动切换到 TCP 媒体传输。' : '媒体使用 TCP 交织 RTP/RTCP。') + '报告仅显示协议和主机。';
}

function rtspFailureLabel(code) {
    return code ? (rtspFailureLabels[code] ? `${rtspFailureLabels[code]} [${code}]` : code) : '未记录';
}

function rtspPhaseDuration(stages, name) {
    const values = (stages || []).filter((stage) => stage.name === name).map((stage) => `${stage.end_ms - stage.start_ms} ms`);
    return values.length ? values.join(' / ') : '未观测';
}

function rtspObservedCounter(track, value) {
    return track.received ? (value == null ? '未观测' : String(value)) : '未观测';
}

function renderRTSPReport(report) {
    const native = Boolean(protocols[report.manifest.protocol]?.nativeRTSP);
    node('rtsp-report').hidden = !native;
    if (!native) return;
    const metrics = report.rtsp;
    node('rtsp-capability').textContent = rtspPreviewNote;
    node('rtsp-boundary').textContent = rtspBoundaryNote;
    node('rtsp-sequence-note').textContent = metrics?.transport === 'udp' ? '序号缺口和缺失包是接收点估计，使用有限回填窗口；源端漏发、链路丢包和迟到包都可能影响结果，需对照源端及中间网络证据。' : rtspSequenceNote;
    if (!metrics) {
        renderMetrics('rtsp-metrics', []);
        for (const id of ['rtsp-request-table', 'rtsp-declared-tracks', 'rtsp-received-tracks', 'rtsp-sequence-table']) node(id).replaceChildren();
        node('rtsp-report-state').textContent = '当前报告未包含 RTSP 会话与 RTP 接收指标。';
        return;
    }
    const stages = report.manifest.observation.stages || [];
    renderMetrics('rtsp-metrics', [
        ['传输', rtspTransportLabel(metrics.transport)],
        ['应用字节边界', metrics.secure ? 'TLS 解密后的 RTSP/RTP' : 'RTSP/RTP 应用字节'],
        ['会话连接数', metrics.connections ?? '未观测'], ['DNS 阶段', rtspPhaseDuration(stages, 'dns')], ['TCP 阶段', rtspPhaseDuration(stages, 'connect')],
        ['TLS 阶段', metrics.secure ? rtspPhaseDuration(stages, 'tls') : '不适用（RTSP）'],
        ['RTP 包', metrics.rtp_packets ?? '未观测'], ['RTCP 包', metrics.rtcp_packets ?? '未观测'], ['RTP 字节', metrics.rtp_bytes == null ? '未观测' : bytes(metrics.rtp_bytes)],
        ...(metrics.transport === 'udp' ? [['收到 UDP 数据报', metrics.udp_datagrams ?? 0], ['UDP 字节', bytes(metrics.udp_bytes ?? 0)], ['符合协商来源', metrics.accepted_datagrams ?? 0], ['来源过滤', metrics.filtered_datagrams ?? 0], ['超过媒体库缓冲的数据报', metrics.udp_oversize_datagrams ?? 0], ['首个 RTCP 到达', time(metrics.first_rtcp_ms)]] : []),
        ['首个 RTP 到达', videoObservedTime(metrics.first_rtp_ms)], ['首个视频 RTP 到达', videoObservedTime(metrics.first_video_rtp_ms)], ['首个音频 RTP 到达', videoObservedTime(metrics.first_audio_rtp_ms)],
        ['格式异常包', metrics.malformed_packets ?? '未观测'], ['未知通道包', metrics.unknown_channel_packets ?? '未观测'], ['未知载荷类型包', metrics.unknown_payload_packets ?? '未观测'], ['尾部不完整字节', metrics.trailing_bytes ?? '未观测'],
    ]);
    const notices = [];
    if (metrics.analysis_limited) notices.push('已达到分析上限，统计只覆盖已分析的部分；原始字节仍按采样限制保留。');
    if (metrics.requests_truncated > 0) notices.push(`另有 ${metrics.requests_truncated} 条控制请求未写入指标，控制历史可能不完整。`);
    node('rtsp-report-state').textContent = notices.join(' ');
    const requests = metrics.requests || [];
    node('rtsp-request-table').replaceChildren(requests.length ? renderTable(['请求', '连接', '方法', 'CSeq', 'RTSP 状态', '开始偏移', '结束 / 已记录至', '请求耗时', '响应记录', '故障代码'], requests.slice(0, 64).map((request) => [
        `#${request.number}`, `#${request.connection_id}`, request.method, request.cseq, request.status_code > 0 ? request.status_code : '未收到响应', time(request.start_ms), time(request.end_ms),
        request.end_ms >= request.start_ms ? time(request.end_ms - request.start_ms) : '未观测', request.complete ? '完整' : '未完整结束', rtspFailureLabel(request.failure_code),
    ])) : element('p', '未观测到可展示的 RTSP 控制响应。', 'timing-note muted'));
    const tracks = (metrics.tracks || []).slice(0, 32);
    node('rtsp-declared-tracks').replaceChildren(tracks.length ? renderTable(['轨道', '媒体标识', 'SDP 媒体类型', 'SDP 声明编码', '载荷类型 PT', '时钟频率', '声道'], tracks.map((track) => [
        `#${track.id}`, track.media_id, rtspKindLabels[track.kind] || track.kind || '未声明', track.codec || '未声明', track.payload_type,
        track.clock_rate > 0 ? `${track.clock_rate} Hz` : '未声明', track.kind === 'audio' ? (track.channels > 0 ? track.channels : '未声明') : '不适用',
    ])) : element('p', metrics.sdp_available ? 'SDP 未声明可识别的媒体轨道。' : '未取得可解析 SDP，不能据此确认轨道或编码。', 'timing-note muted'));
    node('rtsp-received-tracks').replaceChildren(tracks.length ? renderTable(['轨道', '接收状态', 'RTP 包', 'RTP 字节', '首个 RTP 到达', '末个 RTP 到达', '最大包到达间隔', '分析范围'], tracks.map((track) => [
        `#${track.id}`, track.received ? '已收到 RTP' : '未收到对应 RTP', track.rtp_packets ?? '未观测', track.rtp_bytes ?? '未观测', videoObservedTime(track.first_rtp_ms), videoObservedTime(track.last_rtp_ms),
        track.rtp_packets > 1 ? videoObservedTime(track.max_arrival_gap_ms) : '未观测', track.analysis_limited ? '达到分析上限' : '按已保存窗口统计',
    ])) : element('p', '没有可对应到声明轨道的 RTP 接收记录。', 'timing-note muted'));
    node('rtsp-sequence-table').replaceChildren(tracks.length ? renderTable(['轨道', '序号缺口次数', '估计缺失包', '乱序包', '重复包', 'SSRC 变化', '序号跳变/重置候选', 'RTP 时间戳回退'], tracks.map((track) => [
        `#${track.id}`, ...['sequence_gaps', 'missing_sequence_packets', 'out_of_order_packets', 'duplicate_packets', 'ssrc_changes', 'sequence_resets', 'timestamp_regressions'].map((key) => rtspObservedCounter(track, track[key])),
    ])) : element('p', '轨道序号统计未观测。', 'timing-note muted'));
    for (const table of node('rtsp-report').querySelectorAll('.scroll')) { table.tabIndex = 0; table.setAttribute('role', 'region'); table.setAttribute('aria-label', 'RTSP 与 RTP 证据表，可横向滚动'); }
}
const playerSources = { same_capture: '同一采样流', independent_pull: '独立 HLS 拉流' };
const playerStates = {
    awaiting: '等待播放器数据', recording: '采集中', complete: '采集完成',
    unsupported: '当前环境不支持该播放格式', autoplay_blocked: '自动播放受限', error: '播放失败',
};
const playbackErrors = { media_error: '播放器媒体解码失败', network_error: '播放器网络请求失败', no_stream: '播放器未收到媒体流' };
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
    player_stall: '播放器卡顿',
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
let detailRequest = 0;
let activePlayer;
const finalizingPlayers = new Set();
const selectedStalls = new Map();
const selectedPresentationGaps = new Map();
const selectedIntervalKinds = new Map();
const pendingStallRequests = new Set();
const pendingStallExplanations = new Set();
const pendingStallStops = new Set();
const openPlayerNetworkDetails = new Set();
let currentReport;
const investigationStates = { queued: '等待补查', running: '补查中', complete: '补查完成', partial: '保留部分结果', failed: '补查失败', canceled: '补查已停止' };
const investigationMediaStates = { ready: '片段与截图已提取', partial: '媒体产物部分可用', unavailable: '媒体产物不可用', failed: '媒体提取失败' };
const investigationModelStates = { pending: '正在结合时序证据生成建议…', complete: '已生成建议', unconfigured: '尚未配置大模型，可在模型设置中配置后生成建议。', failed: '模型建议生成失败，可重试。', changed: '模型配置已变化，可使用当前模型重新生成建议。' };
const investigationAlignments = { media_clock: '播放器媒体时间对齐', player_clock: '播放器媒体时间对齐', player_clock_estimate: '根据播放器媒体时间估算', arrival_estimate: '按接收时序估算', independent_capture: '独立采样连接参考', independent_capture_reference: '独立连接采样参考' };

function playerSnapshotBody(recording) {
    // Keep the first 10 seconds at native resolution and use about one sample per second after that.
    // This stable selection preserves the uploaded prefix when a later snapshot appends samples.
    const samples = recording.samples.filter((sample, index) => sample.at_ms <= 10_000 || index % 4 === 0 ||
        (recording.complete && index === recording.samples.length - 1));
    return JSON.stringify({ ...recording, samples });
}

function playerRequest(player, body, options = {}) {
    return fetch(apiRoot + runPath(player.id) + '/player', {
        ...options,
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-StreamAnalyzer-Player': player.token },
        body,
    });
}

async function uploadPlayerSnapshot(player, body) {
    const abort = new AbortController();
    player.uploadAbort = abort;
    const deadline = setTimeout(() => abort.abort(), 5000);
    try {
        return (await playerRequest(player, body, { signal: abort.signal })).status;
    } catch {
        return 0;
    } finally {
        clearTimeout(deadline);
        if (player.uploadAbort === abort) player.uploadAbort = null;
    }
}

async function drainPlayerUploads(player) {
    let saved = false;
    try {
        while (player.nextRecording && !player.unloading) {
            const recording = player.nextRecording;
            player.nextRecording = null;
            const body = playerSnapshotBody(recording);
            let status = await uploadPlayerSnapshot(player, body);
            if (recording.complete && !player.unloading && (status === 0 || status >= 500)) {
                status = await uploadPlayerSnapshot(player, body);
            }
            saved = status >= 200 && status < 300;
        }
        return saved;
    } finally {
        player.upload = null;
    }
}

function queuePlayerSnapshot(player, recording) {
    if (player.unloading) return;
    player.nextRecording = recording;
    player.upload ||= drainPlayerUploads(player);
    return player.upload;
}

function sendClosingRecording(player, recording) {
    player.nextRecording = null;
    player.uploadAbort?.abort();
    const body = playerSnapshotBody(recording);
    void playerRequest(player, body, { keepalive: body.length <= 60_000 }).catch(() => {});
}

function finishPlayer(player, recording) {
    if (player.finalized) return;
    player.finalized = true;
    clearInterval(player.heartbeat);
    clearTimeout(player.deadline);
    if (activePlayer === player) {
        activePlayer = null;
        node('player-preview').hidden = true;
        node('play-player').hidden = true;
    }
    if (player.unloading) {
        sendClosingRecording(player, recording);
        return;
    }
    player.finalRecording = recording;
    finalizingPlayers.add(player);
    void queuePlayerSnapshot(player, recording).then((saved) => {
        finalizingPlayers.delete(player);
        if (player.unloading) return;
        const selected = currentID === player.id;
        if (!saved) message(selected ? '播放器体验记录上传失败。' : '上一条采样的播放器体验记录上传失败。', true);
        else if (selected) void refreshDetail(player.id);
    });
}

function stopPlayback(player, unloading = false) {
    if (!player) return;
    player.unloading = unloading;
    finishPlayer(player, player.collector.finish());
}

function completePlayback(player, reason) {
    if (reason !== 'eof' && reason !== 'duration_limit') {
        stopPlayback(player);
        return;
    }
    if (player.draining) return;
    player.draining = true;
    player.collector.cutoff();
    clearTimeout(player.deadline);
    player.deadline = setTimeout(() => stopPlayback(player), 800);
}

function startPlayback(run, startedAt, startedAtPerformanceMS, seconds, hlsRecovery, hlsABR) {
    if (protocols[run.protocol]?.nativeRTSP) return;
    const grant = run.player;
    if (!grant) return;
    const player = { id: run.id, token: grant.token };
    const source = playerSources[grant.source] || grant.source;
    const status = node('player-status');
    const preview = node('player-preview');
    const video = node('sample-player');
    preview.dataset.runId = run.id;
    preview.hidden = false;
    node('play-player').hidden = true;
    const quality = node('hls-quality-select');
    const label = node('hls-quality-playing');
    node('hls-quality').hidden = true;
    label.textContent = '';
    if (typeof PlayerDiagnostics === 'undefined') {
        status.textContent = '播放器模块加载失败。';
        video.hidden = true;
        return;
    }
    video.hidden = false;
    status.textContent = `正在加载播放器… · ${source}`;
    const statuses = {
        playing: '播放中', waiting: '缓冲中…', pause: '已暂停', ended: '播放结束',
        autoplay_blocked: playerStates.autoplay_blocked, unsupported: playerStates.unsupported, error: playerStates.error,
    };
    player.collector = new PlayerDiagnostics(video, grant, {
        startedAt, startedAtPerformanceMS, hlsRecovery, hlsABR,
        onQualities(levels, selectQuality) {
            if (activePlayer !== player || levels.length < 2) return;
            quality.replaceChildren(new Option('自动', '-1'), ...levels.map((level, index) => new Option(level.height + 'p', index)));
            quality.onchange = () => selectQuality(Number(quality.value));
            node('hls-quality').hidden = false;
        },
        onQualityPlaying(level) {
            if (activePlayer === player && level) label.textContent = '正在播放 ' + level.height + 'p';
        },
        onChange(event) {
            if (activePlayer !== player || !statuses[event.kind]) return;
            if (['autoplay_blocked', 'unsupported', 'error'].includes(player.status) &&
                ['waiting', 'pause', 'ended'].includes(event.kind)) return;
            player.status = event.kind;
            const label = playbackErrors[event.code] || statuses[event.kind];
            status.textContent = `${label} · ${source}`;
            node('play-player').hidden = event.kind !== 'autoplay_blocked';
        },
        onFinish: (recording) => finishPlayer(player, recording),
    });
    activePlayer = player;
    player.heartbeat = setInterval(() => queuePlayerSnapshot(player, player.collector.snapshot()), 2000);
    player.deadline = setTimeout(() => stopPlayback(player), seconds * 1000 + 5000);
    player.collector.start();
    if (!player.finalized) queuePlayerSnapshot(player, player.collector.snapshot());
}

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

function diagnosticOrigin(protocol, origin) {
    if (!protocols[protocol]?.nativeRTSP || !origin) return origin;
    try {
        const value = new URL(origin);
        if (value.protocol === 'rtsp:' || value.protocol === 'rtsps:') return value.protocol + '//' + value.host;
    } catch { /* Imported reports can omit the source address. */ }
    return '主机未记录';
}

function renderHistory() {
    node('run-count').hidden = runs.length === 0;
    node('run-count').textContent = runs.length;
    node('empty-history').hidden = runs.length > 0;
    node('empty-history').textContent = '暂无排障记录';
    const items = runs.map((run) => {
        const row = element('li', '');
        const protocol = protocolLabel(run.protocol, run.origin);
        const button = element('button', diagnosticOrigin(run.protocol, run.origin) || protocol);
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
    if (events[0]?.artifact === 'player.json') {
        return renderTable(['对象', '开始偏移 (ms)', '结束偏移 (ms)', '耗时 (ms)', '原始记录'],
            events.map((event) => [kind(event), event.arrival_ms, event.arrival_ms + (event.duration_ms ?? 0), event.duration_ms, event.artifact]));
    }
    if (protocols[protocol]?.nativeRTSP) {
        return renderTable(['对象', '轨道', 'SDP 编码', 'RTP 序号', '到达偏移 (ms)', '原始条目', '偏移（字节）', '长度（字节）'], events.map((event) => [
            event.kind === 'rtp_packet' ? 'RTP 包' : '协议字节', event.track > 0 ? `#${event.track}` : '未记录', event.codec || '未声明', event.rtp_sequence == null ? '未观测' : String(event.rtp_sequence), event.arrival_ms, rtspEvidenceArtifact(event.artifact), event.offset, event.size,
        ]));
    }
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
    for (let parent = finding.parentElement; parent; parent = parent.parentElement) {
        if (parent.tagName === 'DETAILS') parent.open = true;
    }
    finding.querySelector('summary')?.focus({ preventScroll: true });
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

function renderRequestHeaders(requests) {
    const container = node('http-header-details');
    const expanded = new Set(Array.from(container.children).filter((item) => item.open).map((item) => item.dataset.request));
    container.replaceChildren(...requests.map((request) => {
        const details = element('details', '', 'http-header-block');
        details.dataset.request = String(request.number);
        details.open = expanded.has(details.dataset.request);
        const kind = requestKinds[request.kind] || request.kind;
        const title = [kind ? `#${request.number} · ${kind}` : `#${request.number}`,
            request.http_status ? `HTTP ${request.http_status}` : '未收到响应', request.method, request.http_protocol]
            .filter(Boolean).join(' · ');
        const summary = element('summary', title);
        summary.append(element('span', `${request.start_ms}–${request.end_ms} ms`, 'http-header-offset'));
        details.append(summary);
        if (!request.headers_recorded) {
            details.append(element('p', '该记录未采集 HTTP Header。', 'timing-note muted'));
            return details;
        }
        for (const [key, title, empty] of [
            ['request', '请求头', '未记录到请求头字段。'],
            ['response', '响应头', request.http_status ? '未记录到响应头字段。' : '未收到 HTTP 响应头。'],
        ]) {
            const block = element('section', '', 'http-header-values');
            block.append(element('h5', title));
            const fields = request[`${key}_headers`] || [];
            if (fields.length) {
                const table = renderTable(['字段', '值'], fields.map((field) => [field.name, field.value]));
                table.classList.add('http-header-table');
                block.append(table);
            } else {
                block.append(element('p', empty, 'timing-note muted'));
            }
            if (request[`${key}_headers_truncated`]) {
                block.append(element('p', '头字段达到记录上限，部分字段或值已截断。', 'timing-note muted'));
            }
            details.append(block);
        }
        return details;
    }));
}

function renderRequestTiming(requests, omitted) {
    node('request-timing').hidden = requests.length === 0;
    const note = node('request-timing-truncated');
    note.hidden = omitted <= 0;
    note.textContent = omitted > 0 ? `另有 ${omitted} 条请求未展示。` : '';
    if (!requests.length) {
        node('request-table').replaceChildren();
        node('http-header-details').replaceChildren();
        return;
    }
    const hasFailures = requests.some((request) => request.failure_stage || request.failure_code);
    const columns = ['请求', 'HTTP 状态', '采样偏移', ...httpStages.map(([, label]) => label), 'TTFB', '至响应头总耗时', '响应体接收'];
    if (hasFailures) columns.push('故障阶段 / 代码');
    const rows = requests.map((request) => {
        const kind = requestKinds[request.kind] || request.kind;
        const durations = httpStages.map(([name]) => (request.stages || [])
            .filter((stage) => stage.name === name)
            .map((stage) => stage.end_ms - stage.start_ms)
            .join(' / ') || '—');
        const row = [kind ? `#${request.number} · ${kind}` : `#${request.number}`, request.http_status || '—',
            `${request.start_ms}–${request.end_ms}`, ...durations, request.ttfb_ms, request.total_ms,
            request.body_end_ms == null ? null : request.body_end_ms - request.end_ms];
        if (hasFailures) row.push([stageLabels[request.failure_stage] || request.failure_stage, request.failure_code].filter(Boolean).join(' / ') || '—');
        return row;
    });
    const table = renderTable(columns, rows);
    table.tabIndex = 0;
    table.setAttribute('role', 'region');
    table.setAttribute('aria-labelledby', 'request-timing-title');
    table.setAttribute('aria-describedby', 'request-timing-note');
    node('request-table').replaceChildren(table);
    renderRequestHeaders(requests);
}

function renderSamplingTiming(report, requests) {
    const { observation, protocol } = report.manifest;
    const metrics = report.metrics;
    const stages = (observation.stages || []).filter((stage) => !requests.length || !httpStages.some(([name]) => stage.name === name));
    const legacyRTMP = stages.some((stage) => stage.name === 'play') && !stages.some((stage) => stage.name === 'handshake');
    const stageName = (name) => protocols[protocol]?.nativeRTSP ? (rtspStageLabels[name] || name) : name === 'play' && legacyRTMP ? 'RTMP 建连与播放协商' : (stageLabels[name] || name);
    const unit = protocols[protocol]?.nativeRTSP ? 'rtp' : metrics.media_timing_unit || (protocol === 'hls' ? 'segment' : protocols[protocol]?.mpegTS ? 'pes' : '');
    const [video, audio] = mediaArrivalLabels[unit] || ['首个视频帧到达', '首个音频帧到达'];
    const native = protocols[protocol]?.nativeRTSP;
    const arrivals = [[native ? '首个有效 RTP 包' : '首个载荷到达', native ? report.rtsp?.first_rtp_ms : observation.first_payload_ms], [video, native ? report.rtsp?.first_video_rtp_ms : metrics.first_video_ms], [audio, native ? report.rtsp?.first_audio_rtp_ms : metrics.first_audio_ms]];
    if (!protocol || protocol === 'http-flv' || protocol === 'rtmp') arrivals.push(['首个关键帧到达', metrics.first_keyframe_ms]);
    const show = stages.length > 0 || requests.length > 0 || arrivals.some(([, ms]) => ms != null) ||
        Boolean(observation.failure_stage || observation.failure_code);
    node('sampling-timing').hidden = !show;
    if (!show) return;
    const values = arrivals.map(([label, ms]) => [label, ms == null ? '未观测' : time(ms)]);
    if (observation.failure_stage) values.push(['故障阶段', stageName(observation.failure_stage)]);
    if (observation.failure_code) values.push(['故障代码', observation.failure_code]);
    renderMetrics('timing-metrics', values);
    renderRequestTiming(requests, report.requests_truncated ?? 0);
    const rows = stages.map((stage) => [stageName(stage.name), stage.start_ms, stage.end_ms, stage.end_ms - stage.start_ms]);
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

function renderBufferChart(samples, stalls, elapsed, startMS = 0, intervalLabel = '卡顿区间') {
    const figure = element('figure', '', 'player-buffer-chart');
    figure.append(element('figcaption', stalls.length ? `可播放缓冲 · 阴影为${intervalLabel}` : '可播放缓冲'));
    const svgElement = (tag, attributes, text) => {
        const part = document.createElementNS('http://www.w3.org/2000/svg', tag);
        for (const [name, value] of Object.entries(attributes)) part.setAttribute(name, value);
        if (text != null) part.textContent = text;
        return part;
    };
    const duration = Math.max(1, Math.max(elapsed, ...samples.map((sample) => sample.offset_ms), ...stalls.map((stall) => stall.end_ms)) - startMS);
    const maximum = Math.max(1000, Math.ceil(Math.max(...samples.map((sample) => sample.buffer_ms)) / 1000) * 1000);
    const x = (ms) => 45 + 540 * Math.max(ms - startMS, 0) / duration;
    const y = (ms) => 112 - 94 * ms / maximum;
    const svg = svgElement('svg', { viewBox: '0 0 615 152', role: 'img', 'aria-label': '可播放缓冲，横轴是距采样开始的秒数' });
    for (const value of [0, maximum / 2, maximum]) {
        svg.append(svgElement('line', { x1: 45, y1: y(value), x2: 585, y2: y(value), class: 'grid' }),
            svgElement('text', { x: 40, y: y(value) + 4, 'text-anchor': 'end' }, `${value / 1000}s`));
    }
    for (const stall of stalls) {
        svg.append(svgElement('rect', { x: x(stall.start_ms), y: 18, width: Math.max(0, x(stall.end_ms) - x(stall.start_ms)), height: 94, class: 'stall' }));
    }
    svg.append(svgElement('polyline', { points: samples.map((sample) => `${x(sample.offset_ms).toFixed(1)},${y(sample.buffer_ms).toFixed(1)}`).join(' '), class: 'line' }));
    if (samples.length === 1) {
        svg.append(svgElement('circle', { cx: x(samples[0].offset_ms), cy: y(samples[0].buffer_ms), r: 3, class: 'point' }));
    }
    for (const value of [startMS, startMS + duration / 2, startMS + duration]) {
        svg.append(svgElement('text', { x: x(value), y: 131, 'text-anchor': 'middle' }, `${(value / 1000).toFixed(value ? 1 : 0)}s`));
    }
    svg.append(svgElement('text', { x: 315, y: 149, 'text-anchor': 'middle' }, '距采样开始'));
    figure.append(svg);
    return figure;
}

const playerVideoQualitySources = {
    playback_quality: 'VideoPlaybackQuality', webkit: 'WebKit 视频计数',
    mixed: '混合计数来源', unavailable: '未提供帧质量计数',
};
const videoObservedCount = (value) => value == null ? '未观测' : String(value);
const videoObservedTime = (value) => value == null ? '未观测' : time(value);
const investigationIntervalKind = (item) => item.interval_kind || 'buffering';
const intervalName = (kind) => kind === 'presentation_gap' ? '呈帧回调间隔' : '缓冲卡顿';
const intervalSelection = (kind) => kind === 'presentation_gap' ? selectedPresentationGaps : selectedStalls;
const intervalTitle = (kind, index) => kind === 'presentation_gap' ? `第 ${index} 个呈帧回调间隔` : `第 ${index} 次卡顿（缓冲）`;
const presentationMethodNote = '至少 3 个有效回调媒体时间增量用于估算间隔基线，记录阈值取 1000 ms 与 3 倍间隔基线中的较大值；基线不等同于源流编码帧率。只保留完整落在同一连续前台播放区间内的间隔，并排除采样结束后的部分。';
const presentationBoundaryNote = '回调间隔受内容帧率、浏览器呈现和页面调度影响，不能单凭该记录确认画面冻结或解码故障。与缓冲卡顿重合的时长用于对照，次数与时长不叠加到缓冲卡顿统计。';

function renderPresentationReport(presentation, selectable = false, selectedInterval) {
    const block = element('section', '', 'player-presentation-report');
    const title = element(selectable ? 'h4' : 'h5', selectable ? '呈帧回调间隔' : '补查窗口呈帧回调间隔');
    if (selectable) {
        const heading = element('div', '', 'stall-investigation-heading');
        const button = element('button', '补查呈帧回调间隔', 'secondary');
        button.id = 'investigate-presentation-gap';
        button.type = 'button';
        button.disabled = true;
        button.addEventListener('click', () => { void investigateInterval('presentation_gap'); });
        heading.append(title, button);
        block.append(heading);
    } else block.append(title);
    if (!presentation) block.append(element('p', selectable ? '该记录未采集呈帧回调间隔数据，重新采样可补充。' : '该补查未包含呈帧回调间隔数据，可重新采样后补查。', 'timing-note muted'));
    else {
        const metrics = element('dl', '', 'metrics presentation-metrics');
        for (const [label, value] of [
            ['回调能力', presentation.support === 'supported' ? '支持视频帧回调' : presentation.support === 'unsupported' ? '当前环境不支持视频帧回调' : '未记录'],
            ['观测区间', `${videoObservedTime(presentation.observation_start_ms)}–${videoObservedTime(presentation.observation_end_ms)}`],
            ['回调基线就绪偏移', videoObservedTime(presentation.ready_at_ms)],
            ['前台播放有效覆盖', videoObservedTime(presentation.active_coverage_ms)],
            [selectable ? '间隔次数' : '窗口关联间隔数', videoObservedCount(presentation.count)],
            [selectable ? '间隔累计时长' : '窗口内重合累计时长', videoObservedTime(presentation.total_ms)],
            [selectable ? '最长间隔' : '最长重合时长', videoObservedTime(presentation.max_ms)],
        ]) {
            const item = element('div', '');
            item.append(element('dt', label), element('dd', value));
            metrics.append(item);
        }
        block.append(metrics, element('p', presentationMethodNote, 'timing-note muted'), element('p', presentationBoundaryNote, 'timing-note muted'));
        if (!selectable) block.append(element('p', '窗口统计计算与补查窗口重合的时长；下表保留完整观测到的回调间隔，起止可能超出窗口。与缓冲卡顿重合的时长也按完整间隔保留。', 'timing-note muted'));
        if (presentation.support === 'unsupported') block.append(element('p', '当前环境未提供 requestVideoFrameCallback，呈帧回调间隔保持未观测。', 'timing-note muted'));
        else if (presentation.ready_at_ms == null) block.append(element('p', '尚未完成回调基线预热，无法判断是否存在异常回调间隔。', 'timing-note muted'));
        else if (presentation.active_coverage_ms === 0) block.append(element('p', '预热后没有可用于统计的连续前台播放覆盖区间，间隔统计保持未观测。', 'timing-note muted'));
        if (presentation.dropped_gaps > 0) block.append(element('p', `采集达到上限，另有 ${presentation.dropped_gaps} 条呈帧回调间隔记录未保存，统计可能不完整。`, 'timing-note muted presentation-dropped'));
        const gaps = presentation.gaps || [];
        if (selectable && (!selectedPresentationGaps.has(currentID) || selectedPresentationGaps.get(currentID) > gaps.length)) selectedPresentationGaps.set(currentID, 1);
        if (gaps.length) {
            const shown = gaps.slice(0, 128);
            const table = renderTable([...(selectable ? ['选择'] : []), selectable ? '间隔' : '窗口间隔', '开始偏移', '结束偏移', selectable ? '间隔时长' : '完整回调间隔时长', '回调媒体时间间隔基线', '与缓冲卡顿重合', ...(selectedInterval ? ['与本次补查区间重合'] : [])], shown.map((gap, index) => [
                ...(selectable ? [''] : []), `#${index + 1}`, time(gap.start_ms), time(gap.end_ms), time(gap.duration_ms), time(gap.expected_frame_ms), time(gap.buffering_overlap_ms),
                ...(selectedInterval ? [time(Math.max(0, Math.min(gap.end_ms, selectedInterval.end_ms) - Math.max(gap.start_ms, selectedInterval.start_ms)))] : []),
            ]));
            table.tabIndex = 0;
            table.setAttribute('role', 'region');
            table.setAttribute('aria-label', '呈帧回调间隔，可横向滚动');
            if (selectable) Array.from(table.querySelector('tbody').rows).forEach((row, index) => {
                const choice = element('input', '');
                choice.type = 'radio';
                choice.name = 'presentation-gap-selection';
                choice.className = 'presentation-gap-select';
                choice.value = String(index + 1);
                choice.checked = selectedPresentationGaps.get(currentID) === index + 1;
                choice.setAttribute('aria-label', `选择第 ${index + 1} 个呈帧回调间隔，${time(shown[index].start_ms)} 至 ${time(shown[index].end_ms)}`);
                row.setAttribute('aria-selected', String(choice.checked));
                choice.addEventListener('change', () => {
                    selectedPresentationGaps.set(currentID, index + 1);
                    selectedIntervalKinds.set(currentID, 'presentation_gap');
                    for (const other of table.querySelector('tbody').rows) other.setAttribute('aria-selected', String(other === row));
                    renderStallInvestigations(currentReport);
                });
                row.cells[0].append(choice);
            });
            block.append(table);
            if (gaps.length > shown.length) block.append(element('p', `页面仅展示前 ${shown.length} 个间隔，另有 ${gaps.length - shown.length} 个已保存间隔可在报告 JSON 或证据包中查看。`, 'timing-note muted'));
        } else block.append(element('p', presentation.count === 0 ? '有效覆盖内未记录到符合阈值的呈帧回调间隔。' : '当前记录未包含可展示的呈帧回调间隔。', 'timing-note muted'));
    }
    if (selectable) {
        const status = element('p', '', 'timing-note muted');
        status.id = 'presentation-investigation-status';
        status.setAttribute('role', 'status');
        block.append(status);
    }
    return block;
}

function renderPlayerVideo(video) {
    if (!video) {
        renderMetrics('player-video-metrics', []);
        node('player-video-note').textContent = '该记录未采集视频呈帧与丢帧数据。';
        return;
    }
    const ratio = video.dropped_ratio == null ? '未观测' : `${(video.dropped_ratio * 100).toFixed(2)}%`;
    renderMetrics('player-video-metrics', [
        ['帧质量计数来源', playerVideoQualitySources[video.quality_source] || '未提供帧质量计数'],
        ['视频帧增量', videoObservedCount(video.total_frames)],
        ['丢帧增量', videoObservedCount(video.dropped_frames)],
        ['丢帧比例', ratio],
        ['呈帧增量', videoObservedCount(video.presented_frames)],
        ['最大呈帧回调间隔', videoObservedTime(video.max_frame_gap_ms)],
        ['计数重置', videoObservedCount(video.counter_resets)],
        ['视频样本数量', videoObservedCount(video.sample_count)],
    ]);
    node('player-video-note').textContent = '帧增量按连续前台播放样本的差值汇总，暂停、跳转、后台和采样结束后的记录不计入。呈帧与回调间隔来自视频帧回调，低帧率内容和页面调度也会影响间隔，需结合缓冲、收流和媒体帧率一起判断。';
}

function renderVideoWindowSamples(samples) {
    const observed = (samples || []).filter((sample) => sample.video && [sample.video.total_frames, sample.video.dropped_frames,
        sample.video.presented_frames, sample.video.frame_age_ms].some((value) => value != null));
    if (!observed.length) return null;
    const block = element('details', '', 'video-window-samples');
    block.append(element('summary', `窗口视频样本 · ${observed.length} 条`),
        element('p', '此处显示采样时的原始累计计数；距最近呈帧回调的时间受播放器状态和页面调度影响。', 'timing-note muted'),
        renderTable(['采样偏移', '播放状态', '视频帧计数', '丢帧计数', '呈帧计数', '距最近呈帧回调'], observed.map((sample) => [
            time(sample.offset_ms), [sample.paused ? '暂停' : '', sample.seeking ? '跳转' : '', sample.hidden ? '后台' : ''].filter(Boolean).join(' / ') || '前台',
            videoObservedCount(sample.video.total_frames), videoObservedCount(sample.video.dropped_frames),
            videoObservedCount(sample.video.presented_frames), videoObservedTime(sample.video.frame_age_ms),
        ])));
    return block;
}

const schedulingRowLimit = 200;
const schedulingMethodNote = '调度区间按前台播放和观测窗口裁切，区间数与时长表示裁切后的有效片段。定时器每 250 ms 检查，仅采集原始延后 ≥100 ms 的记录；裁切后的区间可能更短。';
const schedulingBoundaryNote = '长任务和定时器延后分别观测。页面负载、系统调度等都可能影响结果，不能单独证明 CPU 瓶颈或解码故障；与异常区间重合用于安排进一步验证。';
const schedulingSupportLabels = { supported: '支持 Long Tasks API', unsupported: '当前环境不支持 Long Tasks API' };

function schedulingMetricsList(metrics) {
    const list = element('dl', '', 'metrics scheduling-metrics');
    for (const [label, value] of [['区间数', videoObservedCount(metrics?.count)], ['区间累计时长', videoObservedTime(metrics?.total_ms)], ['最长区间', videoObservedTime(metrics?.max_ms)]]) {
        const item = element('div', '');
        item.append(element('dt', label), element('dd', value));
        list.append(item);
    }
    return list;
}

function renderPlayerScheduling(scheduling, stall, intervalKind = 'buffering') {
    const block = element('section', '', 'player-scheduling-report');
    block.append(element(stall ? 'h5' : 'h4', stall ? '补查窗口页面调度' : '页面调度观测'));
    if (!scheduling) {
        block.append(element('p', stall ? '该补查未包含页面调度数据，可重新采样后补查。' : '该记录未采集页面调度数据，重新采样可补充长任务和定时器延后证据。', 'timing-note muted'));
        return block;
    }
    const coverage = element('dl', '', 'metrics scheduling-coverage');
    for (const [label, value] of [
        ['长任务接口', schedulingSupportLabels[scheduling.longtask_support] || '接口支持状态未记录'],
        ['观测区间', `${videoObservedTime(scheduling.observation_start_ms)}–${videoObservedTime(scheduling.observation_end_ms)}`],
        ['前台播放有效覆盖', videoObservedTime(scheduling.active_coverage_ms)],
    ]) {
        const item = element('div', '');
        item.append(element('dt', label), element('dd', value));
        coverage.append(item);
    }
    block.append(coverage, element('p', schedulingMethodNote, 'timing-note muted'), element('p', schedulingBoundaryNote, 'timing-note muted'));
    if (scheduling.longtask_support === 'unsupported') block.append(element('p', '长任务未观测不代表没有长任务，定时器延后仍单独记录。', 'timing-note muted scheduling-capability-note'));
    if (scheduling.active_coverage_ms === 0) block.append(element('p', '本记录没有可用于统计的前台播放覆盖区间，异常区间数保持未观测。', 'timing-note muted scheduling-coverage-note'));
    const longTasks = scheduling.longtask_support === 'supported' ? scheduling.long_tasks : null;
    for (const [kind, title, metrics] of [['long_task', '浏览器长任务', longTasks], ['timer_delay', '定时器延后（原始延后 ≥100 ms）', scheduling.timer_delays]]) {
        const group = element('section', '', 'player-scheduling-kind');
        group.dataset.kind = kind;
        group.append(element(stall ? 'h6' : 'h5', title), schedulingMetricsList(metrics));
        block.append(group);
    }
    if (scheduling.dropped_long_tasks > 0 || scheduling.dropped_timer_delays > 0) block.append(element('p', `采集上限外未记录：长任务 ${scheduling.dropped_long_tasks || 0} 条，定时器延后 ${scheduling.dropped_timer_delays || 0} 条。当前统计可能不完整。`, 'timing-note muted scheduling-dropped'));
    const intervals = [
        ...(longTasks?.intervals || []).map((interval) => ({ ...interval, kind: '浏览器长任务' })),
        ...(scheduling.timer_delays?.intervals || []).map((interval) => ({ ...interval, kind: '定时器延后' })),
    ].sort((left, right) => left.start_ms - right.start_ms || left.end_ms - right.end_ms);
    if (!intervals.length) block.append(element('p', '当前记录未包含可展示的调度区间。', 'timing-note muted'));
    else {
        const rows = intervals.slice(0, schedulingRowLimit).map((interval) => {
            const row = [interval.kind, interval.start_ms, interval.end_ms, interval.end_ms - interval.start_ms];
            if (stall) row.push(Math.max(0, Math.min(interval.end_ms, stall.end_ms) - Math.max(interval.start_ms, stall.start_ms)));
            return row;
        });
        const table = renderTable(['类型', '开始偏移 (ms)', '结束偏移 (ms)', '有效区间时长 (ms)', ...(stall ? [intervalKind === 'presentation_gap' ? '与呈帧回调间隔重合 (ms)' : '与本次卡顿重合 (ms)'] : [])], rows);
        table.tabIndex = 0;
        table.setAttribute('role', 'region');
        table.setAttribute('aria-label', '页面调度有效区间，可横向滚动');
        block.append(table);
        if (intervals.length > rows.length) block.append(element('p', `页面仅展示前 ${rows.length} 个有效区间，另有 ${intervals.length - rows.length} 个已保存区间可在报告 JSON 或证据包中查看。`, 'timing-note muted scheduling-omitted'));
    }
    return block;
}

function renderPlayerReport(player, elapsed) {
    node('player-report').hidden = !player;
    if (!player) return;
    const lastError = (player.events || []).filter((event) => event.kind === 'error').at(-1);
    const state = player.state === 'error' ? playbackErrors[lastError?.code] || playerStates.error : playerStates[player.state] || player.state;
    const played = player.first_playing_ms != null;
    const screen = player.first_screen_method === 'loadeddata_estimate' ? '首屏（加载首帧估算）' : '首屏';
    const stallCount = player.stall_threshold_ms > 0 ? `卡顿次数（≥ ${player.stall_threshold_ms} ms）` : '卡顿次数';
    const values = [
        ['状态', player.state === 'complete' && !played ? '采集结束，未开始播放' : state],
        ['来源', playerSources[player.source] || player.source],
        [screen, player.first_screen_ms == null ? '未观测' : time(player.first_screen_ms)],
        ['开始播放', played ? time(player.first_playing_ms) : '未观测'],
        [stallCount, played ? player.stall_count : '未观测'],
        ['累计卡顿', played ? time(player.total_stall_ms) : '未观测'],
        ['最长卡顿', played ? time(player.max_stall_ms) : '未观测'],
        ['播放观察时长', played ? time(player.active_ms) : '未观测'],
    ];
    if (player.min_buffer_ms != null) values.push(['最小可播放缓冲', time(player.min_buffer_ms)]);
    renderMetrics('player-metrics', values);
    const threshold = player.stall_threshold_ms > 0 ? `至少 ${player.stall_threshold_ms} ms 的` : '';
    node('player-method').textContent = `卡顿统计从开始播放到采样结束，计入${threshold}等待。暂停、跳转和后台期间停止计时。正常结束后可短暂补记首屏。`;
    renderPlayerVideo(player.video);
    node('player-presentation-report').replaceChildren(renderPresentationReport(player.presentation, true));
    node('player-scheduling-report').replaceChildren(renderPlayerScheduling(player.scheduling));
    const stalls = player.stalls || [];
    const samples = player.buffer_samples || [];
    node('player-buffer').replaceChildren(...(samples.length ? [renderBufferChart(samples, stalls, elapsed)] : []));
    renderStallSelection(stalls);
}

const playerNetworkRowLimit = 200;
const playerNetworkKinds = { playlist: '播放列表', media: '媒体分片', init: '初始化段', key: '密钥', auxiliary: '辅助资源' };
const playerNetworkOutcomes = { complete: '完成', http_error: '上游 HTTP 错误', transport_error: '传输错误', canceled: '已取消', timeout: '超时', incomplete: '未完成' };
const playerNetworkOffset = (value) => value == null ? '未观测' : String(value);
const playerNetworkBoundaryNote = '本地代理记录实际播放连接的上游请求：响应头、首段响应体和读取字节均在代理侧观测。结束偏移可能包含向播放器写出数据的时间；写出完成不代表浏览器已接收、解码或可播放。';
const playerNetworkTimingNote = '偏移从后端采样开始计时；未完成请求的结束列标为“截至”，表示快照时刻。缺失字段显示“未观测”，0 为已观测值。';
const playerNetworkPhases = [['dns', 'DNS'], ['connect', 'TCP 建连'], ['tls', 'TLS 握手'], ['request_send', '发送请求'], ['response_first_byte', '等待首字节'], ['response_headers', '接收响应头'], ['receive', '上游响应体阶段']];
const playerNetworkFailureStages = { ...stageLabels, receive: '上游响应体阶段', proxy_write: '向播放器写出', proxy_limit: '代理资源限制', playlist: '播放列表处理' };
const playerNetworkFailureCodes = { dns_error: 'DNS 失败', tls_error: 'TLS 失败', connection_refused: '连接被拒绝', timeout: '超时', canceled: '已取消', transport_error: '传输错误', redirect_error: '重定向失败', http_error: 'HTTP 错误', limit_exceeded: '达到限制', invalid_playlist: '播放列表无效', proxy_write_error: '向播放器写出失败' };
const playerNetworkPhaseNote = '阶段耗时来自本地代理的上游观测，TTFB 从本跳开始计时。上游响应体阶段从响应头起至上游读取结束，可能受下游写出影响，不代表浏览器接收完成。未完成请求仅展示已记录部分。复用连接或未触发相关事件时，DNS/TCP/TLS 保持未观测；同阶段多次观测用 / 分列。';

function playerNetworkFailureLabel(stage, code) {
    return [[stage, playerNetworkFailureStages], [code, playerNetworkFailureCodes]].filter(([value]) => value).map(([value, labels]) => labels[value] ? `${labels[value]} [${value}]` : value).join(' · ') || '未记录';
}

function playerNetworkPhaseDuration(stages, name) {
    const observed = (stages || []).slice(0, 16).filter((stage) => stage.name === name);
    return observed.length ? observed.map((stage) => String(stage.end_ms - stage.start_ms)).join(' / ') : '未观测';
}

function renderPlayerNetworkPhaseDetails(request, context) {
    const details = element('details', '', 'player-network-phase-detail');
    details.dataset.requestNumber = request.number;
    const hops = (request.hops || []).slice(0, 8);
    const chain = hops.map((hop) => hop.http_status > 0 ? String(hop.http_status) : '未观测').join(' → ');
    details.append(element('summary', `#${request.number} · ${request.resource_id || '未标注资源'} · ${hops.length} 跳${chain ? ` · HTTP ${chain}` : ''}`));
    let rendered = false;
    const render = () => {
        if (rendered) return;
        rendered = true;
        if (request.failure_stage || request.failure_code) details.append(element('p', `请求终止阶段 / 原因：${playerNetworkFailureLabel(request.failure_stage, request.failure_code)}`, 'timing-note network-terminal-failure'));
        if (request.dropped_hops > 0) details.append(element('p', `逐跳记录达到采集上限或元数据预算，另有 ${request.dropped_hops} 跳未保存，重定向链可能不完整。`, 'timing-note network-hop-dropped'));
        if (!hops.length) details.append(element('p', '此请求未包含可展示的逐跳阶段记录。', 'timing-note muted'));
        else {
            const table = renderTable(['跳', '上游 HTTP', '开始偏移 (ms)', '结束 / 已记录至偏移 (ms)', '连接复用', 'TTFB (ms)', ...playerNetworkPhases.map(([, label]) => `${label} (ms)`), '本跳故障阶段 / 原因'], hops.map((hop) => [
                `#${hop.number}`, hop.http_status > 0 ? String(hop.http_status) : '未观测', playerNetworkOffset(hop.start_ms), playerNetworkOffset(hop.end_ms),
                hop.connection_reused == null ? '未观测' : hop.connection_reused ? '已复用' : '未复用', playerNetworkOffset(hop.ttfb_ms),
                ...playerNetworkPhases.map(([name]) => playerNetworkPhaseDuration(hop.stages, name)), playerNetworkFailureLabel(hop.failure_stage, hop.failure_code),
            ]));
            table.tabIndex = 0;
            table.setAttribute('role', 'region');
            table.setAttribute('aria-label', `请求 ${request.number} 的逐跳阶段耗时，可横向滚动`);
            details.append(table);
        }
        if ((request.hops || []).length > hops.length) details.append(element('p', `页面仅展示前 ${hops.length} 跳，另有 ${request.hops.length - hops.length} 跳已保存记录可在报告 JSON 或证据包中查看。`, 'timing-note muted'));
        if (hops.some((hop) => hop.stages?.length > 16)) details.append(element('p', '页面仅展示每跳前 16 条阶段观测，其余已保存记录可在报告 JSON 或证据包中查看。', 'timing-note muted'));
    };
    const key = `${currentID}/${context}/${request.number}`;
    details.addEventListener('toggle', () => {
        if (details.open) { openPlayerNetworkDetails.add(key); render(); }
        else openPlayerNetworkDetails.delete(key);
    });
    if (openPlayerNetworkDetails.has(key)) { render(); details.open = true; }
    return details;
}

function renderPlayerNetworkRequests(requests, context = 'global') {
    const block = element('div', '', 'player-network-requests');
    if (!requests?.length) {
        block.append(element('p', '当前记录未包含实际播放请求。', 'timing-note muted'));
        return block;
    }
    const shown = requests.slice(0, playerNetworkRowLimit);
    const table = renderTable(['请求', '类型', '资源标识', '列表序号', '方法', '开始偏移 (ms)', '上游响应头偏移 (ms)', '上游首段响应体偏移 (ms)', '代理结束 / 截至偏移 (ms)', '上游 HTTP', '上游读取字节', '结果'], shown.map((request) => [
        `#${request.number}`, (playerNetworkKinds[request.kind] || request.kind || '未标注') + (request.rendition === 'audio' ? ' · 独立音轨' : ''), request.resource_id || '未标注', request.playlist_sequence ?? '—', request.method || '未标注',
        playerNetworkOffset(request.start_ms), playerNetworkOffset(request.header_ms), playerNetworkOffset(request.first_body_ms),
        `${playerNetworkOffset(request.end_ms)}${request.outcome === 'incomplete' ? '（截至）' : ''}`,
        request.http_status > 0 ? String(request.http_status) : '未观测', playerNetworkOffset(request.bytes), playerNetworkOutcomes[request.outcome] || request.outcome || '未标注',
    ]));
    table.tabIndex = 0;
    table.setAttribute('role', 'region');
    table.setAttribute('aria-label', 'HLS 实际播放请求记录，可横向滚动');
    block.append(table);
    const detailed = shown.filter((request) => request.hops?.length || request.dropped_hops > 0 || request.failure_stage || request.failure_code);
    if (detailed.length) {
        const phases = element('section', '', 'player-network-phase-list');
        phases.append(element(context === 'global' ? 'h4' : 'h6', '逐跳阶段耗时'), element('p', playerNetworkPhaseNote, 'timing-note muted'));
        for (const request of detailed) phases.append(renderPlayerNetworkPhaseDetails(request, context));
        if (detailed.length < shown.length) phases.append(element('p', `另有 ${shown.length - detailed.length} 条已展示请求未包含逐跳阶段记录。`, 'timing-note muted network-phase-unobserved'));
        block.append(phases);
    } else block.append(element('p', '当前请求记录未包含逐跳阶段耗时，重新采样可补充。', 'timing-note muted network-phase-unobserved'));
    if (requests.length > shown.length) block.append(element('p', `页面仅展示前 ${shown.length} 条，另有 ${requests.length - shown.length} 条已保存记录可在报告 JSON 或证据包中查看。`, 'timing-note muted player-network-omitted'));
    return block;
}

function renderPlayerNetwork(report) {
    const network = report.player_network;
    const visible = !!network || (report.manifest.protocol === 'hls' && !!report.player);
    node('player-network-report').hidden = !visible;
    if (!visible) return;
    node('player-network-note').textContent = playerNetworkBoundaryNote;
    node('player-network-timing-note').textContent = playerNetworkTimingNote;
    if (!network) {
        renderMetrics('player-network-metrics', []);
        node('player-network-table').replaceChildren(element('p', '该记录未采集实际播放请求，重新采样可补充代理侧请求证据。', 'timing-note muted'));
        node('player-network-dropped').textContent = '';
        node('player-network-dropped').hidden = true;
        return;
    }
    renderMetrics('player-network-metrics', [
        ['观测边界', network.boundary === 'upstream_proxy' ? '本地代理上游侧' : network.boundary || '未标注'],
        ['记录状态', network.complete ? '已结束' : '部分请求快照'],
        ['已保存请求', (network.requests || []).length],
        ['上限外丢弃请求', network.dropped_requests ?? '未观测'],
    ]);
    node('player-network-table').replaceChildren(renderPlayerNetworkRequests(network.requests || []));
    const notices = [];
    if (network.dropped_requests > 0) notices.push(`采集达到上限，另有 ${network.dropped_requests} 条请求未写入本记录，不能按完整请求历史分析。`);
    if (network.dropped_hops > 0) notices.push(`逐跳记录达到采集上限或元数据预算，共有 ${network.dropped_hops} 跳未保存。`);
    node('player-network-dropped').hidden = !notices.length;
    node('player-network-dropped').textContent = notices.join(' ');
}

function renderStallSelection(stalls) {
    if (!stalls.length) {
        node('player-stalls').replaceChildren();
        delete node('player-stalls').dataset.stalls;
        return;
    }
    const signature = JSON.stringify({ id: currentID, stalls });
    if (node('player-stalls').dataset.stalls === signature) return;
    node('player-stalls').dataset.stalls = signature;
    if (!selectedStalls.has(currentID) || selectedStalls.get(currentID) > stalls.length) selectedStalls.set(currentID, 1);
    const table = renderTable(['选择', '卡顿', '开始偏移', '结束偏移', '耗时'], stalls.map((stall, index) => ['', `#${index + 1}`, time(stall.start_ms), time(stall.end_ms), time(stall.duration_ms)]));
    Array.from(table.querySelector('tbody').rows).forEach((row, index) => {
        const choice = element('input', '');
        choice.type = 'radio';
        choice.name = 'stall-selection';
        choice.className = 'stall-select';
        choice.value = String(index + 1);
        choice.checked = selectedStalls.get(currentID) === index + 1;
        choice.setAttribute('aria-label', `选择第 ${index + 1} 次卡顿，${time(stalls[index].start_ms)} 至 ${time(stalls[index].end_ms)}`);
        row.setAttribute('aria-selected', String(choice.checked));
        choice.addEventListener('change', () => {
            selectedStalls.set(currentID, index + 1);
            selectedIntervalKinds.set(currentID, 'buffering');
            for (const other of table.querySelector('tbody').rows) other.setAttribute('aria-selected', String(other === row));
            renderStallInvestigations(currentReport);
        });
        row.cells[0].append(choice);
    });
    node('player-stalls').replaceChildren(element('h4', '缓冲卡顿区间'), table);
}

function investigationAssetURL(item, asset) {
    const match = /^stalls\/(S\d{3})\/(clip\.mp4|before\.jpg|during\.jpg|after\.jpg)$/.exec(asset.name || '');
    if (!match || match[1] !== item.id || (asset.kind === 'clip') !== (match[2] === 'clip.mp4')) return null;
    return apiRoot + runPath(currentID) + '/stall-assets/' + encodeURIComponent(item.id) + '/' + match[2];
}

function renderInvestigationMedia(item) {
    const block = element('section', '', 'stall-media');
    const media = item.media || {};
    block.append(element('h5', '异常附近的源流片段与截图'), element('p', investigationMediaStates[media.status] || '等待提取源流媒体。', 'timing-note muted'));
    if (media.reason) block.append(element('p', media.reason, 'timing-note muted'));
    if (media.alignment) block.append(element('p', `媒体定位方式 ${investigationAlignments[media.alignment] || media.alignment}${media.start_ms != null ? ` · 源流媒体时间 ${time(media.start_ms)}–${time(media.end_ms)}` : ''}`, 'timing-note muted'));
    block.append(element('p', '片段与截图来自采样源流，用于复核媒体内容；原拉流等待和播放器停顿保留在时序证据中。', 'timing-note muted'));
    if (investigationIntervalKind(item) === 'presentation_gap') block.append(element('p', '同期源流片段和截图供内容对照，不能重现或证明本机画面冻结。', 'timing-note muted'));
    const images = element('div', '', 'stall-screenshots');
    for (const asset of media.assets || []) {
        const url = investigationAssetURL(item, asset);
        if (!url) continue;
        const label = asset.name.endsWith('/before.jpg') ? '异常前源流截图' : asset.name.endsWith('/after.jpg') ? '异常后源流截图' : '异常附近源流截图';
        const link = element('a', asset.kind === 'clip' ? `下载源流片段 · ${bytes(asset.bytes)}` : '下载截图');
        link.href = url;
        link.download = asset.name.split('/').at(-1);
        if (asset.kind === 'clip') {
            const video = element('video', '');
            video.controls = true;
            video.playsInline = true;
            video.preload = 'none';
            video.src = url;
            video.setAttribute('aria-label', '异常附近的源流媒体片段');
            block.append(video, link);
        } else {
            const figure = element('figure', '');
            const image = element('img', '');
            image.src = url;
            image.alt = label;
            image.loading = 'lazy';
            const caption = element('figcaption', `${label}${asset.media_time_ms != null ? ` · 源流媒体时间 ${time(asset.media_time_ms)}` : ''}`);
            caption.append(document.createTextNode(' · '), link);
            figure.append(image, caption);
            images.append(figure);
        }
    }
    if (images.childElementCount) block.append(images);
    return block;
}

const stallDirectionStates = { observed: '已观测', candidate: '待验证', insufficient: '证据不足' };

function renderStallAssessment(item) {
    const block = element('section', '', 'stall-assessment');
    block.append(element('h5', '分层排查方向'));
    const assessment = item.assessment;
    if (!assessment) {
        block.append(element('p', ['queued', 'running'].includes(item.status) ? '补查完成后展示分层排查方向。' : '该记录尚未生成排查分层。重新生成建议可补充排查分层。', 'timing-note muted'));
        return block;
    }
    if (assessment.conclusion) block.append(element('p', assessment.conclusion, 'stall-assessment-conclusion'));
    block.append(element('p', '以下方向根据已保存的证据整理，用于安排验证步骤。', 'timing-note muted'));
    const evidenceIDs = new Set([...(item.correlation?.findings || []), ...(assessment.findings || [])].map((finding) => finding.id));
    if (!assessment.directions?.length) block.append(element('p', '当前证据尚未形成排查方向。', 'timing-note muted'));
    for (const direction of assessment.directions || []) {
        const section = element('section', '', 'stall-direction');
        section.dataset.state = direction.status;
        const title = element('h6', direction.title || '待核对方向');
        title.append(element('span', stallDirectionStates[direction.status] || '待核对', 'status direction-state'));
        section.append(title);
        if (direction.message) section.append(element('p', direction.message));
        if (direction.evidence_ids?.length) {
            const evidence = element('p', '关联证据 ', 'stall-direction-evidence');
            for (const id of direction.evidence_ids) {
                if (evidenceIDs.has(id)) {
                    const link = element('a', id);
                    link.href = '#case-' + encodeURIComponent(id);
                    link.addEventListener('click', (event) => { event.preventDefault(); showFinding('case-' + id); });
                    evidence.append(link, document.createTextNode(' '));
                } else evidence.append(document.createTextNode(`${id}（未找到记录） `));
            }
            section.append(evidence);
        }
        if (direction.next_checks?.length) {
            const checks = element('ul', '', 'stall-next-checks');
            for (const check of direction.next_checks) checks.append(element('li', check));
            section.append(element('p', '建议验证', 'timing-note muted'), checks);
        }
        block.append(section);
    }
    if (assessment.limitations?.length) {
        const limitations = element('ul', '', 'stall-assessment-limitations');
        for (const limitation of assessment.limitations) limitations.append(element('li', limitation));
        block.append(element('h6', '证据限制'), limitations);
    }
    if (assessment.version) block.append(element('p', `排查规则 ${assessment.version}`, 'timing-note muted'));
    return block;
}

function renderInvestigationModel(item) {
    const block = element('section', '', 'stall-model');
    block.append(element('h5', '模型建议（时序证据）'), element('p', '模型依据关联的接收间隔、缓冲与关键帧记录提出待验证建议，源流片段和截图供复核。', 'timing-note muted'));
    if (item.explanation) {
        block.append(element('p', `解读来源 ${modelLabel(item.explanation)}`, 'model-source muted'), element('p', item.explanation.summary));
        const list = element('ul', '');
        for (const hypothesis of item.explanation.hypotheses || []) {
            const row = element('li', '');
            row.append(element('strong', `待验证 · ${hypothesis.claim} `));
            for (const id of hypothesis.evidence_ids || []) {
                const link = element('a', `${id} `);
                link.href = '#case-' + encodeURIComponent(id);
                link.addEventListener('click', (event) => { event.preventDefault(); showFinding('case-' + id); });
                row.append(link);
            }
            row.append(element('p', hypothesis.next_check));
            list.append(row);
        }
        if (list.childElementCount) block.append(list);
    } else block.append(element('p', investigationModelStates[item.model_state] || '等待模型建议。', 'timing-note muted'));
    if (item.model_error) block.append(element('p', item.model_error, 'timing-note muted'));
    if (item.status !== 'queued' && item.status !== 'running') {
        const button = element('button', !item.assessment ? '生成排查与模型建议' : item.explanation ? '用当前模型重新生成建议' : '生成模型建议', 'secondary');
        button.type = 'button';
        button.dataset.stallExplain = item.id;
        button.disabled = pendingStallExplanations.has(`${currentID}/${item.id}`);
        const runID = currentID;
        button.addEventListener('click', async () => {
            const key = `${runID}/${item.id}`;
            pendingStallExplanations.add(key);
            button.disabled = true;
            try {
                await api(runPath(runID) + '/stalls/' + encodeURIComponent(item.id) + '/explain', { method: 'POST' });
                if (currentID === runID) await refreshDetail(runID);
            } catch (err) { if (currentID === runID) message(err.message, true); }
            finally { pendingStallExplanations.delete(key); if (currentID === runID) void refreshDetail(runID); }
        });
        block.append(button);
    }
    return block;
}

function renderInvestigation(item) {
    const card = element('details', '', 'stall-investigation');
    card.id = 'investigation-' + item.id;
    card.dataset.investigation = item.id;
    card.dataset.signature = JSON.stringify(item);
    const kind = investigationIntervalKind(item);
    const summary = element('summary', `${item.id} · ${intervalTitle(kind, item.stall_index)}`);
    summary.append(element('span', investigationStates[item.status] || item.status, 'status'));
    card.append(summary);
    if (['queued', 'running'].includes(item.status)) {
        const stop = element('button', '停止补查', 'secondary');
        stop.type = 'button';
        stop.dataset.stallStop = item.id;
        const runID = currentID;
        stop.disabled = pendingStallStops.has(`${runID}/${item.id}`);
        stop.addEventListener('click', async () => {
            const key = `${runID}/${item.id}`;
            pendingStallStops.add(key);
            stop.disabled = true;
            try {
                await api(runPath(runID) + '/stalls/' + encodeURIComponent(item.id) + '/stop', { method: 'POST' });
                if (currentID === runID) { message('已请求停止补查，正在保留已有证据。'); await refreshDetail(runID); }
            } catch (err) { if (currentID === runID) message(err.message, true); }
            finally { pendingStallStops.delete(key); if (currentID === runID) void refreshDetail(runID); }
        });
        card.append(stop);
    }
    const c = item.correlation || {};
    card.append(element('p', `${intervalName(kind)} ${time(item.stall.start_ms)}–${time(item.stall.end_ms)} · ${time(item.stall.duration_ms)}；补查窗口 ${time(c.window_start_ms)}–${time(c.window_end_ms)}`, 'timing-note muted'));
    if (kind === 'presentation_gap') card.append(element('p', '本案例分析呈帧回调间隔，不代表已确认画面冻结；与缓冲卡顿重合的部分仍保留在独立统计中。', 'timing-note muted'));
    card.append(element('p', `来源 ${playerSources[c.source] || c.source || '未记录'}${c.source === 'independent_pull' ? '，采样连接与播放器连接独立，关联结果供对照。' : ''}`, 'timing-note muted'));
    for (const note of c.notes || []) card.append(element('p', note, 'timing-note muted'));
    card.append(renderStallAssessment(item));
    if (c.buffer_samples?.length) card.append(renderBufferChart(c.buffer_samples, [item.stall], c.window_end_ms, c.window_start_ms, kind === 'presentation_gap' ? '呈帧回调间隔' : '卡顿区间'));
    const videoSamples = renderVideoWindowSamples(c.buffer_samples);
    if (videoSamples) card.append(videoSamples);
    card.append(renderPlayerScheduling(c.scheduling, item.stall, kind), renderPresentationReport(c.presentation, false, item.stall));
    card.append(element('h5', '关联收流间隔'));
    if (c.gaps?.length) card.append(renderTable(['媒体', '开始偏移', '结束偏移', '间隔'], c.gaps.map((gap) => [tagLabels[gap.kind] || gap.kind, time(gap.start_ms), time(gap.end_ms), time(gap.duration_ms)])));
    else card.append(element('p', '窗口内没有可关联的音视频接收间隔记录。', 'timing-note muted'));
    card.append(element('h5', '关键帧记录'));
    if (c.keyframes?.length) card.append(renderEvidence(currentReport.manifest.protocol, c.keyframes));
    else card.append(element('p', '当前证据未识别到可关联的关键帧，不能据此判断关键帧是否缺失。', 'timing-note muted'));
    if (c.requests?.length) card.append(element('h5', '关联采样 HTTP 请求'), renderTable(['请求', '对象', 'HTTP', '开始偏移', '响应头偏移', 'TTFB'], c.requests.map((request) => [`#${request.number}`, requestKinds[request.kind] || request.kind || '媒体请求', request.http_status || '—', time(request.start_ms), time(request.end_ms), time(request.ttfb_ms)])));
    if (c.player_requests?.length || currentReport.manifest.protocol === 'hls') {
        const requests = element('section', '', 'stall-player-requests');
        requests.append(element('h5', '关联实际播放请求'), element('p', playerNetworkBoundaryNote, 'timing-note muted'), element('p', playerNetworkTimingNote, 'timing-note muted'), renderPlayerNetworkRequests(c.player_requests || [], item.id));
        card.append(requests);
    }
    card.append(element('h5', '关联证据'));
    for (const finding of c.findings || []) {
        const evidence = element('details', '', 'finding');
        evidence.id = 'case-' + finding.id;
        evidence.append(element('summary', `${finding.id} · ${finding.message}`));
        evidence.append(element('p', `${time(finding.start_ms)}–${time(finding.end_ms)}`, 'timing-note muted'));
        if (finding.next_check) evidence.append(element('p', finding.next_check));
        if (finding.evidence?.length) evidence.append(renderEvidence(currentReport.manifest.protocol, finding.evidence));
        card.append(evidence);
    }
    if (item.assessment?.findings?.length) {
        card.append(element('h5', '分层判断证据'));
        const correlationIDs = new Set((c.findings || []).map((finding) => finding.id));
        for (const finding of item.assessment.findings) {
            if (correlationIDs.has(finding.id)) continue;
            correlationIDs.add(finding.id);
            const evidence = element('details', '', 'finding assessment-finding');
            evidence.id = 'case-' + finding.id;
            evidence.append(element('summary', `${finding.id} · ${finding.message}`),
                element('p', `${time(finding.start_ms)}–${time(finding.end_ms)}`, 'timing-note muted'));
            if (finding.next_check) evidence.append(element('p', finding.next_check));
            if (finding.evidence?.length) evidence.append(renderEvidence(currentReport.manifest.protocol, finding.evidence));
            card.append(evidence);
        }
    }
    card.append(renderInvestigationMedia(item), renderInvestigationModel(item));
    return card;
}

function renderInvestigationAction(report, kind, intervals, buttonID, statusID) {
    const button = node(buttonID);
    const status = node(statusID);
    if (!button || !status) return;
    const items = report?.stall_investigations || [];
    const selected = intervalSelection(kind).get(currentID) || 1;
    const existing = items.find((item) => investigationIntervalKind(item) === kind && item.stall_index === selected);
    const playerPending = activePlayer?.id === currentID || Array.from(finalizingPlayers).some((player) => player.id === currentID) || ['recording', 'awaiting'].includes(report?.player?.state);
    const busy = pendingStallRequests.has(currentID);
    const retry = existing && !['queued', 'running'].includes(existing.status) && !existing.media?.assets?.length;
    const full = !existing && items.length >= 8;
    button.disabled = !intervals.length || currentStatus === 'running' || playerPending || busy || full || Boolean(existing && !retry);
    const label = kind === 'presentation_gap' ? `第 ${selected} 个呈帧回调间隔` : `第 ${selected} 次卡顿`;
    button.textContent = !intervals.length ? (kind === 'presentation_gap' ? '暂无可补查回调间隔' : '暂无缓冲卡顿区间') : busy ? '正在提交…' : retry ? `重新补查${label}` : existing ? (investigationStates[existing.status] || '已补查') : `补查${label}`;
    status.textContent = !intervals.length ? (kind === 'presentation_gap' ? '当前没有可补查的呈帧回调间隔。' : '当前没有可补查的缓冲卡顿区间。') : full ? '本报告已达到 8 个补查案例上限，可查看或重新补查已有案例。' : playerPending || currentStatus === 'running' ? `采样和播放器记录保存完成后，可以选择${intervalName(kind)}补查证据。` : existing ? `${label} ${investigationStates[existing.status] || existing.status}。` : `已选择${label}。补查会关联时序并提取异常附近的源流媒体。`;
}

function renderStallInvestigations(report) {
    const stalls = report?.player?.stalls || [];
    const gaps = report?.player?.presentation?.gaps || [];
    const items = report?.stall_investigations || [];
    renderInvestigationAction(report, 'buffering', stalls, 'investigate-stall', 'stall-investigation-status');
    renderInvestigationAction(report, 'presentation_gap', gaps, 'investigate-presentation-gap', 'presentation-investigation-status');
    node('stall-investigations').hidden = !stalls.length && !gaps.length && !items.length;
    if (!stalls.length && !gaps.length && !items.length) { node('stall-investigation-results').replaceChildren(); return; }
    let kind = selectedIntervalKinds.get(currentID) || (stalls.length ? 'buffering' : 'presentation_gap');
    if (kind === 'buffering' && !stalls.length && gaps.length) kind = 'presentation_gap';
    if (kind === 'presentation_gap' && !gaps.length && stalls.length) kind = 'buffering';
    selectedIntervalKinds.set(currentID, kind);
    const selected = intervalSelection(kind).get(currentID) || 1;
    const container = node('stall-investigation-results');
    const old = new Map(Array.from(container.children).map((card) => [card.dataset.investigation, card]));
    container.replaceChildren(...items.map((item) => {
        const previous = old.get(item.id);
        const card = previous?.dataset.signature === JSON.stringify(item) ? previous : renderInvestigation(item);
        const isSelected = investigationIntervalKind(item) === kind && item.stall_index === selected;
        if (!previous || previous.dataset.signature !== card.dataset.signature) card.open = previous?.open ?? isSelected;
        card.classList.toggle('selected', isSelected);
        const explain = card.querySelector('[data-stall-explain]');
        if (explain) explain.disabled = pendingStallExplanations.has(`${currentID}/${item.id}`);
        const stop = card.querySelector('[data-stall-stop]');
        if (stop) stop.disabled = pendingStallStops.has(`${currentID}/${item.id}`);
        return card;
    }));
}

function renderReport(report) {
    currentReport = report;
    const observed = report.manifest.observation;
    const protocol = report.manifest.protocol;
    const requests = report.requests?.length ? report.requests : (observed.requests || []);
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
    } else if (!protocols[protocol]?.mpegTS && !protocols[protocol]?.nativeRTSP) {
        values.push(
            ['视频帧 / 音频帧', `${m.video_frames} / ${m.audio_frames}`],
            ['视频最大到达间隔', time(m.video_frames > 1 ? m.max_video_gap_ms : null)],
            ['最大关键帧间隔', time(m.max_gop_ms || null)],
        );
        if (m.max_audio_gap_ms > 0) values.push(['音频最大到达间隔', time(m.max_audio_gap_ms)]);
    }
    if (observed.ttfb_ms != null && !requests.length) {
        values.push([protocol === 'rtmp' ? 'RTMP 首字节' : protocols[protocol]?.nativeRTSP ? 'RTSP 首字节' : '响应首字节', time(observed.ttfb_ms)]);
    }
    if (!protocols[protocol]?.nativeRTSP && m.video_codecs?.length) values.push(['视频编码', m.video_codecs.join(' / ')]);
    if (!protocols[protocol]?.nativeRTSP && m.audio_codecs?.length) values.push(['音频编码', m.audio_codecs.join(' / ')]);
    values.push([protocols[protocol]?.dump || '原始流', bytes(report.manifest.stream.bytes)]);
    renderMetrics('metrics', values);
    renderSamplingTiming(report, requests);
    renderRTSPReport(report);
    renderPlayerReport(report.player, observed.elapsed_ms);
    renderPlayerNetwork(report);
    renderStallInvestigations(report);
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
    const parts = [diagnosticOrigin(protocol, detail.origin), statusLabels[detail.status] || detail.status];
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
    const request = ++detailRequest;
    clearTimeout(activePoll);
    try {
        const detail = await api(runPath(id));
        if (id !== currentID || request !== detailRequest) return;
        const previousStatus = currentStatus;
        renderDetail(detail);
        if (detail.status === 'running') {
            activePoll = setTimeout(() => refreshDetail(id), 850);
        } else {
            if (activePlayer?.id === id) completePlayback(activePlayer, detail.report?.manifest.observation.end_reason);
            if (previousStatus === 'running') await refreshHistory();
            if (detail.report?.stall_investigations?.some((item) => ['queued', 'running'].includes(item.status))) activePoll = setTimeout(() => refreshDetail(id), 850);
        }
    } catch (err) {
        if (id !== currentID || request !== detailRequest) return;
        message(err.message, true);
        if (activePlayer?.id === id || currentReport?.stall_investigations?.some((item) => ['queued', 'running'].includes(item.status))) activePoll = setTimeout(() => refreshDetail(id), 1000);
    }
}

function selectRun(id) {
    if (activePlayer && activePlayer.id !== id) stopPlayback(activePlayer);
    if (node('player-preview').dataset.runId !== id) node('player-preview').hidden = true;
    currentID = id;
    currentStatus = '';
    currentReport = null;
    node('stall-investigation-results').replaceChildren();
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
            await selectRun(run.id);
            await refreshHistory();
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
    hint.hidden = !selected.receive && !selected.nativeRTSP;
    hint.textContent = selected.nativeRTSP ? '填写摄像头或 NVR 的播放地址，支持 URL 用户信息认证。采用 TCP 交织传输；RTSPS 使用 TLS 并校验服务端证书。报告仅显示协议和主机。' : selected.receive ? `填写本机 IPv4 和端口。组播示例 ${protocol.split('-')[0]}://239.1.1.1:5000?interface=en0` : '';
    const playback = node('analyze-playback');
    playback.disabled = Boolean(selected.nativeRTSP);
    if (selected.nativeRTSP) playback.checked = false;
    node('playback-capability-note').hidden = !selected.nativeRTSP;
    updateRTSPTransport();
});
node('rtsp-transport').addEventListener('change', updateRTSPTransport);
updateRTSPTransport();
const launchOptions = new URLSearchParams(location.search);
const simulatedHLS = launchOptions.get('hls_simulator');
let simulatorURL = '';
if (simulatedHLS && /^[a-f0-9-]{36}$/.test(simulatedHLS)) {
    node('protocol').value = 'hls';
    node('protocol').dispatchEvent(new Event('change'));
    simulatorURL = new URL('/api/simulators/hls/stream/' + simulatedHLS + '/playlist.m3u8', location.origin).href;
    node('url').value = simulatorURL;
    node('seconds').value = '30';
    node('analyze-playback').checked = true;
}

const simulatedMedia = launchOptions.get('media_simulator');
const simulatedProtocol = launchOptions.get('media_protocol');
if (simulatedMedia && /^[a-f0-9-]{36}$/.test(simulatedMedia) && ['http-flv', 'http-ts'].includes(simulatedProtocol)) {
    node('protocol').value = simulatedProtocol;
    node('protocol').dispatchEvent(new Event('change'));
    node('url').value = new URL('/api/simulators/media/stream/' + simulatedMedia, location.origin).href;
    node('seconds').value = '16';
}

if (simulatedMedia && /^[a-f0-9-]{36}$/.test(simulatedMedia) && ['udp-ts', 'rtp-ts'].includes(simulatedProtocol)) {
    void fetch('/api/simulators/media', {cache: 'no-store'}).then(response => response.json()).then(source => {
        if (!source.active || source.id !== simulatedMedia || source.protocol !== simulatedProtocol) return;
        node('protocol').value = simulatedProtocol;
        node('protocol').dispatchEvent(new Event('change'));
        node('url').value = source.url;
        node('seconds').value = '15';
    }).catch(() => {});
}

if (launchOptions.get('rtmp_simulator') === '1') {
    void fetch('/api/simulators/rtmp', {cache: 'no-store'}).then(response => response.json()).then(source => {
        if (!source.active || !/^rtmps?:\/\/127\.0\.0\.1:\d+\/live\/test$/.test(source.url)) return;
        node('protocol').value = 'rtmp';
        node('protocol').dispatchEvent(new Event('change'));
        node('url').value = source.url;
        node('seconds').value = '10';
    }).catch(() => {});
}
bindForm('sample', async () => {
    const startedAt = Date.now();
    const startedAtPerformanceMS = performance.now();
    const seconds = node('seconds').valueAsNumber;
    const playback = !protocols[node('protocol').value]?.nativeRTSP && !node('analyze-playback').disabled && node('analyze-playback').checked;
    const url = node('url').value.trim();
    const localSimulator = Boolean(simulatorURL && simulatorURL === url);
    const hlsRecovery = localSimulator && launchOptions.get('hls_recovery') === '1';
    const hlsABR = localSimulator && launchOptions.get('hls_abr') === '1';
    const run = await api('/runs', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ protocol: node('protocol').value, transport: protocols[node('protocol').value]?.nativeRTSP ? node('rtsp-transport').value : undefined, url, seconds, playback }),
    });
    stopPlayback(activePlayer);
    startPlayback(run, startedAt, startedAtPerformanceMS, seconds, hlsRecovery, hlsABR);
    return run;
});
bindForm('import', () => {
    message('正在上传证据包并重跑规则…');
    return api('/import', {
        method: 'POST', headers: { 'Content-Type': 'application/zip' }, body: node('archive').files[0],
    });
});
node('stop').addEventListener('click', async () => {
    const id = currentID;
    node('stop').disabled = true;
    if (activePlayer?.id === id) stopPlayback(activePlayer);
    try {
        await api(runPath(id) + '/stop', { method: 'POST' });
        if (id === currentID) message('已请求停止，正在保存采样结果。');
    } catch (err) {
        message(err.message, true);
    } finally {
        node('stop').disabled = false;
    }
});
node('play-player').addEventListener('click', () => { void activePlayer?.collector.play(); });
async function investigateInterval(kind) {
    const id = currentID;
    const stallIndex = intervalSelection(kind).get(id) || 1;
    const previous = currentReport?.stall_investigations?.find((item) => investigationIntervalKind(item) === kind && item.stall_index === stallIndex);
    const retry = Boolean(previous && !['queued', 'running'].includes(previous.status) && !previous.media?.assets?.length);
    if (!id || pendingStallRequests.has(id)) return;
    selectedIntervalKinds.set(id, kind);
    pendingStallRequests.add(id);
    renderStallInvestigations(currentReport);
    try {
        await api(runPath(id) + '/stalls', {
            method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(kind === 'presentation_gap' ? { interval_kind: 'presentation_gap', interval_index: stallIndex, retry } : { stall_index: stallIndex, retry }),
        });
        if (id === currentID) { message(`已开始补查${intervalName(kind)}证据。`); await refreshDetail(id); }
    } catch (err) { if (id === currentID) message(err.message, true); }
    finally {
        pendingStallRequests.delete(id);
        if (id === currentID) renderStallInvestigations(currentReport);
    }
}
node('investigate-stall').addEventListener('click', () => { void investigateInterval('buffering'); });
window.addEventListener('pagehide', () => {
    stopPlayback(activePlayer, true);
    for (const player of finalizingPlayers) {
        player.unloading = true;
        sendClosingRecording(player, player.finalRecording);
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
