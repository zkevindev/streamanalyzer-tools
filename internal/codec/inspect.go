package codec

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	DefaultHexLimit = 2048
	MaxHexLimit     = 8192
)

// InspectOptions configures frame-level deep inspection.
type InspectOptions struct {
	MediaType  string // "video" or "audio"
	Codec      string // "h264", "h265", "aac", "mp3", etc.
	LengthSize int    // 0 = Annex B (or raw audio), 1..4 = AVCC/HVCC length-prefixed
	HexOffset  int
	HexLimit   int
}

// FrameInspection is the structured result returned by InspectFrame.
type FrameInspection struct {
	MediaType   string             `json:"media_type"`
	Codec       string             `json:"codec,omitempty"`
	Format      string             `json:"format"`
	TotalBytes  int                `json:"total_bytes"`
	NALUs       []NALUDetail       `json:"nalus,omitempty"`
	SPS         []SPSDetail        `json:"sps,omitempty"`
	PPS         []PPSDetail        `json:"pps,omitempty"`
	VPS         []VPSDetail        `json:"vps,omitempty"`
	SEIs        []SEIDetail        `json:"seis,omitempty"`
	AV1         *AV1Meta           `json:"av1,omitempty"`
	AudioHeader *AudioHeaderDetail `json:"audio_header,omitempty"`
	Hex         HexDump            `json:"hex"`
	Warnings    []string           `json:"warnings,omitempty"`
}

// NALUDetail describes one NAL unit inside a video frame.
type NALUDetail struct {
	Index         int    `json:"index"`
	Offset        int    `json:"offset"`         // start offset in frame (including start code / length prefix)
	PayloadOffset int    `json:"payload_offset"` // start offset of NALU header byte
	PrefixLen     int    `json:"prefix_len"`
	Length        int    `json:"length"` // NALU byte length excluding start code / length prefix
	Type          int    `json:"type"`
	TypeName      string `json:"type_name"`
	SliceType     string `json:"slice_type,omitempty"` // I / P / B / SP / SI
	IsKey         bool   `json:"is_key,omitempty"`
	RefIDC        int    `json:"ref_idc,omitempty"`
	LayerID       int    `json:"layer_id,omitempty"`
	TemporalID    int    `json:"temporal_id,omitempty"`
}

// VUIDetail holds decoded VUI parameters from an H.264 SPS.
type VUIDetail struct {
	AspectRatioIDC          int     `json:"aspect_ratio_idc,omitempty"`
	SAR                     string  `json:"sar,omitempty"`
	VideoFullRange          *bool   `json:"video_full_range,omitempty"`
	ColorRange              string  `json:"color_range,omitempty"`
	ColourPrimaries         int     `json:"colour_primaries,omitempty"`
	ColourPrimariesName     string  `json:"colour_primaries_name,omitempty"`
	TransferCharacteristics int     `json:"transfer_characteristics,omitempty"`
	TransferName            string  `json:"transfer_name,omitempty"`
	MatrixCoefficients      int     `json:"matrix_coefficients,omitempty"`
	MatrixName              string  `json:"matrix_name,omitempty"`
	TimingPresent           bool    `json:"timing_present,omitempty"`
	NumUnitsInTick          uint32  `json:"num_units_in_tick,omitempty"`
	TimeScale               uint32  `json:"time_scale,omitempty"`
	FixedFrameRate          bool    `json:"fixed_frame_rate,omitempty"`
	FPS                     float64 `json:"fps,omitempty"`
}

// SPSDetail holds field-level decoded H.264 or H.265 SPS parameters.
type SPSDetail struct {
	Codec           string     `json:"codec"`
	SPSID           int        `json:"sps_id"`
	VPSID           int        `json:"vps_id,omitempty"`
	ProfileIDC      int        `json:"profile_idc"`
	ProfileName     string     `json:"profile_name"`
	TierFlag        int        `json:"tier_flag,omitempty"`
	TierName        string     `json:"tier_name,omitempty"`
	ConstraintFlags string     `json:"constraint_flags,omitempty"`
	LevelIDC        int        `json:"level_idc"`
	LevelName       string     `json:"level_name"`
	ChromaFormatIDC int        `json:"chroma_format_idc"`
	ChromaFormat    string     `json:"chroma_format"`
	BitDepthLuma    int        `json:"bit_depth_luma"`
	BitDepthChroma  int        `json:"bit_depth_chroma"`
	CodedWidth      int        `json:"coded_width"`
	CodedHeight     int        `json:"coded_height"`
	Width           int        `json:"width"`
	Height          int        `json:"height"`
	CropLeft        int        `json:"crop_left,omitempty"`
	CropRight       int        `json:"crop_right,omitempty"`
	CropTop         int        `json:"crop_top,omitempty"`
	CropBottom      int        `json:"crop_bottom,omitempty"`
	FrameMbsOnly    *bool      `json:"frame_mbs_only,omitempty"`
	ScanType        string     `json:"scan_type,omitempty"`
	MaxNumRefFrames int        `json:"max_num_ref_frames,omitempty"`
	MaxSubLayers    int        `json:"max_sub_layers,omitempty"`
	Log2MaxFrameNum int        `json:"log2_max_frame_num,omitempty"`
	PicOrderCntType int        `json:"pic_order_cnt_type,omitempty"`
	VUI             *VUIDetail `json:"vui,omitempty"`
}

// PPSDetail holds field-level decoded H.264/H.265 PPS parameters.
type PPSDetail struct {
	Codec                          string `json:"codec"`
	PPSID                          int    `json:"pps_id"`
	SPSID                          int    `json:"sps_id"`
	EntropyCodingMode              string `json:"entropy_coding_mode,omitempty"`
	BottomFieldPicOrderPresent     bool   `json:"bottom_field_pic_order_present,omitempty"`
	NumSliceGroupsMinus1           int    `json:"num_slice_groups_minus1,omitempty"`
	NumRefIdxL0DefaultActive       int    `json:"num_ref_idx_l0_default_active,omitempty"`
	NumRefIdxL1DefaultActive       int    `json:"num_ref_idx_l1_default_active,omitempty"`
	WeightedPred                   bool   `json:"weighted_pred,omitempty"`
	WeightedBipredIDC              int    `json:"weighted_bipred_idc,omitempty"`
	PicInitQP                      int    `json:"pic_init_qp,omitempty"`
	ChromaQPIndexOffset            int    `json:"chroma_qp_index_offset,omitempty"`
	DeblockingFilterControlPresent bool   `json:"deblocking_filter_control_present,omitempty"`
	ConstrainedIntraPred           bool   `json:"constrained_intra_pred,omitempty"`
	RedundantPicCntPresent         bool   `json:"redundant_pic_cnt_present,omitempty"`
}

