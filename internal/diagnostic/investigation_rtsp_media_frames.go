package diagnostic

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// Container duration can be set by audio after the last video picture. Select
// actual decoded video timestamps instead of seeking into that audio-only tail.
func investigationRTSPFramePositions(ctx context.Context, ffprobe, clip string, desired []int64, durationMS int64) ([]int64, error) {
	if len(desired) != 3 || durationMS <= 0 || durationMS > maxPlayerElapsedMS {
		return nil, fmt.Errorf("截图时间范围无效。")
	}
	args := append(investigationMediaInputArgs(clip), "-select_streams", "v:0", "-show_frames", "-show_entries", "format=start_time:frame=best_effort_timestamp_time", "-of", "json", clip)
	output := &investigationMediaLimitedBuffer{limit: 512 << 10}
	if err := investigationMediaRun(ctx, ffprobe, args, output); err != nil {
		return nil, err
	}
	if output.Len() >= output.limit {
		return nil, fmt.Errorf("截图帧时间线超过本机分析上限。")
	}
	var parsed struct {
		Format struct {
			Start string `json:"start_time"`
		} `json:"format"`
		Frames []struct {
			PTS string `json:"best_effort_timestamp_time"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(output.Bytes(), &parsed); err != nil || len(parsed.Frames) > maxTags {
		return nil, fmt.Errorf("无法读取可解码视频帧时间线。")
	}
	origin, err := investigationRTSPFrameMicros(parsed.Format.Start)
	if err != nil || origin < -2_000_000 {
		return nil, fmt.Errorf("派生媒体起始时间无效。")
	}
	var positions []int64
	previous := int64(math.MinInt64)
	for _, frame := range parsed.Frames {
		stamp, err := investigationRTSPFrameMicros(frame.PTS)
		if err != nil || stamp < previous {
			return nil, fmt.Errorf("解码帧时间戳无效或不连续。")
		}
		previous = stamp
		normalized := stamp - origin
		if normalized < 0 || normalized > durationMS*1000 {
			continue
		}
		at := normalized / 1000
		if len(positions) == 0 || at > positions[len(positions)-1] {
			positions = append(positions, at)
		}
	}
	if len(positions) == 0 {
		return nil, fmt.Errorf("可定位范围内没有实际解码画面。")
	}
	middle := positions[0]
	for _, at := range positions {
		if at > desired[1] {
			break
		}
		middle = at
	}
	return []int64{positions[0], middle, positions[len(positions)-1]}, nil
}

// FFprobe's *_time fields are decimal seconds on a microsecond grid. Subtract
// that integer grid before flooring ms, avoiding floating cancellation turning
// an exact 0.4 - 0.4 origin into a seek past the final decoded frame.
func investigationRTSPFrameMicros(value string) (int64, error) {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < -2 || seconds > float64(maxPlayerElapsedMS)/1000+2 {
		return 0, fmt.Errorf("帧时间戳超出范围。")
	}
	return int64(math.Round(seconds * 1_000_000)), nil
}
