package diagnostic

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/onvif"

	"github.com/bluenviron/gortsplib/v5/pkg/auth"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/conn"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
	"github.com/pion/rtp"
)

// These integration fixtures write original RTSP/interleaved bytes rather than
// re-marshalling parsed packets. In particular, nonzero RTP padding proves that
// the downloadable evidence retains bytes that a normal packet Marshal loses.
type rtspIntegrationFixture struct {
	listener   net.Listener
	mu         sync.Mutex
	wire       bytes.Buffer
	clients    map[net.Conn]struct{}
	wg         sync.WaitGroup
	config     rtspIntegrationConfig
	challenges atomic.Uint64
	authorized atomic.Uint64
	plays      atomic.Uint64
}

type rtspIntegrationConfig struct {
	sdp            string
	emptySDP       bool
	optionsStatus  base.StatusCode
	describeStatus base.StatusCode
	redirect       string
	setupStatus    base.StatusCode
	playStatus     base.StatusCode
	username       string
	password       string
	packets        []rtspIntegrationPacket
	closeAfterPlay bool
	tlsConfig      *tls.Config
}

type rtspIntegrationPacket struct {
	track int
	raw   []byte
	delay time.Duration
	rtcp  bool
}

const rtspIntegrationSDP = "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=private-sdp-fixture-title\r\nc=IN IP4 0.0.0.0\r\nt=0 0\r\na=control:*\r\n"

func rtspIntegrationVideoSDP() string {
	return rtspIntegrationSDP + "m=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=fmtp:96 packetization-mode=1\r\na=control:trackID=0\r\n"
}

func rtspIntegrationMulticodecSDP() string {
	return rtspIntegrationVideoSDP() +
		"m=video 0 RTP/AVP 96\r\na=rtpmap:96 H265/90000\r\na=control:trackID=1\r\n" +
		"m=audio 0 RTP/AVP 97\r\na=rtpmap:97 MPEG4-GENERIC/48000/2\r\na=fmtp:97 streamtype=5;profile-level-id=1;mode=AAC-hbr;config=1190;sizelength=13;indexlength=3;indexdeltalength=3\r\na=control:trackID=2\r\n" +
		"m=audio 0 RTP/AVP 98\r\na=rtpmap:98 opus/48000/2\r\na=fmtp:98 sprop-stereo=1\r\na=control:trackID=3\r\n" +
		"m=audio 0 RTP/AVP 0\r\na=control:trackID=4\r\n" +
		"m=audio 0 RTP/AVP 8\r\na=control:trackID=5\r\n"
}

func rtspIntegrationRTP(pt byte, sequence uint16, timestamp, ssrc uint32, payload []byte, unusualPadding bool) []byte {
	headerSize := 12
	if unusualPadding {
		headerSize += 12 // one CSRC, an extension header and a four-byte extension
	}
	padding := 0
	if unusualPadding {
		padding = 4
	}
	raw := make([]byte, headerSize+len(payload)+padding)
	raw[0], raw[1] = 0x80, 0x80|pt
	binary.BigEndian.PutUint16(raw[2:], sequence)
	binary.BigEndian.PutUint32(raw[4:], timestamp)
	binary.BigEndian.PutUint32(raw[8:], ssrc)
	if unusualPadding {
		raw[0] = 0xb1
		copy(raw[12:], []byte{1, 3, 5, 7, 0xbe, 0xde, 0, 1, 0x10, 0x42, 0, 0})
		copy(raw[len(raw)-4:], []byte{0xa1, 0xb2, 0xc3, 4})
	}
	copy(raw[headerSize:], payload)
	return raw
}

