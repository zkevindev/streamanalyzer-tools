package diagnostic

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yutopp/go-flv/tag"
	"github.com/yutopp/go-rtmp/message"
)

func TestDiagnosticPlayerBoundsSlowSubscriberAndKeepsPrefix(t *testing.T) {
	player := newPlayerSession(t.Context(), ProtocolHTTPFLV, nil, nil)
	defer player.Close()
	prefix := []byte{1, 2, 3}
	player.publish(prefix)
	prefix[0] = 9 // capture buffers can be reused after publish
	bootstrap, live, unsubscribe, err := player.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	if len(bootstrap) != 1 || !bytes.Equal(bootstrap[0], []byte{1, 2, 3}) || player.bootBytes != 0 {
		t.Fatalf("incorrect bootstrap: %v", bootstrap)
	}
	if _, _, _, err := player.Subscribe(); !errors.Is(err, ErrBusy) {
		t.Fatalf("second player subscription was accepted: %v", err)
	}
	for i := 0; i <= playerQueueSize; i++ {
		player.publish([]byte{byte(i)})
	}
	if !player.StreamDropped() {
		t.Fatal("queue overflow was not reported")
	}
	count := 0
	for data := range live {
		if !bytes.Equal(data, []byte{byte(count)}) {
			t.Fatal("queue overflow delivered a corrupted prefix")
		}
		count++
	}
	if count != playerQueueSize {
		t.Fatalf("slow player received %d data units, want a bound of %d", count, playerQueueSize)
	}
}

func TestDiagnosticPlayerRTMPRepackagesTagBodiesAndRebasesAggregate(t *testing.T) {
	player := newPlayerSession(t.Context(), ProtocolRTMP, nil, nil)
	defer player.Close()
	handler := &rtmpPlayerHandler{player: player}
	video := []byte{0x17, 1, 0, 0, 0, 0x65}
	at := uint32(0x12030405)
	if err := handler.OnVideo(at, bytes.NewReader(video)); err != nil {
		t.Fatal(err)
	}
	var aggregate []byte
	for _, stamp := range []uint32{4, 38} {
		audio := []byte{0xaf, 1, 0x21}
		header := make([]byte, 11)
		header[0] = byte(tag.TagTypeAudio)
		putFLVUint24(header[1:4], uint32(len(audio)))
		putFLVTimestamp(header[4:8], stamp)
		aggregate = append(aggregate, header...)
		aggregate = append(aggregate, audio...)
		aggregate = binary.BigEndian.AppendUint32(aggregate, uint32(11+len(audio)))
	}
	if err := handler.OnUnknownMessage(1000, &message.AggregateMessage{Payload: bytes.NewReader(aggregate)}); err != nil {
		t.Fatal(err)
	}
	bootstrap, _, unsubscribe, err := player.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	if len(bootstrap) != 3 || !bytes.Equal(bootstrap[0][11:11+len(video)], video) {
		t.Fatal("RTMP video body changed in FLV relay")
	}
	parser := flvReader{r: bytes.NewReader(bytes.Join(bootstrap, nil))}
	for i, want := range []int64{int64(at), 1000, 1034} {
		event, err := parser.next()
		if err != nil || event.DTS != want || (i == 0 && event.Kind != "video") || (i > 0 && event.Kind != "audio") {
			t.Errorf("incorrect FLV tag %d: %+v %v", i, event, err)
		}
	}
}

