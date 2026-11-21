package codec

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

// bitWriter builds synthetic H.264/H.265 RBSP bitstreams for unit tests.
type bitWriter struct {
	buf []byte
	cur byte
	n   uint
}

func (w *bitWriter) writeBit(v uint32) {
	w.cur = (w.cur << 1) | byte(v&1)
	w.n++
	if w.n == 8 {
		w.buf = append(w.buf, w.cur)
		w.cur = 0
		w.n = 0
	}
}

func (w *bitWriter) writeBits(v uint32, bits int) {
	for i := bits - 1; i >= 0; i-- {
		w.writeBit((v >> uint(i)) & 1)
	}
}

func (w *bitWriter) writeUE(v uint32) {
	codeNum := v + 1
	var bitLen int
	for tmp := codeNum; tmp > 0; tmp >>= 1 {
		bitLen++
	}
	leadingZeros := bitLen - 1
	for i := 0; i < leadingZeros; i++ {
		w.writeBit(0)
	}
	w.writeBits(codeNum, bitLen)
}

func (w *bitWriter) writeSE(v int32) {
	var codeNum uint32
	if v > 0 {
		codeNum = uint32(2*v - 1)
	} else {
		codeNum = uint32(-2 * v)
	}
	w.writeUE(codeNum)
}

func (w *bitWriter) bytes() []byte {
	w.writeBit(1) // rbsp_trailing_bits stop bit
	for w.n != 0 {
		w.writeBit(0)
	}
	return escapeEPB(w.buf)
}

