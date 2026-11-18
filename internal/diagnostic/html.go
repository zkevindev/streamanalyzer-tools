package diagnostic

import (
	"fmt"
	"html/template"
	"io"
	"strconv"
	"strings"
)

var httpTimingPhases = []struct{ Name, Label string }{
	{"dns", "DNS"}, {"connect", "TCP"}, {"tls", "TLS"}, {"request_send", "发送请求"},
	{"response_first_byte", "等待首字节"}, {"response_headers", "接收响应头"},
}

var reportPage = template.Must(template.New("report").Funcs(template.FuncMap{
	"seconds":       func(ms int64) float64 { return float64(ms) / 1000 },
	"finish":        endReasonText,
	"dumpName":      dumpName,
	"join":          strings.Join,
	"modelProvider": modelProviderLabel,
	"stageName":     stageLabel,
	"mediaArrival":  mediaArrivalLabel,
	"phaseDuration": requestPhaseDuration,
	"requestKind":   httpTimingKindLabel,
	"elapsed":       func(startMS, endMS int64) int64 { return endMS - startMS },
	"pid":           func(pid uint16) string { return fmt.Sprintf("0x%04X", pid) },
	"streamType":    func(streamType uint8) string { return fmt.Sprintf("0x%02X", streamType) },
	"tsEvent":       tsEventLabel,
}).Parse(`{{define "codecRows"}}
{{with .VideoCodecs}}<dt>视频编码</dt><dd>{{join . " / "}}</dd>{{end}}
{{with .AudioCodecs}}<dt>音频编码</dt><dd>{{join . " / "}}</dd>{{end}}
{{end}}<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Protocol}} 排障报告</title>
<style>
body{font:16px/1.6 system-ui,sans-serif;color:#17273b;background:#f2f5f9;margin:0}
main{max-width:66rem;margin:auto;padding:clamp(1rem,4vw,3rem)}
article{background:white;border:1px solid #d5deea;border-radius:.625rem;padding:1rem 1.4rem;margin:1rem 0}
h1,h2,h3{line-height:1.25}h1{margin-bottom:.4rem;color:#14263e}h3{margin-top:.5rem}
dl{display:grid;grid-template-columns:max-content 1fr;gap:.35rem 1.1rem}
dt{color:#52657e}dd{margin:0;overflow-wrap:anywhere}
table{border-collapse:collapse;width:100%;font-variant-numeric:tabular-nums}
th,td{text-align:left;padding:.4rem .65rem;border-bottom:1px solid #d5deea}
[data-severity="warn"]{border-left:4px solid #a26816}
[data-severity="error"]{border-left:4px solid #b5464d}
thead{color:#52657e;background:#f7f9fc}
.scroll{overflow-x:auto}a{color:#164d92}code{font-size:.9em}
.timing-note{font-size:.88rem;color:#52657e}
.request-timing th,.request-timing td{white-space:nowrap;font-size:.85rem}
.request-timing tbody tr:nth-child(even){background:#f7f9fc}
@media print{body{background:#fff}article{break-inside:avoid}.request-timing th,.request-timing td{white-space:normal}}
</style>
</head>
<body><main>
{{if .BackURL}}<p><a href="{{.BackURL}}">返回排障工作台</a></p>{{end}}
<h1>{{.Protocol}} 排障报告</h1>
<p>规则版本 <code>{{.Report.RuleVersion}}</code></p>
{{with .Report.Manifest.Observation}}
<article><h2>采样概况</h2>
<dl><dt>{{if $.Datagram}}监听地址{{else}}来源{{end}}</dt><dd>{{.Origin}}</dd>
<dt>开始时间</dt><dd>{{.StartedAt.Format "2006-01-02 15:04:05 UTC"}}</dd>
<dt>采样时长</dt><dd>{{printf "%.3f" (seconds .ElapsedMS)}} 秒 / 计划 {{.RequestedSeconds}} 秒</dd>
<dt>结束原因</dt><dd>{{finish .EndReason}}</dd>
{{if .HTTPStatus}}<dt>HTTP 状态</dt><dd>{{.HTTPStatus}}</dd>{{end}}
{{if not $.HTTPRequests}}{{with .TTFBMS}}<dt>{{if eq $.Report.Manifest.Protocol "rtmp"}}RTMP 首字节{{else}}响应首字节{{end}}</dt><dd>{{.}} ms</dd>{{end}}{{end}}
</dl></article>
{{$metrics := $.Report.Metrics}}
{{$arrivals := or .FirstPayloadMS $metrics.FirstVideoMS $metrics.FirstAudioMS $metrics.FirstKeyframeMS}}
{{if or $.TimingStages $.HTTPRequests $arrivals .FailureStage .FailureCode}}
<article><h2>采样时序</h2>
<p class="timing-note">到达时间以采样开始为基准。</p>
{{$unit := $metrics.MediaTimingUnit}}
{{if not $unit}}{{if eq $.Report.Manifest.Protocol "hls"}}{{$unit = "segment"}}{{else if $.MPEGTS}}{{$unit = "pes"}}{{end}}{{end}}
<dl>
<dt>首个载荷到达</dt><dd>{{with .FirstPayloadMS}}{{.}} ms{{else}}未观测{{end}}</dd>
<dt>{{mediaArrival $unit "视频"}}</dt><dd>{{with $metrics.FirstVideoMS}}{{.}} ms{{else}}未观测{{end}}</dd>
<dt>{{mediaArrival $unit "音频"}}</dt><dd>{{with $metrics.FirstAudioMS}}{{.}} ms{{else}}未观测{{end}}</dd>
{{if eq $.Report.Manifest.Protocol "" "http-flv" "rtmp"}}<dt>首个关键帧到达</dt><dd>{{with $metrics.FirstKeyframeMS}}{{.}} ms{{else}}未观测{{end}}</dd>{{end}}
{{with .FailureStage}}<dt>故障阶段</dt><dd>{{stageName . $.LegacyRTMP}}</dd>{{end}}
{{with .FailureCode}}<dt>故障代码</dt><dd><code>{{.}}</code></dd>{{end}}
</dl>
{{if $.HTTPRequests}}
<h3 id="request-timing-title">HTTP 请求耗时</h3>
<p id="request-timing-note" class="timing-note">单位为毫秒。采样偏移从采样开始计时，TTFB 从当次请求开始计时。— 表示未测得。</p>
<div class="scroll" tabindex="0" role="region" aria-labelledby="request-timing-title" aria-describedby="request-timing-note"><table class="request-timing">
<thead><tr><th scope="col">请求</th><th scope="col">HTTP 状态</th><th scope="col">采样偏移</th>{{range $.HTTPPhases}}<th scope="col">{{.Label}}</th>{{end}}<th scope="col">TTFB</th><th scope="col">至响应头总耗时</th><th scope="col">响应体接收</th>{{if $.HTTPFailures}}<th scope="col">故障阶段 / 代码</th>{{end}}</tr></thead>
<tbody>{{range $request := $.HTTPRequests}}
<tr><th scope="row">#{{.Number}}{{with .Kind}} · {{requestKind .}}{{end}}</th><td>{{with .HTTPStatus}}{{.}}{{else}}—{{end}}</td><td>{{.StartMS}}–{{.EndMS}}</td>
{{range $.HTTPPhases}}<td>{{phaseDuration $request.Stages .Name}}</td>{{end}}
<td>{{with .TTFBMS}}{{.}}{{else}}—{{end}}</td><td>{{.TotalMS}}</td><td>{{with .BodyEndMS}}{{elapsed $request.EndMS .}}{{else}}—{{end}}</td>
{{if $.HTTPFailures}}<td>{{if or .FailureStage .FailureCode}}{{with .FailureStage}}{{stageName . false}}{{end}}{{if and .FailureStage .FailureCode}} / {{end}}{{with .FailureCode}}<code>{{.}}</code>{{end}}{{else}}—{{end}}</td>{{end}}</tr>
{{end}}</tbody>
</table></div>
{{with $.Report.RequestsTruncated}}<p class="timing-note">另有 {{.}} 条请求未展示。</p>{{end}}
{{end}}
{{if $.TimingStages}}<div class="scroll"><table>
<thead><tr><th scope="col">阶段</th><th scope="col">开始偏移 (ms)</th><th scope="col">结束偏移 (ms)</th><th scope="col">耗时 (ms)</th></tr></thead>
<tbody>{{range $.TimingStages}}<tr><th scope="row">{{stageName .Name $.LegacyRTMP}}</th><td>{{.StartMS}}</td><td>{{.EndMS}}</td><td>{{elapsed .StartMS .EndMS}}</td></tr>{{end}}</tbody>
</table></div>{{end}}
</article>
{{end}}
{{end}}
{{if eq .Report.Manifest.Protocol "hls"}}
{{with .Report.HLS}}
<article><h2>播放列表与分片指标</h2><dl>
<dt>清单快照</dt><dd>{{.PlaylistsCaptured}}</dd>
<dt>播放列表请求失败</dt><dd>{{.PlaylistFailures}}</dd>
<dt>采集 / 失败 / 跳过分片</dt><dd>{{.SegmentsCaptured}} / {{.SegmentsFailed}} / {{.SegmentsSkipped}}</dd>
<dt>独立音频分片</dt><dd>{{.AudioSegmentsCaptured}}（计入采集）</dd>
<dt>加密分片</dt><dd>{{.EncryptedSegments}}</dd>
<dt>采集 / 失败初始化段</dt><dd>{{.InitSegmentsCaptured}} / {{.InitSegmentsFailed}}</dd>
<dt>最长分片下载</dt><dd>{{if .SegmentsCaptured}}{{.MaxSegmentDownloadMS}} ms{{else}}未测得{{end}}</dd>
<dt>采样变体带宽</dt><dd>{{if .RenditionBandwidth}}{{.RenditionBandwidth}} bit/s{{else}}未声明{{end}}</dd>
<dt>媒体数据</dt><dd>{{.MediaBytes}} 字节</dd>
{{template "codecRows" $.Report.Metrics}}
</dl></article>
{{end}}
{{else if .MPEGTS}}
{{with .Report.MPEGTS}}
<article><h2>MPEG-TS 传输指标</h2><dl>
<dt>TS 包数</dt><dd>{{.Packets}}</dd>
<dt>TS 包大小</dt><dd>{{with .PacketSize}}{{.}} 字节{{else}}未识别{{end}}</dd>
<dt>连续计数错误</dt><dd>{{.ContinuityErrors}}</dd>
<dt>传输错误标记 (TEI)</dt><dd>{{.TransportErrors}}</dd>
<dt>重复 TS 包</dt><dd>{{.Duplicates}}</dd>
<dt>格式错误 TS 包</dt><dd>{{.MalformedPackets}}</dd>
<dt>同步丢失</dt><dd>{{.SyncLossBytes}} 字节</dd>
<dt>PAT / PMT 段数</dt><dd>{{.PATSections}} / {{.PMTSections}}</dd>
<dt>PCR PID 数</dt><dd>{{.PCRPIDs}}</dd>
<dt>PSI 解析错误</dt><dd>{{.PSIErrors}}</dd>
{{with .TrailingBytes}}<dt>尾部不完整数据</dt><dd>{{.}} 字节</dd>{{end}}
{{with .MaxPCRGapMS}}<dt>最大 PCR 间隔</dt><dd>{{.}} ms</dd>{{end}}
{{if $.RTP}}
<dt>RTP 数据报</dt><dd>{{.RTPDatagrams}}</dd>
<dt>估计缺失 RTP 包</dt><dd>{{.RTPMissing}}</dd>
<dt>乱序 RTP 包</dt><dd>{{.RTPOutOfOrder}}</dd>
<dt>重复 RTP 包</dt><dd>{{.RTPDuplicates}}</dd>
<dt>格式错误 RTP 数据报</dt><dd>{{.RTPInvalid}}</dd>
{{end}}
{{template "codecRows" $.Report.Metrics}}
</dl>
{{if .PIDs}}<h3>PID 明细{{if gt .PIDCount (len .PIDs)}}（显示 {{len .PIDs}} / {{.PIDCount}}）{{end}}</h3>
<div class="scroll"><table>
<thead><tr><th scope="col">PID</th><th scope="col">编码</th><th scope="col">流类型</th><th scope="col">TS 包数</th><th scope="col">TEI</th><th scope="col">连续计数错误</th><th scope="col">重复包</th><th scope="col">PES 起始数</th><th scope="col">PCR / PTS / DTS 计数</th></tr></thead>
<tbody>{{range .PIDs}}
<tr><th scope="row"><code>{{pid .PID}}</code></th><td>{{with .Codec}}{{.}}{{else}}—{{end}}</td><td>{{with .StreamType}}{{streamType .}}{{else}}—{{end}}</td><td>{{.Packets}}</td><td>{{.TransportErrors}}</td><td>{{.ContinuityErrors}}</td><td>{{.Duplicates}}</td><td>{{.PESStarts}}</td><td>{{.PCRCount}} / {{.PTSCount}} / {{.DTSCount}}</td></tr>
{{end}}</tbody>
</table></div>{{end}}
</article>
{{end}}
{{else}}
{{with .Report.Metrics}}
<article><h2>媒体指标</h2><dl>
<dt>视频帧</dt><dd>{{.VideoFrames}}</dd>
<dt>音频帧</dt><dd>{{.AudioFrames}}</dd>
<dt>视频帧间最大到达间隔</dt><dd>{{if gt .VideoFrames 1}}{{.MaxVideoGapMS}} ms{{else}}未测得{{end}}</dd>
{{with .MaxAudioGapMS}}<dt>音频帧间最大到达间隔</dt><dd>{{.}} ms</dd>{{end}}
<dt>最大关键帧间隔</dt><dd>{{if .MaxGOPMS}}{{.MaxGOPMS}} ms{{else}}未测得{{end}}</dd>
{{template "codecRows" .}}
</dl></article>
{{end}}
{{end}}
<h2>规则诊断</h2>
{{range .Report.Findings}}
<article id="{{.ID}}" data-severity="{{.Severity}}">
<h3>{{.ID}} · {{.Message}}</h3>
<p>采样相对时间 {{printf "%.3f" (seconds .StartMS)}}–{{printf "%.3f" (seconds .EndMS)}} 秒{{if .Occurrences}} · 共 {{.Occurrences}} 次{{end}}</p>
{{with .NextCheck}}<p><strong>建议检查</strong> {{.}}</p>{{end}}
{{if .Evidence}}<div class="scroll"><table>
{{if eq $.Report.Manifest.Protocol "hls"}}
<thead><tr><th scope="col">对象</th><th scope="col">流</th><th scope="col">分片序号</th><th scope="col">到达 (ms)</th><th scope="col">HTTP</th><th scope="col">请求耗时 (ms)</th><th scope="col">原始包条目</th><th scope="col">长度 (字节)</th></tr></thead>
<tbody>{{range .Evidence}}<tr><td>{{.Kind}}</td><td>{{if eq .Rendition "audio"}}独立音频{{else if eq .Rendition "main"}}主路{{else}}—{{end}}</td><td>{{with .Sequence}}{{.}}{{else}}—{{end}}</td><td>{{.ArrivalMS}}</td><td>{{if .HTTPStatus}}{{.HTTPStatus}}{{else}}—{{end}}</td><td>{{if or .DurationMS .HTTPStatus}}{{.DurationMS}}{{else}}—{{end}}</td><td>{{with .Artifact}}<code>{{.}}</code>{{else}}—{{end}}</td><td>{{if .Artifact}}{{.Size}}{{else}}—{{end}}</td></tr>{{end}}</tbody>
{{else if $.MPEGTS}}
<thead><tr><th scope="col">对象</th><th scope="col">到达 (ms)</th><th scope="col">PID</th>{{if $.RTP}}<th scope="col">RTP 序号</th>{{end}}<th scope="col">偏移 (字节)</th><th scope="col">长度 (字节)</th></tr></thead>
<tbody>{{range .Evidence}}<tr><td>{{tsEvent .Kind}}</td><td>{{.ArrivalMS}}</td><td>{{with .PID}}{{pid .}}{{else}}—{{end}}</td>{{if $.RTP}}<td>{{with .RTPSequence}}{{.}}{{else}}—{{end}}</td>{{end}}<td>{{.Offset}}</td><td>{{.Size}}</td></tr>{{end}}</tbody>
{{else}}
<thead><tr><th scope="col">{{if eq $.Report.Manifest.Protocol "rtmp"}}消息{{else}}标签{{end}}</th><th scope="col">到达 (ms)</th><th scope="col">DTS (ms)</th><th scope="col">PTS (ms)</th><th scope="col">偏移 (字节)</th><th scope="col">{{if eq $.Report.Manifest.Protocol "rtmp"}}消息跨度 (字节){{else}}长度 (字节){{end}}</th></tr></thead>
<tbody>{{range .Evidence}}<tr><td>{{.Kind}}</td><td>{{.ArrivalMS}}</td><td>{{.DTS}}</td><td>{{.PTS}}</td><td>{{.Offset}}</td><td>{{.Size}}</td></tr>{{end}}</tbody>
{{end}}
</table></div>{{end}}
</article>
{{else}}<p>这次采样暂无规则触发。</p>{{end}}
{{with .Explanation}}
<h2>模型解读</h2>
<article><p><small>解读来源 {{modelProvider .Provider}}{{if .Model}} · {{.Model}}{{end}}</small></p>
<p>{{.Summary}}</p>
{{range .Hypotheses}}<h3>待验证 · {{.Claim}}</h3>
<p>{{range .EvidenceIDs}}<a href="#{{.}}">{{.}}</a> {{end}}</p>
<p>{{.NextCheck}}</p>{{end}}
</article>
{{end}}
<article><h2>原始流</h2>
{{if eq .Report.Manifest.Protocol "rtmp"}}<p>保留从服务端握手起的 RTMP 字节。RTMPS 在 TLS 解密后记录。证据偏移从首个 RTMP 块开始，消息跨度可包含交错的块。</p>{{end}}
{{if eq .Report.Manifest.Protocol "http-ts"}}<p>保留采样期间读取的 HTTP 响应体。</p>{{end}}
{{if eq .Report.Manifest.Protocol "udp-ts"}}<p>按接收顺序拼接 UDP 载荷。证据时间线保存各数据报的偏移和长度。</p>{{end}}
{{if eq .Report.Manifest.Protocol "rtp-ts"}}<p>按接收顺序拼接原始 RTP 数据报，保留 RTP 头。证据时间线保存各数据报的偏移和长度。</p>{{end}}
<dl>
<dt>文件</dt><dd><code>{{dumpName .Report.Manifest.Protocol}}</code></dd>
<dt>保留字节</dt><dd>{{.Report.Manifest.Stream.Bytes}}</dd>
<dt>SHA-256</dt><dd><code>{{.Report.Manifest.Stream.SHA256}}</code></dd>
</dl></article>
</main></body></html>`))

