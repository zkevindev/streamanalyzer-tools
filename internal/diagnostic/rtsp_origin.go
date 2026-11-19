package diagnostic

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
)

type RTSPReplayStatus struct {
	Transport     string `json:"transport,omitempty"`
	SenderReports int    `json:"sender_reports,omitempty"`
	URL           string `json:"url,omitempty"`
	Packets       int    `json:"packets"`
	DurationMS    int64  `json:"duration_ms"`
	Plays         uint64 `json:"plays"`
}

type rtspReplayFrame struct {
	rtcp       bool
	offset, at int64
	size       int
	media      int
}

type replayRTPStart struct {
	ssrc      uint32
	seq       uint16
	timestamp uint32
}

type rtspReplayOrigin struct {
	transport              string
	packets, senderReports int
	ctx                    context.Context
	cancel                 context.CancelFunc
	listener               *net.TCPListener
	done                   chan struct{}
	workers                sync.WaitGroup
	sdp                    []byte
	url                    string
	file                   *os.File
	fileName               string
	frames                 []rtspReplayFrame
	tracks                 map[int]bool
	startPacket            map[int]replayRTPStart
	playbackStart          int64
	duration               int64
	plays                  atomic.Uint64
}

func canReplayRTSP(report Report) bool {
	if report.RTSP == nil || (report.RTSP.Transport != "tcp_interleaved" && report.RTSP.Transport != "udp") || report.RTSP.RTPPackets == 0 || !report.RTSP.SDPAvailable {
		return false
	}
	for _, track := range report.RTSP.Tracks {
		if (track.Kind == "video" || track.Kind == "audio") && track.Codec != "unknown" && track.RTPPackets > 0 {
			return true
		}
	}
	return false
}

func (s *Service) StartRTSPReplay(ctx context.Context, id string) (RTSPReplayStatus, error) {
	s.rtspReplayMu.Lock()
	defer s.rtspReplayMu.Unlock()
	if source := s.rtspReplays[id]; source != nil {
		return source.snapshot(), nil
	}
	if s.rtspReplayClosed || len(s.rtspReplays) >= 4 {
		return RTSPReplayStatus{}, ErrBusy
	}
	detail, err := s.Detail(id)
	if err != nil {
		return RTSPReplayStatus{}, err
	}
	if detail.Report == nil {
		return RTSPReplayStatus{}, ErrPending
	}
	if !canReplayRTSP(*detail.Report) {
		return RTSPReplayStatus{}, fmt.Errorf("%w: 该记录缺少可回放的 RTSP 音视频 RTP", ErrInvalid)
	}
	source, err := startRTSPOrigin(ctx, s.root, s.runDir(detail.ID), detail.Report.Manifest)
	if err != nil {
		return RTSPReplayStatus{}, err
	}
	s.rtspReplays[detail.ID] = source
	return source.snapshot(), nil
}

func (s *Service) StopRTSPReplay(id string) {
	s.rtspReplayMu.Lock()
	defer s.rtspReplayMu.Unlock()
	if source := s.rtspReplays[id]; source != nil {
		source.close()
		delete(s.rtspReplays, id)
	}
}

func (s *Service) StopRTSPReplays() {
	s.rtspReplayMu.Lock()
	defer s.rtspReplayMu.Unlock()
	s.rtspReplayClosed = true
	for _, source := range s.rtspReplays {
		source.close()
	}
	clear(s.rtspReplays)
}

func (s *Service) StatusRTSPReplay(id string) RTSPReplayStatus {
	s.rtspReplayMu.Lock()
	defer s.rtspReplayMu.Unlock()
	if source := s.rtspReplays[id]; source != nil {
		return source.snapshot()
	}
	return RTSPReplayStatus{}
}

