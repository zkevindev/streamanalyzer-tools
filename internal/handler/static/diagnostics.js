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
const rtspPreviewNote = '原生 RTSP/RTP 采样不依赖浏览器预览。未记录转封装能力，播放器首屏、缓冲和呈帧指标以实际保存的播放器记录为准。';
const rtspPreviewStates = { ready: '已输出媒体', unsupported: '编码或封装暂不支持', failed: '本机转封装未完成', not_requested: '未请求预览' };
const rtspPreviewReasons = { no_track: '未选择对应轨道', no_media: '未输出对应媒体', codec_unsupported: '首版仅支持 H.264 / AAC-LC', packetization_unsupported: 'RTP 打包方式不支持', aac_configuration_unsupported: 'AAC 配置不支持（需 AAC-LC、1024 采样、单/双声道）', queue_limit: '本机预览队列达到上限', missing_video_parameters: '缺少有效 SPS/PPS', no_random_access: '未取得可用 IDR 随机接入点', timestamp_unsupported: '媒体时间戳或 DTS 不可转换', access_unit_limit: '媒体访问单元达到上限', player_unavailable: '本机播放器已停止接收', capture_setup_failed: '原生播放准备或 PLAY 未完成' };
const rtspPreviewClockNote = '同次采样的 RTP 在本机转封装为 FLV，不另建拉流连接。音视频使用接收端 RTP 共同时间基准，不代表已验证源端音画同步；输出访问单元数不是解码或呈帧数。浏览器解码能力仍需实际播放验证。';
const rtspG711States = { ready: '已输出 AAC', no_media: '未输出音频', unavailable: '本机转码不可用', failed: '本机转码失败' };
const rtspG711Reasons = {
    ffmpeg_unavailable: '未找到 FFmpeg，G.711 音频预览不可用', ffmpeg_unsupported: '本机 FFmpeg 不支持所需的 AAC 转码输出',
    ffmpeg_start_failed: '本机 FFmpeg 音频编码器无法启动', ffmpeg_failed: '本机 G.711 音频转码进程失败',
    ffmpeg_output_invalid: 'AAC 输出与编码时序元数据不一致，已停止预览', ffmpeg_drain_timeout: '音频编码器结束排空超时，已停止预览',
    g711_configuration_unsupported: 'G.711 采样率或声道配置不受本版支持', g711_packet_invalid: 'G.711 包大小或声道采样长度超出本版预览范围',
    g711_queue_limit: '本机 G.711 转码队列达到上限，原始采样继续', g711_timeline_discontinuity: 'G.711 RTP 时钟或 SSRC 出现较大不连续，未重新归零音频',
    g711_silence_limit: '已达到按 RTP 缺口补入静音的上限', g711_sample_limit: '音频样本时间范围达到预览上限',
    g711_player_unavailable: '本机播放器已停止接收 G.711 转码输出', audio_no_media: '没有得到可供预览的 G.711 音频输出',
};
const rtspG711ClockNote = 'G.711 在本机经 FFmpeg 有损转码为 AAC，原始 RTP 不变。样本数按每声道统计；输入、补静音和迟到丢弃使用输入采样率，输出与编码预延迟/填充使用输出采样率。补静音依据 RTP 时间缺口生成。首个编码输出耗时从开始处理首个输入包起计。本机工具检查、首个编码输出和排空耗时不属于网络阶段。';
function rtspPreviewReason(code, preview) {
    if (preview?.version === 2 && preview.codec_capabilities_version >= 3) {
        if (code === 'codec_unsupported') return '本版支持 H.264 / H.265 / AAC-LC / Opus，并可将 G.711 转码为 AAC；其他编码预览暂不支持';
        if (rtspG711Reasons[code]) return rtspG711Reasons[code];
    }
    if (preview?.version === 2 && preview.codec_capabilities_version >= 2 && code === 'codec_unsupported') return '本版支持 H.264 / H.265 / AAC-LC / Opus；其他编码预览暂不支持';
    if (preview?.version === 2 && preview.codec_capabilities_version >= 2 && code === 'opus_configuration_unsupported') return 'Opus 预览仅支持单/双声道和映射族 0';
    if (preview?.version === 2 && code === 'codec_unsupported') return '本版支持 H.264 / H.265 / AAC-LC；其他编码预览暂不支持';
    if (preview?.version === 2 && code === 'missing_hevc_parameters') return '缺少或无法解析有效 VPS/SPS/PPS';
    return code ? (rtspPreviewReasons[code] || '未识别的预览状态') : '无'; }
function renderRTSPPreview(metrics) {
    const target = node('rtsp-preview-metrics');
    const preview = metrics?.preview;
    target.replaceChildren();
    if (!preview) { node('rtsp-capability').textContent = rtspPreviewNote; return; }
    node('rtsp-capability').textContent = rtspPreviewClockNote + (preview.g711 ? ' ' + rtspG711ClockNote : '');
    const entries = [
        ['本机转封装状态', rtspPreviewStates[preview.state] || '未记录'], ['预览原因', rtspPreviewReason(preview.reason_code, preview)],
        ['预览视频', preview.video_track_id ? `#${preview.video_track_id} · ${preview.video_codec}` : '未选择'], ['视频说明', rtspPreviewReason(preview.video_reason, preview)],
        ['预览音频', preview.audio_track_id ? `#${preview.audio_track_id} · ${preview.audio_codec}` : '未选择'], ['音频说明', preview.audio_reason === 'codec_unsupported' ? '音轨预览不支持，已静音；原始 RTP 仍保留' : rtspPreviewReason(preview.audio_reason, preview)],
        ...(preview.video_configurations?.length ? [['H.264 转封装配置', preview.video_configurations.map(config => config.width + '×' + config.height + ' @ ' + config.media_dts_ms + ' ms').join(' → ')]] : []),
        ['视频访问单元', preview.video_aus ?? '未观测'], ['音频访问单元', preview.audio_aus ?? '未观测'], ['转封装包错误', preview.packet_errors ?? '未观测'], ['预览队列丢弃', preview.dropped_packets ?? '未观测'],
    ];
    if (preview.version === 2) entries.push(
        ['编码能力版本', preview.codec_capabilities_version ?? '未记录'],
        ['视频预览输出', preview.video_output_codec || '未选择'], ['视频处理', preview.video_transform === 'native_remux' ? '原生转封装' : preview.video_transform || '未选择'],
        ['音频预览输出', preview.audio_output_codec || '未选择'], ['音频处理', preview.audio_transform === 'g711_to_aac' ? '本机 G.711 → AAC 转码' : preview.audio_transform === 'native_remux' ? '原生转封装' : preview.audio_transform || '未选择'],
    );
    if (preview.g711) {
        const g = preview.g711;
        const observed = (value, suffix = '') => value == null ? '未观测' : `${value}${suffix}`;
        entries.push(
            ['G.711 转码状态', rtspG711States[g.state] || '未记录'], ['G.711 转码原因', rtspPreviewReason(g.reason_code, preview)], ['本机编码器', g.encoder || '未记录'],
            ['输入采样率', observed(g.input_sample_rate, ' Hz')], ['输出采样率', observed(g.output_sample_rate, ' Hz')], ['音频声道', observed(g.channels)],
            ['输入 G.711 包', observed(g.input_packets)], ['输入样本', observed(g.input_samples)], ['按 RTP 缺口补静音样本', observed(g.inserted_silence_samples)],
            ['迟到丢弃样本', observed(g.dropped_late_samples)], ['转码丢弃包', observed(g.dropped_packets)], ['转码包错误', observed(g.packet_errors)], ['时钟不连续次数', observed(g.timeline_resets)],
            ['输出 AAC 访问单元', observed(g.output_aus)], ['输出样本', observed(g.output_samples)], ['编码预延迟样本', observed(g.encoder_priming_samples)], ['编码填充样本', observed(g.encoder_padding_samples)],
            ['工具检查耗时', observed(g.tool_check_ms, ' ms')], ['首个编码输出耗时', observed(g.startup_ms, ' ms')], ['编码器排空耗时', observed(g.drain_ms, ' ms')],
        );
    }
    renderMetrics('rtsp-preview-metrics', entries);
}

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
    renderRTSPPreview(metrics);
    const clocks = metrics?.sender_clocks || [];
    node('rtsp-clocks').hidden = !clocks.length;
    node('rtsp-clock-table').replaceChildren(...(clocks.length ? [renderTable(
        ['轨道 / SSRC', '发送报告', '有效间隔', '累计偏移 (ms)', '最大时钟步进 (ms)', '最大接收间隔偏差 (ms)'],
        clocks.map(clock => ['#' + clock.track_id + ' / ' + clock.ssrc, clock.samples, clock.intervals,
            clock.offset_ms.toFixed(1), clock.max_step_ms.toFixed(1), clock.max_arrival_variation_ms.toFixed(1)])
    )] : []));
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
        ...(metrics.udp_source_evidence?.length ? [['RTP 来源过滤', metrics.udp_source_evidence.map(item =>
            '#' + item.media_id + ' · ' + item.negotiated_port + ' → ' + item.observed_port + ' · ' +
            (item.same_ip ? '同 IP' : '不同 IP') + ' · ' + item.filtered_packets + ' 包 · 恢复 ' + time(item.restored_ms)).join(' / ')]] : []),
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
const playbackErrors = { media_error: '播放器媒体解码失败', network_error: '播放器网络请求失败', no_stream: '播放器未收到媒体流', preview_unsupported: 'RTSP 本机预览编码或封装不支持', preview_failed: 'RTSP 本机转封装失败，详见原生预览原因', preview_queue_limit: 'RTSP 本机预览队列达到上限', browser_codec_unsupported: '浏览器不支持本次预览输出编码' };
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
let currentMode = '';
let currentAutomaticRunning = false;
let activePoll;
let activeBatchPoll;
let activeBatchID = '';
let activeSessionGroupID = '';
let runs = [];
const selectedRunIDs = new Set();
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
const pendingAutomaticRequests = new Set();
const pendingStallExplanations = new Set();
const pendingStallStops = new Set();
const openPlayerNetworkDetails = new Set();
let currentReport;
let currentComparison = null;
const investigationStates = { queued: '等待补查', running: '补查中', complete: '补查完成', partial: '保留部分结果', failed: '补查失败', canceled: '补查已停止' };
const investigationMediaStates = { ready: '片段与截图已提取', partial: '媒体产物部分可用', unavailable: '媒体产物不可用', failed: '媒体提取失败' };
const investigationModelStates = { not_requested: '未单独解读 · 见整体汇总', pending: '正在结合时序证据生成建议…', complete: '已生成建议', unconfigured: '尚未配置大模型，可在模型设置中配置后生成建议。', failed: '模型建议生成失败，可重试。', changed: '模型配置已变化，可使用当前模型重新生成建议。' };
const investigationAlignments = { receiver_rtp_estimate: '接收端 RTP 时钟与播放器媒体时间估算', media_clock: '播放器媒体时间对齐', player_clock: '播放器媒体时间对齐', player_clock_estimate: '根据播放器媒体时间估算', arrival_estimate: '按接收时序估算', independent_capture: '独立采样连接参考', independent_capture_reference: '独立连接采样参考' };

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

