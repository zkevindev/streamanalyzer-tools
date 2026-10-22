// Package media 从 TS 和 fMP4 分片里判断开头是不是关键帧，并估算时长。
package media

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
)

// Info 是一个分片的容器层信息。
type Info struct {
	Format        string
	Codec         string
	Keyframe      *bool
	Duration      float64
	DurationKnown bool
	Timescale     uint32
	PTSStart      uint64
	PTSEnd        uint64
	PTSCount      int
}

// Inspect 检查媒体分片。init 是 fMP4 初始化段，可以为空。
func Inspect(segment, init []byte) Info {
	segment = skipID3(segment)
	init = skipID3(init)
	if isTS(segment) {
		return inspectTS(segment)
	}
	if isMP4(segment) || isMP4(init) {
		return inspectMP4(segment, init)
	}
	return Info{}
}

// DecryptAES128 按 HLS AES-128（CBC + PKCS#7）解密。
func DecryptAES128(ciphertext, key, iv []byte) ([]byte, error) {
	if len(key) != 16 || len(iv) != 16 {
		return nil, errors.New("AES-128 密钥或 IV 长度不对")
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("密文长度不是 16 的倍数")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ciphertext)
	pad := int(out[len(out)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(out) {
		return nil, errors.New("PKCS#7 填充无效")
	}
	for _, b := range out[len(out)-pad:] {
		if int(b) != pad {
			return nil, errors.New("PKCS#7 填充无效")
		}
	}
	return out[:len(out)-pad], nil
}

// IVForSequence 用媒体序号生成 HLS 默认 IV。
func IVForSequence(seq int) []byte {
	iv := make([]byte, 16)
	binary.BigEndian.PutUint64(iv[8:], uint64(seq))
	return iv
}

func skipID3(data []byte) []byte {
	if len(data) < 10 || string(data[0:3]) != "ID3" {
		return data
	}
	size := int(data[6]&0x7f)<<21 | int(data[7]&0x7f)<<14 | int(data[8]&0x7f)<<7 | int(data[9]&0x7f)
	total := 10 + size
	if data[5]&0x10 != 0 {
		total += 10
	}
	if total <= 0 || total > len(data) {
		return data
	}
	return data[total:]
}

func isTS(data []byte) bool {
	if len(data) >= 188*2 && data[0] == 0x47 && data[188] == 0x47 {
		return true
	}
	if len(data) > 196 && data[4] == 0x47 && data[196] == 0x47 {
		return true
	}
	return false
}

func isMP4(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	switch string(data[4:8]) {
	case "ftyp", "styp", "moof", "moov", "sidx", "emsg", "free":
		return true
	default:
		return false
	}
}

func inspectTS(data []byte) Info {
	info := Info{Format: "ts"}
	size, offset := 188, 0
	if data[0] != 0x47 && len(data) > 4 && data[4] == 0x47 {
		size, offset = 192, 4
	}
	pmtPID := -1
	videoPID := -1
	pcrPID := -1
	hevc := false
	var pes []byte
	var sawRAI bool
	var sawVideo bool
	var pesDone bool
	var pcrs []uint64

	for offset+188 <= len(data) {
		p := data[offset : offset+188]
		offset += size
		if p[0] != 0x47 || p[1]&0x80 != 0 {
			continue
		}
		pid := int(p[1]&0x1F)<<8 | int(p[2])
		pusi := p[1]&0x40 != 0
		payload, rai, pcr, hasPCR, ok := packetPayload(p)
		if !ok {
			continue
		}
		if pmtPID < 0 && pid == 0 && pusi {
			pmtPID = parsePAT(payload)
			continue
		}
		if videoPID < 0 && pmtPID >= 0 && pid == pmtPID && pusi {
			videoPID, pcrPID, hevc = parsePMT(payload)
			if hevc {
				info.Codec = "h265"
			} else if videoPID >= 0 {
				info.Codec = "h264"
			}
		}
		if pcrPID >= 0 && pid == pcrPID && hasPCR {
			pcrs = append(pcrs, pcr)
		}
		if videoPID >= 0 && pid == videoPID && pusi {
			if pts, ok := readPTS(payload); ok {
				if info.PTSCount == 0 {
					info.PTSStart = pts
				}
				info.PTSEnd = pts
				info.PTSCount++
			}
		}
		if videoPID < 0 || pid != videoPID || pesDone {
			continue
		}
		sawVideo = true
		if pusi && len(pes) > 0 {
			// 第一个 PES 足够判断关键帧。后面的包继续走，用来收集 PCR。
			pesDone = true
			continue
		}
		if pusi {
			if rai {
				sawRAI = true
			}
			payload = stripPES(payload)
		}
		pes = append(pes, payload...)
		if len(pes) > 256*1024 {
			pesDone = true
		}
	}

	if sawVideo {
		start, known := firstVCLIsIRAP(pes, hevc)
		if !known && sawRAI {
			start, known = true, true
		}
		if known {
			info.Keyframe = &start
		}
	}
	if info.PTSCount >= 2 && info.PTSEnd >= info.PTSStart && info.PTSEnd != info.PTSStart {
		info.Duration = float64(info.PTSEnd-info.PTSStart) / 90000
		info.DurationKnown = true
	} else if len(pcrs) >= 2 {
		info.Duration = float64(pcrDistance(pcrs[0], pcrs[len(pcrs)-1])) / 27000000
		info.DurationKnown = info.Duration > 0
	}
	return info
}

func readPTS(payload []byte) (uint64, bool) {
	if len(payload) < 14 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
		return 0, false
	}
	if payload[3] == 0xBE || payload[3] == 0xBF || payload[7]&0x80 == 0 {
		return 0, false
	}
	b := payload[9:14]
	pts := uint64(b[0]&0x0e)<<29 | uint64(b[1])<<22 | uint64(b[2]&0xfe)<<14 | uint64(b[3])<<7 | uint64(b[4])>>1
	return pts, true
}

func packetPayload(p []byte) (payload []byte, rai bool, pcr uint64, hasPCR bool, ok bool) {
	if len(p) < 4 || p[0] != 0x47 {
		return nil, false, 0, false, false
	}
	afc := (p[3] >> 4) & 0x3
	rest := p[4:]
	if afc == 0 {
		return nil, false, 0, false, false
	}
	if afc == 2 || afc == 3 {
		if len(rest) < 1 {
			return nil, false, 0, false, false
		}
		afl := int(rest[0])
		if afl > len(rest)-1 {
			return nil, false, 0, false, false
		}
		if afl > 0 {
			flags := rest[1]
			af := rest[1 : 1+afl]
			rai = flags&0x40 != 0
			if flags&0x10 != 0 && len(af) >= 7 {
				pcr = decodePCR(af[1:7])
				hasPCR = true
			}
		}
		if afc == 2 {
			return nil, rai, pcr, hasPCR, true
		}
		rest = rest[1+afl:]
	}
	return rest, rai, pcr, hasPCR, true
}

func parsePAT(payload []byte) int {
	if len(payload) < 1 {
		return -1
	}
	pointer := int(payload[0])
	if 1+pointer >= len(payload) {
		return -1
	}
	s := payload[1+pointer:]
	if len(s) < 12 || s[0] != 0x00 {
		return -1
	}
	sl := int(s[1]&0x0F)<<8 | int(s[2])
	end := 3 + sl
	if end > len(s) {
		end = len(s)
	}
	if end < 4 {
		return -1
	}
	end -= 4
	for i := 8; i+4 <= end; i += 4 {
		prog := int(s[i])<<8 | int(s[i+1])
		pid := int(s[i+2]&0x1F)<<8 | int(s[i+3])
		if prog != 0 {
			return pid
		}
	}
	return -1
}

func parsePMT(payload []byte) (videoPID, pcrPID int, hevc bool) {
	videoPID, pcrPID = -1, -1
	if len(payload) < 1 {
		return
	}
	pointer := int(payload[0])
	if 1+pointer >= len(payload) {
		return
	}
	s := payload[1+pointer:]
	if len(s) < 12 || s[0] != 0x02 {
		return
	}
	pcrPID = int(s[8]&0x1F)<<8 | int(s[9])
	pil := int(s[10]&0x0F)<<8 | int(s[11])
	sl := int(s[1]&0x0F)<<8 | int(s[2])
	end := 3 + sl
	if end > len(s) {
		end = len(s)
	}
	if end >= 4 {
		end -= 4
	}
	for i := 12 + pil; i+5 <= end; {
		streamType := s[i]
		epid := int(s[i+1]&0x1F)<<8 | int(s[i+2])
		esil := int(s[i+3]&0x0F)<<8 | int(s[i+4])
		if videoPID < 0 && isVideoStream(streamType) {
			videoPID = epid
			hevc = streamType == 0x24
		}
		i += 5 + esil
	}
	return
}

func isVideoStream(streamType byte) bool {
	switch streamType {
	case 0x02, 0x10, 0x1B, 0x1F, 0x20, 0x24:
		return true
	default:
		return false
	}
}

func stripPES(payload []byte) []byte {
	if len(payload) < 9 || payload[0] != 0 || payload[1] != 0 || payload[2] != 1 {
		return payload
	}
	headerLen := int(payload[8])
	start := 9 + headerLen
	if start > len(payload) {
		return nil
	}
	return payload[start:]
}

func firstVCLIsIRAP(es []byte, hevc bool) (bool, bool) {
	for i := 0; i+3 < len(es); i++ {
		if es[i] != 0 || es[i+1] != 0 {
			continue
		}
		off := -1
		if es[i+2] == 1 {
			off = i + 3
		} else if es[i+2] == 0 && i+3 < len(es) && es[i+3] == 1 {
			off = i + 4
		}
		if off < 0 || off >= len(es) {
			continue
		}
		nal := es[off]
		if hevc {
			nType := (nal >> 1) & 0x3F
			if nType <= 31 {
				return nType >= 16 && nType <= 21, true
			}
		} else {
			nType := nal & 0x1F
			if nType >= 1 && nType <= 5 {
				return nType == 5, true
			}
		}
		i = off
	}
	return false, false
}

func decodePCR(b []byte) uint64 {
	base := uint64(b[0])<<25 | uint64(b[1])<<17 | uint64(b[2])<<9 | uint64(b[3])<<1 | uint64(b[4])>>7
	ext := uint64(b[4]&0x01)<<8 | uint64(b[5])
	return base*300 + ext
}

func pcrDistance(a, b uint64) uint64 {
	const mod = (uint64(1) << 33) * 300
	if b < a {
		b += mod
	}
	return b - a
}

func inspectMP4(segment, init []byte) Info {
	info := Info{Format: "mp4"}
	var trackID uint32
	if len(init) > 0 {
		info.Timescale, info.Codec, trackID = inspectInit(init)
	}
	if info.Timescale == 0 || trackID == 0 {
		ts, codec, id := inspectInit(segment)
		if info.Timescale == 0 && ts != 0 {
			info.Timescale = ts
		}
		if info.Codec == "" {
			info.Codec = codec
		}
		if trackID == 0 {
			trackID = id
		}
	}
	start, known, ticks, haveTicks := inspectFragment(segment, trackID)
	if known {
		info.Keyframe = &start
	}
	if haveTicks && info.Timescale > 0 {
		info.Duration = float64(ticks) / float64(info.Timescale)
		info.DurationKnown = true
	}
	return info
}

func inspectInit(data []byte) (uint32, string, uint32) {
	var timescale uint32
	var codec string
	var trackID uint32
	walkBoxes(data, func(typ string, payload []byte) {
		if typ != "moov" {
			return
		}
		walkBoxes(payload, func(typ string, payload []byte) {
			if typ != "trak" {
				return
			}
			ts, video, c, id := inspectTrak(payload)
			if video && timescale == 0 {
				timescale = ts
				trackID = id
				if c != "" {
					codec = c
				}
			}
		})
	})
	return timescale, codec, trackID
}

func inspectTrak(data []byte) (timescale uint32, video bool, codec string, trackID uint32) {
	var walk func([]byte)
	walk = func(buf []byte) {
		walkBoxes(buf, func(typ string, payload []byte) {
			switch typ {
			case "mdia", "minf", "stbl":
				walk(payload)
			case "stsd":
				if len(payload) >= 8 {
					walk(payload[8:])
				}
			case "mdhd":
				if ts := mdhdTimescale(payload); ts != 0 {
					timescale = ts
				}
			case "hdlr":
				if hdlrIsVideo(payload) {
					video = true
				}
			case "tkhd":
				if id := tkhdTrackID(payload); id != 0 {
					trackID = id
				}
			case "avc1", "avc3", "hvc1", "hev1", "dvh1", "av01":
				video = true
				codec = typ
			}
		})
	}
	walk(data)
	return timescale, video, codec, trackID
}

func tkhdTrackID(payload []byte) uint32 {
	if len(payload) < 16 {
		return 0
	}
	if payload[0] == 1 {
		if len(payload) < 24 {
			return 0
		}
		return binary.BigEndian.Uint32(payload[20:24])
	}
	return binary.BigEndian.Uint32(payload[12:16])
}

func mdhdTimescale(payload []byte) uint32 {
	if len(payload) < 16 {
		return 0
	}
	if payload[0] == 1 {
		if len(payload) < 24 {
			return 0
		}
		return binary.BigEndian.Uint32(payload[20:24])
	}
	return binary.BigEndian.Uint32(payload[12:16])
}

func hdlrIsVideo(payload []byte) bool {
	if len(payload) < 12 {
		return false
	}
	return string(payload[8:12]) == "vide"
}

func inspectFragment(data []byte, videoTrackID uint32) (sync bool, known bool, ticks uint64, haveTicks bool) {
	var defaultFlags uint32
	var haveDefault bool
	var defaultDuration uint32
	var trackID uint32
	var firstFlags uint32
	var haveFirst bool
	var sawTrun bool

	var walk func([]byte)
	walk = func(buf []byte) {
		walkBoxes(buf, func(typ string, payload []byte) {
			switch typ {
			case "moof":
				walk(payload)
			case "traf":
				savedFlags, savedHave, savedDur, savedTrack := defaultFlags, haveDefault, defaultDuration, trackID
				defaultFlags, haveDefault, defaultDuration, trackID = 0, false, 0, 0
				walk(payload)
				defaultFlags, haveDefault, defaultDuration, trackID = savedFlags, savedHave, savedDur, savedTrack
			case "tfhd":
				trackID, defaultFlags, haveDefault, defaultDuration = parseTFHD(payload)
			case "trun":
				if videoTrackID != 0 && trackID != videoTrackID {
					return
				}
				if sawTrun {
					extra, okDur := sumTrun(payload, defaultDuration)
					if okDur {
						ticks += extra
						haveTicks = true
					}
					return
				}
				sawTrun = true
				flags, ok, dur, samplesOK := parseFirstTrun(payload, defaultFlags, haveDefault, defaultDuration)
				if ok {
					firstFlags = flags
					haveFirst = true
				}
				if samplesOK {
					ticks += dur
					haveTicks = true
				}
			}
		})
	}
	walk(data)
	if !haveFirst {
		return false, false, ticks, haveTicks
	}
	// sample_is_non_sync_sample 在第 16 位。该位为 0 表示同步样本。
	return firstFlags&0x00010000 == 0, true, ticks, haveTicks
}

func parseTFHD(payload []byte) (trackID uint32, flags uint32, haveFlags bool, defaultDuration uint32) {
	_, boxFlags, rest, ok := parseFullBox(payload)
	if !ok || len(rest) < 4 {
		return 0, 0, false, 0
	}
	trackID = binary.BigEndian.Uint32(rest[:4])
	rest = rest[4:]
	if boxFlags&0x000001 != 0 {
		if len(rest) < 8 {
			return trackID, 0, false, 0
		}
		rest = rest[8:]
	}
	if boxFlags&0x000002 != 0 {
		if len(rest) < 4 {
			return trackID, 0, false, 0
		}
		rest = rest[4:]
	}
	if boxFlags&0x000008 != 0 {
		if len(rest) < 4 {
			return trackID, 0, false, 0
		}
		defaultDuration = binary.BigEndian.Uint32(rest[:4])
		rest = rest[4:]
	}
	if boxFlags&0x000010 != 0 {
		if len(rest) < 4 {
			return trackID, 0, false, 0
		}
		rest = rest[4:]
	}
	if boxFlags&0x000020 != 0 && len(rest) >= 4 {
		return trackID, binary.BigEndian.Uint32(rest[:4]), true, defaultDuration
	}
	return trackID, 0, false, defaultDuration
}

func parseFirstTrun(payload []byte, defaultFlags uint32, haveDefault bool, defaultDuration uint32) (uint32, bool, uint64, bool) {
	_, boxFlags, rest, ok := parseFullBox(payload)
	if !ok || len(rest) < 4 {
		return 0, false, 0, false
	}
	count := binary.BigEndian.Uint32(rest[:4])
	rest = rest[4:]
	if count == 0 {
		return 0, false, 0, false
	}
	if boxFlags&0x000001 != 0 {
		if len(rest) < 4 {
			return 0, false, 0, false
		}
		rest = rest[4:]
	}
	sampleFlags := defaultFlags
	known := haveDefault
	var firstOverride *uint32
	if boxFlags&0x000004 != 0 {
		if len(rest) < 4 {
			return 0, false, 0, false
		}
		v := binary.BigEndian.Uint32(rest[:4])
		firstOverride = &v
		sampleFlags = v
		known = true
		rest = rest[4:]
	}
	var ticks uint64
	perDur := boxFlags&0x000100 != 0
	perSize := boxFlags&0x000200 != 0
	perFlags := boxFlags&0x000400 != 0
	perCTS := boxFlags&0x000800 != 0
	for i := uint32(0); i < count; i++ {
		if perDur {
			if len(rest) < 4 {
				break
			}
			ticks += uint64(binary.BigEndian.Uint32(rest[:4]))
			rest = rest[4:]
		} else if defaultDuration > 0 {
			ticks += uint64(defaultDuration)
		}
		if perSize {
			if len(rest) < 4 {
				break
			}
			rest = rest[4:]
		}
		if perFlags {
			if len(rest) < 4 {
				break
			}
			if i == 0 && firstOverride == nil {
				sampleFlags = binary.BigEndian.Uint32(rest[:4])
				known = true
			}
			rest = rest[4:]
		}
		if perCTS {
			if len(rest) < 4 {
				break
			}
			rest = rest[4:]
		}
	}
	return sampleFlags, known, ticks, ticks > 0
}

func sumTrun(payload []byte, defaultDuration uint32) (uint64, bool) {
	_, boxFlags, rest, ok := parseFullBox(payload)
	if !ok || len(rest) < 4 {
		return 0, false
	}
	count := binary.BigEndian.Uint32(rest[:4])
	rest = rest[4:]
	if boxFlags&0x000001 != 0 && len(rest) >= 4 {
		rest = rest[4:]
	}
	if boxFlags&0x000004 != 0 && len(rest) >= 4 {
		rest = rest[4:]
	}
	perDur := boxFlags&0x000100 != 0
	if !perDur {
		if defaultDuration == 0 || count == 0 {
			return 0, false
		}
		return uint64(defaultDuration) * uint64(count), true
	}
	var ticks uint64
	step := 0
	if perDur {
		step += 4
	}
	if boxFlags&0x000200 != 0 {
		step += 4
	}
	if boxFlags&0x000400 != 0 {
		step += 4
	}
	if boxFlags&0x000800 != 0 {
		step += 4
	}
	for i := uint32(0); i < count && len(rest) >= 4; i++ {
		ticks += uint64(binary.BigEndian.Uint32(rest[:4]))
		if step > len(rest) {
			break
		}
		rest = rest[step:]
	}
	return ticks, ticks > 0
}

func parseFullBox(payload []byte) (version byte, flags uint32, rest []byte, ok bool) {
	if len(payload) < 4 {
		return 0, 0, nil, false
	}
	flags = uint32(payload[1])<<16 | uint32(payload[2])<<8 | uint32(payload[3])
	return payload[0], flags, payload[4:], true
}

func walkBoxes(data []byte, fn func(typ string, payload []byte)) {
	for len(data) >= 8 {
		size32 := binary.BigEndian.Uint32(data[0:4])
		typ := string(data[4:8])
		header := 8
		var boxSize uint64
		switch size32 {
		case 0:
			boxSize = uint64(len(data))
		case 1:
			if len(data) < 16 {
				return
			}
			boxSize = binary.BigEndian.Uint64(data[8:16])
			header = 16
		default:
			boxSize = uint64(size32)
		}
		if boxSize < uint64(header) || boxSize > uint64(len(data)) {
			return
		}
		fn(typ, data[header:boxSize])
		data = data[boxSize:]
	}
}

// SyntheticMPEGTS 生成最小的 H.264 传输流，供测试和本地夹具使用。
func SyntheticMPEGTS(idr bool) []byte {
	es := []byte{0x00, 0x00, 0x00, 0x01, 0x09, 0xF0, 0x00, 0x00, 0x00, 0x01}
	if idr {
		es = append(es, 0x65, 0x88, 0x84, 0x00)
	} else {
		es = append(es, 0x41, 0x9A, 0x00, 0x00)
	}
	pes := append([]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00}, es...)
	return append(append(tsPacket(0x000, true, patPayload(), false, nil), tsPacket(0x100, true, pmtPayload(0x101), false, nil)...), tsPacket(0x101, true, pes, false, nil)...)
}

