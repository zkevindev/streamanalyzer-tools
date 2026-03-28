package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"streamanalyzer/internal/hls"
)

func main() {
	var (
		u        string
		duration time.Duration
	)
	flag.StringVar(&u, "url", "", "HLS m3u8 URL, e.g. http://127.0.0.1/live/stream.m3u8")
	flag.DurationVar(&duration, "duration", 0, "Optional test duration, e.g. 30s. 0 means run until Ctrl+C")
	flag.Parse()

	if u == "" {
		fmt.Fprintln(os.Stderr, "missing required -url")
		os.Exit(2)
	}

	baseCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx := baseCtx
	if duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(baseCtx, duration)
		defer cancel()
	}

	type streamAgg struct {
		Seg, PAT, PMT, PES, VPES, APES int
		URIs                           []string
	}
	var (
		mu   sync.Mutex
		aggs = make(map[string]*streamAgg)
	)
	client := &hls.Client{
		PlaylistURL: u,
		OnSegment: func(s hls.SegmentStats) {
			mu.Lock()
			agg := aggs[s.StreamID]
			if agg == nil {
				agg = &streamAgg{URIs: make([]string, 0, 128)}
				aggs[s.StreamID] = agg
			}
			agg.Seg++
			agg.PAT += s.PATCount
			agg.PMT += s.PMTCount
			agg.PES += s.PESCount
			agg.VPES += s.VideoPES
			agg.APES += s.AudioPES
			agg.URIs = append(agg.URIs, s.URI)
			mu.Unlock()

			fmt.Printf("[stream=%s seg=%d] uri=%s dur=%.3fs size=%dB PAT=%d PMT=%d PES=%d (video=%d audio=%d)\n",
				s.StreamID, s.Seq, s.URI, s.DurationSec, s.SizeBytes, s.PATCount, s.PMTCount, s.PESCount, s.VideoPES, s.AudioPES)
		},
	}

	fmt.Printf("pulling hls from %s\n", u)
	err := client.Run(ctx)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		fmt.Fprintf(os.Stderr, "hls client error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("done: %v, streams=%d\n", ctx.Err(), len(aggs))
	for streamID, a := range aggs {
		fmt.Printf("[summary stream=%s] segments=%d PAT=%d PMT=%d PES=%d (video=%d audio=%d)\n",
			streamID, a.Seg, a.PAT, a.PMT, a.PES, a.VPES, a.APES)
		if len(a.URIs) > 0 {
			fmt.Printf("[uris stream=%s] (%d)\n%s\n", streamID, len(a.URIs), strings.Join(a.URIs, "\n"))
		}
	}
}