func escapeEPB(rbsp []byte) []byte {
	out := make([]byte, 0, len(rbsp)+4)
	zeros := 0
	for _, b := range rbsp {
		if zeros == 2 && b <= 3 {
			out = append(out, 0x03)
			zeros = 0
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

// buildTestH264SPS creates a valid H.264 High Profile 1920x1080 SPS with VUI (BT.709, 25fps CFR).
func buildTestH264SPS() []byte {
	var w bitWriter
	w.writeBits(100, 8)  // profile_idc = High (100)
	w.writeBits(0x00, 8) // constraint flags
	w.writeBits(40, 8)   // level_idc = 4.0
	w.writeUE(0)         // seq_parameter_set_id
	w.writeUE(1)         // chroma_format_idc = 4:2:0
	w.writeUE(0)         // bit_depth_luma_minus8 = 0 -> 8 bit
	w.writeUE(0)         // bit_depth_chroma_minus8 = 0 -> 8 bit
	w.writeBit(0)        // qpprime_y_zero_transform_bypass_flag
	w.writeBit(0)        // seq_scaling_matrix_present_flag
	w.writeUE(0)         // log2_max_frame_num_minus4
	w.writeUE(0)         // pic_order_cnt_type
	w.writeUE(0)         // log2_max_pic_order_cnt_lsb_minus4
	w.writeUE(4)         // max_num_ref_frames = 4
	w.writeBit(0)        // gaps_in_frame_num_value_allowed_flag
	w.writeUE(119)       // pic_width_in_mbs_minus1 = 119 -> 1920
	w.writeUE(67)        // pic_height_in_map_units_minus1 = 67 -> 1088
	w.writeBit(1)        // frame_mbs_only_flag = 1 (progressive)
	w.writeBit(1)        // direct_8x8_inference_flag
	w.writeBit(1)        // frame_cropping_flag
	w.writeUE(0)         // crop_left
	w.writeUE(0)         // crop_right
	w.writeUE(0)         // crop_top
	w.writeUE(4)         // crop_bottom = 4 -> 4*2 = 8px -> 1080
	// VUI parameters
	w.writeBit(1) // vui_parameters_present_flag
	w.writeBit(1) // aspect_ratio_info_present_flag
	w.writeBits(1, 8) // aspect_ratio_idc = 1 (1:1)
	w.writeBit(0) // overscan_info_present_flag
	w.writeBit(1) // video_signal_type_present_flag
	w.writeBits(5, 3) // video_format = Unspecified
	w.writeBit(0) // video_full_range_flag = 0 (Limited)
	w.writeBit(1) // colour_description_present_flag
	w.writeBits(1, 8) // colour_primaries = 1 (BT.709)
	w.writeBits(1, 8) // transfer_characteristics = 1 (BT.709)
	w.writeBits(1, 8) // matrix_coefficients = 1 (BT.709)
	w.writeBit(0) // chroma_loc_info_present_flag
	w.writeBit(1) // timing_info_present_flag
	w.writeBits(1, 32)  // num_units_in_tick = 1
	w.writeBits(50, 32) // time_scale = 50 -> 50/(2*1) = 25 fps
	w.writeBit(1)       // fixed_frame_rate_flag = 1

	nal := append([]byte{0x67}, w.bytes()...)
	return nal
}

func buildTestH264PPS() []byte {
	var w bitWriter
	w.writeUE(1)  // pps_id = 1
	w.writeUE(0)  // sps_id = 0
	w.writeBit(1) // entropy_coding_mode_flag = CABAC
	w.writeBit(0) // bottom_field_pic_order_in_frame_present_flag
	w.writeUE(0)  // num_slice_groups_minus1
	w.writeUE(2)  // num_ref_idx_l0_default_active_minus1 -> 3
	w.writeUE(0)  // num_ref_idx_l1_default_active_minus1 -> 1
	w.writeBit(1) // weighted_pred_flag
	w.writeBits(2, 2) // weighted_bipred_idc = 2
	w.writeSE(-3) // pic_init_qp_minus26 -> 23
	w.writeSE(0)  // pic_init_qs_minus26
	w.writeSE(-2) // chroma_qp_index_offset
	w.writeBit(1) // deblocking_filter_control_present_flag
	w.writeBit(0) // constrained_intra_pred_flag
	w.writeBit(0) // redundant_pic_cnt_present_flag
	return append([]byte{0x68}, w.bytes()...)
}

func buildTestH264SEI(uuid [16]byte, msg string) []byte {
	var rbsp []byte
	// SEI message 1: payloadType = 5 (user_data_unregistered)
	payload := append(uuid[:], []byte(msg)...)
	rbsp = append(rbsp, 5)
	rem := len(payload)
	for rem >= 255 {
		rbsp = append(rbsp, 255)
		rem -= 255
	}
	rbsp = append(rbsp, byte(rem))
	rbsp = append(rbsp, payload...)

	// SEI message 2: payloadType = 6 (recovery_point), 2 bytes payload
	rbsp = append(rbsp, 6, 2, 0x80, 0x00)
	// rbsp trailing bits
	rbsp = append(rbsp, 0x80)
	return append([]byte{0x06}, escapeEPB(rbsp)...)
}

func buildTestH264IDR() []byte {
	var w bitWriter
	w.writeUE(0) // first_mb_in_slice
	w.writeUE(7) // slice_type = 7 -> I
	w.writeUE(1) // pic_parameter_set_id
	w.writeBits(0xaa, 8)
	return append([]byte{0x65}, w.bytes()...)
}

func buildTestH265VPS() []byte {
	var w bitWriter
	w.writeBits(0, 4)    // vps_video_parameter_set_id = 0
	w.writeBits(3, 2)    // vps_base_layer_internal/available
	w.writeBits(0, 6)    // vps_max_layers_minus1 = 0 -> 1 layer
	w.writeBits(0, 3)    // vps_max_sub_layers_minus1 = 0 -> 1 sub-layer
	w.writeBit(1)        // vps_temporal_id_nesting_flag
	w.writeBits(0xffff, 16) // reserved
	// profile_tier_level(1, 0):
	w.writeBits(0, 2)  // general_profile_space
	w.writeBit(0)      // general_tier_flag = Main
	w.writeBits(2, 5)  // general_profile_idc = 2 (Main 10)
	w.writeBits(0, 32) // general_profile_compatibility_flag
	w.writeBits(0, 32) // constraint flags hi
	w.writeBits(0, 16) // constraint flags lo
	w.writeBits(120, 8) // general_level_idc = 120 -> Level 4.0
	// H.265 VPS NALU header: type=32 -> (32<<1)=0x40, tid=1 -> 0x01
	return append([]byte{0x40, 0x01}, w.bytes()...)
}

func buildTestH265SPS() []byte {
	var w bitWriter
	w.writeBits(0, 4) // sps_video_parameter_set_id
	w.writeBits(0, 3) // sps_max_sub_layers_minus1 = 0
	w.writeBit(1)     // sps_temporal_id_nesting_flag
	// profile_tier_level(1, 0)
	w.writeBits(0, 2)   // general_profile_space
	w.writeBit(1)       // general_tier_flag = High
	w.writeBits(2, 5)   // general_profile_idc = 2 (Main 10)
	w.writeBits(0, 32)  // compatibility
	w.writeBits(0, 32)  // constraint hi
	w.writeBits(0, 16)  // constraint lo
	w.writeBits(153, 8) // general_level_idc = 153 -> Level 5.1
	w.writeUE(0)        // sps_seq_parameter_set_id
	w.writeUE(1)        // chroma_format_idc = 1 (4:2:0)
	w.writeUE(1920)     // pic_width_in_luma_samples
	w.writeUE(1088)     // pic_height_in_luma_samples
	w.writeBit(1)       // conformance_window_flag
	w.writeUE(0)        // left
	w.writeUE(0)        // right
	w.writeUE(0)        // top
	w.writeUE(4)        // bottom -> 4*2 = 8px -> 1080
	w.writeUE(2)        // bit_depth_luma_minus8 = 2 -> 10 bit
	w.writeUE(2)        // bit_depth_chroma_minus8 = 2 -> 10 bit
	// H.265 SPS NALU header: type=33 -> (33<<1)=0x42, tid=1 -> 0x01
	return append([]byte{0x42, 0x01}, w.bytes()...)
}

func buildTestH265SEI(uuid [16]byte, msg string) []byte {
	var rbsp []byte
	payload := append(uuid[:], []byte(msg)...)
	rbsp = append(rbsp, 5, byte(len(payload)))
	rbsp = append(rbsp, payload...)
	rbsp = append(rbsp, 0x80)
	// H.265 PREFIX_SEI type=39 -> (39<<1)=0x4e, tid=1 -> 0x01
	return append([]byte{0x4e, 0x01}, escapeEPB(rbsp)...)
}

func TestInspectFrameH264AnnexBFullDecode(t *testing.T) {
	uuid := [16]byte{0xdc, 0x45, 0xe9, 0xbd, 0xe6, 0xd9, 0x48, 0xb7, 0x96, 0x2c, 0xd8, 0x20, 0xd9, 0x23, 0xee, 0xef}
	seiMsg := `x264 - core 164 r3108 {"stream":"live","pts":12345}`

	sps := buildTestH264SPS()
	pps := buildTestH264PPS()
	sei := buildTestH264SEI(uuid, seiMsg)
	idr := buildTestH264IDR()

	var frame []byte
	for _, nal := range [][]byte{sps, pps, sei, idr} {
		frame = append(frame, 0x00, 0x00, 0x00, 0x01)
		frame = append(frame, nal...)
	}

	res := InspectFrame(frame, InspectOptions{MediaType: "video", Codec: "h264"})
	if res.Format != "annexb" || res.Codec != "h264" {
		t.Fatalf("unexpected format/codec: %+v", res)
	}
	if len(res.NALUs) != 4 {
		t.Fatalf("expected 4 NALUs, got %d", len(res.NALUs))
	}
	if res.NALUs[0].Type != 7 || res.NALUs[1].Type != 8 || res.NALUs[2].Type != 6 || res.NALUs[3].Type != 5 {
		t.Fatalf("unexpected NALU types: %+v", res.NALUs)
	}
	if !res.NALUs[3].IsKey || res.NALUs[3].SliceType != "I" {
		t.Fatalf("expected IDR slice I keyframe, got %+v", res.NALUs[3])
	}

	// Verify SPS + VUI
	if len(res.SPS) != 1 {
		t.Fatalf("expected 1 SPS, got %d", len(res.SPS))
	}
	s := res.SPS[0]
	if s.ProfileIDC != 100 || s.ProfileName != "High" || s.LevelName != "4.0" {
		t.Fatalf("unexpected SPS profile/level: %+v", s)
	}
	if s.ChromaFormat != "4:2:0" || s.BitDepthLuma != 8 || s.BitDepthChroma != 8 {
		t.Fatalf("unexpected chroma/bitdepth: %+v", s)
	}
	if s.CodedWidth != 1920 || s.CodedHeight != 1088 || s.Width != 1920 || s.Height != 1080 {
		t.Fatalf("unexpected dimensions: coded=%dx%d cropped=%dx%d", s.CodedWidth, s.CodedHeight, s.Width, s.Height)
	}
	if s.FrameMbsOnly == nil || !*s.FrameMbsOnly || s.MaxNumRefFrames != 4 {
		t.Fatalf("unexpected scan/ref: %+v", s)
	}
	if s.VUI == nil {
		t.Fatalf("expected VUI to be decoded")
	}
	if s.VUI.SAR != "1:1" || s.VUI.ColourPrimariesName != "BT.709" || s.VUI.TransferName != "BT.709" || s.VUI.MatrixName != "BT.709" {
		t.Fatalf("unexpected VUI color/SAR: %+v", s.VUI)
	}
	if s.VUI.VideoFullRange == nil || *s.VUI.VideoFullRange {
		t.Fatalf("expected limited color range, got %+v", s.VUI)
	}
	if math.Abs(s.VUI.FPS-25.0) > 0.01 || !s.VUI.FixedFrameRate {
		t.Fatalf("expected 25fps CFR, got %+v", s.VUI)
	}

	// Verify PPS
	if len(res.PPS) != 1 {
		t.Fatalf("expected 1 PPS, got %d", len(res.PPS))
	}
	p := res.PPS[0]
	if p.PPSID != 1 || p.SPSID != 0 || p.EntropyCodingMode != "CABAC" || p.NumRefIdxL0DefaultActive != 3 || p.PicInitQP != 23 {
		t.Fatalf("unexpected PPS: %+v", p)
	}

	// Verify SEI (both messages)
	if len(res.SEIs) != 2 {
		t.Fatalf("expected 2 SEI messages, got %d: %+v", len(res.SEIs), res.SEIs)
	}
	if res.SEIs[0].PayloadType != 5 || res.SEIs[0].PayloadTypeName != "user_data_unregistered" {
		t.Fatalf("unexpected SEI[0]: %+v", res.SEIs[0])
	}
	if res.SEIs[0].UUID != "dc45e9bd-e6d9-48b7-962c-d820d923eeef" {
		t.Fatalf("unexpected SEI UUID: %q", res.SEIs[0].UUID)
	}
	if !strings.Contains(res.SEIs[0].Text, "x264") || !strings.Contains(res.SEIs[0].Text, `"stream":"live"`) {
		t.Fatalf("unexpected SEI text: %q", res.SEIs[0].Text)
	}
	if res.SEIs[1].PayloadType != 6 || res.SEIs[1].PayloadTypeName != "recovery_point" {
		t.Fatalf("unexpected SEI[1]: %+v", res.SEIs[1])
	}

	// Verify Hex NALU ranges
	if len(res.Hex.NALURanges) != 4 || res.Hex.TotalBytes != len(frame) {
		t.Fatalf("unexpected hex dump metadata: %+v", res.Hex)
	}
}

func TestInspectFrameAVCCLengthPrefixed(t *testing.T) {
	sps := buildTestH264SPS()
	idr := buildTestH264IDR()

	var avcc []byte
	for _, nal := range [][]byte{sps, idr} {
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], uint32(len(nal)))
		avcc = append(avcc, hdr[:]...)
		avcc = append(avcc, nal...)
	}

	res := InspectFrame(avcc, InspectOptions{MediaType: "video", Codec: "h264", LengthSize: 4})
	if res.Format != "avcc" {
		t.Fatalf("expected avcc format, got %q", res.Format)
	}
	if len(res.NALUs) != 2 || res.NALUs[0].PrefixLen != 4 || res.NALUs[0].Type != 7 || res.NALUs[1].Type != 5 {
		t.Fatalf("unexpected AVCC NALUs: %+v", res.NALUs)
	}
	if len(res.SPS) != 1 || res.SPS[0].Width != 1920 || res.SPS[0].Height != 1080 {
		t.Fatalf("expected decoded SPS from AVCC frame, got %+v", res.SPS)
	}
}

func TestInspectFrameH265VPSSPSSEI(t *testing.T) {
	uuid := [16]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00}
	vps := buildTestH265VPS()
	sps := buildTestH265SPS()
	sei := buildTestH265SEI(uuid, `{"tag":"hevc-marker"}`)
	// IDR_W_RADL type=19 -> (19<<1)=0x26, tid=1 -> 0x01
	idr := []byte{0x26, 0x01, 0xaf, 0x00, 0x11}

	var frame []byte
	for _, nal := range [][]byte{vps, sps, sei, idr} {
		frame = append(frame, 0x00, 0x00, 0x00, 0x01)
		frame = append(frame, nal...)
	}

	res := InspectFrame(frame, InspectOptions{MediaType: "video", Codec: "h265"})
	if len(res.NALUs) != 4 || !res.NALUs[3].IsKey {
		t.Fatalf("unexpected H.265 NALUs: %+v", res.NALUs)
	}
	if len(res.VPS) != 1 || res.VPS[0].ProfileName != "Main 10" || res.VPS[0].LevelName != "4.0" {
		t.Fatalf("unexpected H.265 VPS: %+v", res.VPS)
	}
	if len(res.SPS) != 1 {
		t.Fatalf("expected 1 H.265 SPS, got %d", len(res.SPS))
	}
	s := res.SPS[0]
	if s.ProfileName != "Main 10" || s.TierName != "High" || s.LevelName != "5.1" {
		t.Fatalf("unexpected H.265 SPS profile/tier/level: %+v", s)
	}
	if s.ChromaFormat != "4:2:0" || s.BitDepthLuma != 10 || s.BitDepthChroma != 10 {
		t.Fatalf("unexpected H.265 chroma/bitdepth: %+v", s)
	}
	if s.CodedWidth != 1920 || s.CodedHeight != 1088 || s.Width != 1920 || s.Height != 1080 {
		t.Fatalf("unexpected H.265 dimensions: %+v", s)
	}
	if len(res.SEIs) != 1 || res.SEIs[0].UUID != "11223344-5566-7788-99aa-bbccddeeff00" || !strings.Contains(res.SEIs[0].Text, "hevc-marker") {
		t.Fatalf("unexpected H.265 SEI: %+v", res.SEIs)
	}
}

