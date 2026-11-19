package gb28181

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
)

type RTPReview struct {
	Keyframes    int     `json:"keyframes,omitempty"`
	Packets      int     `json:"packets"`
	SequenceGaps int     `json:"sequence_gaps"`
	OutOfOrder   int     `json:"out_of_order"`
	Duplicates   int     `json:"duplicates"`
	PSUnits      int     `json:"ps_units"`
	PartialUnits int     `json:"partial_units"`
	VideoUnits   int     `json:"video_units"`
	AudioUnits   int     `json:"audio_units"`
	VideoType    byte    `json:"video_stream_type,omitempty"`
	AudioType    byte    `json:"audio_stream_type,omitempty"`
	DurationMS   float64 `json:"duration_ms"`
}

func reviewRTP(data []byte) (RTPReview, error) {
	var result RTPReview
	var firstTime, lastTime uint64
	var highest, lowest int
	seen := make(map[int][]byte)
	for len(data) > 0 {
		if len(data) < 10 {
			return result, errors.New("RTP 采样索引不完整")
		}
		at := binary.BigEndian.Uint64(data[:8])
		size := int(binary.BigEndian.Uint16(data[8:10]))
		if size < 12 || size > len(data)-10 || result.Packets != 0 && at < lastTime {
			return result, errors.New("RTP 采样索引有误")
		}
		packet := data[10 : 10+size]
		data = data[10+size:]
		if packet[0] != 0x80 {
			return result, errors.New("RTP 采样包含无法解析的包头")
		}
		seq := binary.BigEndian.Uint16(packet[2:4])
		if result.Packets == 0 {
			firstTime = at
			highest, lowest = int(seq), int(seq)
		}
		extended := highest + int(int16(seq-uint16(highest)))
		if _, duplicate := seen[extended]; duplicate {
			result.Duplicates++
		} else {
			if extended < highest {
				result.OutOfOrder++
			}
			seen[extended] = packet
		}
		highest, lowest = max(highest, extended), min(lowest, extended)
		result.Packets++
		lastTime = at
	}
	keys := make([]int, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var stamp uint32
	var ps []byte
	for _, key := range keys {
		packet := seen[key]
		clock := binary.BigEndian.Uint32(packet[4:8])
		if stamp != clock && len(ps) != 0 {
			result.PartialUnits++
			ps = ps[:0]
		}
		stamp = clock
		ps = append(ps, packet[12:]...)
		if packet[1]&0x80 != 0 {
			result.reviewPS(ps)
			ps = ps[:0]
		}
	}
	if len(ps) > 0 {
		result.PartialUnits++
	}
	if result.Packets > 0 {
		result.SequenceGaps = highest - lowest + 1 - len(seen)
		result.DurationMS = float64(lastTime-firstTime) / 1e6
	}
	return result, nil
}

func (r *RTPReview) reviewPS(ps []byte) {
	if len(ps) < 14 || !bytes.HasPrefix(ps, []byte{0, 0, 1, 0xba}) {
		r.PartialUnits++
		return
	}
	video, audio, key := false, false, false
	offset := 14 + int(ps[13]&7)
	if offset > len(ps) {
		r.PartialUnits++
		return
	}
	for ps = ps[offset:]; len(ps) > 0; {
		if len(ps) < 6 || !bytes.HasPrefix(ps, []byte{0, 0, 1}) {
			r.PartialUnits++
			return
		}
		size := int(binary.BigEndian.Uint16(ps[4:6]))
		if size <= 0 || size > len(ps)-6 {
			r.PartialUnits++
			return
		}
		switch ps[3] {
		case videoStreamID:
			video = true
			pes := ps[6 : 6+size]
			if len(pes) >= 3 && 3+int(pes[2]) <= len(pes) {
				data := pes[3+int(pes[2]):]
				switch r.VideoType {
				case 0x24:
					key = key || containsRandomAccess(data, "hevc")
				case 0x1b, 0:
					key = key || containsRandomAccess(data, "h264")
				}
			}
		case audioStreamID:
			audio = true
		case 0xbc:
			r.readProgramMap(ps[6 : 6+size])
		}
		ps = ps[6+size:]
	}
	r.PSUnits++
	if key {
		r.Keyframes++
	}
	if video {
		r.VideoUnits++
	}
	if audio {
		r.AudioUnits++
	}
}

func (r *RTPReview) readProgramMap(body []byte) {
	if len(body) < 10 {
		return
	}
	offset := 4 + int(binary.BigEndian.Uint16(body[2:4]))
	if offset+2 > len(body)-4 {
		return
	}
	size := int(binary.BigEndian.Uint16(body[offset : offset+2]))
	if offset+2+size > len(body)-4 {
		return
	}
	for list := body[offset+2 : offset+2+size]; len(list) >= 4; {
		length := 4 + int(binary.BigEndian.Uint16(list[2:4]))
		if length > len(list) {
			return
		}
		switch list[1] {
		case videoStreamID:
			r.VideoType = list[0]
		case audioStreamID:
			r.AudioType = list[0]
		}
		list = list[length:]
	}
}
