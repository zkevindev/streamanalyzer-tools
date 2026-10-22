package media

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"testing"
)

func TestSyntheticTSKeyframe(t *testing.T) {
	idr := Inspect(SyntheticMPEGTS(true), nil)
	if idr.Format != "ts" || idr.Codec != "h264" || idr.Keyframe == nil || !*idr.Keyframe {
		t.Fatalf("idr: %+v", idr)
	}
	p := Inspect(SyntheticMPEGTS(false), nil)
	if p.Keyframe == nil || *p.Keyframe {
		t.Fatalf("p-frame: %+v", p)
	}
}

func TestTSRandomAccessWithoutNAL(t *testing.T) {
	data := append(append(tsPacket(0, true, patPayload(), false, nil), tsPacket(0x100, true, pmtPayload(0x101), false, nil)...),
		tsPacket(0x101, true, nil, true, nil)...)
	info := Inspect(data, nil)
	if info.Keyframe == nil || !*info.Keyframe {
		t.Fatalf("rai: %+v", info)
	}
}

func TestPTSSpanDuration(t *testing.T) {
	es := []byte{0x00, 0x00, 0x00, 0x01, 0x09, 0xF0, 0x00, 0x00, 0x00, 0x01, 0x65, 0x88}
	data := append(tsPacket(0, true, patPayload(), false, nil), tsPacket(0x100, true, pmtPayload(0x101), false, nil)...)
	data = append(data, tsPacket(0x101, true, pesWithPTS(902999, es), false, nil)...)
	data = append(data, tsPacket(0x101, true, pesWithPTS(1800000, es), false, nil)...)
	info := Inspect(data, nil)
	if info.PTSCount != 2 || info.PTSStart != 902999 || info.PTSEnd != 1800000 {
		t.Fatalf("pts: %+v", info)
	}
	want := float64(1800000-902999) / 90000
	if !info.DurationKnown || abs(info.Duration-want) > 1e-9 {
		t.Fatalf("duration: got %v want %v", info.Duration, want)
	}
}

func pesWithPTS(pts uint64, es []byte) []byte {
	hdr := append([]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05}, encodePTS(pts)...)
	return append(hdr, es...)
}

func encodePTS(pts uint64) []byte {
	b := make([]byte, 5)
	b[0] = byte(0x21 | ((pts >> 29) & 0x0e))
	b[1] = byte(pts >> 22)
	b[2] = byte(((pts >> 14) & 0xfe) | 0x01)
	b[3] = byte(pts >> 7)
	b[4] = byte((pts << 1) | 0x01)
	return b
}

func TestTSPCRDuration(t *testing.T) {
	es := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88}
	pes := append([]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00}, es...)
	a := uint64(0)
	b := uint64(27000000)
	data := append(tsPacket(0, true, patPayload(), false, nil), tsPacket(0x100, true, pmtPayload(0x101), false, nil)...)
	data = append(data, tsPacket(0x101, true, pes, false, &a)...)
	data = append(data, tsPacket(0x101, false, []byte{0x11}, false, &b)...)
	info := Inspect(data, nil)
	if !info.DurationKnown || abs(info.Duration-1) > 0.01 {
		t.Fatalf("duration: %+v", info)
	}
	if info.Keyframe == nil || !*info.Keyframe {
		t.Fatalf("keyframe: %+v", info)
	}
}

func TestHEVCIRAP(t *testing.T) {
	es := []byte{0x00, 0x00, 0x01, 0x26, 0x01}
	ok, known := firstVCLIsIRAP(es, true)
	if !ok || !known {
		t.Fatalf("hevc idr: %v %v", ok, known)
	}
	es[3] = 0x02
	ok, known = firstVCLIsIRAP(es, true)
	if ok || !known {
		t.Fatalf("hevc non-irap: %v %v", ok, known)
	}
}