function startPlayback(run, startedAt, startedAtPerformanceMS, seconds, hlsRecovery, hlsABR, hlsLowLatency) {
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
        startedAt, startedAtPerformanceMS, hlsRecovery, hlsABR, hlsLowLatency,
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
        if (run.protected) meta.append(element('span', '重要证据'));
        button.append(meta);
        button.addEventListener('click', () => selectRun(run.id));
        const selected = document.createElement('input');
        selected.type = 'checkbox';
        selected.setAttribute('aria-label', '选择删除 ' + run.id);
        selected.disabled = run.protected || run.status === 'running';
        selected.checked = selectedRunIDs.has(run.id) && !selected.disabled;
        selected.addEventListener('change', () => {
            if (selected.checked) selectedRunIDs.add(run.id); else selectedRunIDs.delete(run.id);
            node('delete-selected').hidden = selectedRunIDs.size === 0;
        });
        row.append(selected, button);
        return row;
    });
    node('runs').replaceChildren(...items);
    const available = new Set(runs.filter(run => !run.protected && run.status !== 'running').map(run => run.id));
    for (const id of selectedRunIDs) if (!available.has(id)) selectedRunIDs.delete(id);
    node('delete-selected').hidden = selectedRunIDs.size === 0;
}

async function refreshStorage() {
    const storage = await api('/storage', { cache: 'no-store' });
    const protectedText = storage.protected_count ? ` · ${storage.protected_count} 条重要证据 ${bytes(storage.protected_bytes)}` : '';
    node('storage-summary').textContent = `${storage.run_count} 条记录 · 排障产物 ${bytes(storage.total_bytes)}${protectedText}`;
}

