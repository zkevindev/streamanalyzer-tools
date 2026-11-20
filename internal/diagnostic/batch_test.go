package diagnostic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBatchDiagnosticGeneratesAutomaticBaselineComparison(t *testing.T) {
	const secret = "batch-private-secret"
	newOrigin := func(delay time.Duration) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+secret {
				t.Error("batch request lost custom authorization")
			}
			time.Sleep(delay)
			_, _ = w.Write(accessTestFLV())
		}))
	}
	first, second := newOrigin(30*time.Millisecond), newOrigin(0)
	defer first.Close()
	defer second.Close()
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	created, err := service.StartBatch(BatchRequest{Samples: []SampleRequest{
		{URL: first.URL + "/live.flv", Seconds: 1, Headers: map[string]string{"Authorization": "Bearer " + secret}},
		{URL: second.URL + "/live.flv", Seconds: 1, Headers: map[string]string{"Authorization": "Bearer " + secret}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var result BatchResult
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		result, err = service.Batch(created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if result.Status == "running" || len(result.Runs) != 2 || len(result.Comparisons) != 1 {
		t.Fatalf("batch did not finish with an automatic comparison: %+v", result)
	}
	comparison := result.Comparisons[0]
	if comparison.BaselineID != result.Runs[0].ID || comparison.CurrentID != result.Runs[1].ID || len(comparison.Comparison.Metrics) == 0 {
		t.Fatalf("incorrect batch baseline: %+v", comparison)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), "Authorization") {
		t.Fatal("batch retained custom access credentials")
	}
}

func TestBatchDiagnosticRejectsPlayerAndInvalidSizes(t *testing.T) {
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for _, req := range []BatchRequest{
		{Samples: []SampleRequest{{URL: "http://example.test/live.flv"}}},
		{Samples: []SampleRequest{{URL: "http://a.test/live.flv", Playback: true}, {URL: "http://b.test/live.flv"}}},
	} {
		if _, err := service.StartBatch(req); err == nil {
			t.Fatalf("accepted invalid batch: %+v", req)
		}
	}
}
