package offline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"streamanalyzer/internal/models"
	"streamanalyzer/internal/rtmpraw"
)

type runningEntry struct {
	ctx        context.Context
	cancel     context.CancelFunc
	lastUpdate time.Time
}

type Manager struct {
	baseDir string
	mu      sync.RWMutex
	tasks   map[string]*models.OfflineTask
	running map[string]*runningEntry
}

func NewManager(baseDir string) *Manager {
	return &Manager{
		baseDir: baseDir,
		tasks:   make(map[string]*models.OfflineTask),
		running: make(map[string]*runningEntry),
	}
}

func (m *Manager) BaseDir() string { return m.baseDir }

// LoadExisting loads persisted task.json under baseDir/offline/* into memory.
// Tasks left in pending/running from an interrupted process are marked failed
// so they do not stay stuck forever and can be reparsed.
func (m *Manager) LoadExisting() {
	root := filepath.Join(m.baseDir, "offline")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		taskID := e.Name()
		b, err := os.ReadFile(filepath.Join(root, taskID, "task.json"))
		if err != nil {
			continue
		}
		var t models.OfflineTask
		if err := json.Unmarshal(b, &t); err != nil {
			continue
		}
		if t.ID == "" {
			t.ID = taskID
		}
		changed := false
		if t.InputSize == 0 && t.InputPath != "" {
			if st, err := os.Stat(t.InputPath); err == nil {
				t.InputSize = st.Size()
				changed = true
			}
		}
		if t.Status == models.OfflineStatusRunning || t.Status == models.OfflineStatusPending {
			t.Status = models.OfflineStatusFailed
			if t.Error == "" {
				t.Error = "服务重启导致解析中断，可点击重新解析"
			}
			t.FinishedAt = time.Now()
			changed = true
		}
		if changed {
			_ = m.writeTaskFile(&t)
		}
		m.mu.Lock()
		m.tasks[t.ID] = &t
		m.mu.Unlock()
	}
}

func (m *Manager) CreateTask(req *models.OfflineTaskRequest, uploadedFilename string) (*models.OfflineTask, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskID := fmt.Sprintf("offline_%d", time.Now().UnixNano())
	taskDir := filepath.Join(m.baseDir, "offline", taskID)
	inputDir := filepath.Join(taskDir, "input")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return nil, "", err
	}

	inputName := "upload"
	if ext := filepath.Ext(uploadedFilename); ext != "" {
		inputName += ext
	}
	inputPath := filepath.Join(inputDir, inputName)

	task := &models.OfflineTask{
		ID:         taskID,
		Mode:       req.Mode,
		Status:     models.OfflineStatusPending,
		ServerPort: req.ServerPort,
		SkipBytes:  req.SkipBytes,
		SourceType: "upload",
		CreatedAt:  time.Now(),
		InputName:  uploadedFilename,
		InputPath:  inputPath,
	}
	m.tasks[taskID] = task

	// Persist task.json early so refresh after restart can show it.
	_ = m.writeTaskFile(task)

	cp := *task
	return &cp, inputPath, nil
}

// CreateLocalTask creates a zero-copy offline analysis task pointing directly at a
// local file on disk without copying it into data/offline/<taskID>/input.
// DeleteTask only removes data/offline/<taskID> and never deletes the user's local file.
func (m *Manager) CreateLocalTask(req *models.OfflineTaskRequest, localPath string) (*models.OfflineTask, error) {
	cleaned := strings.TrimSpace(localPath)
	if cleaned == "" {
		return nil, fmt.Errorf("本地文件路径不能为空")
	}
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return nil, fmt.Errorf("无法解析本地路径: %w", err)
	}
	st, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("本地文件不存在: %s", absPath)
		}
		return nil, fmt.Errorf("无法访问本地文件: %w", err)
	}
	if st.IsDir() {
		return nil, fmt.Errorf("指定路径是目录而非文件: %s", absPath)
	}
	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("本地文件不可读: %w", err)
	}
	_ = f.Close()

	m.mu.Lock()
	defer m.mu.Unlock()

	taskID := fmt.Sprintf("offline_%d", time.Now().UnixNano())
	taskDir := filepath.Join(m.baseDir, "offline", taskID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return nil, err
	}

	task := &models.OfflineTask{
		ID:         taskID,
		Mode:       req.Mode,
		Status:     models.OfflineStatusPending,
		ServerPort: req.ServerPort,
		SkipBytes:  req.SkipBytes,
		SourceType: "local",
		CreatedAt:  time.Now(),
		InputName:  filepath.Base(absPath),
		InputPath:  absPath,
		InputSize:  st.Size(),
	}
	m.tasks[taskID] = task
	_ = m.writeTaskFile(task)

	cp := *task
	return &cp, nil
}