func newRTSPIntegrationFixture(t *testing.T, config rtspIntegrationConfig) *rtspIntegrationFixture {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if config.tlsConfig != nil {
		listener = tls.NewListener(listener, config.tlsConfig)
	}
	if config.sdp == "" && !config.emptySDP {
		config.sdp = rtspIntegrationVideoSDP()
	}
	f := &rtspIntegrationFixture{listener: listener, config: config, clients: make(map[net.Conn]struct{})}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			nc, err := listener.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.clients[nc] = struct{}{}
			f.mu.Unlock()
			f.wg.Add(1)
			go func() {
				defer f.wg.Done()
				defer nc.Close()
				defer func() {
					f.mu.Lock()
					delete(f.clients, nc)
					f.mu.Unlock()
				}()
				f.serve(nc)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		f.mu.Lock()
		for nc := range f.clients {
			_ = nc.Close()
		}
		f.mu.Unlock()
		f.wg.Wait()
	})
	return f
}

func (f *rtspIntegrationFixture) address() string {
	scheme := "rtsp"
	if f.config.tlsConfig != nil {
		scheme = "rtsps"
	}
	return scheme + "://" + f.listener.Addr().String() + "/camera"
}

func (f *rtspIntegrationFixture) recorded() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return bytes.Clone(f.wire.Bytes())
}

func (f *rtspIntegrationFixture) write(nc net.Conn, raw []byte) bool {
	_ = nc.SetWriteDeadline(time.Now().Add(3 * time.Second))
	for len(raw) != 0 {
		n, err := nc.Write(raw)
		if n > 0 {
			f.mu.Lock()
			f.wire.Write(raw[:n])
			f.mu.Unlock()
			raw = raw[n:]
		}
		if err != nil {
			return false
		}
	}
	return true
}

func (f *rtspIntegrationFixture) serve(nc net.Conn) {
	reader := conn.NewConn(bufio.NewReader(nc), nc)
	channels := make(map[int]byte)
	for {
		_ = nc.SetReadDeadline(time.Now().Add(5 * time.Second))
		item, err := reader.Read()
		if err != nil {
			return
		}
		req, ok := item.(*base.Request)
		if !ok {
			continue
		}
		if req.Method == base.Teardown {
			return // do not append a post-capture response to the expected input
		}
		res := &base.Response{StatusCode: base.StatusOK, Header: base.Header{"CSeq": req.Header["CSeq"]}}
		if f.config.username != "" && req.Method != base.Options {
			if auth.Verify(req, f.config.username, f.config.password, []auth.VerifyMethod{auth.VerifyMethodDigestMD5}, "fixture-realm", "fixture-nonce") != nil {
				f.challenges.Add(1)
				res.StatusCode = base.StatusUnauthorized
				res.Header["WWW-Authenticate"] = auth.GenerateWWWAuthenticate([]auth.VerifyMethod{auth.VerifyMethodDigestMD5}, "fixture-realm", "fixture-nonce")
			} else {
				f.authorized.Add(1)
			}
		}
		if res.StatusCode == base.StatusOK {
			switch req.Method {
			case base.Options:
				res.Header["Public"] = base.HeaderValue{"DESCRIBE, SETUP, PLAY, TEARDOWN, GET_PARAMETER"}
				if f.config.optionsStatus != 0 {
					res.StatusCode = f.config.optionsStatus
				}
			case base.Describe:
				if f.config.describeStatus != 0 {
					res.StatusCode = f.config.describeStatus
					if f.config.redirect != "" {
						res.Header["Location"] = base.HeaderValue{f.config.redirect}
					}
				} else {
					res.Header["Content-Type"] = base.HeaderValue{"application/sdp"}
					res.Header["Content-Base"] = base.HeaderValue{f.address() + "/"}
					res.Body = []byte(f.config.sdp)
				}
			case base.Setup:
				if f.config.setupStatus != 0 {
					res.StatusCode = f.config.setupStatus
				} else {
					var transport headers.Transport
					if transport.Unmarshal(req.Header["Transport"]) != nil || transport.InterleavedIDs == nil {
						return
					}
					_, id, _ := strings.Cut(req.URL.Path, "trackID=")
					track, _ := strconv.Atoi(id)
					channels[track] = byte(transport.InterleavedIDs[0])
					res.Header["Transport"] = transport.Marshal()
					res.Header["Session"] = base.HeaderValue{"test-session;timeout=60"}
				}
			case base.Play:
				if f.config.playStatus != 0 {
					res.StatusCode = f.config.playStatus
				} else {
					res.Header["Session"] = base.HeaderValue{"test-session;timeout=60"}
				}
			}
		}
		raw, _ := res.Marshal()
		if !f.write(nc, raw) {
			return
		}
		if req.Method == base.Play && res.StatusCode == base.StatusOK {
			f.plays.Add(1)
			for _, packet := range f.config.packets {
				if packet.delay > 0 {
					time.Sleep(packet.delay)
				}
				frame := make([]byte, 4+len(packet.raw))
				frame[0], frame[1] = '$', channels[packet.track]
				if packet.rtcp {
					frame[1]++
				}
				binary.BigEndian.PutUint16(frame[2:], uint16(len(packet.raw)))
				copy(frame[4:], packet.raw)
				if !f.write(nc, frame) {
					return
				}
			}
			if f.config.closeAfterPlay {
				return
			}
		}
	}
}

