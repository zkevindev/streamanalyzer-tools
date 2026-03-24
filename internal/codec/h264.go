package codec

import (
	"encoding/binary"
	"fmt"
)

type VideoMeta struct {
	Width     int
	Height    int
	FrameRate float64
}

// AVCDecoderConfigurationRecord corresponds to ISO/IEC 14496-15 avcC box payload.
type AVCDecoderConfigurationRecord struct {
	ConfigurationVersion uint8
	AVCProfileIndication uint8
	ProfileCompatibility uint8
	AVCLevelIndication   uint8
	LengthSizeMinusOne   uint8
	SequenceParameterSets [][]byte
	PictureParameterSets  [][]byte
}

func ParseAVCDecoderConfigurationRecord(payload []byte) (*AVCDecoderConfigurationRecord, error) {
	if len(payload) < 7 {
		return nil, fmt.Errorf("avc config too short")
	}
	rec := &AVCDecoderConfigurationRecord{
		ConfigurationVersion: payload[0],
		AVCProfileIndication: payload[1],
		ProfileCompatibility: payload[2],
		AVCLevelIndication:   payload[3],
		LengthSizeMinusOne:   payload[4] & 0x03,
	}

	off := 5
	numSPS := int(payload[off] & 0x1f)
	off++
	for i := 0; i < numSPS; i++ {
		if len(payload) < off+2 {
			return nil, fmt.Errorf("avc config truncated before sps length")
		}
		spsLen := int(binary.BigEndian.Uint16(payload[off : off+2]))
		off += 2
		if len(payload) < off+spsLen {
			return nil, fmt.Errorf("avc config truncated sps")
		}
		rec.SequenceParameterSets = append(rec.SequenceParameterSets, payload[off:off+spsLen])
		off += spsLen
	}

	if len(payload) < off+1 {
		return nil, fmt.Errorf("avc config truncated before pps count")
	}
	numPPS := int(payload[off])
	off++
	for i := 0; i < numPPS; i++ {
		if len(payload) < off+2 {
			return nil, fmt.Errorf("avc config truncated before pps length")
		}
		ppsLen := int(binary.BigEndian.Uint16(payload[off : off+2]))
		off += 2
		if len(payload) < off+ppsLen {
			return nil, fmt.Errorf("avc config truncated pps")
		}
		rec.PictureParameterSets = append(rec.PictureParameterSets, payload[off:off+ppsLen])
		off += ppsLen
	}
	return rec, nil
}

func ParseAVCDecoderConfig(payload []byte) (*VideoMeta, error) {
	rec, err := ParseAVCDecoderConfigurationRecord(payload)
	if err != nil {
		return nil, err
	}
	if len(rec.SequenceParameterSets) == 0 {
		return nil, fmt.Errorf("avc config has no sps")
	}
	return parseH264SPS(rec.SequenceParameterSets[0])
}

