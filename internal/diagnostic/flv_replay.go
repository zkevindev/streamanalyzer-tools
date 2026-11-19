package diagnostic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type flvReplayBoundary struct {
	end int64
	at  int64
}

func canReplayFLV(manifest Manifest) bool {
	return manifest.Protocol == ProtocolHTTPFLV && manifest.Stream.Bytes > 13 && manifest.Timeline.Bytes > 0
}

// StopFLVReplays interrupts local replays when the HTTP server shuts down.
func (s *Service) StopFLVReplays() {
	s.cancelFLVReplays()
}

// ServeFLVReplay serves a finalized run's dump at the recorded tag arrival
// times. Each reader starts its own playback clock.
func (s *Service) ServeFLVReplay(w http.ResponseWriter, r *http.Request, id string) error {
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(s.flvReplayContext, cancel)
	defer stop()
	defer cancel()
	if err := s.flvReplayContext.Err(); err != nil {
		return err
	}
	select {
	case s.flvReplaySlots <- struct{}{}:
		defer func() { <-s.flvReplaySlots }()
	default:
		return ErrBusy
	}

	run, err := s.getRun(id)
	if err != nil {
		return err
	}
	if run.Protocol != ProtocolHTTPFLV {
		return ErrNotFound
	}
	if !run.ReportReady {
		return ErrPending
	}
	dir := s.runDir(run.ID)
	report, err := readJSONFile[Report](dir, reportName)
	if err != nil {
		return err
	}
	manifest := report.Manifest
	if !canReplayFLV(manifest) {
		return fmt.Errorf("%w: 该记录没有可回放的 HTTP-FLV 标签和到达时间", ErrInvalid)
	}
	events, err := readFLVReplayTimeline(ctx, dir, manifest)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return fmt.Errorf("%w: 该记录没有可回放的 HTTP-FLV 标签和到达时间", ErrInvalid)
	}
	wire, err := os.Open(filepath.Join(dir, streamName))
	if err != nil {
		return err
	}
	defer wire.Close()
	stat, err := wire.Stat()
	if err != nil {
		return err
	}
	if stat.Size() != manifest.Stream.Bytes {
		return fmt.Errorf("%w: 原始流大小与证据记录不一致", ErrInvalid)
	}
	plan := planFLVReplay(events, manifest)
	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	conn := http.NewResponseController(w)
	if err := conn.Flush(); err != nil {
		return err
	}
	start := time.Now()
	var position int64
	for _, part := range plan {
		if err := waitReplayAt(ctx, start, part.at); err != nil {
			return err
		}
		if part.end == position {
			continue
		}
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := io.CopyN(w, wire, part.end-position); err != nil {
			return err
		}
		if err := conn.Flush(); err != nil {
			return err
		}
		_ = conn.SetWriteDeadline(time.Time{})
		position = part.end
	}
	return nil
}

func planFLVReplay(events []Event, manifest Manifest) []flvReplayBoundary {
	firstAt := events[0].ArrivalMS
	if observed := manifest.Observation.FirstPayloadMS; observed != nil {
		firstAt = min(firstAt, max(0, *observed))
	}
	plan := []flvReplayBoundary{{end: events[0].Offset, at: firstAt}}
	for _, event := range events {
		boundary := flvReplayBoundary{end: event.Offset + int64(event.Size), at: event.ArrivalMS}
		if plan[len(plan)-1].at == boundary.at {
			plan[len(plan)-1] = boundary
		} else {
			plan = append(plan, boundary)
		}
	}
	end := manifest.Observation.ElapsedMS
	for _, stage := range manifest.Observation.Stages {
		if stage.Name == "receive" {
			end = stage.EndMS
		}
	}
	end = max(end, plan[len(plan)-1].at)
	// The capture may end in the middle of a tag. Preserve those bytes and
	// wait through the last observed receive interval before ending the body.
	plan = append(plan, flvReplayBoundary{end: manifest.Stream.Bytes, at: end})
	return plan
}

func waitReplayAt(ctx context.Context, start time.Time, offsetMS int64) error {
	delay := time.Duration(offsetMS)*time.Millisecond - time.Since(start)
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
