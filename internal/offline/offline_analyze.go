package offline

import (
	"fmt"
	"math"
	"sort"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
)

const (
	maxStoredFrames  = 2000
	maxGOPPoints     = 480
	maxSeriesPoints  = 360
	maxBitratePoints = 240
	largeGapMS       = 1000
)

type timedSample struct {
	pid   int
	media string
	dts   int64
	pts   int64
	cts   int64
	size  int
	key   bool
	b     bool
}

func attachMediaProfile(flow *models.OfflineFlowResult, frames []models.OfflineFrameDetail, seed models.OfflineMediaProfile) {
	samples := make([]timedSample, 0, len(frames))
	for _, frame := range frames {
		samples = append(samples, timedSample{
			media: frame.MediaType,
			dts:   frame.DTS,
			pts:   frame.PTS,
			cts:   frame.CTS,
			size:  frame.FrameLen,
			key:   isKeyframe(frame.FrameType),
			b:     isBFrame(frame.FrameType),
		})
	}
	attachSampleProfile(flow, samples, seed)
	flow.FrameDetails = frames
	if len(frames) > maxStoredFrames {
		flow.FrameDetails = append([]models.OfflineFrameDetail(nil), frames[:maxStoredFrames]...)
		if flow.Profile != nil {
			flow.Profile.FramesTruncated = true
		}
	}
}

func attachSampleProfile(flow *models.OfflineFlowResult, samples []timedSample, seed models.OfflineMediaProfile) {
	stats := analyzeSamples(samples)
	fillProfile(stats, seed)
	stats.Warnings = mergeWarnings(seed.Warnings, stats.Warnings)
	if (seed.VideoCodec == "h264" || seed.VideoCodec == "h265" || stats.VideoCodec == "h264" || stats.VideoCodec == "h265") &&
		stats.VideoFrames > 0 && stats.Keyframes == 0 {
		stats.Warnings = mergeWarnings(stats.Warnings, []string{"未识别到视频关键帧"})
	}
	if stats.VideoCodec == "" {
		stats.VideoCodec = flow.VideoCodec
	}
	if stats.AudioCodec == "" {
		stats.AudioCodec = flow.AudioCodec
	}
	if flow.VideoCodec == "" || flow.VideoCodec == "unknown" {
		if stats.VideoCodec != "" && stats.VideoCodec != "unknown" {
			flow.VideoCodec = stats.VideoCodec
		}
	}
	if flow.AudioCodec == "" || flow.AudioCodec == "unknown" {
		if stats.AudioCodec != "" && stats.AudioCodec != "unknown" {
			flow.AudioCodec = stats.AudioCodec
		}
	}
	flow.Profile = stats
}

