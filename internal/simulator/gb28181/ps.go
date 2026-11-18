package gb28181

import (
	"encoding/binary"
	"slices"
)

const (
	videoStreamID byte = 0xe0
	audioStreamID byte = 0xc0
	// MPEG-2 mux rate is expressed in units of 50 bytes per second.
	programMuxRate = 125000 // 50 Mb/s
)

type mediaUnit struct {
	data     []byte
	pts, dts int64 // 90 kHz
	stream   byte
	key      bool
}

type programMuxer struct {
	header []byte
	first  bool
	buffer []byte
}

func newProgramMuxer(audio string) *programMuxer {
	var audioType byte
	switch audio {
	case "g711a":
		audioType = 0x90
	case "g711u":
		audioType = 0x91
	}
	return &programMuxer{header: buildProgramHeader(audioType), first: true}
}

func buildProgramHeader(audioType byte) []byte {
	entries := []byte{0x1b, videoStreamID, 0, 0} // H.264
	bounds := []byte{videoStreamID, 0xe2, 0x00}
	var audioBound byte
	if audioType != 0 {
		entries = append(entries, audioType, audioStreamID, 0, 0)
		bounds = append(bounds, audioStreamID, 0xc0, 0x20)
		audioBound = 1
	}
	rate := uint32(programMuxRate)
	system := []byte{0, 0, 1, 0xbb, 0, byte(6 + len(bounds)),
		0x80 | byte(rate>>15), byte(rate >> 7), byte(rate<<1) | 1, audioBound << 2, 0xe1, 0x7f}
	system = append(system, bounds...)
	psm := []byte{0, 0, 1, 0xbc, 0, byte(10 + len(entries)), 0xe0, 0xff, 0, 0, 0, byte(len(entries))}
	psm = append(psm, entries...)
	crc := ^uint32(0)
	for _, b := range psm {
		crc ^= uint32(b) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return append(system, binary.BigEndian.AppendUint32(psm, crc)...)
}

func (m *programMuxer) pack(unit mediaUnit) []byte {
	scr := uint64(unit.dts)
	rate := uint32(programMuxRate)
	ps := append(m.buffer[:0], 0, 0, 1, 0xba,
		0x44|byte(scr>>27)&0x38|byte(scr>>28)&3, byte(scr>>20),
		0x04|byte(scr>>12)&0xf8|byte(scr>>13)&3, byte(scr>>5), 0x04|byte(scr<<3),
		0x01, byte(rate>>14), byte(rate>>6), byte(rate<<2)|3, 0xf8)
	if m.first || unit.key {
		ps = append(ps, m.header...)
		m.first = false
	}
	// Timestamp the first PES in this access unit. Continuation PES packets
	// keep large keyframes within the 16-bit packet-length field.
	var timestamps [10]byte
	flags, timestampSize := byte(0x80), 5
	ptsPrefix := byte(0x20)
	if unit.pts != unit.dts {
		flags, timestampSize, ptsPrefix = 0xc0, 10, 0x30
		writeTimestamp(timestamps[5:], 0x10, uint64(unit.dts))
	}
	writeTimestamp(timestamps[:5], ptsPrefix, uint64(unit.pts))
	for chunk := range slices.Chunk(unit.data, 60000) {
		ps = append(ps, 0, 0, 1, unit.stream)
		ps = binary.BigEndian.AppendUint16(ps, uint16(3+timestampSize+len(chunk)))
		ps = append(ps, 0x80, flags, byte(timestampSize))
		ps = append(ps, timestamps[:timestampSize]...)
		ps = append(ps, chunk...)
		flags, timestampSize = 0, 0
	}
	m.buffer = ps
	return ps
}

func writeTimestamp(out []byte, prefix byte, timestamp uint64) {
	out[0] = prefix | 1 | byte(timestamp>>29)&0x0e
	out[1] = byte(timestamp >> 22)
	out[2] = 1 | byte(timestamp>>14)&0xfe
	out[3] = byte(timestamp >> 7)
	out[4] = 1 | byte(timestamp<<1)
}
