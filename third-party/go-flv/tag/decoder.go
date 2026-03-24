//
// Copyright (c) 2018- yutopp (yutopp@gmail.com)
//
// Distributed under the Boost Software License, Version 1.0. (See accompanying
// file LICENSE_1_0.txt or copy at  https://www.boost.org/LICENSE_1_0.txt)
//

package tag

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/yutopp/go-amf0"
)

func DecodeFlvTag(r io.Reader, flvTag *FlvTag) (err error) {
	ui32 := make([]byte, 4)
	buf := make([]byte, 11)
	if _, err := io.ReadAtLeast(r, buf, 1); err != nil {
		return err
	}

	tagType := TagType(buf[0])

	copy(ui32[1:], buf[1:4]) // 24bits
	dataSize := binary.BigEndian.Uint32(ui32)

	copy(ui32[1:], buf[4:7]) // lower 24bits
	ui32[0] = buf[7]         // upper  8bits
	timestamp := binary.BigEndian.Uint32(ui32)

	copy(ui32[1:], buf[8:11])
	ui32[0] = 0 // clear upper 8bits (not used)
	streamID := binary.BigEndian.Uint32(ui32)

	*flvTag = FlvTag{
		TagType:   tagType,
		Timestamp: timestamp,
		StreamID:  streamID,
	}

	lr := io.LimitReader(r, int64(dataSize))
	defer func() {
		if err != nil {
			_, _ = io.Copy(io.Discard, lr) // TODO: wrap an error?
		}
	}()

	switch tagType {
	case TagTypeAudio:
		var v AudioData
		if err := DecodeAudioData(lr, &v); err != nil {
			return fmt.Errorf("failed to decode audio data: %w", err)
		}
		flvTag.Data = &v

	case TagTypeVideo:
		var v VideoData
		if err := DecodeVideoData(lr, &v); err != nil {
			return fmt.Errorf("failed to decode video data: %w", err)
		}
		flvTag.Data = &v

	case TagTypeScriptData:
		var v ScriptData
		if err := DecodeScriptData(lr, &v); err != nil {
			return fmt.Errorf("failed to decode script data: %w", err)
		}
		flvTag.Data = &v

	default:
		return fmt.Errorf("unsupported tag type: %+v", tagType)
	}

	return nil
}

func DecodeAudioData(r io.Reader, audioData *AudioData) error {
	buf := make([]byte, 1)
	if _, err := io.ReadAtLeast(r, buf, 1); err != nil {
		return err
	}

	soundFormat := SoundFormat(buf[0] & 0xf0 >> 4) // 0b11110000
	soundRate := SoundRate(buf[0] & 0x0c >> 2)     // 0b00001100
	soundSize := SoundSize(buf[0] & 0x02 >> 1)     // 0b00000010
	soundType := SoundType(buf[0] & 0x01)          // 0b00000001

	*audioData = AudioData{
		SoundFormat: soundFormat,
		SoundRate:   soundRate,
		SoundSize:   soundSize,
		SoundType:   soundType,
	}

	if soundFormat == SoundFormatExHeader {
		if err := DecodeEnhancedAudioData(r, audioData, buf[0]); err != nil {
			return wrapEOF(err)
		}
	} else if soundFormat == SoundFormatAAC {
		var aacAudioData AACAudioData
		if err := DecodeAACAudioData(r, &aacAudioData); err != nil {
			return wrapEOF(err)
		}

		audioData.AACPacketType = aacAudioData.AACPacketType
		audioData.Data = aacAudioData.Data
	} else {
		audioData.Data = r
	}

	return nil
}

