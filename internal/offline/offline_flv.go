package offline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"

	"github.com/yutopp/go-flv"
	"github.com/yutopp/go-flv/tag"
)

func (m *Manager) runFLV(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	ctx := m.taskContext(task.ID)
	outDir := filepath.Join(taskDir, "flv")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	in, err := os.Open(task.InputPath)
	if err != nil {
		return err
	}
	defer in.Close()

	var totalSize int64
	if st, err := in.Stat(); err == nil {
		totalSize = st.Size()
	}
	m.updateProgress(task.ID, "解析 FLV Tag", 0, totalSize)

	pr := newProgressReader(ctx, in, totalSize, func(read int64) {
		m.updateProgress(task.ID, "解析 FLV Tag", read, totalSize)
	})

	decoder, err := flv.NewDecoder(pr)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return context.Canceled
		}
		return fmt.Errorf("invalid flv header: %w", err)
	}

	ext := &flvExtractor{
		outDir:       outDir,
		avccNALUSize: 4,
		hvcNALUSize:  4,
	}
	defer ext.close()

	frameDetails := make([]models.OfflineFrameDetail, 0, 4096)
	samples := make([]timedSample, 0, 4096)
	var totalTags, videoTags, audioTags, scriptTags int
	var metadataJSON string
	var metadata models.OfflineMediaProfile

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var flvTag tag.FlvTag
		err = decoder.Decode(&flvTag)
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return context.Canceled
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			if totalTags == 0 {
				return err
			}
			ext.warn(fmt.Sprintf("FLV 在 %d 个 Tag 后中断: %s", totalTags, err.Error()))
			break
		}

		totalTags++
		switch d := flvTag.Data.(type) {
		case *tag.VideoData:
			videoTags++
			fd, handleErr := ext.handleVideo(int64(flvTag.Timestamp), d)
			if handleErr != nil {
				return handleErr
			}
			if fd != nil {
				idx := len(frameDetails)
				sig := ext.lastVideoSig
				frameDetails = append(frameDetails, *fd)
				samples = append(samples, timedSample{
					frameIndex:         idx,
					media:              fd.MediaType,
					dts:                fd.DTS,
					pts:                fd.PTS,
					cts:                fd.CTS,
					size:               fd.FrameLen,
					key:                isKeyframe(fd.FrameType),
					b:                  isBFrame(fd.FrameType),
					sliceKnown:         isExplicitSliceType(fd.FrameType),
					hasContainerParams: sig.hasContainerParams,
					hasInbandParams:    sig.hasInbandParams,
					paramHash:          sig.paramHash,
					videoWidth:         sig.width,
					videoHeight:        sig.height,
				})
			}
		case *tag.AudioData:
			audioTags++
			fd, handleErr := ext.handleAudio(int64(flvTag.Timestamp), d)
			if handleErr != nil {
				return handleErr
			}
			if fd != nil {
				idx := len(frameDetails)
				frameDetails = append(frameDetails, *fd)
				samples = append(samples, timedSample{
					frameIndex:      idx,
					media:           fd.MediaType,
					dts:             fd.DTS,
					pts:             fd.PTS,
					cts:             fd.CTS,
					size:            fd.FrameLen,
					audioSampleRate: ext.lastAudioSR,
					audioChannels:   ext.lastAudioCh,
				})
			}
		case *tag.ScriptData:
			scriptTags++
			if metadataJSON == "" {
				if v, ok := normalizeFLVMetadata(d); ok {
					if b, e := json.Marshal(v); e == nil {
						metadataJSON = string(b)
					}
					metadata = profileFromFLVMetadata(v)
				}
			}
		}
		flvTag.Close()
	}

	m.setProgressPercent(task.ID, "生成 FLV 媒体体检报告", 96, totalSize)

	flow := models.OfflineFlowResult{
		FlowID:      1,
		Direction:   "flv",
		DumpRawDir:  "single",
		RawPath:     task.InputPath,
		VideoPath:   ext.videoPath,
		AudioPath:   ext.audioPath,
		VideoCodec:  ext.videoCodec,
		AudioCodec:  ext.audioCodec,
		FLVMetadata: metadataJSON,
	}
	seed := profileFromVideoMeta(ext.videoMeta, ext.hevc)
	seed.VideoCodec = ext.videoCodec
	seed.AudioCodec = ext.audioCodec
	seed.Warnings = append([]string(nil), ext.warnings...)
	if len(ext.naluHist) > 0 {
		seed.NALUHistogram = ext.naluHist
	}
	if ext.audioMeta != nil {
		seed.AudioSampleRate = ext.audioMeta.SampleRate
		seed.AudioChannels = ext.audioMeta.Channels
	}
	fillProfile(&seed, metadata)
	hctx := healthContext{
		Mode:                  models.OfflineModeFLV,
		IsHEVC:                ext.hevc,
		HasContainerSeqHeader: ext.hasSeqHeader,
		CheckIDRParams:        ext.videoCodec == "h264" || ext.videoCodec == "h265",
	}
	attachMediaProfileWithSamples(&flow, frameDetails, samples, seed, hctx)

	cleanupEmptyFile(&flow.VideoPath)
	cleanupEmptyFile(&flow.AudioPath)
	if flow.VideoPath != "" {
		if st, e := os.Stat(flow.VideoPath); e == nil {
			flow.PayloadBytes += int(st.Size())
		}
	}
	if flow.AudioPath != "" {
		if st, e := os.Stat(flow.AudioPath); e == nil {
			flow.PayloadBytes += int(st.Size())
		}
	}

	if totalTags == 0 {
		flow.Error = "no flv tags parsed"
	} else if videoTags == 0 && audioTags == 0 {
		flow.Error = fmt.Sprintf("no audio/video tags parsed (script=%d)", scriptTags)
	}

	summary.Flows = append(summary.Flows, flow)
	return nil
}

