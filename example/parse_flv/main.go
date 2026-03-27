package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"streamanalyzer/internal/codec"

	"github.com/yutopp/go-flv"
	"github.com/yutopp/go-flv/tag"
)

func main() {
	input := flag.String("i", "", "输入 FLV 文件路径")
	videoOutPath := flag.String("video", "", "输出视频原始码流路径（AnnexB，可选）")
	audioOutPath := flag.String("audio", "", "输出音频原始码流路径（ADTS AAC，可选）")
	flag.Parse()

	if *input == "" {
		fmt.Fprintln(os.Stderr, "请通过 -i 指定 FLV 文件路径")
		os.Exit(2)
	}

	f, err := os.Open(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开输入文件失败: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	var videoOut io.Writer
	var audioOut io.Writer
	var closeFns []func() error
	if *videoOutPath != "" {
		vf, createErr := os.Create(*videoOutPath)
		if createErr != nil {
			fmt.Fprintf(os.Stderr, "创建视频输出文件失败: %v\n", createErr)
			os.Exit(1)
		}
		videoOut = vf
		closeFns = append(closeFns, vf.Close)
	}
	if *audioOutPath != "" {
		af, createErr := os.Create(*audioOutPath)
		if createErr != nil {
			fmt.Fprintf(os.Stderr, "创建音频输出文件失败: %v\n", createErr)
			os.Exit(1)
		}
		audioOut = af
		closeFns = append(closeFns, af.Close)
	}
	defer func() {
		for _, fn := range closeFns {
			_ = fn()
		}
	}()

	decoder, err := flv.NewDecoder(f)
	if err != nil {
		// NewDecoder 内部会先解析 FLV Header；签名不匹配时会直接返回错误。
		fmt.Fprintf(os.Stderr, "文件异常：无效或缺失 FLV Header: %v\n", err)
		os.Exit(1)
	}

	h := decoder.Header()
	fmt.Printf("FLV Header: version=%d hasAudio=%v hasVideo=%v dataOffset=%d\n",
		h.Version, hasAudio(h.Flags), hasVideo(h.Flags), h.DataOffset)

	var (
		totalTags int
		videoTags int
		audioTags int
		metaTags  int
	)

	extractor := &mediaExtractor{
		videoOut:     videoOut,
		audioOut:     audioOut,
		avccNALUSize: 4,
		hvcNALUSize:  4,
	}

	for {
		var flvTag tag.FlvTag
		err := decoder.Decode(&flvTag)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "解析 Tag 失败(第%d个): %v\n", totalTags+1, err)
			os.Exit(1)
		}

		totalTags++
		switch d := flvTag.Data.(type) {
		case *tag.VideoData:
			videoTags++
			fmt.Printf("#%d VIDEO ts=%d frameType=%s codec=%s avcPacketType=%d cts=%d ex=%v fourcc=%s\n",
				totalTags,
				flvTag.Timestamp,
				videoFrameTypeName(d.FrameType),
				videoCodecDisplay(d),
				d.AVCPacketType,
				d.CompositionTime,
				d.IsExHeader,
				d.VideoFourCC.String())
			_ = extractor.handleVideo(d)
		case *tag.AudioData:
			audioTags++
			fmt.Printf("#%d AUDIO ts=%d format=%s aacPacketType=%d soundRate=%d channels=%s ex=%v fourcc=%s\n",
				totalTags,
				flvTag.Timestamp,
				audioFormatDisplay(d),
				d.AACPacketType,
				d.SoundRate,
				audioChannelTypeName(d.SoundType),
				d.IsExHeader,
				d.AudioFourCC.String())
			_ = extractor.handleAudio(d)
		case *tag.ScriptData:
			metaTags++
			fmt.Printf("#%d SCRIPT ts=%d keys=%d\n", totalTags, flvTag.Timestamp, len(d.Objects))
			printMetadata(d)
		default:
			fmt.Printf("#%d OTHER ts=%d tagType=%d\n", totalTags, flvTag.Timestamp, flvTag.TagType)
		}
		flvTag.Close()
	}

	fmt.Println("==== 解析完成 ====")
	fmt.Printf("total=%d video=%d audio=%d script=%d\n", totalTags, videoTags, audioTags, metaTags)
	if *videoOutPath != "" || *audioOutPath != "" {
		fmt.Printf("raw output: video=%s audio=%s\n", emptyOrDash(*videoOutPath), emptyOrDash(*audioOutPath))
	}
}