func analyzeSamples(samples []timedSample) *models.OfflineMediaProfile {
	stats := &models.OfflineMediaProfile{FrameTotal: len(samples)}
	videoPID := dominantPID(samples, "video")
	audioPID := dominantPID(samples, "audio")

	var video []timedSample
	var audio []timedSample
	for _, sample := range samples {
		switch sample.media {
		case "video":
			if sample.pid == videoPID {
				video = append(video, sample)
				stats.VideoFrames++
				stats.VideoBytes += int64(sample.size)
				if sample.key {
					stats.Keyframes++
				}
				if sample.b {
					stats.BFrames++
				}
				if sample.cts != 0 || (sample.pts != 0 && sample.pts != sample.dts) {
					stats.HasBFrames = true
				}
			}
		case "audio":
			if sample.pid == audioPID {
				audio = append(audio, sample)
				stats.AudioFrames++
				stats.AudioBytes += int64(sample.size)
			}
		}
	}
	if stats.BFrames > 0 {
		stats.HasBFrames = true
	}

	timeline := video
	if len(timeline) == 0 {
		timeline = audio
	}
	var originDTS int64
	if len(timeline) > 0 {
		originDTS = timeline[0].dts
	}
	if len(video) > 0 && len(audio) > 0 {
		if audio[0].dts < originDTS {
			originDTS = audio[0].dts
		}
	}
	if len(timeline) >= 2 {
		stats.DurationMS = timeline[len(timeline)-1].dts - timeline[0].dts
		if stats.DurationMS < 0 {
			stats.DurationMS = 0
		}
	}
	if stats.VideoFrames >= 2 && stats.DurationMS > 0 {
		stats.FrameRate = round2(float64(stats.VideoFrames-1) * 1000 / float64(stats.DurationMS))
	}
	if stats.DurationMS > 0 {
		if stats.VideoBytes > 0 {
			stats.VideoBitrateKbps = round1(float64(stats.VideoBytes) * 8 / float64(stats.DurationMS))
		}
		if stats.AudioBytes > 0 {
			stats.AudioBitrateKbps = round1(float64(stats.AudioBytes) * 8 / float64(stats.DurationMS))
		}
	}

	var intervals []int64
	var videoDeltas []int64
	var deltaSum int64
	var deltaCount int64
	var lastKey int64
	var hasKey bool
	var prev int64
	var hasPrev bool
	var prevPTS int64
	var hasPrevPTS bool

	for _, sample := range video {
		if hasPrev {
			delta := sample.dts - prev
			videoDeltas = append(videoDeltas, delta)
			if delta < 0 {
				stats.DTSRegressions++
			} else {
				if deltaCount == 0 || delta < stats.VideoDeltaMinMS {
					stats.VideoDeltaMinMS = delta
				}
				if delta > stats.VideoDeltaMaxMS {
					stats.VideoDeltaMaxMS = delta
				}
				deltaSum += delta
				deltaCount++
				if delta > largeGapMS {
					stats.LargeGaps++
					if delta > stats.MaxGapMS {
						stats.MaxGapMS = delta
					}
				}
			}
		}
		if hasPrevPTS && sample.pts != 0 && prevPTS != 0 && sample.pts < prevPTS {
			stats.HasBFrames = true
		}
		if sample.pts != 0 {
			prevPTS = sample.pts
			hasPrevPTS = true
		}
		prev = sample.dts
		hasPrev = true
		if !sample.key {
			continue
		}
		if hasKey {
			gap := sample.dts - lastKey
			if gap >= 0 {
				intervals = append(intervals, gap)
			}
		}
		lastKey = sample.dts
		hasKey = true
	}
	if deltaCount > 0 {
		stats.VideoDeltaAvgMS = deltaSum / deltaCount
	}
	if len(videoDeltas) > 0 {
		stats.VideoDeltaSeries = downsample(videoDeltas, maxSeriesPoints)
	}
	if len(video) > 0 {
		stats.FrameSizeSeries = downsampleFrameSizes(video, maxSeriesPoints)
	}

	// Audio gap check
	var prevAudio int64
	var hasPrevAudio bool
	for _, sample := range audio {
		if hasPrevAudio {
			delta := sample.dts - prevAudio
			if delta > largeGapMS {
				stats.AudioLargeGaps++
			}
		}
		prevAudio = sample.dts
		hasPrevAudio = true
	}

	// GOP intervals
	if len(intervals) > 0 {
		var sum int64
		stats.GOPMinMS = intervals[0]
		for _, gap := range intervals {
			sum += gap
			if gap < stats.GOPMinMS {
				stats.GOPMinMS = gap
			}
			if gap > stats.GOPMaxMS {
				stats.GOPMaxMS = gap
			}
		}
		stats.GOPAvgMS = sum / int64(len(intervals))
		stats.GOPIntervalsMS = downsample(intervals, maxGOPPoints)
	}

	// Bitrate timeline (1-second or scaled buckets)
	stats.BitrateTimeline = computeBitrateTimeline(video, audio, originDTS, stats.DurationMS)

	// A/V sync offset & timeline
	if len(video) > 0 && len(audio) > 0 {
		stats.HasAVOffset = true
		stats.AVStartOffsetMS = audio[0].dts - video[0].dts
		stats.AVSyncTimeline, stats.AVDriftMaxMS = computeAVSyncTimeline(video, audio, originDTS)
	}

	if stats.LargeGaps > 0 {
		stats.Warnings = append(stats.Warnings, fmt.Sprintf("视频时间戳有 %d 处间隔超过 1 秒，最大 %d ms", stats.LargeGaps, stats.MaxGapMS))
	}
	if stats.AudioLargeGaps > 0 {
		stats.Warnings = append(stats.Warnings, fmt.Sprintf("音频时间戳有 %d 处间隔超过 1 秒（疑似断音）", stats.AudioLargeGaps))
	}
	if stats.DTSRegressions > 0 {
		stats.Warnings = append(stats.Warnings, fmt.Sprintf("视频 DTS 回退 %d 次", stats.DTSRegressions))
	}
	if stats.HasAVOffset {
		absStart := abs64(stats.AVStartOffsetMS)
		absDrift := abs64(stats.AVDriftMaxMS)
		if absStart > 500 {
			stats.Warnings = append(stats.Warnings, fmt.Sprintf("音视频首帧时间戳偏差较大（音频-视频 = %d ms）", stats.AVStartOffsetMS))
		} else if absDrift > 500 {
			stats.Warnings = append(stats.Warnings, fmt.Sprintf("音视频时间线存在明显漂移（最大偏差 %d ms）", stats.AVDriftMaxMS))
		}
	}
	return stats
}

