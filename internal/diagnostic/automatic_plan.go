package diagnostic

import "sort"

// Selection uses only observed, positive intervals that overlap the saved
// capture. Ranking by overlap avoids prioritising a long tail after capture.
// A callback gap that overlaps an already selected buffering stall is not a
// second automatic job, and selection never adds to the player's stall count.
func planAutomaticInvestigation(report Report) automaticPlan {
	plan := automaticPlan{Selected: []AutomaticSelection{}}
	if report.Player == nil || report.Manifest.Observation.ElapsedMS <= 0 {
		return plan
	}
	var candidates []AutomaticSelection
	add := func(kind string, index int, interval PlayerStall) {
		if interval.EndMS <= interval.StartMS || interval.DurationMS != interval.EndMS-interval.StartMS {
			return
		}
		if min(interval.EndMS, report.Manifest.Observation.ElapsedMS) <= max(0, interval.StartMS) {
			return
		}
		candidates = append(candidates, AutomaticSelection{IntervalKind: kind, IntervalIndex: index, Interval: interval})
	}
	for index, stall := range report.Player.Stalls {
		add(IntervalBuffering, index+1, stall)
	}
	if report.Player.Presentation != nil {
		for index, gap := range report.Player.Presentation.Gaps {
			add(IntervalPresentationGap, index+1, PlayerStall{StartMS: gap.StartMS, EndMS: gap.EndMS, DurationMS: gap.DurationMS})
		}
	}
	plan.CandidateCount = len(candidates)
	overlap := func(item AutomaticSelection) int64 {
		return min(item.Interval.EndMS, report.Manifest.Observation.ElapsedMS) - max(0, item.Interval.StartMS)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if overlap(a) != overlap(b) {
			return overlap(a) > overlap(b)
		}
		if a.IntervalKind != b.IntervalKind {
			return a.IntervalKind == IntervalBuffering
		}
		if a.Interval.StartMS != b.Interval.StartMS {
			return a.Interval.StartMS < b.Interval.StartMS
		}
		return a.IntervalIndex < b.IntervalIndex
	})
	for _, candidate := range candidates {
		overlaps := false
		for _, selected := range plan.Selected {
			if min(selected.Interval.EndMS, candidate.Interval.EndMS) > max(selected.Interval.StartMS, candidate.Interval.StartMS) {
				overlaps = true
				break
			}
		}
		if !overlaps {
			plan.Selected = append(plan.Selected, candidate)
		}
		if len(plan.Selected) == maxAutomaticInvestigations {
			break
		}
	}
	return plan
}
