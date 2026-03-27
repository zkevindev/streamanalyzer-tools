package offline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"streamanalyzer/internal/models"
	"streamanalyzer/internal/pcaprtmp"
	"streamanalyzer/internal/rtmpraw"

	"github.com/asticode/go-astits"
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

	frameDetails := make([]models.OfflineFrameDetail, 0, 2048)
	res, parseErr := rtmpraw.ParseRTMPRaw(in, rtmpraw.Options{SkipBytes: task.SkipBytes}, rtmpraw.Output{
		VideoWriter: vf,
		AudioWriter: af,
		OnFrame: func(fi rtmpraw.FrameInfo) {
			frameDetails = append(frameDetails, models.OfflineFrameDetail{
				MediaType: fi.MediaType,
				DTS:       fi.DTS,
				PTS:       fi.PTS,
				FrameLen:  fi.FrameLen,
				FrameType: fi.FrameType,
			})
		},
	})
	_ = vf.Close()
	_ = af.Close()

	flow := models.OfflineFlowResult{
		FlowID:       1,
		Direction:    "raw",
		DumpRawDir:   "single",
		RawPath:      task.InputPath,
		VideoPath:    videoPath,
		AudioPath:    audioPath,
		VideoCodec:   res.VideoCodec,
		FrameDetails: frameDetails,
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
			FlowID:       flowID,
			HasSYN:       f.Meta.HasSYN,
			SYNCount:     f.Meta.SYNCount,
			TCPPktCount:  f.Meta.TCPPktCount,
			PayloadBytes: f.Meta.PayloadBytes,
			ClientIP:     f.Meta.ClientIP,
			ClientPort:   uint16(f.Meta.ClientPort),
			ServerIP:     f.Meta.ServerIP,
			ServerPort:   uint16(f.Meta.ServerPort),
			Direction:    string(dir),
			DumpRawDir:   rawDir,
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

func (m *Manager) runTS(task *models.OfflineTask, taskDir string, summary *models.OfflineSummary) error {
	outDir := filepath.Join(taskDir, "ts")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	in, err := os.Open(task.InputPath)
	if err != nil {
		return err
	}
	defer in.Close()

	ctx := context.Background()
	dmx := astits.NewDemuxer(ctx, in)

	type streamOut struct {
		programNumber uint16
		pid           uint16
		streamType    astits.StreamType
		category      string
		artifactRel   string
		file          *os.File
		size          int64
		pesCount      int
		naluCount     int
	}

	streams := make(map[uint16]*streamOut)
	programToPMTPID := map[uint16]uint16{}
	defer func() {
		for _, s := range streams {
			if s != nil && s.file != nil {
				_ = s.file.Close()
			}
		}
	}()

	var pesTotal int
	var naluTotal int
	var patTotal int
	var pmtTotal int

	const (
		maxPESDetailRows   = 8000
		maxNALUBriefPerPES = 48
	)
	pesDetails := make([]models.OfflinePESDetail, 0, 1024)
	var pesSeq int

	for {
		d, err := dmx.NextData()
		if errors.Is(err, io.EOF) || errors.Is(err, astits.ErrNoMorePackets) {
			break
		}
		if err != nil {
			return err
		}
		if d == nil {
			continue
		}
		if d.PAT != nil {
			patTotal++
			for _, p := range d.PAT.Programs {
				programToPMTPID[p.ProgramNumber] = p.ProgramMapID
			}
		}
		if d.PMT != nil {
			pmtTotal++
			for _, es := range d.PMT.ElementaryStreams {
				if streams[es.ElementaryPID] != nil {
					continue
				}
				st := &streamOut{
					programNumber: d.PMT.ProgramNumber,
					pid:           es.ElementaryPID,
					streamType:    es.StreamType,
					category:      streamCategory(es.StreamType),
				}
				name := fmt.Sprintf("pid_%d_%s.es", st.pid, safeName(es.StreamType.String()))
				st.artifactRel = filepath.ToSlash(filepath.Join("ts", name))
				outPath := filepath.Join(taskDir, st.artifactRel)
				f, createErr := os.Create(outPath)
				if createErr != nil {
					return createErr
				}
				st.file = f
				streams[es.ElementaryPID] = st
			}
		}
		if d.PES == nil {
			continue
		}

		pesTotal++
		pesSeq++
		st := streams[d.PID]
		if st == nil {
			st = &streamOut{
				programNumber: 0,
				pid:           d.PID,
				streamType:    0,
				category:      "unknown",
				artifactRel:   filepath.ToSlash(filepath.Join("ts", fmt.Sprintf("pid_%d_unknown.es", d.PID))),
			}
			outPath := filepath.Join(taskDir, st.artifactRel)
			f, createErr := os.Create(outPath)
			if createErr != nil {
				return createErr
			}
			st.file = f
			streams[d.PID] = st
		}

		n, writeErr := st.file.Write(d.PES.Data)
		if writeErr != nil {
			return writeErr
		}
		st.size += int64(n)
		st.pesCount++
		payloadLen := len(d.PES.Data)
		var naluThisPES int
		var naluBriefs []models.OfflineNALUBrief
		if st.streamType == astits.StreamTypeH264Video || st.streamType == astits.StreamTypeH265Video || st.category == "video" {
			nalus := splitAnnexBNALUs(d.PES.Data)
			naluThisPES = len(nalus)
			st.naluCount += naluThisPES
			naluTotal += naluThisPES
			idx := 0
			for _, nalu := range nalus {
				if len(nalu) == 0 {
					continue
				}
				idx++
				if len(naluBriefs) >= maxNALUBriefPerPES {
					break
				}
				if st.streamType == astits.StreamTypeH265Video {
					typ := h265NALUType(nalu)
					naluBriefs = append(naluBriefs, models.OfflineNALUBrief{
						Index:    idx,
						Codec:    "H265",
						Type:     int(typ),
						TypeName: fmt.Sprintf("nalu_t%d", typ),
						Len:      len(nalu),
						Key:      isH265KeyNALUType(typ),
					})
				} else {
					typ := h264NALUType(nalu)
					naluBriefs = append(naluBriefs, models.OfflineNALUBrief{
						Index:    idx,
						Codec:    "H264",
						Type:     int(typ),
						TypeName: h264TypeName(typ),
						Len:      len(nalu),
						Key:      typ == 5,
					})
				}
			}
		}

		if len(pesDetails) < maxPESDetailRows {
			detail := models.OfflinePESDetail{
				Seq:           pesSeq,
				PID:           st.pid,
				ProgramNumber: st.programNumber,
				StreamType:    st.streamType.String(),
				Category:      st.category,
				PayloadLen:    payloadLen,
				NALUCount:     naluThisPES,
				NALUs:         naluBriefs,
			}
			if d.PES.Header != nil {
				detail.StreamID = d.PES.Header.StreamID
				if oh := d.PES.Header.OptionalHeader; oh != nil {
					if oh.PTS != nil {
						detail.PTSBase = oh.PTS.Base
						detail.PTSValid = true
					}
					if oh.DTS != nil {
						detail.DTSBase = oh.DTS.Base
						detail.DTSValid = true
					}
				}
			}
			pesDetails = append(pesDetails, detail)
		}
	}

	flow := models.OfflineFlowResult{
		FlowID:       1,
		Direction:    "ts",
		DumpRawDir:   "single",
		RawPath:      task.InputPath,
		PayloadBytes: 0,
		ProgramCount: len(programToPMTPID),
		PATCount:     patTotal,
		PMTCount:     pmtTotal,
		PESCount:     pesTotal,
		NALUCount:    naluTotal,
	}

	videoPIDCount := 0
	audioPIDCount := 0
	pids := make([]int, 0, len(streams))
	for pid := range streams {
		pids = append(pids, int(pid))
	}
	sort.Ints(pids)
	for _, pid := range pids {
		s := streams[uint16(pid)]
		flow.PayloadBytes += int(s.size)
		if flow.VideoPath == "" && s.category == "video" {
			flow.VideoPath = filepath.Join(taskDir, s.artifactRel)
			flow.VideoCodec = s.streamType.String()
		}
		if flow.AudioPath == "" && s.category == "audio" {
			flow.AudioPath = filepath.Join(taskDir, s.artifactRel)
		}
		if s.category == "video" {
			videoPIDCount++
		}
		if s.category == "audio" {
			audioPIDCount++
		}
	}
	flow.VideoPIDCount = videoPIDCount
	flow.AudioPIDCount = audioPIDCount

	flow.PIDDetails = make([]models.OfflinePIDDetail, 0, len(pids))
	for _, pid := range pids {
		s := streams[uint16(pid)]
		flow.PIDDetails = append(flow.PIDDetails, models.OfflinePIDDetail{
			PID:        s.pid,
			StreamType: s.streamType.String(),
			Category:   s.category,
			PESCount:   s.pesCount,
			NALUCount:  s.naluCount,
			Bytes:      s.size,
			OutputPath: filepath.Join(taskDir, s.artifactRel),
		})
	}

	if len(programToPMTPID) == 0 && pesTotal == 0 {
		flow.Error = "no PAT/PMT/PES parsed from TS"
	}
	if len(programToPMTPID) > 0 || pesTotal > 0 {
		flow.Direction = "ts"
	}

	flow.PESDetailTotal = pesTotal
	flow.PESDetails = pesDetails
	flow.PESDetailsTruncated = pesTotal > len(pesDetails)

	summary.Flows = append(summary.Flows, flow)
	return nil
}

func h264NALUType(nalu []byte) uint8 {
	if len(nalu) == 0 {
		return 0
	}
	return nalu[0] & 0x1F
}

func h265NALUType(nalu []byte) uint8 {
	if len(nalu) < 2 {
		return 0
	}
	return (nalu[0] >> 1) & 0x3F
}

func isH265KeyNALUType(t uint8) bool {
	return (t >= 16 && t <= 21) || t == 19 || t == 20
}

func h264TypeName(t uint8) string {
	switch t {
	case 1:
		return "non-IDR"
	case 5:
		return "IDR"
	case 6:
		return "SEI"
	case 7:
		return "SPS"
	case 8:
		return "PPS"
	case 9:
		return "AUD"
	default:
		return "other"
	}
}

func streamCategory(t astits.StreamType) string {
	switch t {
	case astits.StreamTypeH264Video, astits.StreamTypeH265Video, astits.StreamTypeMPEG1Video, astits.StreamTypeMPEG2Video:
		return "video"
	case astits.StreamTypeAACAudio, astits.StreamTypeMPEG1Audio, astits.StreamTypeMPEG2Audio:
		return "audio"
	default:
		return "other"
	}
}

func splitAnnexBNALUs(b []byte) [][]byte {
	var out [][]byte
	starts := findStartCodes(b)
	if len(starts) == 0 {
		return out
	}
	for i := 0; i < len(starts); i++ {
		start := starts[i]
		next := len(b)
		if i+1 < len(starts) {
			next = starts[i+1]
		}
		scLen := startCodeLenAt(b, start)
		naluStart := start + scLen
		if naluStart >= next {
			continue
		}
		out = append(out, b[naluStart:next])
	}
	return out
}

func findStartCodes(b []byte) []int {
	var idx []int
	for i := 0; i+3 < len(b); i++ {
		if b[i] != 0x00 || b[i+1] != 0x00 {
			continue
		}
		if b[i+2] == 0x01 {
			idx = append(idx, i)
		} else if i+3 < len(b) && b[i+2] == 0x00 && b[i+3] == 0x01 {
			idx = append(idx, i)
		}
	}
	return dedupStartIndex(idx)
}

func dedupStartIndex(in []int) []int {
	if len(in) <= 1 {
		return in
	}
	out := make([]int, 0, len(in))
	out = append(out, in[0])
	for i := 1; i < len(in); i++ {
		if in[i]-in[i-1] <= 1 {
			continue
		}
		out = append(out, in[i])
	}
	return out
}

func startCodeLenAt(b []byte, i int) int {
	if i+3 < len(b) && b[i] == 0x00 && b[i+1] == 0x00 && b[i+2] == 0x00 && b[i+3] == 0x01 {
		return 4
	}
	return 3
}

func safeName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "unknown"
	}
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}
