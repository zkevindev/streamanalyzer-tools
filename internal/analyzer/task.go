package analyzer

import (
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

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
	"streamanalyzer/internal/storage"

	"github.com/yutopp/go-flv/tag"
	gortmp "github.com/yutopp/go-rtmp"
)

type StreamAnalyzer struct {
	tasks       map[string]*analyzeTask
	taskHistory map[string]*models.Task
	storage     *storage.CSVStorage
	// maxDumpBytes caps the raw dump of a single task; 0 means unlimited.
	maxDumpBytes int64
	mu           sync.RWMutex
}

type analyzeTask struct {
	ID              string
	URL             string
	Type            string
	startAt         time.Time
	cancel          context.CancelFunc
	streamCh        chan *models.StreamInfo
	aggDone         chan struct{}
	droppedCount    int64
	lastIFrameTS    int64
	currentGOPCount int
	videoWidth      int
	videoHeight     int
	videoFrameRate  float64
	videoProfile    string
	videoLevel      int
	naluLengthSize  int
	audioSampleRate int
	audioChannels   int
	previewHub      *PreviewHub
	previewEnc      *PreviewEncoder
	dump            *streamDumper
}

type taskStartupError struct {
	err error
}

func (e *taskStartupError) Error() string {
	return e.err.Error()
}

func (e *taskStartupError) Unwrap() error {
	return e.err
}

// NewStreamAnalyzer creates the analyzer. maxDumpMB caps how much raw stream a
// single dump-enabled task may write; 0 means unlimited.
func NewStreamAnalyzer(csvStorage *storage.CSVStorage, maxDumpMB int) *StreamAnalyzer {
	var maxDumpBytes int64
	if maxDumpMB > 0 {
		maxDumpBytes = int64(maxDumpMB) * 1024 * 1024
	}
	return &StreamAnalyzer{
		tasks:        make(map[string]*analyzeTask),
		taskHistory:  make(map[string]*models.Task),
		storage:      csvStorage,
		maxDumpBytes: maxDumpBytes,
	}
}

func (s *StreamAnalyzer) StartTask(ctx context.Context, taskReq *models.TaskRequest) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())

	if err := s.storage.CreateTaskCSV(taskID); err != nil {
		return nil, fmt.Errorf("failed to create task XLSX: %w", err)
	}

	taskCtx, cancel := context.WithCancel(context.Background())
	at := &analyzeTask{
		ID:       taskID,
		URL:      taskReq.URL,
		Type:     taskReq.Type,
		startAt:  time.Now(),
		cancel:   cancel,
		streamCh: make(chan *models.StreamInfo, 4096),
		aggDone:  make(chan struct{}),
	}
	if at.Type == "rtmp" || at.Type == "http-flv" {
		at.previewHub = NewPreviewHub()
		if enc, err := NewPreviewEncoder(); err == nil {
			at.previewEnc = enc
		}
	}
	if taskReq.Dump {
		dumper, err := newStreamDumper(s.storage.BaseDir(), taskID, s.maxDumpBytes)
		if err != nil {
			return nil, err
		}
		at.dump = dumper
	}

	s.tasks[taskID] = at

	task := &models.Task{
		ID:        taskID,
		URL:       taskReq.URL,
		Type:      taskReq.Type,
		Status:    "running",
		Dump:      taskReq.Dump,
		StartTime: time.Now(),
	}

	go s.runTask(taskCtx, at)
	return task, nil
}

