package diagnostic

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHLSEvidenceMasterRangesEncryptionAndReplay(t *testing.T) {
	master := []byte("#EXTM3U\r\n#EXT-X-STREAM-INF:BANDWIDTH=680000\r\n/low.m3u8\r\n#EXT-X-STREAM-INF:BANDWIDTH=1200000\r\n/media.m3u8?auth=fixture-secret\r\n")
	mediaList := []byte(`#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:100
#EXT-X-MAP:URI="/a.mp4?auth=fixture-secret",BYTERANGE="8@0"
#EXTINF:2.0,
#EXT-X-BYTERANGE:8@8
/a.mp4?auth=fixture-secret
#EXT-X-KEY:METHOD=AES-128,URI="/key?auth=fixture-secret"
#EXTINF:2.0,
/encrypted
#EXT-X-KEY:METHOD=NONE
#EXT-X-GAP
#EXTINF:2.0,
/gap
#EXTINF:2.0,
/missing
#EXT-X-ENDLIST
`)
	box := func(ty string) []byte {
		return append(binary.BigEndian.AppendUint32(nil, 8), ty...)
	}
	init, mediaPart := box("ftyp"), box("moof")
	encrypted := bytes.Repeat([]byte{0xa9}, 16)
	var keyRequests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write(master)
		case "/media.m3u8":
			if r.URL.Query().Get("auth") != "fixture-secret" {
				t.Error("variant playback query was dropped")
			}
			_, _ = w.Write(mediaList)
		case "/a.mp4":
			if r.URL.Query().Get("auth") != "fixture-secret" {
				t.Error("range resource query was dropped")
			}
			switch r.Header.Get("Range") {
			case "bytes=0-7":
				w.Header().Set("Content-Range", "bytes 0-7/16")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(init)
			case "bytes=8-15":
				w.Header().Set("Content-Range", "bytes 8-15/16")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(mediaPart)
			default:
				t.Error("range request absent or invalid")
				w.WriteHeader(http.StatusBadRequest)
			}
		case "/encrypted":
			_, _ = w.Write(encrypted)
		case "/key", "/gap", "/low.m3u8":
			keyRequests.Add(1)
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	target, err := url.Parse(srv.URL + "/master.m3u8?auth=fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	report, err := sampleHLS(t.Context(), captureClient(), dir, target, 3)
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()

	if keyRequests.Load() != 0 {
		t.Fatal("unexpected key, EXT-X-GAP, or lower-quality variant fetch")
	}
	if got := report.Manifest.Observation.EndReason; got != "eof" {
		t.Fatalf("end reason %q, want eof", got)
	}
	m := report.HLS
	if m == nil || m.PlaylistsCaptured != 2 || m.SegmentsCaptured != 2 || m.SegmentsFailed != 1 ||
		m.SegmentsSkipped != 1 || m.InitSegmentsCaptured != 1 || m.EncryptedSegments != 1 ||
		m.RenditionBandwidth != 1200000 || m.MediaBytes != int64(len(mediaPart)+len(encrypted)) {
		t.Fatalf("unexpected HLS metrics: %+v", m)
	}
	for _, code := range []string{"hls_segment_fetch_failed", "hls_segment_gap", "hls_encrypted_segment"} {
		if !slices.ContainsFunc(report.Findings, func(f Finding) bool { return f.Code == code }) {
			t.Errorf("missing finding %q", code)
		}
	}
	if slices.ContainsFunc(report.Findings, func(f Finding) bool { return f.Code == "hls_segment_format_unknown" }) {
		t.Fatal("fMP4 segment with archived initialization data was not recognized")
	}

	archive, err := zip.OpenReader(filepath.Join(dir, hlsStreamName))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{
		"playlist-0001.m3u8": master, "playlist-0002.m3u8": mediaList,
		"init-0003.bin": init, "segment-0004.bin": mediaPart, "segment-0005.bin": encrypted,
	}
	for _, file := range archive.File {
		body, readErr := readHLSObject(file)
		if readErr != nil || !bytes.Equal(want[file.Name], body) || len(body) == 0 {
			t.Errorf("raw artifact %s differs: %v", file.Name, readErr)
		}
		delete(want, file.Name)
	}
	_ = archive.Close()
	if len(want) != 0 {
		t.Errorf("missing artifacts: %v", want)
	}

	timelinePath := filepath.Join(dir, timelineName)
	ledger, err := os.ReadFile(timelinePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ledger, []byte("fixture-secret")) {
		t.Fatal("signed URI leaked into the timeline")
	}
	input, err := modelInput(report)
	if err != nil || bytes.Contains(input, []byte("fixture-secret")) {
		t.Fatalf("model input contains URI authentication or failed: %v", err)
	}

	if err := persistReport(dir, report); err != nil {
		t.Fatal(err)
	}
	var bundle bytes.Buffer
	if err := writeBundle(&bundle, dir, report); err != nil {
		t.Fatal(err)
	}
	replayed, err := readBundle(t.TempDir(), &bundle)
	if err != nil || !reflect.DeepEqual(replayed, report) {
		t.Fatalf("offline replay differs: error=%v, replay=%+v, captured=%+v", err, replayed, report)
	}

	ledger = bytes.Replace(ledger, []byte(`"sequence":100`), []byte(`"sequence":999`), 1)
	if err := os.WriteFile(timelinePath, ledger, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := replayHLS(dir, report.Manifest); !errors.Is(err, ErrInvalid) {
		t.Fatalf("inconsistent segment reference was accepted: %v", err)
	}
}

func TestHLSSampleFollowsLiveEdge(t *testing.T) {
	var requests atomic.Int32
	var old atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/live.m3u8":
			polls := requests.Add(1)
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:40\n")
			for i := 40; i < 46+int(min(polls-1, 1)); i++ {
				_, _ = fmt.Fprintf(w, "#EXTINF:1.0,\n/segment-%d.ts\n", i)
			}
		case strings.HasPrefix(r.URL.Path, "/segment-"):
			if slices.Contains([]string{"/segment-40.ts", "/segment-41.ts", "/segment-42.ts"}, r.URL.Path) {
				old.Add(1)
			}
			raw := make([]byte, 2*188)
			raw[0], raw[188] = 0x47, 0x47
			_, _ = w.Write(raw)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	target, err := url.Parse(server.URL + "/live.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	report, err := sampleHLS(context.Background(), captureClient(), t.TempDir(), target, 2)
	if err != nil {
		t.Fatal(err)
	}
	if old.Load() != 0 || requests.Load() < 2 || report.HLS.SegmentsCaptured != 4 ||
		report.Manifest.Observation.EndReason != "duration_limit" {
		t.Fatalf("live sampling requested stale segments or missed new ones: old=%d polls=%d report=%+v", old.Load(), requests.Load(), report)
	}
}

func TestHLSSampleKeepsIndependentAudioRendition(t *testing.T) {
	// Two packets identify an H.264 elementary stream in the main variant.
	ts := bytes.Repeat([]byte{0xff}, 376)
	copy(ts[:188], []byte{0x47, 0x40, 0, 0x10, 0, 0x00, 0xb0, 13, 0, 1, 0xc1, 0, 0, 0, 1, 0xe1, 0, 0, 0, 0, 0})
	copy(ts[188:], []byte{0x47, 0x41, 0, 0x10, 0, 0x02, 0xb0, 18, 0, 1, 0xc1, 0, 0, 0xe1, 1, 0xf0, 0, 0x1b, 0xe1, 1, 0xf0, 0, 0, 0, 0, 0})
	aac := []byte{0xff, 0xf1, 0x50, 0x80, 0x01, 0x1f, 0xfc, 0x13}
	var other atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = io.WriteString(w, `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Other",URI="/other.m3u8",AUTOSELECT=YES
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Default",URI="/audio.m3u8",DEFAULT=YES,AUTOSELECT=YES
#EXT-X-STREAM-INF:BANDWIDTH=1500000,AUDIO="audio",CODECS="av01,ec-3"
/video.m3u8
`)
		case "/video.m3u8", "/audio.m3u8":
			uri := "/video.ts"
			if r.URL.Path == "/audio.m3u8" {
				uri = "/audio.aac"
			}
			_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:80\n#EXTINF:1.0,\n%s\n#EXT-X-ENDLIST\n", uri)
		case "/video.ts":
			_, _ = w.Write(ts)
		case "/audio.aac":
			_, _ = w.Write(aac)
		default:
			other.Add(1)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	target, err := url.Parse(server.URL + "/master.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	report, err := sampleHLS(t.Context(), captureClient(), dir, target, 3)
	if err != nil {
		t.Fatal(err)
	}
	if other.Load() != 0 || report.HLS.SegmentsCaptured != 2 || report.HLS.AudioSegmentsCaptured != 1 ||
		report.HLS.PlaylistsCaptured != 3 || report.Manifest.Observation.EndReason != "eof" {
		t.Fatalf("unexpected multivariant report: %+v", report.HLS)
	}
	if !slices.Equal(report.Metrics.VideoCodecs, []string{"h264"}) || !slices.Equal(report.Metrics.AudioCodecs, []string{"aac"}) {
		t.Fatalf("unexpected observed media codecs: video=%v, audio=%v", report.Metrics.VideoCodecs, report.Metrics.AudioCodecs)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("expected no analysis findings: %+v", report.Findings)
	}
}
