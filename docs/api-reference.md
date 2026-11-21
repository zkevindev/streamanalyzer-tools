# API 接口与数据格式参考

所有接口默认监听于 `config.yaml` 指定地址（默认 `:8087`）。排障与 ONVIF 相关接口仅接受本机连接。

## 1. 排障工作台 API（`/api/diagnostics`）

| 方法与路径 | 说明 |
| --- | --- |
| `GET /api/diagnostics/config` | 返回当前模型配置状态及 `capture_limit_bytes` 原始产物上限 |
| `POST /api/diagnostics/runs` | 发起采样。请求体示例：`{"protocol":"hls","url":"<stream-url>","seconds":60,"playback":true,"auto_investigate":true}`。`protocol` 可选 `http-flv` / `rtmp` / `rtsp` / `rtsps` / `hls` / `http-ts` / `udp-ts` / `rtp-ts`（默认 `http-flv`）；RTSP 可传 `"transport":"udp"`（默认 `tcp`）。HTTP-FLV、HLS、HTTP-TS 可附加 `headers`（字符串映射，最多 16 项）、`proxy_url` 和 `ca_certificate`（PEM）；RTMPS/RTSPS 也可附加 `ca_certificate`。这些配置仅用于本次连接且不会保存原始值。HTTP-FLV/RTMP 可附加 `reconnect_attempts`（0–3）和 `reconnect_delay_ms`（0–5000），异常 EOF/读取失败时创建独立重连 Run。HLS 可附加 `hls_variant`：-1 为最高码率，0 起为主清单常规变体序号；实际选择和带宽写入 manifest。HTTP-FLV/RTMP/HLS 可附加 `segment_seconds`（1–300）和 `rolling_segments`（1–12，默认 3）启用持续滚动分段 |
| `GET /api/diagnostics/sessions/:id` | 查询 HTTP-FLV/RTMP 自动重连或 HTTP-FLV/RTMP/HLS 滚动分段会话及各段独立 Run；会话索引仅保存在当前服务内存 |
| `POST /api/diagnostics/sessions/:id/stop` | 停止滚动会话及当前分段，不再创建下一段；已保留证据不受影响 |
| `POST /api/diagnostics/batches` | 创建 2–4 路批量采样，请求体 `{"samples":[<SampleRequest>,...]}`。批量模式不接受 `playback` / `auto_investigate`；批次只在本服务内存保存运行 ID，不保存访问条件 |
| `GET /api/diagnostics/batches/:id` | 查询批次状态；全部结束后以第一路可用报告为基线，自动返回其余同协议线路的对比结果 |
| `POST /api/diagnostics/batches/:id/stop` | 停止批次内所有运行中采样并保留已采集证据 |
| `GET /api/diagnostics/runs` | 列出最近的采样与复盘记录，包含 `protected` 保护状态 |
| `GET /api/diagnostics/media-tools` | 返回当前 FFmpeg/FFprobe 路径、版本、可用状态及 AAC/H.264/HEVC/Opus 编码器和内容检测过滤器能力 |
| `POST /api/diagnostics/media-tools` | 用 `{"ffmpeg_path":"/path/ffmpeg","ffprobe_path":"/path/ffprobe"}` 测试并持久化工具路径；留空恢复自动检测，失败时保留原配置 |
| `GET /api/diagnostics/storage` | 返回排障记录数、产物字节数及受保护证据占用 |
| `POST /api/diagnostics/storage/prune` | 按 `{"retention_days":30,"max_total_bytes":1073741824}` 从最早记录开始清理；0 表示对应策略不启用，受保护和运行中记录始终跳过 |
| `POST /api/diagnostics/runs/delete` | 批量删除已结束且未保护记录，请求体 `{"ids":["<run-id>"]}`，每次 1–100 条 |
| `GET /api/diagnostics/runs/:id` | 查询任务状态、规则报告与已生成的模型解读 |
| `GET /api/diagnostics/runs/:id/compare?baseline=<run-id>` | 对比当前报告与同协议基线，返回带观测状态的指标差值、新增/未再触发规则及来源、时长、编码和规则版本差异警告 |
| `DELETE /api/diagnostics/runs/:id` | 永久删除已结束且未保护记录及其本地产物 |
| `POST /api/diagnostics/runs/:id/protection` | 用 `{"protected":true}` 保护重要证据，或用 `false` 取消保护 |
| `POST /api/diagnostics/runs/:id/stop` | 停止采样并保留已采集数据 |
| `POST /api/diagnostics/runs/:id/player` | 使用授权头 `X-StreamAnalyzer-Player` 追加播放器事件与缓冲样本（`complete` 后锁定） |
| `POST /api/diagnostics/runs/:id/automatic/retry` | 用 `{}` 显式重新运行自动补查与汇总流程 |
| `POST /api/diagnostics/runs/:id/automatic/stop` | 用 `{}` 停止自动补查流程并保留已有产物 |
| `POST /api/diagnostics/runs/:id/stalls` | 手动补查：缓冲卡顿传 `{"stall_index":1}`，呈帧回调间隔传 `{"interval_kind":"presentation_gap","interval_index":1}`；终态无媒体任务可附加 `"retry":true` |
| `POST /api/diagnostics/runs/:id/stalls/:caseID/explain` | 生成/更新本地分层方向并请求模型建议 |
| `POST /api/diagnostics/runs/:id/stalls/:caseID/stop` | 停止进行中的补查任务 |
| `GET /api/diagnostics/runs/:id/stall-assets/:caseID/:filename` | 访问补查生成的 MP4（支持 Range）或 JPEG 产物 |
| `GET /api/diagnostics/runs/:id/{dump,report.html,report.json,evidence.zip}` | 下载原始流、HTML/JSON 报告或证据包 ZIP |
| `POST /api/diagnostics/runs/:id/share` | 生成仅用于外发查看的最小分享 ZIP。请求体 `{"case_ids":["S001"],"include_stream":false}`；默认包含报告、清单和所选案例媒体，只有显式设置 `include_stream` 才包含完整原始流。该 ZIP 不可导入重算 |
| `POST /api/diagnostics/import` | 导入证据包（`Content-Type: application/zip`，请求体为 ZIP 原始字节） |
| `POST /api/diagnostics/runs/:id/explain` | 请求整份报告的大模型汇总解读 |
| `POST /api/diagnostics/runs/:id/content-quality` | 按需运行本机 FFmpeg 源内容检测，返回黑帧、冻结、静音完整区间及音画时间戳差/漂移；结果绑定原始流 SHA-256 并保存到 `report.content_quality`，相同原始流重复请求复用结果 |

