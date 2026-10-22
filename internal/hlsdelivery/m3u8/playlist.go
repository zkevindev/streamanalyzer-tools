// Package m3u8 解析 HLS 播放列表（RFC 8216）。
package m3u8

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Playlist 是主播放列表或媒体播放列表。
type Playlist struct {
	Version             int
	Type                string
	TargetDuration      float64
	MediaSequence       int
	EndList             bool
	IndependentSegments bool
	Variants            []Variant
	Renditions          []Rendition
	Segments            []Segment
	Map                 *Map
}

// Variant 是主播放列表里的一路码率。
type Variant struct {
	URI              string
	Bandwidth        int
	AverageBandwidth int
	Resolution       string
	Width            int
	Height           int
	Codecs           string
	FrameRate        float64
	Name             string
	IFrame           bool
}

// Rendition 是 EXT-X-MEDIA 里的音频或字幕。
type Rendition struct {
	Type    string
	GroupID string
	Name    string
	URI     string
}

// Segment 是媒体播放列表里的一个分片。
type Segment struct {
	URI             string
	Duration        float64
	Title           string
	Sequence        int
	ByteRange       *ByteRange
	Discontinuity   bool
	Gap             bool
	ProgramDateTime time.Time
	HasTime         bool
	Key             *Key
	Map             *Map
}

// ByteRange 是 EXT-X-BYTERANGE。
type ByteRange struct {
	Length int
	Offset int
}

// Key 是 EXT-X-KEY。IV 为空时用媒体序号。
type Key struct {
	Method string
	URI    string
	IV     []byte
}

// Map 是 EXT-X-MAP，fMP4 的初始化段。
type Map struct {
	URI       string
	ByteRange *ByteRange
}

// Parse 解析 m3u8 文本。
func Parse(text string) (*Playlist, error) {
	text = strings.TrimPrefix(text, "\uFEFF")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if !strings.Contains(text, "#EXTM3U") {
		return nil, fmt.Errorf("不是 HLS 播放列表")
	}

	pl := &Playlist{}
	var pending *Variant
	var seg *Segment
	var currentKey *Key
	var currentMap *Map
	var pendingPDT *time.Time
	var carry time.Time
	var carryOK bool
	var discontinuity bool
	var gap bool
	var nextByte int

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			switch {
			case line == "#EXTM3U":
			case strings.HasPrefix(line, "#EXT-X-VERSION:"):
				pl.Version, _ = strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-VERSION:"))
			case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
				pl.TargetDuration, _ = strconv.ParseFloat(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"), 64)
			case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
				pl.MediaSequence, _ = strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"))
			case strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:"):
				pl.Type = strings.TrimPrefix(line, "#EXT-X-PLAYLIST-TYPE:")
			case line == "#EXT-X-ENDLIST":
				pl.EndList = true
			case line == "#EXT-X-INDEPENDENT-SEGMENTS":
				pl.IndependentSegments = true
			case line == "#EXT-X-GAP":
				gap = true
			case line == "#EXT-X-DISCONTINUITY":
				discontinuity = true
				carryOK = false
				nextByte = 0
			case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
				v := parseVariant(parseAttributes(line[len("#EXT-X-STREAM-INF:"):]))
				pending = &v
			case strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF:"):
				v := parseVariant(parseAttributes(line[len("#EXT-X-I-FRAME-STREAM-INF:"):]))
				v.IFrame = true
				if v.URI != "" {
					pl.Variants = append(pl.Variants, v)
				}
			case strings.HasPrefix(line, "#EXT-X-MEDIA:"):
				attrs := parseAttributes(line[len("#EXT-X-MEDIA:"):])
				pl.Renditions = append(pl.Renditions, Rendition{
					Type:    attrs["TYPE"],
					GroupID: attrs["GROUP-ID"],
					Name:    attrs["NAME"],
					URI:     attrs["URI"],
				})
			case strings.HasPrefix(line, "#EXT-X-MAP:"):
				m, err := parseMap(parseAttributes(line[len("#EXT-X-MAP:"):]))
				if err != nil {
					return nil, err
				}
				currentMap = m
			case strings.HasPrefix(line, "#EXT-X-KEY:"):
				k, err := parseKey(parseAttributes(line[len("#EXT-X-KEY:"):]))
				if err != nil {
					return nil, err
				}
				if k.Method == "NONE" {
					currentKey = nil
				} else {
					currentKey = k
				}
			case strings.HasPrefix(line, "#EXT-X-PROGRAM-DATE-TIME:"):
				raw := strings.TrimPrefix(line, "#EXT-X-PROGRAM-DATE-TIME:")
				t, err := time.Parse(time.RFC3339Nano, raw)
				if err != nil {
					t, err = time.Parse(time.RFC3339, raw)
				}
				if err != nil {
					return nil, fmt.Errorf("无法解析节目时间 %q", raw)
				}
				pendingPDT = &t
			case strings.HasPrefix(line, "#EXTINF:"):
				dur, title := parseEXTINF(line)
				if seg == nil {
					seg = &Segment{}
				}
				seg.Duration = dur
				seg.Title = title
			case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
				br, err := parseByteRange(strings.TrimPrefix(line, "#EXT-X-BYTERANGE:"), nextByte)
				if err != nil {
					return nil, err
				}
				if seg == nil {
					seg = &Segment{}
				}
				seg.ByteRange = br
				nextByte = br.Offset + br.Length
			}
			continue
		}

		if pending != nil {
			pending.URI = line
			pl.Variants = append(pl.Variants, *pending)
			pending = nil
			continue
		}
		if seg == nil {
			continue
		}
		seg.URI = line
		seg.Key = currentKey
		seg.Map = currentMap
		seg.Discontinuity = discontinuity
		discontinuity = false
		seg.Gap = gap
		gap = false
		if len(pl.Segments) == 0 {
			pl.Map = currentMap
		}
		seg.Sequence = pl.MediaSequence + len(pl.Segments)
		if pendingPDT != nil {
			seg.ProgramDateTime = *pendingPDT
			seg.HasTime = true
			carry = pendingPDT.Add(seconds(seg.Duration))
			carryOK = true
			pendingPDT = nil
		} else if carryOK && !seg.Discontinuity {
			seg.ProgramDateTime = carry
			seg.HasTime = true
			carry = carry.Add(seconds(seg.Duration))
		}
		if seg.ByteRange == nil {
			nextByte = 0
		}
		pl.Segments = append(pl.Segments, *seg)
		seg = nil
	}
	return pl, nil
}