async function refreshHistory() {
    runs = await api('/runs');
    renderHistory();
    await refreshStorage();
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
            const cell = row.insertCell();
            if (value instanceof Node) cell.append(value);
            else cell.textContent = value ?? '—';
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
            ['rtp_packet', 'video_rtp', 'audio_rtp'].includes(event.kind) ? 'RTP 包' : '协议字节', event.track > 0 ? `#${event.track}` : '未记录', event.codec || '未声明', event.rtp_sequence == null ? '未观测' : String(event.rtp_sequence), event.arrival_ms, rtspEvidenceArtifact(event.artifact), event.offset, event.size,
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
        ['首个 TS 包', time(ts.first_packet_ms)], ['首个有效 PAT', time(ts.first_pat_ms)],
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
    const programs = ts.programs || [];
    node('ts-programs').hidden = programs.length === 0;
    node('ts-program-table').replaceChildren(...(programs.length ? [renderTable(
        ['节目 / PMT PID', '首个有效 PMT', '媒体 PID', '收到 TS 包', '连续计数错误', '首次切换后收到 TS 包', '首次切换后连续计数错误', 'PTS−PCR 首尾偏移'],
        programs.flatMap(program => program.streams.map(stream => [program.number + ' / ' + hexLabel(program.pmt_pid, 4),
            time(program.first_pmt_ms), hexLabel(stream.pid, 4), stream.packets, stream.continuity_errors, stream.after_switch_packets ?? '—',
            stream.after_switch_errors ?? '—', stream.clock ? stream.clock.first_lead_ms + ' → ' + stream.clock.last_lead_ms +
              ' ms（' + (stream.clock.drift_ms > 0 ? '+' : '') + stream.clock.drift_ms + ' ms）' : '—'])),
    )] : []));
    const maps = ts.program_map_changes || [];
    node('ts-map-changes').hidden = maps.length === 0;
    const mapping = (state, times) => state.streams.map(stream => hexLabel(stream.pid, 4) +
        (!times ? '' : stream.first_packet_ms == null ? ' · 未观测' : ' · ' + time(stream.first_packet_ms))).join(' / ') || '—';
    node('ts-map-table').replaceChildren(...(maps.length ? [renderTable(
        ['节目 / PMT PID', '切换偏移', '版本', 'PCR PID', '原媒体 PID', '新媒体 PID / 首包'],
        maps.map(change => [change.program + ' / ' + (change.before_pmt_pid == null ? '' : hexLabel(change.before_pmt_pid, 4) + ' → ') + hexLabel(change.pmt_pid, 4), time(change.at_ms),
            change.before.version + ' → ' + change.after.version,
            hexLabel(change.before.pcr_pid, 4) + ' → ' + hexLabel(change.after.pcr_pid, 4),
            mapping(change.before, false), mapping(change.after, true)]))] : []));
    const pids = ts.pids || [];
    node('ts-pids').hidden = pids.length === 0;
    if (!pids.length) {
        node('pid-table').replaceChildren();
        return;
    }
    node('pid-summary').textContent = ts.pid_count > pids.length ? `PID 明细（显示 ${pids.length} / ${ts.pid_count}）` : 'PID 明细';
    const table = renderTable(['PID', '编码', '流类型', 'TS 包数', 'TEI', '连续计数错误', '重复包', 'PES 起始数', '首个已识别 PES', 'PCR / PTS / DTS 计数'],
        pids.map((pid) => [hexLabel(pid.pid, 4), pid.codec || '—', hexLabel(pid.stream_type, 2), pid.packets,
            pid.transport_errors, pid.continuity_errors, pid.duplicates, pid.pes_starts, time(pid.first_mapped_pes_ms),
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
    const lastError = (player.events || []).filter((event) => event.kind === 'error' || event.kind === 'unsupported').at(-1);
    const state = (player.state === 'error' || player.state === 'unsupported') ? playbackErrors[lastError?.code] || playerStates[player.state] : playerStates[player.state] || player.state;
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
const playerNetworkKinds = { playlist: '播放列表', media: '媒体分片', part: '低延迟 Part', init: '初始化段', key: '密钥', auxiliary: '辅助资源' };
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
        `#${request.number}`, (playerNetworkKinds[request.kind] || request.kind || '未标注') + (request.blocking_reload ? ' · 请求阻塞式刷新' : '') + (request.rendition === 'audio' ? ' · 独立音轨' : ''), request.resource_id || '未标注', (request.playlist_sequence ?? '—') +
        (request.playlist_boundary ? ' · 跨段序号 ' + request.playlist_boundary.epoch + (request.playlist_boundary.next_sequence == null ? '' : ' / 边界 ' + request.playlist_boundary.next_sequence) : '') +
        (request.cache_age_seconds == null ? '' : ' · Age ' + request.cache_age_seconds + ' s'), request.method || '未标注',
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

const rtspMediaBoundaryNote = '离线重建仅使用已采集的原始 RTP，不会重新连接源站。按保存的播放器媒体时间与接收端 RTP 时钟估算位置，不是源端 UTC 或精确呈帧对齐，也未按收包到达时间直接定位画面。';
const rtspMediaContentNote = '片段与截图用于核对同期源流内容，不能重现或证明本机画面冻结；原收流等待和播放器停顿请对照时序证据。';

function renderRTSPMediaEvidence(media) {
    const evidence = media.rtsp;
    if (!evidence) return null;
    const block = element('section', '', 'rtsp-media-evidence');
    block.append(element('h5', 'RTSP 离线媒体重建'), element('p', rtspMediaBoundaryNote, 'timing-note muted'), element('p', rtspMediaContentNote, 'timing-note muted'));
    const metrics = element('dl', '', 'metrics rtsp-media-metrics');
    const selectedTrack = (id, codec) => id ? `#${id} · ${codec || '编码未记录'}` : '未选择';
    for (const [label, value] of [
        ['源视频轨道', selectedTrack(evidence.video_track_id, evidence.video_codec)],
        ['源音频轨道', selectedTrack(evidence.audio_track_id, evidence.audio_codec)],
        ['重建音频处理', evidence.audio_transform === 'g711_to_pcm' ? 'G.711 解码为 PCM' : evidence.audio_transform === 'native_remux' ? '原生转封装' : '未选择'],
        ['播放器记录修订', evidence.player_revision == null ? '未记录' : `#${evidence.player_revision}`],
        ['锚点采样偏移', videoObservedTime(evidence.anchor_sample_offset_ms)], ['锚点播放器媒体时间', videoObservedTime(evidence.anchor_current_ms)],
        ['预览解码时钟起点', videoObservedTime(evidence.preview_origin_ms)], ['重组媒体起点', videoObservedTime(evidence.media_origin_ms)],
        ['重建媒体范围', videoObservedTime(evidence.duration_ms)], ['检查 RTP 包', videoObservedCount(evidence.packet_count)],
        ['重建视频访问单元', videoObservedCount(evidence.video_access_units)], ['重建音频单元 / PCM 块', videoObservedCount(evidence.audio_access_units)],
        ['未用于重建的 RTP 包', videoObservedCount(evidence.dropped_packets)], ['丢弃不完整访问单元', videoObservedCount(evidence.incomplete_access_units)],
    ]) {
        const row = element('div', ''); row.append(element('dt', label), element('dd', value)); metrics.append(row);
    }
    block.append(metrics, element('p', '锚点重建位置 = 播放器媒体时间 + 预览解码时钟起点 − 重组媒体起点。两项分别对应预览归一化前的 DTS 与重建输入的呈现时间起点；派生 MP4 使用 H.264 / AAC，重建计数不是浏览器解码或呈帧计数。', 'timing-note muted'));
    const frames = evidence.keyframes || [];
    if (frames.length) {
        block.append(element('h5', '离线重建随机接入点'), renderTable(['归零前 DTS', '归零前 PTS', 'RTP 到达偏移', '轨道', '原始条目', '偏移（字节）'], frames.slice(0, 16).map((frame) => [time(frame.dts_ms), time(frame.pts_ms), time(frame.arrival_ms), frame.track == null ? '未记录' : `#${frame.track}`, frame.artifact || '未记录', frame.offset ?? '未记录'])), element('p', '原始引用标识访问单元完成时的 RTP 包，不表示该单包包含完整画面。DTS / PTS 为归零前重建时间，减去重组媒体起点后可与片段定位比较。最多展示 16 个随机接入点。', 'timing-note muted'));
    } else block.append(element('p', '未保留可用的离线重建随机接入点记录。', 'timing-note muted'));
    const identities = element('details', '', 'rtsp-media-identities');
    identities.append(element('summary', '重建依据与文件指纹（SHA-256）'));
    identities.append(renderTable(['依据', '大小', 'SHA-256'], [
        ['原始 RTSP 归档', evidence.source_artifact], ['接收时间线', evidence.timeline_artifact], ['播放器记录', evidence.player_artifact], ['临时重建 fMP4', evidence.remux_artifact],
    ].map(([label, artifact]) => [label, artifact?.bytes == null ? '未记录' : bytes(artifact.bytes), artifact?.sha256 || '未记录'])));
    identities.append(element('p', '临时重建 fMP4 指纹用于复核重建结果，不替代原始 dump，也不作为额外原始流归档。', 'timing-note muted'));
    block.append(identities);
    return block;
}

function renderInvestigationMedia(item) {
    const block = element('section', '', 'stall-media');
    const media = item.media || {};
    block.append(element('h5', '异常附近的源流片段与截图'), element('p', investigationMediaStates[media.status] || '等待提取源流媒体。', 'timing-note muted'));
    if (media.reason) block.append(element('p', media.reason, 'timing-note muted'));
    if (media.alignment) block.append(element('p', `媒体定位方式 ${investigationAlignments[media.alignment] || media.alignment}${media.start_ms != null ? ` · ${media.rtsp ? '重建媒体时间' : '源流媒体时间'} ${time(media.start_ms)}–${time(media.end_ms)}` : ''}`, 'timing-note muted'));
    const rtspEvidence = renderRTSPMediaEvidence(media);
    if (rtspEvidence) block.append(rtspEvidence);
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
            const caption = element('figcaption', `${label}${asset.media_time_ms != null ? ` · ${media.rtsp ? '重建媒体时间' : '源流媒体时间'} ${time(asset.media_time_ms)}` : ''}`);
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

const modelInputFallbackLabels = {
    disabled: '已选择仅文本', no_images: '没有可用截图', images_invalid: '截图未通过完整性校验',
    provider_text_only: '当前厂商使用文本接口', model_text_only: '当前型号仅支持文本', images_unsupported: '图文请求不支持，已回退文本',
};
const modelImageRole = (name) => name?.endsWith('/before.jpg') ? '异常前源流截图' : name?.endsWith('/after.jpg') ? '异常后源流截图' : '异常附近源流截图';
function modelImageAsset(item, reference) {
    if (reference.case_id !== item.id || !/^S\d{3}-I00[1-3]$/.test(reference.id || '') || !reference.id.startsWith(item.id + '-')) return null;
    return (item.media?.assets || []).find((asset) => asset.kind === 'screenshot' && asset.name === reference.name && asset.bytes === reference.bytes && asset.sha256 === reference.sha256) || null;
}
function renderModelInputEvidence(item) {
    const block = element('section', '', 'model-input-evidence');
    const audit = item.explanation?.input_evidence;
    if (!audit) {
        block.append(element('p', '该历史结果未记录截图输入方式。', 'timing-note muted model-input-legacy'));
        return block;
    }
    const used = (audit.images || []).slice(0, 3);
    const attempted = (audit.attempted_images || []).slice(0, 3);
    const vision = audit.mode === 'vision';
    const label = vision ? `本次为图文解读 · ${used.length} 张截图与时序文本` : '本次为时序文本解读';
    block.append(element('p', label, 'model-input-mode'));
    const context = [`请求设置 ${audit.requested_mode === 'text' ? '仅文本' : '自动图文'}`, `播放器记录修订 #${audit.player_revision ?? '未记录'}`];
    if (audit.fallback_code) context.push(modelInputFallbackLabels[audit.fallback_code] || '未记录可识别的回退原因');
    block.append(element('p', context.join(' · '), 'timing-note muted'));
    if (attempted.length) block.append(element('p', `已尝试发送 ${attempted.length} 张截图；图文请求不支持，最终建议基于文本。`, 'timing-note model-input-fallback'));
    else if (!vision) block.append(element('p', '本次仅发送时序文本，未发送截图像素。', 'timing-note muted'));
    if (vision) block.append(element('p', '截图来自采样源流，图像观察仍需结合时序验证；不能据此重现或证明本机画面冻结。', 'timing-note muted'));
    const fingerprints = [];
    const anchors = new Set([...(item.correlation?.findings || []), ...(item.assessment?.findings || [])].map((finding) => finding.id));
    for (const [references, finalInput] of [[used, true], [attempted, false]]) {
        if (!references.length) continue;
        block.append(element('p', finalInput ? '本次采用的截图' : '已尝试发送的截图', 'timing-note'));
        const list = element('ul', '', 'model-input-images');
        for (const reference of references) {
            const row = element('li', '');
            const asset = modelImageAsset(item, reference);
            const id = element('strong', reference.id || '截图编号未记录');
            if (finalInput && vision && asset && !anchors.has(reference.id)) { id.id = 'case-' + reference.id; anchors.add(reference.id); }
            row.append(id, document.createTextNode(` · ${modelImageRole(reference.name)}${reference.media_time_ms == null ? '' : ` · ${item.media?.rtsp ? '重建' : '采样'}媒体时间 ${time(reference.media_time_ms)}`} · ${reference.bytes == null ? '大小未记录' : bytes(reference.bytes)} `));
            const url = asset && investigationAssetURL(item, asset);
            if (url) { const link = element('a', '查看本地截图'); link.href = url; row.append(link); }
            else row.append(element('span', '本地截图引用未匹配', 'muted'));
            list.append(row);
            fingerprints.push([reference.id || '截图', reference.sha256 || '未记录']);
        }
        block.append(list);
    }
    if (fingerprints.length) {
        const details = element('details', '', 'model-input-identities'); details.append(element('summary', '截图文件指纹（SHA-256）'));
        for (const [id, hash] of fingerprints) details.append(element('p', `${id} · ${hash}`, 'timing-note muted'));
        block.append(details);
    }
    return block;
}

function renderInvestigationModel(item) {
    const block = element('section', '', 'stall-model');
    const vision = item.explanation?.input_evidence?.mode === 'vision';
    block.append(element('h5', vision ? '模型建议（截图与时序证据）' : item.media?.rtsp ? '模型建议（时序与重建证据）' : '模型建议（时序证据）'));
    if (item.explanation) block.append(renderModelInputEvidence(item));
    else if (item.model_state === 'not_requested') { const note = element('p', '未单独解读 · ', 'timing-note muted'); const link = element('a', '见整体汇总'); link.href = '#report-model-heading'; note.append(link); block.append(note); }
    else block.append(element('p', currentModel?.image_input === 'text' ? '当前补查设为仅文本，截图供人工对照。' : '当前补查将按模型设置使用可用截图与时序；图片不支持时回退文本，实际输入以解读记录为准。', 'timing-note muted'));
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
    } else if (item.model_state !== 'not_requested') block.append(element('p', investigationModelStates[item.model_state] || '等待模型建议。', 'timing-note muted'));
    if (item.model_error) block.append(element('p', item.model_error, 'timing-note muted'));
    if (item.status !== 'queued' && item.status !== 'running') {
        const button = element('button', item.model_state === 'not_requested' ? '单独解读本次异常' : !item.assessment ? '生成排查与模型建议' : item.explanation ? '用当前模型重新生成建议' : '生成模型建议', 'secondary');
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

const automaticStateLabels = { waiting_player: '等待播放器记录完成', queued: '等待处理', investigating: '正在提取异常证据', summarizing: '正在生成整体汇总', complete: '自动流程完成', partial: '保留部分结果', unconfigured: '媒体补查已完成，模型尚未配置', failed: '自动流程未完成', canceled: '自动流程已停止', interrupted: '任务因服务中断停止' };
const automaticSummaryLabels = { pending: '尚未生成', complete: '已生成', unconfigured: '未配置模型', failed: '生成失败', canceled: '已停止', not_requested: '尚未请求' };
const automaticActiveStates = new Set(['waiting_player', 'queued', 'investigating', 'summarizing']);
function automaticWorkflowActive() { return currentAutomaticRunning; }
function automaticSelectionCase(report, selection) { return (report.stall_investigations || []).find((item) => item.id === selection.case_id); }
function automaticSelectionMedia(item) {
    if (!item) return '尚未创建案例';
    if (['queued', 'running'].includes(item.status)) return '等待媒体提取';
    return investigationMediaStates[item.media?.status] || '未记录';
}
function renderAutomaticInvestigation(report) {
    const section = node('automatic-investigation'), content = node('automatic-investigation-content'), automatic = report.automatic_investigation;
    if (!section || !content) return;
    section.hidden = !automatic;
    if (!automatic) { content.replaceChildren(); return; }
    const active = automaticWorkflowActive(), imported = currentMode === 'replay' && !active;
    const block = document.createDocumentFragment();
    block.append(element('p', `${active ? '当前阶段' : '记录阶段'} · ${automaticStateLabels[automatic.state] || '未记录阶段'}`, 'automatic-stage'));
    if (imported) block.append(element('p', '已导入记录，不自动续跑；任务阶段按证据保留。只有点击重新运行才会开始新的处理。', 'timing-note muted automatic-replay-note'));
    else if (!active && automaticActiveStates.has(automatic.state)) block.append(element('p', '这是已保存的任务快照，当前没有后台任务运行。可按当前配置重新运行。', 'timing-note muted'));
    if (automatic.reason) block.append(element('p', automatic.reason, 'timing-note muted'));
    if (active && automatic.state === 'waiting_player') block.append(element('p', '采样已结束，最多等待 30 秒保存完整播放器记录，再选择补查区间。', 'timing-note muted'));
    const planned = automatic.candidate_count > 0 || (automatic.selected || []).length > 0 || (automatic.player_revision > 0 && ['investigating', 'summarizing', 'complete', 'partial', 'unconfigured'].includes(automatic.state));
    const selected = automatic.selected || [];
    const metrics = element('dl', '', 'metrics automatic-metrics');
    for (const [label, value] of [
        ['候选同期异常区间', planned ? String(automatic.candidate_count) : '尚未记录选择结果'],
        ['自动选择', planned || selected.length ? `${selected.length} 个 / 最多 3 个` : '尚未记录选择结果'],
        ['播放器记录', automatic.player_revision > 0 ? `修订 #${automatic.player_revision}` : '未完成'],
        ['整体汇总状态', automaticSummaryLabels[automatic.model_state] || '尚未记录'],
    ]) { const row = element('div', ''); row.append(element('dt', label), element('dd', value)); metrics.append(row); }
    block.append(metrics, element('p', '按采样内重合时长优先选择，跳过与已选区间重合的候选，最多提取 3 个案例；原始观测区间保持不变。候选数与选择数不是新的卡顿次数，单次案例只提取证据，模型统一汇总一次。', 'timing-note muted'));
    if (planned && automatic.candidate_count === 0) block.append(element('p', '未选出可补查的同期异常；整体汇总仍可使用本次报告指标与规则证据。', 'timing-note muted'));
    if (selected.length) {
        const elapsed = report.manifest?.observation?.elapsed_ms ?? 0;
        const table = renderTable(['观测类型', '原始区间', '采样内重合', '媒体产物', '关联案例'], selected.slice(0, 3).map((selection) => {
            const interval = selection.interval || {}, overlap = Math.max(0, Math.min(interval.end_ms ?? 0, elapsed) - Math.max(0, interval.start_ms ?? 0));
            return [intervalTitle(selection.interval_kind || 'buffering', selection.interval_index), `${time(interval.start_ms)}–${time(interval.end_ms)}`, time(overlap), automaticSelectionMedia(automaticSelectionCase(report, selection)), selection.case_id || '尚未创建'];
        }));
        table.querySelectorAll('tbody tr').forEach((row, index) => { const id = selected[index].case_id; if (!id) return; const cell = row.lastElementChild; cell.textContent = ''; const link = element('a', id); link.href = '#investigation-' + encodeURIComponent(id); link.addEventListener('click', (event) => { event.preventDefault(); showFinding('investigation-' + id); }); cell.append(link); });
        block.append(table);
    }
    const actions = element('div', '', 'tool-row automatic-actions'), busy = pendingAutomaticRequests.has(currentID);
    const button = element('button', busy ? '正在提交…' : active ? '停止自动流程' : '按当前配置重新运行', 'secondary'); button.type = 'button'; button.disabled = busy; button.dataset.automaticAction = active ? 'stop' : 'retry';
    const runID = currentID;
    button.addEventListener('click', async () => {
        if (pendingAutomaticRequests.has(runID)) return; pendingAutomaticRequests.add(runID); button.disabled = true;
        try { await api(runPath(runID) + '/automatic/' + (active ? 'stop' : 'retry'), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }); if (currentID === runID) { message(active ? '已请求停止自动流程，正在保留已获得的证据。' : '已请求按当前配置运行自动补查与整体汇总。'); await refreshDetail(runID); } }
        catch (error) { if (currentID === runID) message(error.message, true); }
        finally { pendingAutomaticRequests.delete(runID); if (currentID === runID) void refreshDetail(runID); }
    });
    actions.append(button);
    const summary = element('a', '查看整体汇总', 'button secondary'); summary.href = '#report-model-heading'; actions.append(summary); block.append(actions);
    content.replaceChildren(block);
}

function renderInvestigationAction(report, kind, intervals, buttonID, statusID) {
    const button = node(buttonID);
    const status = node(statusID);
    if (!button || !status) return;
    const items = report?.stall_investigations || [];
    const selected = intervalSelection(kind).get(currentID) || 1;
    const existing = items.find((item) => investigationIntervalKind(item) === kind && item.stall_index === selected);
    const playerPending = activePlayer?.id === currentID || Array.from(finalizingPlayers).some((player) => player.id === currentID) || ['recording', 'awaiting'].includes(report?.player?.state);
    const busy = pendingStallRequests.has(currentID) || automaticWorkflowActive();
    const retry = existing && !['queued', 'running'].includes(existing.status) && !existing.media?.assets?.length;
    const full = !existing && items.length >= 8;
    button.disabled = !intervals.length || currentStatus === 'running' || playerPending || busy || full || Boolean(existing && !retry);
    const label = kind === 'presentation_gap' ? `第 ${selected} 个呈帧回调间隔` : `第 ${selected} 次卡顿`;
    button.textContent = !intervals.length ? (kind === 'presentation_gap' ? '暂无可补查回调间隔' : '暂无缓冲卡顿区间') : busy ? '正在提交…' : retry ? `重新补查${label}` : existing ? (investigationStates[existing.status] || '已补查') : `补查${label}`;
    status.textContent = automaticWorkflowActive() ? '自动流程正在处理关键异常，完成后仍可手动补充其他区间。' : !intervals.length ? (kind === 'presentation_gap' ? '当前没有可补查的呈帧回调间隔。' : '当前没有可补查的缓冲卡顿区间。') : full ? '本报告已达到 8 个补查案例上限，可查看或重新补查已有案例。' : playerPending || currentStatus === 'running' ? `采样和播放器记录保存完成后，可以选择${intervalName(kind)}补查证据。` : existing ? `${label} ${investigationStates[existing.status] || existing.status}。` : `已选择${label}。补查会关联时序并提取异常附近的源流媒体。`;
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
        if (explain) explain.disabled = automaticWorkflowActive() || pendingStallExplanations.has(`${currentID}/${item.id}`);
        const stop = card.querySelector('[data-stall-stop]');
        if (stop) stop.disabled = automaticWorkflowActive() || pendingStallStops.has(`${currentID}/${item.id}`);
        return card;
    }));
}

function contentQualityIntervalsTable(intervals) {
    return renderTable(['开始', '结束', '时长'], (intervals || []).map(interval => [time(interval.start_ms), time(interval.end_ms), time(interval.duration_ms)]));
}

function renderContentQuality(quality) {
    const content = node('content-quality-result');
    content.replaceChildren();
    node('run-content-quality').textContent = quality ? '重新检测（如有新原始流）' : '开始本地检测';
    if (!quality) return;
    const entries = [
        ['状态', { complete: '检测完成', partial: '部分完成', unavailable: '暂不可用' }[quality.state] || quality.state],
        ['可解码时长', time(quality.duration_ms)],
        ['视频 / 音频轨道', `${quality.has_video ? '有' : '无'} / ${quality.has_audio ? '有' : '无'}`],
        ['黑帧 / 冻结 / 静音区间', `${quality.black_intervals?.length || 0} / ${quality.freeze_intervals?.length || 0} / ${quality.silence_intervals?.length || 0}`],
    ];
    if (quality.reason) entries.push(['说明', quality.reason]);
    if (quality.av_sync?.state === 'complete') entries.push(
        ['音画时间戳样本对', quality.av_sync.sample_pairs],
        ['初始 / 末尾 AV 差', `${time(quality.av_sync.initial_delta_ms)} / ${time(quality.av_sync.final_delta_ms)}`],
        ['AV 差漂移 / 最大绝对差', `${time(quality.av_sync.drift_ms)} / ${time(quality.av_sync.max_abs_delta_ms)}`],
    );
    else if (quality.av_sync?.reason) entries.push(['音画时间戳', quality.av_sync.reason]);
    content.append(renderMetricsFragment(entries));
    const groups = [['黑帧区间', quality.black_intervals], ['画面冻结区间', quality.freeze_intervals], ['音频静音区间', quality.silence_intervals]];
    for (const [title, intervals] of groups) {
        if (!intervals?.length) continue;
        content.append(element('h4', title), contentQualityIntervalsTable(intervals));
    }
    if (quality.correlations?.length) {
        content.append(element('h4', '跨层时间关联'), renderTable(['内容异常', '内容时间', '估算采样时间', '网络证据', '播放器卡顿', 'RTMP 控制', '低吞吐秒', '说明'],
            quality.correlations.map(item => [item.content_kind, `${time(item.content_start_ms)}–${time(item.content_end_ms)}`, `${time(item.aligned_start_ms)}–${time(item.aligned_end_ms)}`,
                (item.network_finding_ids || []).join('、') || '无', (item.player_stalls || []).join('、') || '无', (item.rtmp_controls || []).join('、') || '无', (item.low_throughput_seconds || []).join('、') || '无', item.message])));
        content.append(element('p', `对齐方式 ${quality.correlation_alignment || '未记录'}；同期关系不是确定根因。`, 'timing-note muted'));
    }
    if (quality.truncated) content.append(element('p', '部分检测区间已达到保存上限。', 'timing-note muted'));
    const limits = element('ul', '', 'stall-assessment-limitations');
    for (const limitation of quality.limitations || []) limits.append(element('li', limitation));
    content.append(element('h4', '证据限制'), limits,
        element('p', `检测规则 ${quality.version} · 源流 SHA-256 ${quality.source_artifact?.sha256 || '未记录'}`, 'timing-note muted'));
}

function renderMetricsFragment(entries) {
    const list = document.createElement('dl');
    list.className = 'metrics';
    for (const [label, value] of entries) {
        const item = document.createElement('div');
        const term = element('dt', label);
        const description = element('dd', value);
        item.append(term, description);
        list.append(item);
    }
    return list;
}

node('analyze-content-quality').addEventListener('click', () => {
    node('content-quality').hidden = !node('content-quality').hidden;
});
node('run-content-quality').addEventListener('click', async () => {
    if (!currentID) return;
    const button = node('run-content-quality');
    button.disabled = true;
    button.textContent = '正在本地解码检测…';
    try {
        const quality = await api(runPath(currentID) + '/content-quality', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
        if (currentReport) currentReport.content_quality = quality;
        renderContentQuality(quality);
    } catch (err) { message(err.message, true); }
    finally { button.disabled = false; renderContentQuality(currentReport?.content_quality); }
});

function selectedShareCaseIDs() {
    return [...node('share-cases').querySelectorAll('input[type="checkbox"]:checked')].map(input => input.value);
}

function updateSharePreview() {
    if (!currentReport) return;
    const selected = new Set(selectedShareCaseIDs());
    let mediaBytes = 0, assetCount = 0;
    for (const item of currentReport.stall_investigations || []) {
        if (!selected.has(item.id)) continue;
        for (const asset of item.media?.assets || []) { mediaBytes += asset.bytes || 0; assetCount++; }
    }
    const includeStream = node('share-include-stream').checked;
    const rawBytes = includeStream ? currentReport.manifest.stream.bytes : 0;
    node('share-preview').textContent = `内容：report.json、report.html、share-manifest.json · ${selected.size} 个案例 / ${assetCount} 个媒体文件` +
        (includeStream ? ` · 完整原始流 ${bytes(rawBytes)}` : ' · 不含完整原始流') + ` · 媒体与原始流合计约 ${bytes(mediaBytes + rawBytes)}`;
}

function prepareSharePanel(report) {
    const cases = node('share-cases');
    cases.replaceChildren();
    const investigations = report.stall_investigations || [];
    if (!investigations.length) cases.append(element('p', '当前没有异常补查产物，将只导出报告。', 'timing-note muted'));
    for (const item of investigations) {
        const label = element('label', '', 'playback-option');
        const input = document.createElement('input');
        input.type = 'checkbox';
        input.value = item.id;
        input.checked = true;
        input.addEventListener('change', updateSharePreview);
        const assets = item.media?.assets || [];
        label.append(input, document.createTextNode(`${item.id} · ${assets.length} 个文件 · ${bytes(assets.reduce((sum, asset) => sum + (asset.bytes || 0), 0))}`));
        cases.append(label);
    }
    const include = node('share-include-stream');
    include.checked = false;
    include.disabled = !report.manifest.stream.bytes;
    include.onchange = updateSharePreview;
    updateSharePreview();
}

node('share-report').addEventListener('click', () => {
    const panel = node('share-panel');
    panel.hidden = !panel.hidden;
    if (!panel.hidden && currentReport) prepareSharePanel(currentReport);
});
node('export-share').addEventListener('click', async () => {
    if (!currentID) return;
    const button = node('export-share');
    button.disabled = true;
    try {
        const response = await fetch(apiRoot + runPath(currentID) + '/share', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ case_ids: selectedShareCaseIDs(), include_stream: node('share-include-stream').checked }),
        });
        if (!response.ok) {
            const result = await response.json().catch(() => ({}));
            throw new Error(result.error || `分享包生成失败（HTTP ${response.status}）`);
        }
        const url = URL.createObjectURL(await response.blob());
        const link = document.createElement('a');
        link.href = url;
        link.download = `share-${currentID}.zip`;
        link.click();
        setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (err) { message(err.message, true); }
    finally { button.disabled = false; }
});

const comparisonStates = { improved: '改善', worse: '恶化', unchanged: '不变', changed: '有变化', unobserved: '未观测' };

function comparisonValue(value, unit) {
    if (value == null) return '未观测';
    if (unit === 'ms') return time(value);
    if (unit === 'bytes') return bytes(value);
    return Number(value).toLocaleString();
}

function prepareReportComparison(report) {
    const select = node('comparison-baseline');
    select.replaceChildren();
    const candidates = runs.filter(run => run.id !== currentID && run.report_ready && run.protocol === report.manifest.protocol);
    for (const run of candidates) {
        const when = new Date(run.created_at).toLocaleString();
        select.append(new Option(`${when} · ${diagnosticOrigin(run.protocol, run.origin) || run.origin || run.id}`, run.id));
    }
    node('run-comparison').disabled = candidates.length === 0;
    if (!candidates.length) select.append(new Option('没有同协议的历史报告', ''));
    else {
        const sameOrigin = candidates.find(run => run.origin === report.manifest.observation.origin);
        if (sameOrigin) select.value = sameOrigin.id;
    }
}

function renderComparison(result) {
    currentComparison = result;
    node('export-comparison').hidden = false;
    const content = node('comparison-result');
    content.replaceChildren();
    const warnings = element('ul', '', 'comparison-warnings');
    for (const warning of result.warnings || []) warnings.append(element('li', warning));
    if (warnings.childElementCount) content.append(warnings);
    const rows = (result.metrics || []).map(metric => {
        const state = element('span', comparisonStates[metric.state] || metric.state, 'comparison-state');
        state.dataset.state = metric.state;
        return [metric.label, comparisonValue(metric.baseline, metric.unit), comparisonValue(metric.current, metric.unit),
            metric.delta == null ? '未观测' : (metric.delta > 0 ? '+' : '') + comparisonValue(metric.delta, metric.unit), state];
    });
    content.append(renderTable(['指标', '基线', '当前', '差值（当前−基线）', '判断'], rows));
    const changes = element('p', `新增规则：${(result.new_findings || []).join('、') || '无'} · 未再触发：${(result.resolved_findings || []).join('、') || '无'}`, 'timing-note muted');
    content.append(changes);
}

node('compare-report').addEventListener('click', () => {
    const section = node('report-comparison');
    section.hidden = !section.hidden;
    if (!section.hidden && currentReport) prepareReportComparison(currentReport);
});
node('run-comparison').addEventListener('click', async () => {
    const baseline = node('comparison-baseline').value;
    if (!currentID || !baseline) return;
    const button = node('run-comparison');
    button.disabled = true;
    try {
        const result = await api(runPath(currentID) + '/compare?baseline=' + encodeURIComponent(baseline), { cache: 'no-store' });
        renderComparison(result);
    } catch (err) { message(err.message, true); }
    finally { button.disabled = false; }
});
node('export-comparison').addEventListener('click', () => {
    if (!currentComparison) return;
    const blob = new Blob([JSON.stringify(currentComparison, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `comparison-${currentComparison.baseline.id}-${currentComparison.current.id}.json`;
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
});

function renderReportAssessment(assessment, findings) {
    const section = node('report-assessment');
    section.hidden = !assessment;
    const content = node('report-assessment-content');
    content.replaceChildren();
    if (!assessment) return;
    if (assessment.conclusion) content.append(element('p', assessment.conclusion, 'stall-assessment-conclusion'));
    content.append(element('p', '无需大模型即可生成；方向只引用本次已保存的规则证据，仍需按建议验证根因。', 'timing-note muted'));
    const known = new Set((findings || []).map(finding => finding.id));
    for (const direction of assessment.directions || []) {
        const block = element('section', '', 'stall-direction');
        block.dataset.state = direction.status;
        const title = element('h4', direction.title || '待核对方向');
        title.append(element('span', stallDirectionStates[direction.status] || '待核对', 'status direction-state'));
        block.append(title, element('p', direction.message || ''));
        const evidence = element('p', '关联证据 ');
        for (const id of direction.evidence_ids || []) {
            if (known.has(id)) {
                const link = element('a', id + ' ');
                link.href = '#' + encodeURIComponent(id);
                link.addEventListener('click', event => { event.preventDefault(); showFinding(id); });
                evidence.append(link);
            } else evidence.append(document.createTextNode(id + '（未找到记录） '));
        }
        block.append(evidence);
        if (direction.next_checks?.length) {
            const checks = element('ul', '', 'stall-next-checks');
            for (const check of direction.next_checks) checks.append(element('li', check));
            block.append(element('p', '建议验证', 'timing-note muted'), checks);
        }
        content.append(block);
    }
    if (assessment.limitations?.length) {
        const limits = element('ul', '', 'stall-assessment-limitations');
        for (const limitation of assessment.limitations) limits.append(element('li', limitation));
        content.append(element('h4', '证据限制'), limits);
    }
    content.append(element('p', `排查规则 ${assessment.version}`, 'timing-note muted'));
}

function renderHLSABR(abr) {
    node('hls-abr-report').hidden = !abr;
    if (!abr) return;
    renderMetrics('hls-abr-metrics', [
        ['独立采样变体', abr.sampled_variant == null ? '未记录' : `#${abr.sampled_variant} · ${abr.sampled_bandwidth || '未声明'} bit/s`],
        ['ABR 切换次数', abr.switches?.length || 0], ['命中采样变体的切换', abr.matched_switches || 0],
    ]);
    node('hls-abr-switches').replaceChildren(renderTable(['采样时间', '档位', '带宽 (bit/s)', '分辨率', '最近播放请求', '请求时间差', '命中采样变体'],
        (abr.switches || []).map(item => [time(item.at_ms), item.level, item.bandwidth, item.width && item.height ? `${item.width}×${item.height}` : '未记录', item.nearest_player_request ? `#${item.nearest_player_request}` : '未关联', item.nearest_player_request ? time(item.request_delta_ms) : '—', item.matches_sampled_variant ? '是' : '否'])));
    node('hls-abr-boundary').textContent = abr.boundary;
}

function renderRTMPControl(control) {
    node('rtmp-control-report').hidden = !control;
    if (!control) return;
    renderMetrics('rtmp-control-metrics', [
        ['控制消息总数', control.total_messages], ['Chunk Size 变更', control.chunk_size_changes],
        ['ACK / Window 更新', `${control.acknowledgements} / ${control.window_updates}`],
        ['Peer Bandwidth 更新', control.peer_bandwidth_updates],
        ['User Control / AMF 命令', `${control.user_control_events} / ${control.command_messages}`],
        ['未展示消息', control.truncated || 0],
    ]);
    node('rtmp-control-messages').replaceChildren(renderTable(['到达', '类型', '名称', '值', '事件', 'Level', 'Code'],
        (control.messages || []).map(message => [time(message.arrival_ms), message.type, message.name || '—', message.value ?? '—', message.event ?? '—', message.level || '—', message.code || '—'])));
    node('rtmp-control-boundary').textContent = control.boundary;
}

function renderThroughput(report) {
    const throughput = report.throughput;
    node('throughput-report').hidden = !throughput;
    if (!throughput) return;
    renderMetrics('throughput-metrics', [
        ['平均网络吞吐', throughput.average_network_kbps.toFixed(1) + ' kb/s'],
        ['平均媒体码率', throughput.average_media_kbps.toFixed(1) + ' kb/s'],
        ['视频 / 音频媒体码率', `${throughput.average_video_kbps.toFixed(1)} / ${throughput.average_audio_kbps.toFixed(1)} kb/s`],
        ['每秒吞吐最低 / 最高', `${throughput.minimum_network_kbps.toFixed(1)} / ${throughput.maximum_network_kbps.toFixed(1)} kb/s`],
    ]);
    node('throughput-samples').replaceChildren(renderTable(['秒', '有效时长', '网络字节', '媒体字节', '视频字节', '音频字节', '网络吞吐'],
        (throughput.samples || []).map(sample => [sample.second, time(sample.duration_ms), bytes(sample.network_bytes), bytes(sample.media_bytes), bytes(sample.video_bytes), bytes(sample.audio_bytes), sample.network_kbps.toFixed(1) + ' kb/s'])));
    node('throughput-boundary').textContent = throughput.boundary;
}

function renderMediaToolCapability(name, capability) {
    const entries = [[name + ' 路径', capability.path || '未找到'], [name + ' 版本', capability.version || capability.error || '不可用']];
    if (name === 'FFmpeg') entries.push(['编码器', `AAC ${capability.aac ? '支持' : '无'} · H.264 ${capability.h264 ? '支持' : '无'} · HEVC ${capability.hevc ? '支持' : '无'} · Opus ${capability.opus ? '支持' : '无'}`],
        ['内容检测过滤器', `黑帧 ${capability.blackdetect ? '支持' : '无'} · 冻结 ${capability.freezedetect ? '支持' : '无'} · 静音 ${capability.silencedetect ? '支持' : '无'}`]);
    return renderMetricsFragment(entries);
}

function renderMediaTools(status) {
    node('media-ffmpeg-path').value = status.settings.ffmpeg_path || '';
    node('media-ffprobe-path').value = status.settings.ffprobe_path || '';
    const content = node('media-tools-capabilities');
    content.replaceChildren(renderMediaToolCapability('FFmpeg', status.ffmpeg), renderMediaToolCapability('FFprobe', status.ffprobe));
}

async function refreshMediaTools() {
    const status = await api('/media-tools', { cache: 'no-store' });
    renderMediaTools(status);
}
node('media-tools-settings').addEventListener('click', async () => {
    const panel = node('media-tools-panel');
    if (panel.open) {
        panel.close();
        return;
    }
    panel.showModal();
    try { await refreshMediaTools(); } catch (err) { message(err.message, true); }
});
node('close-media-tools').addEventListener('click', () => { node('media-tools-panel').close(); });
node('media-tools-panel').addEventListener('click', event => {
    if (event.target === event.currentTarget) event.currentTarget.close();
});
node('media-tools-panel').addEventListener('close', () => { node('media-tools-settings').focus(); });
node('media-tools-form').addEventListener('submit', async event => {
    event.preventDefault();
    const button = event.currentTarget.querySelector('button[type="submit"]');
    button.disabled = true;
    try {
        const status = await api('/media-tools', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ffmpeg_path: node('media-ffmpeg-path').value.trim(), ffprobe_path: node('media-ffprobe-path').value.trim() }),
        });
        renderMediaTools(status);
        message('FFmpeg 工具路径已验证并保存。');
    } catch (err) { message(err.message, true); await refreshMediaTools().catch(() => {}); }
    finally { button.disabled = false; }
});

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
    if (report.manifest.analysis_thresholds) {
        const thresholds = report.manifest.analysis_thresholds;
        values.push(['诊断场景', report.manifest.analysis_profile || 'standard'], ['首帧 / 空洞 / DTS / GOP 阈值', `${thresholds.first_frame_warn_ms} / ${thresholds.video_gap_warn_ms} / ${thresholds.dts_gap_warn_ms} / ${thresholds.gop_warn_ms} ms`]);
    }
    const streamLimit = report.manifest.stream_limit_bytes || 128 * 1024 * 1024;
    values.push(
        [protocols[protocol]?.dump || '原始流', bytes(report.manifest.stream.bytes)],
        ['本次原始流上限', bytes(streamLimit)],
        ['原始产物状态', observed.end_reason === 'size_limit' ? '达到上限，已保留可复盘前缀' : (endLabels[observed.end_reason] || observed.end_reason || '未记录')],
    );
    renderMetrics('metrics', values);
    renderThroughput(report);
    renderRTMPControl(report.rtmp_control);
    renderHLSABR(report.hls_abr);
    renderAutomaticInvestigation(report);
    renderSamplingTiming(report, requests);
    renderRTSPReport(report);
    renderPlayerReport(report.player, observed.elapsed_ms);
    renderPlayerNetwork(report);
    renderStallInvestigations(report);
    renderTransport(protocol, report.mpegts);
    renderTrace(report.findings, observed.elapsed_ms);
    renderContentQuality(report.content_quality);
    renderReportAssessment(report.assessment, report.findings);
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
        (!currentModel.model || result.model === currentModel.model) && result.report_input_evidence?.requested_mode === (currentModel.image_input || 'auto');
    button.hidden = !currentModel || (Boolean(cached) && !desktop);
    button.textContent = result ? '用当前模型重新解读' : '生成解读';
    if (automaticWorkflowActive()) { button.disabled = true; button.dataset.automaticDisabled = 'true'; node('model-status').textContent = currentModel ? '自动流程将统一生成整体汇总；也可停止自动流程后手动解读。' : '自动流程正在提取证据；未配置模型时会保留结果，可稍后生成整体汇总。'; return; }
    if (button.dataset.automaticDisabled) { delete button.dataset.automaticDisabled; button.disabled = false; }
    if (button.disabled) return;
    node('model-status').textContent = currentModel
        ? (button.hidden ? '' : `当前模型 ${modelLabel(currentModel)}。${currentModel.image_input === 'text' ? '汇总与补查仅发送时序文本。' : '汇总按关键异常选择最多 3 张截图与时序；图片不支持时回退文本。'}`)
        : '模型服务尚未配置。';
}