func rtspIntegrationAwait(t *testing.T, service *Service, id string) Detail {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		detail, err := service.Detail(id)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Report != nil {
			return detail
		}
		if detail.Run.Status == "failed" {
			t.Fatalf("RTSP capture failed to persist: %+v", detail.Run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("RTSP capture did not finish")
	return Detail{}
}

func rtspIntegrationOpen(t *testing.T) *Service {
	t.Helper()
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func rtspIntegrationCapture(t *testing.T, service *Service, uri, protocol string, seconds int) Detail {
	t.Helper()
	run, err := service.Start(SampleRequest{URL: uri, Protocol: protocol, Seconds: seconds})
	if err != nil {
		t.Fatal(err)
	}
	return rtspIntegrationAwait(t, service, run.ID)
}

func rtspIntegrationZIP(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte)
	for _, item := range zr.File {
		r, err := item.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[item.Name] = data
	}
	return files
}

func rtspIntegrationRoundTrip(t *testing.T, service *Service, detail Detail) map[string][]byte {
	t.Helper()
	dump, err := service.OpenDump(detail.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(dump)
	_ = dump.Close()
	if err != nil {
		t.Fatal(err)
	}
	var bundle bytes.Buffer
	if err := service.WriteBundle(detail.Run.ID, &bundle); err != nil {
		t.Fatal(err)
	}
	imported, err := service.Import(bytes.NewReader(bundle.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.Detail(imported.ID)
	if err != nil || !reflect.DeepEqual(replayed.Report, detail.Report) {
		t.Fatalf("RTSP report changed after exact offline replay: error=%v\nbefore=%+v\nafter=%+v", err, detail.Report, replayed.Report)
	}
	got, err := os.ReadFile(filepath.Join(service.runDir(imported.ID), "stream.rtsp.zip"))
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("RTSP artifact changed after export/import: %v", err)
	}
	return rtspIntegrationZIP(t, raw)
}

func TestRTSPIntegrationMulticodecOriginalWireRoundTrip(t *testing.T) {
	payloads := [][]byte{{0x65, 0x88, 0x84}, {0x26, 0x01, 0x88}, {0, 0x10, 0, 0x18, 0x21, 0x10, 0x56}, {0xf8, 0xff, 0xfe}, bytes.Repeat([]byte{0xff}, 160), bytes.Repeat([]byte{0xd5}, 160)}
	pts := []byte{96, 96, 97, 98, 0, 8}
	packets := make([]rtspIntegrationPacket, len(payloads))
	for i, payload := range payloads {
		packets[i] = rtspIntegrationPacket{track: i, raw: rtspIntegrationRTP(pts[i], 65535, 0, uint32(100+i), payload, i == 0)}
	}
	// A valid receiver report shares track zero's RTCP channel; it must not be
	// mistaken for another video packet or omitted from original wire evidence.
	packets = append(packets, rtspIntegrationPacket{track: 0, rtcp: true, raw: []byte{0x80, 201, 0, 1, 0, 0, 0, 100}})
	f := newRTSPIntegrationFixture(t, rtspIntegrationConfig{sdp: rtspIntegrationMulticodecSDP(), username: "fixture-user", password: "fixture-password-secret", packets: packets, closeAfterPlay: true})
	service := rtspIntegrationOpen(t)
	u, _ := url.Parse(f.address())
	u.User = url.UserPassword("fixture-user", "fixture-password-secret")
	u.RawQuery = "token=fixture-query-secret"
	detail := rtspIntegrationCapture(t, service, u.String(), "rtsp", 2)
	if f.challenges.Load() != 1 || f.authorized.Load() < 8 {
		t.Fatalf("Digest retry/setup did not authenticate all tracks: challenges=%d authorized=%d", f.challenges.Load(), f.authorized.Load())
	}
	rtsp := detail.Report.RTSP
	if rtsp == nil || !rtsp.SDPAvailable || rtsp.Secure || len(rtsp.Tracks) != 6 || rtsp.RTPPackets != 6 || rtsp.RTCPPackets != 1 {
		t.Fatalf("native RTSP multicodec packet summary is incomplete: %+v", rtsp)
	}
	if rtsp.FirstVideoRTPMS == nil || rtsp.FirstAudioRTPMS == nil || detail.Report.Metrics.MediaTimingUnit != "rtp" || detail.Report.Metrics.VideoFrames != 0 || detail.Report.Metrics.AudioFrames != 0 {
		t.Fatalf("RTP observations were omitted or presented as decoded media frames: %+v", detail.Report)
	}
	var expectedBytes int64
	for _, packet := range packets {
		if !packet.rtcp {
			expectedBytes += int64(len(packet.raw))
		}
	}
	if rtsp.RTPBytes != expectedBytes {
		t.Fatalf("RTP bytes include RTCP or omit original headers/padding: got=%d want=%d", rtsp.RTPBytes, expectedBytes)
	}
	for i, want := range []struct {
		kind, codec     string
		clock, channels int
	}{{"video", "h264", 90000, 0}, {"video", "h265", 90000, 0}, {"audio", "aac", 48000, 2}, {"audio", "opus", 48000, 2}, {"audio", "pcmu", 8000, 1}, {"audio", "pcma", 8000, 1}} {
		track := rtsp.Tracks[i]
		if track.Kind != want.kind || track.Codec != want.codec || track.ClockRate != want.clock || track.Channels != want.channels || !track.Received || track.RTPPackets != 1 || track.FirstRTPMS == nil {
			t.Fatalf("track %d uses an incorrect declaration or omitted actual RTP: got=%+v want=%+v", i, track, want)
		}
	}
	files := rtspIntegrationRoundTrip(t, service, detail)
	foundWire := false
	for name, data := range files {
		if bytes.Equal(data, f.recorded()) {
			foundWire = true
		}
		if name == "session.sdp" && string(data) != f.config.sdp {
			t.Fatal("retained SDP differs from original DESCRIBE body")
		}
	}
	if !foundWire || !bytes.Equal(files["session.sdp"], []byte(f.config.sdp)) {
		t.Fatalf("dump lacks original application wire or exact SDP; entries=%v", reflect.ValueOf(files).MapKeys())
	}
	publicReport, _ := json.Marshal(detail.Report)
	for _, private := range []string{"fixture-user", "fixture-password-secret", "fixture-query-secret", "private-sdp-fixture-title", "fixture-nonce"} {
		if bytes.Contains(publicReport, []byte(private)) || bytes.Contains(files["session.json"], []byte(private)) {
			t.Fatalf("public report or numeric archive metadata exposed private field %q", private)
		}
	}
	input, err := modelInput(*detail.Report)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"fixture-user", "fixture-password-secret", "fixture-query-secret", "private-sdp-fixture-title", "fixture-nonce", "rtsp://", "a=rtpmap", "Authorization"} {
		if bytes.Contains(input, []byte(private)) {
			t.Fatalf("model input exposed RTSP private field %q: %s", private, input)
		}
	}
	var summary map[string]any
	if err := json.Unmarshal(input, &summary); err != nil {
		t.Fatal(err)
	}
	if summary["rtsp"] == nil {
		t.Fatalf("model input lacks bounded RTSP numeric summary: %s", input)
	}
}

func TestRTSPIntegrationONVIFDigest(t *testing.T) {
	manager := &onvif.Manager{}
	t.Cleanup(manager.Close)
	status, err := manager.Start(onvif.Config{BindIP: "127.0.0.1", RTSPUsername: "real-camera", RTSPPassword: "real-digest-secret"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(status.StreamURL)
	u.User = url.UserPassword("real-camera", "real-digest-secret")
	service := rtspIntegrationOpen(t)
	run, err := service.Start(SampleRequest{URL: u.String(), Protocol: "rtsp", Seconds: 1, Playback: true})
	if err != nil {
		t.Fatal(err)
	}
	if run.Player != nil {
		t.Fatal("native RTSP capture created an unsupported browser player session")
	}
	detail := rtspIntegrationAwait(t, service, run.ID)
	if detail.Report.Player != nil {
		t.Fatal("native RTSP capture fabricated browser playback metrics")
	}
	if manager.Status().RTSPPlays != 1 || detail.Report.Manifest.Observation.EndReason != "duration_limit" {
		t.Fatalf("native capture did not PLAY the real ONVIF stream: %+v", detail.Report.Manifest.Observation)
	}
	if detail.Report.RTSP == nil || detail.Report.RTSP.RTPPackets == 0 || len(detail.Report.RTSP.Tracks) != 1 || detail.Report.RTSP.Tracks[0].Codec != "h264" || !detail.Report.RTSP.Tracks[0].Received {
		t.Fatalf("real ONVIF H264 RTP did not reach report: %+v", detail.Report.RTSP)
	}
	rtspIntegrationRoundTrip(t, service, detail)
}

func TestRTSPIntegrationStatusAndTerminalStates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config rtspIntegrationConfig
		stage  string
		end    string
	}{
		{name: "options403", config: rtspIntegrationConfig{optionsStatus: base.StatusForbidden}, stage: "rtsp_options"},
		{name: "describe404", config: rtspIntegrationConfig{describeStatus: base.StatusNotFound}, stage: "rtsp_describe"},
		{name: "setup461", config: rtspIntegrationConfig{setupStatus: base.StatusUnsupportedTransport}, stage: "rtsp_setup"},
		{name: "play454", config: rtspIntegrationConfig{playStatus: base.StatusSessionNotFound}, stage: "rtsp_play"},
		{name: "playEOF", config: rtspIntegrationConfig{closeAfterPlay: true}, end: "eof"},
		{name: "silentPLAY", config: rtspIntegrationConfig{}, end: "duration_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRTSPIntegrationFixture(t, tc.config)
			service := rtspIntegrationOpen(t)
			detail := rtspIntegrationCapture(t, service, f.address(), "rtsp", 1)
			observation := detail.Report.Manifest.Observation
			if tc.stage != "" && observation.FailureStage != tc.stage {
				t.Fatalf("wrong RTSP failure stage: got=%+v want=%s", observation, tc.stage)
			}
			if tc.end != "" && observation.EndReason != tc.end {
				t.Fatalf("wrong RTSP terminal state: got=%+v want=%s", observation, tc.end)
			}
			rtspIntegrationRoundTrip(t, service, detail)
		})
	}
}

func TestRTSPIntegrationDigestRejectsWrongPassword(t *testing.T) {
	f := newRTSPIntegrationFixture(t, rtspIntegrationConfig{username: "private-camera", password: "correct-private-password"})
	u, _ := url.Parse(f.address())
	u.User = url.UserPassword("private-camera", "wrong-private-password")
	service := rtspIntegrationOpen(t)
	detail := rtspIntegrationCapture(t, service, u.String(), "rtsp", 1)
	if f.challenges.Load() != 2 || f.authorized.Load() != 0 || detail.Report.Manifest.Observation.FailureStage != "rtsp_describe" {
		t.Fatalf("Digest authentication did not stop after rejected credentials: challenges=%d authorized=%d observation=%+v", f.challenges.Load(), f.authorized.Load(), detail.Report.Manifest.Observation)
	}
	if detail.Report.RTSP == nil || detail.Report.RTSP.RTPPackets != 0 || detail.Report.RTSP.SDPAvailable {
		t.Fatalf("failed authentication invented an SDP or RTP stream: %+v", detail.Report.RTSP)
	}
	rtspIntegrationRoundTrip(t, service, detail)
}

func TestRTSPIntegrationEmptySDP(t *testing.T) {
	f := newRTSPIntegrationFixture(t, rtspIntegrationConfig{emptySDP: true})
	service := rtspIntegrationOpen(t)
	detail := rtspIntegrationCapture(t, service, f.address(), "rtsp", 1)
	observation := detail.Report.Manifest.Observation
	if !detail.Run.ReportReady || detail.Run.Status != "failed" || observation.FailureStage != "rtsp_describe" || observation.FailureCode != "rtsp_parse_error" {
		t.Fatalf("empty DESCRIBE body did not persist a specific failed capture report: run=%+v observation=%+v", detail.Run, observation)
	}
	if detail.Report.RTSP == nil || detail.Report.RTSP.SDPAvailable || len(detail.Report.RTSP.Tracks) != 0 || detail.Report.RTSP.RTPPackets != 0 {
		t.Fatalf("empty SDP invented declared tracks or received RTP: %+v", detail.Report.RTSP)
	}
	files := rtspIntegrationRoundTrip(t, service, detail)
	if len(files["session.sdp"]) != 0 || !bytes.Equal(files["server-wire.bin"], f.recorded()) {
		t.Fatal("empty SDP capture failed to retain exact response bytes for offline replay")
	}
}

func TestRTSPIntegrationRedirectDoesNotForwardCredentials(t *testing.T) {
	trap, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer trap.Close()
	f := newRTSPIntegrationFixture(t, rtspIntegrationConfig{describeStatus: base.StatusMovedPermanently, redirect: "rtsp://" + trap.Addr().String() + "/redirected"})
	u, _ := url.Parse(f.address())
	u.User = url.UserPassword("private-camera", "private-password")
	service := rtspIntegrationOpen(t)
	detail := rtspIntegrationCapture(t, service, u.String(), "rtsp", 1)
	if detail.Report.Manifest.Observation.FailureStage != "rtsp_describe" || detail.Report.RTSP == nil || detail.Report.RTSP.Connections != 1 {
		t.Fatalf("redirect did not stop at the original server: %+v", detail.Report)
	}
	_ = trap.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Millisecond))
	if connection, err := trap.Accept(); err == nil {
		connection.Close()
		t.Fatal("RTSP client followed a redirect that could forward camera credentials")
	}
	rtspIntegrationRoundTrip(t, service, detail)
}

