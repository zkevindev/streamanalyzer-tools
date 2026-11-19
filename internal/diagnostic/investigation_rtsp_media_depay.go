package diagnostic

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph265"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtpmpeg4audio"
	"github.com/bluenviron/gortsplib/v5/pkg/rtptime"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/g711"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
	"github.com/pion/rtp"
)

type investigationRTSPTrack struct {
	accessPackets, dropped int
	id, media              int
	codec                  format.Format
	clockCodec             rtptime.GlobalDecoderTrack
	output                 codecs.Codec
	name                   string
	rate                   int64
	h264                   *rtph264.Decoder
	h265                   *rtph265.Decoder
	aac                    *rtpmpeg4audio.Decoder
	h264DTS                h264.DTSExtractor
	h265DTS                h265.DTSExtractor
	sps, pps, vps          []byte
	started, pending       bool
	reorder                investigationRTSPReorder
	lastDTS                int64
	hasDTS                 bool
	firstDTS, firstPTS     int64
	first                  bool
}
type investigationRTSPAU struct {
	packets  int
	sample   *fmp4.Sample
	dts, pts int64
	event    Event
	video    bool
}

func investigationRTSPSelect(desc *description.Session, preview *RTSPPreviewReport) ([]*investigationRTSPTrack, error) {
	if desc == nil || preview == nil {
		return nil, fmt.Errorf("缺少原始 SDP 或同次播放器轨道选择，无法建立离线媒体时钟。")
	}
	var result []*investigationRTSPTrack
	id := 0
	for mediaIndex, media := range desc.Medias {
		for _, codec := range media.Formats {
			id++
			if media.IsBackChannel || id != preview.VideoTrackID && id != preview.AudioTrackID {
				continue
			}
			name := rtspCodec(codec)
			if id == preview.VideoTrackID && (media.Type != description.MediaTypeVideo || name != preview.VideoCodec) || id == preview.AudioTrackID && (media.Type != description.MediaTypeAudio || name != preview.AudioCodec) {
				return nil, ErrInvalid
			}
			// An unavailable local G711 encoder never advertised audio in its FLV header.
			if id == preview.AudioTrackID && preview.G711 != nil && preview.G711.State == "unavailable" {
				continue
			}
			track := &investigationRTSPTrack{id: id, media: mediaIndex + 1, codec: codec, clockCodec: codec, name: name, rate: int64(codec.ClockRate())}
			var err error
			switch value := codec.(type) {
			case *format.H264:
				if value.PacketizationMode != 0 && value.PacketizationMode != 1 {
					return nil, fmt.Errorf("H.264 RTP 打包方式不支持离线重建。")
				}
				track.h264 = &rtph264.Decoder{PacketizationMode: 1}
				err = track.h264.Init()
				track.sps, track.pps = value.SafeParams()
				track.sps = bytes.Clone(track.sps)
				track.pps = bytes.Clone(track.pps)
				track.h264DTS.Initialize()
			case *format.H265:
				if value.MaxDONDiff != 0 {
					return nil, fmt.Errorf("H.265 DON 打包方式不支持离线重建。")
				}
				track.h265, err = value.CreateDecoder()
				track.vps, track.sps, track.pps = value.SafeParams()
				track.vps = bytes.Clone(track.vps)
				track.sps = bytes.Clone(track.sps)
				track.pps = bytes.Clone(track.pps)
				track.h265DTS.Initialize()
				track.clockCodec = &hevcClockTrack{value}
			case *format.MPEG4Audio:
				if value.Config == nil || value.Config.Type != 2 || value.Config.ExtensionType != 0 || value.Config.FrameLengthFlag || value.Config.DependsOnCoreCoder || value.Config.SampleRate < 8000 || value.Config.SampleRate > 96000 || value.Config.ChannelConfig < 1 || value.Config.ChannelConfig > 2 {
					return nil, fmt.Errorf("AAC 配置不支持安全离线重建。")
				}
				track.aac, err = value.CreateDecoder()
				track.output = &codecs.MPEG4Audio{Config: *value.Config}
			case *format.Opus:
				channels := value.ChannelCount
				if _, err = rtspOpusHead(channels); err == nil {
					track.output = &codecs.Opus{ChannelCount: channels}
				}
			case *format.G711:
				if !rtspG711Configuration(name, value.SampleRate, value.ChannelCount) {
					return nil, fmt.Errorf("G.711 配置不支持离线 PCM 解码。")
				}
				track.output = &codecs.LPCM{LittleEndian: false, BitDepth: 16, SampleRate: value.SampleRate, ChannelCount: value.ChannelCount}
			default:
				return nil, fmt.Errorf("选中轨道编码不支持离线媒体重建。")
			}
			if err != nil || track.rate <= 0 || track.rate > 96000 {
				return nil, fmt.Errorf("无法初始化 RTP 媒体解包器。")
			}
			result = append(result, track)
		}
	}
	if len(result) == 0 || len(result) > 2 {
		return nil, fmt.Errorf("没有可离线重建的选中音视频轨道。")
	}
	return result, nil
}
func (t *investigationRTSPTrack) video() bool { return t.h264 != nil || t.h265 != nil }
func (t *investigationRTSPTrack) decode(packet *rtp.Packet, pts int64, event Event, discover bool, emit func(investigationRTSPAU) error) error {
	if t.video() {
		return t.decodeVideo(packet, pts, event, discover, emit)
	}
	if discover {
		return nil
	}
	var payloads [][]byte
	var duration int64
	switch value := t.codec.(type) {
	case *format.MPEG4Audio:
		t.pending = true
		t.accessPackets++
		var err error
		payloads, err = t.aac.Decode(packet)
		if errors.Is(err, rtpmpeg4audio.ErrMorePacketsNeeded) {
			return nil
		}
		t.pending = false
		t.accessPackets = 0
		if err != nil {
			return fmt.Errorf("AAC RTP 访问单元不完整，未生成损坏片段。")
		}
		duration = 1024
	case *format.Opus:
		var err error
		duration, err = rtspOpusPacketSamples(packet.Payload)
		if err != nil {
			return fmt.Errorf("Opus RTP 帧长度无效。")
		}
		payloads = [][]byte{packet.Payload}
	case *format.G711:
		if len(packet.Payload) == 0 || len(packet.Payload)%value.ChannelCount != 0 || len(packet.Payload) > 64<<10 {
			return fmt.Errorf("G.711 RTP 样本长度无效。")
		}
		var pcm []byte
		if value.MULaw {
			var data g711.Mulaw
			data.Unmarshal(packet.Payload)
			pcm = data
		} else {
			var data g711.Alaw
			data.Unmarshal(packet.Payload)
			pcm = data
		}
		duration = int64(len(packet.Payload) / value.ChannelCount)
		payloads = [][]byte{pcm}
	}
	for index, payload := range payloads {
		at := pts + int64(index)*duration + 2*t.rate
		if len(payload) == 0 || len(payload) > 2<<20 || duration <= 0 || duration > t.rate || t.hasDTS && at < t.lastDTS {
			return fmt.Errorf("音频 RTP 时间线重叠或访问单元越界。")
		}
		t.noteTime(at, at)
		sample := &fmp4.Sample{Duration: uint32(duration), Payload: bytes.Clone(payload)}
		if err := emit(investigationRTSPAU{sample: sample, dts: at, pts: at, event: event}); err != nil {
			return err
		}
	}
	return nil
}
func (t *investigationRTSPTrack) noteTime(dts, pts int64) {
	if !t.first {
		t.first = true
		t.firstDTS = dts
		t.firstPTS = pts
	}
	t.hasDTS = true
	t.lastDTS = dts
}
func (t *investigationRTSPTrack) decodeVideo(packet *rtp.Packet, pts int64, event Event, discover bool, emit func(investigationRTSPAU) error) error {
	var au [][]byte
	var err error
	t.pending = true
	t.accessPackets++
	if t.h264 != nil {
		if t.codec.(*format.H264).PacketizationMode == 0 && len(packet.Payload) > 0 && packet.Payload[0]&31 >= 24 {
			return fmt.Errorf("H.264 packetization-mode=0 含有聚合或分片包。")
		}
		au, err = t.h264.Decode(packet)
	} else {
		au, err = t.h265.Decode(packet)
	}
	if errors.Is(err, rtph264.ErrMorePacketsNeeded) || errors.Is(err, rtph265.ErrMorePacketsNeeded) {
		return nil
	}
	if errors.Is(err, rtph264.ErrNonStartingPacketAndNoPrevious) || errors.Is(err, rtph265.ErrNonStartingPacketAndNoPrevious) {
		t.pending = false
		t.dropped += t.accessPackets
		t.accessPackets = 0
		return nil
	}
	t.pending = false
	packets := t.accessPackets
	t.accessPackets = 0
	if err != nil {
		return fmt.Errorf("视频 RTP 访问单元不完整，未拼接损坏画面。")
	}
	size := 0
	picture, random := false, false
	for _, nalu := range au {
		size += len(nalu)
		if len(nalu) == 0 {
			return ErrInvalid
		}
		kind := int(nalu[0] & 31)
		if t.h265 != nil {
			if !validHEVCNALUHeader(nalu) {
				return ErrInvalid
			}
			kind = int(nalu[0] >> 1 & 63)
		}
		param := func(target *[]byte) error {
			if !discover && !bytes.Equal(*target, nalu) {
				return fmt.Errorf("视频参数在采样中改变，未跨配置拼接异常片段。")
			}
			*target = bytes.Clone(nalu)
			return nil
		}
		if t.h264 != nil {
			switch kind {
			case 7:
				err = param(&t.sps)
			case 8:
				err = param(&t.pps)
			case 5:
				picture = true
				random = true
			case 1:
				picture = true
			}
		} else {
			switch kind {
			case 32:
				err = param(&t.vps)
			case 33:
				err = param(&t.sps)
			case 34:
				err = param(&t.pps)
			default:
				if kind <= 31 {
					picture = true
					random = kind >= 16 && kind <= 21
				}
			}
		}
		if err != nil {
			return err
		}
	}
	if size > 2<<20 {
		return fmt.Errorf("视频访问单元超过重建上限。")
	}
	if !picture || !t.started && !random {
		t.dropped += packets
		return nil
	}
	if discover {
		if t.started {
			return nil
		}
		if t.h264 != nil {
			if len(t.sps) < 4 || len(t.pps) == 0 {
				return nil
			}
			t.output = &codecs.H264{SPS: bytes.Clone(t.sps), PPS: bytes.Clone(t.pps)}
		} else {
			if len(t.vps) < 2 || len(t.sps) < 15 || len(t.pps) < 2 {
				return nil
			}
			if _, err := hevcConfigurationRecord(t.vps, t.sps, t.pps); err != nil {
				return err
			}
			t.output = &codecs.H265{VPS: bytes.Clone(t.vps), SPS: bytes.Clone(t.sps), PPS: bytes.Clone(t.pps)}
		}
		t.started = true
		return nil
	}
	pts += 180000
	var dts int64
	if t.h264 != nil {
		dts, err = t.h264DTS.Extract(append([][]byte{t.sps}, au...), pts)
	} else {
		dts, err = t.h265DTS.Extract(hevcTimingAU(t.sps, t.pps, au), pts)
	}
	if err != nil || dts < 0 || dts > maxPlayerElapsedMS*90+180000 || pts-dts < -1<<31 || pts-dts > 1<<31-1 || t.hasDTS && dts <= t.lastDTS {
		return fmt.Errorf("无法可靠恢复视频 B 帧的 DTS/PTS 顺序。")
	}
	sample := &fmp4.Sample{}
	if t.h264 != nil {
		err = sample.FillH264(int32(pts-dts), au)
	} else {
		err = sample.FillH265(int32(pts-dts), au)
	}
	if err != nil {
		return err
	}
	t.noteTime(dts, pts)
	t.started = true
	return emit(investigationRTSPAU{sample: sample, dts: dts, pts: pts, event: event, video: true, packets: packets})
}

// A missing RTP packet invalidates the current AU, not the entire archive.
// Video can restart only at a later actual random-access picture.
func (t *investigationRTSPTrack) resetLoss() error {
	if t.accessPackets > 0 {
		t.dropped += t.accessPackets
		t.accessPackets = 0
	}
	t.pending = false
	if t.h264 != nil {
		t.h264 = &rtph264.Decoder{PacketizationMode: 1}
		if err := t.h264.Init(); err != nil {
			return err
		}
		t.h264DTS.Initialize()
		t.started = false
	}
	if t.h265 != nil {
		var err error
		t.h265, err = t.codec.(*format.H265).CreateDecoder()
		if err != nil {
			return err
		}
		t.h265DTS.Initialize()
		t.started = false
	}
	if t.aac != nil {
		var err error
		t.aac, err = t.codec.(*format.MPEG4Audio).CreateDecoder()
		if err != nil {
			return err
		}
	}
	return nil
}
