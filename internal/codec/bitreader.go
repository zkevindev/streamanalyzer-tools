package codec

import "fmt"

type bitReader struct {
	data []byte
	bit  int
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data}
}

func (r *bitReader) bitsLeft() int {
	return len(r.data)*8 - r.bit
}

func (r *bitReader) readBit() (uint8, error) {
	if r.bitsLeft() < 1 {
		return 0, fmt.Errorf("bitreader: out of range")
	}
	b := r.data[r.bit/8]
	shift := 7 - (r.bit % 8)
	r.bit++
	return (b >> shift) & 0x01, nil
}

func (r *bitReader) readBits(n int) (uint64, error) {
	if n < 0 || r.bitsLeft() < n {
		return 0, fmt.Errorf("bitreader: not enough bits")
	}
	var v uint64
	for i := 0; i < n; i++ {
		b, err := r.readBit()
		if err != nil {
			return 0, err
		}
		v = (v << 1) | uint64(b)
	}
	return v, nil
}

func (r *bitReader) skipBits(n int) error {
	if n < 0 || r.bitsLeft() < n {
		return fmt.Errorf("bitreader: skip out of range")
	}
	r.bit += n
	return nil
}

func (r *bitReader) readUE() (uint64, error) {
	zeros := 0
	for {
		b, err := r.readBit()
		if err != nil {
			return 0, err
		}
		if b == 1 {
			break
		}
		zeros++
		if zeros > 63 {
			return 0, fmt.Errorf("bitreader: ue too large")
		}
	}
	if zeros == 0 {
		return 0, nil
	}
	rest, err := r.readBits(zeros)
	if err != nil {
		return 0, err
	}
	return (1<<zeros - 1) + rest, nil
}

func (r *bitReader) readSE() (int64, error) {
	ue, err := r.readUE()
	if err != nil {
		return 0, err
	}
	v := int64((ue + 1) / 2)
	if ue%2 == 0 {
		v = -v
	}
	return v, nil
}

func rbspFromNAL(nal []byte) []byte {
	out := make([]byte, 0, len(nal))
	zeroCount := 0
	for _, b := range nal {
		if zeroCount == 2 && b == 0x03 {
			zeroCount = 0
			continue
		}
		out = append(out, b)
		if b == 0x00 {
			zeroCount++
		} else {
			zeroCount = 0
		}
	}
	return out
}
