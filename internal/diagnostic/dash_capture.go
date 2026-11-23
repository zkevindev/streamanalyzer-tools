package diagnostic

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"streamanalyzer/internal/dash"
)

const (
	dashMetaFileName = "metadata.json"
	maxMPDBytes      = 4 << 20
	maxDASHSegments  = 6
)

type DASHMetrics struct {
	MPDType               string                      `json:"mpd_type"`
	Profiles              string                      `json:"profiles,omitempty"`
	MediaPresentationMS   int64                       `json:"media_presentation_ms,omitempty"`
	MinBufferTimeMS       int64                       `json:"min_buffer_time_ms,omitempty"`
	MinimumUpdatePeriodMS int64                       `json:"minimum_update_period_ms,omitempty"`
	Periods               int                         `json:"periods"`
	AdaptationSets        int                         `json:"adaptation_sets"`
	RepresentationsCount  int                         `json:"representations_count"`
	ManifestsCaptured     int                         `json:"manifests_captured"`
	ManifestFailures      int                         `json:"manifest_failures"`
	InitSegmentsCaptured  int                         `json:"init_segments_captured"`
	InitSegmentsFailed    int                         `json:"init_segments_failed"`
	SegmentsCaptured      int                         `json:"segments_captured"`
	SegmentsFailed        int                         `json:"segments_failed"`
	TimelineGaps          int                         `json:"timeline_gaps"`
	MaxSegmentDownloadMS  int64                       `json:"max_segment_download_ms"`
	MediaBytes            int64                       `json:"media_bytes"`
	DRMSystems            []string                    `json:"drm_systems,omitempty"`
	Representations       []DASHRepresentationSummary `json:"representations,omitempty"`
	SampledSegments       []DASHSampledSegment        `json:"sampled_segments,omitempty"`
}

type DASHRepresentationSummary struct {
	PeriodID          string `json:"period_id,omitempty"`
	ContentType       string `json:"content_type"`
	ID                string `json:"id"`
	Bandwidth         int64  `json:"bandwidth"`
	MimeType          string `json:"mime_type,omitempty"`
	Codecs            string `json:"codecs,omitempty"`
	Width             int    `json:"width,omitempty"`
	Height            int    `json:"height,omitempty"`
	FrameRate         string `json:"frame_rate,omitempty"`
	AudioSamplingRate string `json:"audio_sampling_rate,omitempty"`
	TimelineGaps      int    `json:"timeline_gaps,omitempty"`
	Sampled           bool   `json:"sampled,omitempty"`
}

type DASHSampledSegment struct {
	Kind             string `json:"kind"` // init | media
	ContentType      string `json:"content_type"`
	RepresentationID string `json:"representation_id"`
	Number           uint64 `json:"number,omitempty"`
	DurationMS       int64  `json:"duration_ms,omitempty"`
	ArrivalMS        int64  `json:"arrival_ms"`
	DownloadMS       int64  `json:"download_ms"`
	HTTPStatus       int    `json:"http_status"`
	Bytes            int64  `json:"bytes"`
	Artifact         string `json:"artifact,omitempty"`
	Error            string `json:"error,omitempty"`
}

type dashArchiveMetadata struct {
	Version    int         `json:"version"`
	Metrics    DASHMetrics `json:"metrics"`
	ParseError string      `json:"parse_error,omitempty"`
}