func TestRTSPIntegrationStopSilentPLAY(t *testing.T) {
	f := newRTSPIntegrationFixture(t, rtspIntegrationConfig{})
	service := rtspIntegrationOpen(t)
	run, err := service.Start(SampleRequest{URL: f.address(), Protocol: "rtsp", Seconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for f.plays.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.plays.Load() != 1 {
		t.Fatal("capture never reached silent PLAY")
	}
	stoppedAt := time.Now()
	if _, err := service.Stop(run.ID); err != nil {
		t.Fatal(err)
	}
	detail := rtspIntegrationAwait(t, service, run.ID)
	if time.Since(stoppedAt) > time.Second || detail.Report.Manifest.Observation.EndReason != "canceled" || detail.Report.RTSP == nil || detail.Report.RTSP.RTPPackets != 0 {
		t.Fatalf("native RTSP stop did not interrupt silent receive: %+v", detail.Report.Manifest.Observation)
	}
	rtspIntegrationRoundTrip(t, service, detail)
}

func TestRTSPIntegrationSequenceWrapReorderAndSSRC(t *testing.T) {
	var packets []rtspIntegrationPacket
	for i, seq := range []uint16{65534, 65535, 0, 2, 1, 2, 4} {
		packets = append(packets, rtspIntegrationPacket{raw: rtspIntegrationRTP(96, seq, uint32(i*3600), 0x12345678, []byte{0x61, 0x88, 0x84}, false)})
	}
	packets = append(packets, rtspIntegrationPacket{raw: rtspIntegrationRTP(96, 100, 0, 0x87654321, []byte{0x65, 0x88, 0x84}, false)})
	f := newRTSPIntegrationFixture(t, rtspIntegrationConfig{packets: packets, closeAfterPlay: true})
	service := rtspIntegrationOpen(t)
	detail := rtspIntegrationCapture(t, service, f.address(), "rtsp", 1)
	rtspIntegrationRoundTrip(t, service, detail)
	if detail.Report.RTSP == nil || len(detail.Report.RTSP.Tracks) != 1 {
		t.Fatalf("missing sequence track: %+v", detail.Report.RTSP)
	}
	track := detail.Report.RTSP.Tracks[0]
	if track.RTPPackets != 8 || track.SequenceGaps != 2 || track.MissingSequencePackets != 1 || track.DuplicatePackets != 1 || track.OutOfOrderPackets != 1 || track.SSRCChanges != 1 || track.SequenceResets != 0 || track.TimestampRegressions != 0 {
		t.Fatalf("wrap, backfill, duplicate or SSRC handling is incorrect: %+v", track)
	}
}

func TestRTSPIntegrationReceiveStallRoundTrip(t *testing.T) {
	packets := []rtspIntegrationPacket{
		{raw: rtspIntegrationRTP(96, 1, 0, 123, []byte{0x65, 0x88, 0x84}, false)},
		{raw: rtspIntegrationRTP(96, 2, 3600, 123, []byte{0x61, 0x88, 0x84}, false), delay: 1100 * time.Millisecond},
	}
	f := newRTSPIntegrationFixture(t, rtspIntegrationConfig{packets: packets, closeAfterPlay: true})
	service := rtspIntegrationOpen(t)
	detail := rtspIntegrationCapture(t, service, f.address(), "rtsp", 2)
	rtspIntegrationRoundTrip(t, service, detail)
	if detail.Report.RTSP == nil || len(detail.Report.RTSP.Tracks) != 1 || detail.Report.RTSP.Tracks[0].RTPPackets != 2 || detail.Report.RTSP.Tracks[0].MaxArrivalGapMS < 1000 {
		t.Fatalf("actual 1.1 second receive gap was omitted: %+v", detail.Report.RTSP)
	}
	found := false
	for _, finding := range detail.Report.Findings {
		if finding.Code == "rtsp_receive_gap" {
			found = true
		}
	}
	if !found {
		t.Fatalf("receive gap lacks a report finding: %+v", detail.Report.Findings)
	}
}

// Keep a format check close to the fixture builder: two medias deliberately use
// the same dynamic payload number, so a global payload-type track lookup fails.
func TestRTSPIntegrationFixturePacketIntegrity(t *testing.T) {
	raw := rtspIntegrationRTP(96, 65535, 0, 100, []byte{0x65}, true)
	if len(raw) != 29 || !bytes.Equal(raw[len(raw)-4:], []byte{0xa1, 0xb2, 0xc3, 4}) {
		t.Fatalf("invalid noncanonical raw RTP fixture: %x", raw)
	}
	var packet rtp.Packet
	if err := packet.Unmarshal(raw); err != nil {
		t.Fatal(err)
	}
	reconstructed, err := packet.Marshal()
	if err != nil || bytes.Equal(reconstructed, raw) {
		t.Fatalf("fixture does not distinguish original bytes from parsed packet Marshal: %v", err)
	}
}

func rtspIntegrationCertificate(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "RTSP integration fixture"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: certificate}}, MinVersion: tls.VersionTLS12}, pool
}