func parseH264SPS(nal []byte) (*VideoMeta, error) {
	if len(nal) < 2 {
		return nil, fmt.Errorf("h264 sps too short")
	}
	r := newBitReader(rbspFromNAL(nal[1:])) // skip nal header

	profileIDC, err := r.readBits(8)
	if err != nil {
		return nil, err
	}
	if err := r.skipBits(8); err != nil { // constraint flags + reserved
		return nil, err
	}
	if _, err := r.readBits(8); err != nil { // level_idc
		return nil, err
	}
	if _, err := r.readUE(); err != nil { // seq_parameter_set_id
		return nil, err
	}

	chromaFormatIDC := uint64(1)
	switch profileIDC {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		chromaFormatIDC, err = r.readUE()
		if err != nil {
			return nil, err
		}
		if chromaFormatIDC == 3 {
			if err := r.skipBits(1); err != nil { // separate_colour_plane_flag
				return nil, err
			}
		}
		if _, err := r.readUE(); err != nil { // bit_depth_luma_minus8
			return nil, err
		}
		if _, err := r.readUE(); err != nil { // bit_depth_chroma_minus8
			return nil, err
		}
		if err := r.skipBits(1); err != nil { // qpprime_y_zero_transform_bypass_flag
			return nil, err
		}
		seqScalingMatrix, err := r.readBit()
		if err != nil {
			return nil, err
		}
		if seqScalingMatrix == 1 {
			n := 8
			if chromaFormatIDC == 3 {
				n = 12
			}
			for i := 0; i < n; i++ {
				present, err := r.readBit()
				if err != nil {
					return nil, err
				}
				if present == 1 {
					size := 16
					if i >= 6 {
						size = 64
					}
					lastScale := int64(8)
					nextScale := int64(8)
					for j := 0; j < size; j++ {
						if nextScale != 0 {
							delta, err := r.readSE()
							if err != nil {
								return nil, err
							}
							nextScale = (lastScale + delta + 256) % 256
						}
						if nextScale != 0 {
							lastScale = nextScale
						}
					}
				}
			}
		}
	}

	if _, err := r.readUE(); err != nil { // log2_max_frame_num_minus4
		return nil, err
	}
	picOrderCntType, err := r.readUE()
	if err != nil {
		return nil, err
	}
	if picOrderCntType == 0 {
		if _, err := r.readUE(); err != nil { // log2_max_pic_order_cnt_lsb_minus4
			return nil, err
		}
	} else if picOrderCntType == 1 {
		if err := r.skipBits(1); err != nil { // delta_pic_order_always_zero_flag
			return nil, err
		}
		if _, err := r.readSE(); err != nil {
			return nil, err
		}
		if _, err := r.readSE(); err != nil {
			return nil, err
		}
		n, err := r.readUE()
		if err != nil {
			return nil, err
		}
		for i := uint64(0); i < n; i++ {
			if _, err := r.readSE(); err != nil {
				return nil, err
			}
		}
	}
	if _, err := r.readUE(); err != nil { // max_num_ref_frames
		return nil, err
	}
	if err := r.skipBits(1); err != nil { // gaps_in_frame_num_value_allowed_flag
		return nil, err
	}
	picWidthInMbsMinus1, err := r.readUE()
	if err != nil {
		return nil, err
	}
	picHeightInMapUnitsMinus1, err := r.readUE()
	if err != nil {
		return nil, err
	}
	frameMbsOnlyFlag, err := r.readBit()
	if err != nil {
		return nil, err
	}
	if frameMbsOnlyFlag == 0 {
		if err := r.skipBits(1); err != nil { // mb_adaptive_frame_field_flag
			return nil, err
		}
	}
	if err := r.skipBits(1); err != nil { // direct_8x8_inference_flag
		return nil, err
	}
	cropFlag, err := r.readBit()
	if err != nil {
		return nil, err
	}
	var cropLeft, cropRight, cropTop, cropBottom uint64
	if cropFlag == 1 {
		if cropLeft, err = r.readUE(); err != nil {
			return nil, err
		}
		if cropRight, err = r.readUE(); err != nil {
			return nil, err
		}
		if cropTop, err = r.readUE(); err != nil {
			return nil, err
		}
		if cropBottom, err = r.readUE(); err != nil {
			return nil, err
		}
	}

	width := int((picWidthInMbsMinus1+1)*16 - 2*(cropLeft+cropRight))
	heightFactor := uint64(2)
	if frameMbsOnlyFlag == 1 {
		heightFactor = 1
	}
	height := int((picHeightInMapUnitsMinus1+1)*16*heightFactor - 2*(cropTop+cropBottom))

	fps := 0.0
	vuiFlag, err := r.readBit()
	if err == nil && vuiFlag == 1 {
		fps = parseH264VUIFrameRate(r)
	}

	return &VideoMeta{
		Width:     width,
		Height:    height,
		FrameRate: fps,
	}, nil
}

func parseH264VUIFrameRate(r *bitReader) float64 {
	aspectFlag, err := r.readBit()
	if err != nil {
		return 0
	}
	if aspectFlag == 1 {
		idc, err := r.readBits(8)
		if err != nil {
			return 0
		}
		if idc == 255 {
			if err := r.skipBits(32); err != nil {
				return 0
			}
		}
	}
	overscan, _ := r.readBit()
	if overscan == 1 {
		_ = r.skipBits(1)
	}
	videoSignal, _ := r.readBit()
	if videoSignal == 1 {
		if err := r.skipBits(4); err != nil {
			return 0
		}
		colourDesc, _ := r.readBit()
		if colourDesc == 1 {
			if err := r.skipBits(24); err != nil {
				return 0
			}
		}
	}
	chromaLoc, _ := r.readBit()
	if chromaLoc == 1 {
		if _, err := r.readUE(); err != nil {
			return 0
		}
		if _, err := r.readUE(); err != nil {
			return 0
		}
	}
	timing, _ := r.readBit()
	if timing == 1 {
		numUnitsInTick, err := r.readBits(32)
		if err != nil || numUnitsInTick == 0 {
			return 0
		}
		timeScale, err := r.readBits(32)
		if err != nil {
			return 0
		}
		fixed, err := r.readBit()
		if err != nil {
			return 0
		}
		if fixed == 1 {
			return float64(timeScale) / (2.0 * float64(numUnitsInTick))
		}
		return float64(timeScale) / (2.0 * float64(numUnitsInTick))
	}
	return 0
}
