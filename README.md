# Stream Analyzer Tools

音视频流分析工具：支持 **RTMP / HTTP-FLV / HLS（m3u8 + MPEG-TS 切片）** 拉流的实时分析，以及离线文件解析（raw / pcap / FLV / MPEG-TS / **MP4**）。离线结果会持久化到磁盘并在网页端可查看/下载。兼容 Legacy FLV 与 Enhanced RTMP/FLV（E-RTMP）头部解析。

## 功能特性

- 支持 `http-flv`、`rtmp` 与 **`hls`** 三种实时任务类型（HLS 为 **TS 切片维度**，与 FLV/RTMP 的帧级秒级图表不同）
- **HLS 实时分析**：拉取 media playlist，逐 `.ts` 切片统计 URI、playlist 序号、时长、体积；从 PES 解析音视频 **PTS/DTS（90 kHz）**、PAT/PMT/PES 计数；切片内首视频 PTS 与首音频 PTS 之差（AV）。若 PES 头仅有 PTS、无 DTS 字段，则展示时 **DTS 按与 PTS 相同处理**（常见于 HLS 切片）
- 支持 Legacy 与 Enhanced（ExHeader/FourCC）两种 FLV 头部模式
- 帧级数据采集：`DTS` / `PTS` / 帧类型 / GOP / 码率 / 接收 FPS
- 秒级聚合：每秒心跳行（无数据秒也会记录，便于发现抖动/断流）
- 编解码信息解析：
  - H.264/H.265 SequenceHeader 解析宽高（H.264 额外解析 VUI 帧率）
  - AAC SequenceHeader 解析采样率与声道
  - Enhanced FourCC 基础识别（如 `avc1/hvc1/av01`、`mp4a/Opus`）
- Metadata 解析与展示（`onMetaData`）
- XLSX 输出（每任务一个文件，多工作表）
- 前端 ECharts（CDN）+ 原始数据表格联动展示
- 时间戳视图按真实时间轴对齐（非序号对齐），避免音视频不同帧率造成错位观感
- AV 同步偏差视图（`video_dts - nearest_audio_dts`），含分级着色（绿/黄/橙/红）
- 码率/FPS 支持从拉流开始的全量展示（相对秒轴 + 缩放）
- 图表表格支持全量数据展示（滚动查看）
- 离线文件解析支持：
  - RTMP：`raw` 单方向（含握手）与 `pcap` 自动提取 push/pull 方向
  - FLV：解析 FLV Header/Tag/metadata，提取 `video.annexb` 与 `audio.adts.aac`
  - MP4：解析 `stsd`/`meta`/样例时间线，生成 `mp4/mp4_report.txt`，提取 `video.annexb` 与 `audio.adts.aac`（与 `example/parse_mp4` 共用 `internal/mp4parse`）
  - MPEG-TS：解析 `PAT/PMT/PES`，导出各 PID 的 ES 文件，支持 `PTS/DTS` 与视频 NALU 明细展示
- 离线解析任务支持状态轮询与历史记录（`pending/running/done/failed`）

## 项目结构

```text
.
├── cmd/server/main.go
├── internal/
│   ├── analyzer/
│   │   ├── flv_client.go
│   │   ├── rtmp_client.go
│   │   ├── hls_task.go
│   │   └── task.go
│   ├── hls/
│   │   └── client.go
│   ├── codec/
│   │   ├── aac.go
│   │   ├── bitreader.go
│   │   ├── h264.go
│   │   └── h265.go
│   ├── handler/
│   │   ├── handler.go
│   │   ├── embed.go
│   │   └── static/
│   │       ├── echarts_helpers.js
│   │       └── index.html
│   ├── models/models.go
│   ├── offline/
│   │   ├── offline_manager.go
│   │   ├── offline_raw.go
│   │   ├── offline_pcap.go
│   │   ├── offline_flv.go
│   │   ├── offline_mp4.go
│   │   └── offline_ts.go
│   ├── mp4parse/
│   │   └── mp4parse.go …（MP4 解析与导出，供离线任务与 example/parse_mp4 使用）
│   ├── pcaprtmp/pcaprtmp.go
│   ├── rtmpraw/parser.go
│   └── storage/csv.go
├── third-party/
│   ├── go-rtmp/
│   └── go-flv/
├── example/
│   ├── http_flv_client/
│   ├── rtmp_pull_client/
│   ├── parse_rtmp/
│   ├── parse_rtmp_from_wireshark/
│   ├── parse_ts/
│   ├── hls_client/
│   └── parse_mp4/
└── data/
```

## 安装与运行

```bash
go mod tidy
go build -o bin/server ./cmd/server
./bin/server -config config.yaml
```

默认地址由 `config.yaml` 控制（当前默认 `:8087`），启动后访问：