func profileFromFLVMetadata(v any) models.OfflineMediaProfile {
	raw, err := json.Marshal(v)
	if err != nil {
		return models.OfflineMediaProfile{}
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return models.OfflineMediaProfile{}
	}
	var profile models.OfflineMediaProfile
	if n, ok := flvNumber(object["width"]); ok {
		profile.Width = int(n)
	}
	if n, ok := flvNumber(object["height"]); ok {
		profile.Height = int(n)
	}
	if n, ok := flvNumber(object["framerate"]); ok {
		profile.FrameRate = n
	}
	if profile.FrameRate == 0 {
		if n, ok := flvNumber(object["fps"]); ok {
			profile.FrameRate = n
		}
	}
	if n, ok := flvNumber(object["duration"]); ok && n > 0 {
		profile.DurationMS = int64(n * 1000)
	}
	if n, ok := flvNumber(object["audiosamplerate"]); ok {
		profile.AudioSampleRate = int(n)
	}
	if n, ok := flvNumber(object["audiochannels"]); ok {
		profile.AudioChannels = int(n)
	}
	if profile.AudioChannels == 0 {
		if n, ok := flvNumber(object["stereo"]); ok && n > 0 {
			profile.AudioChannels = 2
		}
	}
	return profile
}

func flvNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func normalizeFLVMetadata(sd *tag.ScriptData) (any, bool) {
	if sd == nil || len(sd.Objects) == 0 {
		return nil, false
	}
	if md, ok := sd.Objects["onMetaData"]; ok {
		return md, true
	}
	return sd.Objects, true
}

type videoFrameSignals struct {
	hasContainerParams bool
	hasInbandParams    bool
	paramHash          string
	width              int
	height             int
}

