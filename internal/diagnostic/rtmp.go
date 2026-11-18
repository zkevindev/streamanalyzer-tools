package diagnostic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"streamanalyzer/internal/rtmpraw"

	"github.com/yutopp/go-rtmp"
	"github.com/yutopp/go-rtmp/message"
)

type rtmpReadStamp struct {
	EndOffset int64 `json:"end_offset"`
	ArrivalMS int64 `json:"arrival_ms"`
}

// rtmpTap records receive times before RTMP parsing or handoff to a handler.
// Read calls in the same millisecond share a stamp for all of their bytes.
type rtmpTap struct {
	stream *artifactWriter
	start  time.Time
	cancel context.CancelFunc
	stamps []rtmpReadStamp
}

func (t *rtmpTap) Write(data []byte) (int, error) {
	arrival := time.Since(t.start).Milliseconds()
	limit := int(min(int64(len(data)), maxStreamBytes-t.stream.bytes))
	if limit == 0 {
		return 0, io.ErrShortWrite
	}
	n, err := t.stream.Write(data[:limit])
	if n > 0 {
		stamp := rtmpReadStamp{EndOffset: t.stream.bytes, ArrivalMS: arrival}
		if last := len(t.stamps) - 1; last >= 0 && t.stamps[last].ArrivalMS == arrival {
			t.stamps[last] = stamp
		} else {
			t.stamps = append(t.stamps, stamp)
		}
	}
	if err != nil || t.stream.bytes == maxStreamBytes {
		t.cancel()
	}
	if n != len(data) && err == nil {
		err = io.ErrShortWrite
	}
	return n, err
}

func sampleRTMP(ctx context.Context, dir string, target *url.URL, seconds int) (Report, error) {
	stream, err := newArtifact(dir, rtmpStreamName)
	if err != nil {
		return Report{}, err
	}
	timeline, err := newArtifact(dir, timelineName)
	if err != nil {
		return Report{}, errors.Join(err, stream.close())
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	tap := rtmpTap{stream: stream, start: start, cancel: cancel}
	obs := Observation{
		Origin: origin(target), StartedAt: start.UTC(), RequestedSeconds: seconds,
	}
	obs.EndReason = captureRTMP(ctx, target, &tap, &obs)
	obs.ElapsedMS = time.Since(start).Milliseconds()
	if len(tap.stamps) > 0 {
		obs.TTFBMS = &tap.stamps[0].ArrivalMS
		obs.FirstPayloadMS = &tap.stamps[0].ArrivalMS
	}
	var timelineErr error
	encoder := json.NewEncoder(timeline)
	for _, stamp := range tap.stamps {
		if timelineErr = encoder.Encode(stamp); timelineErr != nil {
			break
		}
	}
	if err := errors.Join(stream.err, stream.close(), timelineErr, timeline.close()); err != nil {
		return Report{}, err
	}
	manifest := Manifest{
		Version: multiProtocolSchemaVersion, Protocol: ProtocolRTMP, Observation: obs,
		Stream: stream.artifact(), Timeline: timeline.artifact(),
	}
	return replayRTMP(dir, manifest)
}

func captureRTMP(ctx context.Context, target *url.URL, tap *rtmpTap, obs *Observation) string {
	setup := &rtmpSetupTiming{start: tap.start}
	ctx = setup.context(ctx)
	client, _, _, err := rtmp.DialAndPlay(ctx, target.String(), &rtmp.DefaultHandler{}, &rtmp.PlayOptions{Start: -2, RecvTap: tap, OnStage: setup.completed})
	playEnd := time.Since(tap.start).Milliseconds()
	setup.finish(obs, err)
	if err != nil && ctx.Err() != nil {
		obs.FailureCode = modelNetworkError(ctx.Err(), 0).Code
	}
	reason := "rtmp_error"
	if err == nil {
		// DialAndPlay closes the socket on cancellation. The receive loop
		// must finish before the tap and raw artifact can be closed.
		<-client.Done()
		obs.Stages = append(obs.Stages, CaptureStage{Name: "receive", StartMS: playEnd, EndMS: time.Since(tap.start).Milliseconds()})
		if errors.Is(client.LastError(), rtmp.ErrServerDisconnected) {
			reason = "eof"
		} else if ctx.Err() == nil {
			obs.FailureStage, obs.FailureCode = "receive", "rtmp_error"
		}
		_ = client.Close()
	}
	if tap.stream.bytes == maxStreamBytes {
		return "size_limit"
	}
	return captureEnd(ctx, reason)
}

func replayRTMP(dir string, manifest Manifest) (Report, error) {
	stamps, err := loadRTMPStamps(dir, manifest)
	if err != nil {
		return Report{}, err
	}
	stream, err := os.Open(filepath.Join(dir, rtmpStreamName))
	if err != nil {
		return Report{}, err
	}
	defer stream.Close()

	events, err := parseRTMPCapture(bufio.NewReaderSize(stream, 64<<10), stamps)
	switch {
	case errors.Is(err, errRTMPTagLimit):
		if manifest.Observation.EndReason == "eof" || manifest.Observation.EndReason == "duration_limit" {
			manifest.Observation.EndReason = "tag_limit"
		}
	case err != nil && manifest.Observation.EndReason == "eof":
		// A connection EOF can also happen in the middle of an RTMP chunk.
		manifest.Observation.EndReason = "rtmp_error"
	}
	report := analyze(manifest, events)
	report.RuleVersion = "rtmp/2"
	return report, nil
}

func loadRTMPStamps(dir string, manifest Manifest) ([]rtmpReadStamp, error) {
	file, err := os.Open(filepath.Join(dir, timelineName))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	var stamps []rtmpReadStamp
	last := rtmpReadStamp{ArrivalMS: -1}
	for {
		var stamp rtmpReadStamp
		if err := decoder.Decode(&stamp); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%w: RTMP 时间线记录无效", ErrInvalid)
		}
		if stamp.EndOffset <= last.EndOffset || stamp.EndOffset > manifest.Stream.Bytes ||
			stamp.ArrivalMS <= last.ArrivalMS || stamp.ArrivalMS > manifest.Observation.ElapsedMS {
			return nil, fmt.Errorf("%w: RTMP 时间线与原始流不一致", ErrInvalid)
		}
		stamps = append(stamps, stamp)
		last = stamp
	}
	ttfb := manifest.Observation.TTFBMS
	if last.EndOffset != manifest.Stream.Bytes || (len(stamps) == 0 && ttfb != nil) ||
		(len(stamps) != 0 && (ttfb == nil || *ttfb != stamps[0].ArrivalMS)) {
		return nil, fmt.Errorf("%w: RTMP 时间线长度或首字节记录不匹配", ErrInvalid)
	}
	return stamps, nil
}

