package diagnostic

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/pion/rtp"
)

func TestRTSPOpusHeadUsesRFC7845LittleEndianMappingZero(t *testing.T) {
	for _, channels := range []int{1, 2} {
		header, err := rtspOpusHead(channels)
		want := append([]byte("OpusHead"), 1, byte(channels), 0, 0, 0x80, 0xbb, 0, 0, 0, 0, 0)
		if err != nil || !bytes.Equal(header, want) {
			t.Fatalf("invalid live RTP OpusHead: %x (%v)", header, err)
		}
	}
	for _, channels := range []int{-1, 0, 3, 255} {
		if _, err := rtspOpusHead(channels); err == nil {
			t.Fatal("unsupported channel mapping accepted")
		}
	}
}

func TestRTSPOpusGeneratedBrowserFixturesHaveDeclaredDurations(t *testing.T) {
	data, err := os.ReadFile("testdata/opus-fixture/packets.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Samples int64  `json:"samples"`
		Payload []byte `json:"payload"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	want := []int64{120, 240, 480, 960, 1920, 2880, 5760}
	if len(fixtures) != len(want) {
		t.Fatal("missing browser duration fixtures")
	}
	for index, fixture := range fixtures {
		got, err := rtspOpusPacketSamples(fixture.Payload)
		if err != nil || fixture.Samples != want[index] || got != want[index] {
			t.Fatalf("encoded browser fixture %d has wrong duration: %d %v", index, got, err)
		}
	}
}

func TestRTSPOpusPreviewPreservesPayloadReceiverClockAndGaps(t *testing.T) {
	player := newPlayerSession(t.Context(), ProtocolRTSP, nil, nil)
	defer player.Close()
	preview := newRTSPPreview(player)
	opus, err := newRTSPOpusPreview(2)
	if err != nil {
		t.Fatal(err)
	}
	packets := [][]byte{{0xf8, 0xff, 0xfe}, {0x88, 1}, {0xfb, 6, 1, 2, 3, 4, 5, 6}}
	for index, at := range []int64{48000, 52800, 53040} {
		opus.write(preview, rtspPreviewPacket{packet: &rtp.Packet{Payload: packets[index]}, pts: at})
	}
	if preview.report.AudioAUs != 3 || preview.report.State != "ready" || preview.report.PacketErrors != 0 {
		t.Fatalf("Opus remux failed: %+v", preview.report)
	}
	prefix, _, stop, err := player.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if len(prefix) != 4 {
		t.Fatalf("want OpusHead and 3 original encoded packets, got %d", len(prefix))
	}
	for index, at := range []uint32{3000, 3000, 3100, 3105} {
		chunk := prefix[index]
		timestamp := uint32(chunk[7])<<24 | uint32(chunk[4])<<16 | uint32(chunk[5])<<8 | uint32(chunk[6])
		if chunk[0] != 8 || timestamp != at || !bytes.Equal(chunk[12:16], []byte("Opus")) {
			t.Fatalf("wrong Enhanced FLV clock or FourCC: %x", chunk[:16])
		}
		if index > 0 && (chunk[11] != 0x91 || !bytes.Equal(chunk[16:len(chunk)-4], packets[index-1])) {
			t.Fatal("encoded Opus bytes changed")
		}
	}
	if prefix[0][11] != 0x90 || !bytes.Equal(prefix[0][16:24], []byte("OpusHead")) {
		t.Fatal("missing Enhanced FLV Opus identification header")
	}
	// A late/overlapping packet and malformed framing cannot manufacture
	// additional encoded AUs or be used to repair the original RTP timeline.
	opus.write(preview, rtspPreviewPacket{packet: &rtp.Packet{Payload: []byte{0xf8, 0xff, 0xfe}}, pts: 53040})
	opus.write(preview, rtspPreviewPacket{packet: &rtp.Packet{Payload: []byte{0xfb, 0}}, pts: 60000})
	if preview.report.AudioAUs != 3 || preview.report.PacketErrors != 2 {
		t.Fatal("invalid/overlapping Opus packets entered the preview")
	}
}

func TestRTSPOpusPacketDurationAndFraming(t *testing.T) {
	tests := []struct {
		name    string
		packet  []byte
		samples int64
	}{
		{"celt_2_5ms", []byte{0x80, 1}, 120},
		{"celt_5ms", []byte{0x88, 1}, 240},
		{"celt_10ms", []byte{0x90, 1}, 480},
		{"silence_20ms", []byte{0xf8, 0xff, 0xfe}, 960},
		{"silk_40ms", []byte{0x10, 1}, 1920},
		{"silk_60ms", []byte{0x18, 1}, 2880},
		{"code1_cbr_40ms", []byte{0xf9, 1, 2, 3, 4}, 1920},
		{"code2_vbr_40ms", []byte{0xfa, 1, 1, 2, 3}, 1920},
		{"code3_vbr_60ms", []byte{0xfb, 0x83, 1, 2, 1, 2, 3, 4}, 2880},
		{"code3_cbr_120ms", []byte{0xfb, 6, 1, 2, 3, 4, 5, 6}, 5760},
		{"code3_padding", []byte{0xfb, 0x41, 1, 1, 0}, 960},
		{"zero_length_frame", []byte{0xf8}, 960},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := bytes.Clone(test.packet)
			got, err := rtspOpusPacketSamples(test.packet)
			if err != nil || got != test.samples || !bytes.Equal(original, test.packet) {
				t.Fatalf("duration=%d want=%d err=%v; packet mutation=%v", got, test.samples, err, !bytes.Equal(original, test.packet))
			}
		})
	}
	longSize := append([]byte{0xfa, 252, 0}, bytes.Repeat([]byte{1}, 252)...)
	longSize = append(longSize, 2)
	if got, err := rtspOpusPacketSamples(longSize); err != nil || got != 1920 {
		t.Fatalf("two-byte VBR size rejected: %v", err)
	}
	longPadding := append([]byte{0xfb, 0x41, 255, 1, 1}, bytes.Repeat([]byte{0}, 255)...)
	if got, err := rtspOpusPacketSamples(longPadding); err != nil || got != 960 {
		t.Fatalf("extended padding rejected: %v", err)
	}
}

func TestRTSPOpusRejectsMalformedFramingAndDurationWithoutMutation(t *testing.T) {
	for _, packet := range [][]byte{
		nil, {}, {0xf9, 1}, {0xfa}, {0xfa, 252}, {0xfa, 2, 1},
		{0xfb}, {0xfb, 0}, {0xfb, 49}, {0xfb, 7, 1, 2, 3, 4, 5, 6, 7},
		{0xfb, 0x41}, {0xfb, 0x41, 2, 0}, {0xfb, 0x43, 255},
		{0xfb, 0x83, 3, 3, 1, 2, 3}, {0xfb, 3, 1, 2},
		append([]byte{0xf8}, bytes.Repeat([]byte{1}, 1276)...),
		append([]byte{0xfa, 0}, bytes.Repeat([]byte{1}, 1276)...),
		append([]byte{0xf8}, bytes.Repeat([]byte{1}, 64<<10)...),
	} {
		original := bytes.Clone(packet)
		if samples, err := rtspOpusPacketSamples(packet); err == nil || samples != 0 || !bytes.Equal(original, packet) {
			t.Fatalf("malformed packet accepted/mutated: samples=%d len=%d prefix=%x err=%v", samples, len(packet), packet[:min(len(packet), 12)], err)
		}
	}
}