func TestRTSPIntegrationTLSValidationAndTrustedCapture(t *testing.T) {
	serverTLS, roots := rtspIntegrationCertificate(t)
	for _, tc := range []struct {
		name      string
		clientTLS *tls.Config
		trusted   bool
	}{
		{name: "untrusted"},
		{name: "trusted", clientTLS: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, trusted: true},
		{name: "wrongHostname", clientTLS: &tls.Config{RootCAs: roots, ServerName: "wrong-host.example", MinVersion: tls.VersionTLS12}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packet := rtspIntegrationRTP(96, 0, 0, 999, []byte{0x65, 0x88, 0x84}, true)
			f := newRTSPIntegrationFixture(t, rtspIntegrationConfig{tlsConfig: serverTLS, packets: []rtspIntegrationPacket{{raw: packet}}, closeAfterPlay: true})
			u, _ := url.Parse(f.address())
			dir := t.TempDir()
			report, err := sampleRTSPWithTLS(t.Context(), dir, u, 1, "rtsps", tc.clientTLS)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.trusted {
				if report.Manifest.Observation.FailureStage != "tls" || report.Manifest.Observation.FailureCode != "tls_error" {
					t.Fatalf("certificate validation was bypassed or misclassified: %+v", report.Manifest.Observation)
				}
			} else if report.RTSP == nil || !report.RTSP.Secure || report.RTSP.RTPPackets != 1 || report.Manifest.Observation.EndReason != "eof" {
				t.Fatalf("trusted RTSPS did not capture native RTP: %+v", report)
			}
			if err := persistReport(dir, report); err != nil {
				t.Fatal(err)
			}
			var bundle bytes.Buffer
			if err := writeBundle(&bundle, dir, report); err != nil {
				t.Fatal(err)
			}
			importDir := t.TempDir()
			replayed, err := readBundle(importDir, &bundle)
			if err != nil || !reflect.DeepEqual(replayed, report) {
				t.Fatalf("RTSPS offline report mismatch: %v\nbefore=%+v\nafter=%+v", err, report, replayed)
			}
			raw, err := os.ReadFile(filepath.Join(importDir, "stream.rtsp.zip"))
			if err != nil {
				t.Fatal(err)
			}
			files := rtspIntegrationZIP(t, raw)
			if !bytes.Equal(files["server-wire.bin"], f.recorded()) {
				t.Fatal("RTSPS evidence differs from original application wire after validated TLS")
			}
		})
	}
}