// PrepareTask inspects the uploaded/local input file to record its size and resolve
// auto-detected mode before launching parsing.
func (m *Manager) PrepareTask(taskID string, skipExplicit bool) (*models.OfflineTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task not found")
	}
	if st, err := os.Stat(task.InputPath); err == nil {
		task.InputSize = st.Size()
	}
	if task.Mode == "" || task.Mode == models.OfflineModeAuto {
		detected, reason := DetectMode(task.InputPath, task.InputName)
		if detected == "" {
			return nil, fmt.Errorf("无法自动识别文件格式，请手动选择模式（支持 pcap / raw / flv / ts / mp4 / annexb）")
		}
		task.Mode = detected
		task.DetectedBy = reason
		if task.Mode == models.OfflineModePCAP && task.ServerPort == 0 {
			task.ServerPort = 1935
		}
		if (task.Mode == models.OfflineModeRaw || task.Mode == models.OfflineModePCAP) && !skipExplicit {
			task.SkipBytes = rtmpraw.DefaultHandshakeSkipBytes
		}
	}
	_ = m.writeTaskFile(task)
	cp := *task
	return &cp, nil
}

func (m *Manager) StartTask(taskID string) error {
	m.mu.Lock()
	task, ok := m.tasks[taskID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("task not found")
	}
	if task.Status == models.OfflineStatusRunning {
		m.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.running[taskID] = &runningEntry{ctx: ctx, cancel: cancel}

	task.Status = models.OfflineStatusRunning
	task.Error = ""
	task.ProgressPct = 0
	task.ProgressStage = "准备解析"
	task.BytesProcessed = 0
	task.StartedAt = time.Now()
	task.FinishedAt = time.Time{}
	_ = m.writeTaskFile(task)
	m.mu.Unlock()

	go m.run(ctx, taskID)
	return nil
}

// CancelTask signals a running offline task to abort parsing promptly.
func (m *Manager) CancelTask(taskID string) error {
	if !isSafeTaskID(taskID) {
		return fmt.Errorf("invalid task id")
	}
	m.mu.Lock()
	task, ok := m.tasks[taskID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("task not found")
	}
	if task.Status != models.OfflineStatusRunning {
		m.mu.Unlock()
		return fmt.Errorf("任务当前未在解析中（状态：%s）", task.Status)
	}
	entry := m.running[taskID]
	task.ProgressStage = "正在取消…"
	m.mu.Unlock()

	if entry != nil && entry.cancel != nil {
		entry.cancel()
	}
	return nil
}

// DeleteTask removes a finished/failed/canceled task from memory and deletes its
// managed workspace directory data/offline/<taskID>. For local zero-copy tasks,
// the user's original input file outside taskDir is never touched.
func (m *Manager) DeleteTask(taskID string) error {
	if !isSafeTaskID(taskID) {
		return fmt.Errorf("invalid task id")
	}
	m.mu.Lock()
	task, ok := m.tasks[taskID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("task not found")
	}
	if task.Status == models.OfflineStatusRunning {
		m.mu.Unlock()
		return fmt.Errorf("任务正在解析中，无法删除")
	}
	delete(m.tasks, taskID)
	delete(m.running, taskID)
	m.mu.Unlock()

	taskDir := filepath.Join(m.baseDir, "offline", taskID)
	return os.RemoveAll(taskDir)
}

// ReparseTask cleans previous derived artifacts (keeping input/) and re-runs parsing.
func (m *Manager) ReparseTask(taskID string) (*models.OfflineTask, error) {
	if !isSafeTaskID(taskID) {
		return nil, fmt.Errorf("invalid task id")
	}
	m.mu.Lock()
	task, ok := m.tasks[taskID]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("task not found")
	}
	if task.Status == models.OfflineStatusRunning {
		m.mu.Unlock()
		return nil, fmt.Errorf("任务正在解析中")
	}
	if task.InputPath == "" {
		m.mu.Unlock()
		return nil, fmt.Errorf("输入文件路径缺失")
	}
	if st, err := os.Stat(task.InputPath); err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("输入文件不存在: %w", err)
	} else {
		task.InputSize = st.Size()
	}

	taskDir := filepath.Join(m.baseDir, "offline", taskID)
	if entries, err := os.ReadDir(taskDir); err == nil {
		for _, e := range entries {
			name := e.Name()
			if name == "input" || name == "task.json" {
				continue
			}
			_ = os.RemoveAll(filepath.Join(taskDir, name))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.running[taskID] = &runningEntry{ctx: ctx, cancel: cancel}

	task.Status = models.OfflineStatusRunning
	task.Error = ""
	task.SummaryPath = ""
	task.ProgressPct = 0
	task.ProgressStage = "准备重新解析"
	task.BytesProcessed = 0
	task.StartedAt = time.Now()
	task.FinishedAt = time.Time{}
	_ = m.writeTaskFile(task)
	cp := *task
	m.mu.Unlock()

	go m.run(ctx, taskID)
	return &cp, nil
}

func (m *Manager) GetTask(taskID string) (*models.OfflineTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task not found")
	}
	cp := *t
	return &cp, nil
}

