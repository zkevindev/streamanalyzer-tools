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
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
)

type videoTrack struct {
	alternate            *profileVideo
	switchDelay          time.Duration
	switchAt, switchedAt atomic.Int64
	clockStep            time.Duration
	clockArmed           atomic.Int64
	profile              *profileVideo
	stream               *gortsplib.ServerStream
	silent               *gortsplib.ServerStream
	media                *description.Media
	audio                *description.Media
	gap                  videoGap
	outage               rtspOutage
	encoder              *rtph264.Encoder
}

type videoServer struct {
	sourceFault    *udpSourceFault
	recorded       *recordingRTSP
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
	if v.recorded != nil && path == recordedRTSPPath {
		return &base.Response{StatusCode: base.StatusOK}, v.recorded.description, nil
	}
	track := v.tracks[path]
	if track == nil || track.stream == nil {
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}
	if track.outage.reject(time.Now()) {
		return &base.Response{StatusCode: base.StatusServiceUnavailable}, nil, nil
	}
	return &base.Response{StatusCode: base.StatusOK}, track.stream, nil
}

func (v *videoServer) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	return v.describe(ctx.Conn, ctx.Request, ctx.Path)
}

func (v *videoServer) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	res, stream, err := v.describe(ctx.Conn, ctx.Request, ctx.Path)
	if stream != nil && v.recorded != nil && ctx.Path == recordedRTSPPath {
		return v.recorded.setup(v.server, ctx.Session)
	}
	if stream != nil && ctx.Transport.Protocol == gortsplib.ProtocolUDP && v.sourceFault != nil {
		var transport headers.Transport
		if transport.Unmarshal(ctx.Request.Header["Transport"]) == nil && transport.ClientPorts != nil {
			remote := ctx.Conn.NetConn().RemoteAddr().(*net.TCPAddr)
			v.sourceFault.setup(ctx.Session, remote.IP, transport.ClientPorts[0])
		}
	}
	if stream != nil && ctx.Transport.Protocol == gortsplib.ProtocolUDP && v.tracks[ctx.Path].silent != nil {
		stream = v.tracks[ctx.Path].silent
	}
	return res, stream, err
}

func (v *videoServer) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	<-v.ready
	if v.recorded != nil {
		v.recorded.remove(ctx.Session)
	}
	if v.sourceFault != nil {
		v.sourceFault.forget(ctx.Session)
	}
	for _, track := range v.tracks {
		track.outage.forget(ctx.Session)
	}
}

func (v *videoServer) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	<-v.ready
	if v.recorded != nil && ctx.Path == recordedRTSPPath {
		return v.recorded.play(ctx)
	}
	track := v.tracks[ctx.Path]
	if track != nil && !track.outage.play(time.Now(), ctx.Session, ctx.Conn) {
		return &base.Response{StatusCode: base.StatusServiceUnavailable}, nil
	}
	if track != nil && track.alternate != nil {
		track.switchAt.CompareAndSwap(0, time.Now().Add(track.switchDelay).UnixNano())
	}
	if track != nil && track.clockStep != 0 {
		track.clockArmed.CompareAndSwap(0, time.Now().Add(3*time.Second).UnixNano())
	}
	if ctx.Session.Transport().Protocol == gortsplib.ProtocolTCP {
		v.tcpPlays.Add(1)
		if track != nil && track.gap.duration > 0 {
			track.gap.armed.CompareAndSwap(0, time.Now().Add(videoGapDelay).UnixNano())
		}
	} else {
		v.udpPlays.Add(1)
		if v.sourceFault != nil {
			v.sourceFault.arm(ctx.Session)
		}
	}
	return &base.Response{StatusCode: base.StatusOK}, nil
}

