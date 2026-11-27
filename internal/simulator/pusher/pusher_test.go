package pusher

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/mediasource"

	"github.com/pion/webrtc/v4"
)

func boolPtr(v bool) *bool { return &v }

func TestConfigNormalizeAndRedact(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid rtmp pattern",
			cfg: Config{
				Protocol:   ProtocolRTMP,
				TargetURL:  "rtmp://user:secret@127.0.0.1:1935/live/test?token=abc123",
				SourceKind: SourcePattern,
			},
		},
		{
			name: "valid rtsp udp",
			cfg: Config{
				Protocol:      ProtocolRTSP,
				TargetURL:     "rtsp://127.0.0.1:8554/live/cam1",
				RTSPTransport: "udp",
				SourceKind:    SourcePattern,
			},
		},
		{
			name: "valid whip",
			cfg: Config{
				Protocol:   ProtocolWHIP,
				TargetURL:  "https://example.com/whip",
				SourceKind: SourcePattern,
			},
		},
		{
			name: "bad scheme for rtmp",
			cfg: Config{
				Protocol:   ProtocolRTMP,
				TargetURL:  "http://127.0.0.1/live/test",
				SourceKind: SourcePattern,
			},
			wantErr: true,
		},
		{
			name: "missing local file",
			cfg: Config{
				Protocol:   ProtocolRTMP,
				TargetURL:  "rtmp://127.0.0.1:1935/live/test",
				SourceKind: SourceFile,
				FilePath:   "/nonexistent/path/video.mp4",
			},
			wantErr: true,
		},
		{
			name: "invalid resolution",
			cfg: Config{
				Protocol:   ProtocolRTMP,
				TargetURL:  "rtmp://127.0.0.1:1935/live/test",
				SourceKind: SourcePattern,
				Resolution: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			err := cfg.normalize()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	redacted := redactTargetURL("rtmp://alice:topsecret@127.0.0.1:1935/live/s?token=xyz&foo=bar")
	if strings.Contains(redacted, "topsecret") || strings.Contains(redacted, "xyz") {
		t.Fatalf("secret leaked in redacted URL: %s", redacted)
	}
	if !strings.Contains(redacted, "foo=bar") {
		t.Fatalf("non-sensitive query dropped: %s", redacted)
	}
}

func TestBuildFFmpegArgsLoopAndRTSP(t *testing.T) {
	cfg := Config{
		Protocol:      ProtocolRTSP,
		TargetURL:     "rtsp://127.0.0.1:8554/live/ch1",
		RTSPTransport: "tcp",
		SourceKind:    SourceFile,
		FilePath:      "/tmp/demo.mp4",
		Loop:          boolPtr(true),
		LoopCount:     0,
		VideoMode:     "copy",
		AudioMode:     "copy",
	}
	args := buildFFmpegArgs(cfg)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-stream_loop -1") {
		t.Fatalf("expected infinite stream_loop, got: %s", joined)
	}
	if !strings.Contains(joined, "-rtsp_transport tcp -f rtsp rtsp://127.0.0.1:8554/live/ch1") {
		t.Fatalf("expected rtsp tcp output args, got: %s", joined)
	}
	if !strings.Contains(joined, "-c:v copy") {
		t.Fatalf("expected video copy, got: %s", joined)
	}
}

