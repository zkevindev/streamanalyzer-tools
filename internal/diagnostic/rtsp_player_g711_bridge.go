package diagnostic

import (
	"encoding/json"
	"fmt"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/pion/rtp"
)

func (p *rtspPreview) startG711(client *gortsplib.Client, desc *description.Session) {
	// Prefer a native supported audio track. G711 is selected only if no AAC
	// (or another native audio bridge) was selected during the ordinary pass.
	if p.hasAudio() {
		return
	}
	id := 0
	for _, media := range desc.Medias {
		for _, codec := range media.Formats {
			id++
			value, ok := codec.(*format.G711)
			if !ok || media.IsBackChannel || media.Type != description.MediaTypeAudio {
				continue
			}
			name := "pcma"
			if value.MULaw {
				name = "pcmu"
			}
			p.report.AudioTrackID, p.report.AudioCodec = id, name
			p.report.AudioOutputCodec, p.report.AudioTransform = "aac", "g711_to_aac"
			p.report.AudioReason = ""
			p.g711 = newRTSPG711Encoder(rtspG711Config{codec: name, rate: value.SampleRate, channels: value.ChannelCount}, p.g711Lookup, func(at int64, asc, au []byte) bool {
				p.mu.Lock()
				failed := p.failed
				p.mu.Unlock()
				if failed {
					return false
				}
				sound := byte(0xae)
				if value.ChannelCount == 2 {
					sound = 0xaf
				}
				if !p.audioReady {
					if !p.tag(8, at, append([]byte{sound, 0}, asc...)) {
						return false
					}
					p.audioReady = true
				}
				if !p.tag(8, at, append([]byte{sound, 1}, au...)) {
					return false
				}
				p.output(false)
				return true
			}, p.fail)
			if !p.g711.available() {
				p.report.AudioReason = p.g711.report.ReasonCode
				return
			}
			client.OnPacketRTP(media, codec, func(packet *rtp.Packet) {
				pts, ok := p.packetPTS(codec, packet)
				if ok {
					p.g711.enqueue(packet, pts)
				}
			})
			return
		}
	}
}

func (p *rtspPreview) finishG711() {
	if p.g711 == nil {
		return
	}
	value := p.g711.finish()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.report.G711 = &value
	p.report.PacketErrors += value.PacketErrors
	p.report.DroppedPackets += value.DroppedPackets
	if value.State != "ready" {
		p.report.AudioReason = value.ReasonCode
	}
}

func validRTSPG711Preview(preview *RTSPPreviewReport, tracks []RTSPTrackMetrics) bool {
	value := preview.G711
	if value == nil {
		return preview.AudioTransform != "g711_to_aac" && preview.AudioCodec != "pcma" && preview.AudioCodec != "pcmu"
	}
	if preview.Version != 2 || preview.CodecCapabilitiesVersion < 3 || preview.AudioTransform != "g711_to_aac" || preview.AudioOutputCodec != "aac" || (preview.AudioCodec != "pcma" && preview.AudioCodec != "pcmu") || preview.AudioTrackID == 0 || value.Version != 1 || value.Encoder != "ffmpeg" || value.OutputSampleRate != rtspG711OutputRate || value.InputSampleRate <= 0 || value.InputSampleRate > 192000 || value.Channels < 1 || value.Channels > 8 || value.OutputAUs != preview.AudioAUs {
		return false
	}
	matched := false
	for _, track := range tracks {
		if track.ID == preview.AudioTrackID && track.Kind == "audio" && track.Codec == preview.AudioCodec && track.ClockRate == value.InputSampleRate && track.Channels == value.Channels {
			matched = true
		}
	}
	if !rtspG711Configuration(preview.AudioCodec, value.InputSampleRate, value.Channels) && !(value.State == "unavailable" && value.ReasonCode == "g711_configuration_unsupported") {
		return false
	}
	if !matched {
		return false
	}
	switch value.State {
	case "ready":
		if value.OutputAUs == 0 || value.ReasonCode != "" {
			return false
		}
	case "no_media":
		if value.OutputAUs != 0 || value.ReasonCode != "audio_no_media" {
			return false
		}
	case "unavailable":
		if value.OutputAUs != 0 || value.InputSamples != 0 {
			return false
		}
	case "failed":
		if value.ReasonCode == "" {
			return false
		}
	default:
		return false
	}
	if value.ReasonCode != "" && !validRTSPG711Reason(value.ReasonCode) {
		return false
	}
	for _, count := range []int64{int64(value.InputPackets), value.InputSamples, value.InsertedSilenceSamples, value.DroppedLateSamples, int64(value.DroppedPackets), int64(value.PacketErrors), int64(value.TimelineResets), int64(value.OutputAUs), value.OutputSamples} {
		if count < 0 || count > 1_000_000_000 {
			return false
		}
	}
	if value.InputSamples > int64(maxSeconds+2)*int64(value.InputSampleRate) || value.InsertedSilenceSamples > 5*int64(value.InputSampleRate) || value.OutputSamples > int64(maxSeconds+4)*rtspG711OutputRate {
		return false
	}
	if (value.InputSamples > 0) != (value.FirstInputPTS != nil) || (value.OutputAUs > 0) != (value.EncoderPrimingSamples != nil && value.EncoderPaddingSamples != nil && value.StartupMS != nil) {
		return false
	}
	if value.FirstInputPTS != nil && (*value.FirstInputPTS < -int64(value.InputSampleRate)*2 || *value.FirstInputPTS > maxPlayerElapsedMS*int64(value.InputSampleRate)/1000) {
		return false
	}
	for _, clock := range []*int64{value.ToolCheckMS, value.StartupMS, value.DrainMS} {
		if clock != nil && (*clock < 0 || *clock > maxPlayerElapsedMS) {
			return false
		}
	}
	if value.EncoderPrimingSamples != nil && (*value.EncoderPrimingSamples < 0 || *value.EncoderPrimingSamples > rtspG711OutputRate) {
		return false
	}
	if value.EncoderPaddingSamples != nil && (*value.EncoderPaddingSamples < 0 || *value.EncoderPaddingSamples >= rtspG711AACSamples) {
		return false
	}
	return true
}

