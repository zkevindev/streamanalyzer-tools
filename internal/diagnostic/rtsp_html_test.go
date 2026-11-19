package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func TestRTSPHTMLControlDeclaredAndReceivedEvidence(t *testing.T) {
	zero, ten := int64(0), int64(10)
	sequence := uint16(0)
	report := Report{Manifest: Manifest{Protocol: ProtocolRTSPS, Observation: Observation{
		Origin: "rtsps://camera-user:private-password@camera.example:8554/private-profile?token=private-token", FirstPayloadMS: &zero,
		Stages: []CaptureStage{{Name: "dns"}, {Name: "connect"}, {Name: "tls"}, {Name: "rtsp_describe"}, {Name: "rtsp_setup"}, {Name: "rtsp_play"}},
	}}, RTSP: &RTSPMetrics{Boundary: "decrypted_rtsp_application", Transport: "tcp_interleaved", Secure: true, SDPAvailable: true, Connections: 1,
		RTPPackets: 3, RTCPPackets: 1, RTPBytes: 130, FirstRTPMS: &zero, FirstVideoRTPMS: &zero, FirstAudioRTPMS: &ten, AnalysisLimited: true, RequestsTruncated: 3,
		Requests: []RTSPRequestTiming{
			{Number: 1, ConnectionID: 1, Method: "DESCRIBE", CSeq: 1, StatusCode: 401, Complete: true},
			{Number: 2, ConnectionID: 1, Method: "DESCRIBE", CSeq: 2, StatusCode: 200, Complete: true},
			{Number: 3, ConnectionID: 1, Method: "SETUP", CSeq: 3, StatusCode: 200, Complete: true},
			{Number: 4, ConnectionID: 1, Method: "PLAY", CSeq: 4, StatusCode: 200, Complete: true},
			{Number: 5, ConnectionID: 1, Method: "GET_PARAMETER", CSeq: 5, StartMS: 20, EndMS: 1000, FailureCode: "timeout"},
		}, Tracks: []RTSPTrackMetrics{
			{ID: 1, MediaID: 1, Kind: "video", Codec: "h264", PayloadType: 96, ClockRate: 90000, Received: true, RTPPackets: 2, RTPBytes: 100, FirstRTPMS: &zero, LastRTPMS: &zero, SequenceGaps: 1, MissingSequencePackets: 2, OutOfOrderPackets: 1, SSRCChanges: 1, SequenceResets: 1, TimestampRegressions: 1, AnalysisLimited: true},
			{ID: 2, MediaID: 2, Kind: "audio", Codec: "pcmu", PayloadType: 0, ClockRate: 8000, Channels: 1},
			{ID: 3, MediaID: 3, Kind: "audio", Codec: "aac", PayloadType: 97, ClockRate: 48000, Channels: 2, Received: true, RTPPackets: 1, RTPBytes: 30, FirstRTPMS: &ten, LastRTPMS: &ten},
		}}, Findings: []Finding{{ID: "R001", Message: "核对 RTP 序号", Evidence: []Event{{Kind: "rtp_packet", Artifact: "server-wire.bin", Track: 1, Codec: "h264", RTPSequence: &sequence, Offset: 100, Size: 50}}}}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, &Explanation{Hypotheses: []Hypothesis{{EvidenceIDs: []string{"R001"}}}}, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, text := range []string{"RTSPS 排障报告", "rtsps://camera.example:8554", "RTSP 会话与 RTP 接收", "TCP 交织 RTP/RTCP", "TLS 解密后的 RTSP/RTP", "RTSP DESCRIBE", "RTSP SETUP", "RTSP PLAY", "SDP 声明轨道", "实际 RTP 接收", "未收到对应 RTP", "不代表已收到或解码媒体", "不是解码首帧", "不能直接等同于 TCP 网络丢包", "序号跳变/重置候选", "B 帧或乱序", "另有 3 条控制请求未写入指标", "已达到分析上限", "stream.rtsp.zip", "session.sdp", "server-wire.bin", "超时 [timeout]", `href="#R001"`} {
		if !strings.Contains(body, text) {
			t.Errorf("native report missing %q", text)
		}
	}
	for _, text := range []string{"camera-user", "private-password", "private-profile", "private-token", "首个视频帧到达", "首个音频帧到达", "视频帧</dt>", "最大关键帧间隔", "RTMP 播放协商"} {
		if strings.Contains(body, text) {
			t.Errorf("native report exposed source detail or unsupported media claim %q", text)
		}
	}
	for _, row := range []string{
		`<dt>首个有效 RTP 包</dt><dd>0 ms</dd>`,
		`<dt>首个视频 RTP 到达</dt><dd>0 ms</dd>`,
		`<td>pcmu</td><td>0</td><td>8000 Hz</td><td>1</td>`,
		`<td>已收到 RTP</td><td>2</td><td>100</td><td>0 ms</td><td>0 ms</td><td>0 ms</td>`,
		`<td>未收到对应 RTP</td><td>0</td><td>0</td><td>未观测</td><td>未观测</td><td>未观测</td>`,
		`<td>401</td><td>0</td><td>0</td><td>0 ms</td>`,
		`<td>未收到响应</td><td>20</td><td>1000</td><td>980 ms</td><td>未完整结束</td>`,
		`<td>RTP 包</td><td>#1</td><td>h264</td><td>0</td><td>0</td><td><code>server-wire.bin</code></td><td>100</td><td>50</td>`,
	} {
		if !strings.Contains(body, row) {
			t.Errorf("native numeric/provenance evidence missing: %s", row)
		}
	}
	if strings.Count(body, `id="R001"`) != 1 {
		t.Fatal("native finding anchor is missing or duplicated")
	}
}

func TestRTSPHTMLNoRTPAndMissingNativeMetrics(t *testing.T) {
	zero := int64(0)
	for _, tc := range []struct {
		name    string
		metrics *RTSPMetrics
		want    string
	}{
		{name: "no SDP or RTP", metrics: &RTSPMetrics{Transport: "tcp_interleaved"}, want: "未取得可解析 SDP"},
		{name: "missing report fields", want: "当前报告未包含 RTSP 会话与 RTP 接收指标"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := Report{Manifest: Manifest{Protocol: ProtocolRTSP, Observation: Observation{Origin: "rtsp://camera.example", FirstPayloadMS: &zero, Stages: []CaptureStage{{Name: "connect"}}}}, RTSP: tc.metrics}
			var output bytes.Buffer
			if err := RenderHTML(&output, report, nil, ""); err != nil {
				t.Fatal(err)
			}
			body := output.String()
			if !strings.Contains(body, tc.want) || !strings.Contains(body, "当前版本不提供浏览器 RTSP 预览") || !strings.Contains(body, "首个有效 RTP 包</dt><dd>未观测</dd>") {
				t.Fatal("missing native fields or raw RTSP bytes became a measured RTP arrival")
			}
			if tc.metrics != nil && !strings.Contains(body, "TLS 阶段</dt><dd>不适用（RTSP）</dd>") {
				t.Fatal("plain RTSP TLS absence was presented as a measured zero")
			}
		})
	}
}

func TestRTSPHTMLFragmentEscapesText(t *testing.T) {
	report := Report{Manifest: Manifest{Protocol: ProtocolRTSP}, RTSP: &RTSPMetrics{Tracks: []RTSPTrackMetrics{{ID: 1, Codec: `<img src=x onerror="alert(1)">`}}, Requests: []RTSPRequestTiming{{Number: 1, Method: `<script>alert(1)</script>`}}}}
	fragment, err := renderRTSPHTML(report)
	if err != nil {
		t.Fatal(err)
	}
	body := string(fragment)
	if !strings.Contains(body, "&lt;img") || !strings.Contains(body, "&lt;script") || strings.Contains(body, `<img src=x`) || strings.Contains(body, `<script>`) {
		t.Fatal("native HTML fragment trusted unescaped report text")
	}
	if report.RTSP.Tracks[0].Codec != `<img src=x onerror="alert(1)">` {
		t.Fatal("HTML formatting mutated its input report")
	}
}
