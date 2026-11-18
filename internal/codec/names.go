package codec

import "strings"

// VideoCodecName maps known sample-entry FourCCs and FLV codec names to the
// names used in diagnostic reports. Unknown values are intentionally omitted.
func VideoCodecName(value string) string {
	switch strings.ToLower(value) {
	case "avc1", "avc2", "avc3", "avc4", "h264":
		return "h264"
	case "hvc1", "hev1", "hvc2", "hev2", "dvh1", "dvhe", "h265":
		return "h265"
	case "vvc1", "vvi1", "h266":
		return "h266"
	case "av01", "av1":
		return "av1"
	case "vp08", "vp8":
		return "vp8"
	case "vp09", "vp9":
		return "vp9"
	case "mp4v", "mpeg4":
		return "mpeg4"
	case "s263", "h263":
		return "h263"
	case "jpeg", "mjpa", "mjpb", "mjpeg":
		return "mjpeg"
	case "sorenson-h263", "screenvideo", "vp6", "vp6-alpha", "screenvideo2":
		return strings.ToLower(value)
	default:
		return ""
	}
}

// AudioCodecName maps known sample-entry FourCCs and FLV codec names. MP4
// callers can refine mp4a using its MPEG-4 DecoderConfigDescriptor.
func AudioCodecName(value string) string {
	switch strings.ToLower(value) {
	case "mp4a", "mpeg4-audio":
		return "mpeg4-audio"
	case ".mp3", "mp3":
		return "mp3"
	case "opus":
		return "opus"
	case "ac-3", "ac3":
		return "ac3"
	case "ec-3", "eac3":
		return "eac3"
	case "flac":
		return "flac"
	case "alac":
		return "alac"
	case "alaw", "g711-alaw":
		return "g711-alaw"
	case "ulaw", "g711-mulaw":
		return "g711-mulaw"
	case "ipcm", "fpcm", "lpcm", "sowt", "twos", "in24", "in32", "pcm":
		return "pcm"
	case "aac", "adpcm", "nellymoser", "speex", "ac4", "mp2":
		return strings.ToLower(value)
	case "ac-4":
		return "ac4"
	default:
		return ""
	}
}