func TestFMP4SyncSample(t *testing.T) {
	init := testInit(90000)
	syncSeg := testFragment(true, 90000)
	info := Inspect(syncSeg, init)
	if info.Format != "mp4" || info.Codec != "avc1" || info.Timescale != 90000 {
		t.Fatalf("init: %+v", info)
	}
	if info.Keyframe == nil || !*info.Keyframe || !info.DurationKnown || abs(info.Duration-1) > 0.001 {
		t.Fatalf("sync: %+v", info)
	}
	non := Inspect(testFragment(false, 90000), init)
	if non.Keyframe == nil || *non.Keyframe {
		t.Fatalf("non-sync: %+v", non)
	}
}

func TestFMP4UsesVideoTrackOnly(t *testing.T) {
	init := testInitTrack(2, 90000)
	audio := testFragmentTrack(1, false, 48000)
	video := testFragmentTrack(2, true, 90000)
	info := Inspect(append(audio, video...), init)
	if info.Keyframe == nil || !*info.Keyframe {
		t.Fatalf("keyframe: %+v", info)
	}
	if !info.DurationKnown || abs(info.Duration-1) > 0.001 {
		t.Fatalf("duration: %+v", info)
	}
}

func TestDecryptAES128(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := IVForSequence(10)
	plain := []byte("hls-segment-body!!")
	enc := encryptAES128(t, plain, key, iv)
	got, err := DecryptAES128(enc, key, iv)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("got %q", got)
	}
	if _, err := DecryptAES128(enc[:len(enc)-1], key, iv); err == nil {
		t.Fatal("expected length error")
	}
}

func encryptAES128(t *testing.T, plain, key, iv []byte) []byte {
	t.Helper()
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytesRepeat(byte(pad), pad)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func testInit(timescale uint32) []byte {
	return testInitTrack(1, timescale)
}

func testInitTrack(trackID, timescale uint32) []byte {
	mdhdRest := make([]byte, 20)
	binary.BigEndian.PutUint32(mdhdRest[8:12], timescale)
	mdhd := box("mdhd", fullbox(0, 0, mdhdRest))
	hdlrRest := make([]byte, 21)
	copy(hdlrRest[4:8], "vide")
	hdlr := box("hdlr", fullbox(0, 0, hdlrRest))
	avc1 := box("avc1", []byte{0, 0, 0, 0})
	stsdBody := append(fullbox(0, 0, []byte{0, 0, 0, 1}), avc1...)
	stsd := box("stsd", stsdBody)
	stbl := box("stbl", stsd)
	minf := box("minf", stbl)
	mdia := box("mdia", append(append(mdhd, hdlr...), minf...))
	tkhdBody := make([]byte, 16)
	binary.BigEndian.PutUint32(tkhdBody[8:12], trackID)
	tkhd := box("tkhd", fullbox(0, 0, tkhdBody))
	trak := box("trak", append(tkhd, mdia...))
	return box("moov", trak)
}

func testFragment(sync bool, sampleDuration uint32) []byte {
	return testFragmentTrack(1, sync, sampleDuration)
}

func testFragmentTrack(trackID uint32, sync bool, sampleDuration uint32) []byte {
	tfhdRest := make([]byte, 12)
	binary.BigEndian.PutUint32(tfhdRest[0:4], trackID)
	binary.BigEndian.PutUint32(tfhdRest[4:8], sampleDuration)
	tfhd := box("tfhd", fullbox(0, 0x000028, tfhdRest))
	first := uint32(0)
	if !sync {
		first = 0x00010000
	}
	trunRest := make([]byte, 12)
	binary.BigEndian.PutUint32(trunRest[0:4], 1)
	binary.BigEndian.PutUint32(trunRest[4:8], first)
	binary.BigEndian.PutUint32(trunRest[8:12], 100)
	trun := box("trun", fullbox(0, 0x000204, trunRest))
	traf := box("traf", append(tfhd, trun...))
	return box("moof", traf)
}

func box(typ string, payload []byte) []byte {
	buf := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(buf)))
	copy(buf[4:8], typ)
	copy(buf[8:], payload)
	return buf
}

func fullbox(version byte, flags uint32, rest []byte) []byte {
	b := make([]byte, 4+len(rest))
	b[0] = version
	b[1] = byte(flags >> 16)
	b[2] = byte(flags >> 8)
	b[3] = byte(flags)
	copy(b[4:], rest)
	return b
}
