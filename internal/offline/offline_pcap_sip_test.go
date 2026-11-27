package offline

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExtractPCAPSIPFlowsGB28181UDPAndTCP(t *testing.T) {
	baseTS := time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)
	inviteSDP := strings.Join([]string{
		"v=0",
		"o=34020000002000000001 0 0 IN IP4 10.0.0.1",
		"s=Play",
		"c=IN IP4 10.0.0.1",
		"t=0 0",
		"m=video 30000 RTP/AVP 96",
		"a=recvonly",
		"a=rtpmap:96 PS/90000",
		"y=0100000001",
	}, "\r\n")
	invite := strings.Join([]string{
		"INVITE sip:34020000001320000002@10.0.0.2:5060 SIP/2.0",
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK-1",
		"From: <sip:34020000002000000001@3402000000>;tag=a1",
		"To: <sip:34020000001320000002@3402000000>",
		"Call-ID: call-gb-001@10.0.0.1",
		"CSeq: 20 INVITE",
		"Authorization: Digest username=\"secret\", response=\"should-be-redacted\"",
		"Subject: 34020000001320000002:0100000001,34020000002000000001:0",
		"Content-Type: application/sdp",
		"Content-Length: " + strconv.Itoa(len(inviteSDP)),
		"",
		inviteSDP,
	}, "\r\n")
	okResp := strings.Join([]string{
		"SIP/2.0 200 OK",
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK-1",
		"From: <sip:34020000002000000001@3402000000>;tag=a1",
		"To: <sip:34020000001320000002@3402000000>;tag=b2",
		"Call-ID: call-gb-001@10.0.0.1",
		"CSeq: 20 INVITE",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")

	udpSIP := []sipDatagram{
		{srcIP: "10.0.0.1", srcPort: 5060, dstIP: "10.0.0.2", dstPort: 5060, transport: "udp", ts: baseTS, payload: []byte(invite)},
		{srcIP: "10.0.0.2", srcPort: 5060, dstIP: "10.0.0.1", dstPort: 5060, transport: "udp", ts: baseTS.Add(20 * time.Millisecond), payload: []byte(okResp)},
	}

	mgr := &Manager{}
	tmpDir := t.TempDir()
	flows, nextID := mgr.extractPCAPSIPFlows(udpSIP, nil, tmpDir, 1)
	if len(flows) != 1 || nextID != 2 {
		t.Fatalf("expected 1 SIP flow and nextID=2, got %d / %d", len(flows), nextID)
	}
	f := flows[0]
	if f.Protocol != "gb28181-sip" || f.Direction != "sip-udp" || f.TCPPktCount != 2 {
		t.Fatalf("unexpected SIP flow metadata: %+v", f)
	}
	if len(f.ArtifactPaths) != 1 || filepath.Base(f.ArtifactPaths[0]) != "sip_signaling.txt" {
		t.Fatalf("expected sip_signaling.txt artifact, got %+v", f.ArtifactPaths)
	}
	raw, err := os.ReadFile(f.ArtifactPaths[0])
	if err != nil {
		t.Fatalf("read sip_signaling.txt: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "INVITE sip:34020000001320000002") || !strings.Contains(text, "y=0100000001") || !strings.Contains(text, "SIP/2.0 200 OK") {
		t.Fatalf("missing expected SIP/SDP content in transcript:\n%s", text)
	}
	if strings.Contains(text, "should-be-redacted") {
		t.Fatalf("Authorization header must not be written into sip_signaling.txt")
	}
}
