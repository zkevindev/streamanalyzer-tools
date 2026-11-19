package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func TestRTSPG711HTMLKeepsLocalConversionSeparateFromNetwork(t *testing.T) {
	zero, delay, startup := int64(0), int64(1024), int64(78)
	report := Report{Manifest: Manifest{Protocol: ProtocolRTSP}, RTSP: &RTSPMetrics{Preview: &RTSPPreviewReport{
		Version: 2, CodecCapabilitiesVersion: 3, State: "ready", AudioTrackID: 1, AudioCodec: "pcma", AudioOutputCodec: "aac", AudioTransform: "g711_to_aac", AudioAUs: 12,
		G711: &RTSPG711PreviewReport{Version: 1, Encoder: "ffmpeg", State: "ready", InputSampleRate: 8000, OutputSampleRate: 48000, Channels: 1, InputPackets: 30, InputSamples: 4800, InsertedSilenceSamples: 160, OutputAUs: 12, EncoderPrimingSamples: &delay, EncoderPaddingSamples: &zero, StartupMS: &startup},
	}}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"pcma", "aac", "本机 G.711 → AAC 转码", "原始 G.711 RTP 不变", "不能解释为网络收流耗时", "按 RTP 缺口补静音样本</dt><dd>160", "编码预延迟样本</dt><dd>1024", "编码填充样本</dt><dd>0", "首个编码输出耗时</dt><dd>78 ms", "工具检查耗时</dt><dd>未观测", "不是解码或呈帧数"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("missing conversion evidence %q", expected)
		}
	}
	// A pipeline reason is displayed even when the raw/video capture succeeded.
	report.RTSP.Preview.G711.State = "unavailable"
	report.RTSP.Preview.G711.ReasonCode = "ffmpeg_unavailable"
	output.Reset()
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "未找到 FFmpeg") || !strings.Contains(output.String(), "本机转码不可用") {
		t.Fatal("missing local tool failure reason")
	}
}
