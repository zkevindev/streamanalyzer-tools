package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/yutopp/go-flv/tag"
	"github.com/yutopp/go-rtmp"
	rtmpmsg "github.com/yutopp/go-rtmp/message"
)

type pullHandler struct {
	rtmp.DefaultHandler
	audioCount   uint64
	videoCount   uint64
	keyCount     uint64
	metaCount    uint64
	metaPrintOnce sync.Once
}

func (h *pullHandler) OnAudio(timestamp uint32, payload io.Reader) error {
	var audio tag.AudioData
	if err := tag.DecodeAudioData(payload, &audio); err != nil {
		return err
	}
	atomic.AddUint64(&h.audioCount, 1)
	return nil
}

func (h *pullHandler) OnVideo(timestamp uint32, payload io.Reader) error {
	var video tag.VideoData
	if err := tag.DecodeVideoData(payload, &video); err != nil {
		return err
	}
	atomic.AddUint64(&h.videoCount, 1)
	if video.FrameType == tag.FrameTypeKeyFrame || video.FrameType == tag.FrameTypeGeneratedKeyFrame {
		atomic.AddUint64(&h.keyCount, 1)
	}
	return nil
}

func (h *pullHandler) OnSetDataFrame(timestamp uint32, data *rtmpmsg.NetStreamSetDataFrame) error {
	atomic.AddUint64(&h.metaCount, 1)
	h.metaPrintOnce.Do(func() {
		h.printMetadata(timestamp, data)
	})
	return nil
}

func (h *pullHandler) OnUnknownDataMessage(timestamp uint32, data *rtmpmsg.DataMessage) error {
	// Many RTMP origins send metadata as "onMetaData" instead of "@setDataFrame".
	if data != nil && (data.Name == "onMetaData" || data.Name == "@setDataFrame") {
		atomic.AddUint64(&h.metaCount, 1)
	}
	return nil
}

func (h *pullHandler) printMetadata(timestamp uint32, data *rtmpmsg.NetStreamSetDataFrame) {
	if data == nil {
		fmt.Printf("metadata ts=%d: <nil>\n", timestamp)
		return
	}

	content, err := json.MarshalIndent(data.AmfData, "", "  ")
	if err != nil {
		fmt.Printf("metadata ts=%d: marshal failed: %v, raw=%#v\n", timestamp, err, data.AmfData)
		return
	}

	fmt.Printf("metadata ts=%d:\n%s\n", timestamp, string(content))
}

func (h *pullHandler) Stats() (audio uint64, video uint64, key uint64, meta uint64) {
	return atomic.LoadUint64(&h.audioCount),
		atomic.LoadUint64(&h.videoCount),
		atomic.LoadUint64(&h.keyCount),
		atomic.LoadUint64(&h.metaCount)
}

func main() {
	var (
		rtmpURL  string
		duration time.Duration
		tz       string
	)
	flag.StringVar(&rtmpURL, "url", "", "RTMP URL, e.g. rtmp://127.0.0.1/live/stream")
	flag.DurationVar(&duration, "duration", 0, "Optional test duration, e.g. 30s. 0 means run until Ctrl+C")
	flag.StringVar(&tz, "tz", "Local", "Timezone for log timestamps, e.g. Local, Asia/Shanghai, UTC")
	flag.Parse()

	if rtmpURL == "" {
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

	handler := &pullHandler{}
	cc, stream, target, err := rtmp.DialAndPlay(ctx, rtmpURL, handler, &rtmp.PlayOptions{
		ChunkSize: 128,
		Start:     -2,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial and play failed: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = stream.Close()
		_ = cc.Close()
	}()

	fmt.Printf("pulling from host=%s app=%s stream=%s\n", target.Host, target.App, target.StreamName)
	fmt.Printf("press Ctrl+C to stop, or wait for -duration (tz=%s)\n", loc.String())

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			audio, video, key, meta := handler.Stats()
			fmt.Printf("[%s] done: %v, audio=%d video=%d key=%d metadata=%d\n", nowIn(loc), ctx.Err(), audio, video, key, meta)
			return
		case <-ticker.C:
			audio, video, key, meta := handler.Stats()
			fmt.Printf("[%s] stats: audio=%d video=%d key=%d metadata=%d\n", nowIn(loc), audio, video, key, meta)

			if err := cc.LastError(); err != nil {
				fmt.Printf("client closed with error: %v\n", err)
				return
			}
		}
	}
}

func nowIn(loc *time.Location) string {
	return time.Now().In(loc).Format("2006-01-02 15:04:05.000 MST")
}
