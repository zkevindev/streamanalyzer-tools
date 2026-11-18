package media

import (
	"bytes"
	"encoding/binary"
	"slices"

	"streamanalyzer/internal/codec"

	"github.com/abema/go-mp4"
)

// InspectCodecs reports codecs observed in MPEG-TS, fMP4, and ADTS segments.
// The caller should skip encrypted media.
func InspectCodecs(segment, init []byte) (video, audio []string) {
	segment, init = skipID3(segment), skipID3(init)
	if isTS(segment) {
		return inspectTSCodecs(segment)
	}
	if isMP4(init) || isMP4(segment) {
		for _, data := range [][]byte{init, segment} {
			inspectMP4Codecs(data, &video, &audio)
		}
	} else if isADTS(segment) {
		audio = []string{"aac"}
	}
	return video, audio
}

func isADTS(data []byte) bool {
	size := adtsFrameSize(data)
	return size > 0 && (size == len(data) || adtsFrameSize(data[size:]) > 0)
}

func adtsFrameSize(data []byte) int {
	if len(data) < 7 || data[0] != 0xff || data[1]&0xf6 != 0xf0 || (data[2]>>2)&0xf > 12 {
		return 0
	}
	header := 7
	if data[1]&1 == 0 {
		header = 9 // CRC present.
	}
	size := int(data[3]&3)<<11 | int(data[4])<<3 | int(data[5]>>5)
	if size <= header || size > len(data) {
		return 0
	}
	return size
}

func inspectTSCodecs(data []byte) (video, audio []string) {
	const maxPMTSectionSize = 1024 // MPEG-TS section_length is at most 1021.
	stride, offset := 188, 0
	if data[0] != 0x47 {
		stride, offset = 192, 4
	}
	pmtPID := -1
	var section []byte
	var nextCounter byte
	for ; offset+188 <= len(data); offset += stride {
		packet := data[offset : offset+188]
		if packet[0] != 0x47 || packet[1]&0x80 != 0 || packet[3]&0xc0 != 0 {
			continue
		}
		pid := int(packet[1]&0x1f)<<8 | int(packet[2])
		if pid != 0 && pid != pmtPID {
			continue
		}
		payload, _, _, _, ok := packetPayload(packet)
		if !ok || len(payload) == 0 {
			continue
		}
		start := packet[1]&0x40 != 0
		if pid == 0 {
			if start && pmtPID < 0 {
				pmtPID = parsePAT(payload)
			}
			continue
		}

		counter := packet[3] & 0x0f
		if start {
			section = section[:0]
			pointer := int(payload[0]) + 1
			if pointer >= len(payload) {
				continue
			}
			payload = payload[pointer:]
		} else if len(section) == 0 || counter != nextCounter {
			section = section[:0]
			continue
		}
		nextCounter = (counter + 1) & 0x0f
		section = append(section, payload[:min(len(payload), maxPMTSectionSize-len(section))]...)
		if len(section) < 3 {
			continue
		}
		size := 3 + (int(section[1]&0x0f)<<8 | int(section[2]))
		if section[0] != 0x02 || section[1]&0x80 == 0 || size < 16 || size > maxPMTSectionSize {
			section = section[:0]
			continue
		}
		if len(section) >= size {
			if section[5]&1 == 0 { // Ignore the next table until it becomes current.
				section = section[:0]
				continue
			}
			return pmtCodecs(section[:size])
		}
	}
	return nil, nil
}

func pmtCodecs(section []byte) (video, audio []string) {
	programDescriptors := int(section[10]&0x0f)<<8 | int(section[11])
	end := len(section) - 4 // CRC
	for offset := 12 + programDescriptors; offset+5 <= end; {
		length := int(section[offset+3]&0x0f)<<8 | int(section[offset+4])
		next := offset + 5 + length
		if next > end {
			return nil, nil
		}
		v, a := tsStreamCodecs(section[offset], section[offset+5:next])
		addCodec(&video, v)
		addCodec(&audio, a)
		offset = next
	}
	return video, audio
}

func tsStreamCodecs(streamType byte, descriptors []byte) (video, audio string) {
	switch streamType {
	case 0x01:
		return "mpeg1video", ""
	case 0x02:
		return "mpeg2video", ""
	case 0x03, 0x04: // The PMT does not identify the MPEG Audio layer.
		return "", "mpeg-audio"
	case 0x06:
		return tsPrivateCodecs(descriptors)
	case 0x0f, 0x11:
		return "", "aac"
	case 0x10:
		return "mpeg4", ""
	case 0x1b:
		return "h264", ""
	case 0x24:
		return "h265", ""
	case 0x33:
		return "h266", ""
	case 0x81:
		return "", "ac3"
	case 0x87:
		return "", "eac3"
	default:
		return "", ""
	}
}