func (s *StreamAnalyzer) runTask(ctx context.Context, at *analyzeTask) {
	go s.runTaskAggregator(ctx, at)

	status := "stopped"

	defer func() {
		at.cancel()
		close(at.streamCh)
		<-at.aggDone
		s.storage.CloseTask(at.ID)
		at.dump.Close()
		s.mu.Lock()
		s.taskHistory[at.ID] = &models.Task{
			ID:      at.ID,
			URL:     at.URL,
			Type:    at.Type,
			Status:  status,
			Dump:    at.dump != nil,
			EndTime: time.Now(),
		}
		delete(s.tasks, at.ID)
		s.mu.Unlock()
	}()

	var err error
	switch at.Type {
	case "rtmp":
		err = s.runRTMPTask(ctx, at)
	case "http-flv":
		err = s.runHTTPFLVTask(ctx, at)
	case "hls":
		err = s.runHLSTask(ctx, at)
	default:
		err = fmt.Errorf("unknown task type: %s", at.Type)
	}

	status = classifyTaskStatus(ctx, err)
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Printf("task=%s ended status=%s err=%v\n", at.ID, status, err)
	}
}

func (s *StreamAnalyzer) runTaskAggregator(ctx context.Context, at *analyzeTask) {
	defer close(at.aggDone)

	heartbeatTicker := time.NewTicker(time.Second)
	flushTicker := time.NewTicker(5 * time.Second)
	selfCheckTicker := time.NewTicker(5 * time.Second)
	defer heartbeatTicker.Stop()
	defer flushTicker.Stop()
	defer selfCheckTicker.Stop()
	defer s.storage.Flush(at.ID)

	pending := make([]*models.StreamInfo, 0, 2048)
	var totalVideoFrames, totalAudioFrames int64
	var totalVideoBytes, totalAudioBytes int64
	selfCheckUntil := at.startAt.Add(30 * time.Second)
	var lastVideoFrames, lastAudioFrames int64
	var lastVideoBytes, lastAudioBytes int64
	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		for _, info := range pending {
			if info == nil {
				continue
			}
			if err := s.storage.WriteStreamInfo(info); err != nil {
				fmt.Printf("task=%s write xlsx failed: %v\n", at.ID, err)
			}
		}
		pending = pending[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flushPending()
			return
		case info, ok := <-at.streamCh:
			if !ok {
				flushPending()
				return
			}
			if info == nil {
				continue
			}
			if info.VideoLen > 0 {
				totalVideoFrames++
				totalVideoBytes += info.VideoLen
			}
			if info.AudioLen > 0 {
				totalAudioFrames++
				totalAudioBytes += info.AudioLen
			}
			pending = append(pending, info)
			if len(pending) >= 1024 {
				flushPending()
			}
		case t := <-heartbeatTicker.C:
			hb := &models.StreamInfo{
				TaskID:     at.ID,
				RecordedAt: t.Truncate(time.Second),
			}
			pending = append(pending, hb)
			flushPending()
		case <-flushTicker.C:
			flushPending()
			s.storage.Flush(at.ID)
			// Keep the dump readable/downloadable while the task is running.
			at.dump.Flush()
		case now := <-selfCheckTicker.C:
			if now.After(selfCheckUntil) {
				continue
			}
			fmt.Printf(
				"task=%s self-check elapsed=%ds vFrames=%d(+%d) aFrames=%d(+%d) vBytes=%d(+%d) aBytes=%d(+%d) pending=%d dropped=%d\n",
				at.ID,
				int(now.Sub(at.startAt).Seconds()),
				totalVideoFrames, totalVideoFrames-lastVideoFrames,
				totalAudioFrames, totalAudioFrames-lastAudioFrames,
				totalVideoBytes, totalVideoBytes-lastVideoBytes,
				totalAudioBytes, totalAudioBytes-lastAudioBytes,
				len(pending), at.droppedCount,
			)
			lastVideoFrames, lastAudioFrames = totalVideoFrames, totalAudioFrames
			lastVideoBytes, lastAudioBytes = totalVideoBytes, totalAudioBytes
		}
	}
}

func (s *StreamAnalyzer) runRTMPTask(ctx context.Context, at *analyzeTask) error {
	// The dump is the raw TCP downstream, handshake included, so it can be fed
	// back into offline "raw" analysis as-is.
	var recvTap io.Writer
	if at.dump != nil {
		recvTap = at.dump.Writer(at.ID + ".rtmp")
	}

	decoder, err := NewRTMPDecoder(ctx, at.URL, recvTap)
	if err != nil {
		return &taskStartupError{err: err}
	}
	defer decoder.Close()

	return s.readRTMPStream(ctx, decoder, at)
}

