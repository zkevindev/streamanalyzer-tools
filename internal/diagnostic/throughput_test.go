package diagnostic

import (
	"encoding/json"
	"errors"
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

func TestValidateStoredThroughputAcceptsLegacyHLSBoundary(t *testing.T) {
	dir := t.TempDir()
	record, err := json.Marshal(hlsRecord{Kind: hlsSegment, Result: "ok", Bytes: 3200, ArrivalMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, timelineName), append(record, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	replayed := Report{Manifest: Manifest{Protocol: ProtocolHLS, Observation: Observation{ElapsedMS: 1000}}}
	stored := Report{Throughput: &ThroughputReport{
		Version: "throughput/1",
		Samples: []ThroughputSample{{
			DurationMS: 1000, NetworkBytes: 3200, MediaBytes: 3200, VideoBytes: 3200, NetworkKbps: 25.6,
		}},
		AverageNetworkKbps: 25.6, AverageMediaKbps: 25.6, AverageVideoKbps: 25.6,
		MinimumNetworkKbps: 25.6, MaximumNetworkKbps: 25.6,
		Boundary: legacyThroughputBoundary,
	}}
	validated, err := validateStoredThroughput(dir, stored, replayed)
	if err != nil {
		t.Fatal(err)
	}
	if validated.Boundary != legacyThroughputBoundary {
		t.Fatalf("legacy wording changed to %q", validated.Boundary)
	}

	t.Run("tampered sample", func(t *testing.T) {
		tampered := *stored.Throughput
		tampered.Samples = append([]ThroughputSample(nil), tampered.Samples...)
		tampered.Samples[0].NetworkBytes++
		if _, err := validateStoredThroughput(dir, Report{Throughput: &tampered}, replayed); !errors.Is(err, ErrInvalid) {
			t.Fatalf("tampered legacy throughput should fail validation, got %v", err)
		}
	})
	t.Run("unknown wording", func(t *testing.T) {
		tampered := *stored.Throughput
		tampered.Boundary += "另有业务开销。"
		if _, err := validateStoredThroughput(dir, Report{Throughput: &tampered}, replayed); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unexpected throughput wording should fail validation, got %v", err)
		}
	})
}
