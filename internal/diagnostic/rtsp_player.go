package diagnostic

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtpmpeg4audio"
	"github.com/bluenviron/gortsplib/v5/pkg/rtptime"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"github.com/pion/rtp"
)

const rtspLegacyPreviewArchiveVersion = 3
const rtspPreviewArchiveVersion = 4
const rtspPreviewCodecCapabilitiesVersion = 3
const rtspPlayerSchemaVersion = 6

// RTSPPreviewReport describes the local remux pipeline, not a decoded source
// frame or source A/V synchronisation. The wire archive remains authoritative.
type RTSPPreviewReport struct {
	G711                     *RTSPG711PreviewReport `json:"g711,omitempty"`
	Version                  int                    `json:"version"`
	CodecCapabilitiesVersion int                    `json:"codec_capabilities_version,omitempty"`
	VideoOutputCodec         string                 `json:"video_output_codec,omitempty"`
	AudioOutputCodec         string                 `json:"audio_output_codec,omitempty"`
	VideoTransform           string                 `json:"video_transform,omitempty"`
	AudioTransform           string                 `json:"audio_transform,omitempty"`
	State                    string                 `json:"state"`
	ReasonCode               string                 `json:"reason_code,omitempty"`
	ClockAlignment           string                 `json:"clock_alignment"`
	VideoTrackID             int                    `json:"video_track_id,omitempty"`
	AudioTrackID             int                    `json:"audio_track_id,omitempty"`
	VideoCodec               string                 `json:"video_codec,omitempty"`
	AudioCodec               string                 `json:"audio_codec,omitempty"`
	VideoReason              string                 `json:"video_reason,omitempty"`
	AudioReason              string                 `json:"audio_reason,omitempty"`
	VideoAUs                 int                    `json:"video_aus"`
	AudioAUs                 int                    `json:"audio_aus"`
	PacketErrors             int                    `json:"packet_errors"`
	DroppedPackets           int                    `json:"dropped_packets"`
}

type rtspPreviewPacket struct {
	packet *rtp.Packet
	pts    int64
	video  bool
}
type rtspPreview struct {
	mu                         sync.Mutex
	report                     RTSPPreviewReport
	player                     *PlayerSession
	queue                      chan rtspPreviewPacket
	done                       chan struct{}
	closed, failed             bool
	started                    bool
	video                      *format.H264
	hevc                       *rtspHEVCPreview
	audio                      *format.MPEG4Audio
	opus                       *rtspOpusPreview
	g711                       *rtspG711Encoder
	g711Lookup                 func() (string, error)
	g711HeaderReady            chan struct{}
	videoDecoder               *rtph264.Decoder
	audioDecoder               *rtpmpeg4audio.Decoder
	sps, pps                   []byte
	videoReady, audioReady     bool
	lastVideoMS, lastAudioMS   int64
	hasVideoTime, hasAudioTime bool
	dts                        h264.DTSExtractor
	clock                      rtptime.GlobalDecoder
}

func newRTSPPreview(player *PlayerSession) *rtspPreview {
	p := &rtspPreview{player: player, queue: make(chan rtspPreviewPacket, 128), done: make(chan struct{}), report: RTSPPreviewReport{Version: 2, CodecCapabilitiesVersion: rtspPreviewCodecCapabilitiesVersion, State: "failed", ReasonCode: "no_media", ClockAlignment: "receiver_rtp", VideoReason: "no_track", AudioReason: "no_track"}}
	p.clock.Initialize()
	return p
}

