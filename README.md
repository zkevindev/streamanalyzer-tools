# Stream Analyzer Tools

音视频流分析工具，排障工作台支持 **HTTP-FLV / RTMP / HLS / HTTP MPEG-TS / UDP MPEG-TS / RTP MPEG-TS** 采样、报告及证据包复盘。另提供实时帧分析和离线文件解析（raw / pcap / FLV / MPEG-TS / **MP4**）。离线结果会持久化到磁盘并在网页端可查看/下载。兼容 Legacy FLV 与 Enhanced RTMP/FLV（E-RTMP）头部解析。

## 排障工作台

启动 Web 服务后打开 [本机排障工作台](http://127.0.0.1:8087/diagnostics)。桌面版启动后直接打开同一个工作台。工作台和对应 API 仅接受本机连接，同一数据目录由一个服务实例使用。

1. 选择 HTTP-FLV、RTMP、HLS、HTTP MPEG-TS、UDP MPEG-TS 或 RTP MPEG-TS，输入采样地址，指定 1–300 秒的采样时长，默认 60 秒。RTMP 同时接受 `rtmp://` 和 `rtmps://`。
2. 查看音频和视频编码、采样时序及异常。HTTP-FLV 和 RTMP 的报告可定位媒体帧的 PTS/DTS 及原始流字节范围。HLS 的报告可定位播放列表、初始化段和媒体分片，并展示原始请求耗时与 HTTP 状态。
3. 下载原始流、HTML/JSON 报告或证据 ZIP。导入时校验清单中的 SHA-256，重新解析原始数据并按采样时记录的时间线运行规则。源流可以已经下线。规则报告独立于模型，原有 HTTP-FLV v1 证据包也可导入。

| 协议 | 原始产物 |
| --- | --- |
| HTTP-FLV | `stream.flv`，采样期间读取的 HTTP 响应体 |
| RTMP / RTMPS | `stream.rtmp`，从服务端握手开始的原始接收字节。RTMPS 保存 TLS 解密后的 RTMP 协议数据 |
| HLS | `stream.hls.zip`，原始 m3u8 快照、初始化段和 MPEG-TS、fMP4 或独立音频分片 |
| HTTP MPEG-TS | `stream.ts`，采样期间读取的原始 HTTP 响应体 |
| UDP MPEG-TS | `stream.udp`，按接收顺序拼接的完整 UDP 载荷，时间线记录每个数据报的边界 |
| RTP MPEG-TS | `stream.rtp`，保留 RTP 头部和载荷的完整数据报，时间线记录每个数据报的边界 |

UDP 与 RTP 使用 IPv4 接收地址，例如 `udp://0.0.0.0:5000` 或 `rtp://127.0.0.1:5000`，发送端向该设备及端口发包。组播示例为 `udp://239.1.1.1:5000?interface=en0`，`interface` 填写本机网卡名，省略时由系统选择网卡。RTP 当前检查静态载荷类型 33 的 MPEG-TS 数据。HTTP MPEG-TS 同时支持 HTTP 和 HTTPS。

传输流报告展示 TS 包长度、PID 与编码、PAT/PMT、TEI、连续计数、重复包、同步丢失以及 PCR/PES 时间戳检查。RTP 报告补充序列缺口、重复和乱序统计。序列缺口反映当前采样窗口中的接收结果。原始数据和到达时间同时保留，导入时重新运行这些检查。

连接报告逐次记录 HTTP 请求及跳转响应，分别展示 302、200 等状态、单次请求耗时和首字节时间，并提供可观测的 DNS、TCP、TLS、发送请求、等待首字节和接收响应头的阶段耗时。HLS 的播放列表、初始化段及媒体分片请求也保留这些时序信息。RTMP 单独展示 TCP、TLS、协议握手、应用连接、创建流和播放协商阶段。

首包、首个视频数据、首个音频数据和首个关键帧按采样起点展示到达时间。FLV/RTMP 使用解析到的媒体帧，MPEG-TS 使用已识别媒体 PID 上的 PES 起始，HLS 使用已解析媒体类型的分片。已有报告还会展示音视频接收间隔、时间戳和关键帧异常、重复异常次数及后续检查建议。

采样时勾选「同时分析播放器体验」，工作台会播放并记录首屏、首次开始播放、可播放缓冲变化、卡顿次数、累计及最长卡顿时长。首屏从提交采样开始计时，优先使用浏览器实际呈现视频帧的回调，缺少该能力时明确标为首帧加载估算。缓冲曲线和卡顿区间对齐后端采样时间轴，与 DNS、TCP、HTTP 跳转及首音视频等阶段耗时放在同一份报告中。播放器音视频解码能力取决于当前系统和浏览器；不支持的编码或播放失败会留下具体状态，拉流采样及 dump 仍可继续。

卡顿从首次开始播放后统计，持续等待至少 500 ms 计为一次。启动等待、手动暂停、跳转和页面隐藏期间排除，正常采样结束造成的断流会截断计时。HTTP-FLV、RTMP 和 TS 播放使用本次采样转发的数据，HLS 使用独立连接播放同一地址，报告会标注来源。播放器结果反映本机播放环境，分析客户现场问题时还需对照客户的播放器埋点。

播放器原始事件和缓冲样本写入证据包的 `player.json` 并校验 SHA-256，导入后离线重算播放器指标和卡顿区间，无需重新播放源流。HTML/JSON 报告包含相同结果；模型解读按需接收播放器指标、卡顿区间及对应证据。页面退出或网络中断前已保存的部分记录会保留，并标明采集状态。

HLS 主清单选取带宽最高的常规变体及其默认独立音频 rendition。直播从清单末尾最多三个完整分片开始采样，随后跟踪新分片，点播按清单顺序下载。加密分片保存原始密文，媒体识别限于能够检查的明文分片。原始产物可能包含签名 URL 或业务元数据。停止、读流失败或达到 128 MiB 的原始产物上限时，工作台保留已收集的数据和可用报告。

编码名称取自采样数据，例如 H.264、H.265、AV1、VP9 以及 AAC、Opus、MP3、AC-3、E-AC-3。识别范围取决于协议与封装，工具保留原始数据用于复盘。

JSON 报告及 SQLite 任务索引位于数据目录下的 `diagnostics/`。基础接口包括：

| 方法与路径 | 用途 |
| --- | --- |
| `POST /api/diagnostics/runs` | 用 `{"protocol":"hls","url":"https://live.example.com/index.m3u8","seconds":60,"playback":true}` 发起采样。协议可选 `http-flv`、`rtmp`、`hls`、`http-ts`、`udp-ts`、`rtp-ts`，省略时默认 `http-flv`。`playback` 可选，启用时响应返回本机播放器会话授权 |
| `GET /api/diagnostics/runs` | 查看最近的采样和复盘记录 |
| `GET /api/diagnostics/runs/:id` | 查询状态、规则报告和已生成的模型解读 |
| `POST /api/diagnostics/runs/:id/stop` | 停止采样并保存已获取的数据 |
| `POST /api/diagnostics/runs/:id/player` | 用会话授权头 `X-StreamAnalyzer-Player` 保存追加的播放器事件和缓冲样本；`complete` 后记录不可更改 |
| `GET /api/diagnostics/runs/:id/{dump,report.html,report.json,evidence.zip}` | 查看或下载产物。原 HTTP-FLV `stream.flv` 路径仍可用 |
| `POST /api/diagnostics/import` | 请求体传 ZIP 原始字节，类型为 `application/zip` |
| `POST /api/diagnostics/runs/:id/explain` | 请求模型解读报告 |

### 配置大模型厂商

桌面端打开「模型设置」，选择厂商并填写 Base URL、模型名及 API Key。保存前可点击「测试连接」，用当前填写的配置发起一条简短的模型请求，查看耗时或具体失败原因。测试只发送固定提示词，测试结果独立于保存操作。保存后，当前工作台中的新解读请求立即采用新配置。已有解读可使用「用当前模型重新解读」更新。

桌面设置按数据目录保存在系统凭据库中，macOS 使用钥匙串，Windows 使用凭据管理器，Linux 使用系统 Secret Service。编辑同一厂商和地址时留空密钥会沿用已配置的密钥。切换厂商或地址时，使用该厂商或网关自己的密钥。桌面端保存的配置优先于环境变量。

| 厂商配置 | API | 默认 Base URL |
| --- | --- | --- |
| `deepseek` | DeepSeek Chat Completions | `https://api.deepseek.com` |
| `anthropic` | Anthropic Messages | `https://api.anthropic.com/v1` |
| `gemini` | Gemini Developer API | `https://generativelanguage.googleapis.com/v1beta` |
| `openai-compatible` | Chat Completions | 需要填写服务地址，例如 `https://gateway.example.com/v1` |

DeepSeek 在桌面端可直接选择。Base URL 使用 `https://api.deepseek.com`，示例模型名为 `deepseek-flash`。也可以填写账号可用的型号，例如 `deepseek-v4-pro`。[DeepSeek API 文档](https://api-docs.deepseek.com/)

独立 Web 服务从环境变量读取模型配置。以下是其 DeepSeek 示例。

```bash
export STREAMANALYZER_MODEL_PROVIDER='deepseek'
export STREAMANALYZER_MODEL_BASE_URL='https://api.deepseek.com'
export STREAMANALYZER_MODEL_NAME='deepseek-flash'
export STREAMANALYZER_MODEL_API_KEY='your-deepseek-api-key'
go run ./cmd/server -config config.yaml
```

本地 Chat Completions 兼容服务可按需省略 API Key。原有 `STREAMANALYZER_OPENAI_BASE_URL`、`STREAMANALYZER_OPENAI_MODEL`、`STREAMANALYZER_OPENAI_API_KEY` 在未设置任何新环境变量时仍可使用。工作台及 HTML 报告会标注实际解读来源。

模型解读按需发送数值指标、异常区间与证据 ID。原始流和播放地址保留在本地。模型假设中的证据引用必须存在于规则报告。

## 功能特性

- 支持 `http-flv`、`rtmp` 与 **`hls`** 三种实时任务类型（HLS 为 **TS 切片维度**，与 FLV/RTMP 的帧级秒级图表不同）
- **HLS 实时分析**：拉取 media playlist，逐 `.ts` 切片统计 URI、playlist 序号、时长、体积；从 PES 解析音视频 **PTS/DTS（90 kHz）**、PAT/PMT/PES 计数；切片内首视频 PTS 与首音频 PTS 之差（AV）。若 PES 头仅有 PTS、无 DTS 字段，则展示时 **DTS 按与 PTS 相同处理**（常见于 HLS 切片）
- **HLS 分析**（首页「HLS 分析」）：对 master 或 media 播放列表做有限次分析，统计 TTFB、吞吐分位、缓存命中、CDN、多码率余量和 PTS 时长偏差。入口是 `/hls-stats`，不替换上面的实时切片分析
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

需要 Go 1.26 或更高版本。macOS 和 Linux 构建需要 CGO 与 libpcap 开发环境。

```bash
go mod tidy
go build -o bin/server ./cmd/server
./bin/server -config config.yaml
```

默认地址由 `config.yaml` 控制（当前默认 `:8087`），启动后访问：

- [http://localhost:8087](http://localhost:8087)

路由说明：
- `GET /`：入口页
- `GET /diagnostics`：排障工作台
- `GET /realtime`：实时拉流分析（标题 **实时拉流分析 · Stream Analyzer**）
- `GET /offline`：离线文件解析（标题 **离线文件解析 · Stream Analyzer**；支持 raw/pcap/flv/ts/mp4 等）
- `GET /history`：历史记录（标题 **历史记录 · Stream Analyzer**）
- `GET /hls-stats`：HLS 分析（标题 **HLS 分析 · Stream Analyzer**）

## 桌面 GUI（Wails v3）

桌面版使用 Wails v3.1.0 和同一套 Web UI。采样默认保存在系统应用数据目录下的 `Stream Analyzer` 文件夹。开发运行：

```bash
go run ./cmd/desktop
```

Linux 使用 GTK3 与 WebKitGTK 4.1 时，运行 `go run -tags gtk3 ./cmd/desktop`。也可以传入 `-data-dir /path/to/data` 直接使用指定目录。

在目标平台打包时，安装固定版本的 CLI，然后执行：

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
wails3 build
```

在 GTK3 环境的 Linux 上，安装 CLI 时使用 `go install -tags gtk3 github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0`。macOS 运行 `wails3 task package:darwin` 生成 `build/bin/streamanalyzer.app` 和对应的 DMG。Windows 和 Linux 的可执行文件也位于 `build/bin/`。

### 多平台桌面产物

仓库包含 `.github/workflows/desktop-build.yml`，使用各平台的原生 GitHub Actions runner 构建桌面程序，避免 Wails/WebView 跨平台交叉编译问题。每次推送到 `main`、推送 `v*` 版本标签时都会自动运行，也可在 GitHub 的 **Actions → Build desktop apps → Run workflow** 手动运行。

构建完成后，在对应 workflow run 的 **Artifacts** 中下载：

- `streamanalyzer-windows-amd64`：Windows `streamanalyzer.exe`
- `streamanalyzer-macos-arm64`：Apple Silicon Mac `StreamAnalyzer-arm64.dmg`
- `streamanalyzer-macos-amd64`：Intel Mac `StreamAnalyzer-amd64.dmg`
- `streamanalyzer-linux-amd64`：Linux `streamanalyzer`

推送 `v*` 标签时，上述文件还会自动附加到对应的 GitHub Release；如果该 Release 尚不存在，workflow 会自动创建。

macOS 本地包使用临时签名。正式对外分发时需要 Developer ID 签名和公证。Linux 运行时需要 GTK3 和 WebKitGTK 4.1。

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

### HLS 分析

和实时 `type=hls` 分开。这里按播放列表抽样分片，不写 XLSX，也不解析整路 PES。

- `POST /api/hls-stats/tasks`：创建任务，立即返回 `{id,status}`
- `GET /api/hls-stats/tasks/:id`：查询状态和报告
- `DELETE /api/hls-stats/tasks/:id`：取消正在跑的任务

```json
{
  "command": "analyze",
  "url": "https://example.com/live/index.m3u8",
  "segments": 10,
  "variant": -1,
  "parse_segments": true,
  "duration": "30s",
  "timeout": "10s",
  "error_threshold": 0
}
```

`command` 可以是 `analyze`、`manifest`、`monitor`、`compare`。`compare` 还要带 `url_b`。`variant` 为 `-1` 时测最高码率，`0` 测最低码率。阈值为 `0` 时不产生对应退出码。报告在 `report` 字段里，`exit_code` 为 2/3/4/5/6 时 HTTP 状态仍是 200，任务状态为 `done` 或 `failed`。

分析报告的 `playlist` 包含目标时长、媒体序号、分片数和加密状态。`EXT-X-GAP` 声明的缺片不发起请求，计入 `summary.segments_skipped`，不计入分片错误率。监控期间的播放列表刷新故障单独记录在 `playlist_refresh`（失败总次数、结束时连续失败次数和最后错误）及报告建议中；恢复后仍保留故障记录。

启用 `parse_segments` 时，`EXT-X-MAP` 初始化段下载失败会记为对应分片失败，报告保留媒体请求的 `http_status`，并在可用时补充 `init_http_status`。停止任务后，状态从 `stopping` 变为 `canceled`，已采集的部分报告仍可查询。

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
