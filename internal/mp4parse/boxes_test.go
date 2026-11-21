package mp4parse

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mp4Box(fourcc string, payload []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(8+len(payload)))
	b.WriteString(fourcc)
	b.Write(payload)
	return b.Bytes()
}

func mp4FullBox(fourcc string, version byte, flags uint32, payload []byte) []byte {
	hdr := make([]byte, 4+len(payload))
	hdr[0] = version
	hdr[1] = byte((flags >> 16) & 0xFF)
	hdr[2] = byte((flags >> 8) & 0xFF)
	hdr[3] = byte(flags & 0xFF)
	copy(hdr[4:], payload)
	return mp4Box(fourcc, hdr)
}

func buildSyntheticNonFastStartMP4WithElst() []byte {
	// ftyp: isom / mp42
	ftypPayload := []byte("isom\x00\x00\x02\x00isommp42")
	ftyp := mp4Box("ftyp", ftypPayload)

	// mdat placed BEFORE moov (non-FastStart)
	mdat := mp4Box("mdat", make([]byte, 512))

	// mvhd v0 (timescale=1000, duration=5000)
	mvhdBody := make([]byte, 96)
	binary.BigEndian.PutUint32(mvhdBody[8:12], 1000)
	binary.BigEndian.PutUint32(mvhdBody[12:16], 5000)
	binary.BigEndian.PutUint32(mvhdBody[92:96], 2) // next_track_ID
	mvhd := mp4FullBox("mvhd", 0, 0, mvhdBody)

	// tkhd v0 (track_id=1, duration=5000, width=1280<<16, height=720<<16)
	tkhdBody := make([]byte, 80)
	binary.BigEndian.PutUint32(tkhdBody[8:12], 1)
	binary.BigEndian.PutUint32(tkhdBody[16:20], 5000)
	binary.BigEndian.PutUint32(tkhdBody[72:76], 1280<<16)
	binary.BigEndian.PutUint32(tkhdBody[76:80], 720<<16)
	tkhd := mp4FullBox("tkhd", 0, 3, tkhdBody)

	// edts -> elst v0 (1 entry: segment_duration=5000, media_time=100, rate=1.0)
	elstBody := make([]byte, 4+12)
	binary.BigEndian.PutUint32(elstBody[0:4], 1)
	binary.BigEndian.PutUint32(elstBody[4:8], 5000)
	binary.BigEndian.PutUint32(elstBody[8:12], 100)
	binary.BigEndian.PutUint16(elstBody[12:14], 1)
	elst := mp4FullBox("elst", 0, 0, elstBody)
	edts := mp4Box("edts", elst)

	// mdhd v0 (timescale=90000, duration=450000)
	mdhdBody := make([]byte, 20)
	binary.BigEndian.PutUint32(mdhdBody[8:12], 90000)
	binary.BigEndian.PutUint32(mdhdBody[12:16], 450000)
	mdhd := mp4FullBox("mdhd", 0, 0, mdhdBody)

	// hdlr (vide)
	hdlrBody := make([]byte, 21)
	copy(hdlrBody[4:8], "vide")
	copy(hdlrBody[20:], "\x00")
	hdlr := mp4FullBox("hdlr", 0, 0, hdlrBody)

	// stsz (3 samples)
	stszBody := make([]byte, 8+12)
	binary.BigEndian.PutUint32(stszBody[4:8], 3)
	binary.BigEndian.PutUint32(stszBody[8:12], 200)
	binary.BigEndian.PutUint32(stszBody[12:16], 150)
	binary.BigEndian.PutUint32(stszBody[16:20], 162)
	stsz := mp4FullBox("stsz", 0, 0, stszBody)

	// stss (1 keyframe: sample 1)
	stssBody := make([]byte, 8)
	binary.BigEndian.PutUint32(stssBody[0:4], 1)
	binary.BigEndian.PutUint32(stssBody[4:8], 1)
	stss := mp4FullBox("stss", 0, 0, stssBody)

	stbl := mp4Box("stbl", append(stsz, stss...))
	minf := mp4Box("minf", stbl)
	mdia := mp4Box("mdia", append(append(mdhd, hdlr...), minf...))
	trak := mp4Box("trak", append(append(tkhd, edts...), mdia...))
	moov := mp4Box("moov", append(mvhd, trak...))

	var out []byte
	out = append(out, ftyp...)
	out = append(out, mdat...)
	out = append(out, moov...)
	return out
}

