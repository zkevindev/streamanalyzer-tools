package diagnostic

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestModelRequestTimingsKeepHeadersLocal(t *testing.T) {
	for _, reportRequests := range []bool{false, true} {
		request := HTTPRequestTiming{
			Number: 1, StartMS: 1, EndMS: 10, TotalMS: 9, HTTPStatus: 200,
			Method: "GET", HTTPProtocol: "HTTP/1.1", HeadersRecorded: true,
			RequestHeaders:  []HTTPHeader{{Name: "Host", Value: "private-customer.example"}},
			ResponseHeaders: []HTTPHeader{{Name: "X-Request-Id", Value: "local-request-identifier"}},
			Stages:          []CaptureStage{{Name: "connect", StartMS: 1, EndMS: 2}},
		}
		report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV}}
		if reportRequests {
			report.Requests = []HTTPRequestTiming{request}
		} else {
			report.Manifest.Observation.Requests = []HTTPRequestTiming{request}
		}
		before, _ := json.Marshal(report)
		data, err := modelInput(report)
		if err != nil {
			t.Fatal(err)
		}
		for _, local := range []string{"private-customer.example", "local-request-identifier", "headers_recorded", "request_headers", "response_headers", "HTTP/1.1"} {
			if bytes.Contains(data, []byte(local)) {
				t.Fatalf("model input exposed header metadata %q: %s", local, data)
			}
		}
		var got struct {
			Requests []modelHTTPRequest `json:"requests"`
		}
		if json.Unmarshal(data, &got) != nil || len(got.Requests) != 1 || got.Requests[0].HTTPStatus != 200 || got.Requests[0].TotalMS != 9 || len(got.Requests[0].Stages) != 1 {
			t.Fatalf("model lost timing summary: %s", data)
		}
		after, _ := json.Marshal(report)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("model projection modified the persisted report")
		}
	}
}
