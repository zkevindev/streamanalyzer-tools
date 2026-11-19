package mediasource

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"
)

const (
	firstProgramPMT  = 4096
	secondProgramPMT = 4097
	replacementPMT   = 4100
)

func planTwoPrograms(raw []byte) ([]chunk, error) {
	switchPMT, err := tsMapSwitch(raw)
	if err != nil {
		return nil, err
	}
	switchPAT, resume := -1, -1
	for i := 0; i < len(raw); i += tsPacketBytes {
		p := raw[i : i+tsPacketBytes]
		pid := fixturePacketPID(p)
		if pid == 0 && i < switchPMT {
			switchPAT = i
		}
		if i >= switchPMT+tsRate/8 && pid == 256 && p[1]&0x40 != 0 && p[3]&0x20 != 0 && p[4] > 0 && p[5]&0x40 != 0 {
			resume = i
			break
		}
	}
	if switchPAT < 0 || resume < 0 {
		return nil, errors.New("双节目测试流缺少 PAT 或恢复关键帧")
	}
	null := bytes.Repeat([]byte{0xff}, tsPacketBytes)
	copy(null, []byte{0x47, 0x1f, 0xff, 0x10})
	out := make([]byte, 0, 2*len(raw))
	resumeAudio := false
	for i := 0; i < len(raw); i += tsPacketBytes {
		packet := raw[i : i+tsPacketBytes]
		pid := fixturePacketPID(packet)
		first := bytes.Clone(packet)
		second := null
		switch pid {
		case 0:
			first = fixturePAT(packet[3]&15, i >= switchPAT)
		case firstProgramPMT:
			second = bytes.Clone(packet)
			setFixturePID(second, secondProgramPMT)
			if err := rewriteFixturePMT(second, 2, false, secondProgramPID); err != nil {
				return nil, err
			}
			if i >= switchPMT {
				setFixturePID(first, replacementPMT)
				if err := switchFixturePMT(first); err != nil {
					return nil, err
				}
			}
		case 256, 257:
			second = bytes.Clone(packet)
			setFixturePID(second, secondProgramPID(pid))
			if i >= switchPMT {
				if pid == 257 && i >= resume && packet[1]&0x40 != 0 {
					resumeAudio = true
				}
				if i < resume || pid == 257 && !resumeAudio {
					first = null
				} else {
					setFixturePID(first, newFixturePID(pid))
				}
			}
		}
		out = append(out, first...)
		out = append(out, second...)
	}
	var plan []chunk
	faultAt := 2 * switchPMT
	for offset := 0; offset < len(out); offset += 7 * tsPacketBytes {
		end := min(offset+7*tsPacketBytes, len(out))
		plan = append(plan, chunk{at: time.Duration(offset*8) * time.Second / (2 * tsRate), data: out[offset:end], fault: offset <= faultAt && faultAt < end})
	}
	return plan, nil
}

func fixturePacketPID(packet []byte) uint16 { return uint16(packet[1]&31)<<8 | uint16(packet[2]) }

func secondProgramPID(pid uint16) uint16 {
	if pid == 256 || pid == 257 {
		return pid + 100
	}
	return pid
}

func fixturePAT(cc byte, changed bool) []byte {
	packet := bytes.Repeat([]byte{0xff}, tsPacketBytes)
	copy(packet, []byte{0x47, 0x40, 0, 0x10 | cc, 0})
	section := packet[5:25]
	copy(section, []byte{0, 0xb0, 17, 0, 1, 0xc1, 0, 0})
	first := uint16(firstProgramPMT)
	if changed {
		section[5] = 0xc3
		first = replacementPMT
	}
	binary.BigEndian.PutUint16(section[8:], 1)
	binary.BigEndian.PutUint16(section[10:], 0xe000|first)
	binary.BigEndian.PutUint16(section[12:], 2)
	binary.BigEndian.PutUint16(section[14:], 0xe000|secondProgramPMT)
	writeFixtureCRC(section)
	return packet
}