func findBox(nodes []*MP4BoxNode, boxType string) *MP4BoxNode {
	for _, n := range nodes {
		if n.Type == boxType {
			return n
		}
		if got := findBox(n.Children, boxType); got != nil {
			return got
		}
	}
	return nil
}

func TestExtractBoxTreeSyntheticNonFastStartAndElst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonfast.mp4")
	if err := os.WriteFile(path, buildSyntheticNonFastStartMP4WithElst(), 0o644); err != nil {
		t.Fatal(err)
	}

	tree, err := ExtractBoxTree(path, DefaultMaxBoxNodes)
	if err != nil {
		t.Fatalf("ExtractBoxTree failed: %v", err)
	}
	if tree.FastStart {
		t.Fatalf("expected FastStart=false when moov is after mdat")
	}
	if tree.Truncated {
		t.Fatalf("expected Truncated=false")
	}
	if len(tree.Boxes) != 3 || tree.Boxes[0].Type != "ftyp" || tree.Boxes[1].Type != "mdat" || tree.Boxes[2].Type != "moov" {
		t.Fatalf("unexpected top-level boxes: %+v", tree.Boxes)
	}

	ftyp := findBox(tree.Boxes, "ftyp")
	if ftyp == nil || !strings.Contains(ftyp.Summary, "isom") {
		t.Fatalf("expected ftyp summary with isom, got %+v", ftyp)
	}
	moov := findBox(tree.Boxes, "moov")
	if moov == nil || moov.Highlight != "warn" || moov.HighlightReason == "" {
		t.Fatalf("expected non-FastStart moov warn highlight, got %+v", moov)
	}
	elst := findBox(tree.Boxes, "elst")
	if elst == nil || elst.Highlight != "warn" || !strings.Contains(elst.Summary, "1") {
		t.Fatalf("expected elst warn highlight and summary, got %+v", elst)
	}
	hdlr := findBox(tree.Boxes, "hdlr")
	if hdlr == nil || !strings.Contains(hdlr.Summary, "vide") {
		t.Fatalf("expected hdlr vide summary, got %+v", hdlr)
	}
	stsz := findBox(tree.Boxes, "stsz")
	if stsz == nil || !strings.Contains(stsz.Summary, "3") {
		t.Fatalf("expected stsz sample count summary, got %+v", stsz)
	}
	stss := findBox(tree.Boxes, "stss")
	if stss == nil || !strings.Contains(stss.Summary, "1") {
		t.Fatalf("expected stss sync count summary, got %+v", stss)
	}
}

func TestExtractBoxTreeRealFMP4AndTruncation(t *testing.T) {
	initBytes, err := os.ReadFile(filepath.Join("..", "simulator", "hlsorigin", "avmedia", "video", "init.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	segBytes, err := os.ReadFile(filepath.Join("..", "simulator", "hlsorigin", "avmedia", "video", "seg-01.m4s"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fmp4Path := filepath.Join(dir, "combined.mp4")
	if err := os.WriteFile(fmp4Path, append(initBytes, segBytes...), 0o644); err != nil {
		t.Fatal(err)
	}

	tree, err := ExtractBoxTree(fmp4Path, DefaultMaxBoxNodes)
	if err != nil {
		t.Fatalf("ExtractBoxTree on real fMP4 failed: %v", err)
	}
	if !tree.Fragmented || !tree.FastStart {
		t.Fatalf("expected Fragmented=true and FastStart=true, got frag=%v fast=%v", tree.Fragmented, tree.FastStart)
	}
	if findBox(tree.Boxes, "moof") == nil || findBox(tree.Boxes, "trun") == nil || findBox(tree.Boxes, "stsd") == nil {
		t.Fatalf("expected moof/trun/stsd in fMP4 tree, got total=%d", tree.TotalNodes)
	}

	// Verify node-limit truncation protection
	small, err := ExtractBoxTree(fmp4Path, 5)
	if err != nil {
		t.Fatalf("ExtractBoxTree with maxNodes=5 failed: %v", err)
	}
	if !small.Truncated || small.TotalNodes != 5 {
		t.Fatalf("expected truncated tree at 5 nodes, got truncated=%v total=%d", small.Truncated, small.TotalNodes)
	}
}
