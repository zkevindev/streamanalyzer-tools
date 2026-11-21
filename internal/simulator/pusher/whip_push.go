package pusher

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"streamanalyzer/internal/simulator/mediafile"
)

var whipHTTPClient = &http.Client{Timeout: 15 * time.Second}

func (t *pushTask) runWHIP(ctx context.Context, ffmpegPath string) {
	t.appendLog(fmt.Sprintf("正在向 WHIP 端点发起 SDP 协商：%s", redactTargetURL(t.cfg.TargetURL)))

	wantAudio := t.cfg.AudioMode != "none"
	if wantAudio && (t.cfg.SourceKind == SourceFile || t.cfg.SourceKind == SourceUpload) {
		if !probeHasAudio(ctx, t.cfg.FilePath) {
			wantAudio = false
			t.appendLog("源录像未检测到音轨，WHIP 仅推送视频轨")
		}
	}

	// Open local UDP sockets to receive RTP packets from FFmpeg.
	videoConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.finish(StatusFailed, fmt.Sprintf("创建本地视频 RTP 端口失败：%v", err))
		return
	}
	defer videoConn.Close()
	videoPort := videoConn.LocalAddr().(*net.UDPAddr).Port

	var audioConn *net.UDPConn
	var audioPort int
	if wantAudio {
		audioConn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		if err != nil {
			t.finish(StatusFailed, fmt.Sprintf("创建本地音频 RTP 端口失败：%v", err))
			return
		}
		defer audioConn.Close()
		audioPort = audioConn.LocalAddr().(*net.UDPAddr).Port
	}

	// Prepare Pion WebRTC PeerConnection with H.264 and Opus.
	var m webrtc.MediaEngine
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		t.finish(StatusFailed, fmt.Sprintf("注册 H.264 编解码失败：%v", err))
		return
	}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   48000,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		t.finish(StatusFailed, fmt.Sprintf("注册 Opus 编解码失败：%v", err))
		return
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(&m))
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.finish(StatusFailed, fmt.Sprintf("创建 WebRTC PeerConnection 失败：%v", err))
		return
	}
	defer pc.Close()

	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		},
		"video",
		"streamanalyzer-pusher",
	)
	if err != nil {
		t.finish(StatusFailed, fmt.Sprintf("创建视频轨道失败：%v", err))
		return
	}
	videoSender, err := pc.AddTrack(videoTrack)
	if err != nil {
		t.finish(StatusFailed, fmt.Sprintf("添加视频轨道失败：%v", err))
		return
	}
	drainRTCP(videoSender)

	var audioTrack *webrtc.TrackLocalStaticRTP
	if wantAudio {
		audioTrack, err = webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{
				MimeType:  webrtc.MimeTypeOpus,
				ClockRate: 48000,
				Channels:  2,
			},
			"audio",
			"streamanalyzer-pusher",
		)
		if err != nil {
			t.finish(StatusFailed, fmt.Sprintf("创建音频轨道失败：%v", err))
			return
		}
		audioSender, err := pc.AddTrack(audioTrack)
		if err != nil {
			t.finish(StatusFailed, fmt.Sprintf("添加音频轨道失败：%v", err))
			return
		}
		drainRTCP(audioSender)
	}

	iceFailedCh := make(chan struct{}, 1)
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		t.mu.Lock()
		t.iceState = state.String()
		t.mu.Unlock()
		t.appendLog("ICE 状态：" + state.String())
		if state == webrtc.ICEConnectionStateFailed {
			select {
			case iceFailedCh <- struct{}{}:
			default:
			}
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.finish(StatusFailed, fmt.Sprintf("生成 SDP Offer 失败：%v", err))
		return
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.finish(StatusFailed, fmt.Sprintf("设置本地 SDP 失败：%v", err))
		return
	}

	select {
	case <-gatherComplete:
	case <-time.After(4 * time.Second):
		t.appendLog("ICE 候选收集达到 4 秒超时，使用当前已收集候选继续协商")
	case <-ctx.Done():
		t.finish(StatusStopped, "")
		return
	}

	localSDP := pc.LocalDescription()
	if localSDP == nil {
		t.finish(StatusFailed, "未能获取本地 SDP")
		return
	}

	// RFC 9725 WHIP HTTP POST
	answerSDP, resourceURL, statusCode, err := sendWHIPOffer(ctx, t.cfg.TargetURL, t.cfg.BearerToken, localSDP.SDP)
	if err != nil {
		t.finish(StatusFailed, fmt.Sprintf("WHIP 信令请求失败：%v", err))
		return
	}
	if statusCode < 200 || statusCode >= 300 {
		t.finish(StatusFailed, fmt.Sprintf("WHIP 服务端返回 HTTP %d：%s", statusCode, truncateText(answerSDP, 200)))
		return
	}
	if resourceURL != "" {
		defer func() {
			delCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			_ = sendWHIPDelete(delCtx, resourceURL, t.cfg.BearerToken)
		}()
	}

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		t.finish(StatusFailed, fmt.Sprintf("解析远端 SDP Answer 失败：%v", err))
		return
	}
	t.appendLog("WHIP SDP 协商成功，开始推送 RTP 媒体流")

	// Start RTP forwarding goroutines + stats ticker.
	var totalBytes atomic.Int64
	var totalFrames atomic.Int64

	fwdCtx, cancelFwd := context.WithCancel(ctx)
	defer cancelFwd()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		forwardUDPToTrack(fwdCtx, videoConn, videoTrack, true, &totalBytes, &totalFrames)
	}()

	if wantAudio && audioConn != nil && audioTrack != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			forwardUDPToTrack(fwdCtx, audioConn, audioTrack, false, &totalBytes, nil)
		}()
	}

	// Stats updater
	statsDone := make(chan struct{})
	go func() {
		defer close(statsDone)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		var prevBytes int64
		var prevFrames int64
		prevTime := time.Now()
		for {
			select {
			case <-fwdCtx.Done():
				return
			case now := <-ticker.C:
				b := totalBytes.Load()
				f := totalFrames.Load()
				dt := now.Sub(prevTime).Seconds()
				if dt <= 0 {
					dt = 1
				}
				kbps := float64(b-prevBytes) * 8 / 1000 / dt
				fps := float64(f-prevFrames) / dt
				prevBytes = b
				prevFrames = f
				prevTime = now

				t.mu.Lock()
				if b > 0 && t.status == StatusStarting {
					t.status = StatusPushing
				}
				t.bytesSent = b
				t.framesSent = f
				t.fps = fps
				t.bitrateKbps = kbps
				t.outTimeMS = time.Since(t.startedAt).Milliseconds()
				t.mu.Unlock()
			}
		}
	}()

	// Launch FFmpeg producing RTP to local UDP sockets.
	args := buildFFmpegWHIPArgs(t.cfg, videoPort, audioPort, wantAudio)
	cmd := exec.CommandContext(fwdCtx, ffmpegPath, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancelFwd()
		videoConn.Close()
		if audioConn != nil {
			audioConn.Close()
		}
		wg.Wait()
		<-statsDone
		t.finish(StatusFailed, fmt.Sprintf("创建 ffmpeg stderr 管道失败：%v", err))
		return
	}
	if err := cmd.Start(); err != nil {
		cancelFwd()
		videoConn.Close()
		if audioConn != nil {
			audioConn.Close()
		}
		wg.Wait()
		<-statsDone
		t.finish(StatusFailed, fmt.Sprintf("启动 ffmpeg 失败：%v", err))
		return
	}

	errDone := make(chan struct{})
	go func() {
		defer close(errDone)
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 16*1024), 256*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				t.appendLog(line)
			}
		}
	}()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	var iceFailed bool
	select {
	case waitErr = <-waitCh:
	case <-iceFailedCh:
		iceFailed = true
		cancelFwd()
		waitErr = <-waitCh
	case <-ctx.Done():
		cancelFwd()
		waitErr = <-waitCh
	}

	cancelFwd()
	_ = videoConn.Close()
	if audioConn != nil {
		_ = audioConn.Close()
	}
	wg.Wait()
	<-statsDone
	<-errDone

	if t.isStopRequested() || errors.Is(ctx.Err(), context.Canceled) {
		t.finish(StatusStopped, "")
		return
	}
	if iceFailed {
		t.finish(StatusFailed, "WebRTC ICE 连接失败")
		return
	}
	if waitErr != nil {
		reason := t.lastErrorLog()
		if reason == "" {
			reason = waitErr.Error()
		}
		t.finish(StatusFailed, reason)
		return
	}
	t.finish(StatusStopped, "")
}

