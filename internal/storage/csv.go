package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"streamanalyzer/internal/models"

	"github.com/xuri/excelize/v2"
)

type CSVStorage struct {
	baseDir string
	mu      sync.RWMutex
	files   map[string]*excelize.File
	paths   map[string]string
	rows    map[string]map[string]int
}

func NewCSVStorage(baseDir string) (*CSVStorage, error) {
	if err := ensureDir(baseDir); err != nil {
		return nil, err
	}
	return &CSVStorage{
		baseDir: baseDir,
		files:   make(map[string]*excelize.File),
		paths:   make(map[string]string),
		rows:    make(map[string]map[string]int),
	}, nil
}

func (s *CSVStorage) CreateTaskCSV(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filename := filepath.Join(s.baseDir, fmt.Sprintf("%s.xlsx", taskID))
	f := excelize.NewFile()

	defaultSheet := f.GetSheetName(0)
	streamSheet := "stream"
	videoSheet := "video"
	audioSheet := "audio"
	hlsSheet := "hls"
	if defaultSheet != streamSheet {
		f.SetSheetName(defaultSheet, streamSheet)
	}
	f.NewSheet(videoSheet)
	f.NewSheet(audioSheet)
	f.NewSheet(hlsSheet)

	if err := writeSheetHeader(f, streamSheet, []string{
		"dts", "video_len", "audio_len", "video_width", "video_height",
		"video_codec", "audio_codec", "sample_rate", "channels", "pts", "cts",
		"frame_type", "is_key_frame", "iframe_interval", "gop_size", "recorded_at", "video_frame_rate", "metadata_json",
	}); err != nil {
		return err
	}

	if err := writeSheetHeader(f, videoSheet, []string{
		"dts", "video_len", "video_width", "video_height",
		"video_codec", "pts", "cts", "frame_type", "is_key_frame", "iframe_interval", "gop_size", "recorded_at", "video_frame_rate",
	}); err != nil {
		return err
	}

	if err := writeSheetHeader(f, audioSheet, []string{
		"dts", "audio_len", "audio_codec", "sample_rate", "channels", "recorded_at",
	}); err != nil {
		return err
	}

	if err := writeSheetHeader(f, hlsSheet, []string{
		"stream_id", "seq", "uri", "duration_s", "size_b",
		"v_pts_f_90k", "v_pts_l_90k", "v_dts_f_90k", "v_dts_l_90k",
		"a_pts_f_90k", "a_pts_l_90k", "a_dts_f_90k", "a_dts_l_90k",
		"av_diff_pts_90k", "av_diff_ok",
		"pat", "pmt", "pes", "video_pes", "audio_pes",
		"av_diff_dts_90k", "av_diff_dts_ok", "iframe_intv_ms",
		"recorded_at",
	}); err != nil {
		return err
	}

	if err := f.SaveAs(filename); err != nil {
		return err
	}

	s.files[taskID] = f
	s.paths[taskID] = filename
	s.rows[taskID] = map[string]int{
		streamSheet: 2,
		videoSheet:  2,
		audioSheet:  2,
		hlsSheet:    2,
	}
	return nil
}

func (s *CSVStorage) WriteStreamInfo(info *models.StreamInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.files[info.TaskID]
	if !ok {
		return fmt.Errorf("task %s not found", info.TaskID)
	}
	rows := s.rows[info.TaskID]

	isKeyFrame := "0"
	if info.IsKeyFrame {
		isKeyFrame = "1"
	}

	streamRow := []string{
		fmt.Sprintf("%d", info.DTS),
		fmt.Sprintf("%d", info.VideoLen),
		fmt.Sprintf("%d", info.AudioLen),
		fmt.Sprintf("%d", info.VideoWidth),
		fmt.Sprintf("%d", info.VideoHeight),
		info.VideoCodec,
		info.AudioCodec,
		fmt.Sprintf("%d", info.SampleRate),
		fmt.Sprintf("%d", info.Channels),
		fmt.Sprintf("%d", info.PTS),
		fmt.Sprintf("%d", info.CTS),
		info.FrameType,
		isKeyFrame,
		fmt.Sprintf("%d", info.IFrameInterval),
		fmt.Sprintf("%d", info.GOPSize),
		info.RecordedAt.Format("2006-01-02 15:04:05"),
		fmt.Sprintf("%.3f", info.VideoFrameRate),
		info.MetadataJSON,
	}
	if err := writeRow(f, "stream", rows["stream"], streamRow); err != nil {
		return err
	}
	rows["stream"]++

	if info.VideoLen > 0 {
		videoRow := []string{
			fmt.Sprintf("%d", info.DTS),
			fmt.Sprintf("%d", info.VideoLen),
			fmt.Sprintf("%d", info.VideoWidth),
			fmt.Sprintf("%d", info.VideoHeight),
			info.VideoCodec,
			fmt.Sprintf("%d", info.PTS),
			fmt.Sprintf("%d", info.CTS),
			info.FrameType,
			isKeyFrame,
			fmt.Sprintf("%d", info.IFrameInterval),
			fmt.Sprintf("%d", info.GOPSize),
			info.RecordedAt.Format("2006-01-02 15:04:05"),
			fmt.Sprintf("%.3f", info.VideoFrameRate),
		}
		if err := writeRow(f, "video", rows["video"], videoRow); err != nil {
			return err
		}
		rows["video"]++
	}

	if info.AudioLen > 0 {
		audioRow := []string{
			fmt.Sprintf("%d", info.DTS),
			fmt.Sprintf("%d", info.AudioLen),
			info.AudioCodec,
			fmt.Sprintf("%d", info.SampleRate),
			fmt.Sprintf("%d", info.Channels),
			info.RecordedAt.Format("2006-01-02 15:04:05"),
		}
		if err := writeRow(f, "audio", rows["audio"], audioRow); err != nil {
			return err
		}
		rows["audio"]++
	}

	return nil
}

