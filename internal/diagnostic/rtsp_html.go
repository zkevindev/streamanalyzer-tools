package diagnostic

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"strconv"
	"strings"
)

func isRTSPProtocol(protocol string) bool {
	return protocol == ProtocolRTSP || protocol == ProtocolRTSPS
}

func rtspFirstPacket(metrics *RTSPMetrics) *int64 {
	if metrics == nil {
		return nil
	}
	return metrics.FirstRTPMS
}

// Reports expose only the RTSP scheme and host, even when rendering an older
// caller-provided origin that still contains source credentials or a path.
func diagnosticReportOrigin(protocol, origin string) string {
	if !isRTSPProtocol(protocol) || origin == "" {
		return origin
	}
	value, err := url.Parse(origin)
	if err == nil && value.Host != "" && (strings.EqualFold(value.Scheme, ProtocolRTSP) || strings.EqualFold(value.Scheme, ProtocolRTSPS)) {
		return strings.ToLower(value.Scheme) + "://" + value.Host
	}
	return "主机未记录"
}

func diagnosticReportStageLabel(name string, legacyRTMP bool, protocols ...string) string {
	native := len(protocols) > 0 && isRTSPProtocol(protocols[0])
	switch name {
	case "rtsp_options":
		return "RTSP OPTIONS"
	case "rtsp_describe":
		return "RTSP DESCRIBE"
	case "rtsp_setup":
		return "RTSP SETUP"
	case "rtsp_play":
		return "RTSP PLAY"
	case "rtsp_receive":
		return "接收 RTSP/RTP 应用字节"
	}
	if native {
		switch name {
		case "options", "describe", "setup", "play", "teardown":
			return "RTSP " + strings.ToUpper(name)
		}
	}
	return stageLabel(name, legacyRTMP)
}

var rtspFailureText = map[string]string{
	"timeout": "超时", "canceled": "已取消", "dns_error": "DNS 失败", "tls_error": "TLS 失败", "connection_refused": "连接被拒绝", "network_error": "网络错误",
	"rtsp_error": "RTSP 错误", "rtsp_status": "RTSP 状态异常", "rtsp_auth_error": "RTSP 认证失败", "rtsp_transport_error": "RTSP 传输协商失败", "rtsp_parse_error": "RTSP 解析失败",
}

func rtspFailureLabel(code string) string {
	if code == "" {
		return "未记录"
	}
	if label := rtspFailureText[code]; label != "" {
		return label + " [" + code + "]"
	}
	return code
}

func rtspKindLabel(kind string) string {
	switch kind {
	case "video":
		return "视频"
	case "audio":
		return "音频"
	case "other":
		return "其他"
	default:
		return "未声明"
	}
}

func rtspPhaseDuration(stages []CaptureStage, name string) string {
	var values []string
	for _, stage := range stages {
		if stage.Name == name {
			values = append(values, fmt.Sprintf("%d ms", stage.EndMS-stage.StartMS))
		}
	}
	if len(values) == 0 {
		return "未观测"
	}
	return strings.Join(values, " / ")
}

func rtspRequestDuration(request RTSPRequestTiming) string {
	if request.EndMS < request.StartMS {
		return "未观测"
	}
	return fmt.Sprintf("%d ms", request.EndMS-request.StartMS)
}

func rtspObservedCounter(track RTSPTrackMetrics, count int) string {
	if !track.Received {
		return "未观测"
	}
	return strconv.Itoa(count)
}

func rtspTransportLabel(transport string) string {
	if transport == "udp" {
		return "UDP 单播 RTP/RTCP"
	}
	if transport == "tcp_interleaved" {
		return "TCP 交织 RTP/RTCP"
	}
	return transport
}

func rtspArtifactName(name string) string {
	if name == "udp-datagrams.bin" {
		return name
	}
	return "server-wire.bin"
}

