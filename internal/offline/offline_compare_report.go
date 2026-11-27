package offline

import (
	"bytes"
	"fmt"
	"html"
	"math"
	"strings"
	"time"

	"streamanalyzer/internal/models"
)

// CompareTasks compares two finished offline tasks (and selected flow index on each side)
// and produces side-by-side parameter diffs, health-check deltas, and aligned chart profiles.
func (m *Manager) CompareTasks(taskIDA string, flowIdxA int, taskIDB string, flowIdxB int) (*models.OfflineCompareResult, error) {
	if !isSafeTaskID(taskIDA) || !isSafeTaskID(taskIDB) {
		return nil, fmt.Errorf("invalid task id")
	}
	taskA, sumA, err := m.loadTaskAndSummary(taskIDA)
	if err != nil {
		return nil, fmt.Errorf("任务 A 加载失败：%w", err)
	}
	taskB, sumB, err := m.loadTaskAndSummary(taskIDB)
	if err != nil {
		return nil, fmt.Errorf("任务 B 加载失败：%w", err)
	}
	flowA := pickFlow(sumA, flowIdxA)
	flowB := pickFlow(sumB, flowIdxB)
	if flowA == nil || flowB == nil {
		return nil, fmt.Errorf("至少有一个任务缺少可对比的媒体流结果")
	}
	profA := flowA.Profile
	if profA == nil {
		profA = &models.OfflineMediaProfile{}
	}
	profB := flowB.Profile
	if profB == nil {
		profB = &models.OfflineMediaProfile{}
	}

	res := &models.OfflineCompareResult{
		SideA: models.OfflineCompareSide{
			TaskID:     taskA.ID,
			InputName:  taskA.InputName,
			InputSize:  taskA.InputSize,
			Mode:       taskA.Mode,
			FlowIndex:  clampFlowIdx(sumA, flowIdxA),
			FlowLabel:  describeFlowLabel(flowA),
			VideoCodec: firstNonEmpty(profA.VideoCodec, flowA.VideoCodec),
			AudioCodec: firstNonEmpty(profA.AudioCodec, flowA.AudioCodec),
			Profile:    profA,
		},
		SideB: models.OfflineCompareSide{
			TaskID:     taskB.ID,
			InputName:  taskB.InputName,
			InputSize:  taskB.InputSize,
			Mode:       taskB.Mode,
			FlowIndex:  clampFlowIdx(sumB, flowIdxB),
			FlowLabel:  describeFlowLabel(flowB),
			VideoCodec: firstNonEmpty(profB.VideoCodec, flowB.VideoCodec),
			AudioCodec: firstNonEmpty(profB.AudioCodec, flowB.AudioCodec),
			Profile:    profB,
		},
	}

	addRow := func(cat, label, valA, valB, sev string) {
		changed := strings.TrimSpace(valA) != strings.TrimSpace(valB)
		if changed {
			res.ChangedCount++
		}
		if !changed {
			sev = ""
		}
		res.Rows = append(res.Rows, models.OfflineDiffRow{
			Category: cat,
			Label:    label,
			ValueA:   valA,
			ValueB:   valB,
			Changed:  changed,
			Severity: sev,
		})
	}

	addRow("基础", "封装模式", string(taskA.Mode), string(taskB.Mode), "")
	addRow("基础", "文件大小", fmtBytesHuman(taskA.InputSize), fmtBytesHuman(taskB.InputSize), "")
	addRow("基础", "媒体时长", fmtDurationMS(profA.DurationMS), fmtDurationMS(profB.DurationMS), diffSeverityPct(float64(profA.DurationMS), float64(profB.DurationMS), 0.10))

	addRow("视频", "编码格式", firstNonEmpty(profA.VideoCodec, flowA.VideoCodec, "-"), firstNonEmpty(profB.VideoCodec, flowB.VideoCodec, "-"), "warn")
	addRow("视频", "分辨率", fmtRes(profA.Width, profA.Height), fmtRes(profB.Width, profB.Height), "error")
	addRow("视频", "Profile / Level", fmtProfLevel(profA.Profile, profA.LevelName), fmtProfLevel(profB.Profile, profB.LevelName), "warn")
	addRow("视频", "平均帧率", fmtFloat(profA.FrameRate, "%.2f fps"), fmtFloat(profB.FrameRate, "%.2f fps"), diffSeverityPct(profA.FrameRate, profB.FrameRate, 0.10))
	addRow("视频", "视频平均码率", fmtFloat(profA.VideoBitrateKbps, "%.1f kbps"), fmtFloat(profB.VideoBitrateKbps, "%.1f kbps"), diffSeverityPct(profA.VideoBitrateKbps, profB.VideoBitrateKbps, 0.25))
	addRow("视频", "200ms 峰值码率 / 峰均比", fmtPeak(profA.Peak200msKbps, profA.PeakToAvgRatio), fmtPeak(profB.Peak200msKbps, profB.PeakToAvgRatio), "warn")
	addRow("视频", "视频帧数 / 关键帧", fmt.Sprintf("%d / %d", profA.VideoFrames, profA.Keyframes), fmt.Sprintf("%d / %d", profB.VideoFrames, profB.Keyframes), diffSeverityPct(float64(profA.VideoFrames), float64(profB.VideoFrames), 0.05))
	addRow("视频", "B 帧", fmtBFrames(profA.HasBFrames, profA.BFrames), fmtBFrames(profB.HasBFrames, profB.BFrames), "warn")
	addRow("视频", "GOP 均值 / 最大", fmtGOP(profA.GOPAvgMS, profA.GOPMaxMS), fmtGOP(profB.GOPAvgMS, profB.GOPMaxMS), diffSeverityPct(float64(profA.GOPAvgMS), float64(profB.GOPAvgMS), 0.20))
	addRow("视频", "ΔDTS 均值 / 最大", fmtDelta(profA.VideoDeltaAvgMS, profA.VideoDeltaMaxMS), fmtDelta(profB.VideoDeltaAvgMS, profB.VideoDeltaMaxMS), "warn")

	addRow("音频", "音频编码", firstNonEmpty(profA.AudioCodec, flowA.AudioCodec, "-"), firstNonEmpty(profB.AudioCodec, flowB.AudioCodec, "-"), "warn")
	addRow("音频", "采样率 / 声道", fmtAudioParams(profA.AudioSampleRate, profA.AudioChannels), fmtAudioParams(profB.AudioSampleRate, profB.AudioChannels), "warn")
	addRow("音频", "音频平均码率", fmtFloat(profA.AudioBitrateKbps, "%.1f kbps"), fmtFloat(profB.AudioBitrateKbps, "%.1f kbps"), diffSeverityPct(profA.AudioBitrateKbps, profB.AudioBitrateKbps, 0.25))
	addRow("音频", "音频帧数", fmt.Sprintf("%d", profA.AudioFrames), fmt.Sprintf("%d", profB.AudioFrames), "")

	addRow("体检", "DTS 回退 / 大间隔", fmt.Sprintf("%d / %d (最大 %d ms)", profA.DTSRegressions, profA.LargeGaps, profA.MaxGapMS), fmt.Sprintf("%d / %d (最大 %d ms)", profB.DTSRegressions, profB.LargeGaps, profB.MaxGapMS), "error")
	addRow("体检", "音画首帧偏移 / 最大漂移", fmtAVSync(profA.HasAVOffset, profA.AVStartOffsetMS, profA.AVDriftMaxMS), fmtAVSync(profB.HasAVOffset, profB.AVStartOffsetMS, profB.AVDriftMaxMS), "warn")
	addRow("体检", "告警总数", fmt.Sprintf("%d 条", len(profA.Warnings)), fmt.Sprintf("%d 条", len(profB.Warnings)), "warn")

	// Build concise summary notes
	if profA.Width != profB.Width || profA.Height != profB.Height {
		res.SummaryNotes = append(res.SummaryNotes, fmt.Sprintf("分辨率存在差异：A 为 %s，B 为 %s", fmtRes(profA.Width, profA.Height), fmtRes(profB.Width, profB.Height)))
	}
	if profA.HasBFrames != profB.HasBFrames {
		res.SummaryNotes = append(res.SummaryNotes, fmt.Sprintf("B 帧配置不同：A=%v，B=%v（可能经历重编码）", profA.HasBFrames, profB.HasBFrames))
	}
	if profA.VideoFrames > 0 && profB.VideoFrames > 0 {
		diffFrames := profB.VideoFrames - profA.VideoFrames
		if diffFrames != 0 {
			res.SummaryNotes = append(res.SummaryNotes, fmt.Sprintf("视频帧数差值（B - A）：%+d 帧", diffFrames))
		}
	}
	if profA.VideoBitrateKbps > 0 && profB.VideoBitrateKbps > 0 {
		pct := (profB.VideoBitrateKbps - profA.VideoBitrateKbps) / profA.VideoBitrateKbps * 100
		if math.Abs(pct) >= 15 {
			res.SummaryNotes = append(res.SummaryNotes, fmt.Sprintf("视频平均码率变化 %+.1f%%（%.1f -> %.1f kbps）", pct, profA.VideoBitrateKbps, profB.VideoBitrateKbps))
		}
	}
	if profB.DTSRegressions > profA.DTSRegressions || profB.LargeGaps > profA.LargeGaps {
		res.SummaryNotes = append(res.SummaryNotes, fmt.Sprintf("B 路时间戳异常多于 A 路（回退 %d vs %d，大间隔 %d vs %d）", profB.DTSRegressions, profA.DTSRegressions, profB.LargeGaps, profA.LargeGaps))
	}
	if len(res.SummaryNotes) == 0 {
		res.SummaryNotes = append(res.SummaryNotes, "两路媒体核心编码参数与时间轴指标基本一致")
	}

	return res, nil
}