// start registers on the existing RTSP client after SETUP. Nothing opens a
// second connection, and no mux work or downstream write runs in the callback.
func (p *rtspPreview) start(client *gortsplib.Client, desc *description.Session) {
	p.started = true
	trackID := 0
	for _, media := range desc.Medias {
		if media.IsBackChannel {
			trackID += len(media.Formats)
			continue
		}
		for _, codec := range media.Formats {
			trackID++
			switch media.Type {
			case description.MediaTypeVideo:
				if p.hasVideo() {
					continue
				}
				p.report.VideoReason = "codec_unsupported"
				if hevc, ok := codec.(*format.H265); ok {
					if hevc.MaxDONDiff != 0 {
						p.report.VideoReason = "packetization_unsupported"
						continue
					}
					if !p.selectHEVC(hevc) {
						p.report.VideoReason = "packetization_unsupported"
						continue
					}
					p.report.VideoTrackID, p.report.VideoCodec, p.report.VideoReason = trackID, "h265", ""
					p.report.VideoOutputCodec, p.report.VideoTransform = "h265", "native_remux"
					client.OnPacketRTP(media, codec, func(packet *rtp.Packet) {
						pts, ok := p.packetPTS(codec, packet)
						if ok {
							p.enqueue(packet, pts, true)
						}
					})
					continue
				}
				video, ok := codec.(*format.H264)
				if !ok {
					continue
				}
				if video.PacketizationMode != 0 && video.PacketizationMode != 1 {
					p.report.VideoReason = "packetization_unsupported"
					continue
				}
				decoder := &rtph264.Decoder{PacketizationMode: 1}
				if decoder.Init() != nil {
					continue
				}
				p.video, p.videoDecoder = video, decoder
				sps, pps := video.SafeParams()
				p.sps, p.pps = bytes.Clone(sps), bytes.Clone(pps)
				p.report.VideoTrackID, p.report.VideoCodec, p.report.VideoReason = trackID, "h264", ""
				p.report.VideoOutputCodec, p.report.VideoTransform = "h264", "native_remux"
				client.OnPacketRTP(media, codec, func(packet *rtp.Packet) {
					pts, ok := p.packetPTS(codec, packet)
					if ok {
						p.enqueue(packet, pts, true)
					}
				})
			case description.MediaTypeAudio:
				if p.hasAudio() {
					continue
				}
				p.report.AudioReason = "codec_unsupported"
				if opus, ok := codec.(*format.Opus); ok {
					state, err := newRTSPOpusPreview(opus.ChannelCount)
					if err != nil {
						p.report.AudioReason = "opus_configuration_unsupported"
						continue
					}
					p.opus = state
					p.report.AudioTrackID, p.report.AudioCodec, p.report.AudioReason = trackID, "opus", ""
					p.report.AudioOutputCodec, p.report.AudioTransform = "opus", "native_remux"
					client.OnPacketRTP(media, codec, func(packet *rtp.Packet) {
						pts, ok := p.packetPTS(codec, packet)
						if ok {
							p.enqueue(packet, pts, false)
						}
					})
					continue
				}
				audio, ok := codec.(*format.MPEG4Audio)
				if !ok {
					continue
				}
				c := audio.Config
				if c == nil || c.Type != mpeg4audio.ObjectTypeAACLC || c.ExtensionType != 0 || c.FrameLengthFlag || c.DependsOnCoreCoder || c.SampleRate < 8000 || c.SampleRate > 96000 || c.ChannelConfig < 1 || c.ChannelConfig > 2 {
					p.report.AudioReason = "aac_configuration_unsupported"
					continue
				}
				decoder, err := audio.CreateDecoder()
				if err != nil {
					p.report.AudioReason = "packetization_unsupported"
					continue
				}
				p.audio, p.audioDecoder = audio, decoder
				p.report.AudioTrackID, p.report.AudioCodec, p.report.AudioReason = trackID, "aac", ""
				p.report.AudioOutputCodec, p.report.AudioTransform = "aac", "native_remux"
				client.OnPacketRTP(media, codec, func(packet *rtp.Packet) {
					pts, ok := p.packetPTS(codec, packet)
					if ok {
						p.enqueue(packet, pts, false)
					}
				})
			}
		}
	}
	p.startG711(client, desc)
	if p.g711 != nil {
		go func() {
			<-p.g711.initialized
			p.begin()
			close(p.g711HeaderReady)
		}()
	} else {
		p.begin()
	}
}