func shouldStopDecode(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, context.Canceled)
}

func classifyTaskStatus(ctx context.Context, err error) string {
	var startupErr *taskStartupError

	switch {
	case err == nil:
		return "stopped"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "stopped"
	case errors.As(err, &startupErr):
		return "failed"
	case errors.Is(err, io.EOF), errors.Is(err, gortmp.ErrServerDisconnected), errors.Is(err, gortmp.ErrClientClosed):
		return "stopped"
	case ctx.Err() != nil:
		return "stopped"
	default:
		return "failed"
	}
}

func (s *StreamAnalyzer) handleFLVTag(flvTag *tag.FlvTag, at *analyzeTask) {
	info := &models.StreamInfo{
		TaskID:     at.ID,
		DTS:        int64(flvTag.Timestamp),
		RecordedAt: time.Now(),
	}

	switch data := flvTag.Data.(type) {
	case *tag.VideoData:
		isLegacyAVCHeader := (data.CodecID == tag.CodecIDAVC || data.CodecID == tag.CodecIDHEVC) &&
			data.AVCPacketType == tag.AVCPacketTypeSequenceHeader
		isEnhancedAVCHeader := data.IsExHeader &&
			data.VideoPacketType == tag.VideoPacketTypeSequenceStart &&
			(data.VideoFourCC == tag.FourCCVideoAVC || data.VideoFourCC == tag.FourCCVideoHEVC)
		isHEVC := data.CodecID == tag.CodecIDHEVC || data.VideoFourCC == tag.FourCCVideoHEVC

		// Read the whole payload: frame sizes drive the bitrate charts, and the
		// NAL units below are parsed from it.
		payload, _ := io.ReadAll(data.Data)
		data.Close()

		if isLegacyAVCHeader || isEnhancedAVCHeader {
			if len(payload) > 0 {
				var meta *codec.VideoMeta
				var err error
				if isHEVC {
					meta, err = codec.ParseHEVCDecoderConfig(payload)
					if rec, rerr := codec.ParseHEVCDecoderConfigurationRecord(payload); rerr == nil {
						at.naluLengthSize = int(rec.LengthSizeMinusOne) + 1
					}
				} else {
					meta, err = codec.ParseAVCDecoderConfig(payload)
					if rec, rerr := codec.ParseAVCDecoderConfigurationRecord(payload); rerr == nil {
						at.naluLengthSize = int(rec.LengthSizeMinusOne) + 1
					}
				}
				if err == nil && meta != nil {
					if meta.Width > 0 {
						at.videoWidth = meta.Width
					}
					if meta.Height > 0 {
						at.videoHeight = meta.Height
					}
					if meta.FrameRate > 0 {
						at.videoFrameRate = meta.FrameRate
					}
					if meta.Profile > 0 {
						at.videoProfile = meta.ProfileName
						at.videoLevel = meta.Level
					}
				}
			}
		}

		at.currentGOPCount++
		info.GOPSize = at.currentGOPCount

		switch data.FrameType {
		case tag.FrameTypeKeyFrame:
			if isLegacyAVCHeader || isEnhancedAVCHeader {
				info.FrameType = "H"
				break
			}
			info.FrameType = "I"
			info.IsKeyFrame = true
			if at.lastIFrameTS > 0 {
				info.IFrameInterval = int64(flvTag.Timestamp) - at.lastIFrameTS
			}
			at.lastIFrameTS = int64(flvTag.Timestamp)
			at.currentGOPCount = 1
			info.GOPSize = 1
		case tag.FrameTypeInterFrame:
			info.FrameType = "P"
		case tag.FrameTypeDisposableInterFrame:
			info.FrameType = fmt.Sprintf("%d", data.FrameType)
		case tag.FrameTypeGeneratedKeyFrame:
			info.FrameType = fmt.Sprintf("%d", data.FrameType)
		default:
			info.FrameType = fmt.Sprintf("%d", data.FrameType)
		}

		info.VideoCodec = normalizeVideoCodec(data)
		info.VideoWidth = at.videoWidth
		info.VideoHeight = at.videoHeight
		info.VideoFrameRate = at.videoFrameRate
		info.VideoProfile = at.videoProfile
		info.VideoLevel = at.videoLevel
		info.CTS = int64(data.CompositionTime)
		if data.CompositionTime != 0 {
			info.PTS = int64(flvTag.Timestamp) + int64(data.CompositionTime)
		} else {
			info.PTS = int64(flvTag.Timestamp)
		}
		info.VideoLen = int64(len(payload))

		if !isLegacyAVCHeader && !isEnhancedAVCHeader && len(payload) > 0 {
			lengthSize := at.naluLengthSize
			if lengthSize == 0 {
				lengthSize = 4
			}
			nalus := codec.SplitAVCC(payload, lengthSize, isHEVC)
			info.NALUCount = len(nalus)
			info.NALUTypes = summarizeNALUTypes(nalus)
			// The FLV frame type cannot express B frames, so refine inter frames
			// with the real slice_type. Keyframes keep the FLV classification,
			// which drives GOP/I-frame accounting and is reliable.
			if !info.IsKeyFrame {
				if kind := codec.FrameKindFromNALUs(payload, lengthSize, isHEVC); kind == "B" || kind == "P" {
					info.FrameType = kind
				}
			}
		}
	case *tag.AudioData:
		isLegacyAACHeader := data.SoundFormat == tag.SoundFormatAAC &&
			data.AACPacketType == tag.AACPacketTypeSequenceHeader
		isEnhancedAACHeader := data.IsExHeader &&
			data.AudioPacketType == tag.AudioPacketTypeSequenceStart &&
			data.AudioFourCC == tag.FourCCAudioAAC

		payload, _ := io.ReadAll(data.Data)
		data.Close()

		if isLegacyAACHeader || isEnhancedAACHeader {
			if len(payload) > 0 {
				meta, err := codec.ParseAACAudioSpecificConfig(payload)
				if err == nil && meta != nil {
					if meta.SampleRate > 0 {
						at.audioSampleRate = meta.SampleRate
					}
					if meta.Channels > 0 {
						at.audioChannels = meta.Channels
					}
				}
			}
			info.FrameType = "H"
			break
		}
		info.AudioCodec = normalizeAudioCodec(data)
		info.SampleRate = at.audioSampleRate
		info.Channels = at.audioChannels
		info.AudioLen = int64(len(payload))
	case *tag.ScriptData:
		if len(data.Objects) > 0 {
			if b, err := json.Marshal(data.Objects); err == nil {
				info.MetadataJSON = string(b)
			}

			// Fallback codec info from metadata (common in RTMP onMetaData).
			if md, ok := data.Objects["onMetaData"]; ok {
				if info.VideoCodec == "" {
					if v, ok := pickNumber(md, "videocodecid"); ok {
						info.VideoCodec = fmt.Sprintf("%d", int(v))
					}
				}
				if info.AudioCodec == "" {
					if v, ok := pickNumber(md, "audiocodecid"); ok {
						info.AudioCodec = fmt.Sprintf("%d", int(v))
					}
				}
				if at.videoWidth == 0 {
					if v, ok := pickNumber(md, "width"); ok && v > 0 {
						at.videoWidth = int(v)
					}
				}
				if at.videoHeight == 0 {
					if v, ok := pickNumber(md, "height"); ok && v > 0 {
						at.videoHeight = int(v)
					}
				}
				if at.videoFrameRate == 0 {
					if v, ok := pickNumber(md, "framerate"); ok && v > 0 {
						at.videoFrameRate = v
					}
				}
				if at.audioSampleRate == 0 {
					if v, ok := pickNumber(md, "audiosamplerate"); ok && v > 0 {
						at.audioSampleRate = int(v)
					}
				}
			}
		}
	}

	select {
	case at.streamCh <- info:
	default:
		at.droppedCount++
		if at.droppedCount%200 == 0 {
			fmt.Printf("task=%s stream backlog: dropped=%d (streamCh full)\n", at.ID, at.droppedCount)
		}
	}
}

