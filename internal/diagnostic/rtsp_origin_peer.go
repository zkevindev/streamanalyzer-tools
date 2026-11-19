package diagnostic

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/conn"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
	"github.com/google/uuid"
)

const replayRTSPMethods = "OPTIONS, DESCRIBE, SETUP, PLAY, GET_PARAMETER, TEARDOWN"

type replayRTSPPeer struct {
	source   *rtspReplayOrigin
	wire     *net.TCPConn
	codec    *conn.Conn
	ctx      context.Context
	cancel   context.CancelFunc
	write    sync.Mutex
	play     sync.WaitGroup
	session  string
	channels map[int][2]int
	playing  bool
}

func (o *rtspReplayOrigin) serve() {
	defer close(o.done)
	slots := make(chan struct{}, 8)
	for {
		socket, err := o.listener.AcceptTCP()
		if err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
			o.workers.Go(func() {
				defer func() { <-slots }()
				ctx, cancel := context.WithCancel(o.ctx)
				peer := &replayRTSPPeer{source: o, wire: socket, codec: conn.NewConn(bufio.NewReader(socket), socket),
					ctx: ctx, cancel: cancel, channels: make(map[int][2]int)}
				peer.serve()
			})
		default:
			socket.Close()
		}
	}
}

func (p *replayRTSPPeer) serve() {
	stop := context.AfterFunc(p.ctx, func() { _ = p.wire.Close() })
	defer stop()
	defer func() {
		p.cancel()
		_ = p.wire.Close()
		p.play.Wait()
	}()
	for p.ctx.Err() == nil {
		if !p.playing {
			_ = p.wire.SetReadDeadline(time.Now().Add(10 * time.Second))
		}
		object, err := p.codec.Read()
		if err != nil {
			return
		}
		r, ok := object.(*base.Request)
		if !ok {
			if _, rtcp := object.(*base.InterleavedFrame); rtcp && p.playing {
				continue
			}
			return
		}
		response, start, closeSession := p.request(r)
		if err := p.writeResponse(response); err != nil || closeSession {
			return
		}
		if start {
			p.playing = true
			_ = p.wire.SetReadDeadline(time.Time{})
			p.source.plays.Add(1)
			p.play.Go(p.send)
		}
	}
}