func TestRTSPIntegrationVideoGapKeepsAudio(t *testing.T) {
	manager := &onvif.Manager{}
	t.Cleanup(manager.Close)
	status, err := manager.Start(onvif.Config{BindIP: "127.0.0.1", VideoGapMS: 1250})
	if err != nil {
		t.Fatal(err)
	}
	service := rtspIntegrationOpen(t)
	run, err := service.Start(SampleRequest{URL: status.StreamURL, Protocol: ProtocolRTSP, Seconds: 4})
	if err != nil {
		t.Fatal(err)
	}
	detail := rtspIntegrationAwait(t, service, run.ID)
	metrics := detail.Report.RTSP
	if metrics == nil || len(metrics.Tracks) != 2 || metrics.Connections != 1 {
		t.Fatalf("expected both tracks in one RTSP session: %+v", metrics)
	}
	var video, audio RTSPTrackMetrics
	for _, track := range metrics.Tracks {
		if track.Kind == "video" {
			video = track
		} else if track.Kind == "audio" {
			audio = track
		}
	}
	if !video.Received || !audio.Received || video.MaxArrivalGapMS < 1100 || audio.MaxArrivalGapMS > 500 {
		t.Fatalf("video-only RTP gap not observed: video=%+v audio=%+v", video, audio)
	}
	var found bool
	for _, finding := range detail.Report.Findings {
		if finding.Code == "rtsp_receive_gap" && finding.EndMS-finding.StartMS >= 1100 && len(finding.Evidence) == 2 && int(finding.Evidence[0].Track) == video.ID {
			found = true
		}
	}
	gaps := manager.Status().VideoGaps
	if !found || len(gaps) != 2 || gaps[0].Begin.IsZero() || gaps[0].End.IsZero() {
		t.Fatalf("missing source/reception gap timestamps: %+v / %+v", gaps, detail.Report.Findings)
	}
}
