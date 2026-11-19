package diagnostic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/pion/rtp"
)

const investigationRTSPAnchorAgeMS int64 = 1000

func investigationRTSPAnchor(report Report, item StallInvestigation) (PlayerBufferSample, error) {
	if report.Player == nil || report.Player.Source != "same_capture" || report.Manifest.Player == nil || report.Player.Revision != item.PlayerRevision {
		return PlayerBufferSample{}, fmt.Errorf("原始 RTP 缺少同次播放器记录及媒体时钟，未按收流时间猜测片段位置。")
	}
	var selected *PlayerBufferSample
	for index := range report.Player.BufferSamples {
		value := &report.Player.BufferSamples[index]
		if value.OffsetMS > item.Stall.StartMS || item.Stall.StartMS-value.OffsetMS > investigationRTSPAnchorAgeMS || value.Paused || value.Seeking || value.Hidden || value.ReadyState < 2 || value.CurrentMS < 0 {
			continue
		}
		if selected == nil || value.OffsetMS > selected.OffsetMS {
			selected = value
		}
	}
	if selected == nil {
		return PlayerBufferSample{}, fmt.Errorf("卡顿附近缺少前台、非跳转的近期播放器媒体时钟，未生成位置不明的 RTSP 片段。")
	}
	// A later lifecycle transition invalidates an earlier apparently usable sample.
	for _, event := range report.Player.Events {
		if at := event.AtMS + report.Player.StartOffsetMS; at > selected.OffsetMS && at <= item.Stall.StartMS {
			switch event.Kind {
			case "pause", "seeking", "hidden", "ended", "error", "stop":
				return PlayerBufferSample{}, fmt.Errorf("媒体时钟采样后播放器已暂停、跳转或离开前台，无法安全定位片段。")
			}
		}
	}
	return *selected, nil
}

func investigationMediaRTSP(ctx context.Context, dir, stage string, report Report, item StallInvestigation) (investigationMediaInput, error) {
	input := investigationMediaInput{alignment: "receiver_rtp_estimate"}
	evidence, err := reconstructRTSPMedia(ctx, dir, filepath.Join(stage, "input.mp4"), report, item)
	if err != nil {
		return input, err
	}
	input.path = filepath.Join(stage, "input.mp4")
	input.rtsp = evidence
	input.partial = evidence.IncompleteAccessUnits > 0
	for _, track := range report.RTSP.Tracks {
		if (track.ID == evidence.VideoTrackID || track.ID == evidence.AudioTrackID) && track.MissingSequencePackets > 0 {
			input.partial = true
		}
	}
	input.anchorMS = evidence.AnchorCurrentMS + evidence.PreviewOriginMS - evidence.MediaOriginMS
	if input.anchorMS < 0 || input.anchorMS > evidence.DurationMS {
		return input, fmt.Errorf("播放器媒体时钟不在可重建的 RTP 内容范围内，未提取不相关片段。")
	}
	input.note = "片段与截图由原始 RTP 离线解包生成，位置按同次播放器媒体时钟和接收端 RTP 时钟估算；不代表卡顿时屏幕的确切画面或发送端音画同步。派生文件保留媒体时间，不重演网络等待。"
	if input.partial {
		input.note += " 原始 RTP 含有缺口或尾部不完整访问单元，已丢弃不完整内容并仅保留可安全重建的部分。"
	}
	if evidence.AudioTransform == "g711_to_pcm" {
		input.note += " G.711 先在本机解码为 PCM，再由片段编码器转换为 AAC。"
	}
	return input, nil
}

