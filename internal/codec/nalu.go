package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// NALU describes one NAL unit inside an AVCC/HVCC length-prefixed frame payload.
type NALU struct {
	Type int
	Len  int
	Name string
}

// SplitAVCC walks a length-prefixed frame payload (FLV/MP4 style) and returns
// its NAL units. lengthSize is normally 4. Malformed tails are ignored so a
// partially corrupted frame still yields the units that could be read.
func SplitAVCC(payload []byte, lengthSize int, isHEVC bool) []NALU {
	if lengthSize <= 0 || lengthSize > 4 {
		lengthSize = 4
	}

	var out []NALU
	for off := 0; off+lengthSize <= len(payload); {
		var size int
		switch lengthSize {
		case 1:
			size = int(payload[off])
		case 2:
			size = int(binary.BigEndian.Uint16(payload[off : off+2]))
		case 3:
			size = int(payload[off])<<16 | int(payload[off+1])<<8 | int(payload[off+2])
		default:
			size = int(binary.BigEndian.Uint32(payload[off : off+4]))
		}
		off += lengthSize
		if size <= 0 || off+size > len(payload) {
			break
		}

		nal := payload[off : off+size]
		var t int
		if isHEVC {
			t = int(nal[0]>>1) & 0x3f
		} else {
			t = int(nal[0] & 0x1f)
		}
		out = append(out, NALU{Type: t, Len: size, Name: naluTypeName(t, isHEVC)})
		off += size
	}
	return out
}

// NALUTypeName returns a short name for an H.264 or H.265 NAL unit type.
func NALUTypeName(t int, isHEVC bool) string {
	return naluTypeName(t, isHEVC)
}

func naluTypeName(t int, isHEVC bool) string {
	if isHEVC {
		switch {
		case t >= 16 && t <= 21:
			return "IDR/IRAP"
		case t == 32:
			return "VPS"
		case t == 33:
			return "SPS"
		case t == 34:
			return "PPS"
		case t == 35:
			return "AUD"
		case t == 39, t == 40:
			return "SEI"
		case t <= 9:
			return "Slice"
		default:
			return "Other"
		}
	}
	switch t {
	case 1:
		return "Slice"
	case 5:
		return "IDR"
	case 6:
		return "SEI"
	case 7:
		return "SPS"
	case 8:
		return "PPS"
	case 9:
		return "AUD"
	default:
		return "Other"
	}
}

// H264SliceType reads slice_type from an H.264 slice NAL (types 1 and 5) and
// maps it to "I", "P" or "B". It returns "" when the NAL is not a slice or the
// header cannot be read.
func H264SliceType(nal []byte) string {
	if len(nal) < 2 {
		return ""
	}
	t := int(nal[0] & 0x1f)
	if t != 1 && t != 5 {
		return ""
	}

	r := newBitReader(rbspFromNAL(nal[1:]))
	if _, err := r.readUE(); err != nil { // first_mb_in_slice
		return ""
	}
	st, err := r.readUE()
	if err != nil {
		return ""
	}
	// slice_type 5..9 mean "all slices in the picture have this type".
	switch st % 5 {
	case 0:
		return "P"
	case 1:
		return "B"
	case 2:
		return "I"
	case 3:
		return "SP"
	case 4:
		return "SI"
	}
	return ""
}

// FrameKindFromNALUs derives the picture type of a frame from its NAL units.
// For H.264 it uses the real slice_type, so B frames are reported as such; for
// H.265 it falls back to the IRAP NAL types, yielding only I or P.
func FrameKindFromNALUs(payload []byte, lengthSize int, isHEVC bool) string {
	nalus := SplitAVCC(payload, lengthSize, isHEVC)
	if len(nalus) == 0 {
		return ""
	}

	if isHEVC {
		for _, n := range nalus {
			if n.Type >= 16 && n.Type <= 21 {
				return "I"
			}
		}
		return "P"
	}

	// A picture may carry several slices; B dominates P, and P dominates I,
	// because the most-dependent slice type describes the picture.
	kind := ""
	off := 0
	for _, n := range nalus {
		off += lengthSize
		nal := payload[off : off+n.Len]
		off += n.Len
		st := H264SliceType(nal)
		switch st {
		case "B":
			return "B"
		case "P":
			kind = "P"
		case "I":
			if kind == "" {
				kind = "I"
			}
		}
	}
	return kind
}

// LengthPrefixedNALToAnnexB converts an AVCC or HVCC access unit into Annex B.
func LengthPrefixedNALToAnnexB(payload []byte, naluLenSize int) ([]byte, error) {
	if naluLenSize < 1 || naluLenSize > 4 {
		naluLenSize = 4
	}
	var out bytes.Buffer
	off := 0
	for off+naluLenSize <= len(payload) {
		n := 0
		for i := 0; i < naluLenSize; i++ {
			n = (n << 8) | int(payload[off+i])
		}
		off += naluLenSize
		if n <= 0 || off+n > len(payload) {
			return nil, fmt.Errorf("invalid nalu size")
		}
		out.Write([]byte{0x00, 0x00, 0x00, 0x01})
		out.Write(payload[off : off+n])
		off += n
	}
	if off != len(payload) {
		return nil, fmt.Errorf("trailing bytes in length-prefixed payload")
	}
	return out.Bytes(), nil
}

// VideoMetaFromNAL parses an Annex B NAL unit when it is an H.264 or H.265 SPS.
func VideoMetaFromNAL(nal []byte) *VideoMeta {
	if len(nal) < 2 {
		return nil
	}
	if int((nal[0]>>1)&0x3f) == 33 {
		if meta, err := parseH265SPS(nal); err == nil {
			return meta
		}
	}
	if int(nal[0]&0x1f) == 7 {
		if meta, err := parseH264SPS(nal); err == nil {
			return meta
		}
	}
	return nil
}

// VideoMetaFromAnnexB returns the first SPS found in an Annex B byte stream.
func VideoMetaFromAnnexB(b []byte) *VideoMeta {
	starts := annexBStarts(b)
	for i, start := range starts {
		sc := 3
		if start+3 < len(b) && b[start] == 0 && b[start+1] == 0 && b[start+2] == 0 && b[start+3] == 1 {
			sc = 4
		}
		nalStart := start + sc
		next := len(b)
		if i+1 < len(starts) {
			next = starts[i+1]
		}
		if nalStart >= next {
			continue
		}
		if meta := VideoMetaFromNAL(b[nalStart:next]); meta != nil {
			return meta
		}
	}
	return nil
}

func annexBStarts(b []byte) []int {
	var idx []int
	for i := 0; i+3 < len(b); i++ {
		if b[i] != 0 || b[i+1] != 0 {
			continue
		}
		if b[i+2] == 1 {
			idx = append(idx, i)
			continue
		}
		if i+3 < len(b) && b[i+2] == 0 && b[i+3] == 1 {
			idx = append(idx, i)
		}
	}
	if len(idx) <= 1 {
		return idx
	}
	out := []int{idx[0]}
	for i := 1; i < len(idx); i++ {
		if idx[i]-idx[i-1] <= 1 {
			continue
		}
		out = append(out, idx[i])
	}
	return out
}