func validRTSPG711Reason(code string) bool {
	switch code {
	case "ffmpeg_unavailable", "ffmpeg_unsupported", "ffmpeg_start_failed", "ffmpeg_failed", "ffmpeg_output_invalid", "ffmpeg_drain_timeout", "g711_configuration_unsupported", "g711_packet_invalid", "g711_queue_limit", "g711_timeline_discontinuity", "g711_silence_limit", "g711_sample_limit", "audio_no_media":
		return true
	}
	return false
}

func validRTSPG711ArchiveFields(preview *RTSPPreviewReport, data []byte) bool {
	var outer map[string]json.RawMessage
	if json.Unmarshal(data, &outer) != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(outer["preview"], &fields) != nil {
		return false
	}
	_, exists := fields["g711"]
	if preview.Version < 2 || preview.CodecCapabilitiesVersion < 3 {
		return !exists
	}
	if exists {
		return preview.G711 != nil
	}
	return preview.G711 == nil
}

func rtspG711ReasonLabel(code string) string {
	switch code {
	case "ffmpeg_unavailable":
		return "未找到 FFmpeg，G711 音频预览不可用"
	case "ffmpeg_unsupported":
		return "本机 FFmpeg 不支持所需的 AAC 转码输出"
	case "ffmpeg_start_failed":
		return "本机 FFmpeg 音频编码器无法启动"
	case "ffmpeg_failed":
		return "本机 G711 音频转码进程失败"
	case "ffmpeg_output_invalid":
		return "AAC 输出与编码时序元数据不一致，已停止预览"
	case "ffmpeg_drain_timeout":
		return "音频编码器结束排空超时，已停止预览"
	case "g711_configuration_unsupported":
		return "G711 采样率或声道配置不受本版支持"
	case "g711_packet_invalid":
		return "G711 包大小或声道采样长度超出本版预览范围"
	case "g711_queue_limit":
		return "本机 G711 转码队列达到上限，原始采样继续"
	case "g711_timeline_discontinuity":
		return "G711 RTP 时钟或 SSRC 出现较大不连续，未重新归零音频"
	case "g711_silence_limit":
		return "已达到按 RTP 缺口补入静音的上限"
	case "g711_sample_limit":
		return "音频样本时间范围达到预览上限"
	case "audio_no_media":
		return "没有得到可供预览的 G711 音频输出"
	}
	return ""
}

func addRTSPG711Finding(report *Report) {
	preview := report.RTSP.Preview
	if preview == nil || preview.G711 == nil || preview.State != "ready" || preview.AudioReason == "" {
		return
	}
	report.Findings = append(report.Findings, Finding{ID: fmt.Sprintf("F%03d", len(report.Findings)+1), Code: "rtsp_preview_audio_unavailable", Severity: "info", Message: rtspG711ReasonLabel(preview.AudioReason) + "；视频转封装与原始 RTP 采样独立保留。", NextCheck: "核对本机 FFmpeg 与音频转码状态，不将该结果解释为上游网络故障。", StartMS: 0, EndMS: report.Manifest.Observation.ElapsedMS, Occurrences: 1})
}