func modelProviderLabel(provider string) string {
	switch provider {
	case "", "openai-compatible":
		return "OpenAI 兼容接口"
	case "deepseek":
		return "DeepSeek"
	case "anthropic":
		return "Anthropic"
	case "gemini":
		return "Google Gemini"
	default:
		return provider
	}
}

func stageLabel(name string, legacyRTMP bool) string {
	switch name {
	case "dns":
		return "DNS 解析"
	case "connect":
		return "TCP 建连"
	case "tls":
		return "TLS 握手"
	case "response_headers":
		return "HTTP 响应头"
	case "request_send":
		return "发送请求"
	case "response_first_byte":
		return "等待响应首字节"
	case "listen":
		return "建立监听"
	case "receive":
		return "接收数据"
	case "handshake":
		return "RTMP 握手"
	case "app_connect":
		return "RTMP 应用连接"
	case "create_stream":
		return "RTMP 创建流"
	case "play":
		if legacyRTMP {
			return "RTMP 建连与播放协商"
		}
		return "RTMP 播放协商"
	default:
		return name
	}
}

func mediaArrivalLabel(unit, track string) string {
	switch unit {
	case "segment":
		return "首个" + track + "分片完成"
	case "pes":
		return "首个" + track + " PES 起始"
	default:
		return "首个" + track + "帧到达"
	}
}