func (p *replayRTSPPeer) request(r *base.Request) (res *base.Response, start bool, closeSession bool) {
	res = &base.Response{StatusCode: base.StatusOK, Header: base.Header{}}
	seq := r.Header["CSeq"]
	if len(seq) != 1 {
		res.StatusCode = base.StatusBadRequest
		return res, false, true
	}
	if _, err := strconv.ParseUint(seq[0], 10, 31); err != nil {
		res.StatusCode = base.StatusBadRequest
		return res, false, true
	}
	res.Header["CSeq"] = seq
	if r.Method == base.Options {
		res.Header["Public"] = base.HeaderValue{replayRTSPMethods}
		return
	}
	if r.URL == nil {
		res.StatusCode = base.StatusBadRequest
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	isTrack := strings.HasPrefix(path, "/replay/trackID=")
	if path != "/replay" && !isTrack {
		res.StatusCode = base.StatusNotFound
		return
	}
	if r.Method == base.Describe && path == "/replay" {
		res.Header["Content-Base"] = base.HeaderValue{p.source.url + "/"}
		res.Header["Content-Type"] = base.HeaderValue{"application/sdp"}
		res.Body = p.source.sdp
		return
	}
	if p.session != "" {
		var session headers.Session
		if session.Unmarshal(r.Header["Session"]) != nil || session.Session != p.session {
			res.StatusCode = base.StatusSessionNotFound
			return
		}
		res.Header["Session"] = base.HeaderValue{p.session}
	} else if r.Method != base.Setup {
		res.StatusCode = base.StatusSessionNotFound
		return
	}
	switch r.Method {
	case base.Setup:
		id, err := strconv.Atoi(strings.TrimPrefix(path, "/replay/trackID="))
		if !isTrack || err != nil || !p.source.tracks[id] {
			res.StatusCode = base.StatusNotFound
			return
		}
		if p.playing {
			res.StatusCode = base.StatusMethodNotValidInThisState
			return
		}
		var transport headers.Transport
		if transport.Unmarshal(r.Header["Transport"]) != nil || transport.Protocol != headers.TransportProtocolTCP ||
			transport.Profile != headers.TransportProfileAVP || transport.InterleavedIDs == nil ||
			transport.Delivery != nil && *transport.Delivery != headers.TransportDeliveryUnicast {
			res.StatusCode = base.StatusUnsupportedTransport
			return
		}
		ids := *transport.InterleavedIDs
		if ids[0] < 0 || ids[1] > 255 || ids[0]+1 != ids[1] {
			res.StatusCode = base.StatusUnsupportedTransport
			return
		}
		for previous, occupied := range p.channels {
			if previous != id && ids[0] <= occupied[1] && occupied[0] <= ids[1] {
				res.StatusCode = base.StatusUnsupportedTransport
				return
			}
		}
		p.channels[id] = ids
		if p.session == "" {
			p.session = uuid.NewString()
		}
		res.Header["Session"] = base.HeaderValue{p.session}
		delivery := headers.TransportDeliveryUnicast
		selected := headers.Transport{Protocol: headers.TransportProtocolTCP, Profile: headers.TransportProfileAVP, Delivery: &delivery, InterleavedIDs: &ids}
		if first, ok := p.source.startPacket[id]; ok {
			selected.SSRC = &first.ssrc
		}
		res.Header["Transport"] = selected.Marshal()
	case base.Play:
		if p.playing || path != "/replay" {
			res.StatusCode = base.StatusMethodNotValidInThisState
			return
		}
		if raw := r.Header["Range"]; len(raw) > 0 {
			var requested headers.Range
			err := requested.Unmarshal(raw)
			span, ok := requested.Value.(*headers.RangeNPT)
			if err != nil || !ok || span.Start != 0 || span.End != nil {
				res.StatusCode = base.StatusInvalidRange
				return
			}
		}
		var info headers.RTPInfo
		for id := range p.channels {
			if first, ok := p.source.startPacket[id]; ok {
				seq := first.seq
				info = append(info, &headers.RTPInfoEntry{URL: p.source.url + "/trackID=" + strconv.Itoa(id), SequenceNumber: &seq, Timestamp: &first.timestamp})
			}
		}
		if len(info) == 0 {
			res.StatusCode = base.StatusMethodNotValidInThisState
			return
		}
		res.Header["RTP-Info"] = info.Marshal()
		res.Header["Range"] = base.HeaderValue{"npt=0-"}
		start = true
	case base.Teardown:
		closeSession = true
	case base.GetParameter:
	default:
		res.StatusCode = base.StatusMethodNotAllowed
		res.Header["Allow"] = base.HeaderValue{replayRTSPMethods}
	}
	return
}

func (p *replayRTSPPeer) send() {
	defer p.wire.Close()
	start := time.Now()
	payload, wire := make([]byte, 65535), make([]byte, 65539)
	for _, frame := range p.source.frames {
		channel, selected := p.channels[frame.media]
		if !selected {
			continue
		}
		if waitReplayAt(p.ctx, start, frame.at-p.source.playbackStart) != nil {
			return
		}
		data := payload[:frame.size]
		if _, err := p.source.file.ReadAt(data, frame.offset); err != nil {
			return
		}
		p.write.Lock()
		_ = p.wire.SetWriteDeadline(time.Now().Add(4 * time.Second))
		err := p.codec.WriteInterleavedFrame(&base.InterleavedFrame{Channel: channel[0], Payload: data}, wire)
		p.write.Unlock()
		if err != nil {
			return
		}
	}
	_ = waitReplayAt(p.ctx, start, p.source.duration)
}

func (p *replayRTSPPeer) writeResponse(res *base.Response) error {
	p.write.Lock()
	defer p.write.Unlock()
	_ = p.wire.SetWriteDeadline(time.Now().Add(4 * time.Second))
	return p.codec.WriteResponse(res)
}
