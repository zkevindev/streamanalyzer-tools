package hlsorigin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
	"time"
)

func TestABRVariantsAndDiscontinuity(t *testing.T) {
	var m Manager
	started, err := m.Start(Config{Mode: "abr"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	get := func(name string, status int) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		m.Serve(w, httptest.NewRequest("GET", path.Join(path.Dir(started.Playlist), name), nil), started.ID, name)
		if w.Code != status {
			t.Fatalf("%s returned %d want %d", name, w.Code, status)
		}
		return w.Body.Bytes()
	}
	master := get("playlist.m3u8", http.StatusOK)
	if !bytes.Contains(master, []byte("RESOLUTION=160x90")) || !bytes.Contains(master, []byte("RESOLUTION=320x180")) {
		t.Fatalf("missing bitrate ladder: %s", master)
	}
	lowInit, highInit := get("init-low-0.mp4", http.StatusOK), get("init-high-0.mp4", http.StatusOK)
	if bytes.Equal(lowInit, highInit) || !bytes.Contains(lowInit, []byte("ftyp")) || !bytes.Contains(highInit, []byte("ftyp")) {
		t.Fatal("expected distinct fMP4 initializations")
	}
	for _, quality := range []string{"low", "high"} {
		if !bytes.Contains(get(quality+"-1.m4s", http.StatusOK), []byte("moof")) {
			t.Fatal("missing fMP4 media data")
		}
	}
	m.mu.Lock()
	m.live.started = time.Now()
	m.live.last = 7
	m.mu.Unlock()
	for _, quality := range []string{"low", "high"} {
		playlist := string(get(quality+".m3u8", http.StatusOK))
		if !strings.Contains(playlist, "#EXT-X-MEDIA-SEQUENCE:5") ||
			!strings.Contains(playlist, "#EXT-X-DISCONTINUITY\n#EXT-X-MAP:URI=\"init-"+quality+"-1.mp4\"") {
			t.Fatalf("missing fMP4 discontinuity and initialization change: %s", playlist)
		}
		get("init-"+quality+"-1.mp4", http.StatusOK)
		get(quality+"-7.m4s", http.StatusOK)
		get(quality+"-8.m4s", http.StatusNotFound)
	}
}