func TestInspectFrameAudioADTSAndMP3(t *testing.T) {
	// ADTS AAC: MPEG-4 (ID=0), AAC-LC (profile=1), 48kHz (freqIdx=3), stereo (ch=2), protection_absent=1, frameLen=15
	frameLen := 15
	adts := make([]byte, frameLen)
	adts[0] = 0xFF
	adts[1] = 0xF1 // MPEG-4, layer 0, protection_absent=1
	adts[2] = byte((1 << 6) | (3 << 2) | ((2 >> 2) & 1))
	adts[3] = byte(((2 & 3) << 6) | ((frameLen >> 11) & 3))
	adts[4] = byte((frameLen >> 3) & 0xFF)
	adts[5] = byte(((frameLen & 7) << 5) | 0x1F)
	adts[6] = 0xFC

	resAAC := InspectFrame(adts, InspectOptions{MediaType: "audio", Codec: "aac"})
	if resAAC.AudioHeader == nil {
		t.Fatalf("expected ADTS header decoded")
	}
	ah := resAAC.AudioHeader
	if ah.Format != "ADTS AAC" || ah.MPEGVersion != "MPEG-4" || ah.Profile != "AAC LC" || ah.SampleRate != 48000 || ah.Channels != 2 || ah.FrameLength != 15 || ah.CRCProtected {
		t.Fatalf("unexpected ADTS header: %+v", ah)
	}

	// MP3: sync=0xFFE, MPEG-1 (version=3), Layer III (layer=1), no CRC (1), 128kbps (idx=9), 44100Hz (idx=0), padding=0, Joint Stereo (mode=1)
	mp3 := []byte{0xFF, 0xFB, 0x90, 0x44, 0x00, 0x01, 0x02, 0x03}
	resMP3 := InspectFrame(mp3, InspectOptions{MediaType: "audio", Codec: "mp3"})
	if resMP3.AudioHeader == nil {
		t.Fatalf("expected MP3 header decoded")
	}
	mh := resMP3.AudioHeader
	if mh.Format != "MP3" || mh.MPEGVersion != "MPEG-1" || mh.Layer != "Layer III" || mh.BitrateKbps != 128 || mh.SampleRate != 44100 || mh.ChannelMode != "Joint Stereo" || mh.Channels != 2 {
		t.Fatalf("unexpected MP3 header: %+v", mh)
	}
}

