package codec

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/zaf/g711"
)

// AV1Meta holds decoded fields from an AV1 Sequence Header OBU (obu_type == 1).
type AV1Meta struct {
	SeqProfile   int    `json:"seq_profile"`
	ProfileName  string `json:"profile_name"`
	SeqLevelIdx  int    `json:"seq_level_idx"`
	LevelName    string `json:"level_name"`
	BitDepth     int    `json:"bit_depth"`
	ChromaFormat string `json:"chroma_format"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Monochrome   bool   `json:"monochrome,omitempty"`
}

func AV1ProfileName(p int) string {
	switch p {
	case 0:
		return "Main"
	case 1:
		return "High"
	case 2:
		return "Professional"
	default:
		return fmt.Sprintf("Profile %d", p)
	}
}

func AV1LevelName(idx int) string {
	if idx < 0 || idx > 31 {
		return ""
	}
	major := 2 + (idx >> 2)
	minor := idx & 3
	return fmt.Sprintf("%d.%d", major, minor)
}

// ParseAV1SequenceHeader scans an AV1 bitstream (raw OBU sequence or av1C configOBUs)
// for a Sequence Header OBU (obu_type == 1) and parses profile, level, dimensions,
// bit depth and chroma subsampling.
func ParseAV1SequenceHeader(data []byte) (*AV1Meta, error) {
	// If data looks like ISO BMFF av1C (4-byte header + configOBUs):
	// marker(1)=1, version(7)=1 -> byte0 == 0x81
	if len(data) >= 6 && data[0] == 0x81 {
		if m, err := parseAV1OBUStream(data[4:]); err == nil && m != nil {
			return m, nil
		}
	}
	return parseAV1OBUStream(data)
}

func parseAV1OBUStream(data []byte) (*AV1Meta, error) {
	off := 0
	for off < len(data) {
		hdr := data[off]
		off++
		if (hdr & 0x80) != 0 { // obu_forbidden_bit must be 0
			return nil, fmt.Errorf("invalid AV1 OBU forbidden bit")
		}
		obuType := (hdr >> 3) & 0x0f
		hasExt := (hdr & 0x04) != 0
		hasSize := (hdr & 0x02) != 0
		if hasExt {
			if off >= len(data) {
				break
			}
			off++
		}
		var payload []byte
		if hasSize {
			sz, n, ok := readAV1LEB128(data[off:])
			if !ok {
				break
			}
			off += n
			if off+int(sz) > len(data) {
				payload = data[off:]
				off = len(data)
			} else {
				payload = data[off : off+int(sz)]
				off += int(sz)
			}
		} else {
			payload = data[off:]
			off = len(data)
		}

		if obuType == 1 && len(payload) >= 4 {
			return decodeAV1SequenceHeaderPayload(payload)
		}
	}
	return nil, fmt.Errorf("AV1 Sequence Header OBU not found")
}

func readAV1LEB128(b []byte) (uint64, int, bool) {
	var val uint64
	for i := 0; i < 8 && i < len(b); i++ {
		val |= uint64(b[i]&0x7f) << (7 * i)
		if (b[i] & 0x80) == 0 {
			return val, i + 1, true
		}
	}
	return 0, 0, false
}

func decodeAV1SequenceHeaderPayload(payload []byte) (*AV1Meta, error) {
	r := newBitReader(payload)
	seqProfile, err := r.readBits(3)
	if err != nil {
		return nil, err
	}
	if _, err := r.readBit(); err != nil { // still_picture
		return nil, err
	}
	reducedStill, err := r.readBit()
	if err != nil {
		return nil, err
	}

	var seqLevelIdx uint64
	if reducedStill == 1 {
		seqLevelIdx, err = r.readBits(5)
		if err != nil {
			return nil, err
		}
	} else {
		timingPresent, err := r.readBit()
		if err != nil {
			return nil, err
		}
		var decoderModelPresent uint64
		var bufferDelayLen uint64
		if timingPresent == 1 {
			if err := r.skipBits(64); err != nil { // num_units_in_display_tick + time_scale
				return nil, err
			}
			equalPic, err := r.readBit()
			if err != nil {
				return nil, err
			}
			if equalPic == 1 {
				if err := skipAV1UVLC(r); err != nil {
					return nil, err
				}
			}
			dmBit, err := r.readBit()
			if err != nil {
				return nil, err
			}
			decoderModelPresent = uint64(dmBit)
			if decoderModelPresent == 1 {
				bufferDelayLen, err = r.readBits(5)
				if err != nil {
					return nil, err
				}
				bufferDelayLen++
				if err := r.skipBits(32 + 10); err != nil { // num_units_in_decoding_tick + buffer_removal_time_length(5) + frame_presentation_time_length(5)
					return nil, err
				}
			}
		}
		initialDisplayDelayPresent, err := r.readBit()
		if err != nil {
			return nil, err
		}
		opPointsMinus1, err := r.readBits(5)
		if err != nil {
			return nil, err
		}
		for i := uint64(0); i <= opPointsMinus1; i++ {
			if err := r.skipBits(12); err != nil { // operating_point_idc
				return nil, err
			}
			lvl, err := r.readBits(5)
			if err != nil {
				return nil, err
			}
			if i == 0 {
				seqLevelIdx = lvl
			}
			if lvl > 7 {
				if _, err := r.readBit(); err != nil { // seq_tier
					return nil, err
				}
			}
			if decoderModelPresent == 1 {
				dmForOp, err := r.readBit()
				if err != nil {
					return nil, err
				}
				if dmForOp == 1 {
					if err := r.skipBits(int(bufferDelayLen*2 + 1)); err != nil {
						return nil, err
					}
				}
			}
			if initialDisplayDelayPresent == 1 {
				iddForOp, err := r.readBit()
				if err != nil {
					return nil, err
				}
				if iddForOp == 1 {
					if err := r.skipBits(4); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	wBitsMinus1, err := r.readBits(4)
	if err != nil {
		return nil, err
	}
	hBitsMinus1, err := r.readBits(4)
	if err != nil {
		return nil, err
	}
	maxWMinus1, err := r.readBits(int(wBitsMinus1 + 1))
	if err != nil {
		return nil, err
	}
	maxHMinus1, err := r.readBits(int(hBitsMinus1 + 1))
	if err != nil {
		return nil, err
	}

	meta := &AV1Meta{
		SeqProfile:   int(seqProfile),
		ProfileName:  AV1ProfileName(int(seqProfile)),
		SeqLevelIdx:  int(seqLevelIdx),
		LevelName:    AV1LevelName(int(seqLevelIdx)),
		Width:        int(maxWMinus1 + 1),
		Height:       int(maxHMinus1 + 1),
		BitDepth:     8,
		ChromaFormat: "4:2:0",
	}

	// Continue best-effort into color_config for bit depth & chroma subsampling.
	if reducedStill == 0 {
		frameIDPresent, err := r.readBit()
		if err != nil {
			return meta, nil
		}
		if frameIDPresent == 1 {
			if err := r.skipBits(7); err != nil {
				return meta, nil
			}
		}
	}
	if _, err := r.readBit(); err != nil { // use_128x128_superblock
		return meta, nil
	}
	if err := r.skipBits(2); err != nil { // enable_filter_intra, enable_intra_edge_filter
		return meta, nil
	}
	if reducedStill == 0 {
		if err := r.skipBits(4); err != nil {
			return meta, nil
		}
		enableOrderHint, err := r.readBit()
		if err != nil {
			return meta, nil
		}
		if enableOrderHint == 1 {
			if err := r.skipBits(2); err != nil {
				return meta, nil
			}
		}
		seqChooseScreen, err := r.readBit()
		if err != nil {
			return meta, nil
		}
		var seqForceScreen uint64 = 2
		if seqChooseScreen == 0 {
			fsBit, err := r.readBit()
			if err != nil {
				return meta, nil
			}
			seqForceScreen = uint64(fsBit)
		}
		if seqForceScreen > 0 {
			seqChooseIntMV, err := r.readBit()
			if err != nil {
				return meta, nil
			}
			if seqChooseIntMV == 0 {
				if _, err := r.readBit(); err != nil {
					return meta, nil
				}
			}
		}
		if enableOrderHint == 1 {
			if err := r.skipBits(3); err != nil {
				return meta, nil
			}
		}
	}
	if err := r.skipBits(3); err != nil { // superres, cdef, restoration
		return meta, nil
	}

	// color_config()
	highBitDepth, err := r.readBit()
	if err != nil {
		return meta, nil
	}
	if seqProfile == 2 && highBitDepth == 1 {
		twelveBit, err := r.readBit()
		if err != nil {
			return meta, nil
		}
		if twelveBit == 1 {
			meta.BitDepth = 12
		} else {
			meta.BitDepth = 10
		}
	} else if highBitDepth == 1 {
		meta.BitDepth = 10
	}
	if seqProfile == 1 {
		meta.ChromaFormat = "4:4:4"
	} else {
		mono, err := r.readBit()
		if err != nil {
			return meta, nil
		}
		if mono == 1 {
			meta.Monochrome = true
			meta.ChromaFormat = "Monochrome"
		} else {
			colorDesc, err := r.readBit()
			if err != nil {
				return meta, nil
			}
			if colorDesc == 1 {
				if err := r.skipBits(24); err != nil {
					return meta, nil
				}
			}
			if _, err := r.readBit(); err != nil { // color_range
				return meta, nil
			}
			if seqProfile == 0 {
				meta.ChromaFormat = "4:2:0"
			} else if seqProfile == 2 {
				sx, err1 := r.readBit()
				sy, err2 := r.readBit()
				if err1 == nil && err2 == nil {
					switch {
					case sx == 1 && sy == 1:
						meta.ChromaFormat = "4:2:0"
					case sx == 1 && sy == 0:
						meta.ChromaFormat = "4:2:2"
					default:
						meta.ChromaFormat = "4:4:4"
					}
				}
			}
		}
	}

	return meta, nil
}

func skipAV1UVLC(r *bitReader) error {
	leading := 0
	for {
		b, err := r.readBit()
		if err != nil {
			return err
		}
		if b == 1 {
			break
		}
		leading++
		if leading > 32 {
			return fmt.Errorf("uvlc overflow")
		}
	}
	if leading == 0 {
		return nil
	}
	return r.skipBits(leading)
}

// WriteG711WAV decodes raw G.711 A-law ("pcma"/"alaw") or Mu-law ("pcmu"/"ulaw")
// samples (8 kHz, mono, 16-bit PCM) into a standard RIFF WAVE file so it can be
// played directly in browsers or desktop players.
func WriteG711WAV(w io.Writer, raw []byte, codecName string) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty G.711 payload")
	}
	low := strings.ToLower(strings.TrimSpace(codecName))
	var pcm []byte
	if strings.Contains(low, "ulaw") || strings.Contains(low, "pcmu") || strings.Contains(low, "mu") {
		pcm = g711.DecodeUlaw(raw)
	} else {
		pcm = g711.DecodeAlaw(raw)
	}

	dataSize := uint32(len(pcm))
	const (
		sampleRate    = 8000
		numChannels   = 1
		bitsPerSample = 16
	)
	byteRate := uint32(sampleRate * numChannels * bitsPerSample / 8)
	blockAlign := uint16(numChannels * bitsPerSample / 8)

	var hdr [44]byte
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], 36+dataSize)
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:24], numChannels)
	binary.LittleEndian.PutUint32(hdr[24:28], sampleRate)
	binary.LittleEndian.PutUint32(hdr[28:32], byteRate)
	binary.LittleEndian.PutUint16(hdr[32:34], blockAlign)
	binary.LittleEndian.PutUint16(hdr[34:36], bitsPerSample)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], dataSize)

	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(pcm)
	return err
}
