package diagnostic

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/yutopp/go-flv"
	"github.com/yutopp/go-flv/tag"
)

var errFLV = errors.New("invalid FLV structure")

type flvReader struct {
	r                 io.Reader
	offset            int64
	data              []byte
	legacyEnhancedCTS bool
}

func openFLV(r io.Reader) (*flvReader, error) {
	var headerBytes [flv.HeaderLength]byte
	if _, err := io.ReadFull(r, headerBytes[:]); err != nil {
		return nil, err
	}
	header, err := flv.DecodeFlvHeader(bytes.NewReader(headerBytes[:]))
	if err != nil || header.Version != 1 || header.DataOffset < flv.HeaderLength || header.DataOffset > 1<<20 {
		return nil, errFLV
	}

	if _, err := io.CopyN(io.Discard, r, int64(header.DataOffset-flv.HeaderLength)); err != nil {
		return nil, incompleteTag(err)
	}
	var previousSize [4]byte
	if _, err := io.ReadFull(r, previousSize[:]); err != nil {
		return nil, incompleteTag(err)
	}
	if binary.BigEndian.Uint32(previousSize[:]) != 0 {
		return nil, errFLV
	}
	return &flvReader{r: r, offset: int64(header.DataOffset) + 4}, nil
}

func (f *flvReader) next() (Event, error) {
	var header [11]byte
	if _, err := io.ReadFull(f.r, header[:]); err != nil {
		return Event{}, err
	}
	size := int(readUint24(header[1:4]))
	if cap(f.data) < size+4 {
		f.data = make([]byte, size+4)
	}
	payloadAndSize := f.data[:size+4]
	if _, err := io.ReadFull(f.r, payloadAndSize); err != nil {
		return Event{}, incompleteTag(err)
	}

	dts := int64(readUint24(header[4:7]) | uint32(header[7])<<24)
	event := Event{Offset: f.offset, Size: len(header) + len(payloadAndSize), DTS: dts, PTS: dts}
	f.offset += int64(event.Size)
	if binary.BigEndian.Uint32(payloadAndSize[size:]) != uint32(len(header)+size) {
		event.Kind = "invalid_tag_size"
		return event, nil
	}
	if header[0]&0xe0 != 0 {
		event.Kind = "unsupported_tag"
		return event, nil
	}

	payload := bytes.NewReader(payloadAndSize[:size])
	switch tag.TagType(header[0]) {
	case tag.TagTypeAudio:
		decodeAudio(payload, &event)
	case tag.TagTypeVideo:
		decodeFLVVideo(payload, &event, f.legacyEnhancedCTS)
	case tag.TagTypeScriptData:
		event.Kind = "script"
	default:
		event.Kind = "unsupported_tag"
	}
	return event, nil
}

func incompleteTag(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

func readUint24(b []byte) uint32 {
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

func decodeAudio(payload *bytes.Reader, event *Event) {
	var audio tag.AudioData
	event.Kind = "invalid_audio"
	if err := tag.DecodeAudioData(payload, &audio); err != nil {
		return
	}
	event.Kind = "audio_aux"
	event.Track = audio.AudioTrackID
	switch {
	case audio.IsExHeader:
		event.Codec = printableFourCC(audio.AudioFourCC)
		if audio.AudioPacketType != tag.AudioPacketTypeCodedFrames || audio.IsAudioMultitrack {
			return
		}
	case audio.SoundFormat == tag.SoundFormatAAC:
		event.Codec = "aac"
		if audio.AACPacketType != tag.AACPacketTypeRaw {
			return
		}
	default:
		event.Codec = legacyFLVAudioCodec(audio.SoundFormat)
	}
	if payload.Len() > 0 {
		event.Kind = "audio"
	}
}

func decodeVideo(payload *bytes.Reader, event *Event) {
	decodeFLVVideo(payload, event, false)
}

func decodeFLVVideo(payload *bytes.Reader, event *Event, legacyCTS bool) {
	var video tag.VideoData
	event.Kind = "invalid_video"
	decode := tag.DecodeVideoData
	if legacyCTS {
		decode = tag.DecodeVideoDataLegacyCTS
	}
	if err := decode(payload, &video); err != nil {
		return
	}
	event.Kind = "video_aux"
	event.Track = video.VideoTrackID
	if video.FrameType == tag.FrameTypeVideoInfoCommandFrame {
		return
	}
	switch {
	case video.IsExHeader:
		event.Codec = printableFourCC(video.VideoFourCC)
		coded := video.VideoPacketType == tag.VideoPacketTypeCodedFrames || video.VideoPacketType == tag.VideoPacketTypeCodedFramesX
		if !coded || video.IsMultitrack {
			return
		}
	case video.CodecID == tag.CodecIDAVC || video.CodecID == tag.CodecIDHEVC:
		event.Codec = "h264"
		if video.CodecID == tag.CodecIDHEVC {
			event.Codec = "h265"
		}
		if video.AVCPacketType != tag.AVCPacketTypeNALU {
			return
		}
	default:
		event.Codec = legacyFLVVideoCodec(video.CodecID)
	}
	if payload.Len() > 0 {
		event.Kind = "video"
		event.PTS += int64(video.CompositionTime)
		event.Keyframe = video.FrameType == tag.FrameTypeKeyFrame || video.FrameType == tag.FrameTypeGeneratedKeyFrame
	}
}

func legacyFLVAudioCodec(format tag.SoundFormat) string {
	switch format {
	case tag.SoundFormatMP3, tag.SoundFormatMP3_8kHz:
		return "mp3"
	case tag.SoundFormatLinearPCMPlatformEndian, tag.SoundFormatLinearPCMLittleEndian:
		return "pcm"
	case tag.SoundFormatADPCM:
		return "adpcm"
	case tag.SoundFormatNellymoser, tag.SoundFormatNellymoser8kHzMono, tag.SoundFormatNellymoser16kHzMono:
		return "nellymoser"
	case tag.SoundFormatG711ALawLogarithmicPCM:
		return "g711-alaw"
	case tag.SoundFormatG711muLawLogarithmicPCM:
		return "g711-mulaw"
	case tag.SoundFormatSpeex:
		return "speex"
	default:
		return ""
	}
}

func legacyFLVVideoCodec(id tag.CodecID) string {
	switch id {
	case tag.CodecIDJPEG:
		return "mjpeg"
	case tag.CodecIDSorensonH263:
		return "sorenson-h263"
	case tag.CodecIDScreenVideo:
		return "screenvideo"
	case tag.CodecIDOn2VP6:
		return "vp6"
	case tag.CodecIDOn2VP6WithAlphaChannel:
		return "vp6-alpha"
	case tag.CodecIDScreenVideoVersion2:
		return "screenvideo2"
	default:
		return ""
	}
}

func printableFourCC(cc tag.FourCC) string {
	for _, b := range cc.Bytes() {
		if b < 32 || b > 126 {
			return ""
		}
	}
	return cc.String()
}