- [http://localhost:8087](http://localhost:8087)

路由说明：
- `GET /`：入口页（浏览器标题 **Stream Analyzer · 首页**；选择实时或离线分析）
- `GET /realtime`：实时拉流分析（标题 **实时拉流分析 · Stream Analyzer**）
- `GET /offline`：离线文件解析（标题 **离线文件解析 · Stream Analyzer**；支持 raw/pcap/flv/ts/mp4 等）
- `GET /history`：历史记录（标题 **历史记录 · Stream Analyzer**）

## API

### 创建任务

`POST /api/task`

```json
{
  "url": "http://example.com/live/stream.flv",
  "type": "http-flv"
}
```

`type` 支持：`http-flv` / `rtmp` / `hls`（填 **master 或 media** 的 m3u8 URL；当前为 **TS** 切片，不支持带 `#EXT-X-MAP` 的 fMP4）

**HLS 示例：**

```json
{
  "url": "https://example.com/live/stream.m3u8",
  "type": "hls"
}
```

### 其它接口

- `DELETE /api/task/:id`
- `GET /api/task`
- `GET /api/task/all`
- `GET /api/task/:id`
- `GET /api/task/:id/status`
- `GET /api/task/:id/data`
- `GET /api/task/:id/xlsx`

说明：图表由前端基于 `/api/task/:id/data` 实时绘制，不再提供服务端图表 HTML 生成接口。

### 离线文件分析接口

离线任务使用 `multipart/form-data` 上传文件：

- `POST /api/offline/tasks`

表单字段：
- `mode`：`raw` / `pcap` / `flv` / `ts` / `mp4`
- `server_port`：**仅 `pcap` 模式**使用（RTMP 服务端口，可选，默认 1935）；其它模式请留空，后端会忽略
- `skip_bytes`：握手跳过字节数（raw/pcap 有效）
- `file`：上传文件（raw / pcap/pcapng / flv / ts / mp4）

查询接口：
- `GET /api/offline/tasks`：列出任务
- `GET /api/offline/tasks/:id`：获取任务与 `summary.json`
- `GET /api/offline/tasks/:id/files/*name`：下载离线产物文件（raw/video/audio/summary 等）

`summary.json` 中包含每个 flow 的方向与产物路径。`flv` / `mp4` 模式会包含逐帧明细（DTS/PTS/长度/帧类型）；每条 flow 的 **`video_codec` / `audio_codec`**（如 `h264`、`aac`）在网页「codec」列中合并为 `h264 / aac` 展示。`ts` 模式会包含 `PAT/PMT/PES/NALU` 统计、PID 明细和 PES 明细，网页端会据此渲染统计卡片与表格。

说明：
- `raw` 模式解析 RTMP 单方向裸流，要求输入包含完整 RTMP 握手与 chunk 流。
- `pcap` 模式从抓包中按端口识别 RTMP 会话，自动区分 push / pull 并导出对应产物。
- `flv` 模式解析 FLV 文件，严格校验 FLV Header；若 Header 缺失或无效会直接报错。
- `ts` 模式解析 MPEG-TS，支持 `PAT/PMT/PES` 与视频 NALU 粒度的分析展示。
- `mp4` 模式解析 MP4（`go-mp4`），产物在任务目录 `mp4/` 下；`artifact_paths` 含 `mp4_report.txt` 绝对路径，便于下载。

## XLSX 格式

每个任务生成 `task_xxx.xlsx`，主要工作表：

- `stream`：全量帧/秒心跳混合记录（用于后续聚合）
- `video`：视频子集
- `audio`：音频子集
- `hls`：**仅 HLS 任务写入**，每个 TS 切片一行（`FLV/RTMP` 任务该表为空表头）

`hls` 表字段（节选）：

| 字段 | 说明 |
|---|---|
| `stream_id` | media playlist URL（多码率时用于区分 variant） |
| `seq` / `uri` / `duration_s` / `size_b` | playlist 序号、切片绝对 URL、EXTINF 时长、字节数 |
| `v_pts_f_90k` ~ `a_dts_l_90k` | 切片内音视频 PTS/DTS 首尾值（90 kHz tick） |
| `av_diff_pts_90k` / `av_diff_ok` | 首视频 PTS − 首音频 PTS；两者均有首 PTS 时为真 |
| `av_diff_dts_90k` / `av_diff_dts_ok` | 首视频 DTS − 首音频 DTS（无 DTS 字段时按 PTS 兜底） |
| `iframe_intv_ms` | 切片内相邻 IDR 的间隔（毫秒），分号分隔；由 H.264/HEVC NAL 检测 |
| `pat` / `pmt` / `pes` / `video_pes` / `audio_pes` | 传输层与 ES 包计数 |
| `recorded_at` | 写入时间 |

实时页 / 历史页在 HLS 任务下除表格外，另有 **ECharts 切片图**（时间戳四线、AV DTS 差、I 帧间隔柱状），横轴为切片序号（按 playlist `#` 排序）。

`stream` 表字段：

| 字段 | 说明 |
|---|---|
| `dts` | 解码时间戳（ms） |
| `pts` | 显示时间戳（ms） |
| `video_len` | 视频数据长度（bytes） |
| `audio_len` | 音频数据长度（bytes） |
| `video_width` / `video_height` | 解析后的分辨率 |
| `video_codec` / `audio_codec` | 编码标识（优先归一为 legacy 数字 ID，必要时回退 FourCC 字符串） |
| `sample_rate` / `channels` | 音频参数 |
| `frame_type` | 帧类型（`I/P/H/...`） |
| `is_key_frame` | 是否关键帧（`0/1`） |
| `iframe_interval` | I 帧间隔（ms） |
| `gop_size` | GOP 帧数 |
| `recorded_at` | 服务接收时间（秒级） |
| `video_frame_rate` | 编码帧率（由 SPS/VUI 解析） |
| `metadata_json` | metadata JSON 字符串 |

## 前端展示

**FLV / RTMP** 任务：ECharts 图表 + 明细表（DTS/PTS、码率、FPS、AV 偏差等）。

**HLS** 任务：以 **TS 切片表格**为主（与上类任务的帧级图表不同），展示每片 URI、序号、时长、90 kHz PTS/DTS、AV 差、PAT/PMT/PES 等；历史记录页对已完成 HLS 任务同样支持该视图。

其它共性：

- 视频/音频 codec（如 `7 -> H.264/AVC`, `12 -> H.265/HEVC`, `10 -> AAC`）
- 分辨率、采样率、声道
- 编码帧率 FPS
- 接收帧率（按 `recorded_at` 秒桶统计）
- AV 同步偏差（ms）及等级（优秀/可接受/偏大/异常）
- metadata 文本区

说明：后端会对 Enhanced FourCC 尽量归一到既有数字 codec（例如 `avc1 -> 7`、`hvc1 -> 12`、`mp4a -> 10`），以减少前端改造成本。

## Enhanced FourCC 归一映射

为兼容现有前端 codec 映射逻辑，后端会将常见 Enhanced FourCC 归一为数字 codec ID：

| 类型 | FourCC | 归一值 | 备注 |
|---|---|---:|---|
| Video | `avc1` | `7` | H.264/AVC |
| Video | `hvc1` | `12` | H.265/HEVC |
| Video | `vp08` | `13` | VP8 |
| Video | `vp09` | `14` | VP9 |
| Video | `av01` | `15` | AV1 |
| Audio | `mp4a` | `10` | AAC |
| Audio | `.mp3` | `2` | MP3 |
| Audio | `Opus` | `13` | Opus |
| Audio | `ac-3` | `15` | AC-3 |
| Audio | `ec-3` | `16` | E-AC-3 |
| Audio | `fLaC` | `17` | FLAC |

若遇到未在表中的 FourCC，后端会保留其 FourCC 字符串写入 `video_codec` / `audio_codec`。

## 可视化说明

- **HLS** 任务：无上述 FLV 图表，页面以 **TS 切片表**为主（见「前端展示」）
- `DTS` 图：按真实时间戳作为 X 轴，视频/音频在同一时间轴对齐展示（**FLV/RTMP**）
- `Bitrate/FPS` 图：按相对秒（从 `T+0s`）全量展示，支持滑动缩放
- `AV偏差` 图：偏差定义为 `video_dts - nearest_audio_dts`
  - `|diff| <= 20ms`：优秀（绿）
  - `20ms < |diff| <= 50ms`：可接受（黄）
  - `50ms < |diff| <= 100ms`：偏大（橙）
  - `|diff| > 100ms`：异常（红）
- `FPS/Bitrate` 表格时间列同时展示：
  - 相对时间（如 `T+12s`）
  - 绝对时间（如 `2026-03-24 16:02:49`）

## 稳定性说明

- `GET /api/task/:id/data` 在读取 XLSX 时对临时不完整行做容错跳过，避免单行异常导致整次请求 500（常见于直播任务写入/刷新并发窗口）。

## 示例

```bash
go run ./example/rtmp_pull_client -url "rtmp://127.0.0.1/live/stream"
go run ./example/http_flv_client -url "http://127.0.0.1/live/stream.flv"
go run ./example/hls_client -url "https://example.com/live/stream.m3u8"
```

## 第三方源码替换

```go
replace github.com/yutopp/go-rtmp => ./third-party/go-rtmp
replace github.com/yutopp/go-flv => ./third-party/go-flv
```

本项目基于上述两个优秀开源仓库进行本地增强，以更好支持国内直播链路常见场景与特性（如 RTMP 拉流能力补全、`onMetaData` 解析、Enhanced RTMP/FLV ExHeader 与 FourCC 兼容、分析链路联调适配等）。

特别感谢这两个项目的作者与贡献者，为社区提供了稳定、清晰且可扩展的 RTMP/FLV 基础实现。

Enhanced 标准参考：
- [Enhanced RTMP (V2) - Veovera](https://veovera.org/docs/enhanced/enhanced-rtmp-v2)

## License

MIT