func sampleDASH(ctx context.Context, client *http.Client, dir string, target *url.URL, seconds int) (Report, error) {
	startedAt := time.Now().UTC()
	start := time.Now()
	nowMS := func() int64 { return max(0, time.Since(start).Milliseconds()) }

	limit := captureLimit(ctx)
	if limit <= 0 {
		limit = maxStreamBytes
	}

	sampleCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()

	obs := Observation{
		Origin:           origin(target),
		StartedAt:        startedAt,
		RequestedSeconds: seconds,
		EndReason:        "duration_limit",
	}

	meta := dashArchiveMetadata{Version: 1}
	var (
		archivedFiles = map[string][]byte{}
		archivedBytes int64
		totalBytes    int64
	)

	addArchive := func(name string, data []byte) {
		if archivedBytes+int64(len(data)) > limit {
			return
		}
		cp := make([]byte, len(data))
		copy(cp, data)
		archivedFiles[name] = cp
		archivedBytes += int64(len(data))
	}

	fetchURL := func(observed bool, rawURL string, maxBytes int64) (int, []byte, int64, int64, error) {
		reqStart := nowMS()
		req, err := http.NewRequestWithContext(sampleCtx, http.MethodGet, rawURL, nil)
		if err != nil {
			return 0, nil, reqStart, 0, err
		}
		applyCaptureHeaders(sampleCtx, req)
		if req.Header.Get("User-Agent") == "" {
			req.Header.Set("User-Agent", "StreamAnalyzer-Diagnostic/1.0")
		}
		req.Header.Set("Accept-Encoding", "identity")
		var resp *http.Response
		if observed {
			resp, err = doObservedRequest(client, req, &obs, start)
		} else {
			resp, err = client.Do(req)
		}
		if err != nil {
			return 0, nil, nowMS(), max(0, nowMS()-reqStart), err
		}
		defer resp.Body.Close()
		recvStart := nowMS()
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
		arr := nowMS()
		dl := max(0, arr-reqStart)
		if observed {
			recordHTTPBody(&obs, CaptureStage{Name: "receive", StartMS: recvStart, EndMS: arr})
		}
		if rerr != nil {
			return resp.StatusCode, body, arr, dl, rerr
		}
		return resp.StatusCode, body, arr, dl, nil
	}

	// 1. Fetch initial MPD manifest (recorded in Observation HTTP timings).
	status, mpdBody, _, _, fetchErr := fetchURL(true, target.String(), maxMPDBytes)
	if fetchErr != nil {
		meta.Metrics.ManifestFailures++
		obs.EndReason = captureEnd(sampleCtx, "request_error")
		if obs.FailureStage == "" {
			obs.FailureStage, obs.FailureCode = "request_send", "network_error"
		}
	} else {
		obs.HTTPStatus = status
		if status < 200 || status >= 300 {
			meta.Metrics.ManifestFailures++
			obs.EndReason = "http_status"
			obs.FailureStage, obs.FailureCode = "response_headers", "http_status"
		} else {
			meta.Metrics.ManifestsCaptured++
			markFirstPayload(&obs, start)
			addArchive("manifest-1.mpd", mpdBody)

			mpd, parseErr := dash.ParseMPD(mpdBody)
			if parseErr != nil {
				meta.ParseError = parseErr.Error()
				obs.EndReason = "body_error"
				obs.FailureStage, obs.FailureCode = "receive", "body_error"
			} else {
				populateDASHManifestMetrics(&meta.Metrics, mpd)
				// Pick top video and top audio representations from first period to sample.
				type repTarget struct {
					period dash.Period
					as     dash.AdaptationSet
					rep    dash.Representation
					index  int
				}
				var chosenVideo, chosenAudio *repTarget
				repIdx := 0
				for _, p := range mpd.Periods {
					for _, as := range p.AdaptationSets {
						for _, rep := range as.Representations {
							if as.ContentType == "video" && (chosenVideo == nil || rep.Bandwidth > chosenVideo.rep.Bandwidth) {
								chosenVideo = &repTarget{period: p, as: as, rep: rep, index: repIdx}
							}
							if as.ContentType == "audio" && (chosenAudio == nil || rep.Bandwidth > chosenAudio.rep.Bandwidth) {
								chosenAudio = &repTarget{period: p, as: as, rep: rep, index: repIdx}
							}
							repIdx++
						}
					}
				}

				targets := []*repTarget{}
				if chosenVideo != nil {
					if chosenVideo.index < len(meta.Metrics.Representations) {
						meta.Metrics.Representations[chosenVideo.index].Sampled = true
					}
					targets = append(targets, chosenVideo)
				}
				if chosenAudio != nil {
					if chosenAudio.index < len(meta.Metrics.Representations) {
						meta.Metrics.Representations[chosenAudio.index].Sampled = true
					}
					targets = append(targets, chosenAudio)
				}

				// 2. Download init segments and up to maxDASHSegments media segments.
				docURL := target.String()
				segCount := 0
				for _, t := range targets {
					if sampleCtx.Err() != nil {
						break
					}
					if initURL := dash.ResolveInitURL(docURL, mpd, t.period, t.as, t.rep); initURL != "" {
						st, data, arr, dl, err := fetchURL(false, initURL, min(limit-totalBytes, 8<<20))
						entry := DASHSampledSegment{
							Kind:             "init",
							ContentType:      t.as.ContentType,
							RepresentationID: t.rep.ID,
							ArrivalMS:        arr,
							DownloadMS:       dl,
							HTTPStatus:       st,
							Bytes:            int64(len(data)),
						}
						if err != nil {
							entry.Error = err.Error()
							meta.Metrics.InitSegmentsFailed++
						} else if st < 200 || st >= 300 {
							entry.Error = fmt.Sprintf("HTTP %d", st)
							meta.Metrics.InitSegmentsFailed++
						} else {
							meta.Metrics.InitSegmentsCaptured++
							meta.Metrics.MediaBytes += int64(len(data))
							totalBytes += int64(len(data))
							art := fmt.Sprintf("init-%s.bin", sanitizeDASHName(t.rep.ID))
							entry.Artifact = art
							addArchive(art, data)
						}
						meta.Metrics.SampledSegments = append(meta.Metrics.SampledSegments, entry)
					}

					segs, _ := dash.ExpandMediaSegments(docURL, mpd, t.period, t.as, t.rep, maxDASHSegments)
					perTrackLimit := maxDASHSegments
					if len(targets) > 1 {
						perTrackLimit = maxDASHSegments / 2
						if perTrackLimit < 2 {
							perTrackLimit = 2
						}
					}
					for i, seg := range segs {
						if i >= perTrackLimit || segCount >= maxDASHSegments || sampleCtx.Err() != nil || totalBytes >= limit {
							break
						}
						segCount++
						st, data, arr, dl, err := fetchURL(false, seg.URL, min(limit-totalBytes, 16<<20))
						entry := DASHSampledSegment{
							Kind:             "media",
							ContentType:      t.as.ContentType,
							RepresentationID: t.rep.ID,
							Number:           seg.Number,
							DurationMS:       seg.DurationMS,
							ArrivalMS:        arr,
							DownloadMS:       dl,
							HTTPStatus:       st,
							Bytes:            int64(len(data)),
						}
						if err != nil {
							entry.Error = err.Error()
							meta.Metrics.SegmentsFailed++
						} else if st < 200 || st >= 300 {
							entry.Error = fmt.Sprintf("HTTP %d", st)
							meta.Metrics.SegmentsFailed++
						} else {
							meta.Metrics.SegmentsCaptured++
							meta.Metrics.MediaBytes += int64(len(data))
							totalBytes += int64(len(data))
							if dl > meta.Metrics.MaxSegmentDownloadMS {
								meta.Metrics.MaxSegmentDownloadMS = dl
							}
							art := fmt.Sprintf("seg-%s-%d.bin", sanitizeDASHName(t.rep.ID), seg.Number)
							entry.Artifact = art
							addArchive(art, data)
						}
						meta.Metrics.SampledSegments = append(meta.Metrics.SampledSegments, entry)
					}
				}

				// 3. For dynamic/live MPDs, perform one additional manifest refresh if time remains.
				if mpd.Type == "dynamic" && sampleCtx.Err() == nil {
					waitDur := 2 * time.Second
					if mpd.MinimumUpdatePeriodMS >= 1000 && mpd.MinimumUpdatePeriodMS <= 6000 {
						waitDur = time.Duration(mpd.MinimumUpdatePeriodMS) * time.Millisecond
					}
					timer := time.NewTimer(waitDur)
					select {
					case <-timer.C:
						st2, body2, _, _, err2 := fetchURL(false, target.String(), maxMPDBytes)
						if err2 == nil && st2 >= 200 && st2 < 300 {
							meta.Metrics.ManifestsCaptured++
							addArchive("manifest-2.mpd", body2)
						} else {
							meta.Metrics.ManifestFailures++
						}
					case <-sampleCtx.Done():
					}
					timer.Stop()
				}

				if ctx.Err() != nil {
					obs.EndReason = "canceled"
				} else if totalBytes >= limit {
					obs.EndReason = "size_limit"
				} else {
					obs.EndReason = "eof"
				}
			}
		}
	}

	obs.ElapsedMS = max(1, nowMS())
	clampObservationStages(&obs)

	streamArtifact, err := writeDASHArchive(dir, meta, archivedFiles)
	if err != nil {
		return Report{}, err
	}

	manifest := Manifest{
		Version:          multiProtocolSchemaVersion,
		Protocol:         ProtocolDASH,
		StreamLimitBytes: limit,
		Observation:      obs,
		Stream:           streamArtifact,
	}
	applyAnalysisConfig(ctx, &manifest)

	report, events, err := replayDASHWithEvents(dir, manifest)
	if err != nil {
		return Report{}, err
	}
	tlArtifact, err := writeTimelineArtifact(dir, events)
	if err != nil {
		return Report{}, err
	}
	report.Manifest.Timeline = tlArtifact
	return report, nil
}

