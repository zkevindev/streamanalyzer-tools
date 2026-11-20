# API 接口与数据格式参考

所有接口默认监听于 `config.yaml` 指定地址（默认 `:8087`）。排障与 ONVIF 相关接口仅接受本机连接。

## 1. 排障工作台 API（`/api/diagnostics`）

| 方法与路径 | 说明 |
| --- | --- |
| `GET /api/diagnostics/config` | 返回当前模型配置状态及 `capture_limit_bytes` 原始产物上限 |
| `POST /api/diagnostics/runs` | 发起采样。请求体示例：`{"protocol":"hls","url":"<stream-url>","seconds":60,"playback":true,"auto_investigate":true}`。`protocol` 可选 `http-flv` / `rtmp` / `rtsp` / `rtsps` / `hls` / `http-ts` / `udp-ts` / `rtp-ts`（默认 `http-flv`）；RTSP 可传 `"transport":"udp"`（默认 `tcp`）。HTTP-FLV、HLS、HTTP-TS 可附加 `headers`（字符串映射，最多 16 项）、`proxy_url` 和 `ca_certificate`（PEM）；RTMPS/RTSPS 也可附加 `ca_certificate`。这些配置仅用于本次连接且不会保存原始值 |
| `POST /api/diagnostics/batches` | 创建 2–4 路批量采样，请求体 `{"samples":[<SampleRequest>,...]}`。批量模式不接受 `playback` / `auto_investigate`；批次只在本服务内存保存运行 ID，不保存访问条件 |
| `GET /api/diagnostics/batches/:id` | 查询批次状态；全部结束后以第一路可用报告为基线，自动返回其余同协议线路的对比结果 |
| `POST /api/diagnostics/batches/:id/stop` | 停止批次内所有运行中采样并保留已采集证据 |
| `GET /api/diagnostics/runs` | 列出最近的采样与复盘记录，包含 `protected` 保护状态 |
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
  - `mode`：`raw`（完整 RTMP 握手单方向裸流）/ `pcap`（抓包自动区分 push/pull）/ `flv` / `ts` / `mp4`
  - `server_port`：仅 `pcap` 模式有效（RTMP 端口，默认 `1935`）
  - `skip_bytes`：握手跳过字节数（`raw` / `pcap` 有效）
  - `file`：上传媒体或抓包文件
- `GET /api/offline/tasks`：列出离线解析任务
- `GET /api/offline/tasks/:id`：获取任务状态与 `summary.json`（含 flow、编解码、逐帧或 TS PAT/PMT/PES/NALU 明细）
- `GET /api/offline/tasks/:id/files/*name`：下载提取产物（如 `video.annexb`、`audio.adts.aac`、`mp4_report.txt`、`summary.json`）

---

## 3. HLS 交付分析 API（`/api/hls-stats/tasks`）

在排障工作台选择 HLS 协议并切换至「交付分析与 A/B 对比」使用，按播放列表抽样分片统计 TTFB、吞吐分位、缓存命中、CDN 与 PTS 时长偏差。

- `POST /api/hls-stats/tasks`：创建任务，返回 `{id, status}`
- `GET /api/hls-stats/tasks`：列出本次运行期间的 HLS 交付分析任务
- `GET /api/hls-stats/tasks/:id`：查询状态与报告（`exit_code` 为 2–6 时 HTTP 仍返回 200）
- `DELETE /api/hls-stats/tasks/:id`：取消运行中的任务（状态变为 `canceled`，保留已采集部分报告）

请求示例：

```json
{
  "command": "analyze",
  "url": "<hls-m3u8-url>",
  "segments": 10,
  "variant": -1,
  "parse_segments": true,
  "duration": "30s",
  "timeout": "10s",
  "error_threshold": 0
}
```

- `command` 支持 `analyze`、`manifest`、`monitor`、`compare`（需额外传 `url_b`）。
- `variant` 为 `-1` 测最高码率，`0` 测最低码率；`EXT-X-GAP` 缺片计入 `summary.segments_skipped`，不计入错误率。

---

## 4. Enhanced FourCC 归一映射

| 类型 | FourCC | 归一数字 ID | 说明 |
| --- | --- | ---: | --- |
| Video | `avc1` / `hvc1` / `vp08` / `vp09` / `av01` | `7` / `12` / `13` / `14` / `15` | H.264 / H.265 / VP8 / VP9 / AV1 |
| Audio | `.mp3` / `mp4a` / `Opus` / `ac-3` / `ec-3` / `fLaC` | `2` / `10` / `13` / `15` / `16` / `17` | MP3 / AAC / Opus / AC-3 / E-AC-3 / FLAC |

未在表中的 FourCC 将保留原字符串。
