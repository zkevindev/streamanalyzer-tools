package onvif

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"streamanalyzer/internal/simulator/testpattern"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

const streamPath = "/live"

type videoServer struct {
	server  *gortsplib.Server
	stream  *gortsplib.ServerStream
	media   *description.Media
	ready   chan struct{}
	plays   *atomic.Uint64
	frames  []h264.AnnexB
	encoder *rtph264.Encoder
}

func (v *videoServer) describe(path string) (*base.Response, *gortsplib.ServerStream, error) {
	<-v.ready
	if path != streamPath || v.stream == nil {
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}
	return &base.Response{StatusCode: base.StatusOK}, v.stream, nil
}

func (v *videoServer) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return v.describe(ctx.Path)
}

func (v *videoServer) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return v.describe(ctx.Path)
}

func (v *videoServer) OnPlay(_ *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	v.plays.Add(1)
	return &base.Response{StatusCode: base.StatusOK}, nil
}

func testVideo() ([]h264.AnnexB, *format.H264, error) {
	frames := make([]h264.AnnexB, testpattern.Len())
	format := &format.H264{PayloadTyp: 96, PacketizationMode: 1}
	for i := range frames {
		if err := frames[i].Unmarshal(testpattern.AccessUnit(i)); err != nil {
			return nil, nil, err
		}
	}
	if len(frames) != 0 {
		for _, nalu := range frames[0] {
			switch h264.NALUType(nalu[0] & 31) {
			case h264.NALUTypeSPS:
				format.SPS = nalu
			case h264.NALUTypePPS:
				format.PPS = nalu
			}
		}
	}
	if len(format.SPS) == 0 || len(format.PPS) == 0 {
		return nil, nil, fmt.Errorf("H.264 测试画面缺少 SPS 或 PPS")
	}
	return frames, format, nil
}

func startVideo(ip net.IP, port int, plays *atomic.Uint64) (*videoServer, error) {
	frames, avc, err := testVideo()
	if err != nil {
		return nil, err
	}
	encoder, err := avc.CreateEncoder()
	if err != nil {
		return nil, err
	}
	tcp, err := net.Listen("tcp4", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	rtp, rtcp, err := listenVideoUDP(ip)
	if err != nil {
		tcp.Close()
		return nil, err
	}
	v := &videoServer{ready: make(chan struct{}), plays: plays, frames: frames, encoder: encoder}
	v.media = &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{avc}}
	v.server = &gortsplib.Server{
		Handler:        v,
		RTSPAddress:    tcp.Addr().String(),
		UDPRTPAddress:  rtp.LocalAddr().String(),
		UDPRTCPAddress: rtcp.LocalAddr().String(),
		Listen:         func(_, _ string) (net.Listener, error) { return tcp, nil },
		ListenPacket: func(_, address string) (net.PacketConn, error) {
			if address == rtp.LocalAddr().String() {
				return rtp, nil
			}
			return rtcp, nil
		},
	}
	if err := v.server.Start(); err != nil {
		tcp.Close()
		rtp.Close()
		rtcp.Close()
		return nil, err
	}
	stream := &gortsplib.ServerStream{Server: v.server, Desc: &description.Session{Medias: []*description.Media{v.media}}}
	err = stream.Initialize()
	if err == nil {
		v.stream = stream
	}
	close(v.ready)
	if err != nil {
		v.server.Close()
		return nil, err
	}
	return v, nil
}

func listenVideoUDP(ip net.IP) (*net.UDPConn, *net.UDPConn, error) {
	// RTSP negotiates an even, consecutive RTP/RTCP pair. Hold both
	// sockets until gortsplib takes ownership so another bind cannot race.
	for range 32 {
		first, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
		if err != nil {
			return nil, nil, err
		}
		port := first.LocalAddr().(*net.UDPAddr).Port
		other := port + 1
		if port%2 != 0 {
			other = port - 1
		}
		second, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: other})
		if err != nil {
			first.Close()
			continue
		}
		if port%2 == 0 {
			return first, second, nil
		}
		return second, first, nil
	}
	return nil, nil, fmt.Errorf("无法分配连续的 RTSP RTP 和 RTCP 端口")
}

func (v *videoServer) play(ctx context.Context) {
	ticker := time.NewTicker(time.Second / testpattern.FPS)
	defer ticker.Stop()
	var timestamp uint32
	for index := 0; ; index = (index + 1) % len(v.frames) {
		packets, err := v.encoder.Encode(v.frames[index])
		if err != nil {
			return
		}
		for _, packet := range packets {
			packet.Timestamp = timestamp
			if err := v.stream.WritePacketRTP(v.media, packet); err != nil {
				return
			}
		}
		timestamp += 90000 / testpattern.FPS
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (v *videoServer) close() {
	v.stream.Close()
	v.server.Close()
}
