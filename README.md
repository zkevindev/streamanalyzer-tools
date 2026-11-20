# Stream Analyzer Tools

![Stream Analyzer](analyzer-logo.png)

面向音视频流排障、离线文件解析与多协议模拟联调的一体化工作台（Web & 桌面端）。

## 核心能力

| 模块 | 访问路由 | 说明 |
| --- | --- | --- |
| **排障工作台** | `/diagnostics` | 支持 HTTP-FLV、RTMP/S、RTSP/S、HLS、HTTP/UDP/RTP MPEG-TS 采样，记录网络阶段耗时、HTTP 头与播放器体验，生成可离线复盘的 SHA-256 证据包及 AI 辅助汇总；选择 **HLS** 协议时可同页切换为**交付分析与 A/B 对比**（抽样分片、播放列表检查、直播窗口监控与缓存命中分析） |
| **ONVIF 探测** | `/diagnostics/onvif` | 局域网 WS-Discovery 设备发现、凭据鉴权与 Media Profile 读取，一键回填 RTSP 采样地址 |
| **协议模拟器** | `/simulators` | 内置 GB28181（含平台侧收流）、ONVIF、通用 SIP、HLS、RTMP/S、HTTP-FLV/MPEG-TS、WHEP 故障源与自动化场景回归 |
| **离线复盘** | `/diagnostics/review`、`/offline` | 支持排障证据包（ZIP）离线校验与重算，以及 `raw` / `pcap` / `flv` / `ts` / `mp4` 媒体文件解析与裸流提取 |
| **任务记录** | `/history` | 统一检索与回看排障采样、离线解析、HLS 交付与模拟器回归记录 |

## 快速开始

### 环境要求

- **Go 1.26+**（macOS / Linux 构建需要 CGO 与 `libpcap` 开发环境）
- **可选依赖**：本地安装 `ffmpeg` / `ffprobe`（放在程序同级目录、加入 `PATH` 或通过 Homebrew / MacPorts 安装），用于卡顿片段与截图提取、G.711 转码预览及模拟器本地视频推流

### 启动 Web 服务

```bash
go mod tidy
go build -o bin/server ./cmd/server
./bin/server -config config.yaml
```

默认监听端口由 `config.yaml` 控制（默认 `:8087`），启动后在浏览器访问本机 `:8087`：

- 首页：`/`
- 排障工作台：`/diagnostics`
- 协议模拟器：`/simulators`

> 排障工作台及对应 API 默认仅接受本机连接，同一数据目录请由单个服务实例使用。

## 桌面端（Wails v3）

桌面版基于 Wails v3.1.0，与 Web 端共用同一套前端界面，数据默认保存在系统应用数据目录下的 `Stream Analyzer` 文件夹（也可通过 `-data-dir /path/to/data` 指定）。

### 开发运行

```bash
# macOS / Windows
go run ./cmd/desktop

# Linux (GTK3 + WebKitGTK 4.1)
go run -tags gtk3 ./cmd/desktop
```

### 打包构建

安装 Wails v3.1.0 CLI 后执行：

```bash
# 当前平台构建
wails3 build

# macOS 生成 .app 与 DMG
wails3 task package:darwin
```

- 仓库内置 `.github/workflows/desktop-build.yml`，在各平台原生 runner 上自动构建 macOS (arm64 / amd64)、Windows (amd64) 与 Linux (amd64) 产物；推送 `v*` 标签时自动发布至 GitHub Release。
- 正式版桌面端支持在线检查更新、SHA-256 校验与一键安装重启（macOS 使用整包 ZIP，Windows / Linux 替换可执行文件）。

## 大模型配置（可选）

排障工作台支持调用大模型对规则证据与异常截图进行综合解读，支持 **DeepSeek、Anthropic Claude、Google Gemini 及任意 OpenAI 兼容接口**。原始流与播放地址始终保留在本地，仅发送脱敏后的指标、证据 ID 及按需附带的异常截图。

- **桌面端**：点击排障工作台右上角「模型设置」直接配置并测试连接，密钥保存在系统凭据库（macOS 钥匙串 / Windows 凭据管理器 / Linux Secret Service），保存后立即生效且优先于环境变量。
- **独立 Web 服务**：通过环境变量配置：