type flvExtractor struct {
	outDir    string
	videoOut  *os.File
	audioOut  *os.File
	videoPath string
	audioPath string
	videoPos  int64
	audioPos  int64

	videoCodec string
	audioCodec string
	hevc       bool
	videoMeta  *codec.VideoMeta
	audioMeta  *codec.AudioMeta
	warnings   []string
	naluHist   map[string]int

	hasSeqHeader bool
	paramHash    string
	lastVideoSig videoFrameSignals
	lastAudioSR  int
	lastAudioCh  int

	avccNALUSize int
	avcSPSList   [][]byte
	avcPPSList   [][]byte

	hvcNALUSize int
	h265VPSList [][]byte
	h265SPSList [][]byte
	h265PPSList [][]byte

	aacASC []byte
}

func (e *flvExtractor) close() {
	if e.videoOut != nil {
		_ = e.videoOut.Close()
	}
	if e.audioOut != nil {
		_ = e.audioOut.Close()
	}
}

func (e *flvExtractor) warn(msg string) {
	if msg == "" || len(e.warnings) >= 12 {
		return
	}
	for _, existing := range e.warnings {
		if existing == msg {
			return
		}
	}
	e.warnings = append(e.warnings, msg)
}

func (e *flvExtractor) ensureVideo() error {
	if e.videoOut != nil {
		return nil
	}
	path := filepath.Join(e.outDir, "video.annexb")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	e.videoOut = f
	e.videoPath = path
	return nil
}

func (e *flvExtractor) ensureAudio(codecName string) error {
	if e.audioOut != nil {
		return nil
	}
	name := "audio.adts.aac"
	if codecName == "mp3" {
		name = "audio.mp3"
	}
	path := filepath.Join(e.outDir, name)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	e.audioOut = f
	e.audioPath = path
	return nil
}

func (e *flvExtractor) handleVideo(ts int64, video *tag.VideoData) (*models.OfflineFrameDetail, error) {
	e.lastVideoSig = videoFrameSignals{}
	if video == nil {
		return nil, nil
	}
	body, err := io.ReadAll(video.Data)
	if err != nil {
		return nil, err
	}
	name, hevc, lengthPrefixed := classifyFLVVideo(video)
	if name != "" && e.videoCodec == "" {
		e.videoCodec = name
		e.hevc = hevc
	}
	if !lengthPrefixed {
		if name != "" {
			e.warn("视频编码 " + name + " 已识别，当前仅导出 H.264/H.265 裸流")
		}
		if len(body) == 0 {
			return nil, nil
		}
		return &models.OfflineFrameDetail{
			MediaType: "video",
			DTS:       ts,
			PTS:       ts + int64(video.CompositionTime),
			CTS:       int64(video.CompositionTime),
			FrameLen:  len(body),
			FrameType: flvFrameType(video.FrameType),
		}, nil
	}
	if flvVideoSequence(video) {
		e.takeVideoSequence(hevc, body)
		return nil, nil
	}
	if !flvVideoCoded(video) {
		return nil, nil
	}
	if err := e.ensureVideo(); err != nil {
		return nil, err
	}
	annexb, kind, startPos, written, sig, err := e.writeCodedVideo(video, hevc, body)
	if err != nil {
		return nil, err
	}
	if annexb == nil {
		return nil, nil
	}
	if kind == "" {
		kind = flvFrameType(video.FrameType)
	}
	e.lastVideoSig = sig
	return &models.OfflineFrameDetail{
		MediaType:    "video",
		DTS:          ts,
		PTS:          ts + int64(video.CompositionTime),
		CTS:          int64(video.CompositionTime),
		FrameLen:     len(annexb),
		FrameType:    kind,
		StreamSource: "video",
		StreamOffset: startPos,
		StreamLen:    written,
	}, nil
}