func populateDASHManifestMetrics(m *DASHMetrics, mpd *dash.MPD) {
	m.MPDType = mpd.Type
	m.Profiles = mpd.Profiles
	m.MediaPresentationMS = mpd.MediaPresentationDurationMS
	m.MinBufferTimeMS = mpd.MinBufferTimeMS
	m.MinimumUpdatePeriodMS = mpd.MinimumUpdatePeriodMS
	m.Periods = len(mpd.Periods)

	drmSeen := map[string]bool{}
	for _, p := range mpd.Periods {
		for _, as := range p.AdaptationSets {
			m.AdaptationSets++
			for _, d := range as.DRM {
				if !drmSeen[d] {
					drmSeen[d] = true
					m.DRMSystems = append(m.DRMSystems, d)
				}
			}
			for _, rep := range as.Representations {
				m.RepresentationsCount++
				_, gaps := dash.ExpandMediaSegments("", mpd, p, as, rep, 32)
				m.TimelineGaps += gaps
				m.Representations = append(m.Representations, DASHRepresentationSummary{
					PeriodID:          p.ID,
					ContentType:       as.ContentType,
					ID:                rep.ID,
					Bandwidth:         rep.Bandwidth,
					MimeType:          rep.MimeType,
					Codecs:            rep.Codecs,
					Width:             rep.Width,
					Height:            rep.Height,
					FrameRate:         rep.FrameRate,
					AudioSamplingRate: rep.AudioSamplingRate,
					TimelineGaps:      gaps,
				})
			}
		}
	}
}