func computeBitrateTimeline(video, audio []timedSample, originDTS, durationMS int64) []models.OfflineBitratePoint {
	if len(video) == 0 && len(audio) == 0 {
		return nil
	}
	bucketSec := int64(1)
	totalSec := durationMS / 1000
	if totalSec > maxBitratePoints {
		bucketSec = (totalSec / maxBitratePoints) + 1
	}
	bucketMS := bucketSec * 1000

	type acc struct {
		vBytes int64
		aBytes int64
	}
	buckets := make(map[int]*acc)
	add := func(list []timedSample, isVideo bool) {
		for _, s := range list {
			rel := s.dts - originDTS
			if rel < 0 {
				rel = 0
			}
			sec := int((rel / bucketMS) * bucketSec)
			b := buckets[sec]
			if b == nil {
				b = &acc{}
				buckets[sec] = b
			}
			if isVideo {
				b.vBytes += int64(s.size)
			} else {
				b.aBytes += int64(s.size)
			}
		}
	}
	add(video, true)
	add(audio, false)
	if len(buckets) == 0 {
		return nil
	}
	secs := make([]int, 0, len(buckets))
	for sec := range buckets {
		secs = append(secs, sec)
	}
	sort.Ints(secs)
	if len(secs) > maxBitratePoints {
		secs = secs[:maxBitratePoints]
	}
	out := make([]models.OfflineBitratePoint, 0, len(secs))
	denom := float64(bucketSec) * 1000.0
	for _, sec := range secs {
		b := buckets[sec]
		out = append(out, models.OfflineBitratePoint{
			Sec:       sec,
			VideoKbps: round1(float64(b.vBytes) * 8.0 / denom),
			AudioKbps: round1(float64(b.aBytes) * 8.0 / denom),
		})
	}
	return out
}

func computeAVSyncTimeline(video, audio []timedSample, originDTS int64) ([]models.OfflineAVSyncPoint, int64) {
	if len(video) == 0 || len(audio) == 0 {
		return nil, 0
	}
	vBuckets := make(map[int]int64)
	aBuckets := make(map[int]int64)
	for _, s := range video {
		rel := s.dts - originDTS
		if rel < 0 {
			rel = 0
		}
		sec := int(rel / 1000)
		if _, ok := vBuckets[sec]; !ok {
			vBuckets[sec] = s.dts
		}
	}
	for _, s := range audio {
		rel := s.dts - originDTS
		if rel < 0 {
			rel = 0
		}
		sec := int(rel / 1000)
		if _, ok := aBuckets[sec]; !ok {
			aBuckets[sec] = s.dts
		}
	}
	secs := make([]int, 0, len(vBuckets))
	for sec := range vBuckets {
		if _, ok := aBuckets[sec]; ok {
			secs = append(secs, sec)
		}
	}
	if len(secs) == 0 {
		return nil, 0
	}
	sort.Ints(secs)

	var maxDrift int64
	all := make([]models.OfflineAVSyncPoint, 0, len(secs))
	for _, sec := range secs {
		diff := aBuckets[sec] - vBuckets[sec]
		if abs64(diff) > abs64(maxDrift) {
			maxDrift = diff
		}
		all = append(all, models.OfflineAVSyncPoint{Sec: sec, DiffMS: diff})
	}
	if len(all) <= maxBitratePoints {
		return all, maxDrift
	}
	out := make([]models.OfflineAVSyncPoint, maxBitratePoints)
	last := len(all) - 1
	for i := 0; i < maxBitratePoints; i++ {
		idx := int(math.Round(float64(i) * float64(last) / float64(maxBitratePoints-1)))
		out[i] = all[idx]
	}
	return out, maxDrift
}

func downsampleFrameSizes(video []timedSample, limit int) []models.OfflineFrameSizePoint {
	if len(video) == 0 {
		return nil
	}
	if limit <= 0 || len(video) <= limit {
		out := make([]models.OfflineFrameSizePoint, len(video))
		for i, s := range video {
			out[i] = models.OfflineFrameSizePoint{Index: i + 1, DTS: s.dts, Bytes: s.size, Key: s.key}
		}
		return out
	}
	// Downsample by bucket keeping keyframes (or largest frame) in each bucket so I-frame spikes stay visible.
	out := make([]models.OfflineFrameSizePoint, 0, limit)
	step := float64(len(video)) / float64(limit)
	for i := 0; i < limit; i++ {
		start := int(math.Floor(float64(i) * step))
		end := int(math.Floor(float64(i+1) * step))
		if end <= start {
			end = start + 1
		}
		if end > len(video) {
			end = len(video)
		}
		pick := start
		for j := start; j < end; j++ {
			if video[j].key && !video[pick].key {
				pick = j
				continue
			}
			if video[j].key == video[pick].key && video[j].size > video[pick].size {
				pick = j
			}
		}
		s := video[pick]
		out = append(out, models.OfflineFrameSizePoint{Index: pick + 1, DTS: s.dts, Bytes: s.size, Key: s.key})
	}
	return out
}

