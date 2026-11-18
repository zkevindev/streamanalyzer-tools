package gb28181

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestProgramMuxerSplitsLargeAccessUnit(t *testing.T) {
	mux := newProgramMuxer("h264", "g711a")
	frame := bytes.Repeat([]byte{0x73}, 140000)
	ps := mux.pack(mediaUnit{stream: videoStreamID, data: frame, key: true, pts: 3600})
	var result []byte
	packets := 0
	for data := ps[14:]; len(data) > 0; {
		if len(data) < 6 || !bytes.HasPrefix(data, []byte{0, 0, 1}) {
			t.Fatal("invalid PS start code")
		}
		length := int(binary.BigEndian.Uint16(data[4:6]))
		if length == 0 || len(data) < 6+length {
			t.Fatal("invalid PS packet length")
		}
		stream, packet := data[3], data[6:6+length]
		data = data[6+length:]
		if stream != videoStreamID {
			continue
		}
		if packets == 0 && (packet[1] != 0xc0 || packet[2] != 10) {
			t.Fatal("the first PES must carry PTS and DTS")
		}
		if packets > 0 && (packet[1] != 0 || packet[2] != 0) {
			t.Fatal("continuation PES must carry no timestamp")
		}
		packets++
		result = append(result, packet[3+int(packet[2]):]...)
	}
	if packets != 3 || !bytes.Equal(frame, result) {
		t.Fatalf("large access unit was not preserved across %d PES packets", packets)
	}
}
