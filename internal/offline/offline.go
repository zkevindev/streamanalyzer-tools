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
	"streamanalyzer/internal/pcaprtmp"
	"streamanalyzer/internal/rtmpraw"

	"github.com/google/gopacket/layers"
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
		ID:        taskID,
		Mode:      req.Mode,
		Status:    models.OfflineStatusPending,
		ServerPort: req.ServerPort,
		SkipBytes: req.SkipBytes,
		CreatedAt: time.Now(),
		InputName: uploadedFilename,
		InputPath: inputPath,
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

func (m *Manager) runRaw(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	outDir := filepath.Join(taskDir, "raw")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	videoPath := filepath.Join(outDir, "video.annexb")
	audioPath := filepath.Join(outDir, "audio.adts.aac")

	in, err := os.Open(task.InputPath)
	if err != nil {
		return err
	}
	defer in.Close()

	vf, err := os.Create(videoPath)
	if err != nil {
		return err
	}
	af, err := os.Create(audioPath)
	if err != nil {
		_ = vf.Close()
		return err
	}

	res, parseErr := rtmpraw.ParseRTMPRaw(in, rtmpraw.Options{SkipBytes: task.SkipBytes}, rtmpraw.Output{
		VideoWriter: vf,
		AudioWriter: af,
	})
	_ = vf.Close()
	_ = af.Close()

	flow := models.OfflineFlowResult{
		FlowID:     1,
		Direction:  "raw",
		DumpRawDir: "single",
		RawPath:    task.InputPath,
		VideoPath:  videoPath,
		AudioPath:  audioPath,
		VideoCodec: res.VideoCodec,
	}
	if parseErr != nil {
		flow.Error = parseErr.Error()
	}
	summary.Flows = append(summary.Flows, flow)

	// Cleanup: if audio file is empty, delete and hide it, and compute payload size
	// from produced artifacts (video/audio) instead of TCP payload bytes.
	cleanupEmptyFile(&summary.Flows[len(summary.Flows)-1].AudioPath)
	i := len(summary.Flows) - 1
	var videoSize int64
	if st, err := os.Stat(summary.Flows[i].VideoPath); err == nil {
		videoSize = st.Size()
	}
	var audioSize int64
	if summary.Flows[i].AudioPath != "" {
		if st, err := os.Stat(summary.Flows[i].AudioPath); err == nil {
			audioSize = st.Size()
		}
	}
	summary.Flows[i].PayloadBytes = int(videoSize + audioSize)
	return parseErr
}

func (m *Manager) runPCAP(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	opt := pcaprtmp.DefaultOptions(layers.TCPPort(task.ServerPort))
	flows, err := pcaprtmp.ExtractFlowsFromPCAP(task.InputPath, opt)
	if err != nil {
		return err
	}
	if len(flows) == 0 {
		return fmt.Errorf("no candidate RTMP flows found")
	}

	for i, f := range flows {
		flowID := i + 1
		dir := pcaprtmp.DetectDirection(f, task.SkipBytes, opt.DetectLimitBytes)
		raw := []byte(nil)
		rawDir := ""
		switch dir {
		case pcaprtmp.DirectionPull:
			raw = f.ServerToClient
			rawDir = "server->client"
		case pcaprtmp.DirectionPush:
			raw = f.ClientToServer
			rawDir = "client->server"
		default:
			// keep summary entry even if unknown
		}

		fr := models.OfflineFlowResult{
			FlowID:      flowID,
			HasSYN:      f.Meta.HasSYN,
			SYNCount:    f.Meta.SYNCount,
			TCPPktCount: f.Meta.TCPPktCount,
			PayloadBytes: f.Meta.PayloadBytes,
			ClientIP:    f.Meta.ClientIP,
			ClientPort:  uint16(f.Meta.ClientPort),
			ServerIP:    f.Meta.ServerIP,
			ServerPort:  uint16(f.Meta.ServerPort),
			Direction:   string(dir),
			DumpRawDir:  rawDir,
		}

		if dir == pcaprtmp.DirectionUnknown || len(raw) == 0 {
			fr.Error = "no media direction detected or empty raw stream"
			summary.Flows = append(summary.Flows, fr)
			continue
		}

		dirOut := filepath.Join(taskDir, string(dir))
		if err := os.MkdirAll(dirOut, 0o755); err != nil {
			return err
		}

		fr.RawPath = filepath.Join(dirOut, fmt.Sprintf("flow%03d_%s.raw", flowID, rawDir))
		if err := os.WriteFile(fr.RawPath, raw, 0o644); err != nil {
			return err
		}

		fr.VideoPath = filepath.Join(dirOut, fmt.Sprintf("flow%03d_video.annexb", flowID))
		fr.AudioPath = filepath.Join(dirOut, fmt.Sprintf("flow%03d_audio.adts.aac", flowID))

		vf, err := os.Create(fr.VideoPath)
		if err != nil {
			return err
		}
		af, err := os.Create(fr.AudioPath)
		if err != nil {
			_ = vf.Close()
			return err
		}
		res, parseErr := rtmpraw.ParseRTMPRaw(bytes.NewReader(raw), rtmpraw.Options{SkipBytes: task.SkipBytes}, rtmpraw.Output{
			VideoWriter: vf,
			AudioWriter: af,
		})
		_ = vf.Close()
		_ = af.Close()
		if parseErr != nil {
			fr.Error = parseErr.Error()
		} else {
			fr.VideoCodec = res.VideoCodec
		}

		// Cleanup: if audio file is empty, delete and hide it.
		cleanupEmptyFile(&fr.AudioPath)
		summary.Flows = append(summary.Flows, fr)
	}
	return nil
}

func cleanupEmptyFile(path *string) {
	if path == nil || *path == "" {
		return
	}
	st, err := os.Stat(*path)
	if err != nil {
		return
	}
	if st.Size() == 0 {
		_ = os.Remove(*path)
		*path = ""
	}
}

