package offline

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
)

const (
	maxStoredFrames        = 2000
	maxGOPPoints           = 480
	maxSeriesPoints        = 360
	maxBitratePoints       = 240
	largeGapMS             = 1000
	maxNormalAudioFrameLen = 65536
)

type timedSample struct {
	pid        int
	frameIndex int
	media      string
	dts        int64
	pts        int64
	cts        int64
	size       int
	key        bool
	b          bool
	sliceKnown bool

	hasContainerParams bool
	hasInbandParams    bool
	paramHash          string
	videoWidth         int
	videoHeight        int
	audioSampleRate    int
	audioChannels      int
}

type healthContext struct {
	Mode                  models.OfflineMode
	IsHEVC                bool
	HasContainerSeqHeader bool
	SyntheticTimestamps   bool
	CheckIDRParams        bool
}

func intPtr(v int) *int {
	if v < 0 {
		return nil
	}
	cp := v
	return &cp
}

func attachMediaProfile(flow *models.OfflineFlowResult, frames []models.OfflineFrameDetail, seed models.OfflineMediaProfile) {
	samples := make([]timedSample, 0, len(frames))
	for idx, frame := range frames {
		samples = append(samples, timedSample{
			frameIndex: idx,
			media:      frame.MediaType,
			dts:        frame.DTS,
			pts:        frame.PTS,
			cts:        frame.CTS,
			size:       frame.FrameLen,
			key:        isKeyframe(frame.FrameType),
			b:          isBFrame(frame.FrameType),
			sliceKnown: isExplicitSliceType(frame.FrameType),
		})
	}
	attachSampleProfileWithContext(flow, samples, seed, healthContext{})
	storeFlowFrames(flow, frames)
}

func attachMediaProfileWithSamples(flow *models.OfflineFlowResult, frames []models.OfflineFrameDetail, samples []timedSample, seed models.OfflineMediaProfile, hctx healthContext) {
	attachSampleProfileWithContext(flow, samples, seed, hctx)
	storeFlowFrames(flow, frames)
}

func storeFlowFrames(flow *models.OfflineFlowResult, frames []models.OfflineFrameDetail) {
	flow.FrameDetails = frames
	if len(frames) > maxStoredFrames {
		flow.FrameDetails = append([]models.OfflineFrameDetail(nil), frames[:maxStoredFrames]...)
		if flow.Profile != nil {
			flow.Profile.FramesTruncated = true
		}
	}
}

func attachSampleProfile(flow *models.OfflineFlowResult, samples []timedSample, seed models.OfflineMediaProfile) {
	attachSampleProfileWithContext(flow, samples, seed, healthContext{})
}

func attachSampleProfileWithContext(flow *models.OfflineFlowResult, samples []timedSample, seed models.OfflineMediaProfile, hctx healthContext) {
	stats := analyzeSamplesWithContext(samples, hctx)
	fillProfile(stats, seed)
	stats.Warnings = mergeWarnings(seed.Warnings, stats.Warnings)
	if (seed.VideoCodec == "h264" || seed.VideoCodec == "h265" || stats.VideoCodec == "h264" || stats.VideoCodec == "h265") &&
		stats.VideoFrames > 0 && stats.Keyframes == 0 {
		stats.Warnings = mergeWarnings(stats.Warnings, []string{"未识别到视频关键帧"})
	}
	// Append container/transport-specific health checks using merged seed fields.
	stats.HealthChecks = appendContainerTransportChecks(stats.HealthChecks, stats, hctx)
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
	return analyzeSamplesWithContext(samples, healthContext{})
}

