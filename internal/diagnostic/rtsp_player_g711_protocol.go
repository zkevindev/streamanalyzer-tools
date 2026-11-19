package diagnostic

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	rtspG711OutputRate    = 48000
	rtspG711AACSamples    = 1024
	rtspG711QueueSize     = 32
	rtspG711MetadataLimit = 2 << 20
)

// RTSPG711PreviewReport describes a local, lossy audio conversion. Sample
// counters use per-channel input samples unless the field says output.
// Unknown encoder timing remains nil; no source-frame measurement is implied.
type RTSPG711PreviewReport struct {
	Version                int    `json:"version"`
	Encoder                string `json:"encoder"`
	State                  string `json:"state"`
	ReasonCode             string `json:"reason_code,omitempty"`
	InputSampleRate        int    `json:"input_sample_rate"`
	OutputSampleRate       int    `json:"output_sample_rate"`
	Channels               int    `json:"channels"`
	InputPackets           int    `json:"input_packets"`
	InputSamples           int64  `json:"input_samples"`
	InsertedSilenceSamples int64  `json:"inserted_silence_samples"`
	DroppedLateSamples     int64  `json:"dropped_late_samples"`
	DroppedPackets         int    `json:"dropped_packets"`
	PacketErrors           int    `json:"packet_errors"`
	TimelineResets         int    `json:"timeline_resets"`
	OutputAUs              int    `json:"output_aus"`
	OutputSamples          int64  `json:"output_samples"`
	FirstInputPTS          *int64 `json:"first_input_pts,omitempty"`
	EncoderPrimingSamples  *int64 `json:"encoder_priming_samples,omitempty"`
	EncoderPaddingSamples  *int64 `json:"encoder_padding_samples,omitempty"`
	ToolCheckMS            *int64 `json:"tool_check_ms,omitempty"`
	StartupMS              *int64 `json:"startup_ms,omitempty"`
	DrainMS                *int64 `json:"drain_ms,omitempty"`
}

type rtspG711CRC struct {
	pts, dts, duration int64
	size               int
	checksum           uint32
}

type rtspG711CRCReader struct {
	scanner                             *bufio.Scanner
	counted                             *rtspCountingReader
	rate, channels                      int
	timebase, codec, sampleRate, layout bool
	rows                                int
}

func newRTSPG711CRCReader(reader io.Reader, channels int) *rtspG711CRCReader {
	counted := &rtspCountingReader{Reader: io.LimitReader(reader, rtspG711MetadataLimit+1)}
	scanner := bufio.NewScanner(counted)
	scanner.Buffer(make([]byte, 1024), 2048)
	return &rtspG711CRCReader{scanner: scanner, counted: counted, rate: rtspG711OutputRate, channels: channels}
}

func (r *rtspG711CRCReader) next() (rtspG711CRC, error) {
	for r.scanner.Scan() {
		line := strings.TrimSpace(r.scanner.Text())
		if strings.HasPrefix(line, "#") {
			key, value, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			value = strings.TrimSpace(value)
			switch strings.TrimSpace(key) {
			case "#tb 0":
				if value != "1/48000" {
					return rtspG711CRC{}, fmt.Errorf("invalid AAC timebase")
				}
				r.timebase = true
			case "#codec_id 0":
				if value != "aac" {
					return rtspG711CRC{}, fmt.Errorf("invalid output codec")
				}
				r.codec = true
			case "#sample_rate 0":
				if value != "48000" {
					return rtspG711CRC{}, fmt.Errorf("invalid AAC rate")
				}
				r.sampleRate = true
			case "#channel_layout_name 0", "#channel_layout 0":
				want := "mono"
				if r.channels == 2 {
					want = "stereo"
				}
				legacy := "0x4"
				if r.channels == 2 {
					legacy = "0x3"
				}
				if value != want && value != legacy {
					return rtspG711CRC{}, fmt.Errorf("invalid AAC layout")
				}
				r.layout = true
			}
			continue
		}
		if !r.timebase || !r.codec || !r.sampleRate || !r.layout {
			return rtspG711CRC{}, fmt.Errorf("missing AAC packet metadata")
		}
		parts := strings.Split(line, ",")
		if len(parts) != 6 {
			return rtspG711CRC{}, fmt.Errorf("invalid AAC packet metadata")
		}
		values := make([]int64, 5)
		for index := range values {
			value, err := strconv.ParseInt(strings.TrimSpace(parts[index]), 10, 64)
			if err != nil {
				return rtspG711CRC{}, fmt.Errorf("invalid AAC packet number")
			}
			values[index] = value
		}
		checksum, err := strconv.ParseUint(strings.TrimSpace(parts[5]), 0, 32)
		if err != nil || values[0] != 0 || values[1] != values[2] || values[1] < -rtspG711OutputRate || values[1] > int64(maxSeconds+3)*rtspG711OutputRate || values[3] <= 0 || values[3] > rtspG711AACSamples || values[4] <= 0 || values[4] > 8191 {
			return rtspG711CRC{}, fmt.Errorf("AAC packet metadata exceeds bounds")
		}
		r.rows++
		if r.rows > (maxSeconds+4)*rtspG711OutputRate/rtspG711AACSamples+2 {
			return rtspG711CRC{}, fmt.Errorf("too many AAC packets")
		}
		return rtspG711CRC{pts: values[2], dts: values[1], duration: values[3], size: int(values[4]), checksum: uint32(checksum)}, nil
	}
	if r.counted.count > rtspG711MetadataLimit || r.counted.readErr != nil {
		return rtspG711CRC{}, fmt.Errorf("AAC metadata exceeds bounds")
	}
	if err := r.scanner.Err(); err != nil {
		return rtspG711CRC{}, err
	}
	return rtspG711CRC{}, io.EOF
}

func readRTSPG711ADTS(reader io.Reader, channels int) ([]byte, error) {
	var header [7]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	if header[0] != 0xff || header[1]&0xf6 != 0xf0 || header[2]>>6 != 1 || (header[2]>>2)&15 != 3 || int(header[2]&1)<<2|int(header[3]>>6) != channels || header[6]&3 != 0 {
		return nil, fmt.Errorf("invalid AAC-LC ADTS header")
	}
	size := int(header[3]&3)<<11 | int(header[4])<<3 | int(header[5]>>5)
	headSize := 7
	if header[1]&1 == 0 {
		var crc [2]byte
		if _, err := io.ReadFull(reader, crc[:]); err != nil {
			return nil, err
		}
		headSize = 9
	}
	if size <= headSize || size > 8191 {
		return nil, fmt.Errorf("invalid ADTS frame size")
	}
	payload := make([]byte, size-headSize)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// The framecrc muxer uses Adler-32 with an initial value of zero.
func rtspG711Adler(data []byte) uint32 {
	var a, b uint32
	for _, value := range data {
		a = (a + uint32(value)) % 65521
		b = (b + a) % 65521
	}
	return b<<16 | a
}
