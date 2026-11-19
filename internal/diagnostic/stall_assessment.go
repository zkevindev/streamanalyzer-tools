package diagnostic

import (
	"fmt"
	"slices"
)

const stallAssessmentVersion = "stall-assessment/5"

type StallDirection struct {
	Code        string   `json:"code"`
	Status      string   `json:"status"` // observed, candidate, insufficient
	Title       string   `json:"title"`
	Message     string   `json:"message"`
	EvidenceIDs []string `json:"evidence_ids"`
	NextChecks  []string `json:"next_checks"`
}

type StallAssessment struct {
	Version     string           `json:"version"`
	Conclusion  string           `json:"conclusion"`
	Directions  []StallDirection `json:"directions"`
	Findings    []Finding        `json:"findings"`
	Limitations []string         `json:"limitations"`
}

// Preserve the original rule set so archived assessments remain reproducible.
func assessStallV1(item StallInvestigation) *StallAssessment {
	result := &StallAssessment{Version: "stall-assessment/1", Directions: []StallDirection{}, Findings: []Finding{},
		Limitations: []string{"时间相关性用于选择验证方向，根因仍需对照源端、链路或客户现场记录。", "缓冲和帧质量是离散观测；缺少字段表示未观测，不能按零处理。"}}
	findIDs := func(code string) []string {
		var ids []string
		for _, f := range item.Correlation.Findings {
			if f.Code == code {
				ids = append(ids, f.ID)
			}
		}
		return ids
	}
	selected := findIDs("stall_selected")
	bufferIDs := findIDs("stall_buffer_window")
	add := func(code, severity, message string, start, end int64) string {
		id := fmt.Sprintf("%s-A%03d", item.ID, len(result.Findings)+1)
		result.Findings = append(result.Findings, Finding{ID: id, Code: code, Severity: severity, Message: message, StartMS: max(0, start), EndMS: max(0, end), Occurrences: 1})
		return id
	}
	direction := func(code, status, title, message string, ids []string, checks ...string) {
		result.Directions = append(result.Directions, StallDirection{Code: code, Status: status, Title: title, Message: message, EvidenceIDs: slices.Clone(ids), NextChecks: checks})
	}
	var samples []PlayerBufferSample
	var positions []int
	excluded := false
	for index, sample := range item.Correlation.BufferSamples {
		if sample.OffsetMS >= item.Stall.StartMS && sample.OffsetMS <= item.Stall.EndMS && (sample.Paused || sample.Seeking || sample.Hidden) {
			excluded = true
		}
		if sample.OffsetMS >= item.Stall.StartMS && sample.OffsetMS <= item.Stall.EndMS && !sample.Paused && !sample.Seeking && !sample.Hidden {
			samples = append(samples, sample)
			positions = append(positions, index)
		}
	}
	minBuffer := int64(-1)
	maxAge := int64(-1)
	qualityPairs := 0
	var total, dropped int64
	for index, sample := range samples {
		if minBuffer < 0 || sample.BufferMS < minBuffer {
			minBuffer = sample.BufferMS
		}
		if sample.Video != nil && sample.Video.FrameAgeMS != nil {
			// A callback age may include a pause or hidden interval before this
			// stall. Only the age accumulated inside the selected interval can
			// support a playback-delay direction.
			maxAge = max(maxAge, min(*sample.Video.FrameAgeMS, sample.OffsetMS-item.Stall.StartMS))
		}
		if index == 0 {
			continue
		}
		previous := samples[index-1]
		if positions[index] != positions[index-1]+1 || sample.OffsetMS-previous.OffsetMS > 1500 || sample.OffsetMS <= previous.OffsetMS || previous.Video == nil || sample.Video == nil {
			continue
		}
		v, p := sample.Video, previous.Video
		if v.QualitySource != p.QualitySource || v.TotalFrames == nil || p.TotalFrames == nil || v.DroppedFrames == nil || p.DroppedFrames == nil {
			continue
		}
		t, d := *v.TotalFrames-*p.TotalFrames, *v.DroppedFrames-*p.DroppedFrames
		if t < 0 || d < 0 || d > t {
			continue
		}
		qualityPairs++
		total += t
		dropped += d
	}
	starved := minBuffer >= 0 && minBuffer <= 250
	if starved {
		ids := append(slices.Clone(selected), bufferIDs...)
		direction("buffer_underflow", "observed", "卡顿附近缓冲接近耗尽", fmt.Sprintf("卡顿附近的前台样本中可播放缓冲最低 %d ms。", minBuffer), ids, "对照卡顿前的接收速率和媒体码率，确认供给是否能持续覆盖播放。")
	}
	var gapIDs []string
	for _, gap := range item.Correlation.Gaps {
		if min(gap.EndMS, item.Stall.EndMS)-max(gap.StartMS, item.Stall.StartMS) < 500 {
			continue
		}
		// A long stall can include unrelated receive gaps and later underflow.
		// Require a low-buffer observation within one normal sample interval
		// of this gap before describing them as temporally related.
		if !slices.ContainsFunc(samples, func(sample PlayerBufferSample) bool {
			return sample.BufferMS <= 250 && sample.OffsetMS >= gap.StartMS-500 && sample.OffsetMS <= gap.EndMS+500
		}) {
			continue
		}
		for _, finding := range item.Correlation.Findings {
			if finding.Code == "stall_"+gap.Kind+"_receive_gap" && finding.StartMS == gap.StartMS && finding.EndMS == gap.EndMS {
				gapIDs = append(gapIDs, finding.ID)
			}
		}
	}
	slices.Sort(gapIDs)
	gapIDs = slices.Compact(gapIDs)
	if starved && len(gapIDs) > 0 && !excluded && item.Correlation.Source == "same_capture" {
		ids := append(slices.Clone(gapIDs), bufferIDs...)
		direction("source_or_transport", "candidate", "优先核对媒体供给与传输", "同一次采样的媒体接收间隔附近观测到缓冲接近耗尽（样本容差 500 ms）；当前接收点无法进一步区分源端停发与链路中断。", ids,
			"在相同时间段对比源端输出和另一个接收点。", "检查服务端断流、发送队列及网络重传记录，并复测另一节点或线路。")
		result.Conclusion = "接收间隔附近观测到缓冲接近耗尽，优先核对源端输出和传输供给。"
	}
	continuous := true
	for i := 1; i < len(samples); i++ {
		if samples[i].OffsetMS-samples[i-1].OffsetMS > 1500 {
			continuous = false
		}
	}
	coverage := !excluded && continuous && len(samples) >= 2 && samples[len(samples)-1].OffsetMS-samples[0].OffsetMS >= 500 &&
		samples[0].OffsetMS <= item.Stall.StartMS+500 && samples[len(samples)-1].OffsetMS >= item.Stall.EndMS-500
	if coverage && minBuffer >= 1000 && maxAge >= 1000 {
		id := add("stall_buffered_frame_delay", "warn", fmt.Sprintf("卡顿附近样本缓冲均至少 %d ms，卡顿区间内至少 %d ms 未观测到新的视频帧回调。", minBuffer, maxAge), item.Stall.StartMS, item.Stall.EndMS)
		direction("local_playback_or_schedule", "candidate", "优先核对本机播放与调度", "已观测到可播放缓冲，视频帧回调仍出现较长间隔；可沿解码、呈现和主线程调度方向补证据。", append([]string{id}, bufferIDs...),
			"核对源视频标称帧率，排除正常低帧率内容。", "检查播放器媒体错误、硬件解码能力及主线程长任务。", "在同设备另一播放器或关闭硬件加速后对照复测。")
		result.Conclusion = "缓冲充足且呈帧回调延后，优先核对本机播放和调度。"
	}
	if qualityPairs > 0 && dropped > 0 {
		id := add("stall_observed_frame_drops", "warn", fmt.Sprintf("卡顿附近连续前台样本观测到视频帧增量 %d、丢帧增量 %d。", total, dropped), item.Stall.StartMS, item.Stall.EndMS)
		direction("frame_drops", "observed", "卡顿附近观测到丢帧", "丢帧计数可用于核对播放性能，单凭该计数不能确定网络或解码根因。", []string{id}, "比较较低分辨率或码率的播放结果，并结合解码器和设备负载记录验证。")
		if result.Conclusion == "" {
			result.Conclusion = "卡顿附近观测到丢帧，需结合设备与播放器记录验证。"
		}
	}
	if maxAge >= 0 {
		result.Limitations = append(result.Limitations, "视频帧回调受内容帧率、浏览器呈现和主线程调度影响，不能单独证明解码失败。")
	}
	if item.Correlation.Source == "independent_pull" {
		result.Limitations = append(result.Limitations, "HLS 采样与播放器使用独立连接；采样连接的间隔不能证明播放连接同样中断。")
		if starved && result.Conclusion == "" {
			result.Conclusion = "播放器缓冲接近耗尽，需要补采实际播放连接的分片请求。"
		}
	}
	if len(item.Correlation.Keyframes) == 0 {
		result.Limitations = append(result.Limitations, "本次缺少可确认的关键帧记录，不能据此断言源流没有关键帧。")
	}
	if result.Conclusion == "" {
		result.Conclusion = "现有证据尚不能将卡顿缩小到单一环节。"
		direction("insufficient_evidence", "insufficient", "继续补充现场证据", "当前窗口缺少足够的连续缓冲、呈帧或同连接收流证据。", selected, "在客户实际设备和网络重新采样。", "结合源端日志、同连接请求记录及播放器解码/呈现日志对照验证。")
	}
	return result
}
