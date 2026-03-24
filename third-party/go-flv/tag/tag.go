//
// Copyright (c) 2018- yutopp (yutopp@gmail.com)
//
// Distributed under the Boost Software License, Version 1.0. (See accompanying
// file LICENSE_1_0.txt or copy at  https://www.boost.org/LICENSE_1_0.txt)
//

package tag

import (
	"fmt"
	"io"

	"github.com/yutopp/go-amf0"
)

// ========================================
// FLV tags

type TagType uint8

const (
	TagTypeAudio      TagType = 8
	TagTypeVideo      TagType = 9
	TagTypeScriptData TagType = 18
)

type FlvTag struct {
	TagType
	Timestamp uint32
	StreamID  uint32      // 24bit
	Data      interface{} // *AudioData | *VideoData | *ScriptData
}

// Close
func (t *FlvTag) Close() {
	// TODO: wrap an error?
	switch data := t.Data.(type) {
	case *AudioData:
		data.Close()
	case *VideoData:
		data.Close()
	}
}

// ========================================
// Audio tags

type SoundFormat uint8

const (
	SoundFormatLinearPCMPlatformEndian SoundFormat = 0
	SoundFormatADPCM                   SoundFormat = 1
	SoundFormatMP3                     SoundFormat = 2
	SoundFormatLinearPCMLittleEndian   SoundFormat = 3
	SoundFormatNellymoser16kHzMono     SoundFormat = 4
	SoundFormatNellymoser8kHzMono      SoundFormat = 5
	SoundFormatNellymoser              SoundFormat = 6
	SoundFormatG711ALawLogarithmicPCM  SoundFormat = 7
	SoundFormatG711muLawLogarithmicPCM SoundFormat = 8
	// SoundFormatExHeader indicates enhanced audio header mode (E-RTMP/FLV).
	SoundFormatExHeader            SoundFormat = 9
	SoundFormatAAC                 SoundFormat = 10
	SoundFormatSpeex               SoundFormat = 11
	SoundFormatMP3_8kHz            SoundFormat = 14
	SoundFormatDeviceSpecificSound SoundFormat = 15
)

type SoundRate uint8

const (
	SoundRate5_5kHz SoundRate = 0
	SoundRate11kHz  SoundRate = 1
	SoundRate22kHz  SoundRate = 2
	SoundRate44kHz  SoundRate = 3
)

type SoundSize uint8

const (
	SoundSize8Bit  SoundSize = 0
	SoundSize16Bit SoundSize = 1
)

type SoundType uint8

const (
	SoundTypeMono   SoundType = 0
	SoundTypeStereo SoundType = 1
)

type AudioData struct {
	SoundFormat   SoundFormat
	SoundRate     SoundRate
	SoundSize     SoundSize
	SoundType     SoundType
	AACPacketType AACPacketType
	// Enhanced audio fields. Effective when SoundFormat == SoundFormatExHeader.
	IsExHeader        bool
	AudioPacketType   AudioPacketType
	AudioFourCC       FourCC
	AudioTrackID      uint8
	IsAudioMultitrack bool
	Data              io.Reader
}

func (d *AudioData) Read(buf []byte) (int, error) {
	return d.Data.Read(buf)
}

func (d *AudioData) Close() {
	_, _ = io.Copy(io.Discard, d.Data) //  // TODO: wrap an error?
}

type AACPacketType uint8

const (
	AACPacketTypeSequenceHeader AACPacketType = 0
	AACPacketTypeRaw            AACPacketType = 1
)

type AACAudioData struct {
	AACPacketType AACPacketType
	Data          io.Reader
}

type AudioPacketType uint8

const (
	AudioPacketTypeSequenceStart      AudioPacketType = 0
	AudioPacketTypeCodedFrames        AudioPacketType = 1
	AudioPacketTypeSequenceEnd        AudioPacketType = 2
	AudioPacketTypeMultichannelConfig AudioPacketType = 4
	AudioPacketTypeMultitrack         AudioPacketType = 5
	AudioPacketTypeModEx              AudioPacketType = 7
)

// ========================================
// Video Tags

type FrameType uint8

const (
	FrameTypeKeyFrame              FrameType = 1
	FrameTypeInterFrame            FrameType = 2
	FrameTypeDisposableInterFrame  FrameType = 3
	FrameTypeGeneratedKeyFrame     FrameType = 4
	FrameTypeVideoInfoCommandFrame FrameType = 5
)

type CodecID uint8