async function refreshModelConfiguration() {
    const request = ++modelConfigRequest;
    const config = await api('/config', { cache: 'no-store' });
    if (request !== modelConfigRequest) return;
    currentModel = config.model_configured
        ? { provider: config.model_provider || 'openai-compatible', model: config.model_name || '', image_input: config.model_image_input === 'text' ? 'text' : 'auto' }
        : null;
    if (Number.isSafeInteger(config.capture_limit_bytes) && config.capture_limit_bytes > 0) {
        node('capture-duration-hint').textContent = `采样 1–300 秒 · 单次原始流上限 ${bytes(config.capture_limit_bytes)} · 达到上限保留可复盘前缀`;
    }
    if (!node('report').hidden) renderModelStatus();
}

function reportImageReferenceItem(reference, audit) {
    const item = (currentReport?.stall_investigations || []).find((candidate) => candidate.id === reference.case_id);
    if (!item || !(audit.cases || []).some((candidate) => candidate.case_id === item.id && candidate.player_revision === item.player_revision)) return null;
    return modelImageAsset(item, reference) ? item : null;
}
function reportImageCitationTarget(id, result) {
    if (!/^S\d{3}-I\d{3}$/.test(id)) return id;
    const audit = result?.report_input_evidence;
    if (audit?.mode !== 'vision') return null;
    const reference = (audit.images || []).find((image) => image.id === id);
    return reference && reportImageReferenceItem(reference, audit) ? 'summary-image-' + id : null;
}
function renderReportInputEvidence(result) {
    const block = element('section', '', 'report-input-evidence model-input-evidence');
    const audit = result.report_input_evidence;
    if (!audit) {
        block.append(element('p', '该历史汇总未记录截图输入方式。', 'timing-note muted report-input-legacy'));
        return block;
    }
    const images = (audit.images || []).slice(0, 3), attempted = (audit.attempted_images || []).slice(0, 3);
    const scope = audit.cases || [], selected = audit.mode === 'vision' ? images : attempted;
    const selectedCases = new Set(selected.map((image) => image.case_id));
    block.append(element('p', audit.mode === 'vision' ? `本次为整份报告图文解读 · 全部 ${scope.length} 个补查案例的时序，选取 ${selectedCases.size} 个案例 ${images.length} 张截图` : `本次为整份报告文本解读 · 全部 ${scope.length} 个补查案例的时序`, 'model-input-mode'));
    const context = [`请求设置 ${audit.requested_mode === 'text' ? '仅文本' : '自动图文'}`, audit.player_revision > 0 ? `播放器记录修订 #${audit.player_revision}` : '未记录播放器观测'];
    if (audit.fallback_code) context.push(modelInputFallbackLabels[audit.fallback_code] || '未记录可识别的回退原因');
    block.append(element('p', context.join(' · '), 'timing-note muted'));
    if (attempted.length) block.append(element('p', `已尝试发送 ${attempted.length} 张截图（来自 ${selectedCases.size} 次异常）；图文请求不支持，最终汇总基于文本。`, 'timing-note model-input-fallback'));
    else if (audit.mode === 'text') block.append(element('p', '本次仅发送时序文本，未发送截图像素。', 'timing-note muted'));
    if (selected.length) {
        block.append(element('p', '每次汇总最多使用 3 张截图；按关键异常顺序优先取各案例附近截图，再补充上下文，重复画面按文件指纹去重。', 'timing-note muted'));
        const omitted = scope.filter((item) => !selectedCases.has(item.case_id));
        if (omitted.length) block.append(element('p', `其余 ${omitted.length} 个案例未提供截图，时序信息仍参与汇总；未选取的案例：${omitted.map((item) => item.case_id).join('、')}。`, 'timing-note muted report-input-omitted'));
    }
    if (!scope.length) block.append(element('p', '当前没有可用于汇总的补查案例，可先补查异常以提供截图；本次仍采用报告指标与规则证据。', 'timing-note muted'));
    if (scope.length) {
        const cases = element('p', '汇总案例：', 'timing-note muted');
        for (const item of scope) {
            const link = element('a', `${item.case_id}${selectedCases.has(item.case_id) ? audit.mode === 'vision' ? '（截图）' : '（曾尝试截图）' : '（时序）'} `); link.href = '#investigation-' + encodeURIComponent(item.case_id);
            link.addEventListener('click', (event) => { event.preventDefault(); showFinding('investigation-' + item.case_id); }); cases.append(link);
        }
        block.append(cases);
    }
    if (images.length) block.append(element('p', '截图是各案例的源流内容，需结合时序和案例边界验证；不能据此重现或证明本机画面冻结。', 'timing-note muted'));
    const fingerprints = [], anchors = new Set();
    for (const [references, used] of [[images, true], [attempted, false]]) {
        if (!references.length) continue;
        block.append(element('p', used ? '汇总实际采用的截图' : '汇总已尝试发送的截图', 'timing-note'));
        const list = element('ul', '', 'model-input-images');
        for (const reference of references) {
            const item = reportImageReferenceItem(reference, audit), asset = item && modelImageAsset(item, reference);
            const row = element('li', ''), id = element('strong', reference.id || '截图编号未记录');
            if (used && audit.mode === 'vision' && asset && !anchors.has(reference.id)) { id.id = 'summary-image-' + reference.id; anchors.add(reference.id); }
            row.append(id, document.createTextNode(` · ${reference.case_id} · ${modelImageRole(reference.name)}${reference.media_time_ms == null ? '' : ` · 媒体时间 ${time(reference.media_time_ms)}`} · ${reference.bytes == null ? '大小未记录' : bytes(reference.bytes)} `));
            const url = asset && investigationAssetURL(item, asset);
            if (url) { const link = element('a', '查看本地截图'); link.href = url; row.append(link); } else row.append(element('span', '本地截图引用未匹配', 'muted'));
            list.append(row); fingerprints.push([reference.id || '截图', reference.sha256 || '未记录']);
        }
        block.append(list);
    }
    if (fingerprints.length || audit.report_sha256) {
        const details = element('details', '', 'model-input-identities'); details.append(element('summary', '汇总输入与截图指纹（SHA-256）'));
        if (audit.report_sha256) details.append(element('p', `汇总输入摘要 · ${audit.report_sha256}`, 'timing-note muted'));
        for (const [id, hash] of fingerprints) details.append(element('p', `${id} · ${hash}`, 'timing-note muted')); block.append(details);
    }
    return block;
}
const reportPriorityLabels = { P1: '优先核对', P2: '随后核对', P3: '补充核对' };
const orderedReportHypotheses = (hypotheses) => (hypotheses || []).map((item, index) => ({ item, index })).sort((a, b) => ({ P1: 0, P2: 1, P3: 2 }[a.item.priority] ?? 3) - ({ P1: 0, P2: 1, P3: 2 }[b.item.priority] ?? 3) || a.index - b.index).map(({ item }) => item);

