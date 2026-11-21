package offline

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
)

// DetectMode inspects file magic bytes (with filename extension fallback) and
// returns the inferred OfflineMode plus a short reason such as "magic:flv".
func DetectMode(path, filename string) (models.OfflineMode, string) {
	f, err := os.Open(path)
	if err != nil {
		return detectByExt(filename)
	}
	defer f.Close()

	var totalSize int64
	if st, err := f.Stat(); err == nil {
		totalSize = st.Size()
	}

	buf := make([]byte, 8192)
	n, _ := io.ReadFull(f, buf)
	head := buf[:n]
	if len(head) == 0 {
		return detectByExt(filename)
	}

	// 1. FLV header: "FLV" + version 1
	if len(head) >= 9 && bytes.Equal(head[:3], []byte{'F', 'L', 'V'}) && head[3] == 1 {
		return models.OfflineModeFLV, "magic:flv"
	}

	// 2. PCAP / PCAPNG
	if len(head) >= 4 {
		b0, b1, b2, b3 := head[0], head[1], head[2], head[3]
		if (b0 == 0xd4 && b1 == 0xc3 && b2 == 0xb2 && b3 == 0xa1) ||
			(b0 == 0xa1 && b1 == 0xb2 && b2 == 0xc3 && b3 == 0xd4) ||
			(b0 == 0x4d && b1 == 0x3c && b2 == 0xb2 && b3 == 0xa1) ||
			(b0 == 0xa1 && b1 == 0xb2 && b2 == 0x3c && b3 == 0x4d) {
			return models.OfflineModePCAP, "magic:pcap"
		}
		if b0 == 0x0a && b1 == 0x0d && b2 == 0x0d && b3 == 0x0a {
			return models.OfflineModePCAP, "magic:pcapng"
		}
	}

	// 3. ISO BMFF / MP4
	if len(head) >= 8 {
		boxSize := binary.BigEndian.Uint32(head[:4])
		boxType := string(head[4:8])
		if (boxSize == 0 || boxSize == 1 || boxSize >= 8) &&
			(boxType == "ftyp" || boxType == "styp" || boxType == "moov" || boxType == "moof") {
			return models.OfflineModeMP4, "magic:mp4(" + boxType + ")"
		}
	}

	// 4. MPEG-TS (188-byte packets or 192-byte M2TS packets)
	if stride, offset := detectTSSync(head); stride > 0 {
		if stride == 192 {
			return models.OfflineModeTS, "magic:m2ts"
		}
		if offset == 0 {
			return models.OfflineModeTS, "magic:ts"
		}
		return models.OfflineModeTS, "magic:ts-sync"
	}

	// 5. Annex B H.264 / H.265 elementary stream
	if looksLikeAnnexB(head) {
		return models.OfflineModeAnnexB, "magic:annexb"
	}

	// 6. Extension fallback
	if mode, reason := detectByExt(filename); mode != "" {
		return mode, reason
	}

	// 7. RTMP raw with C0/S0 version byte 0x03
	if head[0] == 0x03 && totalSize > 3073 {
		return models.OfflineModeRaw, "magic:rtmp-v3"
	}

	return "", ""
}

func detectTSSync(head []byte) (stride int, offset int) {
	for _, s := range []int{188, 192} {
		maxOff := s
		if maxOff > len(head) {
			maxOff = len(head)
		}
		for off := 0; off < maxOff; off++ {
			if head[off] != 0x47 {
				continue
			}
			hits := 1
			for pos := off + s; pos < len(head); pos += s {
				if head[pos] != 0x47 {
					hits = 0
					break
				}
				hits++
				if hits >= 4 {
					return s, off
				}
			}
			if hits >= 3 {
				return s, off
			}
		}
	}
	return 0, 0
}

func looksLikeAnnexB(head []byte) bool {
	if len(head) < 5 {
		return false
	}
	hasPrefix := (head[0] == 0 && head[1] == 0 && head[2] == 1) ||
		(head[0] == 0 && head[1] == 0 && head[2] == 0 && head[3] == 1)
	if !hasPrefix {
		return false
	}
	nalus := splitAnnexBNALUs(head)
	if len(nalus) == 0 {
		return false
	}
	if codec.VideoMetaFromAnnexB(head) != nil {
		return true
	}
	valid := 0
	for _, nalu := range nalus {
		if len(nalu) == 0 {
			continue
		}
		h264Type := nalu[0] & 0x1f
		if h264Type == 1 || h264Type == 5 || h264Type == 6 || h264Type == 7 || h264Type == 8 || h264Type == 9 {
			valid++
			continue
		}
		if len(nalu) >= 2 && (nalu[0]&0x80) == 0 {
			h265Type := (nalu[0] >> 1) & 0x3f
			if h265Type <= 40 {
				valid++
			}
		}
	}
	return valid > 0
}

func detectByExt(filename string) (models.OfflineMode, string) {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	switch ext {
	case ".flv":
		return models.OfflineModeFLV, "ext:.flv"
	case ".mp4", ".m4s", ".m4v", ".mov", ".fmp4":
		return models.OfflineModeMP4, "ext:" + ext
	case ".ts", ".m2ts", ".mts":
		return models.OfflineModeTS, "ext:" + ext
	case ".pcap", ".pcapng", ".cap":
		return models.OfflineModePCAP, "ext:" + ext
	case ".h264", ".264", ".h265", ".265", ".hevc", ".annexb", ".es":
		return models.OfflineModeAnnexB, "ext:" + ext
	case ".raw", ".rtmp":
		return models.OfflineModeRaw, "ext:" + ext
	default:
		return "", ""
	}
}
