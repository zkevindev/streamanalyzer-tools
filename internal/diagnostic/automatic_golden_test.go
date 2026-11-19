package diagnostic

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Captured through the real desktop browser flow: one RTSP sampling PLAY,
// overlapping observed buffering/presentation intervals, automatic evidence
// extraction, and one overall image request after the source was stopped.
func TestAutomaticPublishedV1BundleRemainsUnchanged(t *testing.T) {
	data, err := os.ReadFile("testdata/automatic-v1.zip")
	if err != nil {
		t.Fatal(err)
	}
	var expectedReport Report
	var expectedExplanation Explanation
	var expectedAutomatic AutomaticInvestigation
	if err := json.Unmarshal(reportExplanationZIPEntry(t, data, reportName), &expectedReport); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reportExplanationZIPEntry(t, data, explanationName), &expectedExplanation); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reportExplanationZIPEntry(t, data, automaticInvestigationName), &expectedAutomatic); err != nil {
		t.Fatal(err)
	}
	if expectedAutomatic.Version != 1 || expectedAutomatic.Policy != automaticPolicy || expectedAutomatic.State != "complete" || expectedAutomatic.ModelState != "complete" || expectedAutomatic.CandidateCount != 2 || len(expectedAutomatic.Selected) != 1 || expectedAutomatic.Selected[0].CaseID != "S001" || len(expectedReport.StallInvestigations) != 1 || expectedReport.StallInvestigations[0].ModelState != "not_requested" || expectedReport.StallInvestigations[0].Explanation != nil || len(expectedReport.StallInvestigations[0].Media.Assets) != 4 || expectedExplanation.ReportInputEvidence == nil || expectedExplanation.ReportInputEvidence.Mode != "vision" || len(expectedExplanation.ReportInputEvidence.Images) != 3 || len(expectedExplanation.Hypotheses) == 0 || expectedExplanation.Hypotheses[0].Priority != "P1" {
		t.Fatal("actual automatic-v1 fixture is incomplete")
	}
	t.Setenv("PATH", "")
	dir := t.TempDir()
	actualReport, err := readBundle(dir, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	actualExplanation, err := readJSONFile[Explanation](dir, explanationName)
	if err != nil {
		t.Fatal(err)
	}
	expectedReportJSON, _ := json.Marshal(expectedReport)
	actualReportJSON, _ := json.Marshal(actualReport)
	expectedExplanationJSON, _ := json.Marshal(expectedExplanation)
	actualExplanationJSON, _ := json.Marshal(actualExplanation)
	if !bytes.Equal(expectedReportJSON, actualReportJSON) || !bytes.Equal(expectedExplanationJSON, actualExplanationJSON) {
		t.Fatal("import rewrote automatic selection, source proof or overall conclusion")
	}
	names := []string{rtspStreamName, timelineName, playerName, investigationName, automaticInvestigationName}
	for _, asset := range expectedReport.StallInvestigations[0].Media.Assets {
		names = append(names, asset.Name)
	}
	for _, name := range names {
		saved, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(saved, reportExplanationZIPEntry(t, data, name)) {
			t.Fatalf("published automatic-v1 evidence changed: %s (%v)", name, err)
		}
	}
}