var rtspReportPage = template.Must(template.New("rtsp").Funcs(template.FuncMap{
	"previewState": rtspPreviewStateLabel, "previewReason": rtspPreviewReasonLabel, "previewReasonFor": rtspPreviewReasonLabelFor, "ms": playerVideoObservedTime, "kind": rtspKindLabel, "failure": rtspFailureLabel, "duration": rtspRequestDuration, "counter": rtspObservedCounter, "transport": rtspTransportLabel,
}).Parse(`<article id="rtsp-report"><h2>RTSP 会话与 RTP 接收</h2>
{{if not .Metrics}}<p class="timing-note">原生 RTSP/RTP 采样不依赖浏览器预览。未记录转封装能力，播放器首屏、缓冲和呈帧指标以实际保存的播放器记录为准。</p>{{else}}{{if not .Metrics.Preview}}<p class="timing-note">原生 RTSP/RTP 采样不依赖浏览器预览。未记录转封装能力，播放器首屏、缓冲和呈帧指标以实际保存的播放器记录为准。</p>{{end}}{{end}}
<p class="timing-note">首音视频时间表示对应 RTP 包到达本接收点，不是解码首帧。SDP 声明的轨道与编码单独列出，不代表已收到或解码媒体。</p>
{{with .Metrics}}
{{with .Preview}}<h3>同次采样本机预览</h3><p class="timing-note">同次采样的 RTP 在本机转封装为 FLV，不另建拉流连接。音视频使用接收端 RTP 共同时间基准，不代表已验证源端音画同步；输出访问单元数不是解码或呈帧数。浏览器解码能力仍需实际播放验证。</p>
<dl><dt>本机转封装状态</dt><dd>{{previewState .State}}</dd><dt>预览原因</dt><dd>{{previewReasonFor . .ReasonCode}}</dd>
<dt>预览视频</dt><dd>{{if .VideoTrackID}}#{{.VideoTrackID}} · {{.VideoCodec}}{{else}}未选择{{end}}</dd><dt>视频说明</dt><dd>{{previewReasonFor . .VideoReason}}</dd>
<dt>预览音频</dt><dd>{{if .AudioTrackID}}#{{.AudioTrackID}} · {{.AudioCodec}}{{else}}未选择{{end}}</dd><dt>音频说明</dt><dd>{{if eq .AudioReason "codec_unsupported"}}音轨预览不支持，已静音；原始 RTP 仍保留{{else}}{{previewReasonFor . .AudioReason}}{{end}}</dd>
{{if eq .Version 2}}<dt>编码能力版本</dt><dd>{{.CodecCapabilitiesVersion}}</dd><dt>视频预览输出</dt><dd>{{if .VideoOutputCodec}}{{.VideoOutputCodec}}{{else}}未选择{{end}}</dd><dt>视频处理</dt><dd>{{if eq .VideoTransform "native_remux"}}原生转封装{{else}}{{if .VideoTransform}}{{.VideoTransform}}{{else}}未选择{{end}}{{end}}</dd><dt>音频预览输出</dt><dd>{{if .AudioOutputCodec}}{{.AudioOutputCodec}}{{else}}未选择{{end}}</dd><dt>音频处理</dt><dd>{{if eq .AudioTransform "native_remux"}}原生转封装{{else}}{{if .AudioTransform}}{{.AudioTransform}}{{else}}未选择{{end}}{{end}}</dd>{{end}}
<dt>视频访问单元</dt><dd>{{.VideoAUs}}</dd><dt>音频访问单元</dt><dd>{{.AudioAUs}}</dd><dt>转封装包错误</dt><dd>{{.PacketErrors}}</dd><dt>预览队列丢弃</dt><dd>{{.DroppedPackets}}</dd></dl>{{end}}
<dl><dt>传输</dt><dd>{{transport .Transport}}</dd><dt>应用字节边界</dt><dd>{{if .Secure}}TLS 解密后的 RTSP/RTP{{else}}RTSP/RTP 应用字节{{end}}</dd><dt>会话连接数</dt><dd>{{.Connections}}</dd>
<dt>DNS 阶段</dt><dd>{{$.DNS}}</dd><dt>TCP 阶段</dt><dd>{{$.TCP}}</dd><dt>TLS 阶段</dt><dd>{{if .Secure}}{{$.TLS}}{{else}}不适用（RTSP）{{end}}</dd>
<dt>RTP 包</dt><dd>{{.RTPPackets}}</dd><dt>RTCP 包</dt><dd>{{.RTCPPackets}}</dd><dt>RTP 字节</dt><dd>{{.RTPBytes}}</dd>
{{if eq .Transport "udp"}}<dt>收到 UDP 数据报</dt><dd>{{.UDPDatagrams}}</dd><dt>UDP 字节</dt><dd>{{.UDPBytes}}</dd><dt>符合协商来源</dt><dd>{{.AcceptedDatagrams}}</dd><dt>来源过滤</dt><dd>{{.FilteredDatagrams}}</dd><dt>超过媒体库缓冲的数据报</dt><dd>{{.UDPOversizeDatagrams}}</dd><dt>首个 RTCP 到达</dt><dd>{{ms .FirstRTCPMS}}</dd>{{end}}
<dt>首个 RTP 到达</dt><dd>{{ms .FirstRTPMS}}</dd><dt>首个视频 RTP 到达</dt><dd>{{ms .FirstVideoRTPMS}}</dd><dt>首个音频 RTP 到达</dt><dd>{{ms .FirstAudioRTPMS}}</dd>
<dt>格式异常包</dt><dd>{{.MalformedPackets}}</dd><dt>未知通道包</dt><dd>{{.UnknownChannelPackets}}</dd><dt>未知载荷类型包</dt><dd>{{.UnknownPayloadPackets}}</dd><dt>尾部不完整字节</dt><dd>{{.TrailingBytes}}</dd></dl>
{{if .AnalysisLimited}}<p class="timing-note">已达到分析上限，统计只覆盖已分析的部分；原始字节仍按采样限制保留。</p>{{end}}
{{if .RequestsTruncated}}<p class="timing-note">另有 {{.RequestsTruncated}} 条控制请求未写入指标，控制历史可能不完整。</p>{{end}}
<h3>RTSP 控制请求</h3><p class="timing-note">时间以采样开始为基准。只展示受控方法、状态与数字时序，不展示请求地址、认证字段或 SDP 原文。</p>
{{if .Requests}}<div class="scroll" role="region" tabindex="0" aria-label="RTSP 控制请求，可横向滚动"><table><thead><tr><th scope="col">请求</th><th scope="col">连接</th><th scope="col">方法</th><th scope="col">CSeq</th><th scope="col">RTSP 状态</th><th scope="col">开始偏移 (ms)</th><th scope="col">结束 / 已记录至 (ms)</th><th scope="col">请求耗时</th><th scope="col">响应记录</th><th scope="col">故障代码</th></tr></thead><tbody>
{{range .Requests}}<tr><th scope="row">#{{.Number}}</th><td>#{{.ConnectionID}}</td><td>{{.Method}}</td><td>{{.CSeq}}</td><td>{{if .StatusCode}}{{.StatusCode}}{{else}}未收到响应{{end}}</td><td>{{.StartMS}}</td><td>{{.EndMS}}</td><td>{{duration .}}</td><td>{{if .Complete}}完整{{else}}未完整结束{{end}}</td><td>{{failure .FailureCode}}</td></tr>{{end}}
</tbody></table></div>{{else}}<p class="timing-note">未观测到可展示的 RTSP 控制响应。</p>{{end}}
<h3>SDP 声明轨道</h3>
{{if .Tracks}}<div class="scroll" role="region" tabindex="0" aria-label="SDP 声明轨道，可横向滚动"><table><thead><tr><th scope="col">轨道</th><th scope="col">媒体标识</th><th scope="col">SDP 媒体类型</th><th scope="col">SDP 声明编码</th><th scope="col">载荷类型 PT</th><th scope="col">时钟频率</th><th scope="col">声道</th></tr></thead><tbody>
{{range .Tracks}}<tr><th scope="row">#{{.ID}}</th><td>{{.MediaID}}</td><td>{{kind .Kind}}</td><td>{{if .Codec}}{{.Codec}}{{else}}未声明{{end}}</td><td>{{.PayloadType}}</td><td>{{if .ClockRate}}{{.ClockRate}} Hz{{else}}未声明{{end}}</td><td>{{if eq .Kind "audio"}}{{if .Channels}}{{.Channels}}{{else}}未声明{{end}}{{else}}不适用{{end}}</td></tr>{{end}}
</tbody></table></div>{{else}}<p class="timing-note">{{if .SDPAvailable}}SDP 未声明可识别的媒体轨道。{{else}}未取得可解析 SDP，不能据此确认轨道或编码。{{end}}</p>{{end}}
<h3>实际 RTP 接收</h3>
{{if .Tracks}}<div class="scroll" role="region" tabindex="0" aria-label="实际 RTP 接收，可横向滚动"><table><thead><tr><th scope="col">轨道</th><th scope="col">接收状态</th><th scope="col">RTP 包</th><th scope="col">RTP 字节</th><th scope="col">首个 RTP 到达</th><th scope="col">末个 RTP 到达</th><th scope="col">最大包到达间隔</th><th scope="col">分析范围</th></tr></thead><tbody>
{{range .Tracks}}<tr><th scope="row">#{{.ID}}</th><td>{{if .Received}}已收到 RTP{{else}}未收到对应 RTP{{end}}</td><td>{{.RTPPackets}}</td><td>{{.RTPBytes}}</td><td>{{ms .FirstRTPMS}}</td><td>{{ms .LastRTPMS}}</td><td>{{if gt .RTPPackets 1}}{{.MaxArrivalGapMS}} ms{{else}}未观测{{end}}</td><td>{{if .AnalysisLimited}}达到分析上限{{else}}按已保存窗口统计{{end}}</td></tr>{{end}}
</tbody></table></div>{{else}}<p class="timing-note">没有可对应到声明轨道的 RTP 接收记录。</p>{{end}}
<details class="report-details"><summary>RTP 序号与时间戳统计</summary><p class="timing-note">{{if eq .Transport "udp"}}序号缺口和缺失包是接收点估计，使用有限回填窗口；源端漏发、链路丢包和迟到包都可能影响结果，需对照源端及中间网络证据。{{else}}序号缺口和缺失包是接收点估计，使用有限回填窗口，不能直接等同于 TCP 网络丢包。{{end}}序号跳变/重置属于候选，时间戳回退也可能来自 B 帧或乱序，需结合源端编码、流切换和 RTP 记录验证。</p>
{{if .Tracks}}<div class="scroll"><table><thead><tr><th scope="col">轨道</th><th scope="col">序号缺口次数</th><th scope="col">估计缺失包</th><th scope="col">乱序包</th><th scope="col">重复包</th><th scope="col">SSRC 变化</th><th scope="col">序号跳变/重置候选</th><th scope="col">RTP 时间戳回退</th></tr></thead><tbody>
{{range .Tracks}}<tr><th scope="row">#{{.ID}}</th><td>{{counter . .SequenceGaps}}</td><td>{{counter . .MissingSequencePackets}}</td><td>{{counter . .OutOfOrderPackets}}</td><td>{{counter . .DuplicatePackets}}</td><td>{{counter . .SSRCChanges}}</td><td>{{counter . .SequenceResets}}</td><td>{{counter . .TimestampRegressions}}</td></tr>{{end}}
</tbody></table></div>{{else}}<p class="timing-note">轨道序号统计未观测。</p>{{end}}</details>
{{else}}<p class="timing-note">当前报告未包含 RTSP 会话与 RTP 接收指标。</p>{{end}}
<p class="timing-note">原始 stream.rtsp.zip 保存 server-wire.bin 服务端应用字节、session.sdp 原文和 session.json 数字元数据；RTSPS 字节在 TLS 解密后保存。导入证据包可离线复盘，无需连接摄像头。</p>
{{if .Metrics}}{{if eq .Metrics.Transport "udp"}}<p class="timing-note">UDP 证据另含 udp-datagrams.bin 完整数据报及 udp-index.ndjson 边界、到达时间和来源记录。原始数据含被过滤的数据报，轨道指标仅统计符合协商来源的包。</p>{{end}}{{end}}
</article>`))

