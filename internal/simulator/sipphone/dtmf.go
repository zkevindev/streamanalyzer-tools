package sipphone

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const dtmfDigits = "0123456789*#ABCD"
const offeredDTMF = 101

type DigitEvent struct {
	At        time.Time `json:"at"`
	Direction string    `json:"direction"`
	Digit     string    `json:"digit"`
}

type digitSender struct {
	code      uint8
	timestamp uint32
	frame     int
}

func (d *digitSender) payload() ([4]byte, bool, bool) {
	// 100 ms of tone, with three copies of the final event report.
	duration := min(d.frame+1, 5) * audioFrame
	p := [4]byte{d.code, 10}
	if d.frame >= 4 {
		p[1] |= 0x80
	}
	binary.BigEndian.PutUint16(p[2:], uint16(duration))
	marker := d.frame == 0
	d.frame++
	return p, marker, d.frame == 7
}

func dtmfMask(value string) uint16 {
	if value == "" {
		return 0xffff
	}
	var mask uint16
	for _, part := range strings.Split(value, ",") {
		from, to, ranged := strings.Cut(strings.TrimSpace(part), "-")
		a, err := strconv.Atoi(from)
		if err != nil || a < 0 || a > 15 {
			continue
		}
		b := a
		if ranged {
			b, err = strconv.Atoi(to)
			if err != nil || b < a {
				continue
			}
		}
		for i := a; i <= min(b, 15); i++ {
			mask |= 1 << i
		}
	}
	return mask
}

func dtmfList(mask uint16) string {
	if mask == 0xffff {
		return "0-15"
	}
	var items []string
	for i := range 16 {
		if mask&(1<<i) != 0 {
			items = append(items, strconv.Itoa(i))
		}
	}
	return strings.Join(items, ",")
}

func (m *Manager) SendDigit(digit string) error {
	digit = strings.ToUpper(strings.TrimSpace(digit))
	if len(digit) != 1 || !strings.Contains(dtmfDigits, digit) {
		return fmt.Errorf("%w，DTMF 按键无效", ErrConfig)
	}
	a := m.current()
	if a == nil {
		return ErrConflict
	}
	code := uint8(strings.Index(dtmfDigits, digit))
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.call
	if a.ctx.Err() != nil || c == nil || c.status.State != "incall" || c.dtmfSend&(1<<code) == 0 {
		return fmt.Errorf("%w，当前通话未协商该 DTMF 按键", ErrConflict)
	}
	select {
	case c.audio.digits <- code:
		return nil
	default:
		return fmt.Errorf("%w，DTMF 按键正在发送", ErrConflict)
	}
}

func (a *audioSession) recordDigit(direction string, code uint8) {
	a.mu.Lock()
	defer a.mu.Unlock()
	const limit = 16
	if len(a.digitEvents) >= limit {
		copy(a.digitEvents, a.digitEvents[1:])
		a.digitEvents = a.digitEvents[:limit-1]
	}
	a.digitEvents = append(a.digitEvents, DigitEvent{time.Now(), direction, string(dtmfDigits[code])})
}
