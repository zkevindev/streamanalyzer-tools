package appserver

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"streamanalyzer/internal/diagnostic"
)

func TestDiagnosticPresentationSelectorHTTPRejectsConflictsAndSeparatesKinds(t *testing.T) {
	clearDiagnosticModelEnv(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(testFLV()) }))
	defer origin.Close()
	server := testDiagnosticServer(t, t.TempDir())
	request, _ := json.Marshal(diagnostic.SampleRequest{URL: origin.URL + "/stream.flv", Seconds: 1})
	created := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/runs", 202, request))
	path := "/api/diagnostics/runs/" + created.ID
	waitDiagnostic(t, server, path)
	archive := diagnosticRequest(t, server, "GET", path+"/evidence.zip", 200, nil)
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
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
	var manifest diagnostic.Manifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	// The selector fixture reuses a verified source/timeline and supplies an
	// offline observation window, avoiding dependency on real-time waiting or
	// browser automation to test the HTTP request contract.
	manifest.Version, manifest.Observation.ElapsedMS = 3, 10000
	ready := int64(100)
	record := diagnostic.PlayerRecording{Version: 4, Revision: 1, StartedAt: manifest.Observation.StartedAt, Source: "same_capture", Engine: "mpegtsjs", ElapsedMS: 9000, Complete: true,
		Events:  []diagnostic.PlayerEvent{{Kind: "start"}, {AtMS: 100, Kind: "playing"}, {AtMS: 3500, Kind: "waiting"}, {AtMS: 5000, Kind: "playing"}, {AtMS: 9000, Kind: "stop"}},
		Samples: []diagnostic.PlayerSample{}, Presentation: &diagnostic.PlayerPresentationRecording{Support: "supported", ObservationStartMS: 100, ObservationEndMS: 9000, ReadyAtMS: &ready, Gaps: []diagnostic.PlayerPresentationGap{{StartMS: 1000, EndMS: 2500, ExpectedFrameMS: 33}, {StartMS: 3500, EndMS: 5000, ExpectedFrameMS: 33}}},
	}
	files["player.json"], _ = json.Marshal(record)
	hash := sha256.Sum256(files["player.json"])
	manifest.Player = &diagnostic.Artifact{Bytes: int64(len(files["player.json"])), SHA256: hex.EncodeToString(hash[:])}
	files["manifest.json"], _ = json.Marshal(manifest)
	var prepared bytes.Buffer
	writer := zip.NewWriter(&prepared)
	for name, data := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	imported := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, "POST", "/api/diagnostics/import", 201, prepared.Bytes()))
	path = "/api/diagnostics/runs/" + imported.ID
	detail := waitDiagnostic(t, server, path)
	if detail.Report.Player.Presentation.Count == nil || *detail.Report.Player.Presentation.Count != 2 || detail.Report.Player.StallCount != 1 {
		t.Fatal("selector fixture did not preserve independent presentation and buffering observations")
	}
	for _, body := range []string{
		`{"interval_kind":"unknown","interval_index":1}`,
		`{"stall_index":1,"interval_index":2}`,
		`{"interval_kind":"presentation_gap","stall_index":1,"interval_index":1}`,
		`{"interval_kind":"presentation_gap"}`,
		`{"interval_kind":"presentation_gap","interval_index":99}`,
		`{"interval_kind":"presentation_gap","interval_index":1,"unexpected":true}`,
	} {
		diagnosticRequest(t, server, "POST", path+"/stalls", 400, []byte(body))
	}
	presentation := decodeDiagnostic[diagnostic.StallInvestigation](t, diagnosticRequest(t, server, "POST", path+"/stalls", 202, []byte(`{"interval_kind":"presentation_gap","interval_index":1}`)))
	buffering := decodeDiagnostic[diagnostic.StallInvestigation](t, diagnosticRequest(t, server, "POST", path+"/stalls", 202, []byte(`{"interval_kind":"buffering","interval_index":1}`)))
	if presentation.ID == buffering.ID || presentation.IntervalKind != diagnostic.IntervalPresentationGap || presentation.StallIndex != 1 || presentation.Stall.StartMS != 1000 || presentation.Stall.EndMS != 2500 || buffering.IntervalKind != "" || buffering.Stall.StartMS != 3500 || buffering.Stall.EndMS != 5000 {
		t.Fatalf("typed index1 requests collided or selected the wrong evidence: %+v %+v", presentation, buffering)
	}
	for _, test := range []struct{ body, id string }{{`{"interval_kind":"presentation_gap","interval_index":1}`, presentation.ID}, {`{"stall_index":1}`, buffering.ID}} {
		req, err := http.NewRequestWithContext(t.Context(), "POST", server.URL()+path+"/stalls", bytes.NewBufferString(test.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || (response.StatusCode != 200 && response.StatusCode != 202) {
			t.Fatalf("typed selector retry failed: %d %v %s", response.StatusCode, err, data)
		}
		if item := decodeDiagnostic[diagnostic.StallInvestigation](t, data); item.ID != test.id {
			t.Fatal("typed selector retry created another case")
		}
	}
}
