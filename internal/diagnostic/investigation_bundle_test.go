package diagnostic

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func TestStallEvidenceImportRejectsUnverifiedEmptyFindingsMetadata(t *testing.T) {
	service, id, _ := openStallServiceTestRun(t, nil, false)
	service.investigationSlots <- struct{}{}
	service.investigationSlots <- struct{}{}
	defer func() { <-service.investigationSlots; <-service.investigationSlots }()
	if _, err := service.InvestigateStall(id, StallInvestigationRequest{StallIndex: 1}); err != nil {
		t.Fatal(err)
	}
	var evidence bytes.Buffer
	if err := service.WriteBundle(id, &evidence); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(evidence.Bytes()), int64(evidence.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, entry := range reader.File {
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name] = data
	}
	var ledger investigationLedger
	if err := json.Unmarshal(files[investigationName], &ledger); err != nil {
		t.Fatal(err)
	}
	ledger.Items[0].Correlation.Notes = []string{"unverified-source-url?token=private"}
	files[investigationName], err = json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(files[manifestName], &manifest); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(files[investigationName])
	manifest.StallEvidence = &Artifact{Bytes: int64(len(files[investigationName])), SHA256: hex.EncodeToString(hash[:])}
	files[manifestName], err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var modified bytes.Buffer
	writer := zip.NewWriter(&modified)
	for name, data := range files {
		target, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := target.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Import(bytes.NewReader(modified.Bytes())); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unverified supplementary metadata accepted: %v", err)
	}
	if err := service.StopStall(id, "S001"); err != nil {
		t.Fatal(err)
	}
	awaitStallTestItem(t, service, id)
}