function renderExplanation(result) {
    currentExplanation = result;
    renderModelStatus();
    const content = node('explanation'); content.replaceChildren();
    const heading = node('report-model-heading');
    if (heading) heading.textContent = result?.report_input_evidence?.mode === 'vision' ? '整份报告汇总（图文解读）' : result?.report_input_evidence?.mode === 'text' ? '整份报告汇总（文本解读）' : '整份报告汇总';
    if (!result) return;
    content.append(element('p', `解读来源 ${modelLabel(result)}`, 'model-source muted'), renderReportInputEvidence(result), element('p', result.summary));
    const ordered = orderedReportHypotheses(result.hypotheses);
    if (ordered.some((item) => reportPriorityLabels[item.priority])) content.append(element('p', '优先级表示建议核查顺序，不代表已确认根因或业务严重度。', 'timing-note muted'));
    const hypotheses = element('ol', '', 'report-next-checks');
    for (const item of ordered) {
        const row = element('li', '');
        if (reportPriorityLabels[item.priority]) row.append(element('span', `${item.priority} · ${reportPriorityLabels[item.priority]}`, 'model-priority'));
        row.append(element('strong', `待验证 · ${item.claim} `));
        for (const id of item.evidence_ids || []) {
            const target = reportImageCitationTarget(id, result);
            if (!target) { row.append(element('span', `${id}（未匹配本次截图输入） `, 'muted')); continue; }
            const link = element('a', id + ' '); link.href = '#' + encodeURIComponent(target);
            link.addEventListener('click', (event) => { event.preventDefault(); showFinding(target); }); row.append(link);
        }
        row.append(element('p', `下一步：${item.next_check}`)); hypotheses.append(row);
    }
    if (hypotheses.childElementCount) content.append(hypotheses);
}