func DecodeEnhancedAudioData(r io.Reader, audioData *AudioData, headerByte byte) error {
	audioPacketType := AudioPacketType(headerByte & 0x0f)
	audioData.IsExHeader = true
	audioData.AudioPacketType = audioPacketType

	// Handle ModEx chain first. We currently skip extension payloads.
	for audioPacketType == AudioPacketTypeModEx {
		var sz [1]byte
		if _, err := io.ReadAtLeast(r, sz[:], 1); err != nil {
			return err
		}
		size := int(sz[0]) + 1
		if size == 256 {
			var ext [2]byte
			if _, err := io.ReadAtLeast(r, ext[:], 2); err != nil {
				return err
			}
			size = int(binary.BigEndian.Uint16(ext[:])) + 1
		}
		if _, err := io.CopyN(io.Discard, r, int64(size)); err != nil {
			return err
		}

		// consume [modExType:4bits | nextPacketType:4bits]
		var next [1]byte
		if _, err := io.ReadAtLeast(r, next[:], 1); err != nil {
			return err
		}
		audioPacketType = AudioPacketType(next[0] & 0x0f)
	}
	audioData.AudioPacketType = audioPacketType

	// Multitrack preamble (minimal parsing: track type + next packet type).
	if audioPacketType == AudioPacketTypeMultitrack {
		audioData.IsAudioMultitrack = true
		var mt [1]byte
		if _, err := io.ReadAtLeast(r, mt[:], 1); err != nil {
			return err
		}
		audioPacketType = AudioPacketType(mt[0] & 0x0f)
		audioData.AudioPacketType = audioPacketType
	}

	// Enhanced audio uses FourCC signaling.
	var fcc [4]byte
	if _, err := io.ReadAtLeast(r, fcc[:], 4); err != nil {
		return err
	}
	audioData.AudioFourCC = FourCC(binary.BigEndian.Uint32(fcc[:]))

	if audioData.IsAudioMultitrack {
		// Optional track id for many-track payloads.
		var trackID [1]byte
		if _, err := io.ReadAtLeast(r, trackID[:], 1); err != nil {
			return err
		}
		audioData.AudioTrackID = trackID[0]
	}

	audioData.Data = r
	return nil
}

func DecodeAACAudioData(r io.Reader, aacAudioData *AACAudioData) error {
	buf := make([]byte, 1)
	if _, err := io.ReadAtLeast(r, buf, 1); err != nil {
		return err
	}

	aacPacketType := AACPacketType(buf[0])

	*aacAudioData = AACAudioData{
		AACPacketType: aacPacketType,
		Data:          r,
	}

	return nil
}

func DecodeVideoData(r io.Reader, videoData *VideoData) error {
	buf := make([]byte, 1)
	if _, err := io.ReadAtLeast(r, buf, 1); err != nil {
		return err
	}

	isExVideoHeader := (buf[0] & 0x80) != 0
	if isExVideoHeader {
		return DecodeEnhancedVideoData(r, videoData, buf[0])
	}

	frameType := FrameType(buf[0] & 0xf0 >> 4) // 0b11110000
	codecID := CodecID(buf[0] & 0x0f)          // 0b00001111

	*videoData = VideoData{
		FrameType: frameType,
		CodecID:   codecID,
	}

	if codecID == CodecIDAVC || codecID == CodecIDHEVC {
		var avcVideoPacket AVCVideoPacket
		if err := DecodeAVCVideoPacket(r, &avcVideoPacket); err != nil {
			return wrapEOF(err)
		}
		videoData.AVCPacketType = avcVideoPacket.AVCPacketType
		videoData.CompositionTime = avcVideoPacket.CompositionTime
		videoData.Data = avcVideoPacket.Data
	} else {
		videoData.Data = r
	}

	return nil
}