func (e *flvExtractor) takeVideoSequence(hevc bool, body []byte) {
	if hevc {
		rec, err := codec.ParseHEVCDecoderConfigurationRecord(body)
		if err != nil {
			e.warn("H.265 sequence header 无法解析")
			return
		}
		e.videoCodec = "h265"
		e.hevc = true
		e.hvcNALUSize = int(rec.LengthSizeMinusOne) + 1
		e.h265VPSList = nil
		e.h265SPSList = nil
		e.h265PPSList = nil
		for _, arr := range rec.Arrays {
			switch arr.NALUnitType {
			case 32:
				e.h265VPSList = append(e.h265VPSList, cloneNALs(arr.NALUnits)...)
			case 33:
				e.h265SPSList = append(e.h265SPSList, cloneNALs(arr.NALUnits)...)
			case 34:
				e.h265PPSList = append(e.h265PPSList, cloneNALs(arr.NALUnits)...)
			}
		}
		if len(e.h265VPSList) > 0 && len(e.h265SPSList) > 0 && len(e.h265PPSList) > 0 {
			e.hasSeqHeader = true
		}
		e.paramHash = hashNALSlices(e.h265VPSList, e.h265SPSList, e.h265PPSList)
		if meta, err := codec.ParseHEVCDecoderConfig(body); err == nil {
			e.videoMeta = meta
		}
		return
	}
	rec, err := codec.ParseAVCDecoderConfigurationRecord(body)
	if err != nil {
		e.warn("H.264 sequence header 无法解析")
		return
	}
	e.videoCodec = "h264"
	e.hevc = false
	e.avccNALUSize = int(rec.LengthSizeMinusOne) + 1
	e.avcSPSList = cloneNALs(rec.SequenceParameterSets)
	e.avcPPSList = cloneNALs(rec.PictureParameterSets)
	if len(e.avcSPSList) > 0 && len(e.avcPPSList) > 0 {
		e.hasSeqHeader = true
	}
	e.paramHash = hashNALSlices(e.avcSPSList, e.avcPPSList)
	if meta, err := codec.ParseAVCDecoderConfig(body); err == nil {
		e.videoMeta = meta
	}
}

