package diagnostic

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConfiguredCaptureLimitPersistsReplayablePrefix(t *testing.T) {
	const limit = int64(32)
	body := []byte{'F', 'L', 'V', 1, 1, 0, 0, 0, 9, 0, 0, 0, 0}
	body = append(body, 9, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0)
	body = append(body, make([]byte, 256)...)
	body = append(body, binary.BigEndian.AppendUint32(nil, 267)...)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer origin.Close()

	service, err := OpenWithCaptureLimit(t.TempDir(), nil, limit)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	run, err := service.Start(SampleRequest{URL: origin.URL + "/live.flv", Seconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	var detail Detail
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		detail, err = service.Detail(run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Report != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if detail.Report == nil || detail.Report.Manifest.Observation.EndReason != "size_limit" ||
		detail.Report.Manifest.Stream.Bytes != limit || detail.Report.Manifest.StreamLimitBytes != limit {
		t.Fatalf("capture limit was not enforced and recorded: %+v", detail.Report)
	}

	var bundle bytes.Buffer
	if err := service.WriteBundle(run.ID, &bundle); err != nil {
		t.Fatal(err)
	}
	replay, err := service.Import(&bundle)
	if err != nil {
		t.Fatalf("limited capture did not produce a replayable bundle: %v", err)
	}
	imported, err := service.Detail(replay.ID)
	if err != nil || imported.Report == nil || imported.Report.Manifest.StreamLimitBytes != limit ||
		imported.Report.Manifest.Stream.Bytes != limit || imported.Report.Manifest.Observation.EndReason != "size_limit" {
		t.Fatalf("replay lost the configured limit or saved prefix: %+v, %v", imported.Report, err)
	}
}

func TestCaptureLimitRejectsValuesOutsidePortableEvidenceRange(t *testing.T) {
	for _, limit := range []int64{0, -1, MaxCaptureLimitBytes + 1} {
		if _, err := OpenWithCaptureLimit(t.TempDir(), nil, limit); err == nil {
			t.Fatalf("accepted invalid capture limit %d", limit)
		}
	}
}