var errRTMPTagLimit = errors.New("RTMP message or media event limit reached")

func parseRTMPCapture(stream io.Reader, stamps []rtmpReadStamp) ([]Event, error) {
	var handshake [rtmpraw.DefaultHandshakeSkipBytes]byte
	if _, err := io.ReadFull(stream, handshake[:]); err != nil {
		return nil, incompleteTag(err)
	}
	if handshake[0] != 3 {
		return nil, errors.New("invalid RTMP handshake version")
	}

	streamer := rtmp.NewChunkStreamer(stream, io.Discard, &rtmp.StreamControlStateConfig{MaxChunkStreams: 128})
	defer streamer.Close()
	var events []Event
	stampIndex := 0
	for i := 0; i < 2*maxTags; i++ {
		packet, err := streamer.ReadPayload()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return events, nil
			}
			return events, err
		}
		end := int64(packet.EndOffset) + int64(len(handshake))
		for stampIndex < len(stamps) && stamps[stampIndex].EndOffset < end {
			stampIndex++
		}
		if stampIndex >= len(stamps) {
			return events, errors.New("RTMP message exceeds the captured byte range")
		}
		event := Event{
			Offset: int64(packet.StartOffset) + int64(len(handshake)), Size: int(packet.EndOffset - packet.StartOffset),
			ArrivalMS: stamps[stampIndex].ArrivalMS, DTS: int64(packet.Timestamp), PTS: int64(packet.Timestamp),
		}
		switch packet.TypeID {
		case message.TypeIDAudioMessage:
			decodeAudio(bytes.NewReader(packet.Payload), &event)
			events = append(events, event)
		case message.TypeIDVideoMessage:
			decodeVideo(bytes.NewReader(packet.Payload), &event)
			events = append(events, event)
		case message.TypeIDAggregateMessage:
			events, err = appendRTMPAggregate(events, packet.Payload, event)
			if err != nil {
				return events, err
			}
		}
		if len(events) >= maxTags {
			return events, errRTMPTagLimit
		}
	}
	return events, errRTMPTagLimit
}

// Aggregate messages carry FLV tags without an FLV file header. Their first
// tag is anchored to the enclosing RTMP message timestamp.
func appendRTMPAggregate(events []Event, payload []byte, messageEvent Event) ([]Event, error) {
	parser := flvReader{r: bytes.NewReader(payload)}
	var firstDTS int64
	for first := true; len(events) < maxTags; first = false {
		event, err := parser.next()
		if errors.Is(err, io.EOF) {
			return events, nil
		}
		if err != nil {
			return events, err
		}
		if first {
			firstDTS = event.DTS
		}
		offset := messageEvent.DTS - firstDTS
		event.DTS += offset
		event.PTS += offset
		event.Offset, event.Size, event.ArrivalMS = messageEvent.Offset, messageEvent.Size, messageEvent.ArrivalMS
		if event.Kind != "script" && event.Kind != "unsupported_tag" {
			events = append(events, event)
		}
	}
	return events, errRTMPTagLimit
}