// VPSDetail holds decoded H.265 VPS parameters.
type VPSDetail struct {
	VPSID             int    `json:"vps_id"`
	MaxLayers         int    `json:"max_layers"`
	MaxSubLayers      int    `json:"max_sub_layers"`
	TemporalIDNesting bool   `json:"temporal_id_nesting"`
	ProfileIDC        int    `json:"profile_idc"`
	ProfileName       string `json:"profile_name"`
	TierFlag          int    `json:"tier_flag"`
	TierName          string `json:"tier_name"`
	LevelIDC          int    `json:"level_idc"`
	LevelName         string `json:"level_name"`
}

// SEIDetail holds one decoded SEI message from an H.264 or H.265 SEI NALU.
type SEIDetail struct {
	NALUType        int    `json:"nalu_type"`
	PayloadType     int    `json:"payload_type"`
	PayloadTypeName string `json:"payload_type_name"`
	PayloadSize     int    `json:"payload_size"`
	UUID            string `json:"uuid,omitempty"`
	Text            string `json:"text,omitempty"`
	HexPreview      string `json:"hex_preview,omitempty"`
}

// AudioHeaderDetail holds decoded ADTS AAC or MP3 frame header metadata.
type AudioHeaderDetail struct {
	Format        string `json:"format"`
	MPEGVersion   string `json:"mpeg_version"`
	Profile       string `json:"profile,omitempty"`
	Layer         string `json:"layer,omitempty"`
	SampleRate    int    `json:"sample_rate"`
	Channels      int    `json:"channels"`
	ChannelMode   string `json:"channel_mode,omitempty"`
	BitrateKbps   int    `json:"bitrate_kbps,omitempty"`
	CRCProtected  bool   `json:"crc_protected"`
	FrameLength   int    `json:"frame_length,omitempty"`
	HeaderBytes   int    `json:"header_bytes,omitempty"`
	RawDataBlocks int    `json:"raw_data_blocks,omitempty"`
}

// HexLine represents one 16-byte formatted row in the Hex+ASCII view.
type HexLine struct {
	Offset    int      `json:"offset"`
	OffsetHex string   `json:"offset_hex"`
	HexBytes  []string `json:"hex_bytes"`
	ASCII     string   `json:"ascii"`
}

// HexRange marks one NALU span inside the inspected frame buffer.
type HexRange struct {
	Index        int    `json:"index"`
	Start        int    `json:"start"`
	PayloadStart int    `json:"payload_start"`
	End          int    `json:"end"`
	Type         int    `json:"type"`
	TypeName     string `json:"type_name"`
	IsKey        bool   `json:"is_key,omitempty"`
}

// HexDump holds a paginated 16-column Hex+ASCII view and NALU boundary markers.
type HexDump struct {
	Offset     int        `json:"offset"`
	Limit      int        `json:"limit"`
	Returned   int        `json:"returned"`
	TotalBytes int        `json:"total_bytes"`
	HasMore    bool       `json:"has_more"`
	Lines      []HexLine  `json:"lines"`
	NALURanges []HexRange `json:"nalu_ranges,omitempty"`
}

