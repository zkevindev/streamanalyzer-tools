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
	requestedEnd := info.Until
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
		if value.End != nil {
			requestedEnd = *value.End
			if value.End.Before(*span.End) {
				span.End = value.End
			}
		}
	}
	trace := &RecordingPlayTrace{Requested: RecordingSpan{From: span.Start, Until: requestedEnd}}
	parts := r.fixture.parts(RecordingSpan{From: span.Start, Until: *span.End})
	if len(parts) == 0 {
		trace.Code = int(base.StatusInvalidRange)
		r.fixture.playTrace(trace)
		r.fixture.missing.Add(1)
		return &base.Response{StatusCode: base.StatusInvalidRange}, nil
	}
	returned := RecordingSpan{From: parts[0].From, Until: parts[len(parts)-1].Until}
	trace.Returned, trace.Parts, trace.Code = &returned, parts, int(base.StatusOK)
	span = headers.RangeUTC{Start: returned.From, End: &returned.Until}
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
	r.fixture.playTrace(trace)
	r.fixture.plays.Add(1)
	go func() {
		defer ctx.Session.Close()
		r.send(run, p, trace)
	}()
	value := headers.Range{Value: &span}
	return &base.Response{StatusCode: base.StatusOK, Header: base.Header{"Range": value.Marshal()}}, nil
}

func (r *recordingRTSP) send(ctx context.Context, p *recordingStream, trace *RecordingPlayTrace) {
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
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	var timestamp uint32
	for _, part := range trace.Parts {
		index := int(part.From.Sub(r.fixture.info.From)/period) % len(profile.frames)
		// The first frame in each emitted part must be independently decodable.
		for index > 0 && !recordingIntra(profile.frames[index]) {
			index--
		}
		wroteKeyframe := false
		for elapsed := time.Duration(0); elapsed < part.Until.Sub(part.From); elapsed += period {
			unit := profile.frames[index]
			packets, err := encoder.Encode(unit)
			if err != nil {
				return
			}
			for _, packet := range packets {
				packet.Timestamp = timestamp
				if err := p.stream.WritePacketRTP(p.media, packet); err != nil {
					return
				}
			}
			if !wroteKeyframe && len(packets) > 0 && recordingIntra(unit) {
				r.fixture.mu.Lock()
				trace.Keyframes = append(trace.Keyframes, time.Now().UTC())
				r.fixture.mu.Unlock()
				wroteKeyframe = true
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