func tsPrivateCodecs(descriptors []byte) (video, audio string) {
	for len(descriptors) >= 2 {
		length := int(descriptors[1])
		if length > len(descriptors)-2 {
			break
		}
		switch descriptors[0] {
		case 0x05: // Registration descriptor.
			if length >= 4 {
				switch string(descriptors[2:6]) {
				case "AV01":
					return "av1", ""
				case "HEVC":
					return "h265", ""
				case "Opus":
					return "", "opus"
				case "AC-3":
					return "", "ac3"
				case "EAC3", "EC-3":
					return "", "eac3"
				case "AC-4":
					return "", "ac4"
				}
			}
		case 0x6a:
			return "", "ac3"
		case 0x7a:
			return "", "eac3"
		}
		descriptors = descriptors[2+length:]
	}
	return "", ""
}

func inspectMP4Codecs(data []byte, video, audio *[]string) {
	walkBoxes(firstBox(data, "moov"), func(typ string, track []byte) {
		if typ != "trak" {
			return
		}
		mdia := firstBox(track, "mdia")
		hdlr := firstBox(mdia, "hdlr")
		if len(hdlr) < 12 {
			return
		}
		handler := string(hdlr[8:12])
		if handler != "vide" && handler != "soun" {
			return
		}
		stsd := firstBox(firstBox(firstBox(mdia, "minf"), "stbl"), "stsd")
		if len(stsd) < 8 || stsd[0] != 0 {
			return
		}
		count := binary.BigEndian.Uint32(stsd[4:8])
		walkBoxes(stsd[8:], func(entry string, payload []byte) {
			if count == 0 {
				return
			}
			count--
			switch {
			case handler == "vide" && len(payload) >= 78: // VisualSampleEntry.
				addCodec(video, codec.VideoCodecName(entry))
			case handler == "soun" && len(payload) >= 28: // AudioSampleEntry.
				if entry == "mp4a" {
					addCodec(audio, mp4aCodec(payload))
				} else {
					addCodec(audio, codec.AudioCodecName(entry))
				}
			}
		})
	})
}

func firstBox(data []byte, name string) []byte {
	var result []byte
	walkBoxes(data, func(typ string, payload []byte) {
		if result == nil && typ == name {
			result = payload
		}
	})
	return result
}

func mp4aCodec(payload []byte) string {
	const generic = "mpeg4-audio"
	children := 28
	switch binary.BigEndian.Uint16(payload[8:10]) {
	case 1:
		children += 16 // QuickTime sound sample description v1.
	case 2:
		children += 36 // QuickTime sound sample description v2.
	}
	if len(payload) <= children {
		return generic
	}
	esds := firstBox(payload[children:], "esds")
	if esds == nil {
		esds = firstBox(firstBox(payload[children:], "wave"), "esds")
	}
	if len(esds) == 0 || len(esds) > 4096 {
		return generic
	}

	var description mp4.Esds
	if _, err := mp4.Unmarshal(bytes.NewReader(esds), uint64(len(esds)), &description, mp4.Context{}); err != nil {
		return generic
	}
	var objectType byte
	var config []byte
	for _, descriptor := range description.Descriptors {
		if descriptor.DecoderConfigDescriptor != nil {
			objectType = descriptor.DecoderConfigDescriptor.ObjectTypeIndication
		}
		if descriptor.Tag == mp4.DecSpecificInfoTag {
			config = descriptor.Data
		}
	}
	switch objectType {
	case 0x66, 0x67, 0x68:
		return "aac"
	case 0x69, 0x6b:
		return "mpeg-audio"
	case 0x40:
		if len(config) == 0 {
			return generic
		}
		audioObjectType := config[0] >> 3
		if audioObjectType == 31 && len(config) > 1 {
			audioObjectType = 32 + ((config[0]&0x07)<<3 | config[1]>>5)
		}
		switch audioObjectType {
		case 1, 2, 3, 4, 5, 6, 17, 19, 20, 23, 29, 39:
			return "aac"
		case 33:
			return "mp2"
		case 34:
			return "mp3"
		}
	}
	return generic
}

func addCodec(codecs *[]string, name string) {
	if name != "" && !slices.Contains(*codecs, name) {
		*codecs = append(*codecs, name)
	}
}