func (e *flvExtractor) writeCodedVideo(video *tag.VideoData, hevc bool, body []byte) ([]byte, string, int64, int, videoFrameSignals, error) {
	if e.naluHist == nil {
		e.naluHist = make(map[string]int)
	}
	startPos := e.videoPos
	sig := videoFrameSignals{paramHash: e.paramHash}

	if hevc {
		annexb, err := hevcToAnnexB(body, e.hvcNALUSize)
		if err != nil {
			e.warn("H.265 帧无法转换为 Annex B")
			return nil, "", 0, 0, sig, nil
		}
		nalus := codec.SplitAVCC(body, e.hvcNALUSize, true)
		var hasV, hasS, hasP bool
		var inbandVPS, inbandSPS, inbandPPS [][]byte
		off := 0
		for _, n := range nalus {
			e.naluHist[n.Name]++
			off += e.hvcNALUSize
			nal := body[off : off+n.Len]
			off += n.Len
			switch n.Type {
			case 32:
				hasV = true
				inbandVPS = append(inbandVPS, nal)
			case 33:
				hasS = true
				inbandSPS = append(inbandSPS, nal)
				if meta := codec.VideoMetaFromNAL(nal); meta != nil {
					e.videoMeta = meta
				}
			case 34:
				hasP = true
				inbandPPS = append(inbandPPS, nal)
			}
		}
		if hasS || hasP || hasV {
			if len(inbandVPS) > 0 {
				e.h265VPSList = cloneNALs(inbandVPS)
			}
			if len(inbandSPS) > 0 {
				e.h265SPSList = cloneNALs(inbandSPS)
			}
			if len(inbandPPS) > 0 {
				e.h265PPSList = cloneNALs(inbandPPS)
			}
			e.paramHash = hashNALSlices(e.h265VPSList, e.h265SPSList, e.h265PPSList)
		}
		sig.hasInbandParams = hasV && hasS && hasP
		sig.hasContainerParams = e.hasSeqHeader
		sig.paramHash = e.paramHash
		if e.videoMeta != nil {
			sig.width = e.videoMeta.Width
			sig.height = e.videoMeta.Height
		}

		if video.FrameType == tag.FrameTypeKeyFrame && !sig.hasInbandParams &&
			len(e.h265VPSList) > 0 && len(e.h265SPSList) > 0 && len(e.h265PPSList) > 0 {
			n, err := writeStartCodedNALsCount(e.videoOut, e.h265VPSList, e.h265SPSList, e.h265PPSList)
			if err != nil {
				return nil, "", 0, 0, sig, err
			}
			e.videoPos += int64(n)
		}
		n, err := e.videoOut.Write(annexb)
		if err != nil {
			return nil, "", 0, 0, sig, err
		}
		e.videoPos += int64(n)
		return annexb, codec.FrameKindFromNALUs(body, e.hvcNALUSize, true), startPos, int(e.videoPos - startPos), sig, nil
	}

	annexb, hasIDR, err := avccToAnnexB(body, e.avccNALUSize)
	if err != nil {
		e.warn("H.264 帧无法转换为 Annex B")
		return nil, "", 0, 0, sig, nil
	}
	nalus := codec.SplitAVCC(body, e.avccNALUSize, false)
	var hasS, hasP bool
	var inbandSPS, inbandPPS [][]byte
	off := 0
	for _, n := range nalus {
		e.naluHist[n.Name]++
		off += e.avccNALUSize
		nal := body[off : off+n.Len]
		off += n.Len
		switch n.Type {
		case 7:
			hasS = true
			inbandSPS = append(inbandSPS, nal)
			if meta, err := codec.ParseH264SPS(nal); err == nil {
				e.videoMeta = meta
			}
		case 8:
			hasP = true
			inbandPPS = append(inbandPPS, nal)
		}
	}
	if hasS || hasP {
		if len(inbandSPS) > 0 {
			e.avcSPSList = cloneNALs(inbandSPS)
		}
		if len(inbandPPS) > 0 {
			e.avcPPSList = cloneNALs(inbandPPS)
		}
		e.paramHash = hashNALSlices(e.avcSPSList, e.avcPPSList)
	}
	sig.hasInbandParams = hasS && hasP
	sig.hasContainerParams = e.hasSeqHeader
	sig.paramHash = e.paramHash
	if e.videoMeta != nil {
		sig.width = e.videoMeta.Width
		sig.height = e.videoMeta.Height
	}

	if hasIDR && !sig.hasInbandParams && len(e.avcSPSList) > 0 && len(e.avcPPSList) > 0 {
		n, err := writeStartCodedNALsCount(e.videoOut, e.avcSPSList, e.avcPPSList)
		if err != nil {
			return nil, "", 0, 0, sig, err
		}
		e.videoPos += int64(n)
	}
	n, err := e.videoOut.Write(annexb)
	if err != nil {
		return nil, "", 0, 0, sig, err
	}
	e.videoPos += int64(n)
	return annexb, codec.FrameKindFromNALUs(body, e.avccNALUSize, false), startPos, int(e.videoPos - startPos), sig, nil
}

