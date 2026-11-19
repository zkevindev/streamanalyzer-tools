package diagnostic

import (
	"encoding/binary"
	"errors"
)

var errRTSPOpusPacket = errors.New("invalid Opus packet")

type rtspOpusPreview struct {
	channels int
	ready    bool
	hasPTS   bool
	nextPTS  int64
}

func newRTSPOpusPreview(channels int) (*rtspOpusPreview, error) {
	if _, err := rtspOpusHead(channels); err != nil {
		return nil, err
	}
	return &rtspOpusPreview{channels: channels}, nil
}

func (opus *rtspOpusPreview) write(preview *rtspPreview, item rtspPreviewPacket) {
	samples, err := rtspOpusPacketSamples(item.packet.Payload)
	if err != nil || opus.hasPTS && item.pts < opus.nextPTS {
		preview.packetError()
		return
	}
	// PacketPTS is shared with the selected video track. Never independently
	// zero the audio clock or close a DTX/loss gap between encoded packets.
	at := item.pts/48 + 2000
	if !opus.ready {
		header, _ := rtspOpusHead(opus.channels)
		if !preview.tag(8, at, append([]byte{0x90, 'O', 'p', 'u', 's'}, header...)) {
			return
		}
		opus.ready = true
	}
	if preview.tag(8, at, append([]byte{0x91, 'O', 'p', 'u', 's'}, item.packet.Payload...)) {
		opus.nextPTS, opus.hasPTS = item.pts+samples, true
		preview.output(false)
	}
}

// rtspOpusHead describes live RTP without an Ogg encoder delay. RTP does not
// carry pre-skip or output gain; neither can be inferred from packet arrival.
// Only RFC 7845 mapping family zero (mono/stereo) is supported by this bridge.
func rtspOpusHead(channels int) ([]byte, error) {
	if channels < 1 || channels > 2 {
		return nil, errRTSPOpusPacket
	}
	header := make([]byte, 19)
	copy(header, "OpusHead")
	header[8], header[9] = 1, byte(channels)
	binary.LittleEndian.PutUint32(header[12:16], 48000)
	return header, nil
}

// rtspOpusPacketSamples validates RFC 6716 section 3 packet framing and returns
// the encoded packet duration in the RTP clock's mandatory 48000 Hz units.
// A valid header alone cannot prove that a browser decoded the compressed data.
func rtspOpusPacketSamples(packet []byte) (int64, error) {
	if len(packet) == 0 || len(packet) > 64<<10 {
		return 0, errRTSPOpusPacket
	}
	frameSamples := [...]int64{
		480, 960, 1920, 2880, // SILK narrowband
		480, 960, 1920, 2880, // SILK medium band
		480, 960, 1920, 2880, // SILK wideband
		480, 960, // Hybrid superwideband
		480, 960, // Hybrid fullband
		120, 240, 480, 960, // CELT narrowband
		120, 240, 480, 960, // CELT wideband
		120, 240, 480, 960, // CELT superwideband
		120, 240, 480, 960, // CELT fullband
	}[packet[0]>>3]
	position, end := 1, len(packet)
	readSize := func() (int, bool) {
		if position >= end {
			return 0, false
		}
		first := int(packet[position])
		position++
		if first < 252 {
			return first, true
		}
		if position >= end {
			return 0, false
		}
		size := first + 4*int(packet[position])
		position++
		return size, true
	}
	frames := 1
	switch packet[0] & 3 {
	case 0:
		if end-position > 1275 {
			return 0, errRTSPOpusPacket
		}
	case 1:
		frames = 2
		if (end-position)%2 != 0 || (end-position)/2 > 1275 {
			return 0, errRTSPOpusPacket
		}
	case 2:
		frames = 2
		first, ok := readSize()
		if !ok || first > end-position || end-position-first > 1275 {
			return 0, errRTSPOpusPacket
		}
	case 3:
		if position >= end {
			return 0, errRTSPOpusPacket
		}
		control := packet[position]
		position++
		frames = int(control & 63)
		if frames == 0 || frames > 48 || frameSamples*int64(frames) > 5760 {
			return 0, errRTSPOpusPacket
		}
		if control&0x40 != 0 {
			padding := 0
			for {
				if position >= end {
					return 0, errRTSPOpusPacket
				}
				value := int(packet[position])
				position++
				if value == 255 {
					padding += 254
				} else {
					padding += value
				}
				if padding > end-position {
					return 0, errRTSPOpusPacket
				}
				if value != 255 {
					break
				}
			}
			end -= padding
		}
		if control&0x80 == 0 {
			if (end-position)%frames != 0 || (end-position)/frames > 1275 {
				return 0, errRTSPOpusPacket
			}
		} else {
			sizes := 0
			for index := 0; index < frames-1; index++ {
				value, ok := readSize()
				if !ok {
					return 0, errRTSPOpusPacket
				}
				sizes += value
			}
			if sizes > end-position || end-position-sizes > 1275 {
				return 0, errRTSPOpusPacket
			}
		}
	}
	return frameSamples * int64(frames), nil
}
