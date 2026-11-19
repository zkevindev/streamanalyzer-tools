// A same-session H.264/AAC source with a real four-second RTP pause.
// Encoded media is embedded; no encoder or second source connection is used.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"streamanalyzer/internal/simulator/testpattern"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
)

//go:embed tone.aac
var audioADTS []byte

type fixture struct {
	server          *gortsplib.Server
	stream          *gortsplib.ServerStream
	video, audio    *description.Media
	ready           chan struct{}
	ctx             context.Context
	start           sync.Once
	plays, tcp, udp atomic.Int64
	audioAUs        mpeg4audio.ADTSPackets
	clockStart      time.Time
	workers         sync.WaitGroup
}

func (f *fixture) OnDescribe(*gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	<-f.ready
	return &base.Response{StatusCode: base.StatusOK}, f.stream, nil
}
func (f *fixture) OnSetup(*gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	<-f.ready
	return &base.Response{StatusCode: base.StatusOK}, f.stream, nil
}
func (f *fixture) OnPlay(c *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	f.plays.Add(1)
	if c.Session.Transport().Protocol == gortsplib.ProtocolUDP {
		f.udp.Add(1)
	} else {
		f.tcp.Add(1)
	}
	f.start.Do(func() {
		f.clockStart = time.Now().Add(30 * time.Millisecond)
		f.workers.Add(2)
		go func() {
			defer f.workers.Done()
			if f.waitForReaders() {
				f.sendVideo()
			}
		}()
		go func() {
			defer f.workers.Done()
			if f.waitForReaders() {
				f.sendAudio()
			}
		}()
	})
	return &base.Response{StatusCode: base.StatusOK}, nil
}

// RTP timestamps remain contiguous across this pause. Arrival offsets therefore
// diverge from media time by four seconds; they must not be used as media PTS.
func (f *fixture) waitAt(mediaTime time.Duration) bool {
	deadline := f.clockStart.Add(mediaTime)
	if mediaTime >= 3*time.Second {
		deadline = deadline.Add(4 * time.Second)
	}
	timer := time.NewTimer(max(time.Until(deadline), 0))
	defer timer.Stop()
	select {
	case <-f.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (f *fixture) waitForReaders() bool { return f.waitAt(0) }
func (f *fixture) sendVideo() {
	encoder, err := f.video.Formats[0].(*format.H264).CreateEncoder()
	if err != nil {
		return
	}
	for index := 0; ; index++ {
		if !f.waitAt(time.Duration(index) * time.Second / 15) {
			return
		}
		var au h264.AnnexB
		if au.Unmarshal(testpattern.AccessUnit(index%testpattern.Len())) != nil {
			return
		}
		packets, err := encoder.Encode(au)
		if err != nil {
			return
		}
		for _, packet := range packets {
			packet.Timestamp = 37*90000 + uint32(index*6000)
			if f.stream.WritePacketRTP(f.video, packet) != nil {
				return
			}
		}
	}
}
func (f *fixture) sendAudio() {
	encoder, err := f.audio.Formats[0].(*format.MPEG4Audio).CreateEncoder()
	if err != nil {
		return
	}
	for index := 0; ; index++ {
		if !f.waitAt(time.Duration(index) * 1024 * time.Second / 48000) {
			return
		}
		packets, err := encoder.Encode([][]byte{f.audioAUs[index%len(f.audioAUs)].AU})
		if err != nil {
			return
		}
		for _, packet := range packets {
			packet.Timestamp = 37*48000 + uint32(index*1024)
			if f.stream.WritePacketRTP(f.audio, packet) != nil {
				return
			}
		}
	}
}

func udpPair() (*net.UDPConn, *net.UDPConn, error) {
	for range 64 {
		a, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			return nil, nil, err
		}
		port := a.LocalAddr().(*net.UDPAddr).Port
		other := port + 1
		if port%2 != 0 {
			other = port - 1
		}
		b, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: other})
		if err != nil {
			a.Close()
			continue
		}
		if port%2 == 0 {
			return a, b, nil
		}
		return b, a, nil
	}
	return nil, nil, fmt.Errorf("no consecutive UDP pair")
}

func main() {
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	f := &fixture{ctx: ctx, ready: make(chan struct{})}
	if err := f.audioAUs.Unmarshal(audioADTS); err != nil {
		panic(err)
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	rtpSocket, rtcpSocket, err := udpPair()
	if err != nil {
		panic(err)
	}
	f.video = &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{&format.H264{PayloadTyp: 96, PacketizationMode: 1}}}
	f.audio = &description.Media{Type: description.MediaTypeAudio, Formats: []format.Format{&format.MPEG4Audio{PayloadTyp: 97, Config: &mpeg4audio.AudioSpecificConfig{Type: mpeg4audio.ObjectTypeAACLC, SampleRate: 48000, ChannelConfig: 1}, SizeLength: 13, IndexLength: 3, IndexDeltaLength: 3}}}
	f.server = &gortsplib.Server{Handler: f, RTSPAddress: tcp.Addr().String(), UDPRTPAddress: rtpSocket.LocalAddr().String(), UDPRTCPAddress: rtcpSocket.LocalAddr().String(),
		Listen: func(string, string) (net.Listener, error) { return tcp, nil }, ListenPacket: func(_, address string) (net.PacketConn, error) {
			if address == rtpSocket.LocalAddr().String() {
				return rtpSocket, nil
			}
			return rtcpSocket, nil
		}}
	if err := f.server.Start(); err != nil {
		panic(err)
	}
	f.stream = &gortsplib.ServerStream{Server: f.server, Desc: &description.Session{Medias: []*description.Media{f.video, f.audio}}}
	if err := f.stream.Initialize(); err != nil {
		panic(err)
	}
	close(f.ready)
	status, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	httpServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"plays": f.plays.Load(), "tcp": f.tcp.Load(), "udp": f.udp.Load()})
	}), ReadHeaderTimeout: time.Second}
	go httpServer.Serve(status)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"url": "rtsp://" + tcp.Addr().String() + "/stall", "status_url": "http://" + status.Addr().String() + "/status"})
	<-ctx.Done()
	_ = httpServer.Close()
	f.server.Close()
	f.workers.Wait()
	f.stream.Close()
}