| 环境变量 | 说明 | 示例值 |
| --- | --- | --- |
| `STREAMANALYZER_MODEL_PROVIDER` | 厂商标识：`deepseek` / `anthropic` / `gemini` / `openai-compatible` | `deepseek` |
| `STREAMANALYZER_MODEL_BASE_URL` | 服务端点地址（前三个厂商可留空使用默认值） | 留空或填自定义网关 |
| `STREAMANALYZER_MODEL_NAME` | 模型名称 | `deepseek-flash` |
| `STREAMANALYZER_MODEL_API_KEY` | 接口访问密钥（本地免鉴权网关可留空） | 按厂商填写 |
| `STREAMANALYZER_MODEL_IMAGE_INPUT` | 截图输入策略：`auto`（默认，图文优先并支持回退文本）或 `text` | `auto` |

## 详细文档

为保持主文档简洁，各模块的协议细节、完整接口与回归测试已拆分至 `docs/` 目录：

- **[排障工作台与证据包复盘指南](docs/diagnostics.md)**：各协议原始产物、RTSP/HLS 采样细节、ONVIF 设备探测、播放器体验指标（v1–v4）、自动/手动卡顿补查与多模态汇总机制
- **[协议模拟器与场景回归指南](docs/simulators.md)**：GB28181（含平台侧收流）、ONVIF、通用 SIP、HTTP-FLV/MPEG-TS、HLS、RTMP/S、WHEP 故障源与回归预设使用说明
- **[API 接口与数据格式参考](docs/api-reference.md)**：排障、离线解析与 HLS 交付完整 HTTP API，以及 Enhanced FourCC 归一映射表
- **[端到端回归测试指南](docs/testing.md)**：H.265 / Opus 预览、RTSP 卡顿补查、多模态与自动补查的真实 Chrome 回归命令

## 项目结构

```text
.
├── cmd/
│   ├── desktop/          # Wails v3 桌面端入口
│   └── server/           # 独立 Web 服务入口
├── internal/
│   ├── appserver/        # HTTP 服务与配置加载
│   ├── codec/            # H.264 / H.265 / AAC 码流头解析
│   ├── desktop/          # 桌面端壳页面、凭据存储与在线升级
│   ├── diagnostic/       # 排障采样引擎、证据包、播放器与 AI 补查
│   ├── handler/          # 路由处理器与内嵌前端静态资源 (static/)
│   ├── hls/              # HLS 拉流客户端（供示例使用）
│   ├── hlsdelivery/      # HLS 交付统计、监控与 A/B 对比
│   ├── models/           # 公共数据模型
│   ├── mp4parse/         # MP4 解析与裸流导出
│   ├── offline/          # 离线文件解析（raw / pcap / flv / ts / mp4）
│   ├── onvifdiscovery/   # ONVIF 设备发现与 Media Profile 查询
│   ├── pcaprtmp/         # PCAP 抓包 RTMP 会话重组
│   ├── rtmpraw/          # RTMP 裸流解析
│   └── simulator/        # 多协议模拟器与场景回归引擎
├── third-party/          # 本地增强的 go-rtmp 与 go-flv
├── example/              # 独立命令行解析与拉流示例
├── docs/                 # 专题技术文档
├── config.yaml           # 服务端默认配置
└── Taskfile.yml          # Wails 桌面构建任务
```

## 命令行示例

```bash
go run ./example/rtmp_pull_client -url "<rtmp-url>"
go run ./example/http_flv_client -url "<http-flv-url>"
go run ./example/hls_client -url "<hls-m3u8-url>"
```

更多离线解析示例请查看 [`example/README.md`](example/README.md)。

## 致谢与参考

本项目在 `third-party/` 中对开源仓库 `go-rtmp` 与 `go-flv` 进行了本地增强（补全 RTMP 拉流、`onMetaData` 解析、Enhanced RTMP/FLV ExHeader 与 FourCC 兼容等），并参考了 Veovera Enhanced RTMP (V2) 规范。

## License

[MIT](LICENSE)