// begin publishes the FLV header only once local audio capability is known.
// Native PLAY and wire recording proceed while optional tool checks run.
func (p *rtspPreview) begin() {
	p.mu.Lock()
	if p.g711 != nil {
		p.g711.mu.Lock()
		if p.g711.failed {
			p.report.AudioReason = p.g711.report.ReasonCode
		}
		p.g711.mu.Unlock()
	}
	if p.failed {
		p.mu.Unlock()
		close(p.done)
		return
	}
	if !p.hasVideo() && !p.hasAudio() {
		reason := "codec_unsupported"
		if p.g711 != nil {
			reason = p.report.AudioReason
		}
		p.failLocked(reason)
		p.report.State = "unsupported"
		p.mu.Unlock()
		close(p.done)
		return
	}
	p.mu.Unlock()
	p.dts.Initialize()
	flags := byte(0)
	if p.hasVideo() {
		flags |= 1
	}
	if p.hasAudio() {
		flags |= 4
	}
	p.player.publish([]byte{'F', 'L', 'V', 1, flags, 0, 0, 0, 9, 0, 0, 0, 0})
	go func() {
		defer close(p.done)
		for item := range p.queue {
			p.mu.Lock()
			failed := p.failed
			p.mu.Unlock()
			if failed {
				continue
			}
			if item.video {
				p.writeVideo(item)
			} else {
				p.writeAudio(item)
			}
		}
	}()
}

