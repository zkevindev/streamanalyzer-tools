package rtmpraw

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"streamanalyzer/internal/codec"

	"github.com/yutopp/go-flv/tag"
)

const (
	DefaultHandshakeSkipBytes = 1 + 1536 + 1536 // S0+S1+S2 length
)

type Options struct {
	// SkipBytes drops bytes at the beginning of the raw stream (usually RTMP handshake).
	SkipBytes int
}

type Output struct {
	VideoWriter io.Writer
	AudioWriter io.Writer
	OnFrame     func(FrameInfo)
}

type Result struct {
	VideoCodec string // "h264", "h265", or another recognized name
	AudioCodec string
	Video      *codec.VideoMeta
	Audio      *codec.AudioMeta
	HEVC       bool
	Warnings   []string
}

type FrameInfo struct {
	MediaType string // video/audio
	DTS       int64
	PTS       int64
	CTS       int64
	FrameLen  int
	FrameType string // I/P/B/- for audio
}

type chunkStreamState struct {
	timestamp       uint32
	timestampDelta  uint32
	messageLength   uint32
	messageTypeID   byte
	messageStreamID uint32
	lastFmt         byte

	buf      []byte
	readSize uint32
}

type mediaWriter struct {
	videoOut io.Writer
	audioOut io.Writer

	videoCodec string

	avccNALUSize int
	avcSPSList   [][]byte
	avcPPSList   [][]byte

	hvcNALUSize int
	h265VPSList [][]byte
	h265SPSList [][]byte
	h265PPSList [][]byte

	aacASC []byte

	audioCodec string
	videoMeta  *codec.VideoMeta
	audioMeta  *codec.AudioMeta
	hevc       bool
	warnings   []string

	onFrame func(FrameInfo)
}

// ParseRTMPRaw parses RTMP chunk stream payload from a single TCP direction raw capture.
// It extracts H264/HEVC AnnexB and AAC ADTS.
func ParseRTMPRaw(r io.Reader, opt Options, out Output) (Result, error) {
	if opt.SkipBytes < 0 {
		opt.SkipBytes = 0
	}

	if opt.SkipBytes > 0 {
		if _, err := io.CopyN(io.Discard, r, int64(opt.SkipBytes)); err != nil {
			return Result{}, fmt.Errorf("skip handshake bytes failed: %w", err)
		}
	}

	w := &mediaWriter{
		videoOut:     out.VideoWriter,
		audioOut:     out.AudioWriter,
		avccNALUSize: 4,
		hvcNALUSize:  4,
		onFrame:      out.OnFrame,
	}

	err := parseRTMPChunks(r, w)
	if err != nil &&
		!errors.Is(err, io.EOF) &&
		!errors.Is(err, io.ErrUnexpectedEOF) {
		return Result{}, err
	}

	return Result{
		VideoCodec: w.videoCodec,
		AudioCodec: w.audioCodec,
		Video:      w.videoMeta,
		Audio:      w.audioMeta,
		HEVC:       w.hevc,
		Warnings:   w.warnings,
	}, nil
}

func (w *mediaWriter) warn(msg string) {
	if msg == "" || len(w.warnings) >= 8 {
		return
	}
	for _, existing := range w.warnings {
		if existing == msg {
			return
		}
	}
	w.warnings = append(w.warnings, msg)
}

func parseRTMPChunks(r io.Reader, w *mediaWriter) error {
	const defaultChunkSize = 128
	inChunkSize := uint32(defaultChunkSize)
	states := map[int]*chunkStreamState{}

	for {
		fmtVal, csid, err := readChunkBasicHeader(r)
		if err != nil {
			return err
		}
		st, ok := states[csid]
		if !ok {
			st = &chunkStreamState{}
			states[csid] = st
		}

		if err := readAndApplyChunkMessageHeader(r, fmtVal, st); err != nil {
			return fmt.Errorf("chunk header parse failed (csid=%d fmt=%d): %w", csid, fmtVal, err)
		}

		if st.messageLength == 0 {
			return fmt.Errorf("invalid zero message length on csid=%d", csid)
		}
		if st.readSize == 0 && len(st.buf) == 0 {
			st.buf = make([]byte, st.messageLength)
		}

		remain := st.messageLength - st.readSize
		n := inChunkSize
		if remain < n {
			n = remain
		}
		if n == 0 {
			return fmt.Errorf("invalid chunk data size 0 on csid=%d", csid)
		}

		if _, err := io.ReadFull(r, st.buf[st.readSize:st.readSize+n]); err != nil {
			return err
		}
		st.readSize += n

		if st.readSize == st.messageLength {
			payload := st.buf
			if st.messageTypeID == 1 && len(payload) >= 4 {
				chunkSize := binary.BigEndian.Uint32(payload[:4]) & 0x7fffffff
				if chunkSize > 0 {
					inChunkSize = chunkSize
				}
			} else if st.messageTypeID == 8 || st.messageTypeID == 9 {
				if err := w.handleMessage(st.messageTypeID, st.timestamp, payload); err != nil {
					return err
				}
			}

			st.buf = nil
			st.readSize = 0
		}
	}
}