// InspectFrame performs deep inspection on a single video or audio frame.
func InspectFrame(data []byte, opts InspectOptions) FrameInspection {
	media := strings.ToLower(strings.TrimSpace(opts.MediaType))
	codecName := strings.ToLower(strings.TrimSpace(opts.Codec))

	res := FrameInspection{
		MediaType:  media,
		Codec:      codecName,
		Format:     "raw",
		TotalBytes: len(data),
	}

	if media == "" {
		if codecName == "aac" || codecName == "mp3" || codecName == "mp2" || codecName == "opus" ||
			codecName == "pcma" || codecName == "pcmu" || codecName == "g711a" || codecName == "g711u" || codecName == "g711" {
			media = "audio"
		} else {
			media = "video"
		}
		res.MediaType = media
	}

	if media == "audio" {
		if codecName == "pcma" || codecName == "pcmu" || codecName == "g711a" || codecName == "g711u" || codecName == "g711" {
			fmtName := "G.711 A-law (PCMA)"
			if codecName == "pcmu" || codecName == "g711u" {
				fmtName = "G.711 mu-law (PCMU)"
			}
			res.Format = "g711"
			res.AudioHeader = &AudioHeaderDetail{
				Format:      fmtName,
				Profile:     "8-bit log-PCM",
				SampleRate:  8000,
				Channels:    1,
				BitrateKbps: 64,
				FrameLength: len(data),
			}
			res.Hex = buildHexDump(data, opts.HexOffset, opts.HexLimit, nil)
			return res
		}
		if hdr, ok := parseAudioFrameHeader(data, codecName); ok {
			res.AudioHeader = hdr
			if hdr.Format == "ADTS AAC" {
				res.Format = "adts"
				if res.Codec == "" {
					res.Codec = "aac"
				}
			} else if hdr.Format == "MP3" {
				res.Format = "mp3"
				if res.Codec == "" {
					res.Codec = "mp3"
				}
			}
		} else if len(data) > 0 {
			res.Warnings = append(res.Warnings, "未识别到 ADTS AAC 或 MP3 音频帧头，仅展示原始字节")
		}
		res.Hex = buildHexDump(data, opts.HexOffset, opts.HexLimit, nil)
		return res
	}

	// AV1 OBU frame inspection
	if codecName == "av1" {
		res.Format = "av1-obu"
		if meta, err := ParseAV1SequenceHeader(data); err == nil && meta != nil {
			res.AV1 = meta
		} else if len(data) > 0 {
			res.Warnings = append(res.Warnings, "当前 AV1 帧未包含 Sequence Header OBU（仅关键帧或封装头携带）")
		}
		res.Hex = buildHexDump(data, opts.HexOffset, opts.HexLimit, nil)
		return res
	}

	// Video frame inspection
	isHEVC := codecName == "h265" || codecName == "hevc"
	var spans []naluSpan

	if opts.LengthSize >= 1 && opts.LengthSize <= 4 {
		spans = splitLengthPrefixedSpans(data, opts.LengthSize)
		if isHEVC {
			res.Format = "hvcc"
		} else {
			res.Format = "avcc"
		}
		if len(spans) == 0 && len(data) >= 4 {
			// Fallback: try Annex B if caller didn't know exact container packaging
			if ab := splitAnnexBSpans(data); len(ab) > 0 {
				spans = ab
				res.Format = "annexb"
			}
		}
	} else {
		spans = splitAnnexBSpans(data)
		if len(spans) > 0 {
			res.Format = "annexb"
		} else if len(data) >= 5 {
			// Best-effort auto-detect 4-byte AVCC/HVCC if Annex B start code is absent
			if lp := splitLengthPrefixedSpans(data, 4); len(lp) > 0 {
				spans = lp
				if isHEVC {
					res.Format = "hvcc"
				} else {
					res.Format = "avcc"
				}
			}
		}
	}

	if codecName == "" && len(spans) > 0 {
		isHEVC = guessHEVCFromSpans(data, spans)
		if isHEVC {
			res.Codec = "h265"
			if res.Format == "avcc" {
				res.Format = "hvcc"
			}
		} else {
			res.Codec = "h264"
		}
	}

	var ranges []HexRange
	for i, sp := range spans {
		nal := data[sp.payloadStart:sp.end]
		if len(nal) == 0 {
			continue
		}
		idx := i + 1
		detail := NALUDetail{
			Index:         idx,
			Offset:        sp.start,
			PayloadOffset: sp.payloadStart,
			PrefixLen:     sp.payloadStart - sp.start,
			Length:        len(nal),
		}

		if isHEVC {
			if len(nal) < 2 {
				continue
			}
			typ := int((nal[0] >> 1) & 0x3f)
			layerID := int(((nal[0] & 0x01) << 5) | ((nal[1] >> 3) & 0x1f))
			tid := int(nal[1] & 0x07)
			detail.Type = typ
			detail.TypeName = inspectH265NALUTypeName(typ)
			detail.LayerID = layerID
			detail.TemporalID = tid
			if typ >= 16 && typ <= 21 {
				detail.IsKey = true
				detail.SliceType = "I"
			} else if typ <= 9 {
				detail.SliceType = h265SliceType(nal)
			}
			switch typ {
			case 32:
				if vps, err := ParseH265VPSDetail(nal); err == nil {
					res.VPS = append(res.VPS, *vps)
				} else {
					res.Warnings = append(res.Warnings, fmt.Sprintf("NALU #%d VPS 解码失败: %v", idx, err))
				}
			case 33:
				if sps, err := ParseH265SPSDetail(nal); err == nil {
					res.SPS = append(res.SPS, *sps)
				} else {
					res.Warnings = append(res.Warnings, fmt.Sprintf("NALU #%d H.265 SPS 解码失败: %v", idx, err))
				}
			case 34:
				if pps, err := ParseH265PPSDetail(nal); err == nil {
					res.PPS = append(res.PPS, *pps)
				}
			case 39, 40:
				seis := ParseSEINAL(nal, true)
				res.SEIs = append(res.SEIs, seis...)
			}
		} else {
			typ := int(nal[0] & 0x1f)
			detail.Type = typ
			detail.TypeName = inspectH264NALUTypeName(typ)
			detail.RefIDC = int((nal[0] >> 5) & 0x03)
			if typ == 5 {
				detail.IsKey = true
			}
			if typ == 1 || typ == 5 {
				detail.SliceType = H264SliceType(nal)
				if detail.SliceType == "" && typ == 5 {
					detail.SliceType = "I"
				}
			}
			switch typ {
			case 6:
				seis := ParseSEINAL(nal, false)
				res.SEIs = append(res.SEIs, seis...)
			case 7:
				if sps, err := ParseH264SPSDetail(nal); err == nil {
					res.SPS = append(res.SPS, *sps)
				} else {
					res.Warnings = append(res.Warnings, fmt.Sprintf("NALU #%d H.264 SPS 解码失败: %v", idx, err))
				}
			case 8:
				if pps, err := ParseH264PPSDetail(nal); err == nil {
					res.PPS = append(res.PPS, *pps)
				} else {
					res.Warnings = append(res.Warnings, fmt.Sprintf("NALU #%d H.264 PPS 解码失败: %v", idx, err))
				}
			}
		}

		res.NALUs = append(res.NALUs, detail)
		ranges = append(ranges, HexRange{
			Index:        idx,
			Start:        sp.start,
			PayloadStart: sp.payloadStart,
			End:          sp.end,
			Type:         detail.Type,
			TypeName:     detail.TypeName,
			IsKey:        detail.IsKey,
		})
	}

	res.Hex = buildHexDump(data, opts.HexOffset, opts.HexLimit, ranges)
	return res
}

type naluSpan struct {
	start        int
	payloadStart int
	end          int
}

func splitAnnexBSpans(b []byte) []naluSpan {
	starts := annexBStarts(b)
	if len(starts) == 0 {
		return nil
	}
	out := make([]naluSpan, 0, len(starts))
	for i, start := range starts {
		sc := 3
		if start+3 < len(b) && b[start] == 0 && b[start+1] == 0 && b[start+2] == 0 && b[start+3] == 1 {
			sc = 4
		}
		nalStart := start + sc
		next := len(b)
		if i+1 < len(starts) {
			next = starts[i+1]
		}
		if nalStart >= next {
			continue
		}
		out = append(out, naluSpan{start: start, payloadStart: nalStart, end: next})
	}
	return out
}

func splitLengthPrefixedSpans(payload []byte, lengthSize int) []naluSpan {
	if lengthSize < 1 || lengthSize > 4 {
		lengthSize = 4
	}
	var out []naluSpan
	off := 0
	for off+lengthSize <= len(payload) {
		size := 0
		switch lengthSize {
		case 1:
			size = int(payload[off])
		case 2:
			size = int(binary.BigEndian.Uint16(payload[off : off+2]))
		case 3:
			size = int(payload[off])<<16 | int(payload[off+1])<<8 | int(payload[off+2])
		default:
			size = int(binary.BigEndian.Uint32(payload[off : off+4]))
		}
		if size <= 0 || off+lengthSize+size > len(payload) {
			break
		}
		out = append(out, naluSpan{
			start:        off,
			payloadStart: off + lengthSize,
			end:          off + lengthSize + size,
		})
		off += lengthSize + size
	}
	return out
}

func guessHEVCFromSpans(data []byte, spans []naluSpan) bool {
	var h264Score, h265Score int
	for _, sp := range spans {
		nal := data[sp.payloadStart:sp.end]
		if len(nal) < 2 {
			continue
		}
		t264 := nal[0] & 0x1f
		if t264 == 7 || t264 == 8 || t264 == 5 {
			h264Score += 2
		}
		if (nal[0] & 0x80) == 0 {
			t265 := (nal[0] >> 1) & 0x3f
			if t265 == 32 || t265 == 33 || t265 == 34 || (t265 >= 16 && t265 <= 21) {
				h265Score += 3
			}
		}
	}
	return h265Score > h264Score
}

