# Examples

## RTMP Pull Client

Run:

```bash
go run ./example/rtmp_pull_client -url "rtmp://127.0.0.1/live/stream"
```

Run with timeout:

```bash
go run ./example/rtmp_pull_client -url "rtmp://127.0.0.1/live/stream" -duration 30s
```

## HTTP-FLV Client

Run:

```bash
go run ./example/http_flv_client -url "http://127.0.0.1/live/stream.flv"
```

Run with timeout and timezone:

```bash
go run ./example/http_flv_client -url "http://127.0.0.1/live/stream.flv" -duration 30s -tz Asia/Shanghai
```

## FLV File Parser (Tag / Metadata / Raw Extract)

Parse a local FLV file, validate FLV header, print tag-level info (including metadata),
and optionally extract raw video/audio.

```bash
go run ./example/parse_flv \
  -i ./input.flv \
  -video ./video.annexb \
  -audio ./audio.adts.aac
```

Notes:
- If FLV header is missing or invalid, parser exits with an error immediately.
- Video extraction outputs Annex-B elementary stream.
- Audio extraction currently focuses on AAC and outputs ADTS.
- Omit `-video` / `-audio` to run in parse-only mode.

## RTMP Raw Parser (AnnexB / ADTS)

Parse a single-direction RTMP raw TCP capture (contains S0+S1+S2 handshake by default).

```bash
go run ./example/parse_rtmp \
  -in tcp8.raw \
  -video video.annexb.h264 \
  -audio audio.adts.aac \
  -skip 3073
```

Notes:
- `-skip` defaults to `1 + 1536 + 1536` bytes (S0+S1+S2).
- 该示例仅用于 RTMP raw（包含握手与 RTMP chunk 交互），不适用于 HTTP-FLV 或 TS。
- Output video is H.264 AnnexB, audio is AAC ADTS.

## RTMP Wireshark PCAP Parser (push / pull)

Extract candidate RTMP flows from a Wireshark pcap, dump directional raw streams, and parse media.

```bash
go run ./example/parse_rtmp_from_wireshark \
  -pcap 11935.pcap \
  -serverPort 11935 \
  -outDir example/parse_rtmp_from_wireshark/out \
  -skip 3073
```

Notes:
- It will output results under `outDir/push/` and `outDir/pull/` (plus `unknown` entries if direction can't be detected).
- 该示例仅支持 RTMP 协议：pcap 需要包含 RTMP 握手与 chunk 交互，才可以正常解析。

## MPEG-TS Parser (PAT / PMT / PES / NALU)

Parse TS files with `go-astits`, export ES by PID, and print PES-level details including `PTS/DTS`.

```bash
go run ./example/parse_ts \
  -i ./example/parse_ts/seg-00000.ts \
  -o ./output \
  -print-pat=true \
  -print-pmt=true \
  -print-pes=true \
  -parse-nalu=true
```

Notes:
- Supports parsing `PAT/PMT/PES` and exporting per-PID ES output files.
- For video streams (H.264/H.265), it parses Annex-B NALU info (type/length/key).
- End-of-stream (`astits: no more packets`) is treated as normal EOF.