func DecodeEnhancedVideoData(r io.Reader, videoData *VideoData, headerByte byte) error {
	frameType := FrameType((headerByte & 0x70) >> 4) // UB[3]
	videoPacketType := VideoPacketType(headerByte & 0x0f)
	videoData.IsExHeader = true
	videoData.FrameType = frameType
	videoData.VideoPacketType = videoPacketType

	// Handle ModEx chain first. We currently skip extension payloads.
	for videoPacketType == VideoPacketTypeModEx {
		var sz [1]byte
		if _, err := io.ReadAtLeast(r, sz[:], 1); err != nil {
			return err
		}
		size := int(sz[0]) + 1
		if size == 256 {
			var ext [2]byte
			if _, err := io.ReadAtLeast(r, ext[:], 2); err != nil {
				return err
			}
			size = int(binary.BigEndian.Uint16(ext[:])) + 1
		}
		if _, err := io.CopyN(io.Discard, r, int64(size)); err != nil {
			return err
		}

		// consume [modExType:4bits | nextPacketType:4bits]
		var next [1]byte
		if _, err := io.ReadAtLeast(r, next[:], 1); err != nil {
			return err
		}
		videoPacketType = VideoPacketType(next[0] & 0x0f)
	}
	videoData.VideoPacketType = videoPacketType

	// Command frame has no payload data body.
	if frameType == FrameTypeVideoInfoCommandFrame && videoPacketType != VideoPacketTypeMetadata {
		var cmd [1]byte
		if _, err := io.ReadAtLeast(r, cmd[:], 1); err != nil {
			return err
		}
		videoData.Data = r
		return nil
	}

	// Multitrack preamble (minimal parsing: track type + next packet type).
	if videoPacketType == VideoPacketTypeMultitrack {
		videoData.IsMultitrack = true
		var mt [1]byte
		if _, err := io.ReadAtLeast(r, mt[:], 1); err != nil {
			return err
		}
		videoPacketType = VideoPacketType(mt[0] & 0x0f)
		videoData.VideoPacketType = videoPacketType
	}

	// Enhanced video uses FourCC signaling.
	var fcc [4]byte
	if _, err := io.ReadAtLeast(r, fcc[:], 4); err != nil {
		return err
	}
	videoData.VideoFourCC = FourCC(binary.BigEndian.Uint32(fcc[:]))

	if videoData.IsMultitrack {
		// Optional track id for many-track payloads.
		var trackID [1]byte
		if _, err := io.ReadAtLeast(r, trackID[:], 1); err != nil {
			return err
		}
		videoData.VideoTrackID = trackID[0]
	}

	// For AVC/HEVC-style coded frames, composition offset is present.
	if videoPacketType == VideoPacketTypeCodedFrames {
		var ctBuf [3]byte
		if _, err := io.ReadAtLeast(r, ctBuf[:], 3); err != nil {
			return err
		}
		ctBin := []byte{ctBuf[0], ctBuf[1], ctBuf[2], 0}
		videoData.CompositionTime = int32(binary.BigEndian.Uint32(ctBin)) >> 8
	}

	videoData.Data = r
	return nil
}

func DecodeAVCVideoPacket(r io.Reader, avcVideoPacket *AVCVideoPacket) error {
	buf := make([]byte, 4)
	if _, err := io.ReadAtLeast(r, buf, 4); err != nil {
		return err
	}

	avcPacketType := AVCPacketType(buf[0])
	ctBin := make([]byte, 4)
	copy(ctBin[0:3], buf[1:4])
	compositionTime := int32(binary.BigEndian.Uint32(ctBin)) >> 8 // Signed Interger 24 bits. TODO: check

	*avcVideoPacket = AVCVideoPacket{
		AVCPacketType:   avcPacketType,
		CompositionTime: compositionTime,
		Data:            r,
	}

	return nil
}

func DecodeScriptData(r io.Reader, data *ScriptData) error {
	dec := amf0.NewDecoder(r)

	kv := make(map[string]amf0.ECMAArray)
	for {
		var key string
		if err := dec.Decode(&key); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to decode key: %w", err)
		}

		var value amf0.ECMAArray
		if err := dec.Decode(&value); err != nil {
			return fmt.Errorf("failed to decode value: %w", err)
		}

		kv[key] = value
	}

	data.Objects = kv

	return nil
}

func wrapEOF(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}