// summarizeNALUTypes renders the NAL units of one frame as "SPS:1;IDR:2",
// keeping the order in which the types first appear.
func summarizeNALUTypes(nalus []codec.NALU) string {
	if len(nalus) == 0 {
		return ""
	}
	order := make([]string, 0, 4)
	counts := make(map[string]int, 4)
	for _, n := range nalus {
		if _, ok := counts[n.Name]; !ok {
			order = append(order, n.Name)
		}
		counts[n.Name]++
	}

	var b strings.Builder
	for i, name := range order {
		if i > 0 {
			b.WriteByte(';')
		}
		fmt.Fprintf(&b, "%s:%d", name, counts[name])
	}
	return b.String()
}

func pickNumber(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

func normalizeVideoCodec(data *tag.VideoData) string {
	if data == nil {
		return ""
	}
	if data.IsExHeader && data.VideoFourCC != 0 {
		// Keep compatibility with current frontend numeric codec mapping.
		switch data.VideoFourCC {
		case tag.FourCCVideoAVC:
			return fmt.Sprintf("%d", tag.CodecIDAVC) // 7
		case tag.FourCCVideoHEVC:
			return fmt.Sprintf("%d", tag.CodecIDHEVC) // 12
		case tag.FourCCVideoVP8:
			return "13"
		case tag.FourCCVideoVP9:
			return "14"
		case tag.FourCCVideoAV1:
			return "15"
		default:
			return data.VideoFourCC.String()
		}
	}
	return fmt.Sprintf("%d", data.CodecID)
}

func normalizeAudioCodec(data *tag.AudioData) string {
	if data == nil {
		return ""
	}
	if data.IsExHeader && data.AudioFourCC != 0 {
		// Keep compatibility with current frontend numeric codec mapping.
		switch data.AudioFourCC {
		case tag.FourCCAudioAAC:
			return fmt.Sprintf("%d", tag.SoundFormatAAC) // 10
		case tag.FourCCAudioMP3:
			return fmt.Sprintf("%d", tag.SoundFormatMP3) // 2
		case tag.FourCCAudioOpus:
			return "13"
		case tag.FourCCAudioAC3:
			return "15"
		case tag.FourCCAudioEAC3:
			return "16"
		case tag.FourCCAudioFLAC:
			return "17"
		default:
			return data.AudioFourCC.String()
		}
	}
	return fmt.Sprintf("%d", data.SoundFormat)
}

func (s *StreamAnalyzer) StopTask(taskID string) error {
	s.mu.RLock()
	at, ok := s.tasks[taskID]
	s.mu.RUnlock()

	if !ok {
		// Idempotent stop: if task already stopped or missing, treat as success.
		return nil
	}

	at.cancel()
	return nil
}

func (s *StreamAnalyzer) GetTaskStatus(taskID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.tasks[taskID]; ok {
		return "running", nil
	}
	if task, ok := s.taskHistory[taskID]; ok {
		return task.Status, nil
	}
	return "unknown", fmt.Errorf("task %s not found", taskID)
}

func (s *StreamAnalyzer) GetTask(taskID string) (*models.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if at, ok := s.tasks[taskID]; ok {
		return &models.Task{
			ID:        at.ID,
			URL:       at.URL,
			Type:      at.Type,
			Status:    "running",
			Dump:      at.dump != nil,
			StartTime: at.startAt,
		}, nil
	}
	if task, ok := s.taskHistory[taskID]; ok {
		return task, nil
	}
	return nil, fmt.Errorf("task %s not found", taskID)
}

func (s *StreamAnalyzer) ListAllTasks() []*models.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*models.Task, 0, len(s.tasks)+len(s.taskHistory))
	for id, at := range s.tasks {
		tasks = append(tasks, &models.Task{
			ID:     id,
			URL:    at.URL,
			Type:   at.Type,
			Status: "running",
			Dump:   at.dump != nil,
		})
	}
	for _, task := range s.taskHistory {
		tasks = append(tasks, task)
	}
	return tasks
}

