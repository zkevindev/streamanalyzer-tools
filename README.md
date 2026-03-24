# Stream Analyzer Tools

音视频流分析工具，支持 RTMP / HTTP-FLV 拉流、DTS/PTS 分析、XLSX 持久化和网页实时可视化，兼容 Legacy FLV 与 Enhanced RTMP/FLV（E-RTMP）头部解析。

## 功能特性

- 支持 `http-flv` 与 `rtmp` 两种任务类型
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

## 项目结构

```text
.
├── cmd/server/main.go
├── internal/
│   ├── analyzer/
│   │   ├── task.go
│   │   ├── flv_client.go
│   │   └── rtmp_client.go
│   ├── codec/
│   │   ├── bitreader.go
│   │   ├── h264.go
│   │   ├── h265.go
│   │   └── aac.go
│   ├── handler/
│   │   ├── handler.go
│   │   ├── embed.go
│   │   └── static/echarts_helpers.js
│   ├── models/models.go
│   └── storage/csv.go
├── third-party/
│   ├── go-rtmp/
│   └── go-flv/
├── example/
│   ├── rtmp_pull_client/
│   └── http_flv_client/
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

## API

### 创建任务

`POST /api/task`

```json
{
  "url": "http://example.com/live/stream.flv",
  "type": "http-flv"
}
```

`type` 支持：`http-flv` / `rtmp`

### 其它接口

- `DELETE /api/task/:id`
- `GET /api/task`
- `GET /api/task/all`
- `GET /api/task/:id`
- `GET /api/task/:id/status`
- `GET /api/task/:id/data`
- `GET /api/task/:id/xlsx`

说明：图表由前端基于 `/api/task/:id/data` 实时绘制，不再提供服务端图表 HTML 生成接口。

## XLSX 格式

每个任务生成 `task_xxx.xlsx`，主要工作表：

- `stream`：全量帧/秒心跳混合记录（用于后续聚合）
- `video`：视频子集
- `audio`：音频子集

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

前端会展示并映射以下信息：

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

- `DTS` 图：按真实时间戳作为 X 轴，视频/音频在同一时间轴对齐展示
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
