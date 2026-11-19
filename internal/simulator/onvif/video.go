package onvif

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/auth"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
)

type videoTrack struct {
	profile *profileVideo
	stream  *gortsplib.ServerStream
	silent  *gortsplib.ServerStream
	media   *description.Media
	audio   *description.Media
	gap     videoGap
	encoder *rtph264.Encoder
}

type videoServer struct {
	server         *gortsplib.Server
	tracks         map[string]*videoTrack
	ready          chan struct{}
	udpPlays       atomic.Uint64
	tcpPlays       atomic.Uint64
	user, password string
}

func (v *videoServer) describe(conn *gortsplib.ServerConn, request *base.Request, path string) (*base.Response, *gortsplib.ServerStream, error) {
	<-v.ready
	if v.user != "" && !conn.VerifyCredentials(request, v.user, v.password) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	track := v.tracks[path]
	if track == nil || track.stream == nil {
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}
	return &base.Response{StatusCode: base.StatusOK}, track.stream, nil
}

func (v *videoServer) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return v.describe(ctx.Conn, ctx.Request, ctx.Path)
}

func (v *videoServer) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	res, stream, err := v.describe(ctx.Conn, ctx.Request, ctx.Path)
	if stream != nil && ctx.Transport.Protocol == gortsplib.ProtocolUDP && v.tracks[ctx.Path].silent != nil {
		stream = v.tracks[ctx.Path].silent
	}
	return res, stream, err
}

func (v *videoServer) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	if ctx.Session.Transport().Protocol == gortsplib.ProtocolTCP {
		v.tcpPlays.Add(1)
		if track := v.tracks[ctx.Path]; track != nil && track.gap.duration > 0 {
			track.gap.armed.CompareAndSwap(0, time.Now().Add(videoGapDelay).UnixNano())
		}
	} else {
		v.udpPlays.Add(1)
	}
	return &base.Response{StatusCode: base.StatusOK}, nil
}

func startVideo(ip net.IP, cfg Config, profiles []*profileVideo) (*videoServer, error) {
	v := &videoServer{ready: make(chan struct{}), tracks: make(map[string]*videoTrack),
		user: cfg.RTSPUsername, password: cfg.RTSPPassword}
	for _, profile := range profiles {
		encoder, err := profile.avc.CreateEncoder()
		if err != nil {
			return nil, err
		}
		v.tracks[profile.path] = &videoTrack{profile: profile, encoder: encoder,
			media: &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{profile.avc}}}
		if cfg.VideoGapMS > 0 {
			track := v.tracks[profile.path]
			track.gap.duration = time.Duration(cfg.VideoGapMS) * time.Millisecond
			track.audio = &description.Media{Type: description.MediaTypeAudio, Formats: []format.Format{&format.G711{PayloadTyp: 8, SampleRate: 8000, ChannelCount: 1}}}
		}
	}
	tcp, err := net.Listen("tcp4", net.JoinHostPort(ip.String(), strconv.Itoa(cfg.RTSPPort)))
	if err != nil {
		return nil, err
	}
	rtp, rtcp, err := listenVideoUDP(ip)
	if err != nil {
		tcp.Close()
		return nil, err
	}
	v.server = &gortsplib.Server{
		Handler:        v,
		AuthMethods:    []auth.VerifyMethod{auth.VerifyMethodDigestMD5},
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
	for _, track := range v.tracks {
		medias := []*description.Media{track.media}
		if track.audio != nil {
			medias = append(medias, track.audio)
		}
		stream := &gortsplib.ServerStream{Server: v.server, Desc: &description.Session{Medias: medias}}
		if err := stream.Initialize(); err != nil {
			close(v.ready)
			v.close()
			return nil, err
		}
		track.stream = stream
		if cfg.SilentUDP {
			silent := &gortsplib.ServerStream{Server: v.server, Desc: &description.Session{Medias: medias}}
			if err := silent.Initialize(); err != nil {
				close(v.ready)
				v.close()
				return nil, err
			}
			track.silent = silent
		}
	}
	close(v.ready)
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

func (v *videoTrack) play(ctx context.Context) {
	ticker := time.NewTicker(time.Second / time.Duration(v.profile.FPS))
	defer ticker.Stop()
	var timestamp uint32
	var skipped bool
	for index := 0; ; index = (index + 1) % len(v.profile.frames) {
		if v.gap.skip(time.Now()) {
			skipped = true
		} else {
			if skipped {
				index = 0
			}
			packets, err := v.encoder.Encode(v.profile.frames[index])
			if err != nil {
				return
			}
			for _, packet := range packets {
				packet.Timestamp = timestamp
				if err := v.stream.WritePacketRTP(v.media, packet); err != nil {
					return
				}
			}
			if skipped {
				v.gap.end.CompareAndSwap(0, time.Now().UnixNano())
				skipped = false
			}
		}
		timestamp += uint32(90000 / v.profile.FPS)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (v *videoServer) close() {
	v.server.Close()
	for _, track := range v.tracks {
		if track.stream != nil {
			track.stream.Close()
		}
		if track.silent != nil {
			track.silent.Close()
		}
	}
}
