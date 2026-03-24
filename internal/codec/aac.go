package codec

import "fmt"

type AudioMeta struct {
	SampleRate int
	Channels   int
}

var aacSampleRates = []int{
	96000, 88200, 64000, 48000, 44100, 32000, 24000,
	22050, 16000, 12000, 11025, 8000, 7350,
}

func ParseAACAudioSpecificConfig(payload []byte) (*AudioMeta, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("aac asc too short")
	}
	b0 := payload[0]
	b1 := payload[1]

	audioObjectType := int((b0 & 0xF8) >> 3)
	if audioObjectType == 31 && len(payload) >= 3 {
		audioObjectType = 32 + int((b0&0x07)<<3) + int((b1&0xE0)>>5)
	}
	_ = audioObjectType // currently not needed for display

	freqIdx := int((b0&0x07)<<1 | (b1&0x80)>>7)
	sampleRate := 0
	if freqIdx == 0x0f {
		if len(payload) < 5 {
			return nil, fmt.Errorf("aac asc missing explicit sample rate")
		}
		sampleRate = int(payload[1]&0x7f)<<17 | int(payload[2])<<9 | int(payload[3])<<1 | int(payload[4]>>7)
	} else if freqIdx >= 0 && freqIdx < len(aacSampleRates) {
		sampleRate = aacSampleRates[freqIdx]
	}

	channels := int((b1 & 0x78) >> 3)
	return &AudioMeta{
		SampleRate: sampleRate,
		Channels:   channels,
	}, nil
}