func (s *StreamAnalyzer) ListTasks() []*models.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*models.Task, 0, len(s.tasks))
	for id, at := range s.tasks {
		tasks = append(tasks, &models.Task{
			ID:     id,
			URL:    at.URL,
			Type:   at.Type,
			Status: "running",
			Dump:   at.dump != nil,
		})
	}
	return tasks
}

// ListDumpFiles reports the raw-stream dump files of a task. It reads the dump
// directory directly, so it also works for tasks that already finished.
func (s *StreamAnalyzer) ListDumpFiles(taskID string) ([]models.DumpFile, error) {
	dir := s.DumpDir(taskID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.DumpFile{}, nil
		}
		return nil, err
	}

	files := make([]models.DumpFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, models.DumpFile{Name: e.Name(), SizeBytes: info.Size()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// DumpDir returns the dump directory of a task, whether or not it exists.
func (s *StreamAnalyzer) DumpDir(taskID string) string {
	return filepath.Join(s.storage.BaseDir(), "dump", taskID)
}

// FlushDump makes the bytes buffered so far visible on disk, so a dump of a
// running task can be downloaded without stopping it.
func (s *StreamAnalyzer) FlushDump(taskID string) {
	s.mu.RLock()
	at, ok := s.tasks[taskID]
	s.mu.RUnlock()
	if ok {
		at.dump.Flush()
	}
}

// DumpLimitReached reports whether a running task's dump was cut short by the
// size cap. Finished tasks report false: the state lives with the task.
func (s *StreamAnalyzer) DumpLimitReached(taskID string) bool {
	s.mu.RLock()
	at, ok := s.tasks[taskID]
	s.mu.RUnlock()
	return ok && at.dump.LimitReached()
}

// MaxDumpMB reports the configured per-task dump cap in MB (0 = unlimited).
func (s *StreamAnalyzer) MaxDumpMB() int {
	return int(s.maxDumpBytes / (1024 * 1024))
}

func (s *StreamAnalyzer) SubscribePreview(taskID string) chan []byte {
	s.mu.RLock()
	at, ok := s.tasks[taskID]
	s.mu.RUnlock()
	if !ok || at.previewHub == nil {
		ch := make(chan []byte)
		close(ch)
		return ch
	}
	return at.previewHub.Subscribe(taskID)
}

func (s *StreamAnalyzer) UnsubscribePreview(taskID string, ch chan []byte) {
	s.mu.RLock()
	at, ok := s.tasks[taskID]
	s.mu.RUnlock()
	if !ok || at.previewHub == nil {
		return
	}
	at.previewHub.Unsubscribe(taskID, ch)
}

func (s *StreamAnalyzer) GetPreviewHeader(taskID string) []byte {
	s.mu.RLock()
	at, ok := s.tasks[taskID]
	s.mu.RUnlock()
	if !ok || at.previewEnc == nil {
		return nil
	}
	return at.previewEnc.Header()
}

func (s *StreamAnalyzer) ParseRTMPURL(url string) (host, app, stream string) {
	url = strings.TrimPrefix(url, "rtmp://")
	parts := strings.SplitN(url, "/", 3)
	host = parts[0]
	if len(parts) > 1 {
		app = parts[1]
	}
	if len(parts) > 2 {
		stream = parts[2]
	}
	return
}
