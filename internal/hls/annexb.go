package hls

// Annex B NALU 切分（与 offline_ts / parse_ts 一致），用于在 TS PES 载荷内检测 IDR。

func splitAnnexBNALUs(b []byte) [][]byte {
	var out [][]byte
	starts := findStartCodes(b)
	if len(starts) == 0 {
		return out
	}
	for i := 0; i < len(starts); i++ {
		start := starts[i]
		next := len(b)
		if i+1 < len(starts) {
			next = starts[i+1]
		}
		scLen := startCodeLenAt(b, start)
		naluStart := start + scLen
		if naluStart >= next {
			continue
		}
		out = append(out, b[naluStart:next])
	}
	return out
}

func findStartCodes(b []byte) []int {
	var idx []int
	for i := 0; i+3 < len(b); i++ {
		if b[i] != 0x00 || b[i+1] != 0x00 {
			continue
		}
		if b[i+2] == 0x01 {
			idx = append(idx, i)
		} else if i+3 < len(b) && b[i+2] == 0x00 && b[i+3] == 0x01 {
			idx = append(idx, i)
		}
	}
	return dedupStartIndex(idx)
}

func dedupStartIndex(in []int) []int {
	if len(in) <= 1 {
		return in
	}
	out := make([]int, 0, len(in))
	out = append(out, in[0])
	for i := 1; i < len(in); i++ {
		if in[i]-in[i-1] <= 1 {
			continue
		}
		out = append(out, in[i])
	}
	return out
}

func startCodeLenAt(b []byte, i int) int {
	if i+3 < len(b) && b[i] == 0x00 && b[i+1] == 0x00 && b[i+2] == 0x00 && b[i+3] == 0x01 {
		return 4
	}
	return 3
}

func h264NALUType(nalu []byte) uint8 {
	if len(nalu) == 0 {
		return 0
	}
	return nalu[0] & 0x1F
}

func h265NALUType(nalu []byte) uint8 {
	if len(nalu) < 2 {
		return 0
	}
	return (nalu[0] >> 1) & 0x3F
}

func isH265KeyframeNALUType(t uint8) bool {
	return (t >= 16 && t <= 21) || t == 19 || t == 20
}
