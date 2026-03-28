# Examples

服务端启动后，也可在浏览器打开首页 → **离线分析** → 选择 **mp4** 模式上传，产物路径与 `summary.json` 说明见仓库根目录 `README.md` 的「离线接口」小节。

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

## HLS Client (m3u8 / TS segments)

拉取 HLS playlist，逐 **MPEG-TS** 切片解析（与实时分析 `type: hls` 使用同一 `internal/hls` 逻辑）。不支持带 `#EXT-X-MAP` 的 fMP4。

```bash
go run ./example/hls_client -url "http://127.0.0.1/live/stream.m3u8"
```

限时运行：

```bash
go run ./example/hls_client -url "http://127.0.0.1/live/stream.m3u8" -duration 30s
```

## MP4 文件解析（stsd + meta + 样例时间线 + 可选导出）

实现位于 **`internal/mp4parse`**，命令行示例与本仓库 **Web 离线分析**（模式选 **`mp4`**）共用同一套逻辑：离线任务会在 `offline/<taskId>/mp4/` 下生成 `mp4_report.txt`、`video.annexb`、`audio.adts.aac`，并在 `summary.json` 中附带帧明细。

使用 [github.com/abema/go-mp4](https://github.com/abema/go-mp4)，默认输出：

1. **`stsd` 子树**：各轨 `avc1`/`avcC`、`hvc1`/`hev1`/`hvcC`、`mp4a`/`esds` 等采样描述（分辨率、Profile/Level、声道、码率盒等）。
2. **`meta` 子树**：`hdlr`、`ilst` 与 `data`（如 `©nam`、`©too` 等）；若存在 **`keys`** 且 `KeyNamespace` 为 **`mdta`**，会额外打印 `[mdta key] …`（常见于 Android 写入的自定义元数据）。
3. **样例时间线**：由 `stts` + `ctts` + `stsz` + `stss` 推导每条 sample 的 **DTS/PTS**（媒体时间基 tick 与秒）、**size**；视频轨根据 **stss** 标 `I(stss)` / `non-I`，音频轨标 `audio`。

可选 **`-video` / `-audio`**（与 `parse_flv` 一致）：从 **`mdat`** 按 chunk/sample 读取裸帧；**`-video`** 写出 **Annex-B** 裸流：**H.264**（**`avc1`+`avcC`**）或 **H.265/HEVC**（**`hvc1`/`hev1`+`hvcC`**）；**`-audio`** 写出 **ADTS AAC**（**`mp4a`** 且需 **`esds`** 中的 AudioSpecificConfig）。多视频/多音轨时只导出第一条匹配轨。

```bash
go run ./example/parse_mp4 -i /path/to/bbb_30fps_gop_60_3mbps.mp4
```

默认每轨只打印前 **40** 条 sample（可用 `-n 0` 打全量，大文件慎用）：

```bash
go run ./example/parse_mp4 -i ./your.mp4 -n 12
```

导出示例：

```bash
go run ./example/parse_mp4 -i ./your.mp4 -video ./out.264 -audio ./out.aac
```

`-video` 会从 **`avcC`/`hvcC`** 解析参数 NAL，转成 Annex B，在 **第 1 个 sample 前** 写一次（流头即为 SPS，便于 FFmpeg 探测），并在 **`stss` 标出的每个 I 帧前** 再写一次，便于随机访问。一般可直接 `ffplay ./out.raw`；若仍被误判，可用 `ffplay -f h264` / `-f hevc`。

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
