package offline

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

func TestParseRTSPTCPStreamInterleaved(t *testing.T) {
	sdp := "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=Test\r\nt=0 0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\n"
	var wire bytes.Buffer
	wire.WriteString("RTSP/1.0 200 OK\r\nCSeq: 2\r\nContent-Type: application/sdp\r\nContent-Length: ")
	wire.WriteString(string([]byte{byte('0' + len(sdp)/100), byte('0' + (len(sdp)/10)%10), byte('0' + len(sdp)%10)}))
	wire.WriteString("\r\n\r\n")
	wire.WriteString(sdp)
	wire.WriteString("RTSP/1.0 200 OK\r\nCSeq: 3\r\nTransport: RTP/AVP/TCP;unicast;interleaved=0-1\r\nSession: 123456\r\n\r\n")

	// Append 4 interleaved RTP packets (channel 0) + 1 RTCP SR (channel 1)
	for i := 0; i < 4; i++ {
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: uint16(100 + i),
				Timestamp:      uint32(90000 + i*3600),
				SSRC:           0x11223344,
				Marker:         true,
			},
			Payload: []byte{0x65, 0x88, 0x84, 0x21},
		}
		raw, err := pkt.Marshal()
		if err != nil {
			t.Fatalf("marshal rtp: %v", err)
		}
		var hdr [4]byte
		hdr[0] = '$'
		hdr[1] = 0
		binary.BigEndian.PutUint16(hdr[2:4], uint16(len(raw)))
		wire.Write(hdr[:])
		wire.Write(raw)
	}

	sr := &rtcp.SenderReport{
		SSRC:        0x11223344,
		PacketCount: 4,
		OctetCount:  16,
	}
	rawSR, err := sr.Marshal()
	if err != nil {
		t.Fatalf("marshal rtcp: %v", err)
	}
	var srHdr [4]byte
	srHdr[0] = '$'
	srHdr[1] = 1
	binary.BigEndian.PutUint16(srHdr[2:4], uint16(len(rawSR)))
	wire.Write(srHdr[:])
	wire.Write(rawSR)

	tk := tcpStreamKey{srcIP: "127.0.0.1", srcPort: 8554, dstIP: "127.0.0.1", dstPort: 54321}
	parsed := parseRTSPTCPStream(tk, wire.Bytes(), time.Now())
	if parsed == nil {
		t.Fatal("expected parsed RTSP TCP stream, got nil")
	}
	if len(parsed.signaling) < 2 {
		t.Fatalf("expected >=2 signaling messages, got %d", len(parsed.signaling))
	}
	if !strings.Contains(parsed.sdp, "H264/90000") {
		t.Fatalf("expected parsed SDP to contain H264/90000, got %q", parsed.sdp)
	}
	acc := parsed.rtpBySSRC[0x11223344]
	if acc == nil || len(acc.packets) != 4 {
		t.Fatalf("expected 4 RTP packets for SSRC 0x11223344, got %+v", acc)
	}
	if parsed.rtcpBySSRC[0x11223344].count != 1 || parsed.rtcpBySSRC[0x11223344].senderPkts != 4 {
		t.Fatalf("expected RTCP SR count=1 senderPkts=4, got %+v", parsed.rtcpBySSRC[0x11223344])
	}
}
