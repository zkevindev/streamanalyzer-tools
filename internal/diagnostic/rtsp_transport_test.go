package diagnostic

import (
	"errors"
	"testing"
)

func TestRTSPTransportValidation(t *testing.T) {
	for _, item := range []struct{ protocol, requested, expected string }{
		{ProtocolRTSP, "", "tcp"}, {ProtocolRTSP, "tcp", "tcp"}, {ProtocolRTSP, "udp", "udp"}, {ProtocolRTSPS, "", "tcp"}, {ProtocolRTSPS, "tcp", "tcp"}, {ProtocolHTTPFLV, "", ""},
	} {
		got, err := sampleTransport(item.protocol, item.requested)
		if err != nil || got != item.expected {
			t.Fatalf("%s/%s: got %s %v", item.protocol, item.requested, got, err)
		}
	}
	for _, item := range []struct{ protocol, requested string }{{ProtocolRTSPS, "udp"}, {ProtocolRTSP, "automatic"}, {ProtocolHLS, "udp"}, {ProtocolRTMP, "tcp"}} {
		if _, err := sampleTransport(item.protocol, item.requested); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid transport accepted: %+v", item)
		}
	}
}