func inspectH264NALUTypeName(t int) string {
	switch t {
	case 1:
		return "Slice (Non-IDR)"
	case 2:
		return "DPA"
	case 3:
		return "DPB"
	case 4:
		return "DPC"
	case 5:
		return "IDR"
	case 6:
		return "SEI"
	case 7:
		return "SPS"
	case 8:
		return "PPS"
	case 9:
		return "AUD"
	case 10:
		return "End of Seq"
	case 11:
		return "End of Stream"
	case 12:
		return "Filler"
	case 13:
		return "SPS Ext"
	case 14:
		return "Prefix"
	default:
		return fmt.Sprintf("Type %d", t)
	}
}

func inspectH265NALUTypeName(t int) string {
	switch t {
	case 0:
		return "TRAIL_N"
	case 1:
		return "TRAIL_R"
	case 2:
		return "TSA_N"
	case 3:
		return "TSA_R"
	case 4:
		return "STSA_N"
	case 5:
		return "STSA_R"
	case 6:
		return "RADL_N"
	case 7:
		return "RADL_R"
	case 8:
		return "RASL_N"
	case 9:
		return "RASL_R"
	case 16:
		return "BLA_W_LP"
	case 17:
		return "BLA_W_RADL"
	case 18:
		return "BLA_N_LP"
	case 19:
		return "IDR_W_RADL"
	case 20:
		return "IDR_N_LP"
	case 21:
		return "CRA_NUT"
	case 32:
		return "VPS"
	case 33:
		return "SPS"
	case 34:
		return "PPS"
	case 35:
		return "AUD"
	case 36:
		return "EOS"
	case 37:
		return "EOB"
	case 38:
		return "FD"
	case 39:
		return "PREFIX_SEI"
	case 40:
		return "SUFFIX_SEI"
	default:
		return fmt.Sprintf("Type %d", t)
	}
}

func h265SliceType(nal []byte) string {
	if len(nal) < 3 {
		return ""
	}
	t := int((nal[0] >> 1) & 0x3f)
	if t >= 16 && t <= 21 {
		return "I"
	}
	if t > 9 {
		return ""
	}
	// Best-effort: reading HEVC slice_type requires knowing dependent_slice_segments_enabled_flag
	// from PPS, which is not always in the same frame; return empty when unknown.
	return ""
}

func chromaFormatName(idc int) string {
	switch idc {
	case 0:
		return "Monochrome"
	case 1:
		return "4:2:0"
	case 2:
		return "4:2:2"
	case 3:
		return "4:4:4"
	default:
		return fmt.Sprintf("Unknown (%d)", idc)
	}
}