func writeStartCodedNALsCount(w io.Writer, groups ...[][]byte) (int, error) {
	start := []byte{0x00, 0x00, 0x00, 0x01}
	total := 0
	for _, group := range groups {
		for _, nal := range group {
			n, err := w.Write(start)
			total += n
			if err != nil {
				return total, err
			}
			n, err = w.Write(nal)
			total += n
			if err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

func classifyFLVVideo(video *tag.VideoData) (name string, hevc bool, lengthPrefixed bool) {
	if video.IsExHeader {
		switch video.VideoFourCC {
		case tag.FourCCVideoAVC:
			return "h264", false, true
		case tag.FourCCVideoHEVC:
			return "h265", true, true
		default:
			name = codec.VideoCodecName(video.VideoFourCC.String())
			if name == "" {
				name = video.VideoFourCC.String()
			}
			return name, false, false
		}
	}
	switch video.CodecID {
	case tag.CodecIDAVC:
		return "h264", false, true
	case tag.CodecIDHEVC:
		return "h265", true, true
	case tag.CodecIDJPEG:
		return "mjpeg", false, false
	case tag.CodecIDSorensonH263:
		return "h263", false, false
	case tag.CodecIDOn2VP6, tag.CodecIDOn2VP6WithAlphaChannel:
		return "vp6", false, false
	case tag.CodecIDScreenVideo, tag.CodecIDScreenVideoVersion2:
		return "screenvideo", false, false
	default:
		if video.CodecID == 0 {
			return "", false, false
		}
		return fmt.Sprintf("video:%d", video.CodecID), false, false
	}
}

func flvVideoSequence(video *tag.VideoData) bool {
	if video.IsExHeader {
		return video.VideoPacketType == tag.VideoPacketTypeSequenceStart
	}
	return video.AVCPacketType == tag.AVCPacketTypeSequenceHeader
}

func flvVideoCoded(video *tag.VideoData) bool {
	if video.IsExHeader {
		return video.VideoPacketType == tag.VideoPacketTypeCodedFrames || video.VideoPacketType == tag.VideoPacketTypeCodedFramesX
	}
	return video.AVCPacketType == tag.AVCPacketTypeNALU
}

func (e *flvExtractor) handleAudio(ts int64, audio *tag.AudioData) (*models.OfflineFrameDetail, error) {
	e.lastAudioSR, e.lastAudioCh = 0, 0
	if audio == nil {
		return nil, nil
	}
	body, err := io.ReadAll(audio.Data)
	if err != nil {
		return nil, err
	}
	name := classifyFLVAudio(audio)
	if name == "" {
		return nil, nil
	}
	if e.audioCodec == "" {
		e.audioCodec = name
	} else if name != e.audioCodec {
		e.warn("音频编码从 " + e.audioCodec + " 变为 " + name + "，后续音频未导出")
		return nil, nil
	}
	if name == "aac" {
		return e.writeAAC(ts, audio, body)
	}
	if name == "mp3" {
		if len(body) == 0 {
			return nil, nil
		}
		if err := e.ensureAudio(name); err != nil {
			return nil, err
		}
		startPos := e.audioPos
		n, err := e.audioOut.Write(body)
		if err != nil {
			return nil, err
		}
		e.audioPos += int64(n)
		if hdr, ok := codec.ParseAudioFrameHeader(body, "mp3"); ok {
			e.lastAudioSR, e.lastAudioCh = hdr.SampleRate, hdr.Channels
			if e.audioMeta == nil {
				e.audioMeta = &codec.AudioMeta{SampleRate: hdr.SampleRate, Channels: hdr.Channels}
			}
		} else if e.audioMeta == nil {
			if rate, channels := flvSoundLayout(audio); rate > 0 {
				e.audioMeta = &codec.AudioMeta{SampleRate: rate, Channels: channels}
				e.lastAudioSR, e.lastAudioCh = rate, channels
			}
		} else {
			e.lastAudioSR, e.lastAudioCh = e.audioMeta.SampleRate, e.audioMeta.Channels
		}
		return &models.OfflineFrameDetail{
			MediaType:    "audio",
			DTS:          ts,
			PTS:          ts,
			FrameLen:     len(body),
			FrameType:    "-",
			StreamSource: "audio",
			StreamOffset: startPos,
			StreamLen:    n,
		}, nil
	}
	e.warn("音频编码 " + name + " 已识别，当前仅导出 AAC ADTS 与 MP3")
	if len(body) == 0 {
		return nil, nil
	}
	e.lastAudioSR, e.lastAudioCh = flvSoundLayout(audio)
	return &models.OfflineFrameDetail{
		MediaType: "audio",
		DTS:       ts,
		PTS:       ts,
		FrameLen:  len(body),
		FrameType: "-",
	}, nil
}

func (e *flvExtractor) writeAAC(ts int64, audio *tag.AudioData, body []byte) (*models.OfflineFrameDetail, error) {
	if len(body) == 0 {
		return nil, nil
	}
	if flvAudioSequence(audio) {
		e.aacASC = append(e.aacASC[:0], body...)
		if meta, err := codec.ParseAACAudioSpecificConfig(body); err == nil {
			e.audioMeta = meta
		}
		return nil, nil
	}
	if !flvAudioCoded(audio) {
		return nil, nil
	}
	if len(e.aacASC) < 2 {
		e.warn("AAC 帧缺少 sequence header，已跳过")
		return nil, nil
	}
	adts, err := buildADTSHeader(e.aacASC, len(body))
	if err != nil {
		e.warn("AAC ADTS 头无法生成")
		return nil, nil
	}
	if err := e.ensureAudio("aac"); err != nil {
		return nil, err
	}
	startPos := e.audioPos
	n1, err := e.audioOut.Write(adts)
	if err != nil {
		return nil, err
	}
	e.audioPos += int64(n1)
	n2, err := e.audioOut.Write(body)
	if err != nil {
		return nil, err
	}
	e.audioPos += int64(n2)
	if e.audioMeta != nil {
		e.lastAudioSR, e.lastAudioCh = e.audioMeta.SampleRate, e.audioMeta.Channels
	}
	return &models.OfflineFrameDetail{
		MediaType:    "audio",
		DTS:          ts,
		PTS:          ts,
		FrameLen:     len(adts) + len(body),
		FrameType:    "-",
		StreamSource: "audio",
		StreamOffset: startPos,
		StreamLen:    n1 + n2,
	}, nil
}

func classifyFLVAudio(audio *tag.AudioData) string {
	if audio.IsExHeader {
		if name := codec.AudioCodecName(audio.AudioFourCC.String()); name != "" {
			if name == "mpeg4-audio" {
				return "aac"
			}
			return name
		}
		if audio.AudioFourCC != 0 {
			return audio.AudioFourCC.String()
		}
		return ""
	}
	switch audio.SoundFormat {
	case tag.SoundFormatAAC:
		return "aac"
	case tag.SoundFormatMP3, tag.SoundFormatMP3_8kHz:
		return "mp3"
	case tag.SoundFormatSpeex:
		return "speex"
	case tag.SoundFormatNellymoser, tag.SoundFormatNellymoser16kHzMono, tag.SoundFormatNellymoser8kHzMono:
		return "nellymoser"
	case tag.SoundFormatG711ALawLogarithmicPCM:
		return "g711-alaw"
	case tag.SoundFormatG711muLawLogarithmicPCM:
		return "g711-mulaw"
	case tag.SoundFormatLinearPCMPlatformEndian, tag.SoundFormatLinearPCMLittleEndian:
		return "pcm"
	case tag.SoundFormatADPCM:
		return "adpcm"
	default:
		return ""
	}
}

func flvAudioSequence(audio *tag.AudioData) bool {
	if audio.IsExHeader {
		return audio.AudioPacketType == tag.AudioPacketTypeSequenceStart
	}
	return audio.AACPacketType == tag.AACPacketTypeSequenceHeader
}

func flvAudioCoded(audio *tag.AudioData) bool {
	if audio.IsExHeader {
		return audio.AudioPacketType == tag.AudioPacketTypeCodedFrames
	}
	return audio.AACPacketType == tag.AACPacketTypeRaw
}

func flvSoundLayout(audio *tag.AudioData) (rate int, channels int) {
	switch audio.SoundRate {
	case tag.SoundRate5_5kHz:
		rate = 5512
	case tag.SoundRate11kHz:
		rate = 11025
	case tag.SoundRate22kHz:
		rate = 22050
	case tag.SoundRate44kHz:
		rate = 44100
	}
	channels = 1
	if audio.SoundType == tag.SoundTypeStereo {
		channels = 2
	}
	return rate, channels
}

func flvFrameType(ft tag.FrameType) string {
	switch ft {
	case tag.FrameTypeKeyFrame, tag.FrameTypeGeneratedKeyFrame:
		return "I"
	case tag.FrameTypeInterFrame, tag.FrameTypeDisposableInterFrame:
		return "P/B"
	default:
		return fmt.Sprintf("unknown:%d", ft)
	}
}

func cloneNALs(src [][]byte) [][]byte {
	dst := make([][]byte, 0, len(src))
	for _, n := range src {
		cp := make([]byte, len(n))
		copy(cp, n)
		dst = append(dst, cp)
	}
	return dst
}

func avccToAnnexB(payload []byte, naluLenSize int) ([]byte, bool, error) {
	if naluLenSize < 1 || naluLenSize > 4 {
		naluLenSize = 4
	}
	var out bytes.Buffer
	hasIDR := false
	off := 0
	for off+naluLenSize <= len(payload) {
		n := 0
		for i := 0; i < naluLenSize; i++ {
			n = (n << 8) | int(payload[off+i])
		}
		off += naluLenSize
		if n <= 0 || off+n > len(payload) {
			return nil, false, fmt.Errorf("invalid nalu size")
		}
		nal := payload[off : off+n]
		off += n
		if len(nal) > 0 && (nal[0]&0x1f) == 5 {
			hasIDR = true
		}
		out.Write([]byte{0x00, 0x00, 0x00, 0x01})
		out.Write(nal)
	}
	if off != len(payload) {
		return nil, false, fmt.Errorf("trailing bytes in avcc payload")
	}
	return out.Bytes(), hasIDR, nil
}

func hevcToAnnexB(payload []byte, naluLenSize int) ([]byte, error) {
	if naluLenSize < 1 || naluLenSize > 4 {
		naluLenSize = 4
	}
	var out bytes.Buffer
	off := 0
	for off+naluLenSize <= len(payload) {
		n := 0
		for i := 0; i < naluLenSize; i++ {
			n = (n << 8) | int(payload[off+i])
		}
		off += naluLenSize
		if n <= 0 || off+n > len(payload) {
			return nil, fmt.Errorf("invalid nalu size")
		}
		nal := payload[off : off+n]
		off += n
		out.Write([]byte{0x00, 0x00, 0x00, 0x01})
		out.Write(nal)
	}
	if off != len(payload) {
		return nil, fmt.Errorf("trailing bytes in hevc payload")
	}
	return out.Bytes(), nil
}

func buildADTSHeader(asc []byte, aacRawLen int) ([]byte, error) {
	if len(asc) < 2 {
		return nil, fmt.Errorf("asc too short")
	}
	audioObjectType := (asc[0] & 0xF8) >> 3
	freqIdx := ((asc[0] & 0x07) << 1) | ((asc[1] & 0x80) >> 7)
	channelCfg := (asc[1] & 0x78) >> 3
	if audioObjectType == 0 || freqIdx > 12 {
		return nil, fmt.Errorf("unsupported asc")
	}
	profile := audioObjectType - 1
	fullLen := aacRawLen + 7
	if fullLen > 0x1FFF {
		return nil, fmt.Errorf("aac frame too large")
	}
	h := make([]byte, 7)
	h[0] = 0xFF
	h[1] = 0xF1
	h[2] = (profile << 6) | (freqIdx << 2) | ((channelCfg >> 2) & 0x01)
	h[3] = ((channelCfg & 0x03) << 6) | byte((fullLen>>11)&0x03)
	h[4] = byte((fullLen >> 3) & 0xFF)
	h[5] = byte((fullLen&0x07)<<5) | 0x1F
	h[6] = 0xFC
	return h, nil
}
