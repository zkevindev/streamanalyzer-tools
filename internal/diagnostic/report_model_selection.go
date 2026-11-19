package diagnostic

import (
	"fmt"
	"path/filepath"
	"sort"
)

const reportModelSelection = "critical_stalls_v1"

func reportModelPlayerRevision(report Report) int {
	if report.Player == nil {
		return 0
	}
	return report.Player.Revision
}
func rankedReportModelCases(report Report) []StallInvestigation {
	cases := make([]StallInvestigation, 0, len(report.StallInvestigations))
	if report.Player == nil {
		return cases
	}
	for _, item := range report.StallInvestigations {
		if item.PlayerRevision == report.Player.Revision && investigationMediaID.MatchString(item.ID) {
			cases = append(cases, item)
		}
	}
	severity := func(item StallInvestigation) int {
		maximum := 0
		if item.Assessment != nil {
			for _, finding := range item.Assessment.Findings {
				value := 0
				switch finding.Severity {
				case "error":
					value = 3
				case "warn", "warning":
					value = 2
				case "info":
					value = 1
				}
				maximum = max(maximum, value)
			}
		}
		return maximum
	}
	sort.SliceStable(cases, func(i, j int) bool {
		a, b := cases[i], cases[j]
		if severity(a) != severity(b) {
			return severity(a) > severity(b)
		}
		if a.Stall.DurationMS != b.Stall.DurationMS {
			return a.Stall.DurationMS > b.Stall.DurationMS
		}
		if a.Stall.StartMS != b.Stall.StartMS {
			return a.Stall.StartMS < b.Stall.StartMS
		}
		return a.ID < b.ID
	})
	return cases
}
func reportModelCaseReferences(report Report) []ReportModelCaseReference {
	refs := make([]ReportModelCaseReference, 0, len(report.StallInvestigations))
	for _, item := range rankedReportModelCases(report) {
		refs = append(refs, ReportModelCaseReference{CaseID: item.ID, PlayerRevision: item.PlayerRevision})
	}
	return refs
}

// Cover distinct important cases before adding before/after pictures from the
// same cases. SHA deduplication conserves the three-image network budget.
func selectReportModelImages(dir string, report Report) ([]ModelImage, []ReportModelCaseReference, int, error) {
	cases := rankedReportModelCases(report)
	refs := reportModelCaseReferences(report)
	selected := make([]ModelImage, 0, MaxModelImages)
	type imageCase struct{ images []ModelImage }
	var chosen []imageCase
	hashes := map[string]bool{}
	invalid := false
	for _, item := range cases {
		images, err := loadStallModelImages(dir, item)
		if err != nil {
			invalid = true
			continue
		}
		if len(images) == 0 {
			continue
		}
		candidates := append([]ModelImage(nil), images...)
		sort.SliceStable(candidates, func(i, j int) bool {
			return filepath.Base(candidates[i].Reference.Name) == "during.jpg" && filepath.Base(candidates[j].Reference.Name) != "during.jpg"
		})
		for _, image := range candidates {
			if hashes[image.Reference.SHA256] {
				continue
			}
			selected = append(selected, image)
			hashes[image.Reference.SHA256] = true
			chosen = append(chosen, imageCase{images: images})
			break
		}
		if len(selected) == MaxModelImages {
			break
		}
	}
	for _, item := range chosen {
		for _, image := range item.images {
			if len(selected) == MaxModelImages {
				break
			}
			if hashes[image.Reference.SHA256] {
				continue
			}
			selected = append(selected, image)
			hashes[image.Reference.SHA256] = true
		}
	}
	omitted := len(refs) - len(chosen)
	if len(selected) == 0 && invalid {
		return selected, refs, 0, fmt.Errorf("%w: 未找到通过校验的补查截图", ErrInvalid)
	}
	if len(selected) > 0 {
		if err := validateModelImagesForReport(report, selected); err != nil {
			return nil, refs, 0, err
		}
	} else {
		omitted = 0
	}
	return selected, refs, omitted, nil
}
