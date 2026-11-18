package diagnostic

import (
	"bytes"
	"strings"
	"testing"
)

func TestPlayerVideoHTMLPreservesZeroUnknownAndObservationScope(t *testing.T) {
	zero := int64(0)
	zeroRatio := 0.0
	player := &PlayerReport{State: "complete", FirstPlayingMS: &zero, Video: &PlayerVideoReport{QualitySource: "playback_quality", TotalFrames: &zero, DroppedFrames: &zero, PresentedFrames: &zero, DroppedRatio: &zeroRatio, MaxFrameGapMS: &zero, CounterResets: 0, SampleCount: 2}}
	report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, Player: player}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, text := range []string{"视频呈帧与丢帧", "VideoPlaybackQuality", "视频帧增量</dt><dd>0</dd>", "丢帧增量</dt><dd>0</dd>", "呈帧增量</dt><dd>0</dd>", "丢帧比例</dt><dd>0.00%</dd>", "最大呈帧回调间隔</dt><dd>0 ms</dd>", "连续前台播放样本", "低帧率内容和页面调度"} {
		if !strings.Contains(body, text) {
			t.Errorf("missing measured zero or methodology %q", text)
		}
	}
	player.Video = &PlayerVideoReport{QualitySource: "unavailable", SampleCount: 1}
	output.Reset()
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body = output.String()
	for _, text := range []string{"未提供帧质量计数", "视频帧增量</dt><dd>未观测</dd>", "丢帧比例</dt><dd>未观测</dd>", "最大呈帧回调间隔</dt><dd>未观测</dd>"} {
		if !strings.Contains(body, text) {
			t.Errorf("unknown video metric rendered as measured zero: %q", text)
		}
	}
	player.Video = nil
	output.Reset()
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "该记录未采集视频呈帧与丢帧数据。") || strings.Contains(output.String(), "丢帧比例</dt>") {
		t.Fatal("legacy report invented frame metrics")
	}
	report.Player = nil
	output.Reset()
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "视频呈帧与丢帧") {
		t.Fatal("nonplayer report exposed stale video metrics")
	}
}

func TestPlayerVideoHTMLPositiveCountersAndCaseSamples(t *testing.T) {
	total, dropped, presented, gap, age := int64(200), int64(12), int64(188), int64(1500), int64(1100)
	ratio := 0.06
	report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}, Player: &PlayerReport{Video: &PlayerVideoReport{QualitySource: "mixed", TotalFrames: &total, DroppedFrames: &dropped, PresentedFrames: &presented, DroppedRatio: &ratio, MaxFrameGapMS: &gap, CounterResets: 1, SampleCount: 5}},
		StallInvestigations: []StallInvestigation{{ID: "S001", StallIndex: 1, Status: "partial", Correlation: StallCorrelation{BufferSamples: []PlayerBufferSample{{OffsetMS: 2000, Paused: true, Hidden: true, Video: &PlayerVideoSample{QualitySource: "webkit", TotalFrames: &total, DroppedFrames: &dropped, PresentedFrames: &presented, FrameAgeMS: &age}}}}}}}
	var output bytes.Buffer
	if err := RenderHTML(&output, report, nil, ""); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, text := range []string{"混合计数来源", "视频帧增量</dt><dd>200</dd>", "丢帧增量</dt><dd>12</dd>", "丢帧比例</dt><dd>6.00%</dd>", "最大呈帧回调间隔</dt><dd>1500 ms</dd>", "计数重置</dt><dd>1</dd>", "窗口视频样本 · 1 条", "原始累计计数", "暂停 / 后台", "1100 ms"} {
		if !strings.Contains(body, text) {
			t.Errorf("missing video summary or case sample %q", text)
		}
	}
	if got := playerVideoQualitySourceLabel("webkit"); got != "WebKit 视频计数" {
		t.Fatal("WebKit counter source lost")
	}
}