func (m *Manager) ListTasks() []*models.OfflineTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.OfflineTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		cp := *t
		out = append(out, &cp)
	}
	return out
}

func (m *Manager) taskContext(taskID string) context.Context {
	if taskID == "" {
		return context.Background()
	}
	m.mu.RLock()
	entry := m.running[taskID]
	m.mu.RUnlock()
	if entry != nil && entry.ctx != nil {
		return entry.ctx
	}
	return context.Background()
}

// updateProgress updates in-memory task progress with throttling to avoid disk churn.
func (m *Manager) updateProgress(taskID, stage string, processed, total int64) {
	if taskID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[taskID]
	if task == nil || task.Status != models.OfflineStatusRunning {
		return
	}
	now := time.Now()
	entry := m.running[taskID]
	stageChanged := stage != "" && stage != task.ProgressStage
	if !stageChanged && entry != nil && !entry.lastUpdate.IsZero() && now.Sub(entry.lastUpdate) < 100*time.Millisecond {
		return
	}
	if entry != nil {
		entry.lastUpdate = now
	}
	if stage != "" {
		task.ProgressStage = stage
	}
	if processed >= 0 {
		task.BytesProcessed = processed
	}
	if total > 0 && processed >= 0 {
		pct := float64(processed) * 100.0 / float64(total)
		if pct > 99.5 {
			pct = 99.5
		}
		if pct < 0 {
			pct = 0
		}
		task.ProgressPct = math.Round(pct*10) / 10
	}
}