const (
	CodecIDJPEG                   CodecID = 1
	CodecIDSorensonH263           CodecID = 2
	CodecIDScreenVideo            CodecID = 3
	CodecIDOn2VP6                 CodecID = 4
	CodecIDOn2VP6WithAlphaChannel CodecID = 5
	CodecIDScreenVideoVersion2    CodecID = 6
	CodecIDAVC                    CodecID = 7
	CodecIDHEVC                   CodecID = 12
)

type VideoData struct {
	FrameType       FrameType
	CodecID         CodecID
	AVCPacketType   AVCPacketType
	CompositionTime int32
	// Enhanced video fields. Effective when IsExHeader is true.
	IsExHeader      bool
	VideoPacketType VideoPacketType
	VideoFourCC     FourCC
	VideoTrackID    uint8
	IsMultitrack    bool
	Data            io.Reader
}

func (d *VideoData) Read(buf []byte) (int, error) {
	return d.Data.Read(buf)
}

func (d *VideoData) Close() {
	_, _ = io.Copy(io.Discard, d.Data) //  // TODO: wrap an error?
}

type AVCPacketType uint8

const (
	AVCPacketTypeSequenceHeader AVCPacketType = 0
	AVCPacketTypeNALU           AVCPacketType = 1
	AVCPacketTypeEOS            AVCPacketType = 2
)

type AVCVideoPacket struct {
	AVCPacketType   AVCPacketType
	CompositionTime int32
	Data            io.Reader
}

type VideoPacketType uint8

const (
	VideoPacketTypeSequenceStart        VideoPacketType = 0
	VideoPacketTypeCodedFrames          VideoPacketType = 1
	VideoPacketTypeSequenceEnd          VideoPacketType = 2
	VideoPacketTypeCodedFramesX         VideoPacketType = 3
	VideoPacketTypeMetadata             VideoPacketType = 4
	VideoPacketTypeMPEG2TSSequenceStart VideoPacketType = 5
	VideoPacketTypeMultitrack           VideoPacketType = 6
	VideoPacketTypeModEx                VideoPacketType = 7
)

type FourCC uint32

const (
	// Audio FourCC (E-RTMP enhanced audio signaling)
	FourCCAudioAC3  FourCC = 0x61632d33 // "ac-3"
	FourCCAudioEAC3 FourCC = 0x65632d33 // "ec-3"
	FourCCAudioOpus FourCC = 0x4f707573 // "Opus"
	FourCCAudioMP3  FourCC = 0x2e6d7033 // ".mp3"
	FourCCAudioFLAC FourCC = 0x664c6143 // "fLaC"
	FourCCAudioAAC  FourCC = 0x6d703461 // "mp4a"

	// Video FourCC (E-RTMP enhanced video signaling)
	FourCCVideoVP8  FourCC = 0x76703038 // "vp08"
	FourCCVideoVP9  FourCC = 0x76703039 // "vp09"
	FourCCVideoAV1  FourCC = 0x61763031 // "av01"
	FourCCVideoAVC  FourCC = 0x61766331 // "avc1"
	FourCCVideoHEVC FourCC = 0x68766331 // "hvc1"
	FourCCVideoVVC  FourCC = 0x76766331 // "vvc1"
)

func MakeFourCC(s string) FourCC {
	if len(s) != 4 {
		return 0
	}
	return FourCC(uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3]))
}

func (f FourCC) Bytes() [4]byte {
	return [4]byte{
		byte(uint32(f) >> 24),
		byte(uint32(f) >> 16),
		byte(uint32(f) >> 8),
		byte(uint32(f)),
	}
}

func (f FourCC) String() string {
	b := f.Bytes()
	for _, c := range b {
		if c < 32 || c > 126 {
			return fmt.Sprintf("0x%08x", uint32(f))
		}
	}
	return string(b[:])
}

func (f FourCC) IsKnownEnhancedAudio() bool {
	switch f {
	case FourCCAudioAC3, FourCCAudioEAC3, FourCCAudioOpus, FourCCAudioMP3, FourCCAudioFLAC, FourCCAudioAAC:
		return true
	default:
		return false
	}
}

func (f FourCC) IsKnownEnhancedVideo() bool {
	switch f {
	case FourCCVideoVP8, FourCCVideoVP9, FourCCVideoAV1, FourCCVideoAVC, FourCCVideoHEVC, FourCCVideoVVC:
		return true
	default:
		return false
	}
}

// ========================================
// Data tags

type ScriptData struct {
	// all values are represented as subset of AMF0
	Objects map[string]amf0.ECMAArray
}