func printMetadata(sd *tag.ScriptData) {
	if sd == nil || len(sd.Objects) == 0 {
		fmt.Println("  metadata: (empty)")
		return
	}
	keys := make([]string, 0, len(sd.Objects))
	for k := range sd.Objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := sd.Objects[k]
		b, err := json.Marshal(v)
		if err != nil {
			fmt.Printf("  metadata.%s = <unmarshalable> (%T)\n", k, v)
			continue
		}
		fmt.Printf("  metadata.%s = %s\n", k, string(b))
	}
}

func hasAudio(flags flv.Flags) bool {
	return (flags & flv.FlagsAudio) != 0
}

func hasVideo(flags flv.Flags) bool {
	return (flags & flv.FlagsVideo) != 0
}

func audioChannelTypeName(t tag.SoundType) string {
	if t == tag.SoundTypeStereo {
		return "stereo"
	}
	return "mono"
}

func videoFrameTypeName(t tag.FrameType) string {
	switch t {
	case tag.FrameTypeKeyFrame:
		return "I(key)"
	case tag.FrameTypeInterFrame:
		// FLV 的 InterFrame 仅表示“非关键帧”，不能精确区分 P/B。
		return "inter(P/B)"
	default:
		return fmt.Sprintf("unknown:%d", t)
	}
}

func videoCodecName(c tag.CodecID) string {
	switch c {
	case tag.CodecIDJPEG:
		return "JPEG"
	case tag.CodecIDAVC:
		return "H264/AVC"
	case tag.CodecIDHEVC:
		return "H265/HEVC"
	case tag.CodecIDScreenVideo:
		return "ScreenVideo"
	case tag.CodecIDScreenVideoVersion2:
		return "ScreenVideo2"
	case tag.CodecIDOn2VP6WithAlphaChannel:
		return "VP6A"
	case tag.CodecIDOn2VP6:
		return "VP6"
	case tag.CodecIDSorensonH263:
		return "H263"
	default:
		return fmt.Sprintf("codec-%d", c)
	}
}

func videoCodecDisplay(d *tag.VideoData) string {
	if d == nil {
		return "unknown"
	}
	// Enhanced FLV/RTMP path: codec is carried by FourCC.
	if d.IsExHeader {
		return fmt.Sprintf("enhanced:%s", d.VideoFourCC.String())
	}
	return videoCodecName(d.CodecID)
}

func audioFormatName(f tag.SoundFormat) string {
	switch f {
	case tag.SoundFormatAAC:
		return "AAC"
	case tag.SoundFormatMP3:
		return "MP3"
	case tag.SoundFormatSpeex:
		return "Speex"
	case tag.SoundFormatG711ALawLogarithmicPCM:
		return "G711A"
	case tag.SoundFormatG711muLawLogarithmicPCM:
		return "G711U"
	case tag.SoundFormatExHeader:
		return "EnhancedAudio"
	default:
		return fmt.Sprintf("soundFormat-%d", f)
	}
}

func audioFormatDisplay(d *tag.AudioData) string {
	if d == nil {
		return "unknown"
	}
	if d.IsExHeader {
		return fmt.Sprintf("enhanced:%s", d.AudioFourCC.String())
	}
	return audioFormatName(d.SoundFormat)
}

func emptyOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

type mediaExtractor struct {
	videoOut io.Writer
	audioOut io.Writer

	avccNALUSize int
	avcSPSList   [][]byte
	avcPPSList   [][]byte

	hvcNALUSize int
	h265VPSList [][]byte
	h265SPSList [][]byte
	h265PPSList [][]byte

	aacASC []byte
}