func startVideo(ip net.IP, cfg Config, profiles []*profileVideo, fixture *recordingFixture) (*videoServer, error) {
	v := &videoServer{ready: make(chan struct{}), tracks: make(map[string]*videoTrack),
		user: cfg.RTSPUsername, password: cfg.RTSPPassword}
	for _, profile := range profiles {
		encoder, err := profile.avc.CreateEncoder()
		if err != nil {
			return nil, err
		}
		v.tracks[profile.path] = &videoTrack{profile: profile, encoder: encoder,
			media:  &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{profile.avc}},
			outage: rtspOutage{duration: time.Duration(cfg.RTSPDisconnectMS) * time.Millisecond}}
		if cfg.VideoGapMS > 0 || cfg.RTCPClockStepMS != 0 {
			track := v.tracks[profile.path]
			track.clockStep = time.Duration(cfg.RTCPClockStepMS) * time.Millisecond
			track.gap.duration = time.Duration(cfg.VideoGapMS) * time.Millisecond
			track.audio = &description.Media{Type: description.MediaTypeAudio, Formats: []format.Format{&format.G711{PayloadTyp: 8, SampleRate: 8000, ChannelCount: 1}}}
		}
	}
	if cfg.RTSPResolutionSwitchMS != 0 {
		main := v.tracks[profiles[0].path]
		main.alternate, main.switchDelay = profiles[1], time.Duration(cfg.RTSPResolutionSwitchMS)*time.Millisecond
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
	if cfg.UDPSourceFaultMS != 0 {
		v.sourceFault, err = openUDPSourceFault(rtp, ip, time.Duration(cfg.UDPSourceFaultMS)*time.Millisecond)
		if err != nil {
			tcp.Close()
			rtp.Close()
			rtcp.Close()
			return nil, err
		}
	}
	v.server = &gortsplib.Server{
		Handler:                  v,
		DisableRTCPSenderReports: cfg.RTCPClockStepMS != 0,
		AuthMethods:              []auth.VerifyMethod{auth.VerifyMethodDigestMD5},
		RTSPAddress:              tcp.Addr().String(),
		UDPRTPAddress:            rtp.LocalAddr().String(),
		UDPRTCPAddress:           rtcp.LocalAddr().String(),
		Listen:                   func(_, _ string) (net.Listener, error) { return tcp, nil },
		ListenPacket: func(_, address string) (net.PacketConn, error) {
			if address == rtp.LocalAddr().String() {
				if v.sourceFault != nil {
					return v.sourceFault, nil
				}
				return rtp, nil
			}
			return rtcp, nil
		},
	}
	if err := v.server.Start(); err != nil {
		if v.sourceFault != nil {
			v.sourceFault.alternate.Close()
		}
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
	if fixture != nil {
		fixture.info.StreamURL = "rtsp://" + v.server.RTSPAddress + recordedRTSPPath
		archived, err := newRecordingRTSP(v.server, profiles[0], fixture)
		if err != nil {
			close(v.ready)
			v.close()
			return nil, err
		}
		v.recorded = archived
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
	var clock senderClock
	var skipped bool
	profile := v.profile
	for index := 0; ; index = (index + 1) % len(profile.frames) {
		now := time.Now()
		v.outage.advance(now)
		if v.gap.skip(now) {
			skipped = true
		} else {
			if skipped {
				index = 0
			}
			changing := false
			if at := v.switchAt.Load(); at != 0 && now.UnixNano() >= at && profile != v.alternate {
				profile, index, changing = v.alternate, 0, true
			}
			packets, err := v.encoder.Encode(profile.frames[index])
			if err != nil {
				return
			}
			var octets uint32
			for _, packet := range packets {
				octets += uint32(len(packet.Payload))
				packet.Timestamp = timestamp
				if err := v.stream.WritePacketRTP(v.media, packet); err != nil {
					return
				}
			}
			if changing {
				v.switchedAt.Store(now.UnixNano())
			}
			if len(packets) > 0 && v.clockStep != 0 {
				clock.send(v, v.media, packets[0], 90000, now, 0, uint32(len(packets)), octets)
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
	if v.sourceFault != nil {
		v.sourceFault.alternate.Close()
	}
	if v.recorded != nil {
		v.recorded.description.Close()
	}
	for _, track := range v.tracks {
		if track.stream != nil {
			track.stream.Close()
		}
		if track.silent != nil {
			track.silent.Close()
		}
	}
}
