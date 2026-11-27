package pusher

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Manager struct {
	mu     sync.Mutex
	closed bool
	tasks  map[string]*pushTask
}

type pushTask struct {
	id        string
	cfg       Config
	tempFile  string
	startedAt time.Time

	cancel context.CancelFunc
	done   chan struct{}

	// For SourceBrowser WebSocket -> FFmpeg stdin pipe
	pipeReader     *io.PipeReader
	pipeWriter     *io.PipeWriter
	ingestAttached bool

	mu            sync.Mutex
	status        TaskStatus
	stopRequested bool
	iceState      string
	framesSent    int64
	bytesSent     int64
	fps           float64
	bitrateKbps   float64
	outTimeMS     int64
	finishedAt    time.Time
	errMsg        string
	logs          []string
}

func (m *Manager) Start(cfg Config) (TaskSnapshot, error) {
	if err := cfg.normalize(); err != nil {
		return TaskSnapshot{}, err
	}
	if cfg.SourceKind == SourceUpload {
		return TaskSnapshot{}, fmt.Errorf("%w：上传录像请使用上传接口", ErrInvalid)
	}
	if cfg.SourceKind == SourceBrowser && cfg.Protocol == ProtocolWHIP {
		return TaskSnapshot{}, fmt.Errorf("%w：浏览器采集推 WHIP 请通过页面 WebRTC 与 /whip-proxy 直接推流", ErrInvalid)
	}
	return m.startTask(cfg, "")
}

func (m *Manager) StartUpload(cfg Config, filename string, r io.Reader) (TaskSnapshot, error) {
	cfg.SourceKind = SourceUpload
	ext := filepath.Ext(strings.TrimSpace(filename))
	if len(ext) > 12 {
		ext = ""
	}
	baseName := filepath.Base(strings.TrimSpace(filename))
	if baseName == "" || baseName == "." || baseName == "/" {
		baseName = "upload" + ext
	}
	if cfg.SourceName == "" {
		cfg.SourceName = baseName
	}

	tmp, err := os.CreateTemp("", "sa-pusher-*"+ext)
	if err != nil {
		return TaskSnapshot{}, fmt.Errorf("创建临时文件失败：%w", err)
	}
	tmpPath := tmp.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	limited := io.LimitReader(r, MaxUploadBytes+1)
	written, err := io.Copy(tmp, limited)
	closeErr := tmp.Close()
	if err != nil {
		return TaskSnapshot{}, fmt.Errorf("保存上传文件失败：%w", err)
	}
	if closeErr != nil {
		return TaskSnapshot{}, fmt.Errorf("写入临时文件失败：%w", closeErr)
	}
	if written == 0 {
		return TaskSnapshot{}, fmt.Errorf("%w：上传录像文件为空", ErrInvalid)
	}
	if written > MaxUploadBytes {
		return TaskSnapshot{}, ErrUploadTooBig
	}

	cfg.FilePath = tmpPath
	if err := cfg.normalize(); err != nil {
		return TaskSnapshot{}, err
	}

	snap, err := m.startTask(cfg, tmpPath)
	if err != nil {
		return TaskSnapshot{}, err
	}
	cleanupTmp = false
	return snap, nil
}

func (m *Manager) startTask(cfg Config, tempFile string) (TaskSnapshot, error) {
	ffmpegPath, err := findFFmpeg()
	if err != nil {
		return TaskSnapshot{}, err
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return TaskSnapshot{}, ErrClosed
	}
	if m.tasks == nil {
		m.tasks = make(map[string]*pushTask)
	}
	active := 0
	for _, t := range m.tasks {
		if t.isActive() {
			active++
		}
	}
	if active >= MaxActiveTasks {
		m.mu.Unlock()
		return TaskSnapshot{}, ErrLimitReached
	}

	ctx, cancel := context.WithCancel(context.Background())
	task := &pushTask{
		id:        uuid.NewString()[:8],
		cfg:       cfg,
		tempFile:  tempFile,
		startedAt: time.Now(),
		cancel:    cancel,
		done:      make(chan struct{}),
		status:    StatusStarting,
	}
	if cfg.SourceKind == SourceBrowser {
		task.pipeReader, task.pipeWriter = io.Pipe()
		task.appendLog("等待浏览器媒体采集 WebSocket 接入...")
	}
	m.tasks[task.id] = task
	m.mu.Unlock()

	go func() {
		defer close(task.done)
		defer func() {
			if task.pipeReader != nil {
				_ = task.pipeReader.Close()
			}
			if task.pipeWriter != nil {
				_ = task.pipeWriter.Close()
			}
		}()

		switch cfg.Protocol {
		case ProtocolWHIP:
			task.runWHIP(ctx, ffmpegPath)
		default:
			task.runFFmpeg(ctx, ffmpegPath)
		}
	}()

	return task.snapshot(), nil
}

func (m *Manager) AttachIngest(taskID string, conn *websocket.Conn) error {
	m.mu.Lock()
	task := m.tasks[taskID]
	if task == nil {
		m.mu.Unlock()
		return ErrNotFound
	}
	if task.cfg.SourceKind != SourceBrowser || task.pipeWriter == nil {
		m.mu.Unlock()
		return fmt.Errorf("%w：该任务不支持 WebSocket 采集推流", ErrInvalid)
	}
	task.mu.Lock()
	if task.ingestAttached {
		task.mu.Unlock()
		m.mu.Unlock()
		return fmt.Errorf("%w：该任务已有采集连接接入", ErrInvalid)
	}
	if task.status == StatusStopped || task.status == StatusFailed {
		task.mu.Unlock()
		m.mu.Unlock()
		return fmt.Errorf("%w：任务已结束", ErrInvalid)
	}
	task.ingestAttached = true
	task.mu.Unlock()
	m.mu.Unlock()

	task.appendLog("浏览器采集 WebSocket 已连接，开始实时转发媒体流")

	defer func() {
		_ = task.pipeWriter.Close()
		_ = conn.Close()
	}()

	// If task is stopped from UI/API, close WS so ReadMessage unblocks immediately.
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-task.done:
			_ = conn.Close()
		case <-stopWatch:
		}
	}()

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if !task.isStopRequested() {
				task.requestStop()
			}
			return nil
		}
		if msgType == websocket.TextMessage {
			if strings.TrimSpace(string(data)) == "stop" {
				task.requestStop()
				return nil
			}
			continue
		}
		if msgType != websocket.BinaryMessage || len(data) == 0 {
			continue
		}
		if _, err := task.pipeWriter.Write(data); err != nil {
			return nil
		}
	}
}

