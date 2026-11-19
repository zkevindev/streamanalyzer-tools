package diagnostic

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRTSPModelProjectionIsBoundedAndKeepsRawMetadataLocal(t *testing.T) {
	metrics := &RTSPMetrics{Boundary: "private-camera.example", Transport: "private-session", RTPPackets: 90, RequestsTruncated: 5}
	for i := 1; i <= 64; i++ {
		metrics.Requests = append(metrics.Requests, RTSPRequestTiming{Number: i, Method: "PLAY", StatusCode: 200, Complete: true})
	}
	metrics.Requests[63].StatusCode = 401
	metrics.Requests[63].Method = "private-path"
	metrics.Requests[63].FailureCode = "private-error-text"
	for i := 1; i <= 32; i++ {
		metrics.Tracks = append(metrics.Tracks, RTSPTrackMetrics{ID: i, Kind: "video", Codec: "h264", Received: true})
	}
	metrics.Tracks[31].SequenceGaps = 2
	metrics.Tracks[31].Codec = "private-codec-attribute"
	metrics.Tracks[31].Kind = "private-media-attribute"
	report := Report{Manifest: Manifest{Protocol: ProtocolRTSP, Observation: Observation{Origin: "rtsp://private-origin.example"}}, RTSP: metrics}
	before, _ := json.Marshal(report)
	data, err := modelInput(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-camera", "private-session", "private-path", "private-error", "private-codec", "private-media", "private-origin"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("model exposed local RTSP metadata %s", secret)
		}
	}
	var got struct {
		RTSP modelRTSPMetrics `json:"rtsp"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.RTSP.Requests) != 32 || got.RTSP.OmittedRequests != 32 || got.RTSP.RequestsTruncated != 5 || got.RTSP.Requests[31].StatusCode != 401 ||
		len(got.RTSP.Tracks) != 16 || got.RTSP.OmittedTracks != 16 || got.RTSP.Tracks[15].SequenceGaps != 2 || got.RTSP.Tracks[15].Codec != "unknown" {
		t.Fatalf("RTSP model lost bounded anomaly evidence: %s", data)
	}
	after, _ := json.Marshal(report)
	if !bytes.Equal(before, after) {
		t.Fatal("model projection mutated local evidence")
	}
}