// IsMaster 表示这是主播放列表。
func (p *Playlist) IsMaster() bool {
	for _, v := range p.Variants {
		if !v.IFrame {
			return true
		}
	}
	return false
}

func parseVariant(attrs map[string]string) Variant {
	bw, _ := strconv.Atoi(attrs["BANDWIDTH"])
	abw, _ := strconv.Atoi(attrs["AVERAGE-BANDWIDTH"])
	fr, _ := strconv.ParseFloat(attrs["FRAME-RATE"], 64)
	res := attrs["RESOLUTION"]
	w, h := parseResolution(res)
	return Variant{
		URI:              attrs["URI"],
		Bandwidth:        bw,
		AverageBandwidth: abw,
		Resolution:       res,
		Width:            w,
		Height:           h,
		Codecs:           attrs["CODECS"],
		FrameRate:        fr,
		Name:             attrs["NAME"],
	}
}

func parseResolution(s string) (int, int) {
	w, h, ok := strings.Cut(s, "x")
	if !ok {
		return 0, 0
	}
	wi, _ := strconv.Atoi(w)
	hi, _ := strconv.Atoi(h)
	return wi, hi
}

func parseMap(attrs map[string]string) (*Map, error) {
	m := &Map{URI: attrs["URI"]}
	if m.URI == "" {
		return nil, fmt.Errorf("EXT-X-MAP 缺少 URI")
	}
	if raw := attrs["BYTERANGE"]; raw != "" {
		br, err := parseByteRange(raw, 0)
		if err != nil {
			return nil, err
		}
		m.ByteRange = br
	}
	return m, nil
}

func parseKey(attrs map[string]string) (*Key, error) {
	k := &Key{Method: strings.ToUpper(attrs["METHOD"]), URI: attrs["URI"]}
	if raw := attrs["IV"]; raw != "" {
		iv, err := parseIV(raw)
		if err != nil {
			return nil, err
		}
		k.IV = iv
	}
	return k, nil
}

func parseIV(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if len(s) != 32 {
		return nil, fmt.Errorf("IV 长度不是 128 位")
	}
	return hex.DecodeString(s)
}

func parseEXTINF(line string) (float64, string) {
	body := strings.TrimPrefix(line, "#EXTINF:")
	dur, title, _ := strings.Cut(body, ",")
	d, _ := strconv.ParseFloat(strings.TrimSpace(dur), 64)
	return d, strings.TrimSpace(title)
}

func parseByteRange(s string, next int) (*ByteRange, error) {
	lengthPart, offsetPart, hasOff := strings.Cut(s, "@")
	length, err := strconv.Atoi(strings.TrimSpace(lengthPart))
	if err != nil {
		return nil, fmt.Errorf("无法解析 BYTERANGE %q", s)
	}
	offset := next
	if hasOff {
		offset, err = strconv.Atoi(strings.TrimSpace(offsetPart))
		if err != nil {
			return nil, fmt.Errorf("无法解析 BYTERANGE 偏移 %q", s)
		}
	}
	return &ByteRange{Length: length, Offset: offset}, nil
}

func parseAttributes(s string) map[string]string {
	out := make(map[string]string)
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == ',') {
			i++
		}
		if i >= len(s) {
			break
		}
		eq := strings.IndexByte(s[i:], '=')
		if eq < 0 {
			break
		}
		key := s[i : i+eq]
		i += eq + 1
		if i < len(s) && s[i] == '"' {
			i++
			start := i
			for i < len(s) && s[i] != '"' {
				i++
			}
			out[key] = s[start:i]
			if i < len(s) {
				i++
			}
			continue
		}
		start := i
		for i < len(s) && s[i] != ',' {
			i++
		}
		out[key] = strings.TrimSpace(s[start:i])
	}
	return out
}

func seconds(v float64) time.Duration {
	return time.Duration(v * float64(time.Second))
}