func readChunkBasicHeader(r io.Reader) (fmtVal byte, csid int, err error) {
	var b [1]byte
	if _, err = io.ReadFull(r, b[:]); err != nil {
		return 0, 0, err
	}
	fmtVal = (b[0] >> 6) & 0x03
	base := int(b[0] & 0x3f)
	switch base {
	case 0:
		var ext [1]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return 0, 0, err
		}
		csid = int(ext[0]) + 64
	case 1:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return 0, 0, err
		}
		csid = int(ext[1])*256 + int(ext[0]) + 64
	default:
		csid = base
	}
	return fmtVal, csid, nil
}

func readAndApplyChunkMessageHeader(r io.Reader, fmtVal byte, st *chunkStreamState) error {
	readU24 := func() (uint32, error) {
		var b [3]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2]), nil
	}
	readU32BE := func() (uint32, error) {
		var b [4]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, err
		}
		return binary.BigEndian.Uint32(b[:]), nil
	}

	switch fmtVal {
	case 0:
		ts, err := readU24()
		if err != nil {
			return err
		}
		ml, err := readU24()
		if err != nil {
			return err
		}
		var typeID [1]byte
		if _, err := io.ReadFull(r, typeID[:]); err != nil {
			return err
		}
		var msid [4]byte
		if _, err := io.ReadFull(r, msid[:]); err != nil {
			return err
		}
		st.messageLength = ml
		st.messageTypeID = typeID[0]
		st.messageStreamID = binary.LittleEndian.Uint32(msid[:])
		st.timestampDelta = 0
		if ts == 0xffffff {
			extTS, err := readU32BE()
			if err != nil {
				return err
			}
			st.timestamp = extTS
		} else {
			st.timestamp = ts
		}
		st.buf = nil
		st.readSize = 0

	case 1:
		// Best-effort compatibility: some raw captures start a new csid with fmt=1.
		// In that case the missing field is messageStreamID only; keep the zero value.
		td, err := readU24()
		if err != nil {
			return err
		}
		ml, err := readU24()
		if err != nil {
			return err
		}
		var typeID [1]byte
		if _, err := io.ReadFull(r, typeID[:]); err != nil {
			return err
		}
		st.messageLength = ml
		st.messageTypeID = typeID[0]
		if td == 0xffffff {
			extTD, err := readU32BE()
			if err != nil {
				return err
			}
			st.timestampDelta = extTD
		} else {
			st.timestampDelta = td
		}
		st.timestamp += st.timestampDelta
		st.buf = nil
		st.readSize = 0

	case 2:
		if st.messageLength == 0 {
			return fmt.Errorf("fmt=2 encountered without prior header")
		}
		td, err := readU24()
		if err != nil {
			return err
		}
		if td == 0xffffff {
			extTD, err := readU32BE()
			if err != nil {
				return err
			}
			st.timestampDelta = extTD
		} else {
			st.timestampDelta = td
		}
		st.timestamp += st.timestampDelta
		st.buf = nil
		st.readSize = 0

	case 3:
		if st.messageLength == 0 {
			return fmt.Errorf("fmt=3 encountered without prior header")
		}
		if st.readSize == 0 {
			if st.lastFmt != 0 && st.timestampDelta > 0 {
				st.timestamp += st.timestampDelta
			}
			st.buf = nil
		}
	default:
		return fmt.Errorf("invalid fmt=%d", fmtVal)
	}

	st.lastFmt = fmtVal
	return nil
}

func (w *mediaWriter) handleMessage(typeID byte, timestamp uint32, payload []byte) error {
	switch typeID {
	case 9:
		return w.handleVideo(timestamp, payload)
	case 8:
		return w.handleAudio(timestamp, payload)
	default:
		return nil
	}
}