func (m *mediaExtractor) handleVideo(video *tag.VideoData) error {
	if m.videoOut == nil || video == nil {
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
			rec, e := codec.ParseAVCDecoderConfigurationRecord(body)
			if e != nil {
				return nil
			}
			m.avccNALUSize = int(rec.LengthSizeMinusOne) + 1
			m.avcSPSList = cloneNALs(rec.SequenceParameterSets)
			m.avcPPSList = cloneNALs(rec.PictureParameterSets)
			return nil
		case tag.AVCPacketTypeNALU:
			annexb, hasIDR, e := avccToAnnexB(body, m.avccNALUSize)
			if e != nil {
				return nil
			}
			if hasIDR && len(m.avcSPSList) > 0 && len(m.avcPPSList) > 0 {
				for _, sps := range m.avcSPSList {
					if _, e = m.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); e != nil {
						return e
					}
					if _, e = m.videoOut.Write(sps); e != nil {
						return e
					}
				}
				for _, pps := range m.avcPPSList {
					if _, e = m.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); e != nil {
						return e
					}
					if _, e = m.videoOut.Write(pps); e != nil {
						return e
					}
				}
			}
			_, e = m.videoOut.Write(annexb)
			return e
		}
	case tag.CodecIDHEVC:
		switch video.AVCPacketType {
		case tag.AVCPacketTypeSequenceHeader:
			rec, e := codec.ParseHEVCDecoderConfigurationRecord(body)
			if e != nil {
				return nil
			}
			m.hvcNALUSize = int(rec.LengthSizeMinusOne) + 1
			m.h265VPSList = nil
			m.h265SPSList = nil
			m.h265PPSList = nil
			for _, arr := range rec.Arrays {
				switch arr.NALUnitType {
				case 32:
					m.h265VPSList = append(m.h265VPSList, cloneNALs(arr.NALUnits)...)
				case 33:
					m.h265SPSList = append(m.h265SPSList, cloneNALs(arr.NALUnits)...)
				case 34:
					m.h265PPSList = append(m.h265PPSList, cloneNALs(arr.NALUnits)...)
				}
			}
			return nil
		case tag.AVCPacketTypeNALU:
			annexb, e := hevcToAnnexB(body, m.hvcNALUSize)
			if e != nil {
				return nil
			}
			if video.FrameType == tag.FrameTypeKeyFrame &&
				len(m.h265VPSList) > 0 && len(m.h265SPSList) > 0 && len(m.h265PPSList) > 0 {
				for _, vps := range m.h265VPSList {
					if _, e = m.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); e != nil {
						return e
					}
					if _, e = m.videoOut.Write(vps); e != nil {
						return e
					}
				}
				for _, sps := range m.h265SPSList {
					if _, e = m.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); e != nil {
						return e
					}
					if _, e = m.videoOut.Write(sps); e != nil {
						return e
					}
				}
				for _, pps := range m.h265PPSList {
					if _, e = m.videoOut.Write([]byte{0x00, 0x00, 0x00, 0x01}); e != nil {
						return e
					}
					if _, e = m.videoOut.Write(pps); e != nil {
						return e
					}
				}
			}
			_, e = m.videoOut.Write(annexb)
			return e
		}
	}
	return nil
}

func (m *mediaExtractor) handleAudio(audio *tag.AudioData) error {
	if m.audioOut == nil || audio == nil {
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
		m.aacASC = append(m.aacASC[:0], body...)
		return nil
	case tag.AACPacketTypeRaw:
		if len(m.aacASC) < 2 {
			return nil
		}
		adts, e := buildADTSHeader(m.aacASC, len(body))
		if e != nil {
			return nil
		}
		if _, e = m.audioOut.Write(adts); e != nil {
			return e
		}
		_, e = m.audioOut.Write(body)
		return e
	default:
		return nil
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
