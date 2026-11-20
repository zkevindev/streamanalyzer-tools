package diagnostic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestThroughputUsesArrivalBytesAndPartialSecondDuration(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, timelineName))
	if err != nil {
		t.Fatal(err)
	}
	events := []Event{{Kind: "video", Size: 1000, ArrivalMS: 100}, {Kind: "audio", Size: 2000, ArrivalMS: 1100}}
	for _, event := range events {
		if err := json.NewEncoder(file).Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	file.Close()
	report := Report{Manifest: Manifest{Protocol: ProtocolHTTPFLV, Observation: Observation{ElapsedMS: 2500}, Stream: Artifact{Bytes: 3013}}}
	throughput, err := buildThroughput(dir, report)
	if err != nil {
		t.Fatal(err)
	}
	if len(throughput.Samples) != 3 || throughput.Samples[0].NetworkBytes != 1013 || throughput.Samples[1].NetworkBytes != 2000 ||
		throughput.Samples[2].DurationMS != 500 || throughput.Samples[2].NetworkKbps != 0 || throughput.Samples[0].VideoBytes != 1000 || throughput.Samples[1].AudioBytes != 2000 {
		t.Fatalf("incorrect throughput samples: %+v", throughput.Samples)
	}
	if throughput.AverageNetworkKbps < 9.64 || throughput.AverageNetworkKbps > 9.65 || throughput.AverageMediaKbps != 9.6 {
		t.Fatalf("incorrect average rates: %+v", throughput)
	}
	stored := Report{Throughput: throughput}
	validated, err := validateStoredThroughput(dir, stored, report)
	if err != nil || validated == nil {
		t.Fatal(err)
	}
	tampered := *throughput
	tampered.AverageNetworkKbps++
	if _, err := validateStoredThroughput(dir, Report{Throughput: &tampered}, report); err == nil {
		t.Fatal("accepted throughput inconsistent with the timeline")
	}
}