func (w *mediaWriter) handleVideo(timestamp uint32, payload []byte) error {
	var video tag.VideoData
	if err := tag.DecodeVideoData(bytes.NewReader(payload), &video); err != nil {
		w.warn("视频消息无法解码")
		return nil
	}

	body, err := io.ReadAll(video.Data)
	if err != nil {
		return err
	}

	codecName, hevc, lengthPrefixed := classifyRTMPVideo(&video)
	if codecName != "" && w.videoCodec == "" {
		w.videoCodec = codecName
		w.hevc = hevc
	}
	if !lengthPrefixed {
		if codecName != "" {
			w.warn("视频编码 " + codecName + " 已识别，当前仅导出 H.264/H.265 裸流")
		}
		return nil
	}
	if videoSequence(&video) {
		return w.takeVideoSequence(hevc, body)
	}
	if !videoCoded(&video) {
		return nil
	}
	return w.writeCodedVideo(timestamp, &video, hevc, body)
}

func (w *mediaWriter) takeVideoSequence(hevc bool, body []byte) error {
	if hevc {
		rec, err := codec.ParseHEVCDecoderConfigurationRecord(body)
		if err != nil {
			w.warn("H.265 sequence header 无法解析")
			return nil
		}
		w.videoCodec = "h265"
		w.hevc = true
		w.hvcNALUSize = int(rec.LengthSizeMinusOne) + 1
		w.h265VPSList = nil
		w.h265SPSList = nil
		w.h265PPSList = nil
		for _, arr := range rec.Arrays {
			switch arr.NALUnitType {
			case 32:
				w.h265VPSList = append(w.h265VPSList, cloneNALs(arr.NALUnits)...)
			case 33:
				w.h265SPSList = append(w.h265SPSList, cloneNALs(arr.NALUnits)...)
			case 34:
				w.h265PPSList = append(w.h265PPSList, cloneNALs(arr.NALUnits)...)
			}
		}
		if meta, err := codec.ParseHEVCDecoderConfig(body); err == nil {
			w.videoMeta = meta
		}
		return nil
	}
	rec, err := codec.ParseAVCDecoderConfigurationRecord(body)
	if err != nil {
		w.warn("H.264 sequence header 无法解析")
		return nil
	}
	w.videoCodec = "h264"
	w.hevc = false
	w.avccNALUSize = int(rec.LengthSizeMinusOne) + 1
	w.avcSPSList = cloneNALs(rec.SequenceParameterSets)
	w.avcPPSList = cloneNALs(rec.PictureParameterSets)
	if meta, err := codec.ParseAVCDecoderConfig(body); err == nil {
		w.videoMeta = meta
	}
	return nil
}

func (w *mediaWriter) writeCodedVideo(timestamp uint32, video *tag.VideoData, hevc bool, body []byte) error {
	if w.videoOut == nil {
		return nil
	}
	var (
		annexb []byte
		err    error
		kind   string
	)
	if hevc {
		annexb, err = hevcToAnnexB(body, w.hvcNALUSize)
		if err != nil {
			w.warn("H.265 帧无法转换为 Annex B")
			return nil
		}
		if video.FrameType == tag.FrameTypeKeyFrame &&
			len(w.h265VPSList) > 0 && len(w.h265SPSList) > 0 && len(w.h265PPSList) > 0 {
			if err := writeAnnexBNALs(w.videoOut, w.h265VPSList, w.h265SPSList, w.h265PPSList); err != nil {
				return err
			}
		}
		kind = codec.FrameKindFromNALUs(body, w.hvcNALUSize, true)
	} else {
		var hasIDR bool
		annexb, hasIDR, err = avccToAnnexB(body, w.avccNALUSize)
		if err != nil {
			w.warn("H.264 帧无法转换为 Annex B")
			return nil
		}
		if hasIDR && len(w.avcSPSList) > 0 && len(w.avcPPSList) > 0 {
			if err := writeAnnexBNALs(w.videoOut, w.avcSPSList, w.avcPPSList); err != nil {
				return err
			}
		}
		kind = codec.FrameKindFromNALUs(body, w.avccNALUSize, false)
	}
	if _, err = w.videoOut.Write(annexb); err != nil {
		return err
	}
	if kind == "" {
		kind = frameTypeToString(video.FrameType)
	}
	if w.onFrame != nil {
		w.onFrame(FrameInfo{
			MediaType: "video",
			DTS:       int64(timestamp),
			PTS:       int64(timestamp) + int64(video.CompositionTime),
			CTS:       int64(video.CompositionTime),
			FrameLen:  len(annexb),
			FrameType: kind,
		})
	}
	return nil
}

func writeAnnexBNALs(w io.Writer, groups ...[][]byte) error {
	start := []byte{0x00, 0x00, 0x00, 0x01}
	for _, group := range groups {
		for _, nal := range group {
			if _, err := w.Write(start); err != nil {
				return err
			}
			if _, err := w.Write(nal); err != nil {
				return err
			}
		}
	}
	return nil
}