func patPayload() []byte {
	return []byte{
		0x00,
		0x00, 0xB0, 0x0D,
		0x00, 0x01,
		0xC1,
		0x00, 0x00,
		0x00, 0x01,
		0xE1, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
}

func pmtPayload(videoPID int) []byte {
	pidHi := byte(0xE0 | ((videoPID >> 8) & 0x1F))
	pidLo := byte(videoPID)
	return []byte{
		0x00,
		0x02,
		0xB0, 0x12,
		0x00, 0x01,
		0xC1,
		0x00, 0x00,
		pidHi, pidLo,
		0xF0, 0x00,
		0x1B,
		pidHi, pidLo,
		0xF0, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
}

func tsPacket(pid int, pusi bool, payload []byte, withRAI bool, pcr *uint64) []byte {
	p := make([]byte, 188)
	p[0] = 0x47
	if pusi {
		p[1] |= 0x40
	}
	p[1] |= byte((pid >> 8) & 0x1F)
	p[2] = byte(pid & 0xFF)
	extra := 0
	flags := byte(0)
	var pcrBytes []byte
	if withRAI {
		flags |= 0x40
	}
	if pcr != nil {
		flags |= 0x10
		pcrBytes = encodePCR(*pcr)
		extra = 6
	}
	needAF := withRAI || pcr != nil || len(payload) < 184
	if !needAF {
		p[3] = 0x10
		copy(p[4:], payload)
		return p
	}
	if len(payload) == 0 {
		p[3] = 0x20
	} else {
		p[3] = 0x30
	}
	adAfterLen := 183 - len(payload)
	if len(payload) == 0 {
		adAfterLen = 183
	}
	if adAfterLen < 1+extra {
		panic(fmt.Sprintf("TS 负载过长: %d", len(payload)))
	}
	if adAfterLen == 0 {
		p[4] = 0
		copy(p[5:], payload)
		return p
	}
	p[4] = byte(adAfterLen)
	p[5] = flags
	copy(p[6:], pcrBytes)
	if len(payload) > 0 {
		copy(p[188-len(payload):], payload)
	}
	return p
}

func encodePCR(pcr uint64) []byte {
	base := pcr / 300
	ext := pcr % 300
	b := make([]byte, 6)
	b[0] = byte(base >> 25)
	b[1] = byte(base >> 17)
	b[2] = byte(base >> 9)
	b[3] = byte(base >> 1)
	b[4] = byte((base&1)<<7) | byte((ext>>8)&0x01)
	b[5] = byte(ext)
	return b
}