func writeDASHArchive(dir string, meta dashArchiveMetadata, files map[string][]byte) (Artifact, error) {
	zipPath := filepath.Join(dir, dashStreamName)
	f, err := os.Create(zipPath)
	if err != nil {
		return Artifact{}, err
	}
	zw := zip.NewWriter(f)

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		_ = zw.Close()
		_ = f.Close()
		return Artifact{}, err
	}
	if w, err := zw.Create(dashMetaFileName); err != nil {
		_ = zw.Close()
		_ = f.Close()
		return Artifact{}, err
	} else if _, err := w.Write(metaBytes); err != nil {
		_ = zw.Close()
		_ = f.Close()
		return Artifact{}, err
	}
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			return Artifact{}, err
		}
		if _, err := w.Write(data); err != nil {
			_ = zw.Close()
			_ = f.Close()
			return Artifact{}, err
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return Artifact{}, err
	}
	if err := f.Close(); err != nil {
		return Artifact{}, err
	}
	return hashFileArtifact(zipPath)
}

func replayDASH(dir string, manifest Manifest) (Report, error) {
	report, _, err := replayDASHWithEvents(dir, manifest)
	return report, err
}

func replayDASHWithEvents(dir string, manifest Manifest) (Report, []Event, error) {
	zr, f, err := openEvidenceZIP(filepath.Join(dir, dashStreamName))
	if err != nil {
		return Report{}, nil, fmt.Errorf("%w: 无法读取 DASH 证据归档", ErrInvalid)
	}
	defer f.Close()

	var meta dashArchiveMetadata
	found := false
	for _, zf := range zr.File {
		if zf.Name == dashMetaFileName {
			rc, err := zf.Open()
			if err != nil {
				return Report{}, nil, err
			}
			err = json.NewDecoder(io.LimitReader(rc, 2<<20)).Decode(&meta)
			_ = rc.Close()
			if err != nil {
				return Report{}, nil, fmt.Errorf("%w: DASH 元数据损坏", ErrInvalid)
			}
			found = true
			break
		}
	}
	if !found {
		return Report{}, nil, fmt.Errorf("%w: DASH 证据归档缺少 metadata.json", ErrInvalid)
	}

	dm := meta.Metrics
	metrics := Metrics{MediaTimingUnit: "segment"}
	vSeen := map[string]bool{}
	aSeen := map[string]bool{}
	for _, rep := range dm.Representations {
		if rep.Codecs == "" {
			continue
		}
		if rep.ContentType == "video" && !vSeen[rep.Codecs] {
			vSeen[rep.Codecs] = true
			metrics.VideoCodecs = append(metrics.VideoCodecs, rep.Codecs)
		} else if rep.ContentType == "audio" && !aSeen[rep.Codecs] {
			aSeen[rep.Codecs] = true
			metrics.AudioCodecs = append(metrics.AudioCodecs, rep.Codecs)
		}
	}

	var events []Event
	for _, seg := range dm.SampledSegments {
		kind := "dash_segment"
		if seg.Kind == "init" {
			kind = "dash_init"
		}
		seqVal := int(seg.Number)
		ev := Event{
			Size:       int(seg.Bytes),
			ArrivalMS:  seg.ArrivalMS,
			Kind:       kind,
			Artifact:   seg.Artifact,
			Rendition:  seg.ContentType,
			HTTPStatus: seg.HTTPStatus,
			DurationMS: seg.DownloadMS,
		}
		if seg.Number > 0 {
			ev.Sequence = &seqVal
		}
		events = append(events, ev)

		if seg.Kind == "media" && seg.Error == "" {
			if seg.ContentType == "video" {
				metrics.VideoFrames++
				if metrics.FirstVideoMS == nil {
					v := seg.ArrivalMS
					metrics.FirstVideoMS = &v
				}
			} else if seg.ContentType == "audio" {
				metrics.AudioFrames++
				if metrics.FirstAudioMS == nil {
					v := seg.ArrivalMS
					metrics.FirstAudioMS = &v
				}
			}
		}
	}

	findings := buildDASHFindings(manifest, dm, meta.ParseError)
	report := Report{
		RuleVersion: "dash/1",
		Manifest:    manifest,
		Metrics:     metrics,
		DASH:        &dm,
		Findings:    findings,
	}
	return report, events, nil
}

