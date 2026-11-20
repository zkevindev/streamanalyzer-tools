# 协议模拟器与场景回归指南

在顶部导航进入「模拟器」（`/simulators`），可模拟多类流媒体与安防协议设备或故障源，配合排障工作台完成联调与回归验证。

## 1. GB28181 模拟器

### 设备侧模拟（`/simulators`）

填写上级平台 SIP 地址、平台编号、起始设备/通道编号及鉴权信息，选择 SIP 传输（UDP 默认或 TCP）即可启动：

- **注册与目录**：支持 SIP REGISTER Digest 鉴权、定时心跳、续期、掉线重注册与注销；支持 Catalog 查询、订阅/续订/退订及通道手动上下线通知（ON/OFF）。
- **批量设备与通道**：单批次最多模拟 16 台设备 × 16 通道，同时最多建立 32 路媒体会话（每通道一路），可独立查看设备注册状态、媒体计数与点播时间线。
- **视频源与音频复用**：默认内置 H.264 测试画面（带 440 Hz G.711 A-law 测试音）；支持上传 512 MiB 以内本地视频（MP4/MOV/MKV/WebM/FLV/TS/PS/AVI，需 FFmpeg/ffprobe），H.264/H.265 直拷保真，音轨可选 G.711 A-law / μ-law 或仅视频。
- **媒体传输**：UDP 或 TCP（按平台 SDP `a=setup` 协商主动/被动连接，按 RFC 4571 分帧）。
- **语音广播与对讲**：启用后上报 137 类型语音输出通道，支持 Broadcast 反向 INVITE 下行 G.711 接收与两向对讲联调。
- **录像回放**：可将本地视频重复生成 1–8 段带缺口的可查询录像，支持 RecordInfo 查询、Playback 按时段回放、跨缺口连续时钟、INFO 暂停/继续/npt 定位/0.25–4 倍速与 MediaStatus 121 上报。
- **故障注入与证据导出**：支持延迟起播、时间戳跳变、音频中断、UDP 固定周期丢包与相邻包调序；勾选 RTP 采样后每次点播最多保存 4 MiB 原始 RTP（单批次上限 64 MiB），可导出脱敏证据 ZIP 并导入离线重算。

### 平台侧收流（`/simulators/gb28181/platform`）

- 启动本机测试平台，支持最多 16 台设备通过 SIP UDP/TCP 注册与并发点播，独立选择 UDP 媒体、TCP 主动或被动。
- 观测窗口统计匹配 SSRC 的 RTP、PS 单元与 H.264/H.265 关键帧，并按 Call-ID 关联设备模拟器时间线；支持暂缓 ACK/TCP 建连、断链重注册恢复、在线断流 3.5 秒后自动重连点播，以及录像检索回放与控制。

## 2. ONVIF 模拟器（`/simulators/onvif`）

- **设备发现与服务**：响应 `239.255.255.250:3702/UDP` 的 WS-Discovery Probe/Resolve；提供 Device、Media（Profile / `GetStreamUri` / `GetSnapshotUri`）、PTZ（绝对/相对/连续移动与预置位，快照反映转向缩放）与 Event PullPoint 服务。
- **码流与认证**：主码流 320×180@15fps、子码流 160×90@10fps，支持内置测试画面或 64 MiB 内本地视频，支持 RTSP Digest 与 RTP/UDP、RTSP/TCP 交织。
- **故障与异常场景**：
  - 「重启并更换端口」发送 WS-Discovery Bye 并以同 UUID 新端口发送 Hello。
  - SOAP UsernameToken PasswordDigest 校验、设备时钟偏差调节。
  - RTSP UDP 无媒体回退 TCP、UDP 来源端口变化、TCP 点播后视频停包、断线 503 重连、同会话切换分辨率、音轨 RTCP 时钟跳变。
  - PullPoint 移动侦测告警触发与事件服务重启测试。
  - **Profile G 录像回放**：提供 30 秒带可选缺口的历史录像与移动告警，支持 `Range: clock=...` 绝对时间回放与 457 边界响应。

## 3. 通用 SIP 模拟器（`/simulators/sip`）

- 支持 IPv4 UDP / TCP / TLS 信令（TLS 默认校验证书链，可附加测试 CA），支持 REGISTER Digest、续期、注销与 TCP/TLS 断线自动重连。
- 支持呼出/呼入/拒接/取消/挂断、`100rel` 与 PRACK 早期媒体协商、G.711 PCMU/PCMA 440 Hz 测试音、通话最后 20 秒收音试听、RTP DTMF（telephone-event）与 re-INVITE（保持/恢复/切端口）。
- **故障模拟**：单向暂停发包/丢弃收包、NAT 端口不一致与源地址学习、Session-Expires UPDATE 会话刷新与到期挂断。

## 4. HTTP-FLV / MPEG-TS 媒体源（`/simulators/media`）

- **HTTP-FLV**：提供 12 秒关键帧间隔、视频 DTS 回退等异常样本。
- **HTTP / UDP / RTP MPEG-TS**：支持视频 PID 连续计数缺口、单点 PCR 前移、PMT 换版与音视频 PID 切换、多节目复用单节目断流、单节目 PCR 相对 PTS 漂移，以及组播加入后 PAT/PMT 延迟（2.5 秒）。RTP 源使用 PT 33，支持独立注入丢包、乱序与重复。

## 5. HLS 故障源（`/simulators/hls`）

- 内置 24 秒（12 分片）H.264/AAC 测试流，支持指定分片延迟或返回 404，一键带入排障工作台并开启播放器分析。
- **滚动直播与高级场景**：2 秒滚动发布与手动冻结/恢复、一次性 404 或 HTTP 200 正文截断重试验证、双码率 fMP4 直播（160×90 与 320×180 含不连续切换）、独立音轨分片故障与跨段错位、AES-128 密钥轮换与 403 恢复、LL-HLS 400 ms Part 延迟，以及直播列表回退（旧列表 + Age）。

## 6. RTMP / RTMPS 故障源（`/simulators/rtmp`）

- **故障注入**：支持在握手、应用连接、查找流、播放后无媒体阶段注入故障，以及 RTMPS 临时自签名证书信任失败验证。
- **推拉流中继与主备切换**：支持 OBS/FFmpeg 向 `/live/test` 推流（可延后首关键帧、支持断推重连）；支持 `/live/test`（主路）与 `/live/backup`（备路）停发自动接管、主路稳定后回切，以及仅判断视频停发模式。

## 7. WHEP 模拟器（`/simulators/whep`）

- 提供本机 H.264 WebRTC WHEP 拉流源，支持正常播放、SDP 成功但 ICE 超时、ICE/DTLS 成功但无 RTP、出画后停发 RTP 四类场景，记录 HTTP 201、ICE/DTLS 状态、收包与首帧并导出 JSON，支持 DELETE 关闭会话。

## 8. 场景回归（`/simulators/scenarios`）

- 支持保存、导入和导出 GB28181、ONVIF、HLS、MPEG-TS 等自动化回归预设，自动拉起本机模拟器与排障/播放器链路并校验恢复指标。
- 最近 50 条回归结果持久化至应用数据目录（界面展示最近记录并支持 JSON 导出与前后对比）。
