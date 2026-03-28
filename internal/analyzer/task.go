package analyzer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"streamanalyzer/internal/codec"
	"streamanalyzer/internal/models"
	"streamanalyzer/internal/storage"

	"github.com/yutopp/go-flv/tag"
)

type StreamAnalyzer struct {
	tasks       map[string]*analyzeTask
	taskHistory map[string]*models.Task
	storage     *storage.CSVStorage
	mu          sync.RWMutex
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
	audioSampleRate int
	audioChannels   int
}

func NewStreamAnalyzer(csvStorage *storage.CSVStorage) *StreamAnalyzer {
	return &StreamAnalyzer{
		tasks:       make(map[string]*analyzeTask),
		taskHistory: make(map[string]*models.Task),
		storage:     csvStorage,
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

	s.tasks[taskID] = at

	task := &models.Task{
		ID:        taskID,
		URL:       taskReq.URL,
		Type:      taskReq.Type,
		Status:    "running",
		StartTime: time.Now(),
	}

	go s.runTask(taskCtx, at)
	return task, nil
}

func (s *StreamAnalyzer) runTask(ctx context.Context, at *analyzeTask) {
	go s.runTaskAggregator(ctx, at)

	defer func() {
		at.cancel()
		close(at.streamCh)
		<-at.aggDone
		s.storage.CloseTask(at.ID)
		s.mu.Lock()
		s.taskHistory[at.ID] = &models.Task{
			ID:      at.ID,
			URL:     at.URL,
			Type:    at.Type,
			Status:  "stopped",
			EndTime: time.Now(),
		}
		delete(s.tasks, at.ID)
		s.mu.Unlock()
	}()

	switch at.Type {
	case "rtmp":
		s.runRTMPTask(ctx, at)
	case "http-flv":
		s.runHTTPFLVTask(ctx, at)
	case "hls":
		s.runHLSTask(ctx, at)
	default:
		fmt.Printf("Unknown task type: %s\n", at.Type)
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

func (s *StreamAnalyzer) runRTMPTask(ctx context.Context, at *analyzeTask) {
	decoder, err := NewRTMPDecoder(ctx, at.URL)
	if err != nil {
		fmt.Printf("RTMP decoder error: %v\n", err)
		return
	}
	defer decoder.Close()

	s.readRTMPStream(ctx, decoder, at)
}

func shouldStopDecode(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, context.Canceled)
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

		if isLegacyAVCHeader || isEnhancedAVCHeader {
			payload, _ := io.ReadAll(data.Data)
			if len(payload) > 0 {
				var meta *codec.VideoMeta
				var err error
				if data.CodecID == tag.CodecIDAVC || data.VideoFourCC == tag.FourCCVideoAVC {
					meta, err = codec.ParseAVCDecoderConfig(payload)
				} else {
					meta, err = codec.ParseHEVCDecoderConfig(payload)
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
		if data.CompositionTime != 0 {
			info.PTS = int64(flvTag.Timestamp) + int64(data.CompositionTime)
		} else {
			info.PTS = int64(flvTag.Timestamp)
		}
		buf := make([]byte, 4096)
		n, _ := data.Read(buf)
		info.VideoLen = int64(n)
		data.Close()
	case *tag.AudioData:
		isLegacyAACHeader := data.SoundFormat == tag.SoundFormatAAC &&
			data.AACPacketType == tag.AACPacketTypeSequenceHeader
		isEnhancedAACHeader := data.IsExHeader &&
			data.AudioPacketType == tag.AudioPacketTypeSequenceStart &&
			data.AudioFourCC == tag.FourCCAudioAAC

		if isLegacyAACHeader || isEnhancedAACHeader {
			payload, _ := io.ReadAll(data.Data)
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
		buf := make([]byte, 4096)
		n, _ := data.Read(buf)
		info.AudioLen = int64(n)
		data.Close()
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
			StartTime: time.Now(),
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
		})
	}
	return tasks
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
