package media

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
)

func TestInspectCodecsMPEGTS(t *testing.T) {
	pmt := pmtPayload(0x101)
	pmt = pmt[:len(pmt)-4] // Remove the CRC before adding elementary streams.
	pmt = append(pmt,
		0x11, 0xe1, 0x02, 0xf0, 0, // AAC LATM.
		0x0f, 0xe1, 0x03, 0xf0, 0, // AAC ADTS, reported once.
		0x06, 0xe1, 0x04, 0xf0, 6, 0x05, 4, 'O', 'p', 'u', 's',
		0x06, 0xe1, 0x05, 0xf0, 2, 0x7a, 0, // Private E-AC3 descriptor.
	)
	// An unrelated private mapping forces the section across two packets.
	pmt = append(pmt, 0x06, 0xe1, 0x06, 0xf0, 150, 0x05, 148)
	pmt = append(pmt, make([]byte, 148)...)
	pmt = append(pmt, 0, 0, 0, 0)
	binary.BigEndian.PutUint16(pmt[2:4], 0xb000|uint16(len(pmt)-4))
	data := append(tsPacket(0, true, patPayload(), false, nil), tsPacket(0x100, true, pmt[:184], false, nil)...)
	continuation := tsPacket(0x100, false, pmt[184:], false, nil)
	continuation[3] |= 1
	data = append(data, continuation...)

	video, audio := InspectCodecs(data, nil)
	if !slices.Equal(video, []string{"h264"}) || !slices.Equal(audio, []string{"aac", "opus", "eac3"}) {
		t.Fatalf("TS codecs = %v / %v", video, audio)
	}
	// A missing PMT continuation must not surface a partial codec list.
	video, audio = InspectCodecs(data[:188*2], nil)
	if len(video)+len(audio) != 0 {
		t.Fatalf("truncated TS codecs = %v / %v", video, audio)
	}
}

func TestMPEG2VideoDoesNotUseH264KeyframeRules(t *testing.T) {
	const pid = 0x101
	pmt := pmtPayload(pid)
	pmt[13] = 0x02 // MPEG-2 video.
	data := append(tsPacket(0, true, patPayload(), false, nil), tsPacket(0x100, true, pmt, false, nil)...)
	data = append(data, tsPacket(pid, true, pesWithPTS(0, []byte{0, 0, 1, 0x01, 0xff}), false, nil)...)
	info := Inspect(data, nil)
	if info.Codec != "mpeg2video" || info.Keyframe != nil {
		t.Fatalf("unexpected MPEG-2 video results %+v", info)
	}
}

func TestInspectCodecsFragmentedMP4(t *testing.T) {
	const audioSampleEntryBytes = 28
	config := append([]byte{0x40, 0x15}, make([]byte, 11)...)
	config = append(config, 0x05, 2, 0x12, 0x10) // AAC-LC AudioSpecificConfig.
	decoder := append([]byte{0x04, byte(len(config))}, config...)
	es := append([]byte{0x03, byte(len(decoder) + 3), 0, 2, 0}, decoder...)
	aac := append(make([]byte, audioSampleEntryBytes), box("esds", fullbox(0, 0, es))...)
	init := box("moov", bytes.Join([][]byte{
		codecTestTrack("vide", box("hvc1", make([]byte, 78)), box("av01", make([]byte, 78))),
		codecTestTrack("soun", box("mp4a", aac), box("Opus", make([]byte, audioSampleEntryBytes))),
		codecTestTrack("text", box("avc1", make([]byte, 78))),
		codecTestTrack("soun", box("junk", make([]byte, audioSampleEntryBytes))),
	}, nil))

	video, audio := InspectCodecs(testFragment(true, 90000), init)
	if !slices.Equal(video, []string{"h265", "av1"}) || !slices.Equal(audio, []string{"aac", "opus"}) {
		t.Fatalf("fMP4 codecs = %v / %v", video, audio)
	}
	// The mp4a sample entry alone does not identify the actual codec.
	_, audio = InspectCodecs(nil, box("moov", codecTestTrack("soun", box("mp4a", make([]byte, audioSampleEntryBytes)))))
	if !slices.Equal(audio, []string{"mpeg4-audio"}) {
		t.Fatalf("mp4a without descriptor = %v", audio)
	}
}

func codecTestTrack(handler string, entries ...[]byte) []byte {
	hdlrRest := make([]byte, 21)
	copy(hdlrRest[4:8], handler)
	hdlr := box("hdlr", fullbox(0, 0, hdlrRest))
	stsd := box("stsd", fullbox(0, 0, binary.BigEndian.AppendUint32(nil, uint32(len(entries)))))
	stsd = append(stsd, bytes.Join(entries, nil)...)
	binary.BigEndian.PutUint32(stsd, uint32(len(stsd)))
	return box("trak", box("mdia", append(hdlr, box("minf", box("stbl", stsd))...)))
}

func TestInspectCodecsPackedADTS(t *testing.T) {
	// Empty ID3 metadata followed by two ADTS AAC frames.
	id3 := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}
	frame := []byte{0xff, 0xf1, 0x50, 0x80, 0x01, 0x3f, 0xfc, 0x00, 0x00}
	data := append(append(id3, frame...), frame...)
	video, audio := InspectCodecs(data, nil)
	if len(video) != 0 || !slices.Equal(audio, []string{"aac"}) || Inspect(data, nil).Format != "adts" {
		t.Fatalf("ADTS codecs = %v / %v", video, audio)
	}
	_, audio = InspectCodecs(data[:len(data)-1], nil)
	if len(audio) != 0 {
		t.Fatalf("truncated ADTS codecs = %v", audio)
	}
}
