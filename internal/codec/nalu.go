package codec

import "encoding/binary"

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
