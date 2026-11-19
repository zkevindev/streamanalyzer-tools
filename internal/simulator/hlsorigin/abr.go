package hlsorigin

import (
	"bytes"
	"embed"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:generate sh generate-abr.sh
//go:embed abrmedia/*/*
var abrMedia embed.FS

const abrSegments = 6

var abrMaster = []byte(`#EXTM3U
#EXT-X-VERSION:7
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-STREAM-INF:BANDWIDTH=145000,AVERAGE-BANDWIDTH=105000,RESOLUTION=160x90,FRAME-RATE=15.000,CODECS="avc1.42c01e,mp4a.40.2"
low.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=250000,AVERAGE-BANDWIDTH=190000,RESOLUTION=320x180,FRAME-RATE=15.000,CODECS="avc1.42c01e,mp4a.40.2"
high.m3u8
`)

func abrEpoch(seq uint64) uint64 { return (seq - 1) / abrSegments }

func abrPlaylist(variant string, last uint64) []byte {
	first := last - liveWindow + 1
	var out bytes.Buffer
	fmt.Fprintf(&out, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-DISCONTINUITY-SEQUENCE:%d\n", first, abrEpoch(first))
	for seq := first; seq <= last; seq++ {
		boundary := (seq-1)%abrSegments == 0
		if boundary && seq > first {
			out.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		if seq == first || boundary {
			fmt.Fprintf(&out, "#EXT-X-MAP:URI=\"init-%s-%d.mp4\"\n", variant, abrEpoch(seq))
		}
		fmt.Fprintf(&out, "#EXTINF:2.000,\n%s-%d.m4s\n", variant, seq)
	}
	return out.Bytes()
}

func abrResource(name string, last uint64) (file, contentType string) {
	stem, extension, ok := strings.Cut(name, ".")
	if !ok {
		return "", ""
	}
	init := strings.HasPrefix(stem, "init-")
	variant, number, ok := strings.Cut(strings.TrimPrefix(stem, "init-"), "-")
	if !ok || variant != "low" && variant != "high" {
		return "", ""
	}
	seq, err := strconv.ParseUint(number, 10, 64)
	if err != nil || strconv.FormatUint(seq, 10) != number {
		return "", ""
	}
	first := last - min(last-1, 2*liveWindow-1)
	if init && extension == "mp4" && seq >= abrEpoch(first) && seq <= abrEpoch(last) {
		return "abrmedia/" + variant + "/init.mp4", "video/mp4"
	}
	if !init && extension == "m4s" && seq >= first && seq <= last {
		return fmt.Sprintf("abrmedia/%s/seg-%02d.m4s", variant, (seq-1)%abrSegments+1), "video/iso.segment"
	}
	return "", ""
}

// The origin mutex is held while we determine the live window and evidence.
func (m *Manager) serveABR(w http.ResponseWriter, r *http.Request, name string) {
	now := time.Now()
	last := m.live.sequence(now)
	var file, kind string
	var data []byte
	switch name {
	case "playlist.m3u8":
		kind, data = "application/vnd.apple.mpegurl", abrMaster
	case "low.m3u8", "high.m3u8":
		if r.Method == http.MethodGet {
			m.recordPlaylist(now, last-liveWindow+1)
		}
		kind, data = "application/vnd.apple.mpegurl", abrPlaylist(strings.TrimSuffix(name, ".m3u8"), last)
	default:
		file, kind = abrResource(name, last)
		if r.Method == http.MethodGet && kind == "video/iso.segment" {
			m.status.SegmentRequests++
		}
	}
	m.mu.Unlock()
	if kind == "" {
		http.NotFound(w, r)
		return
	}
	if file != "" {
		var err error
		data, err = abrMedia.ReadFile(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
