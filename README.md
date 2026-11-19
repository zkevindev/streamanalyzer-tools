# Stream Analyzer Tools

音视频流分析工具，排障工作台支持 **HTTP-FLV / RTMP / RTSP / RTSPS / HLS / HTTP MPEG-TS / UDP MPEG-TS / RTP MPEG-TS** 采样、报告及证据包复盘。另提供实时帧分析和离线文件解析（raw / pcap / FLV / MPEG-TS / **MP4**）。离线结果会持久化到磁盘并在网页端可查看/下载。兼容 Legacy FLV 与 Enhanced RTMP/FLV（E-RTMP）头部解析。

## 排障工作台

启动 Web 服务后打开 [本机排障工作台](http://127.0.0.1:8087/diagnostics)。桌面版启动后直接打开同一个工作台。工作台和对应 API 仅接受本机连接，同一数据目录由一个服务实例使用。

1. 选择 HTTP-FLV、RTMP、RTSP、RTSPS、HLS、HTTP MPEG-TS、UDP MPEG-TS 或 RTP MPEG-TS，输入采样地址，指定 1–300 秒的采样时长，默认 60 秒。RTMP 同时接受 `rtmp://` 和 `rtmps://`。RTSP 与 RTSPS 分别使用对应地址方案。
2. 查看音频和视频编码、采样时序及异常。HTTP-FLV 和 RTMP 的报告可定位媒体帧的 PTS/DTS 及原始流字节范围。HLS 的报告可定位播放列表、初始化段和媒体分片，并展示原始请求耗时与 HTTP 状态。
3. 下载原始流、HTML/JSON 报告或证据 ZIP。导入时校验清单中的 SHA-256，重新解析原始数据并按采样时记录的时间线运行规则。源流可以已经下线。规则报告独立于模型，原有 HTTP-FLV v1 证据包也可导入。

| 协议 | 原始产物 |
| --- | --- |
| HTTP-FLV | `stream.flv`，采样期间读取的 HTTP 响应体 |
| RTMP / RTMPS | `stream.rtmp`，从服务端握手开始的原始接收字节。RTMPS 保存 TLS 解密后的 RTMP 协议数据 |
| RTSP / RTSPS | `stream.rtsp.zip`，包含服务端应用字节 `server-wire.bin`、DESCRIBE SDP 原文 `session.sdp` 及会话元数据 `session.json`；UDP 模式另含完整数据报 `udp-datagrams.bin` 和边界、到达时间、来源记录 `udp-index.ndjson`；RTSPS 在 TLS 解密后记录 |
| HLS | `stream.hls.zip`，原始 m3u8 快照、初始化段和 MPEG-TS、fMP4 或独立音频分片 |
| HTTP MPEG-TS | `stream.ts`，采样期间读取的原始 HTTP 响应体 |
| UDP MPEG-TS | `stream.udp`，按接收顺序拼接的完整 UDP 载荷，时间线记录每个数据报的边界 |
| RTP MPEG-TS | `stream.rtp`，保留 RTP 头部和载荷的完整数据报，时间线记录每个数据报的边界 |

UDP 与 RTP 使用 IPv4 接收地址，例如 `udp://0.0.0.0:5000` 或 `rtp://127.0.0.1:5000`，发送端向该设备及端口发包。组播示例为 `udp://239.1.1.1:5000?interface=en0`，`interface` 填写本机网卡名，省略时由系统选择网卡。RTP 当前检查静态载荷类型 33 的 MPEG-TS 数据。HTTP MPEG-TS 同时支持 HTTP 和 HTTPS。

摄像头和 NVR 可直接填写 RTSP/RTSPS 播放地址，支持 URL 用户信息认证。RTSP 可显式选择 TCP 交织或 UDP 单播 RTP/RTCP，默认 TCP，不会在 UDP 失败时自动切换传输；RTSPS 当前使用正常验证证书的 TLS/TCP。报告分别展示 DESCRIBE、SETUP、PLAY 等控制请求状态与耗时、DNS/TCP/TLS 阶段、SDP 声明轨道和实际 RTP 接收统计。UDP 额外展示收到、符合协商来源、被过滤及超过媒体库缓冲的数据报数量，并给出控制成功但没有媒体到达时的排查提示。首音视频时间指对应 RTP 包到达接收点，不是解码首帧。序号缺口和缺失包是接收点估计，需要与源端发送及网络证据对照。达到请求、轨道或包分析上限时会明确标注。

API 中可用 `{"protocol":"rtsp","transport":"udp","url":"rtsp://camera.example/stream","seconds":30}` 选择 UDP；省略 `transport` 仍使用 TCP。旧 TCP 证据包继续离线复盘。UDP 原始包保留符合及不符合协商来源的数据报；报告和模型不包含原始 IP/端口，轨道统计只计算符合来源与端口协商的包。

RTSP/RTSPS 原生采样不依赖浏览器。勾选播放器分析后，同一次采样收到的 RTP 可在本机转封装为 FLV 预览，不另建 RTSP 拉流连接；支持 H.264（packetization-mode 0/1）、H.265（RFC 7798，无 DON 交织）和 RFC 3640 AAC-LC（1024 采样、单/双声道），不转码。H.265 通过 Enhanced FLV 原生转封装，浏览器和系统需提供 HEVC 解码能力；转封装输出不代表已成功解码。Opus（48 kHz RTP 时钟、单/双声道）也可原码预览：本机先转封装为 Enhanced FLV，浏览器根据 MSE 能力选择 MP4 或 WebM 音频容器，不转码、不另建拉流；仅当两种容器均不支持时明确记录浏览器编码能力不足。Opus 包保留 2.5–120 ms 实际时长与 DTX/缺包间隔，不复制音频填充停发区间；真实空洞可能使播放器等待，需与原始 RTP 到达时间对照。G.711 PCMA/PCMU 可将同次采样的原始 RTP 在本机解码，再经 FFmpeg 有损转为 48 kHz AAC-LC 预览，支持 8/16/32/48 kHz、单/双声道；若有可直接预览的 AAC/Opus 轨道则优先原码。FFmpeg 可放在程序旁边、加入 PATH，或安装在常见 Homebrew/MacPorts 目录；未找到时明确提示音频预览不可用，原始采样继续。报告分别保存输入 PCMA/PCMU、输出 AAC、转码状态、输入/输出样本、RTP 缺口补静音、迟到丢弃、实测编码预延迟/填充及工具检查/首个输出/排空耗时；本机处理不计为网络阶段。较大 RTP 时钟跳变、队列上限或运行中编码失败会明确结束预览，原始 RTP 采样不受影响。新采样使用编码能力版本 3，旧版本报告导入后保留原始能力与结论。首屏、缓冲、卡顿、呈帧和页面调度记录沿用 `player.json`，与转封装状态一起保存在报告和证据包中；共同 RTP 时间基准由接收端建立，不等于验证了源端音画同步。原始 dump 仍为 `stream.rtsp.zip`，不替换为派生 FLV；RTSP 补查可依据原始 RTP 和已保存的播放器时钟离线重建 MP4/JPEG，定位方式及边界见下文。HTML/JSON 与模型只使用受控方法、状态、编码标签及数字证据，不展示 RTSP 地址路径、用户信息、认证头或 SDP 原文；来源只显示协议和主机。原始 ZIP 按上述边界保存服务端字节和 SDP，证据包导入后可离线重算，无需重新连接摄像头。

排障工作台提供「ONVIF 设备发现」：选择本机 IPv4 网卡后搜索摄像头/NVR，选择设备并填写 ONVIF 凭据，读取 Profile，再选择 TCP/UDP 获取 RTSP 地址。地址只回填到采样表单，由用户核对后启动采样。未搜到设备时可手动输入服务地址（如 `http://192.168.1.64/onvif/device_service`）；组播发现通常限于本地网段，跨 VLAN 可使用手动地址。macOS 需允许应用访问本地网络。

ONVIF 支持 WS-Security PasswordDigest、设备 UTC 偏差校正以及一次 HTTP Digest 挑战重试。凭据仅用于当前请求，不写入发现列表或磁盘；默认不把 ONVIF 凭据复制到 RTSP，勾选复用后也只允许相同主机。切换设备、Profile 或传输会取消过期请求，避免旧响应回填到新的选择。

发现 API 位于 `/api/diagnostics/onvif`：`GET /interfaces`；`POST /discoveries`（`bind_ip`、1–10 秒）；`GET /discoveries/:id` 和 `POST /discoveries/:id/stop`；`POST /profiles`（设备或手动服务地址、凭据）；`POST /stream`（Profile、传输、凭据）。任务和选择状态短时保留，退出时关闭扫描及请求。

传输流报告展示 TS 包长度、PID 与编码、PAT/PMT、TEI、连续计数、重复包、同步丢失以及 PCR/PES 时间戳检查。RTP 报告补充序列缺口、重复和乱序统计。序列缺口反映当前采样窗口中的接收结果。原始数据和到达时间同时保留，导入时重新运行这些检查。

连接报告逐次记录 HTTP 请求及跳转响应，分别展示 302、200 等状态、单次请求耗时和首字节时间，并提供可观测的 DNS、TCP、TLS、发送请求、等待首字节和接收响应头的阶段耗时。HLS 的播放列表、初始化段及媒体分片请求也保留这些时序信息。RTMP 单独展示 TCP、TLS、协议握手、应用连接、创建流和播放协商阶段。

HTTP-FLV、HTTP MPEG-TS 和 HLS 的每次请求可以展开查看请求头、响应头、请求方法和 HTTP 版本，包含各次跳转。头字段保留重复值，便于核对 Content-Type、Cache-Control、Age、Via、X-Cache、请求 ID 和跳转目标。字段经过协议库归一化，未收到响应和旧报告未采集头字段的情况会分别提示。HLS 展示的是采样连接的头字段。

头字段落盘前会对 Authorization、Cookie、Set-Cookie、API Key、Token 等敏感名称脱敏，URL 类型头字段移除查询参数、片段和用户信息。每组请求头或响应头最多记录 32 个字段、合计 2 KiB，单个值最多 512 字节，达到限制时标记截断。HTML/JSON 报告和证据包保留相同的脱敏记录，导入后无需访问源站即可查看；大模型仍只接收耗时与规则摘要，不接收头字段原文。

首包、首个视频数据、首个音频数据和首个关键帧按采样起点展示到达时间。FLV/RTMP 使用解析到的媒体帧，MPEG-TS 使用已识别媒体 PID 上的 PES 起始，HLS 使用已解析媒体类型的分片。已有报告还会展示音视频接收间隔、时间戳和关键帧异常、重复异常次数及后续检查建议。

采样时勾选「同时分析播放器体验」，工作台会播放并记录首屏、首次开始播放、可播放缓冲变化、卡顿次数、累计及最长卡顿时长。首屏从提交采样开始计时，优先使用浏览器实际呈现视频帧的回调，缺少该能力时明确标为首帧加载估算。缓冲曲线和卡顿区间对齐后端采样时间轴，与 DNS、TCP、HTTP 跳转及首音视频等阶段耗时放在同一份报告中。播放器音视频解码能力取决于当前系统和浏览器；不支持的编码或播放失败会留下具体状态，拉流采样及 dump 仍可继续。

卡顿从首次开始播放后统计，持续等待至少 500 ms 计为一次。启动等待、手动暂停、跳转和页面隐藏期间排除，正常采样结束造成的断流会截断计时。HTTP-FLV、RTMP 和 TS 播放使用本次采样转发的数据，HLS 使用独立连接播放同一地址，报告会标注来源。播放器结果反映本机播放环境，分析客户现场问题时还需对照客户的播放器埋点。

播放器还会记录浏览器提供的视频帧计数、丢帧计数及连续视频帧回调，展示连续前台播放样本间的帧增量、丢帧比例、计数重置和最大呈帧回调间隔。优先使用 `getVideoPlaybackQuality`，兼容 WebKit 计数；不支持时标记未观测，实测零值单独保留。呈帧回调间隔受内容帧率、浏览器呈现和主线程调度影响，用于补充排查方向。启动、暂停、跳转、后台和采样结束后的时段排除统计。新版 v2 播放器记录在 `player.json` 中保留原始数字，旧版 v1 记录仍可导入；补查窗口、HTML/JSON 和模型数字摘要同步包含可用指标。

播放器原始事件和缓冲样本写入证据包的 `player.json` 并校验 SHA-256，导入后离线重算播放器指标和卡顿区间，无需重新播放源流。HTML/JSON 报告包含相同结果；模型解读按需接收播放器指标、卡顿区间及对应证据。页面退出或网络中断前已保存的部分记录会保留，并标明采集状态。

采样和播放器记录保存完成后，可以在卡顿区间中选择一次卡顿，点击「补查证据」。后台任务关联卡顿前后 3 秒的音视频接收间隔、缓冲样本、最近的可确认关键帧、相关 HTTP 请求及已有异常，并给每条关联证据分配独立 ID。HLS 的独立连接仅作同时段参考；TS 等未确认到关键帧时会明确说明。每份报告最多补查 8 次卡顿，支持查看状态、停止任务、重试尚无媒体产物的任务和重新请求模型建议。

媒体提取需要本机 FFmpeg 和 ffprobe，可放在程序旁边或加入 PATH；macOS 还会检查常见的 Homebrew/MacPorts 安装目录。工具只读取采样文件，在本地生成最多 15 秒的 H.264/AAC MP4 和异常前、附近、后的三张 JPEG。片段和截图来自源流，位置按播放器媒体时钟估算；HLS 使用采样分片的接收窗口定位。它们用于复核媒体内容，原网络等待保留在时序证据中。缺少工具、无可用时钟、纯音频、加密分片或解码失败时会显示具体原因，时序证据仍保留。原始 dump 不会被改写。

已配置大模型时，补查会自动请求带证据引用的排查建议。当前模型接收关联时序、媒体产物状态和证据 ID，不接收截图像素或视频文件，页面标注为「模型建议（时序证据）」。未配置、连接失败或切换模型时，可以保留结果并稍后重新解读。

补查还会在本地生成「分层排查方向」，区分已观测、待验证和证据不足，并关联证据 ID 与下一步检查。缓冲接近耗尽且同次采样的接收间隔重合时，优先核对源端与传输供给；连续缓冲充足但呈帧回调延后时，优先核对本机播放与调度；丢帧增量作为独立观测保留。规则只使用选中卡顿内的前台样本，跳过暂停、跳转、后台及稀疏样本之间的计数差，累计最大回调间隔不作为该次卡顿的证明。HLS 独立采样不用于断定实际播放连接中断。这些方向需要现场验证，模型也会接收相同限制。

分层结果会随 HTML/JSON 报告和证据包保存，导入时从已验证的关联证据重新计算并比对，不需模型在线。旧补查记录可点击「生成排查与模型建议」补充分层结果，未配置模型时也能生成本地排查方向。

RTSP / RTSPS 的缓冲卡顿和呈帧回调间隔补查可从 `stream.rtsp.zip` 中离线重建同期源媒体，无需摄像头在线，也不重新拉流。支持 H.264、H.265、AAC、Opus 和 G.711（PCMA/PCMU），G.711 先解码为 PCM；本机 FFmpeg/FFprobe 将可用媒体转为 H.264/AAC MP4，并提取异常前、附近和之后的 JPEG。音频源可以只生成音频片段；工具缺失、编码不支持、无随机接入点、媒体不完整或无法建立播放器时钟关联时保留具体原因及其余时序证据。

RTSP 媒体定位标为「接收端 RTP 时钟与播放器媒体时间估算」。报告展示保存的播放器时钟样本、预览和重建起点、选中轨道、访问单元数量、丢弃记录，以及原始归档、时间线、播放器记录和临时重建媒体的 SHA-256。离线确认的随机接入点附原始 RTP 包引用；该引用指访问单元完成时的包，不能把单个 RTP 包当作完整画面。收包到达时间不会直接作为媒体时间，定位不代表源端 UTC、源端音画同步或精确呈帧对齐。片段与截图用于内容对照，不能重现或证明本机画面冻结。模型接收数字摘要和产物编号，不接收截图像素，也不能据此声称已查看画面。

补查记录保存为 `stall-evidence.json`，派生文件位于证据包的 `stalls/S001/` 等目录，均记录 SHA-256。HTML 报告内嵌截图，片段可单独下载或从证据包打开。导入会校验派生文件并从原始流重新计算关联证据，不需 FFmpeg、源站或模型在线。导入未完成的任务会标为已停止，可以重新补查。旧版证据包仍可导入。

HLS 原生播放和 hls.js 播放均经过本地代理。实际播放请求会单独保存到 `player-network.json`，包含匿名资源编号、资源类型、GET/HEAD、请求开始、响应头、首正文、代理结束或快照截止时刻、HTTP 状态、上游读取字节和完成/失败/取消状态，每份报告最多保留 512 次请求并标明超限数量。不记录源 URL、令牌、HTTP 头内容或错误原文。响应头和首正文未观测时保留未知，实测零值单独显示。代理结束时间可能包含向浏览器写出的影响；这些记录不能证明浏览器已接收、追加缓冲或呈帧。

播放器记录 v3 还会保存「页面调度观测」：支持 Long Tasks API 时记录至少 50 ms 的浏览器长任务，另外记录 250 ms 采样定时器至少 100 ms 的实际延后。两种证据分开保存，每类最多 256 条，并标明接口支持状态、观测范围和丢弃数量；不保存任务名称、脚本地址或 attribution 信息。不支持原生长任务时仍可记录定时器延后，不能用其代替原生长任务计数。

调度指标按前台播放和实际观测覆盖裁剪，排除启动、暂停、跳转、后台和采样结束后的时段；区间数与累计时长描述裁剪后的观测片段。没有有效覆盖或接口不支持时显示未观测，有覆盖且未发现事件才显示零。调度记录不会增加播放器卡顿次数，也不会插值补出缺测缓冲。选中卡顿后，报告关联相交的调度区间；连续重合至少 500 ms 时增加页面调度待验证方向。它只能帮助核对页面性能、事件回调和媒体线程，不能直接证明 CPU 占用、解码故障或画面冻结。原始区间、同版规则和模型数字摘要随报告与证据包保存，v1/v2 播放器和旧规则继续支持离线复盘。

播放器记录 v4 进一步保存独立的「呈帧回调间隔」。在至少三个有效回调媒体时间差建立基线后，记录连续前台播放中两端均有视频帧回调、且间隔达到 `max(1000 ms, 3 × 先前回调媒体时间间隔中位数)` 的完整区间。基线是浏览器回调中的媒体时间估算，不是编码器声明帧率；低帧率、帧率切换和本机调度仍需对照验证。暂停、跳转、后台及采样结束跨越的区间不保留，不补推未恢复的尾段，每份记录最多保留 128 个区间并显示丢弃数量。缺少接口、基线未就绪、没有有效覆盖与实测零分别显示。

即使浏览器没有发出 waiting、缓冲卡顿次数为零，也可以选中一次呈帧回调间隔补查。该类型与缓冲卡顿分别编号，两类补查合计最多 8 次；报告保留真实重合时长，不把两种观测相加为新的卡顿次数。补查仍关联缓冲、收流、关键帧、实际请求和页面调度，提取同期源流片段及截图，供模型根据时序证据提出验证建议。源流产物不能重现或证明本机画面冻结。证据包按类型与编号校验并离线重建，旧播放器 v1–v3 与旧补查记录继续兼容。

选择卡顿补查时，实际播放请求与独立采样连接的请求分开展示，窗口最多保留 16 次实际请求，优先保留选中卡顿内的异常。GET 媒体、初始化或密钥请求失败，或代理处理至少 500 ms，并且在该请求附近观测到低缓冲时，会增加「优先核对实际 HLS 播放请求」待验证方向，样本时间容差为 500 ms。取消和未完成请求仅作为状态展示。采样连接正常不代表播放连接正常，反之亦然。新版规则与旧规则分别重算，旧证据包仍能按原版本离线复盘。

实际播放请求的 v2 台账还会记录逐跳 HTTP 状态（例如 302→200）、DNS、TCP、TLS、请求写出、响应首字节、响应头和最终响应体阶段，以及连接是否复用和受控失败类型。每请求最多 8 跳、每跳最多 16 个阶段，逐跳元数据合计上限 256 KiB，超限会明确记录丢弃跳数。首字节耗时为每跳相对耗时，其余时刻与采样时间轴对齐；连接复用或接口未给出某阶段时显示未观测，测得的零单独保留。响应体阶段表示代理的上游读窗口，可能包含读写交错和等待，不等于纯下载耗时或浏览器接收耗时。

卡顿补查只关联同时间段且有证据支持的阶段异常；末跳被截断时不会借用前面成功跳的阶段解释失败。报告展示全部已保留阶段，模型摘要另设 48 跳、192 阶段的总预算，并区分「采集时丢弃」与「模型摘要省略」。原 v1 网络台账以及旧排查规则仍按原版本校验和重算。

HLS 主清单选取带宽最高的常规变体及其默认独立音频 rendition。直播从清单末尾最多三个完整分片开始采样，随后跟踪新分片，点播按清单顺序下载。加密分片保存原始密文，媒体识别限于能够检查的明文分片。原始产物可能包含签名 URL 或业务元数据。停止、读流失败或达到 128 MiB 的原始产物上限时，工作台保留已收集的数据和可用报告。

编码名称取自采样数据，例如 H.264、H.265、AV1、VP9 以及 AAC、Opus、MP3、AC-3、E-AC-3。识别范围取决于协议与封装，工具保留原始数据用于复盘。

JSON 报告及 SQLite 任务索引位于数据目录下的 `diagnostics/`。基础接口包括：

| 方法与路径 | 用途 |
| --- | --- |
| `POST /api/diagnostics/runs` | 用 `{"protocol":"hls","url":"https://live.example.com/index.m3u8","seconds":60,"playback":true}` 发起采样。协议可选 `http-flv`、`rtmp`、`rtsp`、`rtsps`、`hls`、`http-ts`、`udp-ts`、`rtp-ts`，省略时默认 `http-flv`。`playback` 可选；RTSP/RTSPS 使用同次原生采样重组的本机预览，编码能力和本机转码状态在报告中展示 |
| `GET /api/diagnostics/runs` | 查看最近的采样和复盘记录 |
| `GET /api/diagnostics/runs/:id` | 查询状态、规则报告和已生成的模型解读 |
| `POST /api/diagnostics/runs/:id/stop` | 停止采样并保存已获取的数据 |
| `POST /api/diagnostics/runs/:id/player` | 用会话授权头 `X-StreamAnalyzer-Player` 保存追加的播放器事件和缓冲样本；`complete` 后记录不可更改 |
| `POST /api/diagnostics/runs/:id/stalls` | 用 `{"stall_index":1}` 补查第 1 次缓冲卡顿，或 `{"interval_kind":"presentation_gap","interval_index":1}` 补查第 1 个呈帧回调间隔；无媒体产物的终态任务可加 `"retry":true` 重试 |
| `POST /api/diagnostics/runs/:id/stalls/:caseID/explain` | 补全旧记录的本地分层方向，并使用当前模型重新生成该次卡顿的时序证据建议 |
| `POST /api/diagnostics/runs/:id/stalls/:caseID/stop` | 停止正在进行的补查 |
| `GET /api/diagnostics/runs/:id/stall-assets/:caseID/:filename` | 访问报告中声明的 MP4 或 JPEG 产物，支持片段 Range 播放 |
| `GET /api/diagnostics/runs/:id/{dump,report.html,report.json,evidence.zip}` | 查看或下载产物。原 HTTP-FLV `stream.flv` 路径仍可用 |
| `POST /api/diagnostics/import` | 请求体传 ZIP 原始字节，类型为 `application/zip` |
| `POST /api/diagnostics/runs/:id/explain` | 请求模型解读报告 |

### 协议模拟器

#### GB28181

在排障工作台顶部打开「模拟器」，或在本机访问 [GB28181 模拟器](http://127.0.0.1:8087/simulators)。填写上级平台的 SIP 地址、平台编号、起始设备和通道编号，以及平台要求的鉴权信息，选择「SIP 传输」后点击「启动模拟设备」。信令支持 IPv4 UDP（默认）和 TCP，媒体传输由平台在点播时单独协商。

- 设备通过 SIP REGISTER 接入，支持 Digest 鉴权、定时心跳、注册续期和掉线重注册。停止已注册的设备时向平台发送注销请求。
- 「批量设备与通道」支持一次启动最多 16 台设备，每台最多 16 个通道。编号保留前 13 位。第 i 台设备和它的首通道在各自起始编号上增加 i ×（每台通道数 + 1），i 从 0 开始。其余通道连续递增。如果编号在批次内冲突，调整起始编号后再启动。本机 SIP 端口设为 0 时逐台自动分配，指定端口时从该端口连续分配。
- 每台设备独立注册、保活和注销，整个批次最多同时建立 32 路媒体会话，每通道一路。批次共用选定的视频源，单路 BYE 只结束对应通道的会话。「设备状态」可切换设备与通道，分别查看注册、实时媒体计数和点播时间线。UDP Catalog 按通道分条上报，TCP Catalog 一次返回该设备的完整目录。
- 平台可订阅和续订目录，也可通过 Expires 0 取消订阅。工作台可让选定通道上线或下线。有效订阅收到 ON 或 OFF 通知，后续 Catalog 查询也返回对应状态。离线通道会结束自身媒体会话并暂拒点播。
- SIP TCP 模式由设备主动连接平台。注册、心跳、目录查询及点播请求和应答复用这条连接。连接断开时设备释放当前点播会话，建立新连接并重新注册。本机 SIP 端口在 TCP 模式下是出站连接的源端口，设为 0 时由系统分配。
- 平台可查询通道目录（Catalog）、设备信息和设备状态。设备收到点播 ACK 后以 PS/RTP 循环发送所选视频。「视频源」留空时使用内置 H.264 测试画面，可直接运行。
- 本地视频支持 MP4、MOV、MKV、WebM、FLV、MPEG-TS、MPEG-PS、AVI，单文件上限 512 MiB。需要 FFmpeg 和 ffprobe，可放在程序旁边或加入 PATH。程序直拷 H.264 与 H.265，保留 GOP 和 90 kHz PTS/DTS 的相对关系及亚毫秒精度。其他视频编码使用 FFmpeg 的 `libx264` 转为 H.264。
- 对于带音轨的本地视频，可选 G.711 A-law（默认）、G.711 μ-law 或仅视频。音频以 8 kHz 单声道复用到同一路 PS，音频包通常为 20 ms。纯视频文件按视频推送。工作台显示所选文件、编码处理方式及已发送的音视频数量。文件保存在本机临时目录，整个批次退出后清理。
- TCP 按平台 SDP 协商连接方向。平台发 `a=setup:passive` 时，设备收到 ACK 后连接平台端口。平台发 `a=setup:active` 时，设备在 SDP 应答中公布监听端口，等待平台连入。省略 `setup` 或使用 `actpass` 时，设备选择被动监听。被动模式接受来自 SDP 媒体 IP 或 SIP 平台 IP 的连接。TCP 上的 RTP 按 RFC 4571 分帧。
- 工作台显示注册状态、媒体协商和连接状态、两端媒体地址、连接错误、RTP 包计数及最近 100 条信令。可以展开 SIP 报文，也可以用「导出 JSON」保存当前状态及信令。界面和导出文件中的 SIP 鉴权头均已脱敏。

- 点播时间线按 Call-ID 显示 INVITE、200 OK、ACK、媒体就绪、首 RTP 包、首视频关键帧和 BYE。勾选 RTP 采样后，每次点播最多保存 4 MiB 发送端原始 RTP 和相对发送时间。每台设备保留活动会话及最近的已结束会话证据，整个批次最多采样 64 MiB RTP。可导出脱敏证据 ZIP，再通过模拟器导入离线重算 RTP 序列缺口、乱序和 PS 封装指标。采样观测点位于模拟器发送端。

- 启用「语音广播和对讲」后，每台设备在目录中上报一个 137 类型的语音输出通道。平台发送 Broadcast 通知后，设备应答并反向发起 INVITE，通过 UDP 接收 G.711 A-law / μ-law RTP。设备状态显示下行包数和解码后的 PCM 峰值。按 GB/T 28181-2022，两向对讲由广播下行和实时点播上行组成。内置视频的点播上行带 440 Hz G.711 A-law 测试音，本地视频沿用选择的音轨。

- 「故障场景」可延迟起播，按媒体时间触发时间戳跳变、音频中断、按固定包序丢包或交换相邻包。丢包和调序只对 UDP 媒体生效。会话时间线和证据记录配置、触发时刻和实际注入数量。

- 本地文件可重复生成 1–8 段可查询录像，并设置段间缺口。可选「录像开始时间」，默认最后一段在设备启动时结束。跨段播放跳过缺口并保持输出时钟连续。只落在缺口内的查询返回空目录，只落在缺口内的点播拒绝建立会话。平台可用 RecordInfo 查询，再按 SDP 的 Playback 时间段回放，结束时设备上报 MediaStatus 121。会话内 INFO 支持暂停、继续、npt 定位和 0.25–4 倍速。直拷视频定位按源 GOP 对齐，倍速音频同步调整采样数量和时间戳，音调随倍速变化。


在 GB28181 页打开「平台侧收流」可启动一个测试平台。可配置最多 16 台设备用同一种 SIP UDP 或 TCP 注册，每台设备拥有独立的点播与恢复状态，TCP 点播信令复用注册连接。默认只监听本机，设备密码留空。把实际 SIP 地址填入设备配置，注册后输入通道编号并点播。平台在观测窗口里统计匹配 SSRC 的 RTP、PS 单元和 H.264/H.265 关键帧，并按 Call-ID 关联本工具设备模拟器的时间线。可独立选择 UDP 媒体、TCP 平台主动或被动。可暂缓 ACK 或主动 TCP 建连，区分信令、媒体协商、建连和 RTP 收流失败。平台主动连接使用与注册设备相同的 IP。勾选「检索指定时段并回放录像」后，平台先收集指定通道的 RecordInfo，再发起 Playback 点播。收流时可暂停、继续和按秒定位，时间线保留目录、回放控制 SIP 应答、RTP 与 MediaStatus 121。选择设备后可并发点播不同设备，导出 JSON 保留各设备的通道、Call-ID、SSRC 与媒体统计。

平台点播也可在收到首个关键帧后断开 SIP TCP，或忽略 UDP 心跳直到设备重新注册。重新注册后平台查询在线目录并重新点播，报告关联中断前后的 Call-ID、上一会话结束和恢复关键帧耗时。「场景回归」提供两种预设，UDP 预设使用 5 秒心跳。「在线断流后恢复点播」在首个关键帧后跟踪有效 RTP，连续 3.5 秒无包即结束旧会话，核对在线目录并重新点播。预设验证停流后心跳仍获应答，导出前后 Call-ID、媒体地址、SSRC 和恢复耗时。

#### ONVIF

打开「模拟器」并切换到「ONVIF」。本机联调可用默认的 127.0.0.1。供局域网客户端发现时，选择客户端可达的本机 IPv4 网卡。ONVIF HTTP 和 RTSP 端口填 0 时自动分配，启动后可在「设备状态」复制实际地址。

- 设备响应 WS-Discovery Probe 和 Resolve，组播地址为 `239.255.255.250:3702/UDP`。
- ONVIF Device 服务提供设备信息、Scope、服务和能力查询。Media 服务提供 Profile、视频源、编码配置、`GetStreamUri` 和 `GetSnapshotUri`。
- PTZ 服务按 Profile 提供绝对、相对和连续移动、停止、位置查询与预置位管理。JPEG 用对应视频的首帧模拟转向与缩放，工作台可打开快照查看效果。RTSP 继续播放原视频。
- RTSP 主码流为 320 × 180、15 fps，子码流为 160 × 90、10 fps。两路各有 Profile 和 RTSP URI，可分别选择内置测试画面或 64 MiB 以内的本地视频。需要 FFmpeg 时，程序将本地视频转为相应的 H.264 画面并循环发送。填入用户名和密码即可对两路 RTSP 启用 Digest。支持 RTP/UDP 和 RTSP/TCP 交织。
- 「鉴权、时钟和媒体故障」可要求 SOAP UsernameToken PasswordDigest、调整设备时钟，或令 RTSP UDP 的 SETUP/PLAY 成功但没有 RTP，供客户端验证 TCP 回退。页面分别计数 SOAP 鉴权失败、时钟偏差失败及 UDP/TCP PLAY。可开启 TCP 点播后视频停包，首次 TCP PLAY 两秒后暂停视频 RTP 五秒，G.711 测试音持续，恢复时从关键帧开始。将 RTSP 地址填入排障工作台，报告可核对视频轨的 RTP 到达缺口和恢复。

RTSP 断线模式在每个 Profile 首次点播 3 秒后关闭已有会话，在设定窗口内对同一地址返回 503，之后允许重新建立会话。「场景回归」会用新的 RTSP 采样尝试恢复并保存每次协商结果，记录重连次数和恢复呈帧时刻。「RTSP UDP 无媒体后回退 TCP」先验证 UDP PLAY 成功且首包窗口内无 RTP，再为同一地址重新建立 TCP 会话。场景结果包含两次协商、回退原因和恢复呈帧耗时，并链接到两次原始报告。「UDP 来源端口变化」在同一次 PLAY 中短暂更换实际 RTP 来源端口，报告展示协商端口、实际端口、过滤包数和回到原端口后的首包，场景同时检查继续呈帧。

「同会话切换分辨率」在主码流的同一 RTP 会话中切入子码流尺寸。场景回归验证浏览器实际呈现切换前后的尺寸，报告记录 H.264 转封装配置。

RTSP 音轨时钟跳变场景持续正常发送 H.264 和 G.711 RTP，在首次 PLAY 三秒后改变音轨 Sender Report 的 RTP/NTP 映射。TCP 与 UDP 报告按轨道和 SSRC 展示发送端时钟偏移与独立计算的接收间隔偏差。

事件服务支持 PullPoint 订阅、续订、取消和到期。NVR 订阅后，在模拟器点击「触发移动侦测」和「结束移动侦测」，可拉取 `RuleEngine/CellMotionDetector/Motion` 的两次状态变化。页面显示订阅数、拉取次数和通知数。点击「重启事件服务」立即让现有 PullPoint 失效，并解除正在等待的 PullMessages，原 RTSP 会话继续。状态记录旧订阅故障、首次重新订阅和重启后第一条移动告警送达。

启用「录像检索与按时间回放」可测试 ONVIF Profile G 的录像列举、搜索和回放子集。模拟器提供一段 30 秒录像和两个历史移动告警时间，其中一个落在录像区间内。客户端从 ONVIF 获取录像 RTSP 地址，用 `Range: clock=...` 请求绝对时间；命中时回放 H.264，区间外返回 457。启用录像缺口后，搜索返回整条录像的 UTC 起止范围；状态列出两段实际录像。定位到缺口返回 457，从已有区间开始的跨段回放跳过缺口，保留请求 Range、返回范围、实际分段与各段首关键帧时刻。

#### 通用 SIP

在「模拟器」切换到「通用 SIP」，填写 PBX 地址、分机号和鉴权信息后注册。信令支持 IPv4 UDP、TCP 和 TLS。TCP/TLS 在同一条外连信令连接上收发呼叫。TLS 默认校验证书链与主机名，可附加测试 CA。阶段状态区分连接、TLS 握手、注册和 Digest 鉴权。三种传输均支持 REGISTER Digest 鉴权、续期和注销。SIP 域和 Digest 用户名可按 PBX 配置单独填写。

呼出会声明支持 `100rel`，收到可靠临时应答后自动发送 PRACK。开启「对支持 100rel 的呼入等待 PRACK」后，模拟器发送带 RSeq 的可靠 183，收到匹配的 PRACK 才发送早期测试音。工作台显示临时应答、PRACK 及应答和早期 RTP 的时间，PRACK 等待窗口为 4 秒。

- 注册后可输入分机号或 SIP URI，通过所填服务器呼出，也可接听或拒绝呼入。振铃时可取消呼出，已接通的任一方可通过 BYE 挂断。每次模拟一台分机、一路通话。
- 通过 SDP 协商 G.711 PCMU 或 PCMA。通话时按 20 ms 发包发送 440 Hz 测试音，显示双向 RTP 包计数和音频端点。可试听当前或最近一次通话收到的最后 20 秒音频。
- 通话中可暂停发送或丢弃接收 RTP，模拟单向无声。工作台分方向显示通过与注入丢弃数、首包和末包时间、序列缺口与抖动。
- 「通话场景预设」可在每次接通后自动暂停发送并恢复。默认接通 3 秒后触发、持续 5 秒。结果检查是否有注入丢包、同期接收是否持续，以及恢复后是否重新发包。
- 支持接收 183 SDP 早期音频，也可让模拟分机在呼入振铃时提前发测试音。早期和接通后的 RTP 计数分别显示。通话协商 telephone-event 后可发送和接收 RTP DTMF。
- 通话中可应答对端 re-INVITE，支持保持、恢复和切换 RTP 地址或端口。收到匹配 ACK 后应用新 SDP，工作台显示媒体方向和最近一次端点变化。
- 工作台显示 SIP 方法和响应码，便于对照 REGISTER、INVITE、ACK、CANCEL 和 BYE。


通用 SIP 可让本机实际 RTP 端口与 SDP 声明端口不同，复现端口映射造成的单向无声。也可选择按对端 SDP 回包，或学习同一对端 IP 的第一个有效 G.711 RTP 来源。每次媒体重新协商会重新学习。界面记录双方 SDP 地址、实际来源及最后成功发送的目的地址。

「SIP 会话刷新」协商 Session-Expires 后以 UPDATE 刷新，可拒绝或忽略对端 UPDATE，或暂缓本机刷新以复现到期挂断。默认最短周期 90 秒，本机联调可缩短。电话状态记录刷新方、最近应答、截止时间和到期结果。

#### HTTP-FLV / MPEG-TS 媒体源

在模拟器选择 HTTP-FLV 或 HTTP MPEG-TS，启动后可带入排障工作台。HTTP-FLV 提供 12 秒关键帧间隔和视频 DTS 回退。MPEG-TS 可在视频 PID 丢一个包以制造连续计数缺口，或前移单个 PCR 后返回原时钟。每次请求从同一位置开始，按原媒体节奏发送，可在报告定位到对应标签或包。UDP／RTP MPEG-TS 支持 IPv4 单播和组播，可为组播指定网卡。先启动排障采样，再点击「发送一轮」。RTP 源使用 PT 33，可独立注入丢包、乱序或重复。从发送端触发次数和接收报告核对 RTP 序号与 TS 连续计数。

MPEG-TS 的「PMT 换版与音视频 PID 切换」在关键帧前发布新节目映射，保留传输顺序和连续计数。排障报告记录 PMT 版本、PCR 和媒体 PID 的变化，以及新映射宣告后各 PID 的首个有效包。「多节目复用中单节目断流」同时发送两个节目，节目 1 更换 PMT、音视频和 PCR PID 并短暂停发，节目 2 持续。报告可对照新 PID 首包以及各节目在切换后的收包和连续计数。「单节目 PCR 相对 PTS 漂移」保持两个节目正常发包，只加速节目 1 的 PCR。报告按节目和媒体 PID 比较首尾 PTS−PCR 中位数，采用近邻 PCR 且跨时钟重置后重新建立基线。

HTTP-FLV 采样报告和导入的 HTTP-FLV 证据包可点击「使用本机回放源」。采样表单会填入可复制的本机 URL。每个客户端各自从头读取，原始 FLV 字节、标签到达间隔和流结束前的等待来自证据包。适合用多个播放器复现同一份接收时序。

#### HLS 故障源

在「模拟器」打开「HLS」。内置 24 秒的 H.264/AAC 测试流提供 12 个分片。可选正常播放、指定分片延迟或指定分片返回 404。故障对目标分片的每次 GET 生效，状态区记录触发次数与最近触发时间。用「打开排障工作台」带入播放地址，并开启播放器体验分析。

选择「滚动直播」时，每 2 秒发布一个新分片，列表保留最新 3 段。可手动冻结和恢复更新。工作台记录每次列表请求收到的起始序号，排障报告的实际播放请求也显示该序号，便于识别 HTTP 200 下的停更。直播模式还可按媒体序号安排一次性 404 或 HTTP 200 正文截断，下一次请求恢复完整分片。默认仅作用于工作台的实际播放连接，后台采样独立运行。源站状态显示是否触发及是否收到重试，实际恢复时间以播放器时间线为准。「测试播放器重试」使用 hls.js 并显式允许首次 404 或正文错误后重试，用于验证恢复路径。

「双码率 fMP4 直播」提供 160 × 90 和 320 × 180 两档，分片对齐且各自包含初始化段。每轮测试流重启时间线时声明不连续并发布新的初始化段 URI。通过「打开排障工作台」可用 hls.js 选清晰度，报告保留实际初始化段和媒体请求。

「独立音轨分片故障」提供分离的 H.264 视频与 AAC 音频列表。指定序号的音频分片可首次 404 或截断正文，重试返回完整分片，视频继续发布。报告在实际播放请求上标记「独立音轨」，便于定位故障和重试。

「独立音轨跨段错位」视频在序号 7 切换不连续边界及初始化段，音轨到序号 8 才切换，分片请求正常返回。场景回归保存播放器看到的两轨边界、实际缓冲的媒体时间和后续呈帧。

「AES-128 密钥轮换」在媒体序号 6 切换加密密钥。「轮换后新密钥返回 403」保持列表和加密分片可访问，点击「恢复新密钥」后允许该密钥请求。场景回归确认新密钥对应分片实际呈帧，保存密钥 HTTP 状态及恢复方式。

「低延迟直播 / Part 延迟」发布 400 ms fMP4 Part，支持带媒体序号和 Part 编号的阻塞式列表刷新。可安排目标 Part 延迟响应并继续发布列表。报告区分阻塞式列表刷新和 Part 请求，场景回归额外保存恢复呈帧后 hls.js 估算的直播延迟。

「直播列表回退」保持源站发布，在同一播放地址返回一次 HTTP 200 旧列表。请求台账记录列表序号和响应 Age，场景回归保存旧列表、源站序号和恢复呈帧证据。

#### RTMP／RTMPS 故障源

本机 RTMP 源可发送内置音视频，或在握手、应用连接、查找流、播放后无媒体等环节触发故障。RTMPS 场景使用临时自签名证书验证证书信任失败。通过「打开排障工作台」带入地址，可核对实际失败阶段与服务器触发计数。

接收推流模式允许 OBS 或 FFmpeg 向本机 `/live/test` 发布，拉流客户端使用同一地址。可延后首个可转发视频关键帧，并记录发布确认、首个媒体包、首个上游关键帧和断开时间。推流断开后，等待中的拉流会话可接收同一流名的新一轮推流。

「主路停发后备路接管」向 `/live/test` 和 `/live/backup` 分别推流，拉流地址保持 `/live/test`。保留主路连接但暂停发送音视频后，备路从下一个关键帧接管。状态展示主路末包、停发判定、备路首关键帧和转发空窗。设置「主路稳定后回切」后，主路每次再停发都会重置稳定窗口，持续恢复并出现关键帧才回切。状态保存最近 16 次切换的判定、首关键帧和媒体空窗。

RTSP TCP 与 UDP 的采样和导入报告提供「使用本机 RTSP 回放源」。客户端通过原采样的传输方式读取证据中支持的音视频 RTP，保留包内容、相对到达时间和接收空窗。UDP 单播重新协商本机端口，并保留有效 RTCP Sender Report 与 RTP 的相对到达时间。回放源为每条连接建立独立时钟，并生成该次会话的 RTSP 控制响应。可从报告停止本机回放源。

#### 场景回归

在「场景回归」可保存、导入或导出 GB28181 和 HLS 预设。GB28181 运行时启动本机测试平台与设备并自动核对点播结果。HLS 运行时使用本页播放器，保存故障、同一请求重试及恢复呈帧证据。最近 12 次结果保存在当前浏览器中，可导出 JSON，对照同一场景上次和本次的结果。

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
- `GET /simulators`：协议模拟器工作台，默认打开 GB28181
- `GET /simulators/onvif`：ONVIF 模拟器
- `GET /simulators/sip`：通用 SIP 模拟器
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

H.265 预览的浏览器回归使用仓库内预编码 B 帧素材，无需运行时转码。可分别构建 `./cmd/server` 和 `./internal/diagnostic/testdata/h265-fixture`，将二进制路径传入 `STREAMANALYZER_TEST_SERVER`、`STREAMANALYZER_HEVC_FIXTURE`，执行 `node internal/handler/testdata/rtsp_hevc_e2e.cjs`。用默认 GPU 配置的 Chrome 验证 TCP/UDP 实际解码、首屏与呈帧记录、模型/HTML，以及停止源端后的证据包完整复盘；浏览器需提供 HEVC 解码能力。

Opus 预览回归可构建 `./cmd/server` 和 `./internal/diagnostic/testdata/opus-fixture`，设置 `STREAMANALYZER_TEST_SERVER`、`STREAMANALYZER_OPUS_FIXTURE` 后执行 `node internal/handler/testdata/rtsp_opus_e2e.cjs`。真实 Chrome 验证 TCP/UDP 的音频解码字节、时间推进、一次 PLAY、模型与 HTML，以及停止源端后的报告和原始 dump 一致性。变长包覆盖 2.5/5/10/20/40/60/120 ms；测试先观测保留的 DTX 空洞，再显式跳转越过空洞并记录 seeking 事件，生产流程不会自动跳过。`node internal/handler/testdata/opus_duration.cjs` 单独检查实际 MP4 时长和时间戳。

RTSP 卡顿媒体补查的真实浏览器回归：构建 `./cmd/server` 与 `./internal/diagnostic/testdata/rtsp-stall-fixture`，设置 `STREAMANALYZER_TEST_SERVER` 和 `STREAMANALYZER_RTSP_STALL_FIXTURE` 后执行 `node internal/handler/testdata/rtsp_stall_e2e.cjs`。测试使用同一 RTSP 会话发送 H.264/AAC，先播放三秒、暂停 RTP 四秒、再恢复连续媒体时间，分别验证 TCP/UDP 实际卡顿、停止源站后的 MP4/JPEG 生成、模型数字摘要、证据包导入后重新补查及四份产物逐字节一致。需要 Chrome、FFmpeg 与 FFprobe。