func buildDASHFindings(manifest Manifest, dm DASHMetrics, parseErr string) []Finding {
	var findings []Finding
	nextID := func() string { return fmt.Sprintf("F%02d", len(findings)+1) }

	if dm.ManifestFailures > 0 && dm.ManifestsCaptured == 0 {
		findings = append(findings, Finding{
			ID:        nextID(),
			Code:      "dash_manifest_error",
			Severity:  "error",
			Message:   fmt.Sprintf("MPEG-DASH MPD 清单拉取失败（HTTP %d / %s）", manifest.Observation.HTTPStatus, manifest.Observation.EndReason),
			StartMS:   0,
			EndMS:     manifest.Observation.ElapsedMS,
			NextCheck: "检查 MPD 地址、鉴权 Header、CORS/代理配置或源站可用性。",
		})
	}
	if parseErr != "" {
		findings = append(findings, Finding{
			ID:        nextID(),
			Code:      "dash_manifest_parse_error",
			Severity:  "error",
			Message:   "MPD XML 解析失败：" + parseErr,
			StartMS:   0,
			EndMS:     manifest.Observation.ElapsedMS,
			NextCheck: "下载原始响应检查返回体是否为有效 ISO/IEC 23009-1 XML（而非错误页或跳转 HTML）。",
		})
	}
	if dm.ManifestsCaptured > 0 && parseErr == "" && dm.RepresentationsCount == 0 {
		findings = append(findings, Finding{
			ID:        nextID(),
			Code:      "dash_no_representations",
			Severity:  "error",
			Message:   "MPD 清单中未解析到任何 Representation 码率档位",
			StartMS:   0,
			EndMS:     manifest.Observation.ElapsedMS,
			NextCheck: "检查 Period / AdaptationSet 结构及打包器输出配置。",
		})
	}
	if dm.TimelineGaps > 0 {
		findings = append(findings, Finding{
			ID:          nextID(),
			Code:        "dash_timeline_gap",
			Severity:    "warn",
			Message:     fmt.Sprintf("SegmentTimeline 检测到 %d 处时间线跳变或空洞", dm.TimelineGaps),
			StartMS:     0,
			EndMS:       manifest.Observation.ElapsedMS,
			Occurrences: dm.TimelineGaps,
			NextCheck:   "核查切片编码器/打包器时间戳连续性与断流重推逻辑。",
		})
	}
	if dm.InitSegmentsFailed+dm.SegmentsFailed > 0 {
		sev := "warn"
		if dm.SegmentsCaptured == 0 {
			sev = "error"
		}
		findings = append(findings, Finding{
			ID:          nextID(),
			Code:        "dash_segment_http_error",
			Severity:    sev,
			Message:     fmt.Sprintf("DASH 分片拉取存在失败（初始化段失败 %d、媒体分片失败 %d）", dm.InitSegmentsFailed, dm.SegmentsFailed),
			StartMS:     0,
			EndMS:       manifest.Observation.ElapsedMS,
			Occurrences: dm.InitSegmentsFailed + dm.SegmentsFailed,
			NextCheck:   "检查 BaseURL / SegmentTemplate 路径拼接、CDN 404 回源或分片过早过期问题。",
		})
	}
	var slowCount int
	for _, seg := range dm.SampledSegments {
		if seg.Kind == "media" && seg.Error == "" && seg.DurationMS > 0 && seg.DownloadMS > seg.DurationMS {
			slowCount++
		}
	}
	if slowCount > 0 {
		findings = append(findings, Finding{
			ID:          nextID(),
			Code:        "dash_slow_segment",
			Severity:    "warn",
			Message:     fmt.Sprintf("有 %d 个分片下载耗时超过分片自身时长（峰值 %d ms），易导致缓冲耗尽卡顿", slowCount, dm.MaxSegmentDownloadMS),
			StartMS:     0,
			EndMS:       manifest.Observation.ElapsedMS,
			Occurrences: slowCount,
			NextCheck:   "排查 CDN 边缘吞吐、回源慢或当前码率档位过高问题。",
		})
	}
	if len(dm.DRMSystems) > 0 {
		findings = append(findings, Finding{
			ID:        nextID(),
			Code:      "dash_drm_protected",
			Severity:  "info",
			Message:   "清单声明了 DRM 内容保护：" + strings.Join(dm.DRMSystems, ", "),
			StartMS:   0,
			EndMS:     manifest.Observation.ElapsedMS,
			NextCheck: "加密分片仅能完成传输层与 MPD 结构排查，解密播放需配置对应许可证服务。",
		})
	}
	return findings
}

