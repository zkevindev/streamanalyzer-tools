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
	VideoCodec string // "h264", "h265", or ""
}

type FrameInfo struct {
	MediaType string // video/audio
	DTS       int64
	PTS       int64
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

	return Result{VideoCodec: w.videoCodec}, nil
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
		return nil
	}

	body, err := io.ReadAll(video.Data)
	if err != nil {
		return err
	}

	switch video.CodecID {
	case tag.CodecIDAVC:
		switch video.AVCPacketType {
		case tag.AVCPacketTypeSequenceHeader:
			rec, err := codec.ParseAVCDecoderConfigurationRecord(body)
			if err != nil {
				return nil
			}
			w.videoCodec = "h264"
			w.avccNALUSize = int(rec.LengthSizeMinusOne) + 1
			w.avcSPSList = cloneNALs(rec.SequenceParameterSets)
			w.avcPPSList = cloneNALs(rec.PictureParameterSets)
			return nil

		case tag.AVCPacketTypeNALU:
			annexb, hasIDR, err := avccToAnnexB(body, w.avccNALUSize)
			if err != nil {
				return nil
			}
			if hasIDR && len(w.avcSPSList) > 0 && len(w.avcPPSList) > 0 {
				for _, sps := range w.avcSPSList {
					if _, err := w.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); err != nil {
						return err
					}
					if _, err := w.videoOut.Write(sps); err != nil {
						return err
					}
				}
				for _, pps := range w.avcPPSList {
					if _, err := w.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); err != nil {
						return err
					}
					if _, err := w.videoOut.Write(pps); err != nil {
						return err
					}
				}
			}
			_, err = w.videoOut.Write(annexb)
			if err == nil && w.onFrame != nil {
				pts := int64(timestamp) + int64(video.CompositionTime)
				w.onFrame(FrameInfo{
					MediaType: "video",
					DTS:       int64(timestamp),
					PTS:       pts,
					FrameLen:  len(annexb),
					FrameType: frameTypeToString(video.FrameType),
				})
			}
			return err
		default:
			return nil
		}

	case tag.CodecIDHEVC:
		switch video.AVCPacketType {
		case tag.AVCPacketTypeSequenceHeader:
			rec, err := codec.ParseHEVCDecoderConfigurationRecord(body)
			if err != nil {
				return nil
			}
			w.videoCodec = "h265"
			w.hvcNALUSize = int(rec.LengthSizeMinusOne) + 1
			w.h265VPSList = nil
			w.h265SPSList = nil
			w.h265PPSList = nil
			for _, arr := range rec.Arrays {
				switch arr.NALUnitType {
				case 32: // VPS_NUT
					w.h265VPSList = append(w.h265VPSList, cloneNALs(arr.NALUnits)...)
				case 33: // SPS_NUT
					w.h265SPSList = append(w.h265SPSList, cloneNALs(arr.NALUnits)...)
				case 34: // PPS_NUT
					w.h265PPSList = append(w.h265PPSList, cloneNALs(arr.NALUnits)...)
				}
			}
			return nil

		case tag.AVCPacketTypeNALU:
			annexb, err := hevcToAnnexB(body, w.hvcNALUSize)
			if err != nil {
				return nil
			}
			// FLV keyframe corresponds to an I frame; inject VPS/SPS/PPS before it.
			if video.FrameType == tag.FrameTypeKeyFrame &&
				len(w.h265VPSList) > 0 && len(w.h265SPSList) > 0 && len(w.h265PPSList) > 0 {
				for _, vps := range w.h265VPSList {
					if _, err := w.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); err != nil {
						return err
					}
					if _, err := w.videoOut.Write(vps); err != nil {
						return err
					}
				}
				for _, sps := range w.h265SPSList {
					if _, err := w.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); err != nil {
						return err
					}
					if _, err := w.videoOut.Write(sps); err != nil {
						return err
					}
				}
				for _, pps := range w.h265PPSList {
					if _, err := w.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); err != nil {
						return err
					}
					if _, err := w.videoOut.Write(pps); err != nil {
						return err
					}
				}
			}
			_, err = w.videoOut.Write(annexb)
			if err == nil && w.onFrame != nil {
				pts := int64(timestamp) + int64(video.CompositionTime)
				w.onFrame(FrameInfo{
					MediaType: "video",
					DTS:       int64(timestamp),
					PTS:       pts,
					FrameLen:  len(annexb),
					FrameType: frameTypeToString(video.FrameType),
				})
			}
			return err
		default:
			return nil
		}

	default:
		return nil
	}
}

func (w *mediaWriter) handleAudio(timestamp uint32, payload []byte) error {
	var audio tag.AudioData
	if err := tag.DecodeAudioData(bytes.NewReader(payload), &audio); err != nil {
		return nil
	}
	if audio.SoundFormat != tag.SoundFormatAAC {
		return nil
	}

	body, err := io.ReadAll(audio.Data)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}

	switch audio.AACPacketType {
	case tag.AACPacketTypeSequenceHeader:
		w.aacASC = append(w.aacASC[:0], body...)
		return nil
	case tag.AACPacketTypeRaw:
		if len(w.aacASC) < 2 {
			return nil
		}
		adts, err := buildADTSHeader(w.aacASC, len(body))
		if err != nil {
			return nil
		}
		if _, err := w.audioOut.Write(adts); err != nil {
			return err
		}
		_, err = w.audioOut.Write(body)
		if err == nil && w.onFrame != nil {
			w.onFrame(FrameInfo{
				MediaType: "audio",
				DTS:       int64(timestamp),
				PTS:       int64(timestamp),
				FrameLen:  len(adts) + len(body),
				FrameType: "-",
			})
		}
		return err
	default:
		return nil
	}
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
