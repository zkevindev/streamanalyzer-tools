package offline

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"streamanalyzer/internal/dash"
	"streamanalyzer/internal/models"
)

// runDASH parses an offline MPEG-DASH (.mpd) manifest, writes a human-readable
// mpd_report.txt artifact, and populates media profile + health checks.
func (m *Manager) runDASH(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	ctx := m.taskContext(task.ID)
	m.setProgressPercent(task.ID, "读取 MPEG-DASH MPD 清单", 15, 0)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	raw, err := os.ReadFile(task.InputPath)
	if err != nil {
		return err
	}
	mpd, err := dash.ParseMPD(raw)
	if err != nil {
		return err
	}

	m.setProgressPercent(task.ID, "分析 Period / AdaptationSet / SegmentTimeline", 60, int64(len(raw)))

	outDir := filepath.Join(taskDir, "dash")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	var (
		bestVideo   *dash.Representation
		bestVideoAS *dash.AdaptationSet
		bestAudio   *dash.Representation
		totalAS     int
		totalReps   int
		totalGaps   int
		drmSet      = map[string]bool{}
		drmList     []string
		reportBuf   bytes.Buffer
	)

	reportBuf.WriteString("# MPEG-DASH MPD Manifest Report\n")
	reportBuf.WriteString(fmt.Sprintf("# File: %s (%d bytes)\n", task.InputName, len(raw)))
	reportBuf.WriteString(fmt.Sprintf("Type: %s\n", mpd.Type))
	if mpd.Profiles != "" {
		reportBuf.WriteString(fmt.Sprintf("Profiles: %s\n", mpd.Profiles))
	}
	if mpd.MediaPresentationDuration != "" {
		reportBuf.WriteString(fmt.Sprintf("MediaPresentationDuration: %s (%d ms)\n", mpd.MediaPresentationDuration, mpd.MediaPresentationDurationMS))
	}
	if mpd.MinBufferTime != "" {
		reportBuf.WriteString(fmt.Sprintf("MinBufferTime: %s (%d ms)\n", mpd.MinBufferTime, mpd.MinBufferTimeMS))
	}
	if mpd.MinimumUpdatePeriod != "" {
		reportBuf.WriteString(fmt.Sprintf("MinimumUpdatePeriod: %s (%d ms)\n", mpd.MinimumUpdatePeriod, mpd.MinimumUpdatePeriodMS))
	}
	if mpd.AvailabilityStartTime != "" {
		reportBuf.WriteString(fmt.Sprintf("AvailabilityStartTime: %s\n", mpd.AvailabilityStartTime))
	}
	if mpd.PublishTime != "" {
		reportBuf.WriteString(fmt.Sprintf("PublishTime: %s\n", mpd.PublishTime))
	}
	if len(mpd.BaseURLs) > 0 {
		reportBuf.WriteString(fmt.Sprintf("BaseURLs: %s\n", strings.Join(mpd.BaseURLs, ", ")))
	}
	reportBuf.WriteString(fmt.Sprintf("Periods: %d\n\n", len(mpd.Periods)))

	for pi, p := range mpd.Periods {
		reportBuf.WriteString(fmt.Sprintf("=== Period #%d (id=%s start=%s duration=%s) ===\n",
			pi+1, emptyOrDash(p.ID), emptyOrDash(p.Start), emptyOrDash(p.Duration)))
		for ai := range p.AdaptationSets {
			as := &p.AdaptationSets[ai]
			totalAS++
			for _, d := range as.DRM {
				if !drmSet[d] {
					drmSet[d] = true
					drmList = append(drmList, d)
				}
			}
			reportBuf.WriteString(fmt.Sprintf("  [AdaptationSet #%d] id=%s type=%s mime=%s lang=%s align=%s\n",
				ai+1, emptyOrDash(as.ID), emptyOrDash(as.ContentType), emptyOrDash(as.MimeType), emptyOrDash(as.Lang), emptyOrDash(as.SegmentAlignment)))
			if len(as.DRM) > 0 {
				reportBuf.WriteString(fmt.Sprintf("    DRM / ContentProtection: %s\n", strings.Join(as.DRM, ", ")))
			}
			for ri := range as.Representations {
				rep := &as.Representations[ri]
				totalReps++
				if as.ContentType == "video" && (bestVideo == nil || rep.Bandwidth > bestVideo.Bandwidth) {
					bestVideo = rep
					bestVideoAS = as
				}
				if as.ContentType == "audio" && (bestAudio == nil || rep.Bandwidth > bestAudio.Bandwidth) {
					bestAudio = rep
				}

				initURL := dash.ResolveInitURL("", mpd, p, *as, *rep)
				segs, gaps := dash.ExpandMediaSegments("", mpd, p, *as, *rep, 12)
				totalGaps += gaps

				res := "-"
				if rep.Width > 0 && rep.Height > 0 {
					res = fmt.Sprintf("%dx%d", rep.Width, rep.Height)
				}
				reportBuf.WriteString(fmt.Sprintf("    - Rep id=%s bandwidth=%d bps (%.1f kbps) codecs=%s res=%s fps=%s sr=%s\n",
					emptyOrDash(rep.ID), rep.Bandwidth, float64(rep.Bandwidth)/1000.0,
					emptyOrDash(rep.Codecs), res, emptyOrDash(rep.FrameRate), emptyOrDash(rep.AudioSamplingRate)))
				if initURL != "" {
					reportBuf.WriteString(fmt.Sprintf("      init: %s\n", initURL))
				}
				if len(segs) > 0 {
					reportBuf.WriteString(fmt.Sprintf("      segments (preview %d, timeline_gaps=%d):\n", len(segs), gaps))
					for _, s := range segs {
						gapNote := ""
						if s.GapMS > 0 {
							gapNote = fmt.Sprintf(" [GAP +%dms]", s.GapMS)
						}
						reportBuf.WriteString(fmt.Sprintf("        #%d t=%d dur=%dms %s%s\n",
							s.Number, s.Time, s.DurationMS, s.URL, gapNote))
					}
				}
			}
		}
		reportBuf.WriteString("\n")
	}

	reportPath := filepath.Join(outDir, "mpd_report.txt")
	if err := os.WriteFile(reportPath, reportBuf.Bytes(), 0o644); err != nil {
		return err
	}

	profile := &models.OfflineMediaProfile{
		DurationMS: mpd.MediaPresentationDurationMS,
	}
	if bestVideo != nil {
		profile.VideoCodec = normalizeDASHCodec(bestVideo.Codecs)
		profile.Width = bestVideo.Width
		profile.Height = bestVideo.Height
		profile.VideoBitrateKbps = float64(bestVideo.Bandwidth) / 1000.0
		profile.FrameRate = parseDASHFrameRate(bestVideo.FrameRate)
		if bestVideoAS != nil && profile.FrameRate == 0 {
			profile.FrameRate = parseDASHFrameRate(bestVideoAS.FrameRate)
		}
	}
	if bestAudio != nil {
		profile.AudioCodec = normalizeDASHCodec(bestAudio.Codecs)
		profile.AudioBitrateKbps = float64(bestAudio.Bandwidth) / 1000.0
		if sr, err := strconv.Atoi(strings.TrimSpace(bestAudio.AudioSamplingRate)); err == nil && sr > 0 {
			profile.AudioSampleRate = sr
		}
	}

	var checks []models.OfflineHealthCheck
	if totalReps == 0 {
		checks = append(checks, models.OfflineHealthCheck{
			Code:     "dash_no_representations",
			Category: "container",
			Severity: "error",
			Title:    "未解析到任何 Representation",
			Detail:   fmt.Sprintf("共 %d 个 Period、%d 个 AdaptationSet，但未找到有效码率档位", len(mpd.Periods), totalAS),
		})
		profile.Warnings = append(profile.Warnings, "MPD 中未发现任何 Representation")
	} else {
		checks = append(checks, models.OfflineHealthCheck{
			Code:     "dash_structure_ok",
			Category: "container",
			Severity: "ok",
			Title:    "MPD 清单结构完整",
			Detail:   fmt.Sprintf("type=%s · %d Period · %d AdaptationSet · %d Representation", mpd.Type, len(mpd.Periods), totalAS, totalReps),
			Count:    totalReps,
		})
	}

	if totalGaps > 0 {
		checks = append(checks, models.OfflineHealthCheck{
			Code:     "dash_timeline_gap",
			Category: "timeline",
			Severity: "warn",
			Title:    "SegmentTimeline 存在时间线跳变/缺口",
			Detail:   fmt.Sprintf("在 MPD 分片时间线中检测到 %d 处时间不连续（详情见 mpd_report.txt）", totalGaps),
			Count:    totalGaps,
		})
		profile.Warnings = append(profile.Warnings, fmt.Sprintf("SegmentTimeline 检测到 %d 处时间线缺口", totalGaps))
	} else {
		checks = append(checks, models.OfflineHealthCheck{
			Code:     "dash_timeline_continuous",
			Category: "timeline",
			Severity: "ok",
			Title:    "SegmentTimeline 连续性正常",
			Detail:   "未发现明显的分片时间线跳变或空洞",
		})
	}

	if len(drmList) > 0 {
		checks = append(checks, models.OfflineHealthCheck{
			Code:     "dash_drm_detected",
			Category: "codec",
			Severity: "warn",
			Title:    "清单声明了 DRM 内容保护",
			Detail:   "检测到 ContentProtection: " + strings.Join(drmList, ", ") + "（加密分片需密钥授权后方可解码）",
			Count:    len(drmList),
		})
		profile.Warnings = append(profile.Warnings, "检测到 DRM 保护: "+strings.Join(drmList, ", "))
	}

	profile.HealthChecks = checks

	flow := models.OfflineFlowResult{
		FlowID:        1,
		Protocol:      "mpeg-dash",
		Direction:     mpd.Type,
		DumpRawDir:    "single",
		RawPath:       task.InputPath,
		ArtifactPaths: []string{reportPath},
		PayloadBytes:  len(raw),
		VideoCodec:    profile.VideoCodec,
		AudioCodec:    profile.AudioCodec,
		Profile:       profile,
	}
	summary.Flows = append(summary.Flows, flow)
	return nil
}

func normalizeDASHCodec(raw string) string {
	c := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(c, "avc"):
		return "h264"
	case strings.HasPrefix(c, "hev") || strings.HasPrefix(c, "hvc"):
		return "h265"
	case strings.HasPrefix(c, "av01"):
		return "av1"
	case strings.HasPrefix(c, "vp09") || strings.HasPrefix(c, "vp9"):
		return "vp9"
	case strings.HasPrefix(c, "mp4a"):
		return "aac"
	case strings.HasPrefix(c, "opus"):
		return "opus"
	case strings.HasPrefix(c, "ac-3") || strings.HasPrefix(c, "ec-3"):
		return c
	default:
		return c
	}
}

func parseDASHFrameRate(raw string) float64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	if numStr, denStr, ok := strings.Cut(s, "/"); ok {
		num, err1 := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
		den, err2 := strconv.ParseFloat(strings.TrimSpace(denStr), 64)
		if err1 == nil && err2 == nil && den > 0 {
			return num / den
		}
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