func (m *Manager) setProgressPercent(taskID, stage string, pct float64, processed int64) {
	if taskID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[taskID]
	if task == nil || task.Status != models.OfflineStatusRunning {
		return
	}
	if stage != "" {
		task.ProgressStage = stage
	}
	if processed >= 0 {
		task.BytesProcessed = processed
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 99.5 {
		pct = 99.5
	}
	task.ProgressPct = math.Round(pct*10) / 10
}

func (m *Manager) writeTaskFile(task *models.OfflineTask) error {
	taskDir := filepath.Join(m.baseDir, "offline", task.ID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(taskDir, "task.json"), b, 0o644)
}

func (m *Manager) writeSummary(taskID string, summary *models.OfflineSummary) error {
	taskDir := filepath.Join(m.baseDir, "offline", taskID)
	b, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(taskDir, "summary.json"), b, 0o644); err != nil {
		return err
	}

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("=== Offline Summary (%s) ===\n", summary.Mode))
	buf.WriteString(fmt.Sprintf("task=%s flows=%d\n", taskID, len(summary.Flows)))
	for _, f := range summary.Flows {
		buf.WriteString(fmt.Sprintf(
			"flow%03d SYN=%v pkts=%d payloadBytes=%d client=%s:%d server=%s:%d dir=%s dump=%s raw=%s video=%s audio=%s err=%s\n",
			f.FlowID,
			f.HasSYN,
			f.TCPPktCount,
			f.PayloadBytes,
			f.ClientIP, f.ClientPort,
			f.ServerIP, f.ServerPort,
			f.Direction,
			f.DumpRawDir,
			f.RawPath,
			emptyOrDash(f.VideoCodec),
			emptyOrDash(f.AudioCodec),
			emptyOrDash(f.Error),
		))
		if p := f.Profile; p != nil {
			buf.WriteString(fmt.Sprintf(
				"  profile %dx%d %s@%s duration=%dms v=%d a=%d key=%d bframes=%d fps=%.2f vkbps=%.1f akbps=%.1f peak200ms=%.1f(%.2fx) gop=%d/%d/%d delta=%d/%d/%d gaps=%d regressions=%d avOffset=%dms tsCC=%d warnings=%d\n",
				p.Width, p.Height, emptyOrDash(p.Profile), emptyOrDash(p.LevelName),
				p.DurationMS, p.VideoFrames, p.AudioFrames, p.Keyframes, p.BFrames, p.FrameRate,
				p.VideoBitrateKbps, p.AudioBitrateKbps, p.Peak200msKbps, p.PeakToAvgRatio,
				p.GOPMinMS, p.GOPAvgMS, p.GOPMaxMS,
				p.VideoDeltaMinMS, p.VideoDeltaAvgMS, p.VideoDeltaMaxMS,
				p.LargeGaps, p.DTSRegressions, p.AVStartOffsetMS, p.TSCCErrors, len(p.Warnings),
			))
			for _, w := range p.Warnings {
				buf.WriteString("  warning: " + w + "\n")
			}
		}
	}
	return os.WriteFile(filepath.Join(taskDir, "summary.txt"), buf.Bytes(), 0o644)
}

func emptyOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func isSafeTaskID(id string) bool {
	if id == "" || len(id) > 128 || strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return false
	}
	return true
}

func (m *Manager) run(ctx context.Context, taskID string) {
	defer func() {
		m.mu.Lock()
		if entry := m.running[taskID]; entry != nil && entry.cancel != nil {
			entry.cancel()
		}
		delete(m.running, taskID)
		m.mu.Unlock()
	}()

	m.mu.RLock()
	orig, ok := m.tasks[taskID]
	if !ok {
		m.mu.RUnlock()
		return
	}
	task := *orig
	m.mu.RUnlock()

	summary := &models.OfflineSummary{
		TaskID: taskID,
		Mode:   task.Mode,
	}

	taskDir := filepath.Join(m.baseDir, "offline", taskID)

	var runErr error
	switch task.Mode {
	case models.OfflineModeRaw:
		runErr = m.runRaw(&task, taskDir, summary)
	case models.OfflineModePCAP:
		runErr = m.runPCAP(&task, taskDir, summary)
	case models.OfflineModeTS:
		runErr = m.runTS(&task, taskDir, summary)
	case models.OfflineModeFLV:
		runErr = m.runFLV(&task, taskDir, summary)
	case models.OfflineModeMP4:
		runErr = m.runMP4(&task, taskDir, summary)
	case models.OfflineModeAnnexB:
		runErr = m.runAnnexB(&task, taskDir, summary)
	case models.OfflineModePS:
		runErr = m.runPS(&task, taskDir, summary)
	case models.OfflineModeWebM:
		runErr = m.runWebM(&task, taskDir, summary)
	default:
		runErr = fmt.Errorf("unsupported mode: %s", task.Mode)
	}

	canceled := errors.Is(runErr, context.Canceled) || ctx.Err() != nil
	if !canceled {
		if err := m.writeSummary(taskID, summary); err != nil && runErr == nil {
			runErr = err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.tasks[taskID]
	if cur == nil {
		return
	}
	if canceled {
		cur.Status = models.OfflineStatusCanceled
		cur.Error = "解析已取消"
		cur.ProgressStage = "已取消"
	} else if runErr != nil {
		cur.Status = models.OfflineStatusFailed
		cur.Error = runErr.Error()
		cur.ProgressStage = "解析失败"
	} else {
		cur.Status = models.OfflineStatusDone
		cur.ProgressPct = 100
		cur.ProgressStage = "解析完成"
		if cur.InputSize > 0 {
			cur.BytesProcessed = cur.InputSize
		}
		cur.SummaryPath = filepath.Join(taskDir, "summary.json")
	}
	cur.FinishedAt = time.Now()
	_ = m.writeTaskFile(cur)
}
