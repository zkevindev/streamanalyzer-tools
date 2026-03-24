package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/yutopp/go-flv"
	"github.com/yutopp/go-flv/tag"
)

type flvStats struct {
	audioCount    uint64
	videoCount    uint64
	keyCount      uint64
	metadataCount uint64
	metaPrintOnce sync.Once
}

func (s *flvStats) handleTag(flvTag *tag.FlvTag) {
	switch data := flvTag.Data.(type) {
	case *tag.AudioData:
		atomic.AddUint64(&s.audioCount, 1)
		if data.Data != nil {
			data.Close()
		}
	case *tag.VideoData:
		atomic.AddUint64(&s.videoCount, 1)
		if data.FrameType == tag.FrameTypeKeyFrame || data.FrameType == tag.FrameTypeGeneratedKeyFrame {
			atomic.AddUint64(&s.keyCount, 1)
		}
		if data.Data != nil {
			data.Close()
		}
	case *tag.ScriptData:
		atomic.AddUint64(&s.metadataCount, 1)
		s.metaPrintOnce.Do(func() {
			printMetadata(flvTag.Timestamp, data)
		})
	}
}

func (s *flvStats) snapshot() (audio uint64, video uint64, key uint64, metadata uint64) {
	return atomic.LoadUint64(&s.audioCount),
		atomic.LoadUint64(&s.videoCount),
		atomic.LoadUint64(&s.keyCount),
		atomic.LoadUint64(&s.metadataCount)
}

func printMetadata(ts uint32, data *tag.ScriptData) {
	if data == nil {
		fmt.Printf("metadata ts=%d: <nil>\n", ts)
		return
	}

	content, err := json.MarshalIndent(data.Objects, "", "  ")
	if err != nil {
		fmt.Printf("metadata ts=%d: marshal failed: %v, raw=%#v\n", ts, err, data.Objects)
		return
	}
	fmt.Printf("metadata ts=%d:\n%s\n", ts, string(content))
}

func main() {
	var (
		flvURL   string
		duration time.Duration
		tz       string
	)
	flag.StringVar(&flvURL, "url", "", "HTTP-FLV URL, e.g. http://127.0.0.1/live/stream.flv")
	flag.DurationVar(&duration, "duration", 0, "Optional test duration, e.g. 30s. 0 means run until Ctrl+C")
	flag.StringVar(&tz, "tz", "Local", "Timezone for log timestamps, e.g. Local, Asia/Shanghai, UTC")
	flag.Parse()

	if flvURL == "" {
		fmt.Fprintln(os.Stderr, "missing required -url")
		os.Exit(2)
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid -tz value %q: %v\n", tz, err)
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, flvURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build request failed: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("User-Agent", "FLVAnalyzerExample/1.0")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "http request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "http status not ok: %d\n", resp.StatusCode)
		os.Exit(1)
	}

	decoder, err := flv.NewDecoder(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create flv decoder failed: %v\n", err)
		os.Exit(1)
	}

	stats := &flvStats{}
	fmt.Printf("pulling http-flv from %s\n", flvURL)
	fmt.Printf("press Ctrl+C to stop, or wait for -duration (tz=%s)\n", loc.String())

	doneCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				doneCh <- fmt.Errorf("panic while decoding flv tag: %v", r)
			}
		}()

		for {
			flvTag := &tag.FlvTag{}
			if err := decoder.Decode(flvTag); err != nil {
				doneCh <- err
				return
			}
			stats.handleTag(flvTag)
		}
	}()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			audio, video, key, metadata := stats.snapshot()
			fmt.Printf("[%s] done: %v, audio=%d video=%d key=%d metadata=%d\n", nowIn(loc), ctx.Err(), audio, video, key, metadata)
			return
		case err := <-doneCh:
			audio, video, key, metadata := stats.snapshot()
			if err == io.EOF {
				fmt.Printf("[%s] stream ended, audio=%d video=%d key=%d metadata=%d\n", nowIn(loc), audio, video, key, metadata)
				return
			}
			fmt.Printf("[%s] decode error: %v, audio=%d video=%d key=%d metadata=%d\n", nowIn(loc), err, audio, video, key, metadata)
			return
		case <-ticker.C:
			audio, video, key, metadata := stats.snapshot()
			fmt.Printf("[%s] stats: audio=%d video=%d key=%d metadata=%d\n", nowIn(loc), audio, video, key, metadata)
		}
	}
}

func nowIn(loc *time.Location) string {
	return time.Now().In(loc).Format("2006-01-02 15:04:05.000 MST")
}
