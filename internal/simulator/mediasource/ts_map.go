package mediasource

import (
	"encoding/binary"
	"errors"
)

// The bundled CBR fixture has one program, PMT PID 4096 and video/audio
// PIDs 256/257. Switch its PMT immediately before a video random access point.
func tsMapSwitch(raw []byte) (int, error) {
	lastPMT := -1
	for i := 0; i+tsPacketBytes <= len(raw); i += tsPacketBytes {
		packet := raw[i : i+tsPacketBytes]
		pid := uint16(packet[1]&31)<<8 | uint16(packet[2])
		if pid == 4096 {
			lastPMT = i
		}
		randomAccess := packet[1]&0x40 != 0 && packet[3]&0x20 != 0 && packet[4] > 0 && packet[5]&0x40 != 0
		if i >= tsRate*3/8 && pid == 256 && randomAccess && lastPMT >= 0 && i-lastPMT < 20*tsPacketBytes {
			return lastPMT, nil
		}
	}
	return 0, errors.New("TS 测试流缺少关键帧前的完整 PMT")
}

func newFixturePID(pid uint16) uint16 {
	switch pid {
	case 256, 257:
		return pid + 44
	default:
		return pid
	}
}

func switchFixturePMT(packet []byte) error {
	payload := 4
	if packet[3]&0x20 != 0 {
		payload += int(packet[4]) + 1
	}
	if payload >= len(packet) || packet[3]&0x10 == 0 || packet[1]&0x40 == 0 {
		return errors.New("TS 测试流 PMT 包头无效")
	}
	start := payload + 1 + int(packet[payload])
	if start+12 > len(packet) || packet[start] != 2 {
		return errors.New("TS 测试流 PMT 位置无效")
	}
	end := start + 3 + int(binary.BigEndian.Uint16(packet[start+1:])&0x0fff)
	if end > len(packet) || end < start+16 {
		return errors.New("TS 测试流 PMT 长度无效")
	}
	section := packet[start:end]
	version := (section[5] >> 1) & 31
	section[5] = section[5]&0xc1 | ((version+1)&31)<<1
	replacePID := func(b []byte) {
		value := binary.BigEndian.Uint16(b)
		binary.BigEndian.PutUint16(b, value&0xe000|newFixturePID(value&0x1fff))
	}
	replacePID(section[8:])
	i := 12 + int(binary.BigEndian.Uint16(section[10:])&0x0fff)
	for i < len(section)-4 {
		if i+5 > len(section)-4 {
			return errors.New("TS 测试流 PMT 流描述不完整")
		}
		replacePID(section[i+1:])
		i += 5 + int(binary.BigEndian.Uint16(section[i+3:])&0x0fff)
	}
	if i != len(section)-4 {
		return errors.New("TS 测试流 PMT 流描述长度无效")
	}
	crc := ^uint32(0)
	for _, value := range section[:len(section)-4] {
		crc ^= uint32(value) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	binary.BigEndian.PutUint32(section[len(section)-4:], crc)
	return nil
}

func switchFixturePacket(packet []byte, pid uint16) error {
	if pid == 4096 {
		return switchFixturePMT(packet)
	}
	if dst := newFixturePID(pid); dst != pid {
		packet[1] = packet[1]&0xe0 | byte(dst>>8)
		packet[2] = byte(dst)
	}
	return nil
}