func TestDiagnosticPlayerRTPUsesPayloadAndPreservesWireDump(t *testing.T) {
	player := newPlayerSession(t.Context(), ProtocolRTPTS, nil, nil)
	defer player.Close()
	dir := t.TempDir()
	stream, err := newArtifact(dir, rtpStreamName)
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := newArtifact(dir, timelineName)
	if err != nil {
		t.Fatal(err)
	}
	capture := tsCapture{stream: stream, timeline: timeline, observation: &Observation{}, start: time.Now(), player: player, protocol: ProtocolRTPTS}
	payload := []byte{0x47, 2, 3, 4}
	packet := append([]byte{0xB0, 33, 0, 1, 0, 0, 0, 0, 2, 3, 4, 5, 0xbe, 0xde, 0, 1, 1, 2, 3, 4}, payload...)
	packet = append(packet, 0, 2) // RTP padding
	if more, err := capture.keep(packet); err != nil || !more {
		t.Fatalf("capture failed: %v", err)
	}
	if err := errors.Join(stream.close(), timeline.close()); err != nil {
		t.Fatal(err)
	}
	bootstrap, _, unsubscribe, err := player.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	if len(bootstrap) != 1 || !bytes.Equal(bootstrap[0], payload) {
		t.Fatalf("unexpected TS payload: %v", bootstrap)
	}
	dump, err := os.ReadFile(filepath.Join(dir, rtpStreamName))
	if err != nil || !bytes.Equal(dump, packet) {
		t.Fatalf("RTP dump changed: %v %v", dump, err)
	}
}

func TestDiagnosticPlayerHLSProxyRewritesResourcesAndForwardsRange(t *testing.T) {
	const token = "local-player-grant"
	var requests atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.Contains(r.URL.RawQuery, token) || strings.Contains(r.Referer(), token) {
			t.Error("local playback credential sent upstream")
		}
		switch r.URL.Path {
		case "/entry":
			http.Redirect(w, r, "/sub/index.m3u8?upstreamToken=redirect", http.StatusFound)
		case "/sub/index.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100000\nchild?upstreamToken=variant\n")
		case "/sub/child":
			if r.URL.Query().Get("upstreamToken") != "variant" {
				t.Error("signed upstream variant query was lost")
			}
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4?upstreamToken=init\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"key?upstreamToken=key\"\n#EXTINF:2,\nseg.ts?upstreamToken=seg\n")
		case "/sub/seg.ts":
			if r.Header.Get("Range") != "bytes=5-9" || r.URL.Query().Get("upstreamToken") != "seg" {
				t.Error("upstream segment range or query was lost")
			}
			w.Header().Set("Content-Range", "bytes 5-9/20")
			w.Header().Set("Content-Length", "5")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "bytes")
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	target, _ := url.Parse(origin.URL + "/entry?upstreamToken=root")
	player := newPlayerSession(t.Context(), ProtocolHLS, target, captureClient())
	defer player.Close()
	request := func(resource string, rangeValue string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/player/hls/"+resource, nil)
		r.Header.Set("Range", rangeValue)
		w := httptest.NewRecorder()
		player.ServeHLS(w, r, filepath.Base(r.URL.Path))
		return w
	}
	master := request("index.m3u8?token="+token, "")
	if master.Code != http.StatusOK || strings.Contains(master.Body.String(), "upstreamToken") {
		t.Fatalf("invalid proxy master: %d %s", master.Code, master.Body.String())
	}
	variantLink := strings.TrimSpace(strings.Split(master.Body.String(), "\n")[2])
	variant := request(variantLink, "")
	if variant.Code != http.StatusOK || strings.Contains(variant.Body.String(), "upstreamToken") || strings.Count(variant.Body.String(), token) != 3 {
		t.Fatalf("invalid proxy variant: %d %s", variant.Code, variant.Body.String())
	}
	segmentLink := strings.TrimSpace(strings.Split(variant.Body.String(), "\n")[4])
	segment := request(segmentLink, "bytes=5-9")
	if segment.Code != http.StatusPartialContent || segment.Body.String() != "bytes" || segment.Header().Get("Content-Range") != "bytes 5-9/20" {
		t.Fatalf("invalid proxy range response: %d %s", segment.Code, segment.Body.String())
	}
	before := requests.Load()
	if absent := request("unknown.m3u8?token="+token+"&url="+url.QueryEscape(origin.URL), ""); absent.Code != http.StatusNotFound || requests.Load() != before {
		t.Fatal("unknown resource caused a source request")
	}
}

func TestDiagnosticPlayerWaitExitsOnSessionClose(t *testing.T) {
	player := newPlayerSession(context.Background(), ProtocolHTTPFLV, nil, nil)
	ready := make(chan struct{})
	go func() {
		defer close(ready)
		player.WaitForSubscriber(t.Context(), 3*time.Second)
	}()
	player.Close()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("closing session did not unblock pending attach")
	}
}
