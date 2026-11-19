package diagnostic

type automaticInvestigationHTML struct {
	Automatic                *AutomaticInvestigation
	StateLabel, SummaryLabel string
	PlanKnown, HasSummary    bool
	Rows                     []automaticSelectionHTML
}
type automaticSelectionHTML struct {
	Title, CaseID, CaseHref, MediaLabel string
	StartMS, EndMS, OverlapMS           int64
}

func automaticInvestigationView(report Report, summary *Explanation) *automaticInvestigationHTML {
	automatic := report.AutomaticInvestigation
	if automatic == nil {
		return nil
	}
	view := &automaticInvestigationHTML{Automatic: automatic, StateLabel: automaticStateLabel(automatic.State), SummaryLabel: automaticSummaryLabel(automatic.ModelState), HasSummary: summary != nil}
	view.PlanKnown = automatic.CandidateCount > 0 || len(automatic.Selected) > 0
	if automatic.PlayerRevision > 0 {
		switch automatic.State {
		case "investigating", "summarizing", "complete", "partial", "unconfigured":
			view.PlanKnown = true
		}
	}
	for _, selection := range automatic.Selected[:min(3, len(automatic.Selected))] {
		kind := selection.IntervalKind
		if kind == "" {
			kind = IntervalBuffering
		}
		row := automaticSelectionHTML{Title: playerInvestigationIntervalTitle(StallInvestigation{IntervalKind: kind, StallIndex: selection.IntervalIndex}), CaseID: selection.CaseID, MediaLabel: "尚未创建案例", StartMS: selection.Interval.StartMS, EndMS: selection.Interval.EndMS}
		row.OverlapMS = max(int64(0), min(row.EndMS, report.Manifest.Observation.ElapsedMS)-max(int64(0), row.StartMS))
		for _, item := range report.StallInvestigations {
			if item.ID != selection.CaseID {
				continue
			}
			row.CaseHref = "#investigation-" + item.ID
			row.MediaLabel = investigationMediaLabel(item.Media.Status)
			if item.Status == "queued" || item.Status == "running" {
				row.MediaLabel = "等待媒体提取"
			}
			break
		}
		view.Rows = append(view.Rows, row)
	}
	return view
}

func automaticStateLabel(state string) string {
	switch state {
	case "waiting_player":
		return "等待播放器记录完成"
	case "queued":
		return "等待处理"
	case "investigating":
		return "正在提取异常证据"
	case "summarizing":
		return "正在生成整体汇总"
	case "complete":
		return "自动流程完成"
	case "partial":
		return "保留部分结果"
	case "unconfigured":
		return "媒体补查已完成，模型尚未配置"
	case "failed":
		return "自动流程未完成"
	case "canceled":
		return "自动流程已停止"
	case "interrupted":
		return "任务因服务中断停止"
	default:
		return "未记录阶段"
	}
}

func automaticSummaryLabel(state string) string {
	switch state {
	case "pending":
		return "尚未生成"
	case "complete":
		return "已生成"
	case "unconfigured":
		return "未配置模型"
	case "failed":
		return "生成失败"
	case "canceled":
		return "已停止"
	case "not_requested":
		return "尚未请求"
	default:
		return "尚未记录"
	}
}
