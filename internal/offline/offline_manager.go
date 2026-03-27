package offline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"streamanalyzer/internal/models"
)

type Manager struct {
	baseDir string
	mu      sync.RWMutex
	tasks   map[string]*models.OfflineTask
}

func NewManager(baseDir string) *Manager {
	return &Manager{
		baseDir: baseDir,
		tasks:   make(map[string]*models.OfflineTask),
	}
}

func (m *Manager) BaseDir() string { return m.baseDir }

// LoadExisting loads persisted task.json under baseDir/offline/* into memory.
// It is best-effort: invalid entries are skipped.
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
		CreatedAt:  time.Now(),
		InputName:  uploadedFilename,
		InputPath:  inputPath,
	}
	m.tasks[taskID] = task

	// Persist task.json early so refresh after restart can show it.
	_ = m.writeTaskFile(task)

	return task, inputPath, nil
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
	task.Status = models.OfflineStatusRunning
	task.StartedAt = time.Now()
	_ = m.writeTaskFile(task)
	m.mu.Unlock()

	go m.run(taskID)
	return nil
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
	buf.WriteString("=== RTMP Offline Summary ===\n")
	buf.WriteString(fmt.Sprintf("task=%s flows=%d\n", taskID, len(summary.Flows)))
	for _, f := range summary.Flows {
		buf.WriteString(fmt.Sprintf(
			"flow%03d SYN=%v pkts=%d payloadBytes=%d client=%s:%d server=%s:%d dir=%s dump=%s raw=%s codec=%s err=%s\n",
			f.FlowID,
			f.HasSYN,
			f.TCPPktCount,
			f.PayloadBytes,
			f.ClientIP, f.ClientPort,
			f.ServerIP, f.ServerPort,
			f.Direction,
			f.DumpRawDir,
			f.RawPath,
			f.VideoCodec,
			emptyOrDash(f.Error),
		))
	}
	return os.WriteFile(filepath.Join(taskDir, "summary.txt"), buf.Bytes(), 0o644)
}

func emptyOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (m *Manager) run(taskID string) {
	m.mu.RLock()
	task, ok := m.tasks[taskID]
	m.mu.RUnlock()
	if !ok {
		return
	}

	summary := &models.OfflineSummary{
		TaskID: taskID,
		Mode:   task.Mode,
	}

	taskDir := filepath.Join(m.baseDir, "offline", taskID)

	var runErr error
	switch task.Mode {
	case models.OfflineModeRaw:
		runErr = m.runRaw(task, taskDir, summary)
	case models.OfflineModePCAP:
		runErr = m.runPCAP(task, taskDir, summary)
	case models.OfflineModeTS:
		runErr = m.runTS(task, taskDir, summary)
	case models.OfflineModeFLV:
		runErr = m.runFLV(task, taskDir, summary)
	default:
		runErr = fmt.Errorf("unsupported mode: %s", task.Mode)
	}

	if err := m.writeSummary(taskID, summary); err != nil && runErr == nil {
		runErr = err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.tasks[taskID]
	if cur == nil {
		return
	}
	if runErr != nil {
		cur.Status = models.OfflineStatusFailed
		cur.Error = runErr.Error()
	} else {
		cur.Status = models.OfflineStatusDone
	}
	cur.FinishedAt = time.Now()
	cur.SummaryPath = filepath.Join(taskDir, "summary.json")
	_ = m.writeTaskFile(cur)
}
