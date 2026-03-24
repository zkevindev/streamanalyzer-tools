package codec

import (
	"encoding/binary"
	"fmt"
)

type HEVCNALArray struct {
	ArrayCompleteness uint8
	NALUnitType       uint8
	NALUnits          [][]byte
}

// HEVCDecoderConfigurationRecord corresponds to ISO/IEC 14496-15 hvcC payload.
type HEVCDecoderConfigurationRecord struct {
	ConfigurationVersion uint8
	NumOfArrays          uint8
	Arrays               []HEVCNALArray
}

func ParseHEVCDecoderConfigurationRecord(payload []byte) (*HEVCDecoderConfigurationRecord, error) {
	if len(payload) < 23 {
		return nil, fmt.Errorf("hevc config too short")
	}
	rec := &HEVCDecoderConfigurationRecord{
		ConfigurationVersion: payload[0],
		NumOfArrays:          payload[22],
	}
	off := 23
	for i := 0; i < int(rec.NumOfArrays); i++ {
		if len(payload) < off+3 {
			return nil, fmt.Errorf("hevc config truncated array header")
		}
		arr := HEVCNALArray{
			ArrayCompleteness: (payload[off] & 0x80) >> 7,
			NALUnitType:       payload[off] & 0x3f,
		}
		numNalus := int(binary.BigEndian.Uint16(payload[off+1 : off+3]))
		off += 3
		for j := 0; j < numNalus; j++ {
			if len(payload) < off+2 {
				return nil, fmt.Errorf("hevc config truncated nal length")
			}
			nalLen := int(binary.BigEndian.Uint16(payload[off : off+2]))
			off += 2
			if len(payload) < off+nalLen {
				return nil, fmt.Errorf("hevc config truncated nal")
			}
			arr.NALUnits = append(arr.NALUnits, payload[off:off+nalLen])
			off += nalLen
		}
		rec.Arrays = append(rec.Arrays, arr)
	}
	return rec, nil
}

func ParseHEVCDecoderConfig(payload []byte) (*VideoMeta, error) {
	rec, err := ParseHEVCDecoderConfigurationRecord(payload)
	if err != nil {
		return nil, err
	}
	for _, arr := range rec.Arrays {
		if arr.NALUnitType == 33 { // SPS
			for _, nal := range arr.NALUnits {
				return parseH265SPS(nal)
			}
		}
	}
	return nil, fmt.Errorf("hevc sps not found")
}

func parseH265SPS(nal []byte) (*VideoMeta, error) {
	if len(nal) < 3 {
		return nil, fmt.Errorf("h265 sps too short")
	}
	// nal header is 2 bytes
	r := newBitReader(rbspFromNAL(nal[2:]))
	if _, err := r.readBits(4); err != nil { // sps_video_parameter_set_id
		return nil, err
	}
	maxSubLayersMinus1, err := r.readBits(3)
	if err != nil {
		return nil, err
	}
	if err := r.skipBits(1); err != nil { // sps_temporal_id_nesting_flag
		return nil, err
	}
	if err := skipHEVCProfileTierLevel(r, int(maxSubLayersMinus1)); err != nil {
		return nil, err
	}
	if _, err := r.readUE(); err != nil { // sps_seq_parameter_set_id
		return nil, err
	}
	chromaFormatIDC, err := r.readUE()
	if err != nil {
		return nil, err
	}
	if chromaFormatIDC == 3 {
		if err := r.skipBits(1); err != nil { // separate_colour_plane_flag
			return nil, err
		}
	}
	picWidthInLumaSamples, err := r.readUE()
	if err != nil {
		return nil, err
	}
	picHeightInLumaSamples, err := r.readUE()
	if err != nil {
		return nil, err
	}
	conformanceWindowFlag, err := r.readBit()
	if err != nil {
		return nil, err
	}
	var left, right, top, bottom uint64
	if conformanceWindowFlag == 1 {
		if left, err = r.readUE(); err != nil {
			return nil, err
		}
		if right, err = r.readUE(); err != nil {
			return nil, err
		}
		if top, err = r.readUE(); err != nil {
			return nil, err
		}
		if bottom, err = r.readUE(); err != nil {
			return nil, err
		}
	}
	subWidthC, subHeightC := uint64(1), uint64(1)
	switch chromaFormatIDC {
	case 1:
		subWidthC, subHeightC = 2, 2
	case 2:
		subWidthC, subHeightC = 2, 1
	}
	width := int(picWidthInLumaSamples - subWidthC*(left+right))
	height := int(picHeightInLumaSamples - subHeightC*(top+bottom))
	return &VideoMeta{Width: width, Height: height}, nil
}

func skipHEVCProfileTierLevel(r *bitReader, maxSubLayersMinus1 int) error {
	// general_profile_space(2) + general_tier_flag(1) + general_profile_idc(5)
	if err := r.skipBits(8); err != nil {
		return err
	}
	// general_profile_compatibility_flags(32)
	if err := r.skipBits(32); err != nil {
		return err
	}
	// constraint flags(48)
	if err := r.skipBits(48); err != nil {
		return err
	}
	// general_level_idc(8)
	if err := r.skipBits(8); err != nil {
		return err
	}
	subLayerProfilePresent := make([]uint8, maxSubLayersMinus1)
	subLayerLevelPresent := make([]uint8, maxSubLayersMinus1)
	for i := 0; i < maxSubLayersMinus1; i++ {
		b, err := r.readBit()
		if err != nil {
			return err
		}
		subLayerProfilePresent[i] = b
		b, err = r.readBit()
		if err != nil {
			return err
		}
		subLayerLevelPresent[i] = b
	}
	if maxSubLayersMinus1 > 0 {
		for i := maxSubLayersMinus1; i < 8; i++ {
			if err := r.skipBits(2); err != nil {
				return err
			}
		}
	}
	for i := 0; i < maxSubLayersMinus1; i++ {
		if subLayerProfilePresent[i] == 1 {
			if err := r.skipBits(88); err != nil {
				return err
			}
		}
		if subLayerLevelPresent[i] == 1 {
			if err := r.skipBits(8); err != nil {
				return err
			}
		}
	}
	return nil
}
