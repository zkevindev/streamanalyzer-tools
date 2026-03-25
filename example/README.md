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
- 离线模式当前仅支持 RTMP 协议（包含握手与 RTMP chunk 交互），不支持直接解析 HTTP-FLV 等其它封装。
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
- 同样仅支持 RTMP 协议：pcap 需要包含 RTMP 握手与 chunk 交互，才可以正常解析。
