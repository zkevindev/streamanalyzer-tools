package diagnostic

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Published synthetic captures preserve both a playable H.264 result and the
// old H.265 unsupported result. New codec support must not rewrite either.
func TestRTSPPublishedPreviewV3RemainsExact(t *testing.T) {
	for _, item := range []struct{ name, state string }{{"rtsp-v3-h264-preview.zip", "ready"}, {"rtsp-v3-h265-unsupported.zip", "unsupported"}} {
		t.Run(item.name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + item.name)
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
				reader, err := entry.Open()
				if err != nil {
					t.Fatal(err)
				}
				err = json.NewDecoder(reader).Decode(&expected)
				reader.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if expected.RuleVersion != "rtsp/3" || expected.RTSP == nil || expected.RTSP.Preview == nil || expected.RTSP.Preview.Version != 1 || expected.RTSP.Preview.State != item.state {
				t.Fatal("invalid published preview fixture")
			}
			actual, err := readBundle(t.TempDir(), bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				a, _ := json.Marshal(actual)
				e, _ := json.Marshal(expected)
				t.Fatalf("published preview report changed\nactual=%s\nexpected=%s", a, e)
			}
		})
	}
}
