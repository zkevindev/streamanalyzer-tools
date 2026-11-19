// A native HEVC RTSP server for the real-browser preview regression. It serves
// the pre-encoded B-frame test pattern; no encoder or transcode runs at playback.
package main

import (
	"bytes"
	"context"
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

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

type fixture struct {
	server          *gortsplib.Server
	stream          *gortsplib.ServerStream
	media           *description.Media
	codec           *format.H265
	frames          [][][]byte
	ready           chan struct{}
	ctx             context.Context
	start           sync.Once
	plays, tcp, udp atomic.Int64
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
	f.start.Do(func() { f.workers.Add(1); go func() { defer f.workers.Done(); f.send() }() })
	return &base.Response{StatusCode: base.StatusOK}, nil
}
func (f *fixture) send() {
	encoder, err := f.codec.CreateEncoder()
	if err != nil {
		return
	}
	pts := []int64{0, 4, 2, 1, 3, 8, 6, 5, 7, 12, 10, 9, 11, 14, 13, 15, 19, 17, 16, 18, 23, 21, 20, 22, 27, 25, 24, 26, 29, 28}
	start := time.Now().Add(30 * time.Millisecond)
	for index := 0; ; index++ {
		wait := time.Until(start.Add(time.Duration(index) * time.Second / 15))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-f.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		frame := index % len(f.frames)
		packets, err := encoder.Encode(f.frames[frame])
		if err != nil {
			return
		}
		for _, packet := range packets {
			packet.Timestamp = uint32(int64(index/len(f.frames))*180000 + pts[frame]*6000)
			if f.stream.WritePacketRTP(f.media, packet) != nil {
				return
			}
		}
		select {
		case <-f.ctx.Done():
			return
		default:
		}
	}
}
func loadFrames(input string) ([][][]byte, []byte, []byte, []byte, error) {
	raw, err := os.ReadFile(input)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	positions := []int{0}
	first := true
	for offset := 0; offset+5 < len(raw); {
		relative := bytes.Index(raw[offset:], []byte{0, 0, 1, 0x46, 1})
		if relative < 0 {
			break
		}
		position := offset + relative
		start := position
		if start > 0 && raw[start-1] == 0 {
			start--
		}
		if !first {
			positions = append(positions, start)
		}
		first = false
		offset = position + 5
	}
	positions = append(positions, len(raw))
	var frames [][][]byte
	var vps, sps, pps []byte
	for i := 0; i < len(positions)-1; i++ {
		var nalus h264.AnnexB
		if err := nalus.Unmarshal(raw[positions[i]:positions[i+1]]); err != nil {
			return nil, nil, nil, nil, err
		}
		frames = append(frames, [][]byte(nalus))
		for _, nalu := range nalus {
			switch (nalu[0] >> 1) & 63 {
			case 32:
				vps = nalu
			case 33:
				sps = nalu
			case 34:
				pps = nalu
			}
		}
	}
	if len(frames) != 30 {
		return nil, nil, nil, nil, fmt.Errorf("expected30HEVCAUs")
	}
	return frames, vps, sps, pps, nil
}
func main() {
	input := flag.String("input", "internal/diagnostic/testdata/preview-h265.h265", "HEVC fixture path")
	flag.Parse()
	frames, vps, sps, pps, err := loadFrames(*input)
	if err != nil {
		panic(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	var udpRTP, udpRTCP *net.UDPConn
	for i := 0; i < 100; i++ {
		first, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			panic(err)
		}
		port := first.LocalAddr().(*net.UDPAddr).Port
		if port%2 != 0 {
			first.Close()
			continue
		}
		second, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port + 1})
		if err == nil {
			udpRTP, udpRTCP = first, second
			break
		}
		first.Close()
	}
	if udpRTP == nil {
		panic("UDP pair unavailable")
	}
	f := &fixture{ctx: ctx, frames: frames, ready: make(chan struct{}), codec: &format.H265{PayloadTyp: 96, VPS: vps, SPS: sps, PPS: pps}}
	f.media = &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{f.codec}}
	f.server = &gortsplib.Server{Handler: f, RTSPAddress: listener.Addr().String(), UDPRTPAddress: udpRTP.LocalAddr().String(), UDPRTCPAddress: udpRTCP.LocalAddr().String(), Listen: func(string, string) (net.Listener, error) { return listener, nil }, ListenPacket: func(_ string, address string) (net.PacketConn, error) {
		if address == udpRTP.LocalAddr().String() {
			return udpRTP, nil
		}
		return udpRTCP, nil
	}}
	if err := f.server.Start(); err != nil {
		panic(err)
	}
	f.stream = &gortsplib.ServerStream{Server: f.server, Desc: &description.Session{Medias: []*description.Media{f.media}}}
	if err := f.stream.Initialize(); err != nil {
		panic(err)
	}
	close(f.ready)
	status, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	httpServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]int64{"plays": f.plays.Load(), "tcp": f.tcp.Load(), "udp": f.udp.Load()})
	})}
	go httpServer.Serve(status)
	data, _ := json.Marshal(map[string]string{"url": "rtsp://" + listener.Addr().String() + "/hevc", "status_url": "http://" + status.Addr().String()})
	fmt.Println(string(data))
	<-ctx.Done()
	f.server.Close()
	f.workers.Wait()
	f.stream.Close()
	httpServer.Close()
}
