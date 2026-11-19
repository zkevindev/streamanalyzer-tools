package diagnostic

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"
)

// This bundle was collected by the unchanged published version, with a real
// browser stall, one source PLAY, and its original unsupported media result.
// A new offline decoder must not silently replace that historical result.
func TestRTSPStallPublishedLedgerOneRemainsExact(t *testing.T) {
	data, err := os.ReadFile("testdata/rtsp-v4-legacy-stall.zip")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var expected Report
	var ledger []byte
	for _, entry := range archive.File {
		if entry.Name != reportName && entry.Name != investigationName {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name == reportName {
			if err := json.Unmarshal(content, &expected); err != nil {
				t.Fatal(err)
			}
		} else {
			ledger = content
		}
	}
	if expected.RTSP == nil || expected.Player == nil || expected.Player.StallCount < 1 || len(expected.StallInvestigations) != 1 || expected.StallInvestigations[0].Media.Status != "unavailable" || expected.StallInvestigations[0].Media.RTSP != nil {
		t.Fatal("published RTSP stall fixture is incomplete")
	}
	dir := t.TempDir()
	actual, err := readBundle(dir, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(expected)
	after, _ := json.Marshal(actual)
	if !bytes.Equal(before, after) {
		t.Fatal("import changed an existing RTSP stall report or its media conclusion")
	}
	saved, err := os.ReadFile(dir + "/" + investigationName)
	if err != nil || !bytes.Equal(saved, ledger) {
		t.Fatalf("import rewrote the published ledger: %v", err)
	}
}