function renderDetail(detail) {
    const protocol = detail.report?.manifest.protocol || detail.protocol || 'http-flv';
    const mode = detail.mode === 'replay' ? '复盘结果' : '采样结果';
    node('result-heading').textContent = `${protocolLabel(protocol, detail.origin)} ${mode}`;
    currentStatus = detail.status;
    currentMode = detail.mode || '';
    currentAutomaticRunning = detail.automatic_running === true;
    node('stop').hidden = currentStatus !== 'running';
    const protectedRun = detail.protected === true;
    node('protect-run').hidden = false;
    node('protect-run').textContent = protectedRun ? '取消重要证据保护' : '保护当前证据';
    node('delete-run').hidden = protectedRun || currentStatus === 'running';
    node('report').hidden = !detail.report;
    node('compare-report').hidden = !detail.report;
    node('share-report').hidden = !detail.report;
    node('analyze-content-quality').hidden = !detail.report;
    const quality = node('content-quality');
    if (quality.dataset.run !== detail.id) {
        quality.dataset.run = detail.id;
        quality.hidden = !detail.report?.content_quality;
    }
    const share = node('share-panel');
    if (share.dataset.run !== detail.id) {
        share.dataset.run = detail.id;
        share.hidden = true;
    }
    const comparison = node('report-comparison');
    if (comparison.dataset.run !== detail.id) {
        comparison.dataset.run = detail.id;
        comparison.hidden = true;
        node('comparison-result').replaceChildren();
        node('export-comparison').hidden = true;
        currentComparison = null;
    }
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
    node('rtsp-replay').hidden = node('rtsp-replay-stop').hidden = !detail.rtsp_replay_available;
    const replay = node('flv-replay');
    replay.hidden = !detail.flv_replay_available;
    replay.dataset.url = detail.flv_replay_available ? new URL(base + '/replay.flv', location.origin).href : '';
    replay.dataset.seconds = Math.min(300, Math.max(1, Math.ceil((observed.elapsed_ms + 1000) / 1000)));
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
            if (detail.automatic_running || (detail.mode !== 'replay' && detail.report?.stall_investigations?.some((item) => ['queued', 'running'].includes(item.status)))) activePoll = setTimeout(() => refreshDetail(id), 850);
        }
    } catch (err) {
        if (id !== currentID || request !== detailRequest) return;
        message(err.message, true);
        if (currentAutomaticRunning || activePlayer?.id === id || (currentMode !== 'replay' && currentReport?.stall_investigations?.some((item) => ['queued', 'running'].includes(item.status)))) activePoll = setTimeout(() => refreshDetail(id), 1000);
    }
}

