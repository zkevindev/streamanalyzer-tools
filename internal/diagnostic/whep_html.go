package diagnostic

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
)

var whepReportPage = template.Must(template.New("whep-report").Funcs(template.FuncMap{
	"msPtr": func(v *int64) string {
		if v == nil {
			return "未观测"
		}
		return fmt.Sprintf("%d ms", *v)
	},
	"hexSSRC": func(s uint32) string {
		return fmt.Sprintf("0x%08X", s)
	},
	"join": func(xs []string) string {
		if len(xs) == 0 {
			return "-"
		}
		return strings.Join(xs, ", ")
	},
	"f2": func(v float64) string {
		return fmt.Sprintf("%.2f", v)
	},
	"f1": func(v float64) string {
		return fmt.Sprintf("%.1f", v)
	},
	"f1Ptr": func(v *float64) string {
		if v == nil {
			return "-"
		}
		return fmt.Sprintf("%.1f ms", *v)
	},
}).Parse(`<article class="section" aria-labelledby="whep-title"><h2 id="whep-title">WHEP 信令、ICE/DTLS 与 RTP 接收</h2>
{{if .Metrics}}
<p class="timing-note">观测边界：本机 Pion WebRTC 原生接收（解密后 RTP/RTCP 与 SDP 协商元数据归档于 stream.whep.zip）。</p>
<dl class="metrics">
  <div><dt>信令 HTTP 状态</dt><dd>{{if .Metrics.HTTPStatus}}{{.Metrics.HTTPStatus}}{{else}}失败{{end}}</dd></div>
  <div><dt>ICE 策略 / 状态</dt><dd>{{.Metrics.ICETransportPolicy}} / {{.Metrics.ICEState}}</dd></div>
  <div><dt>DTLS / Peer 状态</dt><dd>{{.Metrics.DTLSState}} / {{.Metrics.PeerState}}</dd></div>
  <div><dt>ICE 收集 / 建连</dt><dd>{{msPtr .Metrics.GatherMS}} / {{msPtr .Metrics.ICEConnectMS}}</dd></div>
  <div><dt>DTLS 握手</dt><dd>{{msPtr .Metrics.DTLSConnectMS}}</dd></div>
  <div><dt>首 RTP / 首 RTCP</dt><dd>{{msPtr .Metrics.FirstRTPMS}} / {{msPtr .Metrics.FirstRTCPMS}}</dd></div>
  <div><dt>候选（本端 / 远端）</dt><dd>{{.Metrics.LocalCandidates}} / {{.Metrics.RemoteCandidates}}</dd></div>
  <div><dt>会话清理 DELETE</dt><dd>{{if .Metrics.DeleteStatus}}HTTP {{.Metrics.DeleteStatus}}{{else}}未发送{{end}}</dd></div>
  {{if .Metrics.SelectedPair}}<div><dt>选中候选对</dt><dd>{{.Metrics.SelectedPair.LocalType}}({{.Metrics.SelectedPair.LocalProtocol}}) ↔ {{.Metrics.SelectedPair.RemoteType}}({{.Metrics.SelectedPair.RemoteProtocol}})</dd></div>{{end}}
  {{if .Metrics.AnswerSDP}}<div><dt>Answer 协商编码</dt><dd>视频: {{join .Metrics.AnswerSDP.VideoCodecs}} · 音频: {{join .Metrics.AnswerSDP.AudioCodecs}}{{if .Metrics.AnswerSDP.ICELite}} · ICE-Lite{{end}}</dd></div>{{end}}
</dl>
{{if .Metrics.Tracks}}
<div class="table-wrap"><table>
  <thead><tr><th scope="col">轨道</th><th scope="col">编码 / PT</th><th scope="col">SSRC</th><th scope="col">收包 / 字节</th><th scope="col">码率</th><th scope="col">丢包 / 丢包率</th><th scope="col">乱序 / 重复</th><th scope="col">Jitter (均值/峰值)</th><th scope="col">首关键帧 / PLI</th><th scope="col">RTCP SR/RR</th></tr></thead>
  <tbody>
  {{range .Metrics.Tracks}}
  <tr>
    <td>#{{.ID}} {{.Kind}}</td>
    <td><code>{{if .Codec}}{{.Codec}}{{else}}-{{end}}</code> / PT {{.PayloadType}}</td>
    <td><code>{{hexSSRC .SSRC}}</code></td>
    <td>{{.Packets}} / {{.Bytes}} B</td>
    <td>{{f1 .BitrateKbps}} kbps</td>
    <td>{{.MissingSequencePackets}} ({{f2 .LossRatePct}}%)</td>
    <td>{{.OutOfOrderPackets}} / {{.DuplicatePackets}}</td>
    <td>{{f2 .JitterMS}} / {{f2 .MaxJitterMS}} ms</td>
    <td>{{msPtr .FirstKeyframeMS}} / {{.PLICount}}次{{if .PLIFirstRecoveryMS}} (恢复 {{msPtr .PLIFirstRecoveryMS}}){{end}}</td>
    <td>{{.RTCPSRCount}} / {{.RTCPRRCount}}</td>
  </tr>
  {{end}}
  </tbody>
</table></div>
{{else}}<p class="timing-note">本次采样未观测到媒体轨道。</p>{{end}}
{{if .WebRTCPlayer}}
<h3>浏览器原生 WebRTC 播放 getStats() 指标</h3>
<dl class="metrics">
  <div><dt>ICE / 连接状态</dt><dd>{{.WebRTCPlayer.ICEState}} / {{.WebRTCPlayer.ConnectionState}}</dd></div>
  <div><dt>选中候选类型</dt><dd>{{if .WebRTCPlayer.SelectedCandidateType}}{{.WebRTCPlayer.SelectedCandidateType}} ↔ {{.WebRTCPlayer.SelectedRemoteType}} ({{.WebRTCPlayer.SelectedProtocol}}){{else}}-{{end}}</dd></div>
  <div><dt>当前 RTT</dt><dd>{{f1Ptr .WebRTCPlayer.CurrentRTTMS}}</dd></div>
  <div><dt>解码 / 丢帧</dt><dd>{{.WebRTCPlayer.FramesDecoded}} / {{.WebRTCPlayer.FramesDropped}}</dd></div>
  <div><dt>画面冻结次数 / 耗时</dt><dd>{{.WebRTCPlayer.FreezeCount}} 次 / {{.WebRTCPlayer.TotalFreezesDurationMS}} ms</dd></div>
  <div><dt>抖动缓冲均值 / Jitter</dt><dd>{{f1 .WebRTCPlayer.JitterBufferAvgMS}} ms / {{f2 .WebRTCPlayer.JitterMS}} ms</dd></div>
  <div><dt>浏览器收包 / 丢包</dt><dd>{{.WebRTCPlayer.PacketsReceived}} / {{.WebRTCPlayer.PacketsLost}}</dd></div>
  <div><dt>PLI / NACK / FIR</dt><dd>{{.WebRTCPlayer.PLICount}} / {{.WebRTCPlayer.NACKCount}} / {{.WebRTCPlayer.FIRCount}}</dd></div>
</dl>
{{end}}
{{else}}<p class="timing-note">当前报告未包含 WHEP 会话指标。</p>{{end}}
</article>`))

func renderWHEPHTML(report Report) (template.HTML, error) {
	if report.Manifest.Protocol != ProtocolWHEP && report.WHEP == nil {
		return "", nil
	}
	var webrtcStats *PlayerWebRTCStats
	if report.Player != nil {
		webrtcStats = report.Player.WebRTC
	}
	var out bytes.Buffer
	if err := whepReportPage.Execute(&out, struct {
		Metrics      *WHEPMetrics
		WebRTCPlayer *PlayerWebRTCStats
	}{report.WHEP, webrtcStats}); err != nil {
		return "", err
	}
	return template.HTML(out.String()), nil
}
