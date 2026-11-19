package diagnostic

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph265"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
	"github.com/pion/rtp"
)

type rtspHEVCPreview struct {
	decoder       *rtph265.Decoder
	vps, sps, pps []byte
	dts           h265.DTSExtractor
	clockTrack    *hevcClockTrack
}

func (p *rtspPreview) hasVideo() bool { return p.video != nil || p.hevc != nil }
func (p *rtspPreview) videoParametersAvailable() bool {
	if p.hevc != nil {
		return len(p.hevc.vps) >= 2 && len(p.hevc.sps) >= 15 && len(p.hevc.pps) >= 2
	}
	return len(p.sps) >= 4 && len(p.pps) > 0
}
func (p *rtspPreview) missingVideoParametersCode() string {
	if p.hevc != nil {
		return "missing_hevc_parameters"
	}
	return "missing_video_parameters"
}
func (p *rtspPreview) selectHEVC(codec *format.H265) bool {
	decoder, err := codec.CreateDecoder()
	if err != nil {
		return false
	}
	vps, sps, pps := codec.SafeParams()
	p.hevc = &rtspHEVCPreview{decoder: decoder, vps: bytes.Clone(vps), sps: bytes.Clone(sps), pps: bytes.Clone(pps)}
	p.hevc.dts.Initialize()
	p.hevc.clockTrack = &hevcClockTrack{codec}
	return true
}

// hevcConfigurationRecord keeps the exact encoded parameter NALUs while
// taking profile/tier/compatibility/constraint bits from unescaped SPS RBSP.
// Using raw SPS offsets would misread emulation-prevention bytes as flags.
func hevcConfigurationRecord(vps, sps, pps []byte) ([]byte, error) {
	for _, nalu := range [][]byte{vps, sps, pps} {
		if !validHEVCNALUHeader(nalu) || len(nalu) > 65535 {
			return nil, ErrInvalid
		}
	}
	if (vps[0]>>1)&63 != 32 || (sps[0]>>1)&63 != 33 || (pps[0]>>1)&63 != 34 {
		return nil, ErrInvalid
	}
	var parsed h265.SPS
	if parsed.Unmarshal(sps) != nil {
		return nil, ErrInvalid
	}
	var parsedPPS h265.PPS
	if parsedPPS.Unmarshal(pps) != nil {
		return nil, ErrInvalid
	}
	rbsp := h264.EmulationPreventionRemove(sps[2:])
	if len(rbsp) < 13 || parsed.MaxSubLayersMinus1 > 6 || parsed.ChromaFormatIdc > 3 || parsed.BitDepthLumaMinus8 > 7 || parsed.BitDepthChromaMinus8 > 7 {
		return nil, ErrInvalid
	}
	out := make([]byte, 23)
	out[0] = 1
	copy(out[1:13], rbsp[1:13])
	out[13], out[14] = 0xf0, 0
	out[15] = 0xfc
	out[16] = 0xfc | byte(parsed.ChromaFormatIdc)
	out[17] = 0xf8 | byte(parsed.BitDepthLumaMinus8)
	out[18] = 0xf8 | byte(parsed.BitDepthChromaMinus8)
	out[21] = (parsed.MaxSubLayersMinus1+1)<<3 | 3
	if parsed.TemporalIDNestingFlag {
		out[21] |= 4
	}
	out[22] = 3
	for index, nalu := range [][]byte{vps, sps, pps} {
		out = append(out, 0x80|byte(32+index), 0, 1, byte(len(nalu)>>8), byte(len(nalu)))
		out = append(out, nalu...)
	}
	return out, nil
}
func (p *rtspPreview) writeHEVCVideo(item rtspPreviewPacket) {
	h := p.hevc
	au, err := h.decoder.Decode(item.packet)
	if errors.Is(err, rtph265.ErrMorePacketsNeeded) || errors.Is(err, rtph265.ErrNonStartingPacketAndNoPrevious) {
		return
	}
	if err != nil {
		p.packetError()
		p.videoReady = false
		h.dts.Initialize()
		return
	}
	size := 0
	changed, picture, random := false, false, false
	for _, nalu := range au {
		size += len(nalu)
		if !validHEVCNALUHeader(nalu) {
			p.packetError()
			p.videoReady = false
			h.dts.Initialize()
			return
		}
		kind := (nalu[0] >> 1) & 63
		switch kind {
		case 32:
			if !bytes.Equal(h.vps, nalu) {
				h.vps = bytes.Clone(nalu)
				changed = true
			}
		case 33:
			if !bytes.Equal(h.sps, nalu) {
				h.sps = bytes.Clone(nalu)
				changed = true
			}
		case 34:
			if !bytes.Equal(h.pps, nalu) {
				h.pps = bytes.Clone(nalu)
				changed = true
			}
		case 16, 17, 18, 19, 20, 21:
			random = true
			picture = true
		default:
			if kind <= 31 {
				picture = true
			}
		}
	}
	if size > 2<<20 {
		p.fail("access_unit_limit")
		return
	}
	if changed {
		p.videoReady = false
		h.dts.Initialize()
	}
	if !picture || !p.videoParametersAvailable() || !p.videoReady && !random {
		return
	}
	pts := item.pts + 180000
	dts, err := h.dts.Extract(hevcTimingAU(h.sps, h.pps, au), pts)
	if err != nil {
		p.packetError()
		p.fail("timestamp_unsupported")
		return
	}
	cts := (pts - dts) / 90
	if cts < -(1<<23) || cts >= 1<<23 {
		p.fail("timestamp_unsupported")
		return
	}
	if !p.videoReady {
		config, err := hevcConfigurationRecord(h.vps, h.sps, h.pps)
		if err != nil {
			p.fail("missing_hevc_parameters")
			return
		}
		// Enhanced RTMP VideoTagHeader: IsExHeader + keyframe + SequenceStart.
		if !p.tag(9, dts/90, append([]byte{0x90, 'h', 'v', 'c', '1'}, config...)) {
			return
		}
		p.videoReady = true
	}
	// Length-prefixed NALUs are shared by AVC and HEVC. Packet type 1 carries
	// signed 24-bit CTS; frame timestamps remain monotonic decode timestamps.
	payload, err := h264.AVCC(au).Marshal()
	if err != nil {
		p.fail("access_unit_limit")
		return
	}
	header := byte(0xa1)
	if random {
		header = 0x91
	}
	body := []byte{header, 'h', 'v', 'c', '1', byte(cts >> 16), byte(cts >> 8), byte(cts)}
	body = append(body, payload...)
	if p.tag(9, dts/90, body) {
		p.output(true)
	}
}