func analyzeSamplesWithContext(samples []timedSample, hctx healthContext) *models.OfflineMediaProfile {
	stats := &models.OfflineMediaProfile{FrameTotal: len(samples)}
	videoPID := dominantPID(samples, "video")
	audioPID := dominantPID(samples, "audio")

	var video []timedSample
	var audio []timedSample
	for i, sample := range samples {
		if sample.frameIndex == 0 && i > 0 && len(samples) > 1 && samples[0].frameIndex == 0 && samples[1].frameIndex == 0 {
			// Caller did not populate frameIndex; assign sequential index.
			sample.frameIndex = i
		}
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

	firstDTSRegIdx := -1
	var firstDTSRegDTS int64
	firstGapIdx := -1
	var firstGapDTS int64

	// Rule 3 trackers (PTS/CTS)
	ptsLtDtsCount := 0
	firstPtsLtDtsIdx := -1
	var firstPtsLtDtsDTS int64
	nonZeroCTSCount := 0
	firstNonZeroCTSIdx := -1
	var firstNonZeroCTSDTS int64
	ptsRegCount := 0
	firstPTSRegIdx := -1
	var firstPTSRegDTS int64
	firstBIdx := -1
	var firstBDTS int64
	sliceKnownCount := 0

	// Rule 1 trackers (IDR missing SPS/PPS)
	idrMissingInband := 0
	firstMissingInbandIdx := -1
	var firstMissingInbandDTS int64
	idrMissingAny := 0
	firstMissingAnyIdx := -1
	var firstMissingAnyDTS int64
	firstIDRSeen := false
	firstIDRMissingAny := false

	// Rule 2 trackers (SPS/PPS / resolution changes)
	lastParamHash := ""
	lastW, lastH := 0, 0
	paramChanges := 0
	firstParamChangeIdx := -1
	var firstParamChangeDTS int64
	paramChangeResDesc := ""

	for _, sample := range video {
		if sample.sliceKnown {
			sliceKnownCount++
		}
		if sample.b && firstBIdx < 0 {
			firstBIdx = sample.frameIndex
			firstBDTS = sample.dts
		}
		if hasPrev {
			delta := sample.dts - prev
			videoDeltas = append(videoDeltas, delta)
			if delta < 0 {
				stats.DTSRegressions++
				if firstDTSRegIdx < 0 {
					firstDTSRegIdx = sample.frameIndex
					firstDTSRegDTS = sample.dts
				}
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
					if firstGapIdx < 0 {
						firstGapIdx = sample.frameIndex
						firstGapDTS = sample.dts
					}
					if delta > stats.MaxGapMS {
						stats.MaxGapMS = delta
					}
				}
			}
		}

		// PTS/CTS tracking
		if !hctx.SyntheticTimestamps {
			if sample.cts < 0 || (sample.pts != 0 && sample.pts < sample.dts) || (sample.pts == 0 && sample.dts > 0 && sample.cts < 0) {
				ptsLtDtsCount++
				if firstPtsLtDtsIdx < 0 {
					firstPtsLtDtsIdx = sample.frameIndex
					firstPtsLtDtsDTS = sample.dts
				}
			}
			if sample.cts != 0 {
				nonZeroCTSCount++
				if firstNonZeroCTSIdx < 0 {
					firstNonZeroCTSIdx = sample.frameIndex
					firstNonZeroCTSDTS = sample.dts
				}
			}
			if hasPrevPTS && sample.pts < prevPTS {
				ptsRegCount++
				if firstPTSRegIdx < 0 {
					firstPTSRegIdx = sample.frameIndex
					firstPTSRegDTS = sample.dts
				}
			}
		}
		if hasPrevPTS && sample.pts != 0 && prevPTS != 0 && sample.pts < prevPTS {
			stats.HasBFrames = true
		}
		if !hctx.SyntheticTimestamps || sample.pts != 0 {
			prevPTS = sample.pts
			hasPrevPTS = true
		}

		// Rule 2: Parameter set / resolution change tracking
		if sample.paramHash != "" {
			if lastParamHash != "" && sample.paramHash != lastParamHash {
				paramChanges++
				if firstParamChangeIdx < 0 {
					firstParamChangeIdx = sample.frameIndex
					firstParamChangeDTS = sample.dts
					if lastW > 0 && lastH > 0 && sample.videoWidth > 0 && sample.videoHeight > 0 &&
						(lastW != sample.videoWidth || lastH != sample.videoHeight) {
						paramChangeResDesc = fmt.Sprintf("%dx%d -> %dx%d", lastW, lastH, sample.videoWidth, sample.videoHeight)
					}
				}
			}
			lastParamHash = sample.paramHash
			if sample.videoWidth > 0 && sample.videoHeight > 0 {
				lastW = sample.videoWidth
				lastH = sample.videoHeight
			}
		}

		prev = sample.dts
		hasPrev = true
		if !sample.key {
			continue
		}

		// Rule 1: IDR parameter set check
		if hctx.CheckIDRParams {
			hasContainer := sample.hasContainerParams || hctx.HasContainerSeqHeader
			if !sample.hasInbandParams {
				idrMissingInband++
				if firstMissingInbandIdx < 0 {
					firstMissingInbandIdx = sample.frameIndex
					firstMissingInbandDTS = sample.dts
				}
				if !hasContainer {
					idrMissingAny++
					if firstMissingAnyIdx < 0 {
						firstMissingAnyIdx = sample.frameIndex
						firstMissingAnyDTS = sample.dts
					}
					if !firstIDRSeen {
						firstIDRMissingAny = true
					}
				}
			}
			firstIDRSeen = true
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

	// Rule 5: Audio gap, parameter change, and abnormal frame length checks
	var prevAudio int64
	var hasPrevAudio bool
	firstAudioGapIdx := -1
	var firstAudioGapDTS int64
	var maxAudioGapMS int64

	lastAudioSR, lastAudioCh := 0, 0
	audioParamChanges := 0
	firstAudioParamIdx := -1
	var firstAudioParamDTS int64
	audioParamDesc := ""

	zeroAudioFrames := 0
	oversizedAudioFrames := 0
	firstBadAudioSizeIdx := -1
	var firstBadAudioSizeDTS int64

	for _, sample := range audio {
		if hasPrevAudio {
			delta := sample.dts - prevAudio
			if delta > largeGapMS {
				stats.AudioLargeGaps++
				if firstAudioGapIdx < 0 {
					firstAudioGapIdx = sample.frameIndex
					firstAudioGapDTS = sample.dts
				}
				if delta > maxAudioGapMS {
					maxAudioGapMS = delta
				}
			}
		}
		if sample.audioSampleRate > 0 || sample.audioChannels > 0 {
			sr := sample.audioSampleRate
			ch := sample.audioChannels
			if (lastAudioSR > 0 && sr > 0 && sr != lastAudioSR) || (lastAudioCh > 0 && ch > 0 && ch != lastAudioCh) {
				audioParamChanges++
				if firstAudioParamIdx < 0 {
					firstAudioParamIdx = sample.frameIndex
					firstAudioParamDTS = sample.dts
					audioParamDesc = fmt.Sprintf("%dHz/%dch -> %dHz/%dch", lastAudioSR, lastAudioCh, sr, ch)
				}
			}
			if sr > 0 {
				lastAudioSR = sr
			}
			if ch > 0 {
				lastAudioCh = ch
			}
		}
		if sample.size <= 0 {
			zeroAudioFrames++
			if firstBadAudioSizeIdx < 0 {
				firstBadAudioSizeIdx = sample.frameIndex
				firstBadAudioSizeDTS = sample.dts
			}
		} else if sample.size > maxNormalAudioFrameLen {
			oversizedAudioFrames++
			if firstBadAudioSizeIdx < 0 {
				firstBadAudioSizeIdx = sample.frameIndex
				firstBadAudioSizeDTS = sample.dts
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

	// Rule 4: 200ms short-window peak bitrate & burst spike detection
	peak200Kbps, peakFrameIdx, peakDTS := computePeak200msBitrate(video)
	if peak200Kbps > 0 {
		stats.Peak200msKbps = round1(peak200Kbps)
		if stats.VideoBitrateKbps > 0 {
			stats.PeakToAvgRatio = round2(peak200Kbps / stats.VideoBitrateKbps)
		}
	}

	// A/V sync offset & timeline
	if len(video) > 0 && len(audio) > 0 {
		stats.HasAVOffset = true
		stats.AVStartOffsetMS = audio[0].dts - video[0].dts
		stats.AVSyncTimeline, stats.AVDriftMaxMS = computeAVSyncTimeline(video, audio, originDTS)
	}

	// Legacy warnings
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

	// Build structured HealthChecks
	var checks []models.OfflineHealthCheck
	paramLabel := "SPS/PPS"
	if hctx.IsHEVC {
		paramLabel = "VPS/SPS/PPS"
	}

	// Check 1: IDR missing SPS/PPS (or VPS/SPS/PPS)
	if hctx.CheckIDRParams && stats.Keyframes > 0 {
		if idrMissingAny > 0 {
			sev := "warn"
			title := fmt.Sprintf("IDR 前缺失 %s", paramLabel)
			detail := fmt.Sprintf("流内有 %d/%d 个 IDR 关键帧前未携带 %s（且无容器外带 Sequence Header），影响中途接入与 HLS 切片独立解码",
				idrMissingAny, stats.Keyframes, paramLabel)
			if firstIDRMissingAny {
				sev = "error"
				detail = fmt.Sprintf("首帧 IDR 即缺失 %s（共 %d/%d 个 IDR 缺失），无法独立起播且影响中途接入/HLS 切片解码",
					paramLabel, idrMissingAny, stats.Keyframes)
			}
			checks = append(checks, models.OfflineHealthCheck{
				Code:            "idr_missing_params",
				Category:        "codec",
				Severity:        sev,
				Title:           title,
				Detail:          detail,
				Count:           idrMissingAny,
				FirstFrameIndex: intPtr(firstMissingAnyIdx),
				FirstDTSMS:      firstMissingAnyDTS,
			})
			stats.Warnings = append(stats.Warnings, detail)
		} else if idrMissingInband > 0 {
			checks = append(checks, models.OfflineHealthCheck{
				Code:            "idr_missing_params",
				Category:        "codec",
				Severity:        "ok",
				Title:           fmt.Sprintf("IDR 参数集（容器外带 %s）", paramLabel),
				Detail:          fmt.Sprintf("容器已携带外带 Sequence Header，流内 %d/%d 个 IDR 未重复内嵌 %s；当前封装可正常解码，转封 TS/HLS 时需自动补齐参数集", idrMissingInband, stats.Keyframes, paramLabel),
				Count:           idrMissingInband,
				FirstFrameIndex: intPtr(firstMissingInbandIdx),
				FirstDTSMS:      firstMissingInbandDTS,
			})
		} else {
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "idr_missing_params",
				Category: "codec",
				Severity: "ok",
				Title:    fmt.Sprintf("IDR 参数集完整 (%s)", paramLabel),
				Detail:   fmt.Sprintf("全部 %d 个 IDR 关键帧前均携带完整 %s，支持中途接入与 HLS 切片独立解码", stats.Keyframes, paramLabel),
			})
		}
	}

	// Check 2: Dynamic resolution / SPS·PPS parameter switch
	if lastParamHash != "" {
		if paramChanges > 0 {
			detail := fmt.Sprintf("检测到 %d 次 %s 参数切换，首次位于帧 #%d (DTS %d ms)", paramChanges, paramLabel, firstParamChangeIdx+1, firstParamChangeDTS)
			if paramChangeResDesc != "" {
				detail = fmt.Sprintf("检测到 %d 次分辨率/编码参数切换（%s），首次位于帧 #%d (DTS %d ms)，部分播放器可能花屏或重初始化解码器",
					paramChanges, paramChangeResDesc, firstParamChangeIdx+1, firstParamChangeDTS)
			}
			checks = append(checks, models.OfflineHealthCheck{
				Code:            "param_set_change",
				Category:        "codec",
				Severity:        "warn",
				Title:           "分辨率 / 编码参数动态切换",
				Detail:          detail,
				Count:           paramChanges,
				FirstFrameIndex: intPtr(firstParamChangeIdx),
				FirstDTSMS:      firstParamChangeDTS,
			})
			stats.Warnings = append(stats.Warnings, detail)
		} else {
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "param_set_change",
				Category: "codec",
				Severity: "ok",
				Title:    "分辨率与编码参数稳定",
				Detail:   fmt.Sprintf("全程 %s 参数哈希与分辨率未发生动态切换", paramLabel),
			})
		}
	}

	// Check 3: PTS regression & B-frame CTS consistency
	if len(video) > 0 && !hctx.SyntheticTimestamps {
		allSlicesKnown := sliceKnownCount > 0 && sliceKnownCount >= len(video)/2
		hasExplicitB := stats.BFrames > 0
		switch {
		case ptsLtDtsCount > 0:
			detail := fmt.Sprintf("共 %d 帧出现 PTS < DTS（负 CTS），首次位于帧 #%d (DTS %d ms)，可能导致解码顺序或渲染时间戳异常",
				ptsLtDtsCount, firstPtsLtDtsIdx+1, firstPtsLtDtsDTS)
			checks = append(checks, models.OfflineHealthCheck{
				Code:            "pts_cts_anomaly",
				Category:        "timeline",
				Severity:        "error",
				Title:           "PTS < DTS 异常",
				Detail:          detail,
				Count:           ptsLtDtsCount,
				FirstFrameIndex: intPtr(firstPtsLtDtsIdx),
				FirstDTSMS:      firstPtsLtDtsDTS,
			})
			stats.Warnings = append(stats.Warnings, detail)
		case hasExplicitB && nonZeroCTSCount == 0:
			detail := fmt.Sprintf("检测到 %d 个 B 帧但全部视频帧 CTS 均为 0（PTS == DTS），播放时易出现画面回跳闪烁", stats.BFrames)
			checks = append(checks, models.OfflineHealthCheck{
				Code:            "pts_cts_anomaly",
				Category:        "timeline",
				Severity:        "warn",
				Title:           "B 帧 CTS 异常（全为 0）",
				Detail:          detail,
				Count:           stats.BFrames,
				FirstFrameIndex: intPtr(firstBIdx),
				FirstDTSMS:      firstBDTS,
			})
			stats.Warnings = append(stats.Warnings, detail)
		case !hasExplicitB && allSlicesKnown && nonZeroCTSCount > 0 && ptsRegCount == 0:
			detail := fmt.Sprintf("未检测到 B 帧但有 %d 帧携带非零 CTS 偏移，首次位于帧 #%d (DTS %d ms)",
				nonZeroCTSCount, firstNonZeroCTSIdx+1, firstNonZeroCTSDTS)
			checks = append(checks, models.OfflineHealthCheck{
				Code:            "pts_cts_anomaly",
				Category:        "timeline",
				Severity:        "warn",
				Title:           "无 B 帧却存在非零 CTS",
				Detail:          detail,
				Count:           nonZeroCTSCount,
				FirstFrameIndex: intPtr(firstNonZeroCTSIdx),
				FirstDTSMS:      firstNonZeroCTSDTS,
			})
			stats.Warnings = append(stats.Warnings, detail)
		case !hasExplicitB && nonZeroCTSCount == 0 && ptsRegCount > 0:
			detail := fmt.Sprintf("无 B 帧序列检测到 %d 次 PTS 倒退，首次位于帧 #%d (DTS %d ms)",
				ptsRegCount, firstPTSRegIdx+1, firstPTSRegDTS)
			checks = append(checks, models.OfflineHealthCheck{
				Code:            "pts_cts_anomaly",
				Category:        "timeline",
				Severity:        "error",
				Title:           "非 B 帧序列 PTS 倒退",
				Detail:          detail,
				Count:           ptsRegCount,
				FirstFrameIndex: intPtr(firstPTSRegIdx),
				FirstDTSMS:      firstPTSRegDTS,
			})
			stats.Warnings = append(stats.Warnings, detail)
		default:
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "pts_cts_anomaly",
				Category: "timeline",
				Severity: "ok",
				Title:    "PTS / CTS 时序正常",
				Detail:   "未检测到 PTS < DTS、非 B 帧 PTS 倒退或 B 帧 CTS 失配",
			})
		}
	}

	// Check 4: 200ms short-window bitrate burst spike
	if len(video) >= 2 && stats.DurationMS > 0 && stats.Peak200msKbps > 0 {
		if stats.VideoBitrateKbps > 100 && stats.PeakToAvgRatio >= 3.0 {
			detail := fmt.Sprintf("200ms 瞬时峰值码率 %.1f kbps，为平均码率 (%.1f kbps) 的 %.2f 倍（>=3.0x），存在 VBV 溢出与弱网卡顿风险",
				stats.Peak200msKbps, stats.VideoBitrateKbps, stats.PeakToAvgRatio)
			checks = append(checks, models.OfflineHealthCheck{
				Code:            "bitrate_spike_200ms",
				Category:        "bitrate",
				Severity:        "warn",
				Title:           "短窗口 (200ms) 瞬时码率突刺",
				Detail:          detail,
				Count:           1,
				FirstFrameIndex: intPtr(peakFrameIdx),
				FirstDTSMS:      peakDTS,
			})
			stats.Warnings = append(stats.Warnings, detail)
		} else {
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "bitrate_spike_200ms",
				Category: "bitrate",
				Severity: "ok",
				Title:    "短窗口 (200ms) 码率平稳",
				Detail:   fmt.Sprintf("200ms 瞬时峰值 %.1f kbps，峰均比 %.2fx（均值 %.1f kbps）", stats.Peak200msKbps, stats.PeakToAvgRatio, stats.VideoBitrateKbps),
			})
		}
	}

	// Check 5: Audio anomalies (param switches, gaps, zero/oversized frames)
	if len(audio) > 0 {
		var parts []string
		sev := "ok"
		totalAudioIssues := audioParamChanges + stats.AudioLargeGaps + zeroAudioFrames + oversizedAudioFrames
		firstIdx := -1
		var firstDTS int64
		pickFirst := func(idx int, dts int64) {
			if idx < 0 {
				return
			}
			if firstIdx < 0 || idx < firstIdx {
				firstIdx = idx
				firstDTS = dts
			}
		}
		if audioParamChanges > 0 {
			sev = "error"
			msg := fmt.Sprintf("采样率/声道突变 %d 次（%s）", audioParamChanges, audioParamDesc)
			parts = append(parts, msg)
			stats.Warnings = append(stats.Warnings, "音频"+msg)
			pickFirst(firstAudioParamIdx, firstAudioParamDTS)
		}
		if stats.AudioLargeGaps > 0 {
			if sev == "ok" {
				sev = "warn"
			}
			parts = append(parts, fmt.Sprintf("断音大间隔 %d 处（最大 %d ms）", stats.AudioLargeGaps, maxAudioGapMS))
			pickFirst(firstAudioGapIdx, firstAudioGapDTS)
		}
		if zeroAudioFrames > 0 || oversizedAudioFrames > 0 {
			if zeroAudioFrames > 0 {
				sev = "error"
			} else if sev == "ok" {
				sev = "warn"
			}
			msg := fmt.Sprintf("异常音频帧长（零长 %d 帧 / 超大 %d 帧）", zeroAudioFrames, oversizedAudioFrames)
			parts = append(parts, msg)
			stats.Warnings = append(stats.Warnings, msg)
			pickFirst(firstBadAudioSizeIdx, firstBadAudioSizeDTS)
		}
		if totalAudioIssues > 0 {
			checks = append(checks, models.OfflineHealthCheck{
				Code:            "audio_health",
				Category:        "audio",
				Severity:        sev,
				Title:           "音频流异常",
				Detail:          strings.Join(parts, "；"),
				Count:           totalAudioIssues,
				FirstFrameIndex: intPtr(firstIdx),
				FirstDTSMS:      firstDTS,
			})
		} else {
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "audio_health",
				Category: "audio",
				Severity: "ok",
				Title:    "音频流连续正常",
				Detail:   fmt.Sprintf("共 %d 个音频帧，采样率/声道稳定，无断音或异常帧长", len(audio)),
			})
		}
	}

	// Check 6: Video DTS continuity
	if len(video) > 1 && !hctx.SyntheticTimestamps {
		if stats.DTSRegressions > 0 || stats.LargeGaps > 0 {
			sev := "warn"
			idx := firstGapIdx
			dts := firstGapDTS
			if stats.DTSRegressions > 0 {
				sev = "error"
				idx = firstDTSRegIdx
				dts = firstDTSRegDTS
			}
			checks = append(checks, models.OfflineHealthCheck{
				Code:            "dts_continuity",
				Category:        "timeline",
				Severity:        sev,
				Title:           "视频 DTS 连续性异常",
				Detail:          fmt.Sprintf("DTS 回退 %d 次，超过 1s 大间隔 %d 处（最大 %d ms）", stats.DTSRegressions, stats.LargeGaps, stats.MaxGapMS),
				Count:           stats.DTSRegressions + stats.LargeGaps,
				FirstFrameIndex: intPtr(idx),
				FirstDTSMS:      dts,
			})
		} else {
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "dts_continuity",
				Category: "timeline",
				Severity: "ok",
				Title:    "视频 DTS 连续性正常",
				Detail:   fmt.Sprintf("平均帧间隔 %d ms（范围 %d~%d ms），无回退或卡顿间隔", stats.VideoDeltaAvgMS, stats.VideoDeltaMinMS, stats.VideoDeltaMaxMS),
			})
		}
	}

	// Check 7: A/V sync
	if stats.HasAVOffset {
		absStart := abs64(stats.AVStartOffsetMS)
		absDrift := abs64(stats.AVDriftMaxMS)
		if absStart > 500 || absDrift > 500 {
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "av_sync",
				Category: "timeline",
				Severity: "warn",
				Title:    "音视频同步偏差",
				Detail:   fmt.Sprintf("首帧偏差 %d ms，最大时间线漂移 %d ms（建议 <=500 ms）", stats.AVStartOffsetMS, stats.AVDriftMaxMS),
			})
		} else {
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "av_sync",
				Category: "timeline",
				Severity: "ok",
				Title:    "音视频同步良好",
				Detail:   fmt.Sprintf("首帧偏差 %d ms，最大漂移 %d ms", stats.AVStartOffsetMS, stats.AVDriftMaxMS),
			})
		}
	}

	stats.HealthChecks = checks
	return stats
}