// Import validation uses only bounded Go parsing and deterministic muxing. The
// scratch file is necessary because MP4 serialization backpatches box sizes.
func recomputeRTSPMediaEvidence(ctx context.Context, dir string, report Report, item StallInvestigation) (*RTSPMediaEvidence, error) {
	stage, err := os.MkdirTemp(dir, ".rtsp-media-verify-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	return reconstructRTSPMedia(ctx, dir, filepath.Join(stage, "input.mp4"), report, item)
}

func reconstructRTSPMedia(ctx context.Context, dir, path string, report Report, item StallInvestigation) (*RTSPMediaEvidence, error) {
	sample, err := investigationRTSPAnchor(report, item)
	if err != nil {
		return nil, err
	}
	if !isRTSPProtocol(report.Manifest.Protocol) || report.RTSP == nil || !validRTSPPreview(report.RTSP.Preview, report.RTSP.Tracks) {
		return nil, fmt.Errorf("RTSP 原始证据或预览轨道信息无效。")
	}
	if !sourceArtifactMatches(dir, rtspStreamName, report.Manifest.Stream, maxStreamBytes) || !sourceArtifactMatches(dir, timelineName, report.Manifest.Timeline, maxTimeline) {
		return nil, fmt.Errorf("RTSP 原始证据与接收时间线校验不一致。")
	}
	if report.RTSP.Preview.State == "unsupported" || report.RTSP.Preview.VideoAUs+report.RTSP.Preview.AudioAUs == 0 {
		return nil, fmt.Errorf("原预览没有可定位的媒体输出，未假定播放器使用了原始轨道。")
	}
	var configured []*investigationRTSPTrack
	discovery := &rtspWireExport{describe: func(desc *description.Session) error {
		var err error
		configured, err = investigationRTSPSelect(desc, report.RTSP.Preview)
		for _, track := range configured {
			track.reorder.onGap = track.resetLoss
		}
		return err
	}}
	discovery.packet = func(media int, data []byte, event Event) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var packet rtp.Packet
		if packet.Unmarshal(data) != nil {
			return ErrInvalid
		}
		track := investigationRTSPFind(configured, media, packet.PayloadType)
		if track == nil {
			return nil
		}
		return track.reorder.push(&packet, event, func(packet *rtp.Packet, event Event) error {
			if track.video() && track.started {
				return nil
			}
			return track.decode(packet, 0, event, true, nil)
		})
	}
	if _, err := readRTSPWire(dir, report.Manifest, nil, discovery); err != nil {
		return nil, err
	}
	for _, track := range configured {
		if err := track.reorder.finish(); err != nil {
			return nil, err
		}
		if track.output == nil {
			return nil, fmt.Errorf("采样中缺少可解码的随机接入画面或完整视频参数。")
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	writer := &investigationRTSPFile{File: file}
	proof := &RTSPMediaEvidence{Version: 1, SourceArtifact: report.Manifest.Stream, TimelineArtifact: report.Manifest.Timeline, PlayerArtifact: *report.Manifest.Player, PlayerRevision: report.Player.Revision, AnchorSampleOffsetMS: sample.OffsetMS, AnchorCurrentMS: sample.CurrentMS, Keyframes: []Event{}}
	init := fmp4.Init{}
	for _, track := range configured {
		init.Tracks = append(init.Tracks, &fmp4.InitTrack{ID: track.id, TimeScale: uint32(track.rate), Codec: track.output})
		if track.video() {
			proof.VideoTrackID = track.id
			proof.VideoCodec = track.name
		} else {
			proof.AudioTrackID = track.id
			proof.AudioCodec = track.name
			proof.AudioTransform = "native_remux"
			if track.name == "pcma" || track.name == "pcmu" {
				proof.AudioTransform = "g711_to_pcm"
			}
		}
	}
	if err := init.Marshal(writer); err != nil {
		return nil, err
	}
	var tracks []*investigationRTSPTrack
	clock := investigationRTSPClock{}
	pending := map[int]investigationRTSPAU{}
	sequence := uint32(0)
	minPTS := int64(math.MaxInt64)
	previewOrigin := int64(math.MaxInt64)
	maxEnd := int64(0)
	emit := func(track *investigationRTSPTrack, au investigationRTSPAU) error {
		if au.dts < 0 || au.dts > maxPlayerElapsedMS*track.rate/1000+2*track.rate {
			return ErrInvalid
		}
		if sequence == math.MaxUint32 {
			return ErrInvalid
		}
		sequence++
		if err := (fmp4.Part{SequenceNumber: sequence, Tracks: []*fmp4.PartTrack{{ID: track.id, BaseTime: uint64(au.dts), Samples: []*fmp4.Sample{au.sample}}}}).Marshal(writer); err != nil {
			return err
		}
		start := au.pts * 1000 / track.rate
		minPTS = min(minPTS, start)
		maxEnd = max(maxEnd, (au.pts+int64(au.sample.Duration))*1000/track.rate)
		if au.video {
			proof.VideoAccessUnits++
			if !au.sample.IsNonSyncSample {
				event := au.event
				event.Kind = "video"
				event.Codec = track.name
				event.Track = uint8(track.id)
				event.Keyframe = true
				event.DTS = au.dts * 1000 / track.rate
				event.PTS = start
				proof.Keyframes = investigationRTSPKeyframes(proof.Keyframes, event, proof.AnchorCurrentMS+previewOrigin)
			}
		} else {
			proof.AudioAccessUnits++
		}
		return nil
	}
	processing := &rtspWireExport{describe: func(desc *description.Session) error {
		var err error
		tracks, err = investigationRTSPSelect(desc, report.RTSP.Preview)
		if err != nil {
			return err
		}
		if len(tracks) != len(configured) {
			return ErrInvalid
		}
		for index, track := range tracks {
			original := configured[index]
			if track.id != original.id || track.name != original.name {
				return ErrInvalid
			}
			track.sps = original.sps
			track.pps = original.pps
			track.vps = original.vps
			track.output = original.output
			track.reorder.onGap = func() error {
				if track.pending {
					proof.IncompleteAccessUnits++
				}
				if previous, exists := pending[track.id]; exists {
					proof.DroppedPackets += previous.packets
					delete(pending, track.id)
				}
				return track.resetLoss()
			}
		}
		return nil
	}}
	processing.packet = func(media int, data []byte, event Event) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var packet rtp.Packet
		if packet.Unmarshal(data) != nil {
			return ErrInvalid
		}
		track := investigationRTSPFind(tracks, media, packet.PayloadType)
		if track == nil {
			return nil
		}
		proof.PacketCount++
		if proof.PacketCount > maxTags {
			return fmt.Errorf("RTP 媒体重建达到包数上限。")
		}
		return track.reorder.push(&packet, event, func(packet *rtp.Packet, event Event) error {
			pts, ok, err := clock.decode(track.id, track.clockCodec, packet, event.ArrivalMS)
			if err != nil {
				return err
			}
			if !ok {
				proof.DroppedPackets++
				return nil
			}
			return track.decode(packet, pts, event, false, func(au investigationRTSPAU) error {
				if !track.video() {
					first := au.dts * 1000 / track.rate
					if proof.AudioAccessUnits == 0 {
						if p := report.RTSP.Preview.G711; p != nil && p.EncoderPrimingSamples != nil && p.FirstInputPTS != nil && proof.AudioTransform == "g711_to_pcm" {
							first = ((*p.FirstInputPTS*48000-*p.EncoderPrimingSamples*track.rate)*1000 + 2000*track.rate*48000) / (track.rate * 48000)
						}
						previewOrigin = min(previewOrigin, first)
					}
					return emit(track, au)
				}
				if previous, exists := pending[track.id]; exists {
					duration := au.dts - previous.dts
					if duration <= 0 || duration > maxPlayerElapsedMS*90 || duration > math.MaxUint32 {
						return ErrInvalid
					}
					previous.sample.Duration = uint32(duration)
					if proof.VideoAccessUnits == 0 {
						previewOrigin = min(previewOrigin, previous.dts*1000/track.rate)
					}
					if err := emit(track, previous); err != nil {
						return err
					}
				}
				pending[track.id] = au
				return nil
			})
		})
	}
	if _, err := readRTSPWire(dir, report.Manifest, nil, processing); err != nil {
		return nil, err
	}
	for _, track := range tracks {
		if err := track.reorder.finish(); err != nil {
			return nil, err
		}
		proof.DroppedPackets += track.reorder.duplicates + track.dropped
		if track.pending {
			proof.IncompleteAccessUnits++
			proof.DroppedPackets += track.accessPackets
		}
	}
	// No FPS guess: the final video's decode duration has no following DTS.
	for _, au := range pending {
		proof.DroppedPackets += au.packets
	}
	if proof.VideoAccessUnits+proof.AudioAccessUnits == 0 || minPTS == math.MaxInt64 || previewOrigin == math.MaxInt64 || maxEnd <= minPTS {
		return nil, fmt.Errorf("原始 RTP 中没有可完整定位的媒体访问单元。")
	}
	var firstVideo, firstAudio *investigationRTSPTrack
	for _, track := range tracks {
		if track.video() {
			firstVideo = track
		} else {
			firstAudio = track
		}
	}
	if firstVideo != nil && firstAudio != nil && firstVideo.first && firstAudio.first && firstAudio.firstDTS*1000/firstAudio.rate-firstVideo.firstDTS*1000/firstVideo.rate > 1000 {
		return nil, fmt.Errorf("首个音频相对视频明显延后，原播放器首批媒体归零点不明，未猜测截图位置。")
	}
	proof.MediaOriginMS = minPTS
	proof.PreviewOriginMS = previewOrigin
	proof.DurationMS = maxEnd - minPTS
	if proof.DurationMS > maxPlayerElapsedMS+2000 {
		return nil, ErrInvalid
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	sort.SliceStable(proof.Keyframes, func(i, j int) bool {
		if proof.Keyframes[i].PTS == proof.Keyframes[j].PTS {
			return proof.Keyframes[i].DTS < proof.Keyframes[j].DTS
		}
		return proof.Keyframes[i].PTS < proof.Keyframes[j].PTS
	})
	proof.RemuxArtifact, err = investigationRTSPArtifact(path)
	if err != nil {
		return nil, err
	}
	return proof, nil
}
func investigationRTSPFind(tracks []*investigationRTSPTrack, media int, pt uint8) *investigationRTSPTrack {
	for _, track := range tracks {
		if track.media == media && track.codec.PayloadType() == pt {
			return track
		}
	}
	return nil
}

type investigationRTSPFile struct{ *os.File }

func (w *investigationRTSPFile) Write(data []byte) (int, error) {
	offset, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if offset < 0 || int64(len(data)) > maxStreamBytes-offset {
		return 0, fmt.Errorf("离线媒体重建达到文件大小上限。")
	}
	return w.File.Write(data)
}
func investigationRTSPArtifact(path string) (Artifact, error) {
	file, err := os.Open(path)
	if err != nil {
		return Artifact{}, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, maxStreamBytes+1))
	if err != nil || size > maxStreamBytes {
		return Artifact{}, ErrInvalid
	}
	return Artifact{Bytes: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func investigationRTSPKeyframes(events []Event, event Event, target int64) []Event {
	events = append(events, event)
	var preceding *Event
	for index := range events {
		candidate := &events[index]
		if candidate.PTS <= target && (preceding == nil || candidate.PTS > preceding.PTS) {
			copy := *candidate
			preceding = &copy
		}
	}
	distance := func(value int64) int64 {
		if value < target {
			return target - value
		}
		return value - target
	}
	sort.SliceStable(events, func(i, j int) bool {
		a, b := distance(events[i].PTS), distance(events[j].PTS)
		if a == b {
			return events[i].PTS < events[j].PTS
		}
		return a < b
	})
	if len(events) > 16 {
		events = events[:16]
	}
	if preceding != nil {
		found := false
		for _, candidate := range events {
			if candidate.Offset == preceding.Offset && candidate.Artifact == preceding.Artifact {
				found = true
				break
			}
		}
		if !found {
			events[len(events)-1] = *preceding
		}
	}
	return events
}