func TestInspectFrameHexPaginationAndClamp(t *testing.T) {
	buf := make([]byte, 10000)
	for i := range buf {
		buf[i] = byte('A' + (i % 26))
	}

	// Default limit = 2048
	r1 := InspectFrame(buf, InspectOptions{MediaType: "audio"})
	if r1.Hex.Offset != 0 || r1.Hex.Limit != DefaultHexLimit || r1.Hex.Returned != DefaultHexLimit || !r1.Hex.HasMore {
		t.Fatalf("unexpected default hex page: offset=%d limit=%d returned=%d hasMore=%v", r1.Hex.Offset, r1.Hex.Limit, r1.Hex.Returned, r1.Hex.HasMore)
	}
	if len(r1.Hex.Lines) != DefaultHexLimit/16 || len(r1.Hex.Lines[0].HexBytes) != 16 || len(r1.Hex.Lines[0].ASCII) != 16 {
		t.Fatalf("unexpected 16-col formatting: lines=%d row0=%+v", len(r1.Hex.Lines), r1.Hex.Lines[0])
	}

	// Clamp > MaxHexLimit (8192)
	r2 := InspectFrame(buf, InspectOptions{MediaType: "audio", HexOffset: 1024, HexLimit: 99999})
	if r2.Hex.Offset != 1024 || r2.Hex.Limit != MaxHexLimit || r2.Hex.Returned != MaxHexLimit || !r2.Hex.HasMore {
		t.Fatalf("unexpected clamped hex page: offset=%d limit=%d returned=%d hasMore=%v", r2.Hex.Offset, r2.Hex.Limit, r2.Hex.Returned, r2.Hex.HasMore)
	}

	// Last partial page
	r3 := InspectFrame(buf, InspectOptions{MediaType: "audio", HexOffset: 9990, HexLimit: 64})
	if r3.Hex.Returned != 10 || r3.Hex.HasMore || len(r3.Hex.Lines) != 1 || len(r3.Hex.Lines[0].HexBytes) != 10 {
		t.Fatalf("unexpected tail hex page: %+v", r3.Hex)
	}
}