func classifyRTMPVideo(video *tag.VideoData) (name string, hevc bool, lengthPrefixed bool) {
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

func videoSequence(video *tag.VideoData) bool {
	if video.IsExHeader {
		return video.VideoPacketType == tag.VideoPacketTypeSequenceStart
	}
	return video.AVCPacketType == tag.AVCPacketTypeSequenceHeader
}

func videoCoded(video *tag.VideoData) bool {
	if video.IsExHeader {
		return video.VideoPacketType == tag.VideoPacketTypeCodedFrames || video.VideoPacketType == tag.VideoPacketTypeCodedFramesX
	}
	return video.AVCPacketType == tag.AVCPacketTypeNALU
}

func (w *mediaWriter) handleAudio(timestamp uint32, payload []byte) error {
	var audio tag.AudioData
	if err := tag.DecodeAudioData(bytes.NewReader(payload), &audio); err != nil {
		w.warn("音频消息无法解码")
		return nil
	}
	body, err := io.ReadAll(audio.Data)
	if err != nil {
		return err
	}
	name := classifyRTMPAudio(&audio)
	if name == "" {
		return nil
	}
	if w.audioCodec == "" {
		w.audioCodec = name
	} else if name != w.audioCodec {
		w.warn("音频编码从 " + w.audioCodec + " 变为 " + name + "，后续音频未导出")
		return nil
	}
	if name == "aac" {
		return w.writeAAC(timestamp, &audio, body)
	}
	if name == "mp3" {
		return w.writePassthroughAudio(timestamp, body)
	}
	w.warn("音频编码 " + name + " 已识别，当前仅导出 AAC ADTS 与 MP3")
	if w.audioMeta == nil {
		if rate, channels := legacySoundLayout(&audio); rate > 0 {
			w.audioMeta = &codec.AudioMeta{SampleRate: rate, Channels: channels}
		}
	}
	return nil
}

func (w *mediaWriter) writeAAC(timestamp uint32, audio *tag.AudioData, body []byte) error {
	if len(body) == 0 {
		return nil
	}
	if audioSequence(audio) {
		w.aacASC = append(w.aacASC[:0], body...)
		if meta, err := codec.ParseAACAudioSpecificConfig(body); err == nil {
			w.audioMeta = meta
		}
		return nil
	}
	if !audioCoded(audio) {
		return nil
	}
	if len(w.aacASC) < 2 || w.audioOut == nil {
		w.warn("AAC 帧缺少 sequence header，已跳过")
		return nil
	}
	adts, err := buildADTSHeader(w.aacASC, len(body))
	if err != nil {
		w.warn("AAC ADTS 头无法生成")
		return nil
	}
	if _, err := w.audioOut.Write(adts); err != nil {
		return err
	}
	if _, err := w.audioOut.Write(body); err != nil {
		return err
	}
	if w.onFrame != nil {
		w.onFrame(FrameInfo{
			MediaType: "audio",
			DTS:       int64(timestamp),
			PTS:       int64(timestamp),
			FrameLen:  len(adts) + len(body),
			FrameType: "-",
		})
	}
	return nil
}

func (w *mediaWriter) writePassthroughAudio(timestamp uint32, body []byte) error {
	if len(body) == 0 || w.audioOut == nil {
		return nil
	}
	if _, err := w.audioOut.Write(body); err != nil {
		return err
	}
	if w.onFrame != nil {
		w.onFrame(FrameInfo{
			MediaType: "audio",
			DTS:       int64(timestamp),
			PTS:       int64(timestamp),
			FrameLen:  len(body),
			FrameType: "-",
		})
	}
	return nil
}

func classifyRTMPAudio(audio *tag.AudioData) string {
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

func audioSequence(audio *tag.AudioData) bool {
	if audio.IsExHeader {
		return audio.AudioPacketType == tag.AudioPacketTypeSequenceStart
	}
	return audio.AACPacketType == tag.AACPacketTypeSequenceHeader
}

func audioCoded(audio *tag.AudioData) bool {
	if audio.IsExHeader {
		return audio.AudioPacketType == tag.AudioPacketTypeCodedFrames
	}
	return audio.AACPacketType == tag.AACPacketTypeRaw
}

func legacySoundLayout(audio *tag.AudioData) (rate int, channels int) {
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

func frameTypeToString(ft tag.FrameType) string {
	switch ft {
	case tag.FrameTypeKeyFrame, tag.FrameTypeGeneratedKeyFrame:
		return "I"
	case tag.FrameTypeInterFrame, tag.FrameTypeDisposableInterFrame:
		return "P"
	default:
		return "-"
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
