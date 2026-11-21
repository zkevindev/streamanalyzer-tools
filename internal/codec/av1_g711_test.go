package codec

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildTestAV1SeqHdrOBU builds a minimal compliant AV1 Sequence Header OBU (obu_type=1)
// with reduced_still_picture_header=1, seq_profile=0 (Main), seq_level_idx_0=8 (4.0),
// width=1280, height=720, 8-bit 4:2:0.
func buildTestAV1SeqHdrOBU(width, height int) []byte {
	var bits []uint8
	push := func(val uint64, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, uint8((val>>uint(i))&1))
		}
	}
	push(0, 3)  // seq_profile = 0 (Main)
	push(1, 1)  // still_picture = 1
	push(1, 1)  // reduced_still_picture_header = 1
	push(8, 5)  // seq_level_idx[0] = 8 (4.0)
	push(10, 4) // frame_width_bits_minus_1 = 10 -> 11 bits
	push(10, 4) // frame_height_bits_minus_1 = 10 -> 11 bits
	push(uint64(width-1), 11)
	push(uint64(height-1), 11)
	push(0, 1) // use_128x128_superblock
	push(0, 1) // enable_filter_intra
	push(0, 1) // enable_intra_edge_filter
	push(0, 1) // enable_superres
	push(0, 1) // enable_cdef
	push(0, 1) // enable_restoration
	// color_config():
	push(0, 1) // high_bitdepth = 0 -> 8-bit
	push(0, 1) // mono_chrome = 0
	push(0, 1) // color_description_present_flag = 0
	push(0, 1) // color_range = 0
	push(0, 2) // chroma_sample_position = 0
	push(0, 1) // separate_uv_delta_q = 0
	push(0, 1) // film_grain_params_present = 0
	// trailing bit
	push(1, 1)
	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}
	payload := make([]byte, len(bits)/8)
	for i, b := range bits {
		if b != 0 {
			payload[i/8] |= 1 << uint(7-(i%8))
		}
	}
	// OBU header: forbidden(0)=0, type(4)=1 (SEQ_HDR), extension(1)=0, has_size(1)=1, reserved(1)=0 -> 0x0A
	out := []byte{0x0A, byte(len(payload))}
	out = append(out, payload...)
	return out
}

func TestParseAV1SequenceHeader(t *testing.T) {
	obu := buildTestAV1SeqHdrOBU(1280, 720)
	meta, err := ParseAV1SequenceHeader(obu)
	if err != nil {
		t.Fatalf("ParseAV1SequenceHeader failed: %v", err)
	}
	if meta.SeqProfile != 0 || meta.ProfileName != "Main" {
		t.Fatalf("unexpected profile: %+v", meta)
	}
	if meta.LevelName != "4.0" {
		t.Fatalf("expected level 4.0, got %+v", meta)
	}
	if meta.Width != 1280 || meta.Height != 720 {
		t.Fatalf("expected 1280x720, got %dx%d", meta.Width, meta.Height)
	}
	if meta.BitDepth != 8 || meta.ChromaFormat != "4:2:0" {
		t.Fatalf("unexpected bitdepth/chroma: %+v", meta)
	}

	// Test av1C container wrapper (4-byte header + configOBUs)
	av1c := append([]byte{0x81, 0x08, 0x0C, 0x00}, obu...)
	meta2, err := ParseAV1SequenceHeader(av1c)
	if err != nil {
		t.Fatalf("ParseAV1SequenceHeader(av1C) failed: %v", err)
	}
	if meta2.Width != 1280 || meta2.Height != 720 {
		t.Fatalf("av1C dimensions mismatch: %+v", meta2)
	}

	// Test InspectFrame on AV1
	ins := InspectFrame(obu, InspectOptions{MediaType: "video", Codec: "av1"})
	if ins.AV1 == nil || ins.AV1.Width != 1280 || ins.AV1.Height != 720 {
		t.Fatalf("InspectFrame AV1 failed: %+v", ins)
	}
}

func TestWriteG711WAVAndInspect(t *testing.T) {
	raw := make([]byte, 160) // 20ms @ 8kHz
	for i := range raw {
		raw[i] = byte(0x55 ^ i)
	}
	for _, cname := range []string{"pcma", "pcmu"} {
		var buf bytes.Buffer
		if err := WriteG711WAV(&buf, raw, cname); err != nil {
			t.Fatalf("WriteG711WAV(%s) failed: %v", cname, err)
		}
		b := buf.Bytes()
		if len(b) != 44+len(raw)*2 {
			t.Fatalf("unexpected wav length for %s: got %d want %d", cname, len(b), 44+len(raw)*2)
		}
		if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
			t.Fatalf("invalid WAV header for %s", cname)
		}
		sampleRate := binary.LittleEndian.Uint32(b[24:28])
		if sampleRate != 8000 {
			t.Fatalf("expected 8000Hz, got %d", sampleRate)
		}

		ins := InspectFrame(raw, InspectOptions{MediaType: "audio", Codec: cname})
		if ins.AudioHeader == nil || ins.AudioHeader.SampleRate != 8000 || ins.AudioHeader.Channels != 1 {
			t.Fatalf("InspectFrame(%s) failed: %+v", cname, ins)
		}
	}
}
