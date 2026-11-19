package diagnostic

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"image/jpeg"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var httpTimingPhases = []struct{ Name, Label string }{
	{"dns", "DNS"}, {"connect", "TCP"}, {"tls", "TLS"}, {"request_send", "发送请求"},
	{"response_first_byte", "等待首字节"}, {"response_headers", "接收响应头"},
}

var reportPage = template.Must(template.New("report").Funcs(template.FuncMap{
	"seconds":                func(ms int64) float64 { return float64(ms) / 1000 },
	"finish":                 endReasonText,
	"dumpName":               dumpName,
	"join":                   strings.Join,
	"modelProvider":          modelProviderLabel,
	"stageName":              diagnosticReportStageLabel,
	"reportOrigin":           diagnosticReportOrigin,
	"isRTSP":                 isRTSPProtocol,
	"rtspReport":             renderRTSPHTML,
	"rtspEvidenceRows":       renderRTSPEvidenceRows,
	"rtspFirstPacket":        rtspFirstPacket,
	"mediaArrival":           mediaArrivalLabel,
	"phaseDuration":          requestPhaseDuration,
	"requestKind":            httpTimingKindLabel,
	"elapsed":                func(startMS, endMS int64) int64 { return endMS - startMS },
	"pid":                    func(pid uint16) string { return fmt.Sprintf("0x%04X", pid) },
	"streamType":             func(streamType uint8) string { return fmt.Sprintf("0x%02X", streamType) },
	"tsEvent":                tsEventLabel,
	"playerState":            playerReportState,
	"playerSource":           playerSourceLabel,
	"playerEvidence":         func(events []Event) bool { return len(events) > 0 && events[0].Artifact == playerName },
	"intervalEnd":            func(startMS, durationMS int64) int64 { return startMS + durationMS },
	"investigationState":     investigationStateLabel,
	"investigationMedia":     investigationMediaLabel,
	"investigationAlignment": investigationAlignmentLabel,
	"investigationModel":     investigationModelLabel,
	"videoSource":            playerVideoQualitySourceLabel,
	"videoCount":             playerVideoObservedCount,
	"videoMS":                playerVideoObservedTime,
	"videoRatio":             playerVideoDroppedRatio,
	"videoSampleState":       playerVideoSampleState,
	"assessmentState":        stallDirectionStateLabel,
	"caseEvidenceKnown":      stallAssessmentEvidenceKnown,
	"assessmentFindings":     stallAssessmentExtraFindings,
	"networkKind":            playerNetworkKindLabel,
	"networkOutcome":         playerNetworkOutcomeLabel,
	"networkBoundary":        playerNetworkBoundaryLabel,
	"networkOffset":          playerVideoObservedCount,
	"networkEnd":             playerNetworkEndLabel,
	"networkRows":            playerNetworkVisibleRequests,
	"networkOmitted":         playerNetworkOmittedRequests,
	"networkDetails":         playerNetworkDetailedRequests,
	"networkPhaseMissing":    playerNetworkPhaseMissingRequests,
	"networkHops":            playerNetworkVisibleHops,
	"networkHopChain":        playerNetworkHopChain,
	"networkHopPhase":        playerNetworkPhaseDurationLabel,
	"networkConnection":      playerNetworkConnectionLabel,
	"networkFailure":         playerNetworkFailureLabel,
	"networkHopOmitted":      playerNetworkOmittedHops,
	"networkStageOmitted":    playerNetworkHasOmittedStages,
	"schedulingView":         playerSchedulingView,
	"schedulingSupport":      playerSchedulingSupportLabel,
	"schedulingCaseView":     playerSchedulingCaseView,
	"presentationView":       playerPresentationView,
	"presentationSupport":    playerPresentationSupportLabel,
	"presentationCount":      playerPresentationCountLabel,
	"intervalName":           playerIntervalLabel,
	"investigationTitle":     playerInvestigationIntervalTitle,
}).Parse(`{{define "codecRows"}}
{{with .VideoCodecs}}<dt>视频编码</dt><dd>{{join . " / "}}</dd>{{end}}
{{with .AudioCodecs}}<dt>音频编码</dt><dd>{{join . " / "}}</dd>{{end}}
{{end}}{{define "playerVideo"}}
<section class="player-video-report"><h3>视频呈帧与丢帧</h3>
{{with .}}<dl>
<dt>帧质量计数来源</dt><dd>{{videoSource .QualitySource}}</dd>
<dt>视频帧增量</dt><dd>{{videoCount .TotalFrames}}</dd>
<dt>丢帧增量</dt><dd>{{videoCount .DroppedFrames}}</dd>
<dt>丢帧比例</dt><dd>{{videoRatio .DroppedRatio}}</dd>
<dt>呈帧增量</dt><dd>{{videoCount .PresentedFrames}}</dd>
<dt>最大呈帧回调间隔</dt><dd>{{videoMS .MaxFrameGapMS}}</dd>
<dt>计数重置</dt><dd>{{.CounterResets}}</dd>
<dt>视频样本数量</dt><dd>{{.SampleCount}}</dd>
</dl><p class="timing-note">帧增量按连续前台播放样本的差值汇总，暂停、跳转、后台和采样结束后的记录不计入。呈帧与回调间隔来自视频帧回调，低帧率内容和页面调度也会影响间隔，需结合缓冲、收流和媒体帧率一起判断。</p>
{{else}}<p class="timing-note">该记录未采集视频呈帧与丢帧数据。</p>{{end}}
</section>
{{end}}{{define "stallAssessment"}}{{$case := .}}
<section class="stall-assessment"><h3>分层排查方向</h3>
{{with .Assessment}}
{{with .Conclusion}}<p class="stall-assessment-conclusion">{{.}}</p>{{end}}
<p class="timing-note">以下方向根据已保存的证据整理，用于安排验证步骤。</p>
{{range .Directions}}<section class="stall-direction" data-state="{{.Status}}"><h4>{{if .Title}}{{.Title}}{{else}}待核对方向{{end}} <span class="direction-state">{{assessmentState .Status}}</span></h4>
{{with .Message}}<p>{{.}}</p>{{end}}
{{if .EvidenceIDs}}<p>关联证据 {{range .EvidenceIDs}}{{if caseEvidenceKnown $case .}}<a href="#case-{{.}}">{{.}}</a>{{else}}{{.}}（未找到记录）{{end}} {{end}}</p>{{end}}
{{if .NextChecks}}<p class="timing-note">建议验证</p><ul>{{range .NextChecks}}<li>{{.}}</li>{{end}}</ul>{{end}}
</section>{{else}}<p class="timing-note">当前证据尚未形成排查方向。</p>{{end}}
{{if .Limitations}}<h4>证据限制</h4><ul>{{range .Limitations}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{with .Version}}<p class="timing-note">排查规则 {{.}}</p>{{end}}
{{with assessmentFindings $case}}<h4>分层判断证据</h4>{{range .}}<section id="case-{{.ID}}"><h4>{{.ID}} · {{.Message}}</h4><p class="timing-note">{{.StartMS}}–{{.EndMS}} ms</p>{{with .NextCheck}}<p>{{.}}</p>{{end}}</section>{{end}}{{end}}
{{else}}<p class="timing-note">{{if or (eq .Status "queued") (eq .Status "running")}}补查完成后展示分层排查方向。{{else}}该记录尚未生成排查分层。重新生成建议可补充排查分层，可在工作台点击“生成排查与模型建议”。{{end}}</p>{{end}}
</section>
{{end}}{{define "playerNetworkNotes"}}
<p class="timing-note">本地代理记录实际播放连接的上游请求：响应头、首段响应体和读取字节均在代理侧观测。结束偏移可能包含向播放器写出数据的时间；写出完成不代表浏览器已接收、解码或可播放。</p>
<p class="timing-note">偏移从后端采样开始计时；未完成请求的结束列标为“截至”，表示快照时刻。缺失字段显示“未观测”，0 为已观测值。</p>
{{end}}{{define "playerNetworkRequests"}}
{{if .}}<div class="scroll player-network-requests" role="region" tabindex="0" aria-label="HLS 实际播放请求记录，可横向滚动"><table>
<thead><tr><th scope="col">请求</th><th scope="col">类型</th><th scope="col">资源标识</th><th scope="col">列表序号</th><th scope="col">方法</th><th scope="col">开始偏移 (ms)</th><th scope="col">上游响应头偏移 (ms)</th><th scope="col">上游首段响应体偏移 (ms)</th><th scope="col">代理结束 / 截至偏移 (ms)</th><th scope="col">上游 HTTP</th><th scope="col">上游读取字节</th><th scope="col">结果</th></tr></thead>
<tbody>{{range networkRows .}}<tr><th scope="row">#{{.Number}}</th><td>{{networkKind .Kind}}</td><td>{{.ResourceID}}</td><td>{{if .PlaylistSequence}}{{.PlaylistSequence}}{{else}}—{{end}}</td><td>{{.Method}}</td><td>{{.StartMS}}</td><td>{{networkOffset .HeaderMS}}</td><td>{{networkOffset .FirstBodyMS}}</td><td>{{networkEnd .}}</td><td>{{if .HTTPStatus}}{{.HTTPStatus}}{{else}}未观测{{end}}</td><td>{{.Bytes}}</td><td>{{networkOutcome .Outcome}}</td></tr>{{end}}</tbody>
</table></div>{{with networkOmitted .}}<p class="timing-note">页面仅展示前 200 条，另有 {{.}} 条已保存记录可在报告 JSON 或证据包中查看。</p>{{end}}
{{$details := networkDetails .}}{{if $details}}<section class="player-network-phase-list"><h3>逐跳阶段耗时</h3>
<p class="timing-note">阶段耗时来自本地代理的上游观测，TTFB 从本跳开始计时。上游响应体阶段从响应头起至上游读取结束，可能受下游写出影响，不代表浏览器接收完成。未完成请求仅展示已记录部分。复用连接或未触发相关事件时，DNS/TCP/TLS 保持未观测；同阶段多次观测用 / 分列。</p>
{{range $details}}<details class="player-network-phase-detail"><summary>#{{.Number}} · {{.ResourceID}} · {{len (networkHops .Hops)}} 跳{{networkHopChain .Hops}}</summary>
{{if or .FailureStage .FailureCode}}<p class="timing-note network-terminal-failure">请求终止阶段 / 原因：{{networkFailure .FailureStage .FailureCode}}</p>{{end}}
{{if .DroppedHops}}<p class="timing-note">逐跳记录达到采集上限或元数据预算，另有 {{.DroppedHops}} 跳未保存，重定向链可能不完整。</p>{{end}}
{{if .Hops}}<div class="scroll player-network-hop-table" role="region" tabindex="0" aria-label="请求逐跳阶段耗时，可横向滚动"><table><thead><tr><th scope="col">跳</th><th scope="col">上游 HTTP</th><th scope="col">开始偏移 (ms)</th><th scope="col">结束 / 已记录至偏移 (ms)</th><th scope="col">连接复用</th><th scope="col">TTFB (ms)</th><th scope="col">DNS (ms)</th><th scope="col">TCP 建连 (ms)</th><th scope="col">TLS 握手 (ms)</th><th scope="col">发送请求 (ms)</th><th scope="col">等待首字节 (ms)</th><th scope="col">接收响应头 (ms)</th><th scope="col">上游响应体阶段 (ms)</th><th scope="col">本跳故障阶段 / 原因</th></tr></thead>
<tbody>{{range networkHops .Hops}}<tr><th scope="row">#{{.Number}}</th><td>{{if .HTTPStatus}}{{.HTTPStatus}}{{else}}未观测{{end}}</td><td>{{.StartMS}}</td><td>{{.EndMS}}</td><td>{{networkConnection .ConnectionReused}}</td><td>{{networkOffset .TTFBMS}}</td><td>{{networkHopPhase .Stages "dns"}}</td><td>{{networkHopPhase .Stages "connect"}}</td><td>{{networkHopPhase .Stages "tls"}}</td><td>{{networkHopPhase .Stages "request_send"}}</td><td>{{networkHopPhase .Stages "response_first_byte"}}</td><td>{{networkHopPhase .Stages "response_headers"}}</td><td>{{networkHopPhase .Stages "receive"}}</td><td>{{networkFailure .FailureStage .FailureCode}}</td></tr>{{end}}</tbody></table></div>
{{else}}<p class="timing-note">此请求未包含可展示的逐跳阶段记录。</p>{{end}}
{{with networkHopOmitted .Hops}}<p class="timing-note">页面仅展示前 8 跳，另有 {{.}} 跳已保存记录可在报告 JSON 或证据包中查看。</p>{{end}}
{{if networkStageOmitted .Hops}}<p class="timing-note">页面仅展示每跳前 16 条阶段观测，其余已保存记录可在报告 JSON 或证据包中查看。</p>{{end}}
</details>{{end}}
{{with networkPhaseMissing .}}<p class="timing-note">另有 {{.}} 条已展示请求未包含逐跳阶段记录。</p>{{end}}
</section>{{else}}<p class="timing-note">当前请求记录未包含逐跳阶段耗时，重新采样可补充。</p>{{end}}
{{else}}<p class="timing-note">当前记录未包含实际播放请求。</p>{{end}}
{{end}}{{define "schedulingMetrics"}}
<dl class="scheduling-metrics"><dt>区间数</dt><dd>{{with .}}{{.Count}}{{else}}未观测{{end}}</dd><dt>区间累计时长</dt><dd>{{with .}}{{.TotalMS}} ms{{else}}未观测{{end}}</dd><dt>最长区间</dt><dd>{{with .}}{{.MaxMS}} ms{{else}}未观测{{end}}</dd></dl>
{{end}}{{define "playerScheduling"}}
<section class="player-scheduling-report"><h3>{{.Title}}</h3>
{{with .Report}}
<dl class="scheduling-coverage"><dt>长任务接口</dt><dd>{{schedulingSupport .LongTaskSupport}}</dd><dt>观测区间</dt><dd>{{.ObservationStartMS}}–{{.ObservationEndMS}} ms</dd><dt>前台播放有效覆盖</dt><dd>{{.ActiveCoverageMS}} ms</dd></dl>
<p class="timing-note">调度区间按前台播放和观测窗口裁切，区间数与时长表示裁切后的有效片段。定时器每 250 ms 检查，仅采集原始延后 ≥100 ms 的记录；裁切后的区间可能更短。</p>
<p class="timing-note">长任务和定时器延后分别观测。页面负载、系统调度等都可能影响结果，不能单独证明 CPU 瓶颈或解码故障；与异常区间重合用于安排进一步验证。</p>
{{if eq .LongTaskSupport "unsupported"}}<p class="timing-note">长任务未观测不代表没有长任务，定时器延后仍单独记录。</p>{{end}}
{{if eq .ActiveCoverageMS 0}}<p class="timing-note">本记录没有可用于统计的前台播放覆盖区间，异常区间数保持未观测。</p>{{end}}
<section data-kind="long_task"><h4>浏览器长任务</h4>{{template "schedulingMetrics" $.Native}}</section>
<section data-kind="timer_delay"><h4>定时器延后（原始延后 ≥100 ms）</h4>{{template "schedulingMetrics" .TimerDelays}}</section>
{{if or .DroppedLongTasks .DroppedTimerDelays}}<p class="timing-note">采集上限外未记录：长任务 {{.DroppedLongTasks}} 条，定时器延后 {{.DroppedTimerDelays}} 条。当前统计可能不完整。</p>{{end}}
{{end}}
{{if .Rows}}<div class="scroll scheduling-intervals" role="region" tabindex="0" aria-label="页面调度有效区间，可横向滚动"><table><thead><tr><th scope="col">类型</th><th scope="col">开始偏移 (ms)</th><th scope="col">结束偏移 (ms)</th><th scope="col">有效区间时长 (ms)</th>{{if .HasStall}}<th scope="col">{{.OverlapLabel}}</th>{{end}}</tr></thead>
<tbody>{{range .Rows}}<tr><td>{{.Kind}}</td><td>{{.StartMS}}</td><td>{{.EndMS}}</td><td>{{.DurationMS}}</td>{{if $.HasStall}}<td>{{.OverlapMS}}</td>{{end}}</tr>{{end}}</tbody></table></div>
{{else}}{{if .Report}}<p class="timing-note">当前记录未包含可展示的调度区间。</p>{{end}}{{end}}
{{with .Omitted}}<p class="timing-note">页面仅展示前 200 个有效区间，另有 {{.}} 个已保存区间可在报告 JSON 或证据包中查看。</p>{{end}}
{{if not .Report}}<p class="timing-note">{{if .HasStall}}该补查未包含页面调度数据，可重新采样后补查。{{else}}该记录未采集页面调度数据，重新采样可补充长任务和定时器延后证据。{{end}}</p>{{end}}
</section>
{{end}}{{define "playerPresentation"}}
<section class="player-presentation-report"><h3>{{.Title}}</h3>
{{with .Report}}<dl><dt>回调能力</dt><dd>{{presentationSupport .Support}}</dd><dt>观测区间</dt><dd>{{.ObservationStartMS}}–{{.ObservationEndMS}} ms</dd><dt>回调基线就绪偏移</dt><dd>{{videoMS .ReadyAtMS}}</dd><dt>前台播放有效覆盖</dt><dd>{{.ActiveCoverageMS}} ms</dd><dt>{{if $.Window}}窗口关联间隔数{{else}}间隔次数{{end}}</dt><dd>{{presentationCount .Count}}</dd><dt>{{if $.Window}}窗口内重合累计时长{{else}}间隔累计时长{{end}}</dt><dd>{{videoMS .TotalMS}}</dd><dt>{{if $.Window}}最长重合时长{{else}}最长间隔{{end}}</dt><dd>{{videoMS .MaxMS}}</dd></dl>
<p class="timing-note">至少 3 个有效回调媒体时间增量用于估算间隔基线，记录阈值取 1000 ms 与 3 倍间隔基线中的较大值；基线不等同于源流编码帧率。只保留完整落在同一连续前台播放区间内的间隔，并排除采样结束后的部分。</p>
<p class="timing-note">回调间隔受内容帧率、浏览器呈现和页面调度影响，不能单凭该记录确认画面冻结或解码故障。与缓冲卡顿重合的时长用于对照，次数与时长不叠加到缓冲卡顿统计。</p>
{{if $.Window}}<p class="timing-note">窗口统计计算与补查窗口重合的时长；下表保留完整观测到的回调间隔，起止可能超出窗口。与缓冲卡顿重合的时长也按完整间隔保留。</p>{{end}}
{{if eq .Support "unsupported"}}<p class="timing-note">当前环境未提供 requestVideoFrameCallback，呈帧回调间隔保持未观测。</p>{{else if not .ReadyAtMS}}<p class="timing-note">尚未完成回调基线预热，无法判断是否存在异常回调间隔。</p>{{else if eq .ActiveCoverageMS 0}}<p class="timing-note">预热后没有可用于统计的连续前台播放覆盖区间，间隔统计保持未观测。</p>{{end}}
{{if .DroppedGaps}}<p class="timing-note">采集达到上限，另有 {{.DroppedGaps}} 条呈帧回调间隔记录未保存，统计可能不完整。</p>{{end}}
{{end}}
{{if .Rows}}<div class="scroll" role="region" tabindex="0" aria-label="呈帧回调间隔，可横向滚动"><table><thead><tr><th scope="col">{{if .Window}}窗口间隔{{else}}间隔{{end}}</th><th scope="col">开始偏移 (ms)</th><th scope="col">结束偏移 (ms)</th><th scope="col">{{if .Window}}完整回调间隔时长{{else}}间隔时长{{end}} (ms)</th><th scope="col">回调媒体时间间隔基线 (ms)</th><th scope="col">与缓冲卡顿重合 (ms)</th>{{if .Window}}<th scope="col">与本次补查区间重合 (ms)</th>{{end}}</tr></thead><tbody>
{{range .Rows}}<tr><th scope="row">#{{.Index}}</th><td>{{.StartMS}}</td><td>{{.EndMS}}</td><td>{{.DurationMS}}</td><td>{{.ExpectedFrameMS}}</td><td>{{.BufferingOverlapMS}}</td>{{if $.Window}}<td>{{.SelectedOverlapMS}}</td>{{end}}</tr>{{end}}
</tbody></table></div>{{else}}{{with .Report}}<p class="timing-note">{{if eq (presentationCount .Count) "0"}}有效覆盖内未记录到符合阈值的呈帧回调间隔。{{else}}当前记录未包含可展示的呈帧回调间隔。{{end}}</p>{{end}}{{end}}
{{with .Omitted}}<p class="timing-note">页面仅展示前 128 个间隔，另有 {{.}} 个已保存间隔可在报告 JSON 或证据包中查看。</p>{{end}}
{{if not .Report}}<p class="timing-note">{{if .Window}}该补查未包含呈帧回调间隔数据，可重新采样后补查。{{else}}该记录未采集呈帧回调间隔数据，重新采样可补充。{{end}}</p>{{end}}
</section>
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
.player-network-requests{max-height:26rem;border:1px solid #d5deea;border-radius:.375rem}
.player-network-requests th,.player-network-requests td{white-space:nowrap;font-size:.8rem}
.player-network-requests thead th{position:sticky;top:0;background:#f7f9fc}
.player-network-requests tbody tr:nth-child(even){background:#f7f9fc}
.player-network-requests:focus-visible{outline:2px solid #164d92;outline-offset:2px}
.player-network-phase-list{margin-top:.8rem}
.player-network-phase-detail{border:1px solid #d5deea;border-radius:.375rem;margin-block:.5rem;padding:0 .7rem}
.player-network-phase-detail summary{min-height:2.75rem;padding-block:.55rem;cursor:pointer;font-size:.85rem;font-weight:600;overflow-wrap:anywhere}
.player-network-phase-detail p{overflow-wrap:anywhere}
.player-network-hop-table{max-height:24rem;border:1px solid #d5deea;border-radius:.375rem}
.player-network-hop-table th,.player-network-hop-table td{white-space:nowrap;font-size:.8rem}
.player-network-hop-table thead th{position:sticky;top:0;background:#f7f9fc}
.player-network-hop-table:focus-visible{outline:2px solid #164d92;outline-offset:2px}
.network-terminal-failure{color:#854b16}
.http-header-block{border:1px solid #d5deea;border-radius:.375rem;margin:.5rem 0;padding:0 .7rem}
.http-header-block summary{cursor:pointer;min-height:2.75rem;padding:.55rem 0;font-size:.85rem;font-weight:650;overflow-wrap:anywhere;box-sizing:border-box}
.http-header-offset{margin-left:.75rem;color:#52657e;font:400 .75rem ui-monospace,monospace;white-space:nowrap}
.http-header-values{margin-bottom:.8rem}
.http-header-values h4{font-size:.85rem;margin:.4rem 0;color:#52657e}
.http-header-table{border:1px solid #d5deea;border-radius:.25rem}
.http-header-table table{table-layout:fixed}
.http-header-table th:first-child{width:31%}
.http-header-table th,.http-header-table td{white-space:pre-wrap;overflow-wrap:anywhere;vertical-align:top}
.http-header-table td{font:.8rem/1.6 ui-monospace,monospace}
.player-buffer-chart{margin:1rem 0;border:1px solid #d5deea;border-radius:.375rem;background:#f7f9fc;padding:.5rem .65rem}
.player-buffer-chart figcaption{color:#52657e;font-size:.875rem}
.player-buffer-chart svg{display:block;width:100%;height:auto}
.player-buffer-chart text{fill:#52657e;font:12px ui-monospace,monospace}
.player-buffer-chart .grid{stroke:#d4dfec;stroke-width:1}
.player-buffer-chart .line{fill:none;stroke:#164d92;stroke-width:2}
.player-buffer-chart .point{fill:#164d92}
.player-buffer-chart .stall{fill:#ecc781;opacity:.55}
.player-video-report{margin-block:.9rem}
.player-presentation-report{margin-block:.9rem}
.player-presentation-report .scroll{max-height:24rem;border:1px solid #d5deea;border-radius:.375rem}
.player-presentation-report th,.player-presentation-report td{white-space:nowrap;font-size:.8rem}
.player-presentation-report thead th{position:sticky;top:0;background:#f7f9fc}
.player-presentation-report .scroll:focus-visible{outline:2px solid #164d92;outline-offset:2px}
.player-presentation-report p{overflow-wrap:anywhere}
.player-scheduling-report{margin-block:.9rem}
.player-scheduling-report h4{font-size:.9rem;margin-bottom:.4rem}
.player-scheduling-report .scroll{max-height:24rem;border:1px solid #d5deea;border-radius:.375rem}
.player-scheduling-report th,.player-scheduling-report td{white-space:nowrap;font-size:.8rem}
.player-scheduling-report thead th{position:sticky;top:0;background:#f7f9fc}
.player-scheduling-report p{overflow-wrap:anywhere}
.player-scheduling-report .scroll:focus-visible{outline:2px solid #164d92;outline-offset:2px}
.video-window-samples summary{cursor:pointer;min-height:2.75rem;padding-block:.5rem;font-size:.875rem;font-weight:600}
.video-window-samples table{white-space:nowrap}
.stall-investigation{border-color:#9fb4ce}
.stall-investigation h3{margin-top:1rem}
.stall-investigation p{overflow-wrap:anywhere}
.stall-images{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,190px),1fr));gap:.75rem}
.stall-images figure{margin:0;border:1px solid #d5deea;padding:.45rem;border-radius:.375rem}
.stall-images img{display:block;width:100%;height:auto;background:#101d2d}
.stall-images figcaption{font-size:.8rem;overflow-wrap:anywhere}
.stall-assessment{border:1px solid #d5deea;border-radius:.375rem;padding:.2rem .75rem;margin-block:.8rem;background:#f7f9fc}
.stall-assessment-conclusion{font-weight:620}
.stall-assessment a,.stall-assessment p,.stall-assessment li{overflow-wrap:anywhere}
.stall-direction{padding-bottom:.3rem;margin-block:.65rem;border-bottom:1px solid #d5deea}
.stall-direction:last-of-type{border-bottom:0}
.direction-state{display:inline-block;font:.75rem system-ui,sans-serif;font-weight:650;padding:.12rem .42rem;border-radius:.25rem;color:#465b73;background:#ecf1f7}
.stall-direction[data-state="observed"] .direction-state{color:#216247;background:#e5f3eb}
.stall-direction[data-state="candidate"] .direction-state{color:#774705;background:#fff2d7}
@media print{body{background:#fff}article{break-inside:avoid}.request-timing th,.request-timing td{white-space:normal}.http-header-block:not([open])>:not(summary),.player-network-phase-detail:not([open])>:not(summary){display:block}.player-network-requests,.player-network-hop-table,.player-scheduling-report .scroll,.player-presentation-report .scroll{max-height:none}}
</style>
</head>
<body><main>
{{if .BackURL}}<p><a href="{{.BackURL}}">返回排障工作台</a></p>{{end}}
<h1>{{.Protocol}} 排障报告</h1>
<p>规则版本 <code>{{.Report.RuleVersion}}</code></p>
{{with .Report.Manifest.Observation}}
<article><h2>采样概况</h2>
<dl><dt>{{if $.Datagram}}监听地址{{else}}来源{{end}}</dt><dd>{{reportOrigin $.Report.Manifest.Protocol .Origin}}</dd>
<dt>开始时间</dt><dd>{{.StartedAt.Format "2006-01-02 15:04:05 UTC"}}</dd>
<dt>采样时长</dt><dd>{{printf "%.3f" (seconds .ElapsedMS)}} 秒 / 计划 {{.RequestedSeconds}} 秒</dd>
<dt>结束原因</dt><dd>{{finish .EndReason}}</dd>
{{if .HTTPStatus}}<dt>HTTP 状态</dt><dd>{{.HTTPStatus}}</dd>{{end}}
{{if not $.HTTPRequests}}{{with .TTFBMS}}<dt>{{if eq $.Report.Manifest.Protocol "rtmp"}}RTMP 首字节{{else if isRTSP $.Report.Manifest.Protocol}}RTSP 首字节{{else}}响应首字节{{end}}</dt><dd>{{.}} ms</dd>{{end}}{{end}}
</dl></article>
{{$metrics := $.Report.Metrics}}
{{$firstPayload := .FirstPayloadMS}}
{{$firstVideo := $metrics.FirstVideoMS}}{{$firstAudio := $metrics.FirstAudioMS}}
{{if isRTSP $.Report.Manifest.Protocol}}{{$firstPayload = rtspFirstPacket $.Report.RTSP}}{{with $.Report.RTSP}}{{$firstVideo = .FirstVideoRTPMS}}{{$firstAudio = .FirstAudioRTPMS}}{{end}}{{end}}
{{$arrivals := or $firstPayload $firstVideo $firstAudio $metrics.FirstKeyframeMS}}
{{if or $.TimingStages $.HTTPRequests $arrivals .FailureStage .FailureCode}}
<article><h2>采样时序</h2>
<p class="timing-note">到达时间以采样开始为基准。</p>
{{$unit := $metrics.MediaTimingUnit}}
{{if isRTSP $.Report.Manifest.Protocol}}{{$unit = "rtp"}}{{end}}
{{if not $unit}}{{if eq $.Report.Manifest.Protocol "hls"}}{{$unit = "segment"}}{{else if $.MPEGTS}}{{$unit = "pes"}}{{end}}{{end}}
<dl>
<dt>{{if isRTSP $.Report.Manifest.Protocol}}首个有效 RTP 包{{else}}首个载荷到达{{end}}</dt><dd>{{with $firstPayload}}{{.}} ms{{else}}未观测{{end}}</dd>
<dt>{{mediaArrival $unit "视频"}}</dt><dd>{{with $firstVideo}}{{.}} ms{{else}}未观测{{end}}</dd>
<dt>{{mediaArrival $unit "音频"}}</dt><dd>{{with $firstAudio}}{{.}} ms{{else}}未观测{{end}}</dd>
{{if eq $.Report.Manifest.Protocol "" "http-flv" "rtmp"}}<dt>首个关键帧到达</dt><dd>{{with $metrics.FirstKeyframeMS}}{{.}} ms{{else}}未观测{{end}}</dd>{{end}}
{{with .FailureStage}}<dt>故障阶段</dt><dd>{{stageName . $.LegacyRTMP $.Report.Manifest.Protocol}}</dd>{{end}}
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
<h3>HTTP 请求与响应头</h3>
<p class="timing-note">逐次请求展示客户端记录的头字段。敏感字段已脱敏，URL 查询参数已移除；字段大小写和顺序可能经过协议库归一化。</p>
{{range $.HTTPRequests}}
<details class="http-header-block"><summary>#{{.Number}}{{with .Kind}} · {{requestKind .}}{{end}} · {{if .HTTPStatus}}HTTP {{.HTTPStatus}}{{else}}未收到响应{{end}}{{with .Method}} · {{.}}{{end}}{{with .HTTPProtocol}} · {{.}}{{end}}<span class="http-header-offset">{{.StartMS}}–{{.EndMS}} ms</span></summary>
{{if .HeadersRecorded}}
<section class="http-header-values"><h4>请求头</h4>
{{if .RequestHeaders}}<div class="scroll http-header-table"><table><thead><tr><th scope="col">字段</th><th scope="col">值</th></tr></thead><tbody>
{{range .RequestHeaders}}<tr><td>{{.Name}}</td><td>{{.Value}}</td></tr>{{end}}
</tbody></table></div>{{else}}<p class="timing-note">未记录到请求头字段。</p>{{end}}
{{if .RequestHeadersTruncated}}<p class="timing-note">头字段达到记录上限，部分字段或值已截断。</p>{{end}}
</section>
<section class="http-header-values"><h4>响应头</h4>
{{if .ResponseHeaders}}<div class="scroll http-header-table"><table><thead><tr><th scope="col">字段</th><th scope="col">值</th></tr></thead><tbody>
{{range .ResponseHeaders}}<tr><td>{{.Name}}</td><td>{{.Value}}</td></tr>{{end}}
</tbody></table></div>{{else}}<p class="timing-note">{{if .HTTPStatus}}未记录到响应头字段。{{else}}未收到 HTTP 响应头。{{end}}</p>{{end}}
{{if .ResponseHeadersTruncated}}<p class="timing-note">头字段达到记录上限，部分字段或值已截断。</p>{{end}}
</section>
{{else}}<p class="timing-note">该记录未采集 HTTP Header。</p>{{end}}
</details>
{{end}}
{{end}}
{{if $.TimingStages}}<div class="scroll"><table>
<thead><tr><th scope="col">阶段</th><th scope="col">开始偏移 (ms)</th><th scope="col">结束偏移 (ms)</th><th scope="col">耗时 (ms)</th></tr></thead>
<tbody>{{range $.TimingStages}}<tr><th scope="row">{{stageName .Name $.LegacyRTMP $.Report.Manifest.Protocol}}</th><td>{{.StartMS}}</td><td>{{.EndMS}}</td><td>{{elapsed .StartMS .EndMS}}</td></tr>{{end}}</tbody>
</table></div>{{end}}
</article>
{{end}}
{{end}}
{{with .Report.Player}}
<article><h2>播放器体验</h2>
<p class="timing-note">首屏与开始播放从提交采样计时。缓冲曲线和卡顿区间从后端采样开始计时。</p>
<dl>
<dt>状态</dt><dd>{{playerState .}}</dd>
<dt>来源</dt><dd>{{playerSource .Source}}</dd>
<dt>首屏{{if eq .FirstScreenMethod "loadeddata_estimate"}}（加载首帧估算）{{end}}</dt><dd>{{with .FirstScreenMS}}{{.}} ms{{else}}未观测{{end}}</dd>
<dt>开始播放</dt><dd>{{with .FirstPlayingMS}}{{.}} ms{{else}}未观测{{end}}</dd>
<dt>卡顿次数{{with .StallThresholdMS}}（≥ {{.}} ms）{{end}}</dt><dd>{{if .FirstPlayingMS}}{{.StallCount}}{{else}}未观测{{end}}</dd>
<dt>累计卡顿</dt><dd>{{if .FirstPlayingMS}}{{.TotalStallMS}} ms{{else}}未观测{{end}}</dd>
<dt>最长卡顿</dt><dd>{{if .FirstPlayingMS}}{{.MaxStallMS}} ms{{else}}未观测{{end}}</dd>
<dt>播放观察时长</dt><dd>{{if .FirstPlayingMS}}{{.ActiveMS}} ms{{else}}未观测{{end}}</dd>
{{with .MinBufferMS}}<dt>最小可播放缓冲</dt><dd>{{.}} ms</dd>{{end}}
</dl>
<p class="timing-note">卡顿统计从开始播放到采样结束，计入{{with .StallThresholdMS}}至少 {{.}} ms 的{{end}}等待。暂停、跳转和后台期间停止计时。正常结束后可短暂补记首屏。</p>
{{template "playerVideo" .Video}}
{{template "playerPresentation" (presentationView .Presentation)}}
{{template "playerScheduling" (schedulingView .Scheduling)}}
{{with $.PlayerChart}}
<figure class="player-buffer-chart"><figcaption>可播放缓冲{{if .Stalls}} · 阴影为卡顿区间{{end}}</figcaption>
<svg viewBox="0 0 615 152" role="img" aria-label="可播放缓冲，横轴是距采样开始的秒数">
{{range .YTicks}}<line x1="45" y1="{{.Position}}" x2="585" y2="{{.Position}}" class="grid"/><text x="40" y="{{.Position}}" dy=".34em" text-anchor="end">{{.Label}}</text>{{end}}
{{range .Stalls}}<rect x="{{.X}}" y="18" width="{{.Width}}" height="94" class="stall"/>{{end}}
<polyline points="{{.Points}}" class="line"/>
{{if .SinglePoint}}<circle cx="{{.PointX}}" cy="{{.PointY}}" r="3" class="point"/>{{end}}
{{range .XTicks}}<text x="{{.Position}}" y="131" text-anchor="middle">{{.Label}}</text>{{end}}
<text x="315" y="149" text-anchor="middle">距采样开始</text>
</svg></figure>
{{end}}
{{if .Stalls}}<h3>缓冲卡顿区间</h3><div class="scroll"><table>
<thead><tr><th scope="col">开始偏移 (ms)</th><th scope="col">结束偏移 (ms)</th><th scope="col">耗时 (ms)</th></tr></thead>
<tbody>{{range .Stalls}}<tr><td>{{.StartMS}}</td><td>{{.EndMS}}</td><td>{{.DurationMS}}</td></tr>{{end}}</tbody>
</table></div>{{end}}
</article>
{{end}}
{{with .Report.PlayerNetwork}}
<article id="player-network-report"><h2>HLS 实际播放请求 · 代理观测</h2>
{{template "playerNetworkNotes" .}}
<dl><dt>观测边界</dt><dd>{{networkBoundary .Boundary}}</dd><dt>记录状态</dt><dd>{{if .Complete}}已结束{{else}}部分请求快照{{end}}</dd><dt>已保存请求</dt><dd>{{len .Requests}}</dd><dt>上限外丢弃请求</dt><dd>{{.DroppedRequests}}</dd></dl>
{{if .DroppedRequests}}<p class="timing-note">采集达到上限，另有 {{.DroppedRequests}} 条请求未写入本记录，不能按完整请求历史分析。</p>{{end}}
{{if .DroppedHops}}<p class="timing-note">逐跳记录达到采集上限或元数据预算，共有 {{.DroppedHops}} 跳未保存。</p>{{end}}
{{template "playerNetworkRequests" .Requests}}
</article>
{{else}}{{if and .Report.Player (eq .Report.Manifest.Protocol "hls")}}<article id="player-network-report"><h2>HLS 实际播放请求 · 代理观测</h2>{{template "playerNetworkNotes" .}}<p class="timing-note">该记录未采集实际播放请求，重新采样可补充代理侧请求证据。</p></article>{{end}}{{end}}
{{if .Investigations}}
<h2>播放异常证据补查</h2>
<p class="timing-note">缓冲卡顿证据补查与呈帧回调间隔补查共享最多 8 个案例，两类区间独立统计。</p>
<p class="timing-note">时间区间以采样开始为基准。片段与截图来自采样源流，用于复核媒体内容；原拉流等待和播放器停顿保留在时序证据中。</p>
{{range .Investigations}}
{{$case := .}}
<article class="stall-investigation" id="investigation-{{.ID}}">
<h3>{{.ID}} · {{investigationTitle .StallInvestigation}} · {{investigationState .Status}}</h3>
<dl><dt>{{if eq .IntervalKind "presentation_gap"}}呈帧回调间隔{{else}}缓冲卡顿区间{{end}}</dt><dd>{{.Stall.StartMS}}–{{.Stall.EndMS}} ms · {{.Stall.DurationMS}} ms</dd>
<dt>补查窗口</dt><dd>{{.Correlation.WindowStartMS}}–{{.Correlation.WindowEndMS}} ms</dd>
<dt>数据来源</dt><dd>{{playerSource .Correlation.Source}}</dd></dl>
{{if eq .IntervalKind "presentation_gap"}}<p class="timing-note">本案例分析呈帧回调间隔，不代表已确认画面冻结；与缓冲卡顿重合的部分仍保留在独立统计中。</p>{{end}}
{{if eq .Correlation.Source "independent_pull"}}<p class="timing-note">播放器与采样使用独立连接，关联结果供对照。</p>{{end}}
{{range .Correlation.Notes}}<p class="timing-note">{{.}}</p>{{end}}
{{template "stallAssessment" .StallInvestigation}}
{{with .Chart}}
<figure class="player-buffer-chart"><figcaption>补查窗口可播放缓冲 · 阴影为{{intervalName $case.IntervalKind}}</figcaption>
<svg viewBox="0 0 615 152" role="img" aria-label="补查窗口缓冲曲线">
{{range .YTicks}}<line x1="45" y1="{{.Position}}" x2="585" y2="{{.Position}}" class="grid"/><text x="40" y="{{.Position}}" dy=".34em" text-anchor="end">{{.Label}}</text>{{end}}
{{range .Stalls}}<rect x="{{.X}}" y="18" width="{{.Width}}" height="94" class="stall"/>{{end}}
<polyline points="{{.Points}}" class="line"/>
{{if .SinglePoint}}<circle cx="{{.PointX}}" cy="{{.PointY}}" r="3" class="point"/>{{end}}
{{range .XTicks}}<text x="{{.Position}}" y="131" text-anchor="middle">{{.Label}}</text>{{end}}
<text x="315" y="149" text-anchor="middle">距采样开始</text>
</svg></figure>{{end}}
{{if .VideoSamples}}<details class="video-window-samples"><summary>窗口视频样本 · {{len .VideoSamples}} 条</summary>
<p class="timing-note">此处显示采样时的原始累计计数；距最近呈帧回调的时间受播放器状态和页面调度影响。</p>
<div class="scroll"><table><thead><tr><th scope="col">采样偏移 (ms)</th><th scope="col">播放状态</th><th scope="col">视频帧计数</th><th scope="col">丢帧计数</th><th scope="col">呈帧计数</th><th scope="col">距最近呈帧回调</th></tr></thead>
<tbody>{{range .VideoSamples}}<tr><td>{{.OffsetMS}}</td><td>{{videoSampleState .}}</td>{{with .Video}}<td>{{videoCount .TotalFrames}}</td><td>{{videoCount .DroppedFrames}}</td><td>{{videoCount .PresentedFrames}}</td><td>{{videoMS .FrameAgeMS}}</td>{{end}}</tr>{{end}}</tbody></table></div></details>{{end}}
{{template "playerScheduling" (schedulingCaseView .Correlation.Scheduling .StallInvestigation)}}
{{template "playerPresentation" (presentationView .Correlation.Presentation .StallInvestigation)}}
<h3>关联收流间隔</h3>
{{if .Correlation.Gaps}}<div class="scroll"><table><thead><tr><th scope="col">媒体</th><th scope="col">开始偏移 (ms)</th><th scope="col">结束偏移 (ms)</th><th scope="col">间隔 (ms)</th></tr></thead>
<tbody>{{range .Correlation.Gaps}}<tr><td>{{if eq .Kind "video"}}视频{{else if eq .Kind "audio"}}音频{{else}}{{.Kind}}{{end}}</td><td>{{.StartMS}}</td><td>{{.EndMS}}</td><td>{{.DurationMS}}</td></tr>{{end}}</tbody></table></div>
{{else}}<p class="timing-note">窗口内没有可关联的音视频接收间隔记录。</p>{{end}}
<h3>关键帧记录</h3>
{{if .Correlation.Keyframes}}<div class="scroll"><table><thead><tr><th scope="col">到达偏移 (ms)</th><th scope="col">DTS (ms)</th><th scope="col">PTS (ms)</th><th scope="col">编码</th><th scope="col">原始条目 / 偏移</th></tr></thead>
<tbody>{{range .Correlation.Keyframes}}<tr><td>{{.ArrivalMS}}</td><td>{{.DTS}}</td><td>{{.PTS}}</td><td>{{.Codec}}</td><td>{{if .Artifact}}{{.Artifact}}{{else}}{{.Offset}}{{end}}</td></tr>{{end}}</tbody></table></div>
{{else}}<p class="timing-note">当前证据未识别到可关联的关键帧，不能据此判断关键帧是否缺失。</p>{{end}}
{{if .Correlation.Requests}}<h3>关联采样 HTTP 请求</h3><div class="scroll"><table><thead><tr><th scope="col">请求</th><th scope="col">对象</th><th scope="col">HTTP</th><th scope="col">开始偏移 (ms)</th><th scope="col">响应头偏移 (ms)</th><th scope="col">TTFB (ms)</th></tr></thead>
<tbody>{{range .Correlation.Requests}}<tr><td>#{{.Number}}</td><td>{{requestKind .Kind}}</td><td>{{.HTTPStatus}}</td><td>{{.StartMS}}</td><td>{{.EndMS}}</td><td>{{with .TTFBMS}}{{.}}{{else}}—{{end}}</td></tr>{{end}}</tbody></table></div>{{end}}
{{if or .Correlation.PlayerRequests (eq $.Report.Manifest.Protocol "hls")}}<section class="stall-player-requests"><h3>关联实际播放请求</h3>{{template "playerNetworkNotes" .}}{{template "playerNetworkRequests" .Correlation.PlayerRequests}}</section>{{end}}
<h3>关联证据</h3>
{{range .Correlation.Findings}}<section id="case-{{.ID}}"><h4>{{.ID}} · {{.Message}}</h4><p class="timing-note">{{.StartMS}}–{{.EndMS}} ms</p>{{with .NextCheck}}<p>{{.}}</p>{{end}}
{{if .Evidence}}<div class="scroll"><table><thead><tr><th scope="col">对象</th><th scope="col">到达偏移 (ms)</th><th scope="col">DTS (ms)</th><th scope="col">PTS (ms)</th><th scope="col">原始条目 / 偏移</th></tr></thead><tbody>{{range .Evidence}}<tr><td>{{.Kind}}</td><td>{{.ArrivalMS}}</td><td>{{.DTS}}</td><td>{{.PTS}}</td><td>{{if .Artifact}}{{.Artifact}}{{else}}{{.Offset}}{{end}}</td></tr>{{end}}</tbody></table></div>{{end}}</section>{{end}}
<h3>异常附近的源流片段与截图</h3>
{{if eq .IntervalKind "presentation_gap"}}<p class="timing-note">同期源流片段和截图供内容对照，不能重现或证明本机画面冻结。</p>{{end}}
<p>{{investigationMedia .Media.Status}}{{with .Media.Reason}} · {{.}}{{end}}</p>
{{with .Media.Alignment}}<p class="timing-note">媒体定位方式 {{investigationAlignment .}}</p>{{end}}
{{if .Media.StartMS}}<p class="timing-note">源流媒体时间 {{.Media.StartMS}}–{{.Media.EndMS}} ms</p>{{end}}
{{if .Images}}<div class="stall-images">{{range .Images}}<figure><img src="{{.DataURL}}" alt="{{.Label}}"><figcaption>{{.Label}}{{with .MediaTimeMS}} · 源流媒体时间 {{.}} ms{{end}} · {{.Bytes}} 字节</figcaption></figure>{{end}}</div>{{end}}
{{with .ImagesOmitted}}<p class="timing-note">达到报告截图内嵌上限，另有 {{.}} 张截图通过链接或证据包查看。</p>{{end}}
{{range .OtherImages}}<p><a href="{{.Href}}">下载源流截图 · {{.Bytes}} 字节</a></p>{{end}}
{{range .Clips}}<p><a href="{{.Href}}">下载源流片段 · {{.Bytes}} 字节</a></p>{{end}}
{{if .Media.Assets}}<p class="timing-note">媒体文件保存在证据包中；独立 HTML 内嵌可用截图，片段通过下载链接或解压证据包后查看。</p>{{end}}
<h3>模型建议（时序证据）</h3>
<p class="timing-note">模型依据关联的接收间隔、缓冲与关键帧记录提出待验证建议，源流片段和截图供复核。</p>
{{with .Explanation}}<p class="timing-note">解读来源 {{modelProvider .Provider}}{{if .Model}} · {{.Model}}{{end}}</p><p>{{.Summary}}</p>
{{range .Hypotheses}}<h4>待验证 · {{.Claim}}</h4><p>{{range .EvidenceIDs}}<a href="#case-{{.}}">{{.}}</a> {{end}}</p><p>{{.NextCheck}}</p>{{end}}
{{else}}<p>{{investigationModel .ModelState}}</p>{{end}}
{{with .ModelError}}<p class="timing-note">{{.}}</p>{{end}}
</article>{{end}}{{end}}
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
{{else if isRTSP .Report.Manifest.Protocol}}{{rtspReport .Report}}
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
{{if playerEvidence .Evidence}}
<thead><tr><th scope="col">对象</th><th scope="col">开始偏移 (ms)</th><th scope="col">结束偏移 (ms)</th><th scope="col">耗时 (ms)</th><th scope="col">原始记录</th></tr></thead>
<tbody>{{range .Evidence}}<tr><td>{{if eq .Kind "player_stall"}}播放器卡顿{{else}}{{.Kind}}{{end}}</td><td>{{.ArrivalMS}}</td><td>{{intervalEnd .ArrivalMS .DurationMS}}</td><td>{{.DurationMS}}</td><td><code>{{.Artifact}}</code></td></tr>{{end}}</tbody>
{{else if isRTSP $.Report.Manifest.Protocol}}{{rtspEvidenceRows .Evidence}}
{{else if eq $.Report.Manifest.Protocol "hls"}}
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
	case "rtp":
		return "首个" + track + " RTP 到达"
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

func playerVideoQualitySourceLabel(source string) string {
	switch source {
	case "playback_quality":
		return "VideoPlaybackQuality"
	case "webkit":
		return "WebKit 视频计数"
	case "mixed":
		return "混合计数来源"
	default:
		return "未提供帧质量计数"
	}
}

func playerVideoObservedCount(value *int64) string {
	if value == nil {
		return "未观测"
	}
	return strconv.FormatInt(*value, 10)
}

func playerVideoObservedTime(value *int64) string {
	if value == nil {
		return "未观测"
	}
	return strconv.FormatInt(*value, 10) + " ms"
}

func playerVideoDroppedRatio(value *float64) string {
	if value == nil {
		return "未观测"
	}
	return fmt.Sprintf("%.2f%%", *value*100)
}

func playerVideoSampleState(sample PlayerBufferSample) string {
	var states []string
	if sample.Paused {
		states = append(states, "暂停")
	}
	if sample.Seeking {
		states = append(states, "跳转")
	}
	if sample.Hidden {
		states = append(states, "后台")
	}
	if len(states) == 0 {
		return "前台"
	}
	return strings.Join(states, " / ")
}

func playerSourceLabel(source string) string {
	switch source {
	case "same_capture":
		return "同一采样流"
	case "independent_pull":
		return "独立 HLS 拉流"
	default:
		return source
	}
}

func playerReportState(player *PlayerReport) string {
	switch player.State {
	case "awaiting":
		return "等待播放器数据"
	case "recording":
		return "采集中"
	case "complete":
		if player.FirstPlayingMS == nil {
			return "采集结束，未开始播放"
		}
		return "采集完成"
	case "unsupported":
		for _, event := range player.Events {
			if event.Kind == "unsupported" && event.Code == "preview_unsupported" {
				return "RTSP 本机预览编码或封装不支持"
			}
		}
		return "当前环境不支持该播放格式"
	case "autoplay_blocked":
		return "自动播放受限"
	case "error":
		for i := len(player.Events) - 1; i >= 0; i-- {
			event := player.Events[i]
			if event.Kind != "error" {
				continue
			}
			switch event.Code {
			case "media_error":
				return "播放器媒体解码失败"
			case "network_error":
				return "播放器网络请求失败"
			case "no_stream":
				return "播放器未收到媒体流"
			case "preview_failed":
				return "RTSP 本机转封装失败，详见原生预览原因"
			case "preview_queue_limit":
				return "RTSP 本机预览队列达到上限"
			}
			break
		}
		return "播放失败"
	default:
		return player.State
	}
}

type playerChartTick struct {
	Position float64
	Label    string
}

type playerChartSpan struct{ X, Width float64 }

type playerChartData struct {
	Points         string
	SinglePoint    bool
	PointX, PointY float64
	XTicks, YTicks []playerChartTick
	Stalls         []playerChartSpan
}

func playerChart(player *PlayerReport, elapsed int64) *playerChartData {
	return playerChartRange(player, elapsed, 0)
}

func playerChartRange(player *PlayerReport, elapsed, startMS int64) *playerChartData {
	if player == nil || len(player.BufferSamples) == 0 {
		return nil
	}
	duration, maximum := max(1, elapsed), 1000.0
	for _, sample := range player.BufferSamples {
		duration = max(duration, sample.OffsetMS)
		maximum = max(maximum, math.Ceil(float64(sample.BufferMS)/1000)*1000)
	}
	for _, stall := range player.Stalls {
		duration = max(duration, stall.EndMS)
	}
	duration = max(1, duration-startMS)
	x := func(ms float64) float64 { return 45 + 540*max(0, ms-float64(startMS))/float64(duration) }
	y := func(ms float64) float64 { return 112 - 94*ms/maximum }
	chart := &playerChartData{SinglePoint: len(player.BufferSamples) == 1}
	var points strings.Builder
	for _, sample := range player.BufferSamples {
		fmt.Fprintf(&points, "%.1f,%.1f ", x(float64(sample.OffsetMS)), y(float64(sample.BufferMS)))
	}
	chart.Points = strings.TrimSpace(points.String())
	first := player.BufferSamples[0]
	chart.PointX, chart.PointY = x(float64(first.OffsetMS)), y(float64(first.BufferMS))
	for _, stall := range player.Stalls {
		start, end := x(float64(stall.StartMS)), x(float64(stall.EndMS))
		chart.Stalls = append(chart.Stalls, playerChartSpan{start, max(0, end-start)})
	}
	chart.XTicks = []playerChartTick{
		{x(float64(startMS)), fmt.Sprintf("%gs", float64(startMS)/1000)},
		{x(float64(startMS) + float64(duration)/2), fmt.Sprintf("%.1fs", float64(startMS)/1000+float64(duration)/2000)},
		{x(float64(startMS + duration)), fmt.Sprintf("%.1fs", float64(startMS+duration)/1000)},
	}
	for _, value := range []float64{0, maximum / 2, maximum} {
		chart.YTicks = append(chart.YTicks, playerChartTick{y(value), fmt.Sprintf("%gs", value/1000)})
	}
	return chart
}

func RenderHTML(w io.Writer, report Report, explanation *Explanation, backURL string) error {
	return RenderHTMLWithAssets(w, report, explanation, backURL, nil)
}

// RenderHTMLWithAssets embeds validated JPEG evidence so a saved report can
// show source screenshots without a server or a network connection.
func RenderHTMLWithAssets(w io.Writer, report Report, explanation *Explanation, backURL string, assets map[string][]byte) error {
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
	case ProtocolRTSP:
		protocol = "RTSP"
	case ProtocolRTSPS:
		protocol = "RTSPS"
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
		Report         Report
		Explanation    *Explanation
		BackURL        string
		Protocol       string
		MPEGTS         bool
		Datagram       bool
		RTP            bool
		HTTPRequests   []HTTPRequestTiming
		HTTPPhases     []struct{ Name, Label string }
		HTTPFailures   bool
		TimingStages   []CaptureStage
		LegacyRTMP     bool
		PlayerChart    *playerChartData
		Investigations []investigationHTML
	}{report, explanation, backURL, protocol, mpegTS, datagram, rtp, requests, httpTimingPhases, hasHTTPFailures, stages,
		report.Manifest.Protocol == ProtocolRTMP && !hasRTMPHandshake, playerChart(report.Player, report.Manifest.Observation.ElapsedMS), investigationHTMLItems(report.StallInvestigations, backURL, assets)})
}

type investigationHTML struct {
	StallInvestigation
	Images        []investigationHTMLImage
	ImagesOmitted int
	OtherImages   []investigationHTMLClip
	Clips         []investigationHTMLClip
	Chart         *playerChartData
	VideoSamples  []PlayerBufferSample
}

type investigationHTMLImage struct {
	Label       string
	Bytes       int64
	MediaTimeMS *int64
	DataURL     template.URL
}

type investigationHTMLClip struct {
	Href  string
	Bytes int64
}

var investigationHTMLAssetName = regexp.MustCompile(`^stalls/(S[0-9]{3})/(clip\.mp4|before\.jpg|during\.jpg|after\.jpg)$`)
var investigationHTMLRunID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func investigationHTMLItems(items []StallInvestigation, backURL string, assets map[string][]byte) []investigationHTML {
	var result []investigationHTML
	embeddedBytes := 0
	for _, item := range items {
		view := investigationHTML{StallInvestigation: item}
		for _, sample := range item.Correlation.BufferSamples {
			if sample.Video != nil && (sample.Video.TotalFrames != nil || sample.Video.DroppedFrames != nil || sample.Video.PresentedFrames != nil || sample.Video.FrameAgeMS != nil) {
				view.VideoSamples = append(view.VideoSamples, sample)
			}
		}
		if len(item.Correlation.BufferSamples) > 0 {
			view.Chart = playerChartRange(&PlayerReport{BufferSamples: item.Correlation.BufferSamples, Stalls: []PlayerStall{item.Stall}}, item.Correlation.WindowEndMS, item.Correlation.WindowStartMS)
		}
		for _, asset := range item.Media.Assets {
			parts := investigationHTMLAssetName.FindStringSubmatch(asset.Name)
			if len(parts) != 3 || parts[1] != item.ID {
				continue
			}
			if asset.Kind == "clip" && parts[2] == "clip.mp4" {
				view.Clips = append(view.Clips, investigationHTMLClip{Href: investigationAssetHTMLHref(asset.Name, backURL), Bytes: asset.Bytes})
				continue
			}
			data := assets[asset.Name]
			if asset.Kind != "screenshot" || parts[2] == "clip.mp4" || len(data) == 0 || len(data) > maxDerivedImageBytes || int64(len(data)) != asset.Bytes {
				if asset.Kind == "screenshot" && parts[2] != "clip.mp4" {
					view.OtherImages = append(view.OtherImages, investigationHTMLClip{Href: investigationAssetHTMLHref(asset.Name, backURL), Bytes: asset.Bytes})
				}
				continue
			}
			hash := sha256.Sum256(data)
			if hex.EncodeToString(hash[:]) != asset.SHA256 {
				continue
			}
			config, err := jpeg.DecodeConfig(bytes.NewReader(data))
			if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 {
				continue
			}
			if embeddedBytes+len(data) > 8<<20 {
				view.ImagesOmitted++
				view.OtherImages = append(view.OtherImages, investigationHTMLClip{Href: investigationAssetHTMLHref(asset.Name, backURL), Bytes: asset.Bytes})
				continue
			}
			embeddedBytes += len(data)
			label := "异常附近源流截图"
			if parts[2] == "before.jpg" {
				label = "异常前源流截图"
			}
			if parts[2] == "after.jpg" {
				label = "异常后源流截图"
			}
			view.Images = append(view.Images, investigationHTMLImage{Label: label, Bytes: asset.Bytes, MediaTimeMS: asset.MediaTimeMS,
				DataURL: template.URL("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data))})
		}
		result = append(result, view)
	}
	return result
}

type schedulingHTMLData struct {
	Report       *PlayerSchedulingReport
	Native       *PlayerSchedulingMetrics
	Title        string
	HasStall     bool
	OverlapLabel string
	Rows         []schedulingHTMLInterval
	Omitted      int
}

type schedulingHTMLInterval struct {
	Kind       string
	StartMS    int64
	EndMS      int64
	DurationMS int64
	OverlapMS  int64
}

func playerSchedulingSupportLabel(support string) string {
	switch support {
	case "supported":
		return "支持 Long Tasks API"
	case "unsupported":
		return "当前环境不支持 Long Tasks API"
	default:
		return "接口支持状态未记录"
	}
}

func playerSchedulingView(report *PlayerSchedulingReport, stalls ...PlayerStall) schedulingHTMLData {
	view := schedulingHTMLData{Report: report, Title: "页面调度观测", HasStall: len(stalls) > 0, OverlapLabel: "与本次卡顿重合 (ms)"}
	if view.HasStall {
		view.Title = "补查窗口页面调度"
	}
	if report == nil {
		return view
	}
	if report.LongTaskSupport == "supported" {
		view.Native = report.LongTasks
	}
	for _, group := range []struct {
		kind    string
		metrics *PlayerSchedulingMetrics
	}{{"浏览器长任务", view.Native}, {"定时器延后", report.TimerDelays}} {
		if group.metrics == nil {
			continue
		}
		for _, interval := range group.metrics.Intervals {
			row := schedulingHTMLInterval{Kind: group.kind, StartMS: interval.StartMS, EndMS: interval.EndMS, DurationMS: interval.EndMS - interval.StartMS}
			if view.HasStall {
				row.OverlapMS = max(0, min(interval.EndMS, stalls[0].EndMS)-max(interval.StartMS, stalls[0].StartMS))
			}
			view.Rows = append(view.Rows, row)
		}
	}
	sort.SliceStable(view.Rows, func(i, j int) bool {
		if view.Rows[i].StartMS == view.Rows[j].StartMS {
			return view.Rows[i].EndMS < view.Rows[j].EndMS
		}
		return view.Rows[i].StartMS < view.Rows[j].StartMS
	})
	if len(view.Rows) > 200 {
		view.Omitted = len(view.Rows) - 200
		view.Rows = view.Rows[:200]
	}
	return view
}

func playerSchedulingCaseView(report *PlayerSchedulingReport, item StallInvestigation) schedulingHTMLData {
	view := playerSchedulingView(report, item.Stall)
	if item.IntervalKind == IntervalPresentationGap {
		view.OverlapLabel = "与呈帧回调间隔重合 (ms)"
	}
	return view
}

type presentationHTMLData struct {
	Report  *PlayerPresentationReport
	Title   string
	Window  bool
	Rows    []presentationHTMLGap
	Omitted int
}

type presentationHTMLGap struct {
	PlayerPresentationGapReport
	Index             int
	SelectedOverlapMS int64
}

func playerPresentationSupportLabel(support string) string {
	switch support {
	case "supported":
		return "支持视频帧回调"
	case "unsupported":
		return "当前环境不支持视频帧回调"
	default:
		return "未记录"
	}
}

func playerPresentationCountLabel(count *int) string {
	if count == nil {
		return "未观测"
	}
	return strconv.Itoa(*count)
}

func playerPresentationView(report *PlayerPresentationReport, cases ...StallInvestigation) presentationHTMLData {
	view := presentationHTMLData{Report: report, Title: "呈帧回调间隔", Window: len(cases) > 0}
	if view.Window {
		view.Title = "补查窗口呈帧回调间隔"
	}
	if report == nil {
		return view
	}
	for index, gap := range report.Gaps[:min(len(report.Gaps), maxPlayerPresentationGaps)] {
		row := presentationHTMLGap{PlayerPresentationGapReport: gap, Index: index + 1}
		if view.Window {
			row.SelectedOverlapMS = max(0, min(gap.EndMS, cases[0].Stall.EndMS)-max(gap.StartMS, cases[0].Stall.StartMS))
		}
		view.Rows = append(view.Rows, row)
	}
	view.Omitted = max(0, len(report.Gaps)-maxPlayerPresentationGaps)
	return view
}

func playerIntervalLabel(kind string) string {
	if kind == IntervalPresentationGap {
		return "呈帧回调间隔"
	}
	return "缓冲卡顿"
}

func playerInvestigationIntervalTitle(item StallInvestigation) string {
	if item.IntervalKind == IntervalPresentationGap {
		return fmt.Sprintf("第 %d 个呈帧回调间隔", item.StallIndex)
	}
	return fmt.Sprintf("第 %d 次卡顿（缓冲）", item.StallIndex)
}

const playerNetworkHTMLRowLimit = 200

var playerNetworkFailureStageLabels = map[string]string{
	"dns": "DNS 解析", "connect": "TCP 建连", "tls": "TLS 握手", "request_send": "发送请求",
	"response_first_byte": "等待响应首字节", "response_headers": "HTTP 响应头", "receive": "上游响应体阶段",
	"proxy_write": "向播放器写出", "proxy_limit": "代理资源限制", "playlist": "播放列表处理",
}

var playerNetworkFailureCodeLabels = map[string]string{
	"dns_error": "DNS 失败", "tls_error": "TLS 失败", "connection_refused": "连接被拒绝", "timeout": "超时", "canceled": "已取消",
	"transport_error": "传输错误", "redirect_error": "重定向失败", "http_error": "HTTP 错误", "limit_exceeded": "达到限制",
	"invalid_playlist": "播放列表无效", "proxy_write_error": "向播放器写出失败",
}

func playerNetworkFailureLabel(stage, code string) string {
	var parts []string
	for _, item := range []struct {
		value  string
		labels map[string]string
	}{{stage, playerNetworkFailureStageLabels}, {code, playerNetworkFailureCodeLabels}} {
		if item.value == "" {
			continue
		}
		label := item.value
		if text := item.labels[item.value]; text != "" {
			label = text + " [" + item.value + "]"
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return "未记录"
	}
	return strings.Join(parts, " · ")
}

func playerNetworkDetailedRequests(requests []PlayerNetworkRequest) []PlayerNetworkRequest {
	var details []PlayerNetworkRequest
	for _, request := range playerNetworkVisibleRequests(requests) {
		if len(request.Hops) > 0 || request.DroppedHops > 0 || request.FailureStage != "" || request.FailureCode != "" {
			details = append(details, request)
		}
	}
	return details
}

func playerNetworkPhaseMissingRequests(requests []PlayerNetworkRequest) int {
	return len(playerNetworkVisibleRequests(requests)) - len(playerNetworkDetailedRequests(requests))
}

func playerNetworkVisibleHops(hops []PlayerNetworkHop) []PlayerNetworkHop {
	return hops[:min(len(hops), maxPlayerNetworkHops)]
}

func playerNetworkOmittedHops(hops []PlayerNetworkHop) int {
	return max(0, len(hops)-maxPlayerNetworkHops)
}

func playerNetworkHasOmittedStages(hops []PlayerNetworkHop) bool {
	for _, hop := range playerNetworkVisibleHops(hops) {
		if len(hop.Stages) > maxPlayerNetworkHopStages {
			return true
		}
	}
	return false
}

func playerNetworkHopChain(hops []PlayerNetworkHop) string {
	var statuses []string
	for _, hop := range playerNetworkVisibleHops(hops) {
		status := "未观测"
		if hop.HTTPStatus > 0 {
			status = strconv.Itoa(hop.HTTPStatus)
		}
		statuses = append(statuses, status)
	}
	if len(statuses) == 0 {
		return ""
	}
	return " · HTTP " + strings.Join(statuses, " → ")
}

func playerNetworkPhaseDurationLabel(stages []CaptureStage, name string) string {
	var durations []string
	for _, stage := range stages[:min(len(stages), maxPlayerNetworkHopStages)] {
		if stage.Name == name {
			durations = append(durations, strconv.FormatInt(stage.EndMS-stage.StartMS, 10))
		}
	}
	if len(durations) == 0 {
		return "未观测"
	}
	return strings.Join(durations, " / ")
}

func playerNetworkConnectionLabel(reused *bool) string {
	if reused == nil {
		return "未观测"
	}
	if *reused {
		return "已复用"
	}
	return "未复用"
}

func playerNetworkKindLabel(kind string) string {
	switch kind {
	case "playlist":
		return "播放列表"
	case "media":
		return "媒体分片"
	case "init":
		return "初始化段"
	case "key":
		return "密钥"
	case "auxiliary":
		return "辅助资源"
	case "":
		return "未标注"
	default:
		return kind
	}
}

func playerNetworkOutcomeLabel(outcome string) string {
	switch outcome {
	case "complete":
		return "完成"
	case "http_error":
		return "上游 HTTP 错误"
	case "transport_error":
		return "传输错误"
	case "canceled":
		return "已取消"
	case "timeout":
		return "超时"
	case "incomplete":
		return "未完成"
	case "":
		return "未标注"
	default:
		return outcome
	}
}

func playerNetworkBoundaryLabel(boundary string) string {
	if boundary == "upstream_proxy" {
		return "本地代理上游侧"
	}
	if boundary == "" {
		return "未标注"
	}
	return boundary
}

func playerNetworkEndLabel(request PlayerNetworkRequest) string {
	label := strconv.FormatInt(request.EndMS, 10)
	if request.Outcome == "incomplete" {
		label += "（截至）"
	}
	return label
}

func playerNetworkVisibleRequests(requests []PlayerNetworkRequest) []PlayerNetworkRequest {
	return requests[:min(len(requests), playerNetworkHTMLRowLimit)]
}

func playerNetworkOmittedRequests(requests []PlayerNetworkRequest) int {
	return max(0, len(requests)-playerNetworkHTMLRowLimit)
}

func stallDirectionStateLabel(status string) string {
	switch status {
	case "observed":
		return "已观测"
	case "candidate":
		return "待验证"
	case "insufficient":
		return "证据不足"
	default:
		return "待核对"
	}
}

func stallAssessmentEvidenceKnown(item StallInvestigation, id string) bool {
	for _, finding := range item.Correlation.Findings {
		if finding.ID == id {
			return true
		}
	}
	if item.Assessment != nil {
		for _, finding := range item.Assessment.Findings {
			if finding.ID == id {
				return true
			}
		}
	}
	return false
}

func stallAssessmentExtraFindings(item StallInvestigation) []Finding {
	if item.Assessment == nil {
		return nil
	}
	seen := make(map[string]bool)
	for _, finding := range item.Correlation.Findings {
		seen[finding.ID] = true
	}
	var findings []Finding
	for _, finding := range item.Assessment.Findings {
		if !seen[finding.ID] {
			findings = append(findings, finding)
			seen[finding.ID] = true
		}
	}
	return findings
}

func investigationAssetHTMLHref(name, backURL string) string {
	if id := strings.TrimPrefix(backURL, "/diagnostics#"); strings.HasPrefix(backURL, "/diagnostics#") && investigationHTMLRunID.MatchString(id) {
		return "/api/diagnostics/runs/" + id + "/stall-assets/" + strings.TrimPrefix(name, "stalls/")
	}
	return name
}

func investigationStateLabel(state string) string {
	switch state {
	case "queued":
		return "等待补查"
	case "running":
		return "补查中"
	case "complete":
		return "补查完成"
	case "partial":
		return "保留部分结果"
	case "failed":
		return "补查失败"
	case "canceled":
		return "补查已停止"
	default:
		return state
	}
}

func investigationMediaLabel(state string) string {
	switch state {
	case "ready":
		return "片段与截图已提取"
	case "partial":
		return "媒体产物部分可用"
	case "unavailable":
		return "媒体产物不可用"
	case "failed":
		return "媒体提取失败"
	default:
		return "等待提取源流媒体"
	}
}

func investigationAlignmentLabel(alignment string) string {
	switch alignment {
	case "media_clock", "player_clock":
		return "播放器媒体时间对齐"
	case "player_clock_estimate":
		return "根据播放器媒体时间估算"
	case "arrival_estimate":
		return "按接收时序估算"
	case "independent_capture", "independent_capture_reference":
		return "独立采样连接参考"
	default:
		return alignment
	}
}

func investigationModelLabel(state string) string {
	switch state {
	case "pending":
		return "正在结合时序证据生成建议"
	case "complete":
		return "已生成建议"
	case "unconfigured":
		return "尚未配置大模型，可配置后生成建议"
	case "failed":
		return "模型建议生成失败，可重试"
	case "changed":
		return "模型配置已变化，可使用当前模型重新生成建议"
	default:
		return "等待模型建议"
	}
}