### ONVIF 设备探测 API（`/api/diagnostics/onvif`）

- `GET /api/diagnostics/onvif/interfaces`：获取本机可用 IPv4 网卡列表
- `POST /api/diagnostics/onvif/discoveries`：发起发现（参数 `bind_ip`、时长 1–10 秒）
- `GET /api/diagnostics/onvif/discoveries/:id`：查询发现结果
- `POST /api/diagnostics/onvif/discoveries/:id/stop`：停止发现
- `POST /api/diagnostics/onvif/profiles`：查询设备或手动服务地址的 Media Profiles
- `POST /api/diagnostics/onvif/stream`：获取指定 Profile 与传输方式的 RTSP 流地址

---

## 2. 离线文件解析 API（`/api/offline/tasks`）

- `POST /api/offline/tasks`：使用 `multipart/form-data` 上传文件
  - `mode`：`auto`（默认，基于魔数与扩展名自动识别）/ `flv` / `mp4`（含 fMP4）/ `ts`（MPEG-TS / M2TS）/ `ps`（MPEG-PS / GB28181 `.ps`/`.mpg`，支持 H.264/H.265 + AAC/G.711 转 WAV）/ `webm`（WebM / Matroska `.webm`/`.mkv`，含 AV1/VP8/VP9/Opus）/ `annexb`（H.264 / H.265 裸码流）/ `pcap`（抓包自动提取 RTMP push/pull、UDP RTP/RTCP、UDP MPEG-TS、TCP HTTP-FLV 与 HTTP MPEG-TS）/ `raw`（单方向 RTMP 裸流）
  - `server_port`：仅 `pcap` 模式有效（RTMP 端口，默认 `1935`）
  - `skip_bytes`：握手跳过字节数（`raw` / `pcap` 有效，默认 `3073`，无握手可传 `0`）
  - `file`：上传媒体或抓包文件