func appendContainerTransportChecks(checks []models.OfflineHealthCheck, stats *models.OfflineMediaProfile, hctx healthContext) []models.OfflineHealthCheck {
	if stats == nil {
		return checks
	}
	if hctx.Mode == models.OfflineModeMP4 && stats.MP4FastStart != nil {
		if !*stats.MP4FastStart && !stats.MP4Fragmented {
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "container_faststart",
				Category: "container",
				Severity: "warn",
				Title:    "MP4 非 FastStart 结构",
				Detail:   "moov 位于 mdat 之后，边下边播起播前需额外 Range 请求，建议 ffmpeg -movflags +faststart 优化",
			})
		} else if stats.MP4Fragmented {
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "container_faststart",
				Category: "container",
				Severity: "ok",
				Title:    "fMP4 分片封装",
				Detail:   "检测到 moof/trun 分片结构，适合流式/低延迟分段播放",
			})
		} else {
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "container_faststart",
				Category: "container",
				Severity: "ok",
				Title:    "MP4 FastStart 就绪",
				Detail:   "moov 位于 mdat 之前，支持边下边播快速起播",
			})
		}
	}
	if hctx.Mode == models.OfflineModeTS && stats.TSPacketCount > 0 {
		errTotal := stats.TSCCErrors + stats.TSTEIErrors + stats.TSSyncErrors
		if errTotal > 0 || stats.PCRMaxGapMS > 100 {
			sev := "warn"
			if stats.TSCCErrors > 0 || stats.TSTEIErrors > 0 || stats.TSSyncErrors > 0 {
				sev = "error"
			}
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "ts_transport",
				Category: "transport",
				Severity: sev,
				Title:    "TS 传输层异常",
				Detail:   fmt.Sprintf("CC 错误 %d、TEI 错误 %d、Sync 丢失 %d、PCR 最大间隔 %d ms", stats.TSCCErrors, stats.TSTEIErrors, stats.TSSyncErrors, stats.PCRMaxGapMS),
				Count:    errTotal,
			})
		} else {
			checks = append(checks, models.OfflineHealthCheck{
				Code:     "ts_transport",
				Category: "transport",
				Severity: "ok",
				Title:    "TS 传输层健康",
				Detail:   fmt.Sprintf("共 %d 个 TS 包，无 CC/TEI/Sync 异常，PCR 最大间隔 %d ms", stats.TSPacketCount, stats.PCRMaxGapMS),
			})
		}
	}
	return checks
}

func computePeak200msBitrate(video []timedSample) (peakKbps float64, peakFrameIdx int, peakDTS int64) {
	if len(video) < 2 {
		return 0, -1, 0
	}
	type pt struct {
		dts  int64
		size int
		idx  int
	}
	pts := make([]pt, len(video))
	for i, s := range video {
		pts[i] = pt{dts: s.dts, size: s.size, idx: s.frameIndex}
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].dts < pts[j].dts })

	left := 0
	var winBytes int64
	peakFrameIdx = -1
	for right := 0; right < len(pts); right++ {
		winBytes += int64(pts[right].size)
		for left < right && pts[right].dts-pts[left].dts >= 200 {
			winBytes -= int64(pts[left].size)
			left++
		}
		kbps := float64(winBytes) * 8.0 / 200.0
		if kbps > peakKbps {
			peakKbps = kbps
			peakFrameIdx = pts[right].idx
			peakDTS = pts[right].dts
		}
	}
	return peakKbps, peakFrameIdx, peakDTS
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
			if len(out) >= 16 {
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

func isExplicitSliceType(frameType string) bool {
	switch frameType {
	case "I", "P", "B", "SP", "SI":
		return true
	default:
		return false
	}
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