func httpTimingKindLabel(kind string) string {
	switch kind {
	case "hls_playlist":
		return "播放列表"
	case "hls_init":
		return "初始化段"
	case "hls_segment":
		return "媒体分片"
	default:
		return kind
	}
}

func requestPhaseDuration(stages []CaptureStage, name string) string {
	var durations []string
	for _, stage := range stages {
		if stage.Name == name {
			durations = append(durations, strconv.FormatInt(stage.EndMS-stage.StartMS, 10))
		}
	}
	if len(durations) == 0 {
		return "—"
	}
	return strings.Join(durations, " / ")
}

func nonHTTPStages(stages []CaptureStage) []CaptureStage {
	var result []CaptureStage
	for _, stage := range stages {
		switch stage.Name {
		case "dns", "connect", "tls", "request_send", "response_first_byte", "response_headers":
			continue
		}
		result = append(result, stage)
	}
	return result
}

func tsEventLabel(kind string) string {
	switch kind {
	case "ts_packet":
		return "TS 包"
	case "ts_sync_loss":
		return "TS 同步丢失"
	case "rtp_datagram":
		return "RTP 数据报"
	default:
		return kind
	}
}

func RenderHTML(w io.Writer, report Report, explanation *Explanation, backURL string) error {
	protocol := "HTTP-FLV"
	mpegTS, datagram, rtp := false, false, false
	switch report.Manifest.Protocol {
	case ProtocolRTMP:
		protocol = "RTMP"
		if strings.HasPrefix(report.Manifest.Observation.Origin, "rtmps://") {
			protocol = "RTMPS"
		}
	case ProtocolHLS:
		protocol = "HLS"
	case ProtocolHTTPTS:
		protocol, mpegTS = "HTTP MPEG-TS", true
	case ProtocolUDPTS:
		protocol, mpegTS, datagram = "UDP MPEG-TS", true, true
	case ProtocolRTPTS:
		protocol, mpegTS, datagram, rtp = "RTP MPEG-TS", true, true, true
	}
	requests := report.Requests
	if len(requests) == 0 {
		requests = report.Manifest.Observation.Requests
	}
	stages := report.Manifest.Observation.Stages
	if len(requests) > 0 {
		stages = nonHTTPStages(stages)
	}
	var hasHTTPFailures, hasRTMPHandshake bool
	for _, request := range requests {
		if request.FailureStage != "" || request.FailureCode != "" {
			hasHTTPFailures = true
			break
		}
	}
	for _, stage := range stages {
		if stage.Name == "handshake" {
			hasRTMPHandshake = true
			break
		}
	}
	return reportPage.Execute(w, struct {
		Report       Report
		Explanation  *Explanation
		BackURL      string
		Protocol     string
		MPEGTS       bool
		Datagram     bool
		RTP          bool
		HTTPRequests []HTTPRequestTiming
		HTTPPhases   []struct{ Name, Label string }
		HTTPFailures bool
		TimingStages []CaptureStage
		LegacyRTMP   bool
	}{report, explanation, backURL, protocol, mpegTS, datagram, rtp, requests, httpTimingPhases, hasHTTPFailures, stages, report.Manifest.Protocol == ProtocolRTMP && !hasRTMPHandshake})
}