func walkDASHThroughput(dir string, fn func(kind string, rendition string, size int64, arrivalMS int64)) error {
	zr, f, err := openEvidenceZIP(filepath.Join(dir, dashStreamName))
	if err != nil {
		return err
	}
	defer f.Close()
	for _, zf := range zr.File {
		if zf.Name != dashMetaFileName {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		var meta dashArchiveMetadata
		err = json.NewDecoder(io.LimitReader(rc, 2<<20)).Decode(&meta)
		_ = rc.Close()
		if err != nil {
			return err
		}
		for _, seg := range meta.Metrics.SampledSegments {
			if seg.Error == "" && seg.Bytes > 0 {
				fn(hlsSegment, seg.ContentType, seg.Bytes, seg.ArrivalMS)
			}
		}
		return nil
	}
	return nil
}

func sanitizeDASHName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "rep"
	}
	return b.String()
}

func clampObservationStages(obs *Observation) {
	for i := range obs.Stages {
		if obs.Stages[i].EndMS > obs.ElapsedMS {
			obs.Stages[i].EndMS = obs.ElapsedMS
		}
		if obs.Stages[i].StartMS > obs.Stages[i].EndMS {
			obs.Stages[i].StartMS = obs.Stages[i].EndMS
		}
	}
}

var dashReportPage = template.Must(template.New("dash-report").Funcs(template.FuncMap{
	"join": func(xs []string) string {
		if len(xs) == 0 {
			return "无"
		}
		return strings.Join(xs, ", ")
	},
	"kbps": func(bps int64) string {
		return fmt.Sprintf("%.1f kbps", float64(bps)/1000.0)
	},
}).Parse(`<article><h2>MPEG-DASH 清单与分片指标</h2>
{{if .}}
<dl>
<dt>MPD 类型</dt><dd>{{if .MPDType}}{{.MPDType}}{{else}}未知{{end}}</dd>
<dt>Period / AdaptationSet / Representation</dt><dd>{{.Periods}} / {{.AdaptationSets}} / {{.RepresentationsCount}}</dd>
<dt>清单采集 / 失败</dt><dd>{{.ManifestsCaptured}} / {{.ManifestFailures}}</dd>
<dt>初始化段采集 / 失败</dt><dd>{{.InitSegmentsCaptured}} / {{.InitSegmentsFailed}}</dd>
<dt>媒体分片采集 / 失败</dt><dd>{{.SegmentsCaptured}} / {{.SegmentsFailed}}</dd>
<dt>SegmentTimeline 缺口</dt><dd>{{.TimelineGaps}}</dd>
<dt>最长分片下载耗时</dt><dd>{{if .SegmentsCaptured}}{{.MaxSegmentDownloadMS}} ms{{else}}未测得{{end}}</dd>
<dt>采集媒体数据</dt><dd>{{.MediaBytes}} 字节</dd>
<dt>DRM 保护</dt><dd>{{join .DRMSystems}}</dd>
</dl>
{{if .Representations}}
<h3>码率档位（Representations）</h3>
<div class="scroll"><table>
<thead><tr><th scope="col">ID</th><th scope="col">类型</th><th scope="col">码率</th><th scope="col">编码</th><th scope="col">分辨率 / 帧率</th><th scope="col">时间线缺口</th><th scope="col">采样</th></tr></thead>
<tbody>{{range .Representations}}<tr>
<td><code>{{.ID}}</code></td><td>{{.ContentType}}</td><td>{{kbps .Bandwidth}}</td><td><code>{{.Codecs}}</code></td>
<td>{{if and .Width .Height}}{{.Width}}×{{.Height}}{{else}}—{{end}}{{if .FrameRate}} @ {{.FrameRate}}{{end}}</td>
<td>{{.TimelineGaps}}</td><td>{{if .Sampled}}已采样{{else}}—{{end}}</td>
</tr>{{end}}</tbody>
</table></div>
{{end}}
{{if .SampledSegments}}
<h3>采样分片明细</h3>
<div class="scroll"><table>
<thead><tr><th scope="col">类型</th><th scope="col">档位</th><th scope="col">序号</th><th scope="col">HTTP</th><th scope="col">到达 (ms)</th><th scope="col">下载耗时 (ms)</th><th scope="col">字节</th><th scope="col">状态</th></tr></thead>
<tbody>{{range .SampledSegments}}<tr>
<td>{{.Kind}} ({{.ContentType}})</td><td><code>{{.RepresentationID}}</code></td><td>{{if .Number}}#{{.Number}}{{else}}init{{end}}</td>
<td>{{if .HTTPStatus}}{{.HTTPStatus}}{{else}}—{{end}}</td><td>{{.ArrivalMS}}</td><td>{{.DownloadMS}}</td><td>{{.Bytes}}</td>
<td>{{if .Error}}{{.Error}}{{else}}OK{{end}}</td>
</tr>{{end}}</tbody>
</table></div>
{{end}}
{{else}}<p class="timing-note">当前报告未包含 MPEG-DASH 指标。</p>{{end}}
</article>`))

func renderDASHHTML(report Report) (template.HTML, error) {
	if report.Manifest.Protocol != ProtocolDASH && report.DASH == nil {
		return "", nil
	}
	var out bytes.Buffer
	if err := dashReportPage.Execute(&out, report.DASH); err != nil {
		return "", err
	}
	return template.HTML(out.String()), nil
}
