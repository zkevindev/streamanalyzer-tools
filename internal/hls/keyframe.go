package hls

import (
	"github.com/asticode/go-astits"
)

func isKeyframeAccessUnit(payload []byte, st astits.StreamType) bool {
	nalus := splitAnnexBNALUs(payload)
	for _, nal := range nalus {
		if len(nal) == 0 {
			continue
		}
		switch st {
		case astits.StreamTypeH264Video:
			if h264NALUType(nal) == 5 {
				return true
			}
		case astits.StreamTypeH265Video:
			t := h265NALUType(nal)
			if isH265KeyframeNALUType(t) {
				return true
			}
		}
	}
	return false
}
