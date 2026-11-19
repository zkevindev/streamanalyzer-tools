package mediasource

import (
	"encoding/binary"
	"errors"
	"time"
)

func planFLV(raw []byte, fault string) ([]chunk, error) {
	if len(raw) < 13 || string(raw[:3]) != "FLV" {
		return nil, errors.New("FLV 头无效")
	}
	offset := int(binary.BigEndian.Uint32(raw[5:9])) + 4
	if offset < 13 || offset > len(raw) {
		return nil, errors.New("FLV 标签边界无效")
	}
	plan := []chunk{{data: raw[:offset]}}
	var seenKey, injected bool
	var previousStamp uint32
	for offset < len(raw) {
		if len(raw)-offset < 15 {
			return nil, errors.New("FLV 标签不完整")
		}
		tag := raw[offset:]
		length := int(tag[1])<<16 | int(tag[2])<<8 | int(tag[3])
		if length+15 > len(tag) {
			return nil, errors.New("FLV 标签长度越界")
		}
		tag = tag[:length+15]
		stamp := uint32(tag[7])<<24 | uint32(tag[4])<<16 | uint32(tag[5])<<8 | uint32(tag[6])
		hit := false
		if tag[0] == 9 && length >= 5 && tag[12] == 1 {
			if fault == "dts_backwards" && !injected && stamp >= 2000 && previousStamp > 1000 {
				back := previousStamp - 1000
				tag[4], tag[5], tag[6], tag[7] = byte(back>>16), byte(back>>8), byte(back), byte(back>>24)
				hit = true
			}
			if tag[11]>>4 == 1 {
				hit = hit || fault == "long_gop" && seenKey && !injected
				seenKey = true
			}
			previousStamp = stamp
		}
		injected = injected || hit
		at := max(plan[len(plan)-1].at, time.Duration(stamp)*time.Millisecond)
		plan = append(plan, chunk{at: at, data: tag, fault: hit})
		offset += len(tag)
	}
	if len(plan) == 1 || fault != "normal" && !injected {
		return nil, errors.New("FLV 测试流缺少目标视频帧")
	}
	return plan, nil
}

const tsPacketBytes = 188
const tsRate = 260000

func planTS(raw []byte, fault string) ([]chunk, error) {
	if len(raw) < tsPacketBytes || len(raw)%tsPacketBytes != 0 {
		return nil, errors.New("TS 分包长度无效")
	}
	if fault == "program_outage" {
		return planTwoPrograms(raw)
	}
	switchOffset := len(raw)
	if fault == "pmt_pid_switch" {
		var err error
		switchOffset, err = tsMapSwitch(raw)
		if err != nil {
			return nil, err
		}
	}
	var plan []chunk
	batchStart := 0
	injected, hit := false, false
	flush := func(end int) {
		if end > batchStart {
			plan = append(plan, chunk{at: time.Duration(batchStart*8) * time.Second / tsRate, data: raw[batchStart:end], fault: hit})
			hit = false
		}
		batchStart = end
	}
	for offset := 0; offset < len(raw); offset += tsPacketBytes {
		p := raw[offset : offset+tsPacketBytes]
		if p[0] != 0x47 {
			return nil, errors.New("TS 同步字无效")
		}
		pid := uint16(p[1]&0x1f)<<8 | uint16(p[2])
		if offset >= switchOffset {
			if err := switchFixturePacket(p, pid); err != nil {
				return nil, err
			}
			if offset == switchOffset {
				injected, hit = true, true
			}
		}
		if !injected && offset >= tsRate/4 && pid == 256 {
			switch {
			case fault == "continuity" && p[1]&0x40 == 0 && p[3]&0x10 != 0:
				flush(offset)
				batchStart = offset + tsPacketBytes
				injected, hit = true, true
				continue
			case fault == "pcr_jump" && p[3]&0x20 != 0 && p[4] >= 7 && p[5]&0x10 != 0:
				jumpPCR(p)
				injected, hit = true, true
			}
		}
		if offset+tsPacketBytes-batchStart >= 7*tsPacketBytes {
			flush(offset + tsPacketBytes)
		}
	}
	flush(len(raw))
	if fault != "normal" && !injected {
		return nil, errors.New("TS 测试流缺少目标视频包")
	}
	return plan, nil
}

func jumpPCR(packet []byte) {
	base := uint64(packet[6])<<25 | uint64(packet[7])<<17 | uint64(packet[8])<<9 | uint64(packet[9])<<1 | uint64(packet[10]>>7)
	base = (base + 20*90000) & ((1 << 33) - 1)
	packet[6], packet[7], packet[8], packet[9] = byte(base>>25), byte(base>>17), byte(base>>9), byte(base>>1)
	packet[10] = (packet[10] & 0x7f) | byte(base&1)<<7
}
