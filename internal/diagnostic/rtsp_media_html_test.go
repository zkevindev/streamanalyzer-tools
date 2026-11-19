package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func TestRTSPMediaHTMLShowsClockProvenanceAndContentBoundary(t *testing.T) {
	zero, end := int64(0), int64(3200)
	jpeg := investigationTestJPEG(t)
	image := investigationTestImage("stalls/S001/during.jpg", jpeg)
	image.MediaTimeMS = &zero
	media := InvestigationMedia{Status: "ready", Alignment: "receiver_rtp_estimate", StartMS: &zero, EndMS: &end,
		Assets: []DerivedArtifact{image, {Name: "stalls/S001/clip.mp4", Kind: "clip", Artifact: Artifact{Bytes: 1234}}},
		RTSP: &RTSPMediaEvidence{Version: 1, PlayerRevision: 0, VideoTrackID: 1, VideoCodec: "h265", AudioTrackID: 2, AudioCodec: "pcmu", AudioTransform: "g711_to_pcm",
			AnchorSampleOffsetMS: 7000, AnchorCurrentMS: 3000, PreviewOriginMS: 2000, MediaOriginMS: -66, DurationMS: 8000, PacketCount: 123,
			VideoAccessUnits: 60, AudioAccessUnits: 100, DroppedPackets: 0, IncompleteAccessUnits: 2,
			SourceArtifact: Artifact{Bytes: 10000, SHA256: strings.Repeat("a", 64)}, TimelineArtifact: Artifact{Bytes: 200, SHA256: strings.Repeat("b", 64)},
			PlayerArtifact: Artifact{Bytes: 300, SHA256: strings.Repeat("c", 64)}, RemuxArtifact: Artifact{Bytes: 400, SHA256: strings.Repeat("d", 64)},
			Keyframes: []Event{{Kind: "video", Keyframe: true, DTS: 0, PTS: 66, ArrivalMS: 9000, Track: 1, Artifact: "udp-datagrams.bin", Offset: 123, Size: 80}}},
	}
	report := Report{Manifest: Manifest{Protocol: ProtocolRTSP}, StallInvestigations: []StallInvestigation{{ID: "S001", StallIndex: 1, IntervalKind: "presentation_gap", Media: media, ModelState: "unconfigured"}}}
	var output bytes.Buffer
	if err := RenderHTMLWithAssets(&output, report, nil, "", map[string][]byte{image.Name: jpeg}); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, text := range []string{"RTSP 离线媒体重建", "接收端 RTP 时钟与播放器媒体时间估算", "锚点采样偏移</dt><dd>7000 ms", "锚点播放器媒体时间</dt><dd>3000 ms", "预览解码时钟起点</dt><dd>2000 ms", "重组媒体起点</dt><dd>-66 ms", "播放器记录修订</dt><dd>#0", "未用于重建的 RTP 包</dt><dd>0", "丢弃不完整访问单元</dt><dd>2", "G.711 解码为 PCM", "h265", "pcmu", "重建媒体时间 0", "原始 RTSP 归档", strings.Repeat("d", 64), "离线重建随机接入点", "<td>0</td><td>66</td><td>9000</td>", "udp-datagrams.bin", "data:image/jpeg;base64,", "stalls/S001/clip.mp4", "不能重现或证明本机画面冻结", "未向模型发送截图像素", "不会重新连接源站", "也未按收包到达时间直接定位画面"} {
		if !strings.Contains(body, text) {
			t.Errorf("RTSP media HTML missing %q", text)
		}
	}
	if strings.Contains(body, "精确呈帧对齐</dd>") || strings.Contains(body, "已分析截图画面") {
		t.Fatal("source media was presented as observed browser pixels")
	}
}

func TestRTSPMediaHTMLLegacyAbsentAndAudioOnlyEscapesLabels(t *testing.T) {
	report := Report{Manifest: Manifest{Protocol: ProtocolRTSP}, StallInvestigations: []StallInvestigation{{ID: "S001", Media: InvestigationMedia{Status: "unavailable", Reason: "旧记录未生成媒体"}}}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "RTSP 离线媒体重建") || strings.Contains(output.String(), "锚点播放器媒体时间") {
		t.Fatal("legacy RTSP record acquired invented reconstruction provenance")
	}
	report.StallInvestigations[0].Media.RTSP = &RTSPMediaEvidence{Version: 1, AudioTrackID: 1, AudioCodec: `<script>alert(1)</script>`, AudioTransform: "native_remux"}
	output.Reset()
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, text := range []string{"源视频轨道</dt><dd>未选择", "原生转封装", "未保留可用的离线重建随机接入点记录", "&lt;script&gt;"} {
		if !strings.Contains(body, text) {
			t.Errorf("audio-only HTML missing %q", text)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Fatal("reconstruction labels became active HTML")
	}
}