// RenderMarkdownReport generates a clean Markdown diagnostic report suitable for issue trackers or docs.
func (m *Manager) RenderMarkdownReport(taskID string, flowIdx int) ([]byte, string, error) {
	task, summary, err := m.loadTaskAndSummary(taskID)
	if err != nil {
		return nil, "", err
	}
	flow := pickFlow(summary, flowIdx)
	if flow == nil {
		return nil, "", fmt.Errorf("未找到可导出的媒体流")
	}
	prof := flow.Profile
	if prof == nil {
		prof = &models.OfflineMediaProfile{}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Stream Analyzer 离线媒体诊断报告\n\n")
	fmt.Fprintf(&b, "- **文件名**：`%s` (%s)\n", task.InputName, fmtBytesHuman(task.InputSize))
	fmt.Fprintf(&b, "- **任务 ID**：`%s` · **模式**：`%s`", task.ID, task.Mode)
	if task.DetectedBy != "" {
		fmt.Fprintf(&b, " (%s)", task.DetectedBy)
	}
	fmt.Fprintf(&b, "\n- **导出时间**：%s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	fmt.Fprintf(&b, "## 1. 媒体概况\n\n")
	fmt.Fprintf(&b, "| 指标 | 数值 |\n| --- | --- |\n")
	fmt.Fprintf(&b, "| 视频编码 / Profile | %s %s |\n", firstNonEmpty(prof.VideoCodec, flow.VideoCodec, "-"), fmtProfLevel(prof.Profile, prof.LevelName))
	fmt.Fprintf(&b, "| 分辨率 / 帧率 | %s @ %s |\n", fmtRes(prof.Width, prof.Height), fmtFloat(prof.FrameRate, "%.2f fps"))
	fmt.Fprintf(&b, "| 时长 | %s |\n", fmtDurationMS(prof.DurationMS))
	fmt.Fprintf(&b, "| 视频码率（均值 / 200ms 峰值） | %s / %s |\n", fmtFloat(prof.VideoBitrateKbps, "%.1f kbps"), fmtPeak(prof.Peak200msKbps, prof.PeakToAvgRatio))
	fmt.Fprintf(&b, "| 视频帧 / 关键帧 / B 帧 | %d / %d / %d |\n", prof.VideoFrames, prof.Keyframes, prof.BFrames)
	fmt.Fprintf(&b, "| GOP（最小 / 均值 / 最大） | %d / %d / %d ms |\n", prof.GOPMinMS, prof.GOPAvgMS, prof.GOPMaxMS)
	fmt.Fprintf(&b, "| ΔDTS（最小 / 均值 / 最大） | %d / %d / %d ms |\n", prof.VideoDeltaMinMS, prof.VideoDeltaAvgMS, prof.VideoDeltaMaxMS)
	fmt.Fprintf(&b, "| 音频编码 / 参数 | %s · %s · %s |\n", firstNonEmpty(prof.AudioCodec, flow.AudioCodec, "-"), fmtAudioParams(prof.AudioSampleRate, prof.AudioChannels), fmtFloat(prof.AudioBitrateKbps, "%.1f kbps"))
	fmt.Fprintf(&b, "| 音画同步（首帧偏移 / 最大漂移） | %s |\n", fmtAVSync(prof.HasAVOffset, prof.AVStartOffsetMS, prof.AVDriftMaxMS))
	if prof.TSPacketCount > 0 {
		fmt.Fprintf(&b, "| TS 包统计 | 共 %d 包，CC 错误 %d，TEI %d，同步丢失 %d，PCR %d（最大间隔 %d ms） |\n",
			prof.TSPacketCount, prof.TSCCErrors, prof.TSTEIErrors, prof.TSSyncErrors, prof.PCRCount, prof.PCRMaxGapMS)
	}
	if flow.RTPStats != nil {
		rs := flow.RTPStats
		fmt.Fprintf(&b, "| RTP 统计 (SSRC 0x%08X) | PT=%d(%s)，收包 %d，丢包 %d (%.2f%%)，乱序 %d，Jitter %.2f ms (峰值 %.2f ms) |\n",
			rs.SSRC, rs.PayloadType, rs.PayloadName, rs.Packets, rs.Lost, rs.LossRatePct, rs.OutOfOrder, rs.JitterMS, rs.MaxJitterMS)
	}

	fmt.Fprintf(&b, "\n## 2. 深度体检规则\n\n")
	if len(prof.HealthChecks) == 0 {
		fmt.Fprintf(&b, "无深度体检条目。\n")
	} else {
		fmt.Fprintf(&b, "| 级别 | 类别 | 检查项 | 详情 | 首次位置 |\n| --- | --- | --- | --- | --- |\n")
		for _, hc := range prof.HealthChecks {
			loc := "-"
			if hc.FirstFrameIndex != nil {
				loc = fmt.Sprintf("帧 #%d (%d ms)", *hc.FirstFrameIndex+1, hc.FirstDTSMS)
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", hc.Severity, hc.Category, hc.Title, strings.ReplaceAll(hc.Detail, "|", "\\|"), loc)
		}
	}

	if len(prof.Warnings) > 0 {
		fmt.Fprintf(&b, "\n## 3. 告警摘要\n\n")
		for _, w := range prof.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}

	if len(flow.FrameDetails) > 0 {
		limit := len(flow.FrameDetails)
		if limit > 50 {
			limit = 50
		}
		fmt.Fprintf(&b, "\n## 4. 前 %d 帧样本明细\n\n", limit)
		fmt.Fprintf(&b, "| # | 类型 | 帧型 | DTS (ms) | PTS (ms) | CTS (ms) | 大小 (B) |\n| ---: | --- | --- | ---: | ---: | ---: | ---: |\n")
		for i := 0; i < limit; i++ {
			fd := flow.FrameDetails[i]
			fmt.Fprintf(&b, "| %d | %s | %s | %d | %d | %d | %d |\n",
				i+1, fd.MediaType, fd.FrameType, fd.DTS, fd.PTS, fd.CTS, fd.FrameLen)
		}
	}

	filename := fmt.Sprintf("offline-report-%s.md", task.ID)
	return []byte(b.String()), filename, nil
}

// RenderHTMLReport generates a self-contained single-file HTML report (zero external CDN deps)
// with inline SVG bitrate sparkline, parameter overview, health checks, and frame samples.
func (m *Manager) RenderHTMLReport(taskID string, flowIdx int) ([]byte, string, error) {
	task, summary, err := m.loadTaskAndSummary(taskID)
	if err != nil {
		return nil, "", err
	}
	flow := pickFlow(summary, flowIdx)
	if flow == nil {
		return nil, "", fmt.Errorf("未找到可导出的媒体流")
	}
	prof := flow.Profile
	if prof == nil {
		prof = &models.OfflineMediaProfile{}
	}

	esc := html.EscapeString
	var b bytes.Buffer
	fmt.Fprintf(&b, `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>离线媒体诊断报告 · %s</title>
<style>
  :root{--bg:#f8fafc;--card:#fff;--text:#0f172a;--muted:#64748b;--border:#e2e8f0;--ok:#047857;--warn:#b45309;--err:#b91c1c;--accent:#2563eb;}
  body{margin:0;padding:24px;background:var(--bg);color:var(--text);font:14px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;}
  .wrap{max-width:1100px;margin:0 auto;display:grid;gap:18px;}
  .card{background:var(--card);border:1px solid var(--border);border-radius:10px;padding:18px 20px;}
  h1{margin:0 0 6px;font-size:1.35rem;}
  h2{margin:0 0 12px;font-size:1.05rem;border-bottom:1px solid var(--border);padding-bottom:8px;}
  .meta{color:var(--muted);font-size:.86rem;display:flex;flex-wrap:wrap;gap:14px;}
  .grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(210px,1fr));gap:10px;}
  .stat{background:var(--bg);border:1px solid var(--border);border-radius:8px;padding:10px 12px;}
  .stat .k{font-size:.76rem;color:var(--muted);}
  .stat .v{font-size:1rem;font-weight:700;margin-top:2px;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;}
  table{width:100%%;border-collapse:collapse;font-size:.84rem;}
  th,td{text-align:left;padding:7px 9px;border-bottom:1px solid var(--border);vertical-align:top;}
  th{background:var(--bg);color:var(--muted);font-weight:600;}
  .pill{display:inline-block;padding:1px 8px;border-radius:999px;font-size:.74rem;font-weight:600;}
  .pill.ok{background:#d1fae5;color:var(--ok);}
  .pill.warn{background:#fef3c7;color:var(--warn);}
  .pill.error{background:#fee2e2;color:var(--err);}
  .mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;}
  svg{width:100%%;height:auto;display:block;}
</style>
</head>
<body>
<div class="wrap">
  <section class="card">
    <h1>Stream Analyzer 离线媒体诊断报告</h1>
    <div class="meta">
      <span><strong>文件：</strong><span class="mono">%s</span> (%s)</span>
      <span><strong>任务 ID：</strong><span class="mono">%s</span></span>
      <span><strong>模式：</strong><span class="mono">%s</span></span>
      <span><strong>生成时间：</strong>%s</span>
    </div>
  </section>
`, esc(task.InputName), esc(task.InputName), esc(fmtBytesHuman(task.InputSize)), esc(task.ID), esc(string(task.Mode)), time.Now().Format("2006-01-02 15:04:05"))

	// Overview stat cards
	fmt.Fprintf(&b, `<section class="card"><h2>媒体体检概况</h2><div class="grid">`)
	cards := [][2]string{
		{"视频编码", firstNonEmpty(prof.VideoCodec, flow.VideoCodec, "-") + " " + fmtProfLevel(prof.Profile, prof.LevelName)},
		{"分辨率 / 帧率", fmtRes(prof.Width, prof.Height) + " · " + fmtFloat(prof.FrameRate, "%.2f fps")},
		{"媒体时长", fmtDurationMS(prof.DurationMS)},
		{"平均 / 200ms 峰值码率", fmtFloat(prof.VideoBitrateKbps, "%.1f kbps") + " / " + fmtPeak(prof.Peak200msKbps, prof.PeakToAvgRatio)},
		{"视频帧 / 关键帧 / B 帧", fmt.Sprintf("%d / %d / %d", prof.VideoFrames, prof.Keyframes, prof.BFrames)},
		{"GOP 均值 / 最大", fmtGOP(prof.GOPAvgMS, prof.GOPMaxMS)},
		{"ΔDTS 均值 / 最大", fmtDelta(prof.VideoDeltaAvgMS, prof.VideoDeltaMaxMS)},
		{"音频编码 / 参数", firstNonEmpty(prof.AudioCodec, flow.AudioCodec, "-") + " · " + fmtAudioParams(prof.AudioSampleRate, prof.AudioChannels)},
		{"音画偏移 / 最大漂移", fmtAVSync(prof.HasAVOffset, prof.AVStartOffsetMS, prof.AVDriftMaxMS)},
		{"DTS 回退 / 大间隔", fmt.Sprintf("%d / %d", prof.DTSRegressions, prof.LargeGaps)},
	}
	for _, c := range cards {
		fmt.Fprintf(&b, `<div class="stat"><div class="k">%s</div><div class="v">%s</div></div>`, esc(c[0]), esc(strings.TrimSpace(c[1])))
	}
	fmt.Fprintf(&b, `</div></section>`)

	// Inline SVG Bitrate chart
	if len(prof.BitrateTimeline) >= 2 {
		fmt.Fprintf(&b, `<section class="card"><h2>瞬时码率趋势图 (kbps)</h2>%s</section>`, renderBitrateSVG(prof.BitrateTimeline))
	}

	// RTP Stats if present
	if flow.RTPStats != nil {
		rs := flow.RTPStats
		fmt.Fprintf(&b, `<section class="card"><h2>RTP / RTCP 传输统计</h2><div class="grid">`+
			`<div class="stat"><div class="k">SSRC / PayloadType</div><div class="v">0x%08X / PT %d (%s)</div></div>`+
			`<div class="stat"><div class="k">实收 / 预期包数</div><div class="v">%d / %d</div></div>`+
			`<div class="stat"><div class="k">丢包数 / 丢包率</div><div class="v">%d (%.2f%%)</div></div>`+
			`<div class="stat"><div class="k">乱序 / 重复包</div><div class="v">%d / %d</div></div>`+
			`<div class="stat"><div class="k">RFC 3550 Jitter（均值 / 峰值）</div><div class="v">%.2f / %.2f ms</div></div>`+
			`</div></section>`,
			rs.SSRC, rs.PayloadType, esc(rs.PayloadName), rs.Packets, rs.Expected, rs.Lost, rs.LossRatePct, rs.OutOfOrder, rs.Duplicates, rs.JitterMS, rs.MaxJitterMS)
	}

	// Health checks table
	if len(prof.HealthChecks) > 0 {
		fmt.Fprintf(&b, `<section class="card"><h2>深度体检规则</h2><table><thead><tr><th>级别</th><th>类别</th><th>规则项</th><th>详情</th><th>首次位置</th></tr></thead><tbody>`)
		for _, hc := range prof.HealthChecks {
			loc := "—"
			if hc.FirstFrameIndex != nil {
				loc = fmt.Sprintf("帧 #%d (%d ms)", *hc.FirstFrameIndex+1, hc.FirstDTSMS)
			}
			fmt.Fprintf(&b, `<tr><td><span class="pill %s">%s</span></td><td>%s</td><td><strong>%s</strong></td><td>%s</td><td class="mono">%s</td></tr>`,
				esc(hc.Severity), esc(hc.Severity), esc(hc.Category), esc(hc.Title), esc(hc.Detail), esc(loc))
		}
		fmt.Fprintf(&b, `</tbody></table></section>`)
	}

	// Frame sample table (first 150 frames)
	if len(flow.FrameDetails) > 0 {
		limit := len(flow.FrameDetails)
		if limit > 150 {
			limit = 150
		}
		fmt.Fprintf(&b, `<section class="card"><h2>逐帧样本明细（前 %d / 共 %d 帧）</h2><table><thead><tr><th>#</th><th>媒体</th><th>帧类型</th><th>DTS (ms)</th><th>PTS (ms)</th><th>CTS (ms)</th><th>大小 (B)</th></tr></thead><tbody>`, limit, len(flow.FrameDetails))
		for i := 0; i < limit; i++ {
			fd := flow.FrameDetails[i]
			fmt.Fprintf(&b, `<tr><td class="mono">%d</td><td>%s</td><td>%s</td><td class="mono">%d</td><td class="mono">%d</td><td class="mono">%d</td><td class="mono">%d</td></tr>`,
				i+1, esc(fd.MediaType), esc(fd.FrameType), fd.DTS, fd.PTS, fd.CTS, fd.FrameLen)
		}
		fmt.Fprintf(&b, `</tbody></table></section>`)
	}

	fmt.Fprintf(&b, `</div></body></html>`)
	filename := fmt.Sprintf("offline-report-%s.html", task.ID)
	return b.Bytes(), filename, nil
}

func renderBitrateSVG(pts []models.OfflineBitratePoint) string {
	const w, h, padL, padR, padT, padB = 960.0, 210.0, 54.0, 16.0, 16.0, 28.0
	maxV := 10.0
	for _, p := range pts {
		if p.VideoKbps > maxV {
			maxV = p.VideoKbps
		}
		if p.AudioKbps > maxV {
			maxV = p.AudioKbps
		}
	}
	plotW := w - padL - padR
	plotH := h - padT - padB
	var vLine, aLine strings.Builder
	for i, p := range pts {
		x := padL + float64(i)*plotW/float64(len(pts)-1)
		yv := padT + plotH*(1.0-p.VideoKbps/maxV)
		ya := padT + plotH*(1.0-p.AudioKbps/maxV)
		if i > 0 {
			vLine.WriteByte(' ')
			aLine.WriteByte(' ')
		}
		fmt.Fprintf(&vLine, "%.1f,%.1f", x, yv)
		fmt.Fprintf(&aLine, "%.1f,%.1f", x, ya)
	}
	return fmt.Sprintf(
		`<svg viewBox="0 0 %.0f %.0f" role="img" aria-label="瞬时码率趋势">`+
			`<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="#cbd5e1"/>`+
			`<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="#cbd5e1"/>`+
			`<text x="6" y="%.0f" fill="#64748b" font-size="11">%.0f</text>`+
			`<text x="6" y="%.0f" fill="#64748b" font-size="11">0</text>`+
			`<polyline fill="none" stroke="#2563eb" stroke-width="2" points="%s"/>`+
			`<polyline fill="none" stroke="#10b981" stroke-width="1.5" stroke-dasharray="4 3" points="%s"/>`+
			`<text x="%.0f" y="%.0f" fill="#2563eb" font-size="11">— 视频 kbps</text>`+
			`<text x="%.0f" y="%.0f" fill="#10b981" font-size="11">-- 音频 kbps</text>`+
			`</svg>`,
		w, h,
		padL, padT, padL, padT+plotH,
		padL, padT+plotH, padL+plotW, padT+plotH,
		padT+10, maxV,
		padT+plotH,
		vLine.String(),
		aLine.String(),
		padL+8, h-6,
		padL+105, h-6,
	)
}

func pickFlow(sum *models.OfflineSummary, idx int) *models.OfflineFlowResult {
	if sum == nil || len(sum.Flows) == 0 {
		return nil
	}
	if idx < 0 || idx >= len(sum.Flows) {
		idx = 0
	}
	return &sum.Flows[idx]
}

func clampFlowIdx(sum *models.OfflineSummary, idx int) int {
	if sum == nil || len(sum.Flows) == 0 || idx < 0 || idx >= len(sum.Flows) {
		return 0
	}
	return idx
}

func describeFlowLabel(f *models.OfflineFlowResult) string {
	if f == nil {
		return ""
	}
	if f.Protocol != "" {
		return fmt.Sprintf("Flow #%d (%s)", f.FlowID, strings.ToUpper(f.Protocol))
	}
	return fmt.Sprintf("Flow #%d (%s)", f.FlowID, f.Direction)
}

func fmtBytesHuman(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	if b < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(b)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(b)/(1024*1024*1024))
}

func fmtDurationMS(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f s", float64(ms)/1000.0)
}

func fmtRes(w, h int) string {
	if w <= 0 || h <= 0 {
		return "—"
	}
	return fmt.Sprintf("%dx%d", w, h)
}

func fmtProfLevel(prof, lvl string) string {
	s := strings.TrimSpace(strings.TrimSpace(prof) + " " + strings.TrimSpace(lvl))
	if s == "" {
		return "—"
	}
	return s
}

func fmtFloat(v float64, layout string) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf(layout, v)
}

func fmtPeak(peak, ratio float64) string {
	if peak <= 0 {
		return "—"
	}
	if ratio > 0 {
		return fmt.Sprintf("%.0f kbps (%.1fx)", peak, ratio)
	}
	return fmt.Sprintf("%.0f kbps", peak)
}

func fmtBFrames(has bool, cnt int) string {
	if !has && cnt == 0 {
		return "无"
	}
	return fmt.Sprintf("有 (%d 帧)", cnt)
}

func fmtGOP(avg, max int64) string {
	if avg <= 0 && max <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d / %d ms", avg, max)
}

func fmtDelta(avg, max int64) string {
	if avg <= 0 && max <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d / %d ms", avg, max)
}

func fmtAudioParams(sr, ch int) string {
	if sr <= 0 && ch <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d Hz / %d ch", sr, ch)
}

func fmtAVSync(has bool, off, drift int64) string {
	if !has {
		return "—"
	}
	return fmt.Sprintf("%+d ms / %d ms", off, drift)
}

func diffSeverityPct(a, b, warnThreshold float64) string {
	if a <= 0 && b <= 0 {
		return ""
	}
	base := math.Max(math.Abs(a), math.Abs(b))
	if base == 0 {
		return ""
	}
	if math.Abs(a-b)/base >= warnThreshold {
		return "warn"
	}
	return ""
}
