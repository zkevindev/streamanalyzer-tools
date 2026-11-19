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

// Collected from the unchanged pre-vision release: a real browser stall,
// verified source screenshots, and the original successful text explanation.
func TestMultimodalPublishedTextBundleRemainsUnchanged(t *testing.T) {
	data, err := os.ReadFile("testdata/multimodal-legacy-text.zip")
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
	var expected Report
	if err := json.Unmarshal(files[reportName], &expected); err != nil {
		t.Fatal(err)
	}
	if len(expected.StallInvestigations) != 1 {
		t.Fatal("missing historical case")
	}
	item := expected.StallInvestigations[0]
	if item.Explanation == nil || item.ModelState != "complete" || item.Explanation.InputEvidence != nil || len(item.Media.Assets) != 4 {
		t.Fatal("historical text fixture incomplete")
	}
	dir := t.TempDir()
	actual, err := readBundle(dir, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(expected)
	after, _ := json.Marshal(actual)
	if !bytes.Equal(before, after) {
		t.Fatal("import rewrote the historical report or invented image-input audit")
	}
	for _, name := range []string{investigationName, rtspStreamName, "stalls/S001/clip.mp4", "stalls/S001/before.jpg", "stalls/S001/during.jpg", "stalls/S001/after.jpg"} {
		saved, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(saved, files[name]) {
			t.Fatalf("historical artifact changed: %s (%v)", name, err)
		}
	}
}
