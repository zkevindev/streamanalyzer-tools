# 端到端回归测试指南

先运行标准 Go 回归：

```bash
go test ./...
```

仓库另有多组基于真实 Chrome 浏览器与本地固定素材（Fixture）的端到端（E2E）回归脚本，位于 `internal/handler/testdata/`。执行前需准备 Node.js、Chrome 浏览器，部分用例需要本机 `ffmpeg` 与 `ffprobe`。

## 1. H.265 RTSP 预览回归

验证预编码 B 帧 HEVC 素材在 TCP/UDP 下的实际解码、首屏与呈帧记录、模型/HTML 报告及离线证据包复盘（需浏览器提供 HEVC 解码能力）：

```bash
go build -o /tmp/sa-server ./cmd/server
go build -o /tmp/sa-hevc-fixture ./internal/diagnostic/testdata/h265-fixture

STREAMANALYZER_TEST_SERVER=/tmp/sa-server \
STREAMANALYZER_HEVC_FIXTURE=/tmp/sa-hevc-fixture \
node internal/handler/testdata/rtsp_hevc_e2e.cjs
```

## 2. Opus RTSP 预览与 DTX 回归

验证变长包（2.5/5/10/20/40/60/120 ms）与保留 DTX 空洞在 TCP/UDP 下的解码推进、单次 PLAY、HTML/模型报告及原始 dump 一致性：

```bash
go build -o /tmp/sa-server ./cmd/server
go build -o /tmp/sa-opus-fixture ./internal/diagnostic/testdata/opus-fixture

STREAMANALYZER_TEST_SERVER=/tmp/sa-server \
STREAMANALYZER_OPUS_FIXTURE=/tmp/sa-opus-fixture \
node internal/handler/testdata/rtsp_opus_e2e.cjs

# 单独检查 MP4 时长与时间戳
node internal/handler/testdata/opus_duration.cjs
```

## 3. RTSP 卡顿媒体补查回归

使用同一 RTSP 会话发送 H.264/AAC（播放 3 秒 → 暂停 RTP 4 秒 → 恢复），验证 TCP/UDP 卡顿捕获、停源后 MP4/JPEG 离线重建及导入复盘逐字节一致（需要 Chrome、FFmpeg、FFprobe）：

```bash
go build -o /tmp/sa-server ./cmd/server
go build -o /tmp/sa-stall-fixture ./internal/diagnostic/testdata/rtsp-stall-fixture

STREAMANALYZER_TEST_SERVER=/tmp/sa-server \
STREAMANALYZER_RTSP_STALL_FIXTURE=/tmp/sa-stall-fixture \
node internal/handler/testdata/rtsp_stall_e2e.cjs
```

## 4. 多模态图文补查与桌面设置回归

复用上述 `sa-server` 与 `sa-stall-fixture`，验证真实 JPEG 与时序发送、整份报告图文汇总、DeepSeek 文本回退、仅文本模式与图片不支持时的单次文本重试：

```bash
STREAMANALYZER_TEST_SERVER=/tmp/sa-server \
STREAMANALYZER_RTSP_STALL_FIXTURE=/tmp/sa-stall-fixture \
node internal/handler/testdata/model_images_e2e.cjs

# 隔离 HTTP 设置服务验证桌面热更新、连接测试与回退记录（不访问系统凭据库）
node internal/handler/testdata/model_images_ui.cjs
```

## 5. 自动补查端到端回归

验证表单默认自动补查联动、停源后自动提取真实 MP4/JPEG、单次整体模型汇总及离线证据包一致性：

```bash
STREAMANALYZER_TEST_SERVER=/tmp/sa-server \
STREAMANALYZER_RTSP_STALL_FIXTURE=/tmp/sa-stall-fixture \
node internal/handler/testdata/auto_investigation_e2e.cjs

# 单独验证自动补查 UI 状态联动、停止/重试与历史快照行为
node internal/handler/testdata/automatic_ui.cjs
```