// ParseH264SPSDetail decodes detailed fields from an H.264 SPS NALU (including 1-byte header).
func ParseH264SPSDetail(nal []byte) (*SPSDetail, error) {
	if len(nal) < 4 {
		return nil, fmt.Errorf("h264 sps too short")
	}
	r := newBitReader(rbspFromNAL(nal[1:]))

	profileIDC, err := r.readBits(8)
	if err != nil {
		return nil, err
	}
	constraintByte, err := r.readBits(8)
	if err != nil {
		return nil, err
	}
	levelIDC, err := r.readBits(8)
	if err != nil {
		return nil, err
	}
	spsID, err := r.readUE()
	if err != nil {
		return nil, err
	}

	chromaFormatIDC := uint64(1)
	separateColourPlane := uint8(0)
	bitDepthLuma := 8
	bitDepthChroma := 8

	switch profileIDC {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		chromaFormatIDC, err = r.readUE()
		if err != nil {
			return nil, err
		}
		if chromaFormatIDC == 3 {
			separateColourPlane, err = r.readBit()
			if err != nil {
				return nil, err
			}
		}
		lumaMinus8, err := r.readUE()
		if err != nil {
			return nil, err
		}
		chromaMinus8, err := r.readUE()
		if err != nil {
			return nil, err
		}
		bitDepthLuma = 8 + int(lumaMinus8)
		bitDepthChroma = 8 + int(chromaMinus8)
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

	log2MaxFrameNumMinus4, err := r.readUE()
	if err != nil {
		return nil, err
	}
	picOrderCntType, err := r.readUE()
	if err != nil {
		return nil, err
	}
	if picOrderCntType == 0 {
		if _, err := r.readUE(); err != nil {
			return nil, err
		}
	} else if picOrderCntType == 1 {
		if err := r.skipBits(1); err != nil {
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
	maxNumRefFrames, err := r.readUE()
	if err != nil {
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
		if err := r.skipBits(1); err != nil {
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

	codedW := int((picWidthInMbsMinus1 + 1) * 16)
	heightFactor := uint64(2 - frameMbsOnlyFlag)
	codedH := int((picHeightInMapUnitsMinus1 + 1) * 16 * heightFactor)

	var cropUnitX, cropUnitY uint64
	if chromaFormatIDC == 0 || (chromaFormatIDC == 3 && separateColourPlane == 1) {
		cropUnitX = 1
		cropUnitY = 2 - uint64(frameMbsOnlyFlag)
	} else {
		var subW, subH uint64 = 1, 1
		switch chromaFormatIDC {
		case 1:
			subW, subH = 2, 2
		case 2:
			subW, subH = 2, 1
		}
		cropUnitX = subW
		cropUnitY = subH * (2 - uint64(frameMbsOnlyFlag))
	}
	width := codedW - int(cropUnitX*(cropLeft+cropRight))
	height := codedH - int(cropUnitY*(cropTop+cropBottom))
	if width <= 0 {
		width = codedW
	}
	if height <= 0 {
		height = codedH
	}

	frameMbsOnly := frameMbsOnlyFlag == 1
	scanType := "逐行 (Progressive)"
	if !frameMbsOnly {
		scanType = "隔行 (Interlaced)"
	}

	var vui *VUIDetail
	if vuiFlag, err := r.readBit(); err == nil && vuiFlag == 1 {
		vui = parseH264VUIDetail(r)
	}

	return &SPSDetail{
		Codec:           "h264",
		SPSID:           int(spsID),
		ProfileIDC:      int(profileIDC),
		ProfileName:     H264ProfileName(int(profileIDC)),
		ConstraintFlags: formatConstraintFlags(uint8(constraintByte)),
		LevelIDC:        int(levelIDC),
		LevelName:       H264LevelName(int(levelIDC)),
		ChromaFormatIDC: int(chromaFormatIDC),
		ChromaFormat:    chromaFormatName(int(chromaFormatIDC)),
		BitDepthLuma:    bitDepthLuma,
		BitDepthChroma:  bitDepthChroma,
		CodedWidth:      codedW,
		CodedHeight:     codedH,
		Width:           width,
		Height:          height,
		CropLeft:        int(cropLeft * cropUnitX),
		CropRight:       int(cropRight * cropUnitX),
		CropTop:         int(cropTop * cropUnitY),
		CropBottom:      int(cropBottom * cropUnitY),
		FrameMbsOnly:    &frameMbsOnly,
		ScanType:        scanType,
		MaxNumRefFrames: int(maxNumRefFrames),
		Log2MaxFrameNum: int(log2MaxFrameNumMinus4 + 4),
		PicOrderCntType: int(picOrderCntType),
		VUI:             vui,
	}, nil
}

func formatConstraintFlags(b uint8) string {
	var sets []string
	for i := 0; i < 6; i++ {
		if (b & (1 << uint(7-i))) != 0 {
			sets = append(sets, fmt.Sprintf("set%d", i))
		}
	}
	if len(sets) == 0 {
		return fmt.Sprintf("0x%02X (none)", b)
	}
	return fmt.Sprintf("0x%02X (%s)", b, strings.Join(sets, ","))
}

func parseH264VUIDetail(r *bitReader) *VUIDetail {
	v := &VUIDetail{}
	aspectFlag, err := r.readBit()
	if err != nil {
		return v
	}
	if aspectFlag == 1 {
		idc, err := r.readBits(8)
		if err != nil {
			return v
		}
		v.AspectRatioIDC = int(idc)
		if idc == 255 {
			sarW, err1 := r.readBits(16)
			sarH, err2 := r.readBits(16)
			if err1 != nil || err2 != nil {
				return v
			}
			v.SAR = fmt.Sprintf("%d:%d", sarW, sarH)
		} else {
			v.SAR = sarFromIDC(int(idc))
		}
	}

	overscan, err := r.readBit()
	if err != nil {
		return v
	}
	if overscan == 1 {
		if err := r.skipBits(1); err != nil {
			return v
		}
	}

	videoSignal, err := r.readBit()
	if err != nil {
		return v
	}
	if videoSignal == 1 {
		if _, err := r.readBits(3); err != nil { // video_format
			return v
		}
		fullRangeBit, err := r.readBit()
		if err != nil {
			return v
		}
		fr := fullRangeBit == 1
		v.VideoFullRange = &fr
		if fr {
			v.ColorRange = "Full (PC)"
		} else {
			v.ColorRange = "Limited (TV)"
		}
		colourDesc, err := r.readBit()
		if err != nil {
			return v
		}
		if colourDesc == 1 {
			cp, err1 := r.readBits(8)
			tc, err2 := r.readBits(8)
			mc, err3 := r.readBits(8)
			if err1 != nil || err2 != nil || err3 != nil {
				return v
			}
			v.ColourPrimaries = int(cp)
			v.ColourPrimariesName = colourPrimariesName(int(cp))
			v.TransferCharacteristics = int(tc)
			v.TransferName = transferCharacteristicsName(int(tc))
			v.MatrixCoefficients = int(mc)
			v.MatrixName = matrixCoefficientsName(int(mc))
		}
	}

	chromaLoc, err := r.readBit()
	if err != nil {
		return v
	}
	if chromaLoc == 1 {
		if _, err := r.readUE(); err != nil {
			return v
		}
		if _, err := r.readUE(); err != nil {
			return v
		}
	}

	timing, err := r.readBit()
	if err != nil {
		return v
	}
	if timing == 1 {
		numUnits, err1 := r.readBits(32)
		timeScale, err2 := r.readBits(32)
		fixed, err3 := r.readBit()
		if err1 != nil || err2 != nil || err3 != nil {
			return v
		}
		v.TimingPresent = true
		v.NumUnitsInTick = uint32(numUnits)
		v.TimeScale = uint32(timeScale)
		v.FixedFrameRate = fixed == 1
		if numUnits > 0 && timeScale > 0 {
			fps := float64(timeScale) / (2.0 * float64(numUnits))
			v.FPS = math.Round(fps*100) / 100
		}
	}
	return v
}

func sarFromIDC(idc int) string {
	table := map[int]string{
		1:  "1:1",
		2:  "12:11",
		3:  "10:11",
		4:  "16:11",
		5:  "40:33",
		6:  "24:11",
		7:  "20:11",
		8:  "32:11",
		9:  "80:33",
		10: "18:11",
		11: "15:11",
		12: "64:33",
		13: "160:99",
		14: "4:3",
		15: "3:2",
		16: "2:1",
	}
	if s, ok := table[idc]; ok {
		return s
	}
	return fmt.Sprintf("idc=%d", idc)
}

func colourPrimariesName(v int) string {
	switch v {
	case 1:
		return "BT.709"
	case 2:
		return "Unspecified"
	case 4:
		return "BT.470M"
	case 5:
		return "BT.601 PAL (BT.470BG)"
	case 6:
		return "BT.601 NTSC (SMPTE 170M)"
	case 7:
		return "SMPTE 240M"
	case 8:
		return "Generic Film"
	case 9:
		return "BT.2020"
	case 10:
		return "SMPTE ST 428"
	case 11:
		return "DCI-P3"
	case 12:
		return "Display P3"
	default:
		return fmt.Sprintf("Code %d", v)
	}
}

func transferCharacteristicsName(v int) string {
	switch v {
	case 1:
		return "BT.709"
	case 2:
		return "Unspecified"
	case 4:
		return "BT.470M (Gamma 2.2)"
	case 5:
		return "BT.470BG (Gamma 2.8)"
	case 6:
		return "BT.601 (SMPTE 170M)"
	case 7:
		return "SMPTE 240M"
	case 8:
		return "Linear"
	case 13:
		return "sRGB / IEC 61966-2-1"
	case 14:
		return "BT.2020 10-bit"
	case 15:
		return "BT.2020 12-bit"
	case 16:
		return "PQ (SMPTE ST 2084)"
	case 18:
		return "HLG (ARIB STD-B67)"
	default:
		return fmt.Sprintf("Code %d", v)
	}
}

func matrixCoefficientsName(v int) string {
	switch v {
	case 0:
		return "RGB / Identity"
	case 1:
		return "BT.709"
	case 2:
		return "Unspecified"
	case 4:
		return "FCC"
	case 5:
		return "BT.601 (BT.470BG)"
	case 6:
		return "BT.601 (SMPTE 170M)"
	case 7:
		return "SMPTE 240M"
	case 8:
		return "YCgCo"
	case 9:
		return "BT.2020 NCL"
	case 10:
		return "BT.2020 CL"
	default:
		return fmt.Sprintf("Code %d", v)
	}
}

// ParseH264PPSDetail decodes key fields from an H.264 PPS NALU (including 1-byte header).
func ParseH264PPSDetail(nal []byte) (*PPSDetail, error) {
	if len(nal) < 2 {
		return nil, fmt.Errorf("h264 pps too short")
	}
	r := newBitReader(rbspFromNAL(nal[1:]))
	ppsID, err := r.readUE()
	if err != nil {
		return nil, err
	}
	spsID, err := r.readUE()
	if err != nil {
		return nil, err
	}
	entropyBit, err := r.readBit()
	if err != nil {
		return nil, err
	}
	entropy := "CAVLC"
	if entropyBit == 1 {
		entropy = "CABAC"
	}
	bottomField, err := r.readBit()
	if err != nil {
		return nil, err
	}
	numSliceGroupsMinus1, err := r.readUE()
	if err != nil {
		return nil, err
	}
	out := &PPSDetail{
		Codec:                      "h264",
		PPSID:                      int(ppsID),
		SPSID:                      int(spsID),
		EntropyCodingMode:          entropy,
		BottomFieldPicOrderPresent: bottomField == 1,
		NumSliceGroupsMinus1:       int(numSliceGroupsMinus1),
	}
	if numSliceGroupsMinus1 > 0 {
		// Complex slice group maps are rarely used in streaming; return parsed prefix fields.
		return out, nil
	}
	l0, err := r.readUE()
	if err != nil {
		return out, nil
	}
	l1, err := r.readUE()
	if err != nil {
		return out, nil
	}
	out.NumRefIdxL0DefaultActive = int(l0) + 1
	out.NumRefIdxL1DefaultActive = int(l1) + 1

	wp, err := r.readBit()
	if err != nil {
		return out, nil
	}
	out.WeightedPred = wp == 1

	wb, err := r.readBits(2)
	if err != nil {
		return out, nil
	}
	out.WeightedBipredIDC = int(wb)

	qpMinus26, err := r.readSE()
	if err != nil {
		return out, nil
	}
	out.PicInitQP = 26 + int(qpMinus26)
	_, err = r.readSE() // pic_init_qs_minus26
	if err != nil {
		return out, nil
	}
	chromaQP, err := r.readSE()
	if err != nil {
		return out, nil
	}
	out.ChromaQPIndexOffset = int(chromaQP)

	deblock, err := r.readBit()
	if err != nil {
		return out, nil
	}
	out.DeblockingFilterControlPresent = deblock == 1

	constrained, err := r.readBit()
	if err != nil {
		return out, nil
	}
	out.ConstrainedIntraPred = constrained == 1

	redundant, err := r.readBit()
	if err != nil {
		return out, nil
	}
	out.RedundantPicCntPresent = redundant == 1
	return out, nil
}

// ParseH265VPSDetail decodes an H.265 VPS NALU (including 2-byte header).
func ParseH265VPSDetail(nal []byte) (*VPSDetail, error) {
	if len(nal) < 16 {
		return nil, fmt.Errorf("h265 vps too short")
	}
	r := newBitReader(rbspFromNAL(nal[2:]))
	vpsID, err := r.readBits(4)
	if err != nil {
		return nil, err
	}
	if err := r.skipBits(2); err != nil { // base_layer_internal + available
		return nil, err
	}
	maxLayersMinus1, err := r.readBits(6)
	if err != nil {
		return nil, err
	}
	maxSubLayersMinus1, err := r.readBits(3)
	if err != nil {
		return nil, err
	}
	temporalNesting, err := r.readBit()
	if err != nil {
		return nil, err
	}
	if err := r.skipBits(16); err != nil { // vps_reserved_0xffff_16bits
		return nil, err
	}
	var profileIDC, tierFlag, levelIDC int
	if err := readHEVCProfileTierLevel(r, int(maxSubLayersMinus1), &profileIDC, &tierFlag, &levelIDC); err != nil {
		return nil, err
	}
	tierName := "Main"
	if tierFlag == 1 {
		tierName = "High"
	}
	return &VPSDetail{
		VPSID:             int(vpsID),
		MaxLayers:         int(maxLayersMinus1) + 1,
		MaxSubLayers:      int(maxSubLayersMinus1) + 1,
		TemporalIDNesting: temporalNesting == 1,
		ProfileIDC:        profileIDC,
		ProfileName:       h265ProfileName(profileIDC),
		TierFlag:          tierFlag,
		TierName:          tierName,
		LevelIDC:          levelIDC,
		LevelName:         H265LevelName(levelIDC),
	}, nil
}

// ParseH265SPSDetail decodes detailed fields from an H.265 SPS NALU (including 2-byte header).
func ParseH265SPSDetail(nal []byte) (*SPSDetail, error) {
	if len(nal) < 16 {
		return nil, fmt.Errorf("h265 sps too short")
	}
	r := newBitReader(rbspFromNAL(nal[2:]))
	vpsID, err := r.readBits(4)
	if err != nil {
		return nil, err
	}
	maxSubLayersMinus1, err := r.readBits(3)
	if err != nil {
		return nil, err
	}
	if err := r.skipBits(1); err != nil { // sps_temporal_id_nesting_flag
		return nil, err
	}
	var profileIDC, tierFlag, levelIDC int
	if err := readHEVCProfileTierLevel(r, int(maxSubLayersMinus1), &profileIDC, &tierFlag, &levelIDC); err != nil {
		return nil, err
	}
	spsID, err := r.readUE()
	if err != nil {
		return nil, err
	}
	chromaFormatIDC, err := r.readUE()
	if err != nil {
		return nil, err
	}
	separateColourPlane := uint8(0)
	if chromaFormatIDC == 3 {
		separateColourPlane, err = r.readBit()
		if err != nil {
			return nil, err
		}
	}
	codedW, err := r.readUE()
	if err != nil {
		return nil, err
	}
	codedH, err := r.readUE()
	if err != nil {
		return nil, err
	}
	confWindow, err := r.readBit()
	if err != nil {
		return nil, err
	}
	var left, right, top, bottom uint64
	if confWindow == 1 {
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
	bitDepthLuma := 8
	bitDepthChroma := 8
	if lumaMinus8, err := r.readUE(); err == nil {
		bitDepthLuma = 8 + int(lumaMinus8)
		if chromaMinus8, err := r.readUE(); err == nil {
			bitDepthChroma = 8 + int(chromaMinus8)
		}
	}

	subW, subH := uint64(1), uint64(1)
	if separateColourPlane == 0 {
		switch chromaFormatIDC {
		case 1:
			subW, subH = 2, 2
		case 2:
			subW, subH = 2, 1
		}
	}
	width := int(codedW - subW*(left+right))
	height := int(codedH - subH*(top+bottom))
	if width <= 0 {
		width = int(codedW)
	}
	if height <= 0 {
		height = int(codedH)
	}
	tierName := "Main"
	if tierFlag == 1 {
		tierName = "High"
	}

	return &SPSDetail{
		Codec:           "h265",
		SPSID:           int(spsID),
		VPSID:           int(vpsID),
		ProfileIDC:      profileIDC,
		ProfileName:     h265ProfileName(profileIDC),
		TierFlag:        tierFlag,
		TierName:        tierName,
		LevelIDC:        levelIDC,
		LevelName:       H265LevelName(levelIDC),
		ChromaFormatIDC: int(chromaFormatIDC),
		ChromaFormat:    chromaFormatName(int(chromaFormatIDC)),
		BitDepthLuma:    bitDepthLuma,
		BitDepthChroma:  bitDepthChroma,
		CodedWidth:      int(codedW),
		CodedHeight:     int(codedH),
		Width:           width,
		Height:          height,
		CropLeft:        int(left * subW),
		CropRight:       int(right * subW),
		CropTop:         int(top * subH),
		CropBottom:      int(bottom * subH),
		MaxSubLayers:    int(maxSubLayersMinus1) + 1,
	}, nil
}

// ParseH265PPSDetail decodes basic IDs from an H.265 PPS NALU (including 2-byte header).
func ParseH265PPSDetail(nal []byte) (*PPSDetail, error) {
	if len(nal) < 3 {
		return nil, fmt.Errorf("h265 pps too short")
	}
	r := newBitReader(rbspFromNAL(nal[2:]))
	ppsID, err := r.readUE()
	if err != nil {
		return nil, err
	}
	spsID, err := r.readUE()
	if err != nil {
		return nil, err
	}
	return &PPSDetail{
		Codec:             "h265",
		PPSID:             int(ppsID),
		SPSID:             int(spsID),
		EntropyCodingMode: "CABAC",
	}, nil
}

func readHEVCProfileTierLevel(r *bitReader, maxSubLayersMinus1 int, profile, tier, level *int) error {
	head, err := r.readBits(8)
	if err != nil {
		return err
	}
	if tier != nil {
		*tier = int((head >> 5) & 0x01)
	}
	if profile != nil {
		*profile = int(head & 0x1f)
	}
	if err := r.skipBits(32); err != nil { // general_profile_compatibility_flags
		return err
	}
	if err := r.skipBits(48); err != nil { // constraint flags
		return err
	}
	lvl, err := r.readBits(8)
	if err != nil {
		return err
	}
	if level != nil {
		*level = int(lvl)
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

// ParseSEINAL parses all SEI messages inside an H.264 (type 6) or H.265 (type 39/40) NALU.
func ParseSEINAL(nal []byte, isHEVC bool) []SEIDetail {
	hdrLen := 1
	nalType := 6
	if isHEVC {
		hdrLen = 2
		if len(nal) >= 2 {
			nalType = int((nal[0] >> 1) & 0x3f)
		}
	} else if len(nal) >= 1 {
		nalType = int(nal[0] & 0x1f)
	}
	if len(nal) <= hdrLen {
		return nil
	}
	rbsp := rbspFromNAL(nal[hdrLen:])
	var out []SEIDetail
	off := 0
	for off < len(rbsp) {
		// RBSP trailing stop bit (0x80 followed by optional zero padding)
		if rbsp[off] == 0x80 && isZeroPadding(rbsp[off+1:]) {
			break
		}
		payloadType := 0
		for off < len(rbsp) && rbsp[off] == 0xFF {
			payloadType += 255
			off++
		}
		if off >= len(rbsp) {
			break
		}
		payloadType += int(rbsp[off])
		off++

		payloadSize := 0
		for off < len(rbsp) && rbsp[off] == 0xFF {
			payloadSize += 255
			off++
		}
		if off >= len(rbsp) {
			break
		}
		payloadSize += int(rbsp[off])
		off++

		avail := len(rbsp) - off
		if avail < 0 {
			break
		}
		readLen := payloadSize
		if readLen > avail {
			readLen = avail
		}
		payload := rbsp[off : off+readLen]
		off += readLen

		item := SEIDetail{
			NALUType:        nalType,
			PayloadType:     payloadType,
			PayloadTypeName: seiPayloadTypeName(payloadType),
			PayloadSize:     payloadSize,
		}
		if len(payload) > 0 {
			previewLen := len(payload)
			if previewLen > 64 {
				previewLen = 64
			}
			item.HexPreview = strings.ToUpper(hex.EncodeToString(payload[:previewLen]))
		}
		if payloadType == 5 && len(payload) >= 16 {
			item.UUID = formatUUID16(payload[:16])
			if len(payload) > 16 {
				item.Text = extractReadableSEIText(payload[16:])
			}
		}
		out = append(out, item)
	}
	return out
}

func isZeroPadding(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func seiPayloadTypeName(t int) string {
	switch t {
	case 0:
		return "buffering_period"
	case 1:
		return "pic_timing"
	case 2:
		return "pan_scan_rect"
	case 3:
		return "filler_payload"
	case 4:
		return "user_data_registered_itu_t_t35"
	case 5:
		return "user_data_unregistered"
	case 6:
		return "recovery_point"
	case 129:
		return "active_parameter_sets"
	case 132:
		return "decoded_picture_hash"
	case 137:
		return "mastering_display_colour_volume"
	case 144:
		return "content_light_level_info"
	case 147:
		return "alternative_transfer_characteristics"
	default:
		return fmt.Sprintf("sei_%d", t)
	}
}

func formatUUID16(b []byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func extractReadableSEIText(b []byte) string {
	trimmed := strings.TrimRight(string(b), "\x00\r\n ")
	if trimmed == "" {
		return ""
	}
	if utf8.ValidString(trimmed) {
		var sb strings.Builder
		printable := 0
		total := 0
		for _, r := range trimmed {
			total++
			if r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r != 0x7f) {
				sb.WriteRune(r)
				printable++
			} else {
				sb.WriteByte('.')
			}
		}
		if total > 0 && float64(printable)/float64(total) >= 0.7 {
			return sb.String()
		}
	}
	return ""
}

// ParseAudioFrameHeader decodes ADTS AAC or MP3 frame header metadata.
func ParseAudioFrameHeader(data []byte, codecHint string) (*AudioHeaderDetail, bool) {
	return parseAudioFrameHeader(data, codecHint)
}

func parseAudioFrameHeader(data []byte, codecHint string) (*AudioHeaderDetail, bool) {
	hint := strings.ToLower(strings.TrimSpace(codecHint))
	if hint == "mp3" || hint == "mp2" {
		if hdr, ok := parseMP3Header(data); ok {
			return hdr, true
		}
		if hdr, ok := parseADTSHeader(data); ok {
			return hdr, true
		}
		return nil, false
	}
	if hdr, ok := parseADTSHeader(data); ok {
		return hdr, true
	}
	if hdr, ok := parseMP3Header(data); ok {
		return hdr, true
	}
	return nil, false
}

func parseADTSHeader(b []byte) (*AudioHeaderDetail, bool) {
	if len(b) < 7 {
		return nil, false
	}
	// ADTS syncword 12 bits = 0xFFF, layer bits (bits 1..2 of b[1]) MUST be 00
	if b[0] != 0xFF || (b[1]&0xF6) != 0xF0 {
		return nil, false
	}
	mpegID := (b[1] >> 3) & 0x01
	mpegVer := "MPEG-4"
	if mpegID == 1 {
		mpegVer = "MPEG-2"
	}
	protectionAbsent := (b[1] & 0x01) == 1
	profileIdx := int((b[2] >> 6) & 0x03)
	profiles := []string{"AAC Main", "AAC LC", "AAC SSR", "AAC LTP"}
	profile := profiles[profileIdx]

	freqIdx := int((b[2] >> 2) & 0x0F)
	if freqIdx >= len(aacSampleRates) {
		return nil, false
	}
	sampleRate := aacSampleRates[freqIdx]
	channels := int(((b[2] & 0x01) << 2) | ((b[3] >> 6) & 0x03))
	if channels == 7 {
		channels = 8
	}
	frameLen := int((uint16(b[3]&0x03) << 11) | (uint16(b[4]) << 3) | (uint16(b[5] >> 5)))
	rawBlocks := int(b[6]&0x03) + 1
	hdrBytes := 7
	if !protectionAbsent {
		hdrBytes = 9
	}
	chMode := fmt.Sprintf("%d ch", channels)
	switch channels {
	case 1:
		chMode = "Mono (1 ch)"
	case 2:
		chMode = "Stereo (2 ch)"
	case 6:
		chMode = "5.1 (6 ch)"
	case 8:
		chMode = "7.1 (8 ch)"
	}
	return &AudioHeaderDetail{
		Format:        "ADTS AAC",
		MPEGVersion:   mpegVer,
		Profile:       profile,
		SampleRate:    sampleRate,
		Channels:      channels,
		ChannelMode:   chMode,
		CRCProtected:  !protectionAbsent,
		FrameLength:   frameLen,
		HeaderBytes:   hdrBytes,
		RawDataBlocks: rawBlocks,
	}, true
}

var mp3BitratesV1L1 = [16]int{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0}
var mp3BitratesV1L2 = [16]int{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0}
var mp3BitratesV1L3 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
var mp3BitratesV2L1 = [16]int{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0}
var mp3BitratesV2L23 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}

var mp3SampleRates = map[int][3]int{
	3: {44100, 48000, 32000}, // MPEG-1
	2: {22050, 24000, 16000}, // MPEG-2
	0: {11025, 12000, 8000},  // MPEG-2.5
}

func parseMP3Header(b []byte) (*AudioHeaderDetail, bool) {
	if len(b) < 4 {
		return nil, false
	}
	// Syncword 11 bits = 0xFFE
	if b[0] != 0xFF || (b[1]&0xE0) != 0xE0 {
		return nil, false
	}
	verBits := int((b[1] >> 3) & 0x03)
	layerBits := int((b[1] >> 1) & 0x03)
	if verBits == 1 || layerBits == 0 {
		return nil, false
	}
	crcProtected := (b[1] & 0x01) == 0
	brIdx := int((b[2] >> 4) & 0x0F)
	srIdx := int((b[2] >> 2) & 0x03)
	padding := int((b[2] >> 1) & 0x01)
	if brIdx == 0 || brIdx == 15 || srIdx == 3 {
		return nil, false
	}

	var verName string
	switch verBits {
	case 3:
		verName = "MPEG-1"
	case 2:
		verName = "MPEG-2"
	case 0:
		verName = "MPEG-2.5"
	}

	var layerName string
	var bitrate int
	switch layerBits {
	case 3:
		layerName = "Layer I"
		if verBits == 3 {
			bitrate = mp3BitratesV1L1[brIdx]
		} else {
			bitrate = mp3BitratesV2L1[brIdx]
		}
	case 2:
		layerName = "Layer II"
		if verBits == 3 {
			bitrate = mp3BitratesV1L2[brIdx]
		} else {
			bitrate = mp3BitratesV2L23[brIdx]
		}
	case 1:
		layerName = "Layer III"
		if verBits == 3 {
			bitrate = mp3BitratesV1L3[brIdx]
		} else {
			bitrate = mp3BitratesV2L23[brIdx]
		}
	}
	if bitrate == 0 {
		return nil, false
	}
	sr := mp3SampleRates[verBits][srIdx]
	if sr == 0 {
		return nil, false
	}

	var frameLen int
	switch layerBits {
	case 3: // Layer I
		frameLen = ((12*bitrate*1000)/sr + padding) * 4
	case 2: // Layer II
		frameLen = (144*bitrate*1000)/sr + padding
	case 1: // Layer III
		if verBits == 3 {
			frameLen = (144*bitrate*1000)/sr + padding
		} else {
			frameLen = (72*bitrate*1000)/sr + padding
		}
	}

	modeBits := int((b[3] >> 6) & 0x03)
	modes := []string{"Stereo", "Joint Stereo", "Dual Channel", "Mono"}
	chMode := modes[modeBits]
	channels := 2
	if modeBits == 3 {
		channels = 1
	}

	return &AudioHeaderDetail{
		Format:       "MP3",
		MPEGVersion:  verName,
		Layer:        layerName,
		SampleRate:   sr,
		Channels:     channels,
		ChannelMode:  chMode,
		BitrateKbps:  bitrate,
		CRCProtected: crcProtected,
		FrameLength:  frameLen,
		HeaderBytes:  4,
	}, true
}

func buildHexDump(data []byte, offset, limit int, ranges []HexRange) HexDump {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = DefaultHexLimit
	}
	if limit > MaxHexLimit {
		limit = MaxHexLimit
	}
	total := len(data)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}

	chunk := data[offset:end]
	lines := make([]HexLine, 0, (len(chunk)+15)/16)
	for rel := 0; rel < len(chunk); rel += 16 {
		rowEnd := rel + 16
		if rowEnd > len(chunk) {
			rowEnd = len(chunk)
		}
		row := chunk[rel:rowEnd]
		hexBytes := make([]string, len(row))
		ascii := make([]byte, len(row))
		for i, b := range row {
			hexBytes[i] = fmt.Sprintf("%02X", b)
			if b >= 0x20 && b <= 0x7E {
				ascii[i] = b
			} else {
				ascii[i] = '.'
			}
		}
		absOff := offset + rel
		lines = append(lines, HexLine{
			Offset:    absOff,
			OffsetHex: fmt.Sprintf("%08X", absOff),
			HexBytes:  hexBytes,
			ASCII:     string(ascii),
		})
	}

	return HexDump{
		Offset:     offset,
		Limit:      limit,
		Returned:   len(chunk),
		TotalBytes: total,
		HasMore:    end < total,
		Lines:      lines,
		NALURanges: ranges,
	}
}