func renderRTSPHTML(report Report) (template.HTML, error) {
	var metrics *RTSPMetrics
	if report.RTSP != nil {
		copy := *report.RTSP
		copy.Requests = copy.Requests[:min(len(copy.Requests), maxRTSPRequests)]
		copy.Tracks = copy.Tracks[:min(len(copy.Tracks), maxRTSPTracks)]
		metrics = &copy
	}
	stages := report.Manifest.Observation.Stages
	var output bytes.Buffer
	if err := rtspReportPage.Execute(&output, struct {
		Metrics       *RTSPMetrics
		DNS, TCP, TLS string
	}{metrics, rtspPhaseDuration(stages, "dns"), rtspPhaseDuration(stages, "connect"), rtspPhaseDuration(stages, "tls")}); err != nil {
		return "", err
	}
	// Every variable in this fragment was escaped by html/template above.
	return template.HTML(output.String()), nil
}

var rtspEvidenceRows = template.Must(template.New("rtsp-evidence").Funcs(template.FuncMap{"artifact": rtspArtifactName}).Parse(`<thead><tr><th scope="col">对象</th><th scope="col">轨道</th><th scope="col">SDP 编码</th><th scope="col">RTP 序号</th><th scope="col">到达偏移 (ms)</th><th scope="col">原始条目</th><th scope="col">偏移 (字节)</th><th scope="col">长度 (字节)</th></tr></thead><tbody>{{range .}}<tr><td>{{if eq .Kind "rtp_packet" "video_rtp" "audio_rtp"}}RTP 包{{else}}RTSP 应用字节{{end}}</td><td>{{if .Track}}#{{.Track}}{{else}}未记录{{end}}</td><td>{{if .Codec}}{{.Codec}}{{else}}未声明{{end}}</td><td>{{with .RTPSequence}}{{.}}{{else}}未观测{{end}}</td><td>{{.ArrivalMS}}</td><td><code>{{artifact .Artifact}}</code></td><td>{{.Offset}}</td><td>{{.Size}}</td></tr>{{end}}</tbody>`))