func (p *rtspPreview) enqueue(packet *rtp.Packet, pts int64, video bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.failed {
		return
	}
	if packet.MarshalSize() > 64<<10 {
		p.report.DroppedPackets++
		p.failLocked("queue_limit")
		return
	}
	select {
	case p.queue <- rtspPreviewPacket{packet.Clone(), pts, video}:
	default:
		p.report.DroppedPackets++
		p.failLocked("queue_limit")
	}
}
func (p *rtspPreview) failLocked(code string) {
	if p.failed {
		return
	}
	p.failed = true
	p.report.State = "failed"
	p.report.ReasonCode = code
	p.player.failPreview(code)
}
func (p *rtspPreview) fail(code string) { p.mu.Lock(); defer p.mu.Unlock(); p.failLocked(code) }
func (p *rtspPreview) packetError()     { p.mu.Lock(); p.report.PacketErrors++; p.mu.Unlock() }
func (p *rtspPreview) output(video bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.failed {
		p.report.State = "ready"
		p.report.ReasonCode = ""
	}
	if video {
		p.report.VideoAUs++
	} else {
		p.report.AudioAUs++
	}
}
func (p *rtspPreview) finish() RTSPPreviewReport {
	p.mu.Lock()
	p.closed = true
	close(p.queue)
	p.mu.Unlock()
	<-p.done
	p.finishG711()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.failed && p.report.State != "ready" {
		if p.hasVideo() && !p.videoParametersAvailable() {
			p.report.ReasonCode = p.missingVideoParametersCode()
		} else if p.hasVideo() {
			p.report.ReasonCode = "no_random_access"
		}
		p.player.failPreview(p.report.ReasonCode)
	}
	if p.hasVideo() && p.report.VideoAUs == 0 {
		if !p.videoParametersAvailable() {
			p.report.VideoReason = p.missingVideoParametersCode()
		} else {
			p.report.VideoReason = "no_random_access"
		}
	}
	if p.g711 == nil && p.hasAudio() && p.report.AudioAUs == 0 {
		p.report.AudioReason = "no_media"
	}
	if p.player.StreamDropped() && !p.failed {
		p.report.State = "failed"
		p.report.ReasonCode = "queue_limit"
	}
	return p.report
}
func (p *rtspPreview) tag(kind byte, timestamp int64, body []byte) bool {
	if len(body) > 2<<20 {
		p.fail("access_unit_limit")
		return false
	}
	if timestamp < 0 || timestamp > maxPlayerElapsedMS+2000 {
		p.fail("timestamp_unsupported")
		return false
	}
	if kind == 9 {
		if p.hasVideoTime && timestamp < p.lastVideoMS {
			p.fail("timestamp_unsupported")
			return false
		}
		p.lastVideoMS, p.hasVideoTime = timestamp, true
	}
	if kind == 8 {
		if p.hasAudioTime && timestamp < p.lastAudioMS {
			p.fail("timestamp_unsupported")
			return false
		}
		p.lastAudioMS, p.hasAudioTime = timestamp, true
	}

	if !p.player.acceptsMedia() {
		p.fail("player_unavailable")
		return false
	}
	packet := make([]byte, 15+len(body))
	packet[0] = kind
	putFLVUint24(packet[1:4], uint32(len(body)))
	putFLVTimestamp(packet[4:8], uint32(timestamp))
	copy(packet[11:], body)
	binary.BigEndian.PutUint32(packet[11+len(body):], uint32(11+len(body)))
	p.player.publish(packet)
	if p.player.StreamDropped() {
		p.fail("queue_limit")
		return false
	}
	return true
}
func (p *rtspPreview) writeVideo(item rtspPreviewPacket) {
	if p.hevc != nil {
		p.writeHEVCVideo(item)
		return
	}
	if len(item.packet.Payload) == 0 {
		return
	}
	if p.video.PacketizationMode == 0 && item.packet.Payload[0]&31 >= 24 {
		p.packetError()
		p.fail("packetization_unsupported")
		return
	}
	au, err := p.videoDecoder.Decode(item.packet)
	if errors.Is(err, rtph264.ErrMorePacketsNeeded) || errors.Is(err, rtph264.ErrNonStartingPacketAndNoPrevious) {
		return
	}
	if err != nil {
		p.packetError()
		p.videoReady = false
		p.dts.Initialize()
		return
	}
	size := 0
	idr := false
	hasPicture := false
	changed := false
	for _, nalu := range au {
		size += len(nalu)
		if len(nalu) == 0 {
			continue
		}
		switch nalu[0] & 31 {
		case 7:
			if !bytes.Equal(p.sps, nalu) {
				p.sps = bytes.Clone(nalu)
				changed = true
			}
		case 8:
			if !bytes.Equal(p.pps, nalu) {
				p.pps = bytes.Clone(nalu)
				changed = true
			}
		case 5:
			idr = true
			hasPicture = true
		case 1:
			hasPicture = true
		}
	}
	if size > 2<<20 {
		p.fail("access_unit_limit")
		return
	}
	if changed {
		p.videoReady = false
		p.dts.Initialize()
	}
	if !hasPicture || len(p.sps) < 4 || len(p.sps) > 65535 || len(p.pps) == 0 || len(p.pps) > 65535 || (!p.videoReady && !idr) {
		return
	}
	// A shared fixed 2 s shift keeps initial B-frame DTS non-negative while
	// preserving PacketPTS's common receiver clock across audio and video.
	pts := item.pts + 180000
	withSPS := append([][]byte{p.sps}, au...)
	dts, err := p.dts.Extract(withSPS, pts)
	if err != nil {
		p.packetError()
		p.fail("timestamp_unsupported")
		return
	}
	cts := (pts - dts) / 90
	if cts < -(1<<23) || cts >= (1<<23) {
		p.fail("timestamp_unsupported")
		return
	}
	if !p.videoReady {
		config := []byte{0x17, 0, 0, 0, 0, 1, p.sps[1], p.sps[2], p.sps[3], 0xff, 0xe1, byte(len(p.sps) >> 8), byte(len(p.sps))}
		config = append(config, p.sps...)
		config = append(config, 1, byte(len(p.pps)>>8), byte(len(p.pps)))
		config = append(config, p.pps...)
		if !p.tag(9, dts/90, config) {
			return
		}
		p.videoReady = true
	}
	avcc, err := h264.AVCC(au).Marshal()
	if err != nil {
		p.fail("access_unit_limit")
		return
	}
	frame := byte(0x27)
	if idr {
		frame = 0x17
	}
	body := []byte{frame, 1, byte(cts >> 16), byte(cts >> 8), byte(cts)}
	body = append(body, avcc...)
	if p.tag(9, dts/90, body) {
		p.output(true)
	}
}
func (p *rtspPreview) writeAudio(item rtspPreviewPacket) {
	if p.opus != nil {
		p.opus.write(p, item)
		return
	}
	aus, err := p.audioDecoder.Decode(item.packet)
	if errors.Is(err, rtpmpeg4audio.ErrMorePacketsNeeded) {
		return
	}
	if err != nil {
		p.packetError()
		return
	}
	rate := int64(p.audio.ClockRate())
	sound := byte(0xaf)
	if p.audio.Config.ChannelConfig == 1 {
		sound = 0xae
	}
	for index, au := range aus {
		if len(au) > 2<<20 {
			p.fail("access_unit_limit")
			return
		}
		pts := (item.pts+int64(index)*1024)*1000/rate + 2000
		if !p.audioReady {
			config, err := p.audio.Config.Marshal()
			if err != nil {
				p.fail("aac_configuration_unsupported")
				return
			}
			if !p.tag(8, pts, append([]byte{sound, 0}, config...)) {
				return
			}
			p.audioReady = true
		}
		if p.tag(8, pts, append([]byte{sound, 1}, au...)) {
			p.output(false)
		}
	}
}

