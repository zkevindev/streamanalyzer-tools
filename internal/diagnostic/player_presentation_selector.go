package diagnostic

import "fmt"

const (
	IntervalBuffering       = "buffering"
	IntervalPresentationGap = "presentation_gap"
)

func normalizePlayerIntervalKind(kind string) (string, error) {
	if kind == "" {
		return IntervalBuffering, nil
	}
	if kind != IntervalBuffering && kind != IntervalPresentationGap {
		return "", ErrInvalid
	}
	return kind, nil
}

func normalizePlayerIntervalRequest(request StallInvestigationRequest) (string, int, error) {
	kind, err := normalizePlayerIntervalKind(request.IntervalKind)
	if err != nil || request.StallIndex < 0 || request.IntervalIndex < 0 {
		return "", 0, ErrInvalid
	}
	if kind == IntervalPresentationGap {
		if request.StallIndex > 0 || request.IntervalIndex < 1 {
			return "", 0, ErrInvalid
		}
		return kind, request.IntervalIndex, nil
	}
	if request.StallIndex > 0 && request.IntervalIndex > 0 && request.StallIndex != request.IntervalIndex {
		return "", 0, ErrInvalid
	}
	index := request.IntervalIndex
	if index == 0 {
		index = request.StallIndex
	}
	if index < 1 {
		return "", 0, ErrInvalid
	}
	return kind, index, nil
}

func resolvePlayerInterval(report Report, kind string, index int) (PlayerStall, error) {
	kind, err := normalizePlayerIntervalKind(kind)
	if err != nil || report.Player == nil || index < 1 {
		return PlayerStall{}, fmt.Errorf("%w: 请选择实际观测到的播放器异常区间", ErrInvalid)
	}
	if kind == IntervalBuffering {
		if index > len(report.Player.Stalls) {
			return PlayerStall{}, ErrInvalid
		}
		return report.Player.Stalls[index-1], nil
	}
	if report.Player.Presentation == nil || index > len(report.Player.Presentation.Gaps) {
		return PlayerStall{}, ErrInvalid
	}
	gap := report.Player.Presentation.Gaps[index-1]
	return PlayerStall{StartMS: gap.StartMS, EndMS: gap.EndMS, DurationMS: gap.DurationMS}, nil
}
