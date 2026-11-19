package diagnostic

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// This synthetic sequence fixture was exported by the published TCP release
// before UDP support. Compare its stored report with independently replayed
// evidence, so changing both capture and replay cannot hide a legacy change.
func TestRTSPLegacyPublishedTCPReportRemainsExact(t *testing.T) {
	data, err := os.ReadFile("testdata/rtsp-v1-sequence.zip")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var expected Report
	for _, entry := range archive.File {
		if entry.Name != reportName {
			continue
		}
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewDecoder(file).Decode(&expected)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if expected.RuleVersion != "rtsp/1" || expected.RTSP == nil || expected.RTSP.RTPPackets != 8 {
		t.Fatal("legacy fixture is missing its published report")
	}
	actual, err := readBundle(t.TempDir(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		a, _ := json.Marshal(actual)
		e, _ := json.Marshal(expected)
		t.Fatalf("published TCP report changed\nactual=%s\nexpected=%s", a, e)
	}
}