func dominantPID(samples []timedSample, media string) int {
	counts := map[int]int{}
	var order []int
	for _, sample := range samples {
		if sample.media != media {
			continue
		}
		if _, ok := counts[sample.pid]; !ok {
			order = append(order, sample.pid)
		}
		counts[sample.pid]++
	}
	best := 0
	bestN := -1
	for _, pid := range order {
		if counts[pid] > bestN {
			best = pid
			bestN = counts[pid]
		}
	}
	return best
}

func fillProfile(dst *models.OfflineMediaProfile, src models.OfflineMediaProfile) {
	if dst.VideoCodec == "" {
		dst.VideoCodec = src.VideoCodec
	}
	if dst.AudioCodec == "" {
		dst.AudioCodec = src.AudioCodec
	}
	if dst.Width == 0 {
		dst.Width = src.Width
	}
	if dst.Height == 0 {
		dst.Height = src.Height
	}
	if dst.Profile == "" {
		dst.Profile = src.Profile
	}
	if dst.Level == 0 {
		dst.Level = src.Level
	}
	if dst.LevelName == "" {
		dst.LevelName = src.LevelName
	}
	if dst.FrameRate == 0 {
		dst.FrameRate = src.FrameRate
	}
	if dst.AudioSampleRate == 0 {
		dst.AudioSampleRate = src.AudioSampleRate
	}
	if dst.AudioChannels == 0 {
		dst.AudioChannels = src.AudioChannels
	}
	if dst.DurationMS == 0 {
		dst.DurationMS = src.DurationMS
	}
	if dst.VideoBitrateKbps == 0 {
		dst.VideoBitrateKbps = src.VideoBitrateKbps
	}
	if dst.AudioBitrateKbps == 0 {
		dst.AudioBitrateKbps = src.AudioBitrateKbps
	}
	if dst.MP4FastStart == nil && src.MP4FastStart != nil {
		dst.MP4FastStart = src.MP4FastStart
	}
	if src.MP4Fragmented {
		dst.MP4Fragmented = true
	}
	if dst.TSPacketCount == 0 {
		dst.TSPacketCount = src.TSPacketCount
	}
	if dst.TSSyncErrors == 0 {
		dst.TSSyncErrors = src.TSSyncErrors
	}
	if dst.TSTEIErrors == 0 {
		dst.TSTEIErrors = src.TSTEIErrors
	}
	if dst.TSCCErrors == 0 {
		dst.TSCCErrors = src.TSCCErrors
	}
	if dst.PCRCount == 0 {
		dst.PCRCount = src.PCRCount
	}
	if dst.PCRMaxGapMS == 0 {
		dst.PCRMaxGapMS = src.PCRMaxGapMS
	}
	if len(dst.NALUHistogram) == 0 && len(src.NALUHistogram) > 0 {
		dst.NALUHistogram = src.NALUHistogram
	}
}

func mergeWarnings(groups ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, group := range groups {
		for _, warning := range group {
			if warning == "" || seen[warning] {
				continue
			}
			seen[warning] = true
			out = append(out, warning)
			if len(out) >= 12 {
				return out
			}
		}
	}
	return out
}

func isKeyframe(frameType string) bool {
	if frameType == "" {
		return false
	}
	return frameType[0] == 'I' || frameType[0] == 'i'
}

func isBFrame(frameType string) bool {
	if frameType == "" {
		return false
	}
	return frameType == "B" || frameType == "b"
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func downsample(values []int64, limit int) []int64 {
	if limit <= 0 || len(values) <= limit {
		out := make([]int64, len(values))
		copy(out, values)
		return out
	}
	out := make([]int64, limit)
	last := len(values) - 1
	for i := 0; i < limit; i++ {
		idx := int(math.Round(float64(i) * float64(last) / float64(limit-1)))
		out[i] = values[idx]
	}
	return out
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }

func profileFromVideoMeta(meta *codec.VideoMeta, hevc bool) models.OfflineMediaProfile {
	if meta == nil {
		return models.OfflineMediaProfile{}
	}
	levelName := codec.H264LevelName(meta.Level)
	if hevc {
		levelName = codec.H265LevelName(meta.Level)
	}
	return models.OfflineMediaProfile{
		Width:     meta.Width,
		Height:    meta.Height,
		Profile:   meta.ProfileName,
		Level:     meta.Level,
		LevelName: levelName,
		FrameRate: meta.FrameRate,
	}
}