- `POST /api/offline/tasks/local`（仅本机访问）：零拷贝直读本机大文件并创建解析任务
  - JSON 请求体：`{ "path": "/abs/path/to/media.mp4", "mode": "auto", "server_port": 1935, "skip_bytes": 3073 }`
  - 不复制原文件，任务标记 `source_type: "local"`；删除任务时仅清理 `data/offline/<taskID>` 产物目录，不会删除用户原始文件
- `GET /api/offline/tasks`：列出离线解析任务（运行中任务包含 `progress_pct`、`progress_stage`、`bytes_processed` 实时进度字段，状态含 `running` / `done` / `failed` / `canceled`）
- `GET /api/offline/tasks/:id`：获取任务状态与 `summary.json`（含 flow、协议 `protocol`、RTP/RTCP 传输统计 `rtp_stats`（按 SSRC 统计丢包/乱序/重复/RFC 3550 Jitter/RTCP SR·RR）、编解码、逐帧字节定位 `stream_source`/`stream_offset`/`stream_len`/`nalu_len_size` 或 TS PAT/PMT/PES/NALU 明细，以及 `profile`：分辨率、Profile、时长、帧率、码率曲线、200ms 瞬时峰值码率 `peak_200ms_kbps`/`peak_to_avg_ratio`、帧大小、帧间隔、GOP、音画偏移、TS 传输层 CC/TEI/PCR、告警及结构化深度体检明细 `health_checks`）
- `POST /api/offline/tasks/:id/cancel`：取消正在运行中的离线解析任务（任务状态变为 `canceled`）
- `POST /api/offline/tasks/:id/reparse`：保留原始输入文件并重新解析任务
- `DELETE /api/offline/tasks/:id`：删除非运行中的离线解析任务及其磁盘产物目录（本机直读模式不删除原始文件）
- `GET /api/offline/tasks/:id/inspect/frame`：按需读取单帧或 TS PES 原始字节（单帧读取上限 4 MiB）并进行深度解码
  - Query 参数：`flow_idx`（默认 `0`）、`frame_idx`（0-based 帧序号）或 `pes_idx`（TS 模式 0-based PES 序号）、`hex_offset`（默认 `0`）、`hex_limit`（默认 `2048`，上限 `8192`）
  - 返回字段：帧/PES 基础信息、NALU 列表（偏移/长度/类型/Slice 类型/关键帧标记）、H.264 SPS（含 VUI）/ PPS 与 H.265 VPS / SPS / PPS 字段解码、AV1 Sequence Header OBU 字段解码、SEI 载荷解码（含 `user_data_unregistered` UUID 与可读明文）、ADTS AAC / MP3 / G.711(PCMA/PCMU) 音频帧头、以及 16 列分页 Hex+ASCII 视图与 NALU 边界区间
- `GET /api/offline/tasks/:id/mp4/boxes`：解析 MP4 / fMP4 任务的完整递归 Box 树结构
  - Query 参数：`max_nodes`（可选，默认 `2500`，超限时返回 `truncated: true`）
  - 返回字段：文件大小、FastStart / 分片标记，以及递归 `boxes`（每节点含 `type`、`offset`、`size`、`header_size`、`percent`、可读 `summary`、`highlight`/`highlight_reason` 如 `elst` 与非 FastStart `moov` 标黄提示、`children`）
- `GET /api/offline/tasks/:id/thumbnail`：调用本机 FFmpeg 按时间点抽取 JPEG 画面截图（并缓存至任务目录 `thumbs/`）
  - Query 参数：`flow`（默认 `0`）、`time_ms`（时间点毫秒，默认 `0`）
  - 返回：`image/jpeg` 二进制响应
- `POST /api/offline/tasks/:id/decode-audit`：调用本机 FFmpeg 执行全流解码体检（缓存至 `decode_audit_f<N>.json`）
  - Query 参数：`flow`（默认 `0`）、`force`（`1` 强制重新扫描，默认使用缓存）
  - 返回字段：解码报错次数与错误样本 `decode_errors` / `error_samples`，以及黑帧（`black_frames`）、冻结帧（`freeze_frames`）、静音（`silence_ranges`）时间段列表