func int64OrEmpty(v int64) string {
	if v < 0 {
		return ""
	}
	return fmt.Sprintf("%d", v)
}

func joinInt64SliceSemicolon(vs []int64) string {
	if len(vs) == 0 {
		return ""
	}
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = fmt.Sprintf("%d", v)
	}
	return strings.Join(parts, ";")
}

// WriteHLSSegment 写入 HLS TS 切片一行（实时 HLS 任务使用）。
func (s *CSVStorage) WriteHLSSegment(seg *models.ChartHLSSegment) error {
	if seg == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.files[seg.TaskID]
	if !ok {
		return fmt.Errorf("task %s not found", seg.TaskID)
	}
	rows := s.rows[seg.TaskID]
	sheet := "hls"

	avOk := "0"
	if seg.AVDiffValid {
		avOk = "1"
	}
	avDiff := ""
	if seg.AVDiffValid {
		avDiff = fmt.Sprintf("%d", seg.AVDiffPTS90k)
	}
	avDtsOk := "0"
	if seg.AVDiffDTSValid {
		avDtsOk = "1"
	}
	avDtsDiff := ""
	if seg.AVDiffDTSValid {
		avDtsDiff = fmt.Sprintf("%d", seg.AVDiffDTS90k)
	}
	iframeJoined := joinInt64SliceSemicolon(seg.IFrameIntervalsMs)

	row := []string{
		seg.StreamID,
		fmt.Sprintf("%d", seg.Seq),
		seg.URI,
		fmt.Sprintf("%.6f", seg.DurationSec),
		fmt.Sprintf("%d", seg.SizeBytes),
		int64OrEmpty(seg.VideoPTSFirst90k),
		int64OrEmpty(seg.VideoPTSLast90k),
		int64OrEmpty(seg.VideoDTSFirst90k),
		int64OrEmpty(seg.VideoDTSLast90k),
		int64OrEmpty(seg.AudioPTSFirst90k),
		int64OrEmpty(seg.AudioPTSLast90k),
		int64OrEmpty(seg.AudioDTSFirst90k),
		int64OrEmpty(seg.AudioDTSLast90k),
		avDiff,
		avOk,
		fmt.Sprintf("%d", seg.PATCount),
		fmt.Sprintf("%d", seg.PMTCount),
		fmt.Sprintf("%d", seg.PESCount),
		fmt.Sprintf("%d", seg.VideoPES),
		fmt.Sprintf("%d", seg.AudioPES),
		avDtsDiff,
		avDtsOk,
		iframeJoined,
		time.Now().Format("2006-01-02 15:04:05"),
	}
	if err := writeRow(f, sheet, rows[sheet], row); err != nil {
		return err
	}
	rows[sheet]++
	return nil
}

func (s *CSVStorage) Flush(taskID string) {
	s.mu.RLock()
	f, ok := s.files[taskID]
	path := s.paths[taskID]
	s.mu.RUnlock()

	if ok {
		_ = f.SaveAs(path)
	}
}

func (s *CSVStorage) CloseTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if f, ok := s.files[taskID]; ok {
		path := s.paths[taskID]
		if err := f.SaveAs(path); err != nil {
			return err
		}
		_ = f.Close()
		delete(s.files, taskID)
		delete(s.paths, taskID)
		delete(s.rows, taskID)
	}
	return nil
}

func (s *CSVStorage) GetCSVPath(taskID string) string {
	return filepath.Join(s.baseDir, fmt.Sprintf("%s.xlsx", taskID))
}

func (s *CSVStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for taskID, f := range s.files {
		if err := f.SaveAs(s.paths[taskID]); err != nil {
			return err
		}
		_ = f.Close()
	}
	return nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func writeSheetHeader(f *excelize.File, sheet string, headers []string) error {
	return writeRow(f, sheet, 1, headers)
}

func writeRow(f *excelize.File, sheet string, row int, values []string) error {
	cell, err := excelize.CoordinatesToCellName(1, row)
	if err != nil {
		return err
	}
	rowValues := make([]interface{}, len(values))
	for i, v := range values {
		rowValues[i] = v
	}
	return f.SetSheetRow(sheet, cell, &rowValues)
}
