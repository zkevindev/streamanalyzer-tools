// A portable native RTSP fixture for the real-browser Opus preview regression.
// It serves pre-encoded fixture packets; no encoder or ffmpeg runs at playback.
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
	"github.com/pion/rtp"
)

//go:embed packets.json
var packetsJSON []byte

type audioPacket struct {
	DurationMS float64 `json:"duration_ms"`
	Samples    int64   `json:"samples"`
	Payload    []byte  `json:"payload"`
}

type fixture struct {
	server          *gortsplib.Server
	stream          *gortsplib.ServerStream
	video, audio    *description.Media
	ready           chan struct{}
	ctx             context.Context
	start           sync.Once
	plays, tcp, udp atomic.Int64
	mode            string
	packets         []audioPacket
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
		go func() { defer f.workers.Done(); f.sendVideo() }()
		go func() { defer f.workers.Done(); f.sendAudio() }()
	})
	return &base.Response{StatusCode: base.StatusOK}, nil
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
	start := time.Now()
	var timestamp int64
	var sequence uint16
	gap := false
	for index := 0; ; index++ {
		packet := f.packets[3] // 20 ms common RFC 7587 packet
		if f.mode == "variants" {
			packet = f.packets[index%len(f.packets)]
		}
		if f.mode == "variants" && !gap && timestamp >= 96000 {
			timestamp += 9600
			gap = true
		}
		wait := time.Until(start.Add(time.Duration(timestamp) * time.Second / 48000))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-f.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		p := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 97, SequenceNumber: sequence, Timestamp: uint32(timestamp), SSRC: 0x12345678}, Payload: packet.Payload}
		if f.stream.WritePacketRTP(f.audio, p) != nil {
			return
		}
		timestamp += packet.Samples
		sequence++
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
	mode := flag.String("mode", "common", "common or variants (including DTX gap)")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	f := &fixture{ctx: ctx, ready: make(chan struct{}), mode: *mode}
	if err := json.Unmarshal(packetsJSON, &f.packets); err != nil {
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
	f.audio = &description.Media{Type: description.MediaTypeAudio, Formats: []format.Format{&format.Opus{PayloadTyp: 97, ChannelCount: 2}}}
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
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"url": "rtsp://" + tcp.Addr().String() + "/opus", "status_url": "http://" + status.Addr().String() + "/status"})
	<-ctx.Done()
	_ = httpServer.Close()
	f.server.Close()
	f.workers.Wait()
	f.stream.Close()
}