- `POST /api/offline/tasks/:id/clip`：调用本机 FFmpeg `-c copy` 无损截取最小复现片段并导出至 `clips/`
  - JSON 请求体：`{ "flow": 0, "start_sec": 1.5, "end_sec": 8.0 }`
  - 返回字段：`rel_path`、`filename`、`size_bytes` 与可直接下载的 `download_url`
- `GET /api/offline/compare`：离线两文件 A/B 参数与曲线对比
  - Query 参数：`task_a`、`task_b`（必填），`flow_a`、`flow_b`（可选，默认 `0`）
  - 返回字段：`side_a`、`side_b`（含各自 `profile` 时序曲线）、字段级 Diff 表 `rows`（含 `changed` 与 `severity`）、`changed_count` 及差异结论 `summary_notes`
- `GET /api/offline/tasks/:id/report`：导出独立离线诊断报告
  - Query 参数：`flow`（默认 `0`）、`format`（`html` 或 `md`，默认 `html`）、`inline`（`1` 浏览器内联查看，否则附件下载）
  - `html` 报告零外部 CDN 依赖，内联 SVG 瞬时码率趋势图、完整媒体概况、RTP 传输统计与深度体检规则，可直接发给同事或客户离线打开
- `GET /api/offline/tasks/:id/files/*name`：下载提取产物（如 `video.annexb`、`audio.adts.aac`、`audio.mp3`、`audio.wav`、`mp4_report.txt`、`clips/*.mp4`、`summary.json`）

---

## 3. 推流器模拟器 API（`/api/simulators/pusher`）

在模拟器工作台 `/simulators/pusher` 使用（仅本机访问），支持最多 **16 路并发** RTMP／RTMPS、RTSP／RTSPS（TCP/UDP）与标准 RFC 9725 WHIP 推流，推流源支持上传录像、本机文件路径（支持循环读取）、内置测试画面（`testsrc2` + 440Hz 正弦波）及浏览器摄像头／屏幕共享采集。

- `GET /api/simulators/pusher/tasks`：列出所有推流任务与实时状态（已推时长、帧数、实时 FPS、码率、累计流量、ICE 状态、最近日志）
- `GET /api/simulators/pusher/tasks/:id`：查询单路推流任务详情
- `POST /api/simulators/pusher/tasks`：JSON 创建并启动推流任务（支持 `source_kind` 为 `file`、`pattern`、`browser`）
- `POST /api/simulators/pusher/tasks/upload`：`multipart/form-data` 上传录像文件（上限 512 MiB）并启动推流，先传 `config`（JSON）再传 `file`
- `POST /api/simulators/pusher/tasks/:id/stop`：停止指定推流任务（WHIP 会自动向 `Location` 发 `DELETE` 清理远端会话）
- `POST /api/simulators/pusher/stop-all`：停止全部运行中任务
- `DELETE /api/simulators/pusher/tasks/:id`：移除已结束／失败任务记录并清理上传临时文件
- `GET /api/simulators/pusher/tasks/:id/ingest`：WebSocket 端点，接收浏览器摄像头／屏幕采集流（`MediaRecorder`）并实时转推到 RTMP／RTSP
- `POST /api/simulators/pusher/whip-proxy` & `DELETE /api/simulators/pusher/whip-proxy`：供浏览器 WebRTC 采集直推外部 WHIP 服务使用的本机信令代理（规避 CORS）

---

## 4. Enhanced FourCC 归一映射

| 类型 | FourCC | 归一数字 ID | 说明 |
| --- | --- | ---: | --- |
| Video | `avc1` / `hvc1` / `vp08` / `vp09` / `av01` | `7` / `12` / `13` / `14` / `15` | H.264 / H.265 / VP8 / VP9 / AV1 |
| Audio | `.mp3` / `mp4a` / `Opus` / `ac-3` / `ec-3` / `fLaC` | `2` / `10` / `13` / `15` / `16` / `17` | MP3 / AAC / Opus / AC-3 / E-AC-3 / FLAC |

未在表中的 FourCC 将保留原字符串。
