package diagnostic

import (
	"bytes"
	"io"
	"net"
	"testing"
)

func TestRTSPTLSHandshakeTimeoutKeepsActualFailureStage(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(io.Discard, connection) // Accept TCP and deliberately never complete TLS.
	}()
	service := rtspIntegrationOpen(t)
	detail := rtspIntegrationCapture(t, service, "rtsps://"+listener.Addr().String()+"/stream", ProtocolRTSPS, 1)
	observation := detail.Report.Manifest.Observation
	if observation.FailureStage != "tls" || observation.FailureCode != "timeout" || len(detail.Report.RTSP.Requests) != 0 {
		t.Fatalf("handshake timeout was attributed to an unsent RTSP request: %+v", observation)
	}
	rtspIntegrationRoundTrip(t, service, detail)
	<-finished
}

func TestRTSPMalformedSDPStillProducesReplayableDiagnostic(t *testing.T) {
	const invalid = "v=0\r\nthis-is-not-valid-session-description\r\n"
	fixture := newRTSPIntegrationFixture(t, rtspIntegrationConfig{sdp: invalid})
	service := rtspIntegrationOpen(t)
	detail := rtspIntegrationCapture(t, service, fixture.address(), ProtocolRTSP, 1)
	if detail.Status != "failed" || detail.Report.RTSP == nil || !detail.Report.RTSP.SDPAvailable ||
		detail.Report.Manifest.Observation.FailureCode != "rtsp_parse_error" {
		t.Fatalf("malformed SDP discarded diagnostic evidence: %+v", detail)
	}
	files := rtspIntegrationRoundTrip(t, service, detail)
	if !bytes.Equal(files[rtspSDPName], []byte(invalid)) {
		t.Fatal("malformed original SDP was not retained")
	}
}

func TestRTSPUnmatchedPayloadCannotEstablishAudioVideoArrival(t *testing.T) {
	fixture := newRTSPIntegrationFixture(t, rtspIntegrationConfig{packets: []rtspIntegrationPacket{{raw: rtspIntegrationRTP(99, 1, 0, 100, []byte{1, 2, 3}, false)}}, closeAfterPlay: true})
	service := rtspIntegrationOpen(t)
	detail := rtspIntegrationCapture(t, service, fixture.address(), ProtocolRTSP, 1)
	metrics := detail.Report.RTSP
	if metrics == nil || metrics.RTPPackets != 1 || metrics.UnknownPayloadPackets != 1 || metrics.FirstRTPMS == nil ||
		metrics.FirstVideoRTPMS != nil || metrics.FirstAudioRTPMS != nil || metrics.Tracks[0].Received {
		t.Fatalf("unmatched RTP payload established a media arrival: %+v", metrics)
	}
	rtspIntegrationRoundTrip(t, service, detail)
}
