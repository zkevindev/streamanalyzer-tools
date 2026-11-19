// A portable native RTSP fixture for the real-browser G.711 preview regression.
// It generates a pure-Go 440 Hz PCMA/PCMU source; no FFmpeg runs in this fixture.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
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
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/g711"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/pion/rtp"
)

type fixture struct {
	server          *gortsplib.Server
	stream          *gortsplib.ServerStream
	video, audio    *description.Media
	ready           chan struct{}
	ctx             context.Context
	start           sync.Once
	plays, tcp, udp atomic.Int64
	codec           string
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

// PLAY's response must reach the client before a fixture starts its first
// random-access picture; this is transport readiness, not a second pull.
func (f *fixture) waitForReaders() bool {
	timer := time.NewTimer(30 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-f.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (f *fixture) sendVideo() {
	encoder, err := f.video.Formats[0].(*format.H264).CreateEncoder()
	if err != nil {
		return
	}
	ticker := time.NewTicker(time.Second / 15)
	defer ticker.Stop()
	for index := 0; ; index++ {
		var au h264.AnnexB
		if au.Unmarshal(testpattern.AccessUnit(index%testpattern.Len())) != nil {
			return
		}
		packets, err := encoder.Encode(au)
		if err != nil {
			return
		}
		for _, p := range packets {
			p.Timestamp = uint32(index * 6000)
			if f.stream.WritePacketRTP(f.video, p) != nil {
				return
			}
		}
		select {
		case <-f.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (f *fixture) sendAudio() {
	const rate = 8000
	const samples = 160
	start := time.Now()
	for index := 0; ; index++ {
		wait := time.Until(start.Add(time.Duration(index) * 20 * time.Millisecond))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-f.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		pcm := make([]byte, samples*2)
		for sample := 0; sample < samples; sample++ {
			value := int16(12000 * math.Sin(2*math.Pi*440*float64(index*samples+sample)/rate))
			binary.BigEndian.PutUint16(pcm[sample*2:], uint16(value))
		}
		var payload []byte
		var err error
		pt := uint8(0)
		if f.codec == "pcma" {
			payload, err = g711.Alaw(pcm).Marshal()
			pt = 8
		} else {
			payload, err = g711.Mulaw(pcm).Marshal()
		}
		if err != nil {
			return
		}
		packet := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: pt, SequenceNumber: uint16(index), Timestamp: uint32(index * samples), SSRC: 0x71108000}, Payload: payload}
		if f.stream.WritePacketRTP(f.audio, packet) != nil {
			return
		}
		select {
		case <-f.ctx.Done():
			return
		default:
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
	codec := flag.String("codec", "pcmu", "pcma or pcmu")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *codec != "pcma" && *codec != "pcmu" {
		panic("codec must be pcma or pcmu")
	}
	f := &fixture{ctx: ctx, ready: make(chan struct{}), codec: *codec}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	rtpSocket, rtcpSocket, err := udpPair()
	if err != nil {
		panic(err)
	}
	f.video = &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{&format.H264{PayloadTyp: 96, PacketizationMode: 1}}}
	payloadType := uint8(0)
	if *codec == "pcma" {
		payloadType = 8
	}
	f.audio = &description.Media{Type: description.MediaTypeAudio, Formats: []format.Format{&format.G711{PayloadTyp: payloadType, MULaw: *codec == "pcmu", SampleRate: 8000, ChannelCount: 1}}}
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
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"url": "rtsp://" + tcp.Addr().String() + "/g711", "status_url": "http://" + status.Addr().String() + "/status"})
	<-ctx.Done()
	_ = httpServer.Close()
	f.server.Close()
	f.workers.Wait()
	f.stream.Close()
}