func startRTSPOrigin(ctx context.Context, root, dir string, manifest Manifest) (*rtspReplayOrigin, error) {
	file, err := os.CreateTemp(root, ".rtsp-replay-*")
	if err != nil {
		return nil, err
	}
	source := &rtspReplayOrigin{file: file, fileName: file.Name(), tracks: make(map[int]bool), startPacket: make(map[int]replayRTPStart), done: make(chan struct{})}
	// Unlink while open on systems that support it. The handle remains
	// available to each client through ReadAt until the origin shuts down.
	if os.Remove(source.fileName) == nil {
		source.fileName = ""
	}
	ready := false
	defer func() {
		if !ready {
			source.closeFile()
		}
	}()

	appendFrame := func(media int, data []byte, arrival int64, rtcp bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !source.tracks[media] {
			return nil
		}
		if !rtcp {
			if _, known := source.startPacket[media]; !known {
				source.startPacket[media] = replayRTPStart{ssrc: binary.BigEndian.Uint32(data[8:]), seq: binary.BigEndian.Uint16(data[2:]), timestamp: binary.BigEndian.Uint32(data[4:])}
			}
			source.packets++
		} else {
			source.senderReports++
		}
		var offset int64
		if n := len(source.frames); n > 0 {
			last := source.frames[n-1]
			offset = last.offset + int64(last.size)
		}
		if _, err := file.Write(data); err != nil {
			return err
		}
		source.frames = append(source.frames, rtspReplayFrame{offset: offset, at: arrival, size: len(data), media: media, rtcp: rtcp})
		return nil
	}
	export := &rtspWireExport{describe: source.setDescription, packet: func(media int, data []byte, at int64) error {
		return appendFrame(media, data, at, false)
	}, senderReport: func(media int, data []byte, at int64) error {
		return appendFrame(media, data, at, true)
	}}
	report, err := readRTSPWire(dir, manifest, nil, export)
	if err != nil {
		return nil, err
	}
	if source.packets == 0 {
		return nil, fmt.Errorf("%w: 未找到可回放的音视频 RTP 包", ErrInvalid)
	}
	source.transport = "tcp"
	if report.RTSP.Transport == "udp" {
		source.transport = "udp"
	}
	for _, request := range report.RTSP.Requests {
		if request.Method == "PLAY" && request.StatusCode == 200 {
			source.playbackStart = min(request.EndMS, source.frames[0].at)
			break
		}
	}
	source.duration = manifest.Observation.ElapsedMS - source.playbackStart
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	source.listener = listener
	source.url = "rtsp://" + listener.Addr().String() + "/replay"
	source.ctx, source.cancel = context.WithCancel(context.Background())
	ready = true
	go source.serve()
	return source, nil
}

func (o *rtspReplayOrigin) setDescription(captured *description.Session) error {
	if captured == nil {
		return ErrInvalid
	}
	local := description.Session{Title: "Stream Analyzer RTSP replay"}
	for index, input := range captured.Medias {
		if input.IsBackChannel || input.Type != description.MediaTypeVideo && input.Type != description.MediaTypeAudio {
			continue
		}
		var codecs []format.Format
		for _, codec := range input.Formats {
			if rtspCodec(codec) != "unknown" {
				codecs = append(codecs, codec)
			}
		}
		if len(codecs) == 0 {
			continue
		}
		id := index + 1
		local.Medias = append(local.Medias, &description.Media{Type: input.Type, Control: "trackID=" + strconv.Itoa(id),
			Profile: headers.TransportProfileAVP, Formats: codecs})
		o.tracks[id] = true
	}
	if len(local.Medias) == 0 {
		return ErrInvalid
	}
	var err error
	o.sdp, err = local.Marshal()
	return err
}

func (o *rtspReplayOrigin) snapshot() RTSPReplayStatus {
	return RTSPReplayStatus{URL: o.url, Transport: o.transport, Packets: o.packets, SenderReports: o.senderReports, Plays: o.plays.Load(), DurationMS: o.duration}
}

func (o *rtspReplayOrigin) close() {
	o.cancel()
	o.listener.Close()
	<-o.done
	o.workers.Wait()
	o.closeFile()
}

func (o *rtspReplayOrigin) closeFile() {
	o.file.Close()
	if o.fileName != "" {
		_ = os.Remove(o.fileName)
	}
}
