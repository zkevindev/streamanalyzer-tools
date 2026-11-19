package diagnostic

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// This published v1 input audit must remain readable when the live model input
// projection evolves. It was captured from a real browser RTSP stall, after the
// source had stopped, using the exact saved screenshots in the model request.
func TestReportModelPublishedVisionV1BundleRemainsUnchanged(t *testing.T) {
	data, err := os.ReadFile("testdata/report-vision-v1.zip")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, entry := range archive.File {
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name] = content
	}
	var expectedReport Report
	var expectedExplanation Explanation
	if err := json.Unmarshal(files[reportName], &expectedReport); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(files[explanationName], &expectedExplanation); err != nil {
		t.Fatal(err)
	}
	audit := expectedExplanation.ReportInputEvidence
	if audit == nil || audit.Version != 1 || audit.Mode != "vision" || len(audit.Images) != 3 || len(audit.Cases) != 1 || len(expectedReport.StallInvestigations) != 1 || len(expectedExplanation.Hypotheses) != 2 || expectedExplanation.Hypotheses[0].Priority != "P1" || expectedExplanation.Hypotheses[1].Priority != "P3" {
		t.Fatal("published report-vision-v1 fixture is incomplete")
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
		t.Fatal("import rewrote the published report, image audit or priority")
	}
	names := []string{rtspStreamName, timelineName, playerName, investigationName}
	for _, asset := range expectedReport.StallInvestigations[0].Media.Assets {
		names = append(names, asset.Name)
	}
	for _, name := range names {
		saved, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(saved, files[name]) {
			t.Fatalf("published v1 evidence changed: %s (%v)", name, err)
		}
	}
}
