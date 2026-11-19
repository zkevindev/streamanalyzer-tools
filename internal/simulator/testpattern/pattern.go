package testpattern

import (
	"bytes"
	_ "embed"
)

//go:generate ffmpeg -hide_banner -loglevel error -y -f lavfi -i testsrc2=size=320x180:rate=15 -t 2 -c:v libx264 -preset medium -crf 28 -pix_fmt yuv420p -x264-params aud=1:repeat-headers=1:keyint=30:scenecut=0:bframes=0 -f h264 pattern.h264
//go:embed pattern.h264
var annexB []byte

//go:generate ffmpeg -hide_banner -loglevel error -y -f lavfi -i testsrc2=size=320x180:rate=15 -t 2 -c:v libx264 -threads 2 -preset medium -crf 30 -pix_fmt yuv420p -profile:v baseline -level:v 3.1 -x264-params aud=1:repeat-headers=1:keyint=30:scenecut=0:bframes=0 -f h264 baseline.h264
//go:embed baseline.h264
var baselineAnnexB []byte

const (
	Width  = 320
	Height = 180
	FPS    = 15
)

var offsets = frameOffsets(annexB)
var baselineOffsets = frameOffsets(baselineAnnexB)

func frameOffsets(data []byte) []int {
	// The fixture starts every access unit with a four-byte Annex B start
	// code and an H.264 Access Unit Delimiter.
	aud := []byte{0, 0, 0, 1, 9}
	var starts []int
	for offset := 0; ; {
		index := bytes.Index(data[offset:], aud)
		if index == -1 {
			return append(starts, len(data))
		}
		start := offset + index
		starts = append(starts, start)
		offset = start + len(aud)
	}
}

func Len() int {
	return len(offsets) - 1
}

// AccessUnit returns read-only Annex B bytes from the embedded test pattern.
func AccessUnit(index int) []byte {
	return annexB[offsets[index]:offsets[index+1]:offsets[index+1]]
}

func BaselineLen() int {
	return len(baselineOffsets) - 1
}

// BaselineAccessUnit returns read-only Annex B bytes for the WebRTC test video.
func BaselineAccessUnit(index int) []byte {
	return baselineAnnexB[baselineOffsets[index]:baselineOffsets[index+1]:baselineOffsets[index+1]]
}