func buildFFmpegWHIPArgs(cfg Config, videoPort, audioPort int, wantAudio bool) []string {
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-nostats",
	}

	switch cfg.SourceKind {
	case SourceFile, SourceUpload:
		args = append(args, "-re", "-fflags", "+genpts")
		if cfg.Loop != nil && *cfg.Loop {
			if cfg.LoopCount <= 0 {
				args = append(args, "-stream_loop", "-1")
			} else if cfg.LoopCount > 1 {
				args = append(args, "-stream_loop", strconv.Itoa(cfg.LoopCount-1))
			}
		}
		args = append(args, "-i", cfg.FilePath)

	case SourcePattern:
		args = append(args, "-re", "-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%s:rate=%d", cfg.Resolution, cfg.FPS))
		if wantAudio {
			args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000")
		}
		if cfg.Loop == nil || !*cfg.Loop {
			args = append(args, "-t", "30")
		} else if cfg.LoopCount > 0 {
			args = append(args, "-t", strconv.Itoa(cfg.LoopCount*30))
		}
	}

	// Output 1: Video RTP (H.264 Baseline without B-frames for WebRTC compatibility)
	args = append(args, "-map", "0:v:0", "-an")
	if cfg.VideoMode == "copy" && (cfg.SourceKind == SourceFile || cfg.SourceKind == SourceUpload) {
		args = append(args, "-c:v", "copy")
	} else {
		gop := cfg.FPS * 2
		if gop < 25 {
			gop = 25
		}
		bitrate := fmt.Sprintf("%dk", cfg.VideoBitrateKbps)
		bufsize := fmt.Sprintf("%dk", cfg.VideoBitrateKbps*2)
		args = append(args,
			"-c:v", "libx264",
			"-profile:v", "baseline",
			"-level", "3.1",
			"-preset", "veryfast",
			"-tune", "zerolatency",
			"-pix_fmt", "yuv420p",
			"-bf", "0",
			"-g", strconv.Itoa(gop),
			"-keyint_min", strconv.Itoa(gop),
			"-sc_threshold", "0",
			"-b:v", bitrate,
			"-maxrate", bitrate,
			"-bufsize", bufsize,
		)
	}
	args = append(args, "-payload_type", "96", "-f", "rtp", fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1200", videoPort))

	// Output 2: Audio RTP (Opus 48kHz stereo)
	if wantAudio {
		if cfg.SourceKind == SourcePattern {
			args = append(args, "-map", "1:a:0")
		} else {
			args = append(args, "-map", "0:a:0")
		}
		args = append(args,
			"-vn",
			"-c:a", "libopus",
			"-ar", "48000",
			"-ac", "2",
			"-b:a", "64k",
			"-application", "lowdelay",
			"-payload_type", "111",
			"-f", "rtp",
			fmt.Sprintf("rtp://127.0.0.1:%d", audioPort),
		)
	}

	return args
}

