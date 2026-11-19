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
//go:generate sh generate-av.sh
//go:embed abrmedia/*/* avmedia/*/*
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

var audioMaster = []byte(`#EXTM3U
#EXT-X-VERSION:7
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Test tone",DEFAULT=YES,AUTOSELECT=YES,URI="audio.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=270000,RESOLUTION=320x180,FRAME-RATE=15.000,CODECS="avc1.42c01e,mp4a.40.2",AUDIO="audio"
video.m3u8
`)

func fragmentedVariant(variant string, separateAudio bool) bool {
	if separateAudio {
		return variant == "audio" || variant == "video"
	}
	return variant == "low" || variant == "high"
}

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

func fragmentedResource(name string, last uint64, separateAudio bool) (file, contentType string, segment uint64) {
	stem, extension, ok := strings.Cut(name, ".")
	if !ok {
		return "", "", 0
	}
	init := strings.HasPrefix(stem, "init-")
	variant, number, ok := strings.Cut(strings.TrimPrefix(stem, "init-"), "-")
	if !ok || !fragmentedVariant(variant, separateAudio) {
		return "", "", 0
	}
	seq, err := strconv.ParseUint(number, 10, 64)
	if err != nil || strconv.FormatUint(seq, 10) != number {
		return "", "", 0
	}
	first := last - min(last-1, 2*liveWindow-1)
	root, mediaType := "abrmedia", "video"
	if separateAudio {
		root = "avmedia"
		if variant == "audio" {
			mediaType = "audio"
		}
	}
	if init && extension == "mp4" && seq >= abrEpoch(first) && seq <= abrEpoch(last) {
		return root + "/" + variant + "/init.mp4", mediaType + "/mp4", 0
	}
	if !init && extension == "m4s" && seq >= first && seq <= last {
		return fmt.Sprintf("%s/%s/seg-%02d.m4s", root, variant, (seq-1)%abrSegments+1), mediaType + "/iso.segment", seq
	}
	return "", "", 0
}

// The origin mutex is held while we determine the live window and evidence.
func (m *Manager) serveFragmented(w http.ResponseWriter, r *http.Request, name string) {
	now := time.Now()
	last := m.live.sequence(now)
	separateAudio := m.status.Mode == "audio"
	var file, kind, fault string
	var data []byte
	variant, playlist := strings.CutSuffix(name, ".m3u8")
	switch {
	case name == "playlist.m3u8":
		kind, data = "application/vnd.apple.mpegurl", abrMaster
		if separateAudio {
			data = audioMaster
		}
	case playlist && fragmentedVariant(variant, separateAudio):
		if r.Method == http.MethodGet {
			m.recordPlaylist(now, last-liveWindow+1)
		}
		kind, data = "application/vnd.apple.mpegurl", abrPlaylist(variant, last)
	default:
		var segment uint64
		file, kind, segment = fragmentedResource(name, last, separateAudio)
		if r.Method == http.MethodGet && segment != 0 {
			m.status.SegmentRequests++
			if separateAudio && strings.HasPrefix(name, "audio-") {
				fault = m.checkLiveFault(r, segment, now)
			}
		}
	}
	m.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	if kind == "" || fault == "not_found" {
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
	if fault == "truncate" {
		serveTruncatedSegment(w, data)
		return
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
