# RTSP stall evidence fixture

Build with `go build -o /tmp/rtsp-stall-fixture ./internal/diagnostic/testdata/rtsp-stall-fixture`, then run the binary without arguments. The first stdout line is JSON with `url` and `status_url`. Ports are allocated on loopback; the status endpoint reports TCP/UDP PLAY counts. SIGTERM shuts down the source and all listeners.

The source sends the embedded 320×180, 15 fps H.264 test pattern and mono 48 kHz AAC. After three seconds of media it pauses both RTP tracks for four seconds, then resumes with contiguous RTP timestamps and sequence numbers. Both tracks share one schedule. Arrival offsets intentionally diverge from media time; an implementation that substitutes arrival offsets for media PTS must not pass. Playback uses pre-encoded bytes, with no runtime encoder and no second source pull.

`tone.aac` is a one-second 440 Hz test tone generated with:

```sh
ffmpeg -f lavfi -i sine=frequency=440:sample_rate=48000 -t 1 -ac 1 -c:a aac -b:a 64k -f adts tone.aac
```

The browser regression is `internal/handler/testdata/rtsp_stall_e2e.cjs`. It stops this source before any media investigation, proves MP4 playback and JPEG decoding, investigates an imported raw bundle again, and verifies unchanged report, raw bytes and all four derived assets through a second bundle import. It checks exactly one source PLAY for each TCP/UDP capture.