func forwardUDPToTrack(ctx context.Context, conn *net.UDPConn, track *webrtc.TrackLocalStaticRTP, countFrames bool, totalBytes, totalFrames *atomic.Int64) {
	buf := make([]byte, 1600)
	var pkt rtp.Packet
	for {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if ctx.Err() != nil {
					return
				}
				continue
			}
			return
		}
		if n < 12 {
			continue
		}
		if _, err := track.Write(buf[:n]); err != nil {
			if errors.Is(err, io.ErrClosedPipe) {
				return
			}
			continue
		}
		totalBytes.Add(int64(n))
		if countFrames && totalFrames != nil {
			if err := pkt.Unmarshal(buf[:n]); err == nil && pkt.Marker {
				totalFrames.Add(1)
			}
		}
	}
}

func drainRTCP(sender *webrtc.RTPSender) {
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(buf); err != nil {
				return
			}
		}
	}()
}

func probeHasAudio(ctx context.Context, filePath string) bool {
	ffprobePath, err := mediafile.FindTool("ffprobe", "")
	if err != nil {
		// If ffprobe is unavailable, conservatively assume no audio check failed and skip audio only if probe says empty.
		return true
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, ffprobePath,
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_type",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	).Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) != ""
}

func sendWHIPOffer(ctx context.Context, targetURL, bearerToken, offerSDP string) (answerSDP string, resourceURL string, statusCode int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewBufferString(offerSDP))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/sdp")
	req.Header.Set("Accept", "application/sdp")
	if tok := strings.TrimSpace(bearerToken); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := whipHTTPClient.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", resp.StatusCode, err
	}

	if loc := strings.TrimSpace(resp.Header.Get("Location")); loc != "" {
		if base, err := url.Parse(targetURL); err == nil {
			if ref, err := url.Parse(loc); err == nil {
				resourceURL = base.ResolveReference(ref).String()
			}
		}
		if resourceURL == "" {
			resourceURL = loc
		}
	}
	return string(body), resourceURL, resp.StatusCode, nil
}

func sendWHIPDelete(ctx context.Context, resourceURL, bearerToken string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, resourceURL, nil)
	if err != nil {
		return err
	}
	if tok := strings.TrimSpace(bearerToken); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := whipHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return nil
}

// ProxyWHIPOffer forwards a browser-generated SDP offer to a WHIP endpoint on behalf of the local UI
// to avoid browser CORS restrictions when publishing webcam/screen streams directly via WebRTC.
func ProxyWHIPOffer(ctx context.Context, targetURL, bearerToken, offerSDP string) (answerSDP string, resourceURL string, statusCode int, err error) {
	u, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", "", 0, fmt.Errorf("%w：WHIP 地址必须为 http:// 或 https://", ErrInvalid)
	}
	if strings.TrimSpace(offerSDP) == "" {
		return "", "", 0, fmt.Errorf("%w：SDP Offer 不能为空", ErrInvalid)
	}
	return sendWHIPOffer(ctx, u.String(), bearerToken, offerSDP)
}

// ProxyWHIPDelete forwards a WHIP resource teardown DELETE request.
func ProxyWHIPDelete(ctx context.Context, resourceURL, bearerToken string) error {
	u, err := url.Parse(strings.TrimSpace(resourceURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w：WHIP 资源地址必须为 http:// 或 https://", ErrInvalid)
	}
	return sendWHIPDelete(ctx, u.String(), bearerToken)
}

func truncateText(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