func (m *Manager) List() []TaskSnapshot {
	m.mu.Lock()
	list := make([]TaskSnapshot, 0, len(m.tasks))
	for _, t := range m.tasks {
		list = append(list, t.snapshot())
	}
	m.mu.Unlock()

	sort.Slice(list, func(i, j int) bool {
		return list[i].StartedAt.After(list[j].StartedAt)
	})
	return list
}

func (m *Manager) Get(id string) (TaskSnapshot, bool) {
	m.mu.Lock()
	t := m.tasks[id]
	m.mu.Unlock()
	if t == nil {
		return TaskSnapshot{}, false
	}
	return t.snapshot(), true
}

func (m *Manager) Stop(id string) (TaskSnapshot, error) {
	m.mu.Lock()
	t := m.tasks[id]
	m.mu.Unlock()
	if t == nil {
		return TaskSnapshot{}, ErrNotFound
	}
	t.requestStop()
	select {
	case <-t.done:
	case <-time.After(4 * time.Second):
	}
	return t.snapshot(), nil
}

func (m *Manager) StopAll() int {
	m.mu.Lock()
	active := make([]*pushTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		if t.isActive() {
			active = append(active, t)
		}
	}
	m.mu.Unlock()

	for _, t := range active {
		t.requestStop()
	}
	deadline := time.After(4 * time.Second)
	for _, t := range active {
		select {
		case <-t.done:
		case <-deadline:
			return len(active)
		}
	}
	return len(active)
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil {
		m.mu.Unlock()
		return ErrNotFound
	}
	if t.isActive() {
		m.mu.Unlock()
		return ErrTaskRunning
	}
	delete(m.tasks, id)
	m.mu.Unlock()

	if t.tempFile != "" {
		_ = os.Remove(t.tempFile)
	}
	return nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	all := make([]*pushTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		all = append(all, t)
	}
	m.tasks = nil
	m.mu.Unlock()

	for _, t := range all {
		t.requestStop()
	}
	deadline := time.After(5 * time.Second)
	for _, t := range all {
		select {
		case <-t.done:
		case <-deadline:
		}
		if t.tempFile != "" {
			_ = os.Remove(t.tempFile)
		}
	}
	return nil
}

func (t *pushTask) isActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status == StatusStarting || t.status == StatusPushing
}

func (t *pushTask) isStopRequested() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stopRequested
}

func (t *pushTask) requestStop() {
	t.mu.Lock()
	t.stopRequested = true
	t.mu.Unlock()
	if t.pipeWriter != nil {
		_ = t.pipeWriter.Close()
	}
	t.cancel()
}

func (t *pushTask) finish(status TaskStatus, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.status == StatusStopped || t.status == StatusFailed {
		return
	}
	t.status = status
	t.errMsg = strings.TrimSpace(errMsg)
	t.finishedAt = time.Now()
}

func (t *pushTask) appendLog(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	entry := time.Now().Format("15:04:05") + " " + line
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logs = append(t.logs, entry)
	if len(t.logs) > maxRecentLogs {
		t.logs = t.logs[len(t.logs)-maxRecentLogs:]
	}
}

func (t *pushTask) lastErrorLog() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.logs) - 1; i >= 0; i-- {
		line := t.logs[i]
		if len(line) > 9 {
			line = line[9:]
		}
		if line != "" {
			return line
		}
	}
	return ""
}

func (t *pushTask) snapshot() TaskSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	elapsed := time.Since(t.startedAt).Milliseconds()
	if !t.finishedAt.IsZero() {
		elapsed = t.finishedAt.Sub(t.startedAt).Milliseconds()
	}
	if elapsed < 0 {
		elapsed = 0
	}
	logsCopy := make([]string, len(t.logs))
	copy(logsCopy, t.logs)

	loop := false
	if t.cfg.Loop != nil {
		loop = *t.cfg.Loop
	}

	return TaskSnapshot{
		ID:            t.id,
		Name:          t.cfg.Name,
		Protocol:      t.cfg.Protocol,
		TargetURL:     redactTargetURL(t.cfg.TargetURL),
		RTSPTransport: t.cfg.RTSPTransport,
		SourceKind:    t.cfg.SourceKind,
		SourceName:    t.cfg.SourceName,
		Loop:          loop,
		LoopCount:     t.cfg.LoopCount,
		VideoMode:     t.cfg.VideoMode,
		AudioMode:     t.cfg.AudioMode,
		Status:        t.status,
		ICEState:      t.iceState,
		FramesSent:    t.framesSent,
		BytesSent:     t.bytesSent,
		FPS:           t.fps,
		BitrateKbps:   t.bitrateKbps,
		OutTimeMS:     t.outTimeMS,
		ElapsedMS:     elapsed,
		StartedAt:     t.startedAt,
		FinishedAt:    t.finishedAt,
		Error:         t.errMsg,
		RecentLogs:    logsCopy,
	}
}
