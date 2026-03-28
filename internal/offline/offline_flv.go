package offline

import (
	"bytes"
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
	outDir := filepath.Join(taskDir, "flv")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	videoPath := filepath.Join(outDir, "video.annexb")
	audioPath := filepath.Join(outDir, "audio.adts.aac")

	in, err := os.Open(task.InputPath)
	if err != nil {
		return err
	}
	defer in.Close()

	decoder, err := flv.NewDecoder(in)
	if err != nil {
		return fmt.Errorf("invalid flv header: %w", err)
	}

	vf, err := os.Create(videoPath)
	if err != nil {
		return err
	}
	af, err := os.Create(audioPath)
	if err != nil {
		_ = vf.Close()
		return err
	}
	defer func() {
		_ = vf.Close()
		_ = af.Close()
	}()

	ext := &flvExtractor{
		videoOut:     vf,
		audioOut:     af,
		avccNALUSize: 4,
		hvcNALUSize:  4,
	}

	frameDetails := make([]models.OfflineFrameDetail, 0, 4096)
	var totalTags, videoTags, audioTags, scriptTags int
	var metadataJSON string

	for {
		var flvTag tag.FlvTag
		err = decoder.Decode(&flvTag)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		totalTags++
		switch d := flvTag.Data.(type) {
		case *tag.VideoData:
			videoTags++
			fd, _ := ext.handleVideo(int64(flvTag.Timestamp), d)
			if fd != nil {
				frameDetails = append(frameDetails, *fd)
			}
		case *tag.AudioData:
			audioTags++
			fd, _ := ext.handleAudio(int64(flvTag.Timestamp), d)
			if fd != nil {
				frameDetails = append(frameDetails, *fd)
			}
		case *tag.ScriptData:
			scriptTags++
			if metadataJSON == "" {
				if v, ok := normalizeFLVMetadata(d); ok {
					if b, e := json.Marshal(v); e == nil {
						metadataJSON = string(b)
					}
				}
			}
		}
		flvTag.Close()
	}

	flow := models.OfflineFlowResult{
		FlowID:       1,
		Direction:    "flv",
		DumpRawDir:   "single",
		RawPath:      task.InputPath,
		VideoPath:    videoPath,
		AudioPath:    audioPath,
		VideoCodec:   ext.videoCodec,
		FLVMetadata:  metadataJSON,
		FrameDetails: frameDetails,
		PayloadBytes: 0,
	}
	if flow.VideoCodec == "" {
		flow.VideoCodec = "unknown"
	}
	if len(ext.aacASC) >= 2 {
		flow.AudioCodec = "aac"
	}

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

func normalizeFLVMetadata(sd *tag.ScriptData) (any, bool) {
	if sd == nil || len(sd.Objects) == 0 {
		return nil, false
	}
	if md, ok := sd.Objects["onMetaData"]; ok {
		return md, true
	}
	return sd.Objects, true
}

type flvExtractor struct {
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
}

func (e *flvExtractor) handleVideo(ts int64, video *tag.VideoData) (*models.OfflineFrameDetail, error) {
	if e.videoOut == nil || video == nil {
		return nil, nil
	}
	body, err := io.ReadAll(video.Data)
	if err != nil {
		return nil, err
	}

	switch video.CodecID {
	case tag.CodecIDAVC:
		e.videoCodec = "h264"
		switch video.AVCPacketType {
		case tag.AVCPacketTypeSequenceHeader:
			rec, err := codec.ParseAVCDecoderConfigurationRecord(body)
			if err != nil {
				return nil, nil
			}
			e.avccNALUSize = int(rec.LengthSizeMinusOne) + 1
			e.avcSPSList = cloneNALs(rec.SequenceParameterSets)
			e.avcPPSList = cloneNALs(rec.PictureParameterSets)
			return nil, nil
		case tag.AVCPacketTypeNALU:
			annexb, hasIDR, err := avccToAnnexB(body, e.avccNALUSize)
			if err != nil {
				return nil, nil
			}
			if hasIDR && len(e.avcSPSList) > 0 && len(e.avcPPSList) > 0 {
				for _, sps := range e.avcSPSList {
					if _, err := e.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); err != nil {
						return nil, err
					}
					if _, err := e.videoOut.Write(sps); err != nil {
						return nil, err
					}
				}
				for _, pps := range e.avcPPSList {
					if _, err := e.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); err != nil {
						return nil, err
					}
					if _, err := e.videoOut.Write(pps); err != nil {
						return nil, err
					}
				}
			}
			if _, err := e.videoOut.Write(annexb); err != nil {
				return nil, err
			}
			return &models.OfflineFrameDetail{
				MediaType: "video",
				DTS:       ts,
				PTS:       ts + int64(video.CompositionTime),
				FrameLen:  len(annexb),
				FrameType: flvFrameType(video.FrameType),
			}, nil
		}
	case tag.CodecIDHEVC:
		e.videoCodec = "h265"
		switch video.AVCPacketType {
		case tag.AVCPacketTypeSequenceHeader:
			rec, err := codec.ParseHEVCDecoderConfigurationRecord(body)
			if err != nil {
				return nil, nil
			}
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
			return nil, nil
		case tag.AVCPacketTypeNALU:
			annexb, err := hevcToAnnexB(body, e.hvcNALUSize)
			if err != nil {
				return nil, nil
			}
			if video.FrameType == tag.FrameTypeKeyFrame &&
				len(e.h265VPSList) > 0 && len(e.h265SPSList) > 0 && len(e.h265PPSList) > 0 {
				for _, vps := range e.h265VPSList {
					if _, err := e.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); err != nil {
						return nil, err
					}
					if _, err := e.videoOut.Write(vps); err != nil {
						return nil, err
					}
				}
				for _, sps := range e.h265SPSList {
					if _, err := e.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); err != nil {
						return nil, err
					}
					if _, err := e.videoOut.Write(sps); err != nil {
						return nil, err
					}
				}
				for _, pps := range e.h265PPSList {
					if _, err := e.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); err != nil {
						return nil, err
					}
					if _, err := e.videoOut.Write(pps); err != nil {
						return nil, err
					}
				}
			}
			if _, err := e.videoOut.Write(annexb); err != nil {
				return nil, err
			}
			return &models.OfflineFrameDetail{
				MediaType: "video",
				DTS:       ts,
				PTS:       ts + int64(video.CompositionTime),
				FrameLen:  len(annexb),
				FrameType: flvFrameType(video.FrameType),
			}, nil
		}
	}
	return nil, nil
}

func (e *flvExtractor) handleAudio(ts int64, audio *tag.AudioData) (*models.OfflineFrameDetail, error) {
	if e.audioOut == nil || audio == nil {
		return nil, nil
	}
	if audio.SoundFormat != tag.SoundFormatAAC {
		return nil, nil
	}
	body, err := io.ReadAll(audio.Data)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	switch audio.AACPacketType {
	case tag.AACPacketTypeSequenceHeader:
		e.aacASC = append(e.aacASC[:0], body...)
		return nil, nil
	case tag.AACPacketTypeRaw:
		if len(e.aacASC) < 2 {
			return nil, nil
		}
		adts, err := buildADTSHeader(e.aacASC, len(body))
		if err != nil {
			return nil, nil
		}
		if _, err := e.audioOut.Write(adts); err != nil {
			return nil, err
		}
		if _, err := e.audioOut.Write(body); err != nil {
			return nil, err
		}
		return &models.OfflineFrameDetail{
			MediaType: "audio",
			DTS:       ts,
			PTS:       ts,
			FrameLen:  len(adts) + len(body),
			FrameType: "-",
		}, nil
	default:
		return nil, nil
	}
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