func renderRTSPEvidenceRows(events []Event) (template.HTML, error) {
	var output bytes.Buffer
	if err := rtspEvidenceRows.Execute(&output, events); err != nil {
		return "", err
	}
	return template.HTML(output.String()), nil
}

func rtspPreviewStateLabel(state string) string {
	switch state {
	case "ready":
		return "已输出媒体"
	case "unsupported":
		return "编码或封装暂不支持"
	case "failed":
		return "本机转封装未完成"
	case "not_requested":
		return "未请求预览"
	}
	return "未记录"
}
func rtspPreviewReasonLabel(code string) string {
	switch code {
	case "":
		return "无"
	case "no_track":
		return "未选择对应轨道"
	case "no_media":
		return "未输出对应媒体"
	case "codec_unsupported":
		return "首版仅支持 H.264 / AAC-LC"
	case "packetization_unsupported":
		return "RTP 打包方式不支持"
	case "aac_configuration_unsupported":
		return "AAC 配置不支持（需 AAC-LC、1024 采样、单/双声道）"
	case "queue_limit":
		return "本机预览队列达到上限"
	case "missing_video_parameters":
		return "缺少有效 SPS/PPS"
	case "no_random_access":
		return "未取得可用 IDR 随机接入点"
	case "timestamp_unsupported":
		return "媒体时间戳或 DTS 不可转换"
	case "access_unit_limit":
		return "媒体访问单元达到上限"
	case "player_unavailable":
		return "本机播放器已停止接收"
	case "capture_setup_failed":
		return "原生播放准备或 PLAY 未完成"
	}
	return "未识别的预览状态"
}

func rtspPreviewReasonLabelFor(preview *RTSPPreviewReport, code string) string {
	if preview != nil && preview.Version == 2 {
		if preview.CodecCapabilitiesVersion >= 2 && code == "codec_unsupported" {
			return "本版支持 H.264 / H.265 / AAC-LC / Opus；其他编码预览暂不支持"
		}
		if preview.CodecCapabilitiesVersion >= 2 && code == "opus_configuration_unsupported" {
			return "Opus 预览仅支持单/双声道和映射族 0"
		}
		if code == "codec_unsupported" {
			return "本版支持 H.264 / H.265 / AAC-LC；其他编码预览暂不支持"
		}
		if code == "missing_hevc_parameters" {
			return "缺少或无法解析有效 VPS/SPS/PPS"
		}
	}
	return rtspPreviewReasonLabel(code)
}