function selectRun(id) {
    if (activePlayer && activePlayer.id !== id) stopPlayback(activePlayer);
    if (node('player-preview').dataset.runId !== id) node('player-preview').hidden = true;
    currentID = id;
    currentStatus = '';
    currentMode = '';
    currentAutomaticRunning = false;
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

function updateAutomaticOption() { const playback = node('analyze-playback'); node('auto-investigate').disabled = playback.disabled || !playback.checked; }
node('analyze-playback').addEventListener('change', updateAutomaticOption);
node('analysis-profile').addEventListener('change', () => { node('custom-thresholds').hidden = node('analysis-profile').value !== 'custom'; });
node('protocol').addEventListener('change', () => {
    const protocol = node('protocol').value;
    const selected = protocols[protocol];
    node('url').placeholder = selected.example;
    node('url-label').textContent = selected.receive ? '监听地址' : '播放地址';
    const hint = node('url-hint');
    hint.hidden = !selected.receive && !selected.nativeRTSP;
    hint.textContent = selected.nativeRTSP ? '填写摄像头或 NVR 的播放地址，支持 URL 用户信息认证。采用 TCP 交织传输；RTSPS 使用 TLS 并校验服务端证书。报告仅显示协议和主机。' : selected.receive ? `填写本机 IPv4 和端口。组播示例 ${protocol.split('-')[0]}://239.1.1.1:5000?interface=en0` : '';
    const playback = node('analyze-playback');
    playback.disabled = false;
    node('playback-capability-note').hidden = !selected.nativeRTSP;
    const httpAccess = ['http-flv', 'hls', 'http-ts'].includes(protocol);
    node('http-access-options').hidden = !httpAccess && !['rtmp', 'rtsps'].includes(protocol);
    node('reconnect-options').hidden = !['http-flv', 'rtmp'].includes(protocol);
    node('hls-variant-option').hidden = protocol !== 'hls';
    node('rolling-options').hidden = !['http-flv', 'rtmp', 'hls'].includes(protocol);
    for (const id of ['http-headers', 'http-proxy-url']) {
        node(id).hidden = !httpAccess;
        document.querySelector(`label[for="${id}"]`).hidden = !httpAccess;
    }
    updateRTSPTransport();
    updateAutomaticOption();
});
node('rtsp-transport').addEventListener('change', updateRTSPTransport);
updateRTSPTransport();
updateAutomaticOption();
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
    updateAutomaticOption();
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
node('flv-replay').addEventListener('click', () => {
    const replay = node('flv-replay');
    if (!replay.dataset.url) return;
    node('protocol').value = 'http-flv';
    node('protocol').dispatchEvent(new Event('change'));
    node('url').value = replay.dataset.url;
    node('seconds').value = replay.dataset.seconds;
    node('url').focus();
    node('url').select();
    message('已填入本机回放地址，可复制到其他本机播放器。每个连接从头按原采样节奏回放。');
});
async function changeRTSPReplay(start) {
    const id = currentID;
    const button = node(start ? 'rtsp-replay' : 'rtsp-replay-stop');
    if (!id || button.disabled) return;
    button.disabled = true;
    try {
        const source = await api(runPath(id) + '/rtsp-replay/' + (start ? 'start' : 'stop'), {method: 'POST'});
        if (currentID !== id) return;
        if (!start) {
            message('本机 RTSP 回放源已停止。');
            return;
        }
        node('protocol').value = 'rtsp';
        node('protocol').dispatchEvent(new Event('change'));
        node('rtsp-transport').value = source.transport === 'udp' ? 'udp' : 'tcp';
        node('url').value = source.url;
        node('seconds').value = Math.min(300, Math.max(1, Math.ceil((source.duration_ms + 1500) / 1000)));
        node('url').focus();
        node('url').select();
        message('本机 RTSP 回放地址已填入采样表单。每个客户端重新建立会话并独立按原节奏收包。');
    } catch (error) {
        if (currentID === id) message(error.message, true);
    } finally {
        button.disabled = false;
    }
}
node('rtsp-replay').addEventListener('click', () => { void changeRTSPReplay(true); });
node('rtsp-replay-stop').addEventListener('click', () => { void changeRTSPReplay(false); });
function captureAccessOptions(protocol) {
    const httpAccess = ['http-flv', 'hls', 'http-ts'].includes(protocol);
    const tlsAccess = httpAccess || ['rtmp', 'rtsps'].includes(protocol);
    if (!tlsAccess) return {};
    const headers = {};
    for (const line of node('http-headers').value.split(/\r?\n/)) {
        if (!line.trim()) continue;
        const separator = line.indexOf(':');
        if (separator <= 0) throw new Error('自定义请求头须使用 Name: value 格式。');
        headers[line.slice(0, separator).trim()] = line.slice(separator + 1).trim();
    }
    if (Object.keys(headers).length > 16) throw new Error('自定义请求头最多 16 个。');
    return {
        headers: httpAccess ? headers : undefined,
        proxy_url: httpAccess ? node('http-proxy-url').value.trim() || undefined : undefined,
        ca_certificate: node('http-ca-certificate').value.trim() || undefined,
    };
}
function renderBatchResult(batch) {
    node('batch-results').hidden = false;
    node('batch-results-title').textContent = '批量诊断结果';
    activeSessionGroupID = '';
    node('batch-status').textContent = `批次 ${batch.id} · ${statusLabels[batch.status] || batch.status} · ${batch.runs.length} 路`;
    node('stop-batch').hidden = batch.status !== 'running';
    const runsContent = node('batch-runs');
    runsContent.replaceChildren();
    for (const run of batch.runs) {
        const row = element('div', '', 'batch-run');
        const link = element('button', `${protocolLabel(run.protocol, run.origin)} · ${diagnosticOrigin(run.protocol, run.origin) || run.id}`, 'secondary');
        link.type = 'button';
        link.addEventListener('click', () => selectRun(run.id));
        const state = element('span', statusLabels[run.status] || run.status, 'status');
        state.dataset.status = run.status;
        row.append(link, state, element('code', run.id));
        runsContent.append(row);
    }
    const comparisons = node('batch-comparisons');
    comparisons.replaceChildren();
    for (const warning of batch.warnings || []) comparisons.append(element('p', warning, 'comparison-warnings'));
    for (const item of batch.comparisons || []) {
        const block = element('section', '', 'batch-comparison');
        block.append(element('h3', `${item.current_id} 对比基线 ${item.baseline_id}`));
        for (const warning of item.comparison.warnings || []) block.append(element('p', warning, 'comparison-warnings'));
        const rows = (item.comparison.metrics || []).map(metric => [metric.label, comparisonValue(metric.baseline, metric.unit), comparisonValue(metric.current, metric.unit),
            metric.delta == null ? '未观测' : (metric.delta > 0 ? '+' : '') + comparisonValue(metric.delta, metric.unit), comparisonStates[metric.state] || metric.state]);
        block.append(renderTable(['指标', '基线', '当前', '差值', '判断'], rows));
        comparisons.append(block);
    }
    if (batch.status !== 'running' && !(batch.comparisons || []).length) comparisons.append(element('p', '没有可自动对比的同协议报告。', 'muted'));
}

async function refreshBatch(id) {
    clearTimeout(activeBatchPoll);
    try {
        const batch = await api('/batches/' + encodeURIComponent(id), { cache: 'no-store' });
        if (activeBatchID !== id) return;
        renderBatchResult(batch);
        if (batch.status === 'running') activeBatchPoll = setTimeout(() => refreshBatch(id), 1000);
        else await refreshHistory();
    } catch (err) { message(err.message, true); }
}

node('batch-sample').addEventListener('submit', async event => {
    event.preventDefault();
    const button = event.currentTarget.querySelector('button[type="submit"]');
    button.disabled = true;
    try {
        const protocol = node('batch-protocol').value;
        const urls = node('batch-urls').value.split(/\r?\n/).map(value => value.trim()).filter(Boolean);
        if (urls.length < 2 || urls.length > 4) throw new Error('请输入 2 至 4 路地址。');
        const seconds = node('batch-seconds').valueAsNumber;
        const access = captureAccessOptions(protocol);
        const samples = urls.map(url => ({ protocol, url, seconds, ...access }));
        const batch = await api('/batches', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ samples }) });
        activeBatchID = batch.id;
        renderBatchResult(batch);
        await refreshHistory();
        await refreshBatch(batch.id);
    } catch (err) { message(err.message, true); }
    finally { button.disabled = false; }
});
node('stop-batch').addEventListener('click', async () => {
    try {
        if (activeSessionGroupID && !node('stop-batch').hidden) {
            await api('/sessions/' + encodeURIComponent(activeSessionGroupID) + '/stop', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
            await refreshSessionGroup(activeSessionGroupID);
            return;
        }
        if (!activeBatchID) return;
        await api('/batches/' + encodeURIComponent(activeBatchID) + '/stop', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' });
        await refreshBatch(activeBatchID);
    } catch (err) { message(err.message, true); }
});

async function refreshSessionGroup(id) {
    try {
        const group = await api('/sessions/' + encodeURIComponent(id), { cache: 'no-store' });
        node('batch-results').hidden = false;
        node('batch-results-title').textContent = group.rolling ? '滚动分段会话' : '重连会话组';
        activeSessionGroupID = group.id;
        node('stop-batch').hidden = !group.rolling || group.status !== 'running';
        node('batch-status').textContent = `会话 ${group.id} · ${group.status === 'running' ? '采样中' : '已结束'} · ${group.runs.length} 段${group.rolling ? ` · 每段 ${group.segment_seconds} 秒 / 保留最近 ${group.retain_segments} 段` : ''}`;
        const content = node('batch-runs');
        content.replaceChildren();
        node('batch-comparisons').replaceChildren();
        group.runs.forEach((run, index) => {
            const row = element('div', '', 'batch-run');
            const button = element('button', `第 ${index + 1} 段 · ${run.id}`, 'secondary');
            button.type = 'button';
            button.addEventListener('click', () => selectRun(run.id));
            const state = element('span', statusLabels[run.status] || run.status, 'status');
            state.dataset.status = run.status;
            row.append(button, state);
            content.append(row);
        });
        if (group.status === 'running') setTimeout(() => refreshSessionGroup(id), 1000);
        else await refreshHistory();
    } catch (err) { message(err.message, true); }
}

bindForm('sample', async () => {
    const startedAt = Date.now();
    const startedAtPerformanceMS = performance.now();
    const seconds = node('seconds').valueAsNumber;
    const playback = !node('analyze-playback').disabled && node('analyze-playback').checked;
    updateAutomaticOption();
    const autoInvestigate = playback && node('auto-investigate').checked;
    const url = node('url').value.trim();
    const localSimulator = Boolean(simulatorURL && simulatorURL === url);
    const hlsRecovery = localSimulator && launchOptions.get('hls_recovery') === '1';
    const hlsABR = localSimulator && launchOptions.get('hls_abr') === '1';
    const hlsLowLatency = localSimulator && launchOptions.get('hls_ll') === '1';
    const protocol = node('protocol').value;
    const run = await api('/runs', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ protocol, transport: protocols[protocol]?.nativeRTSP ? node('rtsp-transport').value : undefined, url, seconds, playback, auto_investigate: autoInvestigate, reconnect_attempts: ['http-flv', 'rtmp'].includes(protocol) ? node('reconnect-attempts').valueAsNumber : 0, reconnect_delay_ms: ['http-flv', 'rtmp'].includes(protocol) ? node('reconnect-delay').valueAsNumber : 0, hls_variant: protocol === 'hls' ? node('hls-variant').valueAsNumber : undefined, segment_seconds: ['http-flv', 'rtmp', 'hls'].includes(protocol) ? node('segment-seconds').valueAsNumber : 0, rolling_segments: ['http-flv', 'rtmp', 'hls'].includes(protocol) ? node('rolling-segments').valueAsNumber : 0, profile: node('analysis-profile').value, custom_thresholds: node('analysis-profile').value === 'custom' ? { first_frame_warn_ms: node('threshold-first-frame').valueAsNumber, video_gap_warn_ms: node('threshold-video-gap').valueAsNumber, dts_gap_warn_ms: node('threshold-dts-gap').valueAsNumber, gop_warn_ms: node('threshold-gop').valueAsNumber } : undefined, ...captureAccessOptions(protocol) }),
    });
    stopPlayback(activePlayer);
    startPlayback(run, startedAt, startedAtPerformanceMS, seconds, hlsRecovery, hlsABR, hlsLowLatency);
    if (run.session_group_id) void refreshSessionGroup(run.session_group_id);
    return run;
});
bindForm('import', () => {
    message('正在上传证据包并重跑规则…');
    return api('/import', {
        method: 'POST', headers: { 'Content-Type': 'application/zip' }, body: node('archive').files[0],
    });
});
node('protect-run').addEventListener('click', async () => {
    const run = runs.find(item => item.id === currentID);
    if (!run) return;
    try {
        await api(runPath(currentID) + '/protection', {
            method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ protected: !run.protected }),
        });
        await refreshHistory();
        await refreshDetail(currentID);
    } catch (err) { message(err.message, true); }
});