func TestRTMPConcurrentFileLoopAndPatternPush(t *testing.T) {
	if _, err := findFFmpeg(); err != nil {
		t.Skipf("skipping real FFmpeg push test: %v", err)
	}

	var rtmpServer1 mediasource.RTMPManager
	defer rtmpServer1.Close()
	rtmpStatus1, err := rtmpServer1.Start(mediasource.RTMPConfig{
		Fault: "publish_relay",
		Port:  0,
	})
	if err != nil {
		t.Fatalf("start rtmp publish_relay 1 failed: %v", err)
	}

	var rtmpServer2 mediasource.RTMPManager
	defer rtmpServer2.Close()
	rtmpStatus2, err := rtmpServer2.Start(mediasource.RTMPConfig{
		Fault: "publish_relay",
		Port:  0,
	})
	if err != nil {
		t.Fatalf("start rtmp publish_relay 2 failed: %v", err)
	}

	fixturePath, err := filepath.Abs("../mediasource/fixtures/regular.flv")
	if err != nil {
		t.Fatalf("resolve fixture path: %v", err)
	}
	if _, err := os.Stat(fixturePath); err != nil {
		t.Skipf("fixture missing: %v", err)
	}

	var mgr Manager
	defer mgr.Close()

	// Task 1: Loop local recording file to RTMP
	t1, err := mgr.Start(Config{
		Name:       "录像循环推流",
		Protocol:   ProtocolRTMP,
		TargetURL:  rtmpStatus1.URL,
		SourceKind: SourceFile,
		FilePath:   fixturePath,
		Loop:       boolPtr(true),
		VideoMode:  "copy",
		AudioMode:  "copy",
	})
	if err != nil {
		t.Fatalf("start file loop rtmp push failed: %v", err)
	}

	// Task 2: Concurrent built-in pattern push to second RTMP server
	t2, err := mgr.Start(Config{
		Name:             "测试画面并发推流",
		Protocol:         ProtocolRTMP,
		TargetURL:        rtmpStatus2.URL,
		SourceKind:       SourcePattern,
		Loop:             boolPtr(true),
		Resolution:       "640x360",
		FPS:              15,
		VideoBitrateKbps: 500,
	})
	if err != nil {
		t.Fatalf("start pattern rtmp push failed: %v", err)
	}

	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		s1, _ := mgr.Get(t1.ID)
		s2, _ := mgr.Get(t2.ID)
		if s1.Status == StatusFailed {
			t.Fatalf("task 1 failed: %s (logs: %v)", s1.Error, s1.RecentLogs)
		}
		if s2.Status == StatusFailed {
			t.Fatalf("task 2 failed: %s (logs: %v)", s2.Error, s2.RecentLogs)
		}
		if s1.Status == StatusPushing && s1.BytesSent > 0 && s2.Status == StatusPushing && s2.BytesSent > 0 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	s1, _ := mgr.Get(t1.ID)
	s2, _ := mgr.Get(t2.ID)
	if s1.Status != StatusPushing || s1.BytesSent == 0 {
		t.Fatalf("task 1 did not enter pushing state: %+v", s1)
	}
	if s2.Status != StatusPushing || s2.BytesSent == 0 {
		t.Fatalf("task 2 did not enter pushing state: %+v", s2)
	}

	// Stop all and delete
	stopped := mgr.StopAll()
	if stopped != 2 {
		t.Fatalf("expected 2 stopped tasks, got %d", stopped)
	}
	if err := mgr.Delete(t1.ID); err != nil {
		t.Fatalf("delete task 1 failed: %v", err)
	}
	if err := mgr.Delete(t2.ID); err != nil {
		t.Fatalf("delete task 2 failed: %v", err)
	}
	if len(mgr.List()) != 0 {
		t.Fatalf("expected 0 tasks after delete, got %d", len(mgr.List()))
	}
}

func TestWHIPPatternPushAndTeardown(t *testing.T) {
	if _, err := findFFmpeg(); err != nil {
		t.Skipf("skipping real WHIP push test: %v", err)
	}

	var receivedRTP atomic.Int64
	var deleteCalled atomic.Bool
	var authHeaderOK atomic.Bool

	var serverPC *webrtc.PeerConnection
	defer func() {
		if serverPC != nil {
			_ = serverPC.Close()
		}
	}()

	whipSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer secret-tok" {
			authHeaderOK.Store(true)
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/whip/res/session-1" {
			deleteCalled.Store(true)
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		offerBytes, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		serverPC = pc
		pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			buf := make([]byte, 1600)
			for {
				n, _, err := remote.Read(buf)
				if err != nil {
					return
				}
				if n > 0 {
					receivedRTP.Add(1)
				}
			}
		})

		if err := pc.SetRemoteDescription(webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer,
			SDP:  string(offerBytes),
		}); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer, err := pc.CreateAnswer(nil)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		gather := webrtc.GatheringCompletePromise(pc)
		if err := pc.SetLocalDescription(answer); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		select {
		case <-gather:
		case <-time.After(3 * time.Second):
		}

		w.Header().Set("Content-Type", "application/sdp")
		w.Header().Set("Location", "/whip/res/session-1")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(pc.LocalDescription().SDP))
	}))
	defer whipSrv.Close()

	var mgr Manager
	defer mgr.Close()

	task, err := mgr.Start(Config{
		Protocol:         ProtocolWHIP,
		TargetURL:        whipSrv.URL + "/whip",
		BearerToken:      "secret-tok",
		SourceKind:       SourcePattern,
		Loop:             boolPtr(true),
		Resolution:       "640x360",
		FPS:              15,
		VideoBitrateKbps: 400,
		AudioMode:        "none",
	})
	if err != nil {
		t.Fatalf("start WHIP push failed: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		snap, _ := mgr.Get(task.ID)
		if snap.Status == StatusFailed {
			t.Fatalf("WHIP task failed: %s (logs: %v)", snap.Error, snap.RecentLogs)
		}
		if snap.Status == StatusPushing && snap.BytesSent > 0 && receivedRTP.Load() > 5 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	if receivedRTP.Load() == 0 {
		snap, _ := mgr.Get(task.ID)
		t.Fatalf("expected mock WHIP server to receive RTP packets, got 0 (snap: %+v)", snap)
	}
	if !authHeaderOK.Load() {
		t.Fatalf("expected Authorization Bearer header to be sent")
	}

	stopped, err := mgr.Stop(task.ID)
	if err != nil {
		t.Fatalf("stop WHIP task failed: %v", err)
	}
	if stopped.Status != StatusStopped {
		t.Fatalf("expected stopped status, got %s", stopped.Status)
	}
	if !deleteCalled.Load() {
		t.Fatalf("expected WHIP DELETE teardown to be called on stop")
	}
}

func TestStartUploadCleansTempFile(t *testing.T) {
	if _, err := findFFmpeg(); err != nil {
		t.Skipf("skipping upload test: %v", err)
	}
	fixtureBytes, err := os.ReadFile("../mediasource/fixtures/regular.flv")
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}

	var rtmpServer mediasource.RTMPManager
	defer rtmpServer.Close()
	rtmpStatus, err := rtmpServer.Start(mediasource.RTMPConfig{
		Fault: "publish_relay",
		Port:  0,
	})
	if err != nil {
		t.Fatalf("start rtmp publish_relay failed: %v", err)
	}

	var mgr Manager
	defer mgr.Close()

	snap, err := mgr.StartUpload(Config{
		Protocol:  ProtocolRTMP,
		TargetURL: rtmpStatus.URL,
		Loop:      boolPtr(true),
		VideoMode: "copy",
		AudioMode: "copy",
	}, "sample.flv", bytes.NewReader(fixtureBytes))
	if err != nil {
		t.Fatalf("StartUpload failed: %v", err)
	}

	mgr.mu.Lock()
	tmpPath := mgr.tasks[snap.ID].tempFile
	mgr.mu.Unlock()
	if tmpPath == "" {
		t.Fatalf("expected tempFile to be recorded")
	}
	if _, err := os.Stat(tmpPath); err != nil {
		t.Fatalf("expected tempFile to exist while running: %v", err)
	}

	if _, err := mgr.Stop(snap.ID); err != nil {
		t.Fatalf("stop upload task failed: %v", err)
	}
	if err := mgr.Delete(snap.ID); err != nil {
		t.Fatalf("delete upload task failed: %v", err)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("expected tempFile %s to be deleted after task removal", tmpPath)
	}
}

