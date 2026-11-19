package diagnostic

import (
	"cmp"
	"slices"
	"strings"
)

func rtspCorrelationEvents(dir string, manifest Manifest) ([]Event, []string, error) {
	events := []Event{}
	err := walkRTSPMediaEvents(dir, manifest, func(event Event) {
		if len(events) < maxTags && (event.Kind == "video_rtp" || event.Kind == "audio_rtp") {
			events = append(events, event)
		}
	})
	return events, []string{"RTSP 收流关联按各轨道 RTP 包到达计算；包间隔不是编码帧间隔，未据此确认关键帧或源端音视频同步。", "预览使用同次采样的本地媒体重组；缓冲卡顿也可能来自重组、浏览器解码或调度，应结合播放器证据验证。"}, err
}

// Preserve track identity: a second video track must not fill the first
// track's receive gap. Events remain RTP evidence without invented PTS/DTS.
func rtspReceptionGaps(events []Event, elapsed, start, end int64) []ReceptionGap {
	tracks := map[uint8][]Event{}
	for _, event := range events {
		if event.Kind != "video_rtp" && event.Kind != "audio_rtp" {
			continue
		}
		event.Kind = strings.TrimSuffix(event.Kind, "_rtp")
		tracks[event.Track] = append(tracks[event.Track], event)
	}
	ids := make([]uint8, 0, len(tracks))
	for id := range tracks {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	result := []ReceptionGap{}
	for _, id := range ids {
		for _, gap := range stallReceiveGaps(tracks[id], elapsed, start, end) {
			if gap.Before != nil {
				gap.Before.Kind += "_rtp"
			}
			if gap.After != nil {
				gap.After.Kind += "_rtp"
			}
			result = append(result, gap)
		}
	}
	slices.SortStableFunc(result, func(a, b ReceptionGap) int {
		if n := cmp.Compare(b.DurationMS, a.DurationMS); n != 0 {
			return n
		}
		return cmp.Compare(a.StartMS, b.StartMS)
	})
	result = result[:min(len(result), maxStallGaps)]
	slices.SortStableFunc(result, func(a, b ReceptionGap) int { return cmp.Compare(a.StartMS, b.StartMS) })
	return result
}
