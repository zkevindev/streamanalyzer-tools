package onvif

import (
	"context"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/headers"
)

type recordingRTSP struct {
	fixture     *recordingFixture
	profile     *profileVideo
	description *gortsplib.ServerStream
	mu          sync.Mutex
	sessions    map[*gortsplib.ServerSession]*recordingStream
}

type recordingStream struct {
	stream *gortsplib.ServerStream
	media  *description.Media
	cancel context.CancelFunc
}

func newRecordingRTSP(server *gortsplib.Server, profile *profileVideo, fixture *recordingFixture) (*recordingRTSP, error) {
	r := &recordingRTSP{fixture: fixture, profile: profile, sessions: make(map[*gortsplib.ServerSession]*recordingStream)}
	metadata, err := r.newStream(server)
	if err != nil {
		return nil, err
	}
	r.description = metadata.stream
	return r, nil
}

func (r *recordingRTSP) newStream(server *gortsplib.Server) (*recordingStream, error) {
	media := &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{r.profile.avc}}
	stream := &gortsplib.ServerStream{Server: server, Desc: &description.Session{Medias: []*description.Media{media}}}
	if err := stream.Initialize(); err != nil {
		return nil, err
	}
	return &recordingStream{stream: stream, media: media}, nil
}

func (r *recordingRTSP) setup(server *gortsplib.Server, session *gortsplib.ServerSession) (*base.Response, *gortsplib.ServerStream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.sessions[session]
	if p == nil {
		if len(r.sessions) >= 8 {
			return &base.Response{StatusCode: base.StatusServiceUnavailable}, nil, nil
		}
		var err error
		p, err = r.newStream(server)
		if err != nil {
			return &base.Response{StatusCode: base.StatusInternalServerError}, nil, err
		}
		r.sessions[session] = p
	}
	return &base.Response{StatusCode: base.StatusOK}, p.stream, nil
}

func (r *recordingRTSP) remove(session *gortsplib.ServerSession) {
	r.mu.Lock()
	p := r.sessions[session]
	delete(r.sessions, session)
	r.mu.Unlock()
	if p != nil {
		if p.cancel != nil {
			p.cancel()
		}
		p.stream.Close()
	}
}

func (r *recordingRTSP) play(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	info := r.fixture.info
	span := headers.RangeUTC{Start: info.From, End: &info.Until}
	if raw := ctx.Request.Header["Range"]; len(raw) != 0 {
		var request headers.Range
		if request.Unmarshal(raw) != nil {
			return &base.Response{StatusCode: base.StatusInvalidRange}, nil
		}
		value, ok := request.Value.(*headers.RangeUTC)
		if !ok {
			return &base.Response{StatusCode: base.StatusInvalidRange}, nil
		}
		span.Start = value.Start
		if value.End != nil && value.End.Before(*span.End) {
			span.End = value.End
		}
	}
	if span.Start.Before(info.From) || !span.Start.Before(*span.End) || !span.Start.Before(info.Until) {
		r.fixture.missing.Add(1)
		return &base.Response{StatusCode: base.StatusInvalidRange}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.sessions[ctx.Session]
	if p == nil {
		return &base.Response{StatusCode: base.StatusSessionNotFound}, nil
	}
	if p.cancel != nil {
		return &base.Response{StatusCode: base.StatusMethodNotValidInThisState}, nil
	}
	run, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	r.fixture.plays.Add(1)
	go func() {
		defer ctx.Session.Close()
		r.send(run, p, span.Start.Sub(info.From), span.End.Sub(span.Start))
	}()
	value := headers.Range{Value: &span}
	return &base.Response{StatusCode: base.StatusOK, Header: base.Header{"Range": value.Marshal()}}, nil
}

func (r *recordingRTSP) send(ctx context.Context, p *recordingStream, offset, duration time.Duration) {
	const prepDelay = 150 * time.Millisecond
	if !recordingWait(ctx, prepDelay) {
		return
	}
	profile := r.profile
	encoder, err := profile.avc.CreateEncoder()
	if err != nil {
		return
	}
	period := time.Second / time.Duration(profile.FPS)
	index := int(offset/period) % len(profile.frames)
	// Decode starts at the previous intra frame in this loop of the fixture.
	for index > 0 && !recordingIntra(profile.frames[index]) {
		index--
	}
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	start := time.Now()
	var timestamp uint32
	for ctx.Err() == nil && time.Since(start) < duration {
		packets, err := encoder.Encode(profile.frames[index])
		if err != nil {
			return
		}
		for _, packet := range packets {
			packet.Timestamp = timestamp
			if err := p.stream.WritePacketRTP(p.media, packet); err != nil {
				return
			}
		}
		timestamp += uint32(90000 / profile.FPS)
		index = (index + 1) % len(profile.frames)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func recordingIntra(unit [][]byte) bool {
	for _, nalu := range unit {
		if len(nalu) > 0 && nalu[0]&31 == 5 {
			return true
		}
	}
	return false
}

func recordingWait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
