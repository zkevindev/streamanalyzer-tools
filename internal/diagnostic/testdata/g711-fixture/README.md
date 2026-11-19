This native RTSP fixture produces H.264 video and a continuous 440 Hz G.711
signal. Audio is generated and encoded in Go with the same G.711 library used
by the application. The source never starts FFmpeg or a second RTSP session.

`-codec pcma` advertises PCMA/PT 8; `-codec pcmu` advertises PCMU/PT 0. Both use
8 kHz mono, 160 samples per packet, and a continuous RTP clock. The process
prints a JSON line with `url` and `status_url`; the status endpoint counts PLAY
requests and the chosen TCP/UDP transport. SIGTERM closes the source and sockets.

Build the app and fixture, then run the four actual Chrome workflows:

```sh
go build -o /tmp/streamanalyzer-g711-app ./cmd/server
go build -o /tmp/streamanalyzer-g711-fixture ./internal/diagnostic/testdata/g711-fixture
STREAMANALYZER_TEST_SERVER=/tmp/streamanalyzer-g711-app \
STREAMANALYZER_G711_FIXTURE=/tmp/streamanalyzer-g711-fixture \
node internal/handler/testdata/rtsp_g711_e2e.cjs
```

FFmpeg must be available to the app for its explicitly reported G.711-to-AAC
preview conversion. Node 22+ and Chrome are required; set `CHROME_BIN` when
Chrome is not installed at the default macOS path. The test checks actual audio
decoding, video first screen, conversion statistics, one source PLAY, model and
HTML output, and exact report/raw-dump replay after stopping the source.
