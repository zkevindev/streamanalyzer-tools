package diagnostic

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func shareTestArtifact(t *testing.T, dir, name string, data []byte) Artifact {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := newArtifact(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := file.close(); err != nil {
		t.Fatal(err)
	}
	return file.artifact()
}

func shareTestService(t *testing.T) (*Service, string, Report) {
	t.Helper()
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	run := newRun("sample", ProtocolHTTPFLV, "https://example.test")
	run.Status, run.ReportReady = "done", true
	if err := os.Mkdir(service.runDir(run.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := service.insertRun(run); err != nil {
		t.Fatal(err)
	}
	raw := []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0}
	clip := []byte("selected diagnostic clip")
	stream := shareTestArtifact(t, service.runDir(run.ID), streamName, raw)
	asset := DerivedArtifact{Name: "stalls/S001/clip.mp4", Kind: "clip", Artifact: shareTestArtifact(t, service.runDir(run.ID), "stalls/S001/clip.mp4", clip)}
	report := Report{RuleVersion: "http-flv/2", Manifest: Manifest{Version: investigationSchemaVersion, Protocol: ProtocolHTTPFLV,
		Observation: Observation{Origin: run.Origin, StartedAt: run.CreatedAt, RequestedSeconds: 1, ElapsedMS: 1000, EndReason: "eof"}, Stream: stream,
		Timeline: shareTestArtifact(t, service.runDir(run.ID), timelineName, nil)},
		Findings: []Finding{}, StallInvestigations: []StallInvestigation{{ID: "S001", CreatedAt: time.Now(), UpdatedAt: time.Now(), Status: "complete", Media: InvestigationMedia{Status: "ready", Assets: []DerivedArtifact{asset}}}}}
	if err := persistReport(service.runDir(run.ID), report); err != nil {
		t.Fatal(err)
	}
	return service, run.ID, report
}

func TestMinimalShareBundleContainsOnlySelectedArtifacts(t *testing.T) {
	service, id, _ := shareTestService(t)
	defer service.Close()
	var output bytes.Buffer
	if err := service.WriteShareBundle(id, ShareRequest{CaseIDs: []string{"S001"}}, &output); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	var manifest ShareManifest
	var report Report
	for _, entry := range archive.File {
		names = append(names, entry.Name)
		reader, _ := entry.Open()
		data, _ := io.ReadAll(reader)
		reader.Close()
		switch entry.Name {
		case "share-manifest.json":
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
		case reportName:
			if err := json.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
		case "stalls/S001/clip.mp4":
			if string(data) != "selected diagnostic clip" {
				t.Fatal("selected clip content changed")
			}
		}
	}
	if !reflect.DeepEqual(names, []string{reportName, htmlName, "stalls/S001/clip.mp4", "share-manifest.json"}) {
		t.Fatalf("unexpected share entries: %v", names)
	}
	if manifest.OfflineReplay || manifest.IncludeStream || len(manifest.Artifacts) != 3 || report.Manifest.Stream.Bytes != 0 ||
		report.Manifest.Timeline.Bytes != 0 || len(report.StallInvestigations) != 1 {
		t.Fatalf("minimal share retained full evidence references: %+v %+v", manifest, report.Manifest)
	}
}

func TestShareBundleRawStreamIsExplicitAndVerified(t *testing.T) {
	service, id, _ := shareTestService(t)
	defer service.Close()
	var output bytes.Buffer
	if err := service.WriteShareBundle(id, ShareRequest{IncludeStream: true}, &output); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range archive.File {
		if entry.Name == streamName {
			found = true
		}
	}
	if !found {
		t.Fatal("explicit raw stream was omitted")
	}
	if err := os.WriteFile(filepath.Join(service.runDir(id), streamName), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := service.WriteShareBundle(id, ShareRequest{IncludeStream: true}, &output); err == nil {
		t.Fatal("share bundle accepted a changed raw stream")
	}
	if err := service.WriteShareBundle(id, ShareRequest{CaseIDs: []string{"S999"}}, io.Discard); err == nil {
		t.Fatal("share bundle accepted an unknown case")
	}
}