func validRTSPPreview(p *RTSPPreviewReport, tracks []RTSPTrackMetrics) bool {
	if p == nil || (p.Version != 1 && p.Version != 2) || p.ClockAlignment != "receiver_rtp" {
		return false
	}
	if p.Version == 1 {
		if p.G711 != nil || p.CodecCapabilitiesVersion != 0 || p.VideoOutputCodec != "" || p.AudioOutputCodec != "" || p.VideoTransform != "" || p.AudioTransform != "" {
			return false
		}
	} else {
		if p.CodecCapabilitiesVersion < 1 || p.CodecCapabilitiesVersion > rtspPreviewCodecCapabilitiesVersion {
			return false
		}
		if p.VideoTrackID == 0 {
			if p.VideoOutputCodec != "" || p.VideoTransform != "" {
				return false
			}
		} else if p.VideoOutputCodec != p.VideoCodec || p.VideoTransform != "native_remux" {
			return false
		}
		if p.AudioTrackID == 0 {
			if p.AudioOutputCodec != "" || p.AudioTransform != "" {
				return false
			}
		} else if p.AudioTransform != "g711_to_aac" && (p.AudioOutputCodec != p.AudioCodec || p.AudioTransform != "native_remux") {
			return false
		}
	}
	if !validRTSPG711Preview(p, tracks) {
		return false
	}
	switch p.State {
	case "not_requested":
		if p.ReasonCode != "" || p.VideoTrackID != 0 || p.AudioTrackID != 0 || p.VideoCodec != "" || p.AudioCodec != "" || p.VideoReason != "" || p.AudioReason != "" || p.VideoAUs != 0 || p.AudioAUs != 0 || p.PacketErrors != 0 || p.DroppedPackets != 0 {
			return false
		}
	case "unsupported":
		if p.G711 != nil && p.G711.State == "unavailable" {
			if p.VideoTrackID != 0 || p.VideoAUs != 0 || p.AudioAUs != 0 {
				return false
			}
			break
		}
		if p.VideoTrackID != 0 || p.AudioTrackID != 0 || p.VideoCodec != "" || p.AudioCodec != "" || p.VideoAUs != 0 || p.AudioAUs != 0 || p.PacketErrors != 0 || p.DroppedPackets != 0 {
			return false
		}
	case "ready":
		if p.DroppedPackets != 0 {
			return false
		}
	case "failed":
	default:
		return false
	}
	for _, code := range []string{p.ReasonCode, p.VideoReason, p.AudioReason} {
		if !validRTSPPreviewReason(code) || p.Version == 1 && (code == "missing_hevc_parameters" || validRTSPG711Reason(code)) || code == "opus_configuration_unsupported" && (p.Version != 2 || p.CodecCapabilitiesVersion < 2) || p.Version == 2 && p.CodecCapabilitiesVersion < 3 && validRTSPG711Reason(code) {
			return false
		}
	}
	if (p.State == "failed" || p.State == "unsupported") && p.ReasonCode == "" {
		return false
	}
	if p.VideoTrackID == 0 && p.VideoAUs > 0 || p.AudioTrackID == 0 && p.AudioAUs > 0 {
		return false
	}
	if p.State == "ready" && (p.ReasonCode != "" || p.VideoAUs+p.AudioAUs == 0) {
		return false
	}
	for _, n := range []int{p.VideoAUs, p.AudioAUs, p.PacketErrors, p.DroppedPackets} {
		if n < 0 || n > 1_000_000_000 {
			return false
		}
	}
	selected := func(id int, codec, kind string) bool {
		if id == 0 {
			return codec == ""
		}
		for _, track := range tracks {
			if track.ID == id && track.Codec == codec && track.Kind == kind {
				return true
			}
		}
		return false
	}
	return (p.VideoCodec == "" || p.VideoCodec == "h264" || p.Version == 2 && p.CodecCapabilitiesVersion >= 1 && p.VideoCodec == "h265") && (p.AudioCodec == "" || p.AudioCodec == "aac" || p.Version == 2 && p.CodecCapabilitiesVersion >= 2 && p.AudioCodec == "opus" || p.Version == 2 && p.CodecCapabilitiesVersion >= 3 && (p.AudioCodec == "pcma" || p.AudioCodec == "pcmu")) && selected(p.VideoTrackID, p.VideoCodec, "video") && selected(p.AudioTrackID, p.AudioCodec, "audio")
}
func validRTSPPreviewReason(code string) bool {
	if validRTSPG711Reason(code) {
		return true
	}
	switch code {
	case "", "missing_hevc_parameters", "no_track", "no_media", "codec_unsupported", "packetization_unsupported", "aac_configuration_unsupported", "opus_configuration_unsupported", "queue_limit", "missing_video_parameters", "no_random_access", "timestamp_unsupported", "access_unit_limit", "player_unavailable", "capture_setup_failed":
		return true
	}
	return false
}

func addRTSPPreviewFinding(report *Report) {
	addRTSPG711Finding(report)
	p := report.RTSP.Preview
	if p == nil {
		return
	}
	if p.State == "unsupported" || p.State == "failed" {
		severity := "warn"
		if p.State == "unsupported" {
			severity = "info"
		}
		report.Findings = append(report.Findings, Finding{ID: fmt.Sprintf("F%03d", len(report.Findings)+1), Code: "rtsp_preview_" + p.State, Severity: severity, Message: "本机预览转封装：" + rtspPreviewReasonLabelFor(p, p.ReasonCode) + "；该结果不等同于原生采样或网络失败。", NextCheck: "先对照原生 RTSP 控制和 RTP 接收，再检查选中轨道、编码配置、随机接入点及本机播放器能力。", StartMS: 0, EndMS: report.Manifest.Observation.ElapsedMS, Occurrences: 1})
	}
}

func (p *rtspPreview) hasAudio() bool {
	return p.audio != nil || p.opus != nil || p.g711 != nil && p.g711.available()
}