// Kept separate from raw transport version validation so v1/v2 reports and
// preview-v1 archives keep their original DTO and rule/finding projection.
func validRTSPPreviewArchiveVersion(metadata rtspArchiveMetadata, data []byte) bool {
	if metadata.Version < rtspLegacyPreviewArchiveVersion {
		return true
	}
	if metadata.Preview == nil {
		return false
	}
	if metadata.Version == rtspPreviewArchiveVersion {
		return metadata.Preview.Version == 2
	}
	if metadata.Preview.Version != 1 {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return false
	}
	var previewFields map[string]json.RawMessage
	if json.Unmarshal(fields["preview"], &previewFields) != nil {
		return false
	}
	for _, key := range []string{"codec_capabilities_version", "video_output_codec", "audio_output_codec", "video_transform", "audio_transform"} {
		if _, exists := previewFields[key]; exists {
			return false
		}
	}
	return true
}

func validHEVCNALUHeader(nalu []byte) bool {
	return len(nalu) >= 2 && nalu[0]&0x80 == 0 && nalu[1]&7 != 0
}

// BLA pictures reset reference-picture state just like IDR for DTS anchoring.
// The pinned extractor handles IDR/CRA but omits BLA from its switch. Adapt
// only that timing input; emitted and archived NALUs keep their original type.
func hevcTimingAU(sps, pps []byte, au [][]byte) [][]byte {
	result := make([][]byte, 0, len(au)+2)
	result = append(result, sps, pps)
	for _, nalu := range au {
		kind := (nalu[0] >> 1) & 63
		if kind >= 16 && kind <= 18 {
			copy := bytes.Clone(nalu)
			copy[0] = (copy[0] & 0x81) | 20<<1
			result = append(result, copy)
		} else {
			result = append(result, nalu)
		}
	}
	return result
}

type hevcClockTrack struct{ *format.H265 }

func (t *hevcClockTrack) PTSEqualsDTS(packet *rtp.Packet) bool {
	payload := packet.Payload
	if !validHEVCNALUHeader(payload) {
		return false
	}
	anchor := func(kind byte) bool { return kind >= 16 && kind <= 21 || kind >= 32 && kind <= 34 }
	kind := (payload[0] >> 1) & 63
	if anchor(kind) {
		return true
	}
	if kind == 49 {
		return len(payload) >= 3 && payload[2]&0x80 != 0 && anchor(payload[2]&63)
	}
	if kind == 48 {
		for rest := payload[2:]; len(rest) >= 2; {
			size := int(rest[0])<<8 | int(rest[1])
			rest = rest[2:]
			if size < 2 || size > len(rest) {
				return false
			}
			if validHEVCNALUHeader(rest[:size]) && anchor((rest[0]>>1)&63) {
				return true
			}
			rest = rest[size:]
		}
	}
	return false
}
func (p *rtspPreview) packetPTS(codec format.Format, packet *rtp.Packet) (int64, bool) {
	// All selected audio/video tracks share one receiver clock; never zero each
	// codec separately. The wrapper additionally recognises BLA clock anchors.
	if _, ok := codec.(*format.H265); ok && p.hevc != nil {
		return p.clock.Decode(p.hevc.clockTrack, packet)
	}
	return p.clock.Decode(codec, packet)
}
