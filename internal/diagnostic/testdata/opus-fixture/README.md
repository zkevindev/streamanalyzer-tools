The JSON fixture packets are a generated stereo 440 Hz sine, encoded once with
libopus at 64 kbit/s using FFmpeg. The second encoded packet was retained for
each of the 2.5, 5, 10, 20, 40, 60 and 120 ms frame-duration settings. No encoder
is invoked by the fixture server or by production preview.

The source produces H.264 test-pattern RTP and the original Opus payloads on
one native RTSP session, over TCP or UDP. `-mode variants` cycles all durations
and deliberately leaves a 200 ms audio DTX gap after two seconds. `/status`
counts PLAY requests so browser tests can detect an accidental second pull.

Build with:

```sh
go build -o /tmp/streamanalyzer-opus-fixture ./internal/diagnostic/testdata/opus-fixture
```