func TestProxyWHIPOfferAndDelete(t *testing.T) {
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost {
			w.Header().Set("Location", "/sessions/42")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("v=0\r\ns=answer\r\n"))
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/sessions/42" {
			deleted = true
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ans, resURL, code, err := ProxyWHIPOffer(context.Background(), srv.URL+"/whip", "my-token", "v=0\r\ns=offer\r\n")
	if err != nil {
		t.Fatalf("ProxyWHIPOffer failed: %v", err)
	}
	if code != http.StatusCreated || !strings.Contains(ans, "s=answer") {
		t.Fatalf("unexpected proxy offer result: code=%d ans=%q", code, ans)
	}
	if resURL != srv.URL+"/sessions/42" {
		t.Fatalf("expected resolved resource URL %s, got %s", srv.URL+"/sessions/42", resURL)
	}
	if err := ProxyWHIPDelete(context.Background(), resURL, "my-token"); err != nil {
		t.Fatalf("ProxyWHIPDelete failed: %v", err)
	}
	if !deleted {
		t.Fatalf("expected DELETE request to reach upstream WHIP server")
	}
}

func TestFriendlyFFmpegError(t *testing.T) {
	gotRTMP := friendlyFFmpegError(ProtocolRTMP, "Error opening output files: Operation not permitted", []string{
		"17:50:00 [rtmp @ 0x1] Server error: Publish failed.",
		"17:50:00 Error opening output files: Operation not permitted",
	})
	if !strings.Contains(gotRTMP, "publish_relay") {
		t.Fatalf("expected RTMP publish_relay guidance, got %q", gotRTMP)
	}

	gotRTSP403 := friendlyFFmpegError(ProtocolRTSP, "Error opening output files: Server returned 403 Forbidden", []string{
		"17:50:00 [rtsp @ 0x1] method ANNOUNCE failed: 403 Forbidden",
	})
	if !strings.Contains(gotRTSP403, "publish_relay") {
		t.Fatalf("expected RTSP 403 publish_relay guidance, got %q", gotRTSP403)
	}

	gotRTSP409 := friendlyFFmpegError(ProtocolRTSP, "409 Conflict", []string{
		"17:50:00 [rtsp @ 0x1] method ANNOUNCE failed: 409 Conflict",
	})
	if !strings.Contains(gotRTSP409, "占用") {
		t.Fatalf("expected RTSP 409 occupied guidance, got %q", gotRTSP409)
	}

	raw := friendlyFFmpegError(ProtocolRTMP, "Connection refused", []string{"Connection refused"})
	if raw != "Connection refused" {
		t.Fatalf("expected unmatched error to remain intact, got %q", raw)
	}
}