async function finishEvidenceDeletion(deletedIDs) {
    selectedRunIDs.clear();
    await refreshHistory();
    if (deletedIDs.has(currentID)) {
        currentID = '';
        node('report').hidden = true;
        node('protect-run').hidden = node('delete-run').hidden = true;
        node('summary').textContent = '选择一条记录查看报告。';
        history.replaceState(null, '', location.pathname + location.search);
        if (runs.length) await selectRun(runs[0].id);
    }
}

node('delete-run').addEventListener('click', async () => {
    if (!currentID || !confirm('确定删除当前排障记录及全部本地产物？此操作不可撤销。')) return;
    try {
        await api(runPath(currentID), { method: 'DELETE' });
        const deleted = currentID;
        message('排障记录已删除。');
        await finishEvidenceDeletion(new Set([deleted]));
    } catch (err) { message(err.message, true); }
});

node('delete-selected').addEventListener('click', async () => {
    const ids = [...selectedRunIDs];
    if (!ids.length || !confirm(`确定删除所选 ${ids.length} 条排障记录及全部本地产物？此操作不可撤销。`)) return;
    try {
        await api('/runs/delete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids }) });
        message(`已删除 ${ids.length} 条排障记录。`);
        await finishEvidenceDeletion(new Set(ids));
    } catch (err) { message(err.message, true); }
});

node('storage-prune').addEventListener('submit', async event => {
    event.preventDefault();
    const button = event.currentTarget.querySelector('button[type="submit"]');
    button.disabled = true;
    try {
        const retentionDays = node('retention-days').valueAsNumber || 0;
        const budgetMiB = node('storage-budget').valueAsNumber || 0;
        if (!retentionDays && !budgetMiB) throw new Error('请至少设置保留天数或磁盘预算。');
        if (!confirm('将按策略永久删除符合条件的未保护证据，是否继续？')) return;
        const result = await api('/storage/prune', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ retention_days: retentionDays, max_total_bytes: budgetMiB > 0 ? Math.round(budgetMiB * 1024 * 1024) : 0 }),
        });
        message(`清理完成：删除 ${result.deleted} 条记录，释放 ${bytes(result.deleted_bytes)}。`);
        await refreshHistory();
        if (!runs.some(run => run.id === currentID)) await finishEvidenceDeletion(new Set([currentID]));
    } catch (err) { message(err.message, true); }
    finally { button.disabled = false; }
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
    settingsButton?.addEventListener('click', () => {
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
            if (settingsButton) settingsButton.hidden = !event.data.modelSettings;
            if (!node('report').hidden) renderModelStatus();
        }
        if (event.data?.kind === 'model-settings-closed' && settingsButton && !settingsButton.hidden) settingsButton.focus();
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
