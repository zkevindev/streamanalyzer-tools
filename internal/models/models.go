package models

import "time"

type Task struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Type      string    `json:"type"`   // rtmp, http-flv, hls
	Status    string    `json:"status"` // running, stopped, failed
	Dump      bool      `json:"dump"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time,omitempty"`
}

type StreamInfo struct {
	TaskID         string    `json:"task_id"`
	DTS            int64     `json:"dts"`
	VideoLen       int64     `json:"video_len"`
	AudioLen       int64     `json:"audio_len"`
	VideoWidth     int       `json:"video_width"`
	VideoHeight    int       `json:"video_height"`
	VideoFrameRate float64   `json:"video_frame_rate"`
	VideoCodec     string    `json:"video_codec"`
	AudioCodec     string    `json:"audio_codec"`
	SampleRate     int       `json:"sample_rate"`
	Channels       int       `json:"channels"`
	PTS            int64     `json:"pts"`
	CTS            int64     `json:"cts"`
	FrameType      string    `json:"frame_type"`      // I/P/B frame
	IsKeyFrame     bool      `json:"is_key_frame"`    // is I frame
	IFrameInterval int64     `json:"iframe_interval"` // I帧间隔(ms)
	GOPSize        int       `json:"gop_size"`        // 当前GOP中的帧数
	RecordedAt     time.Time `json:"recorded_at"`
	MetadataJSON   string    `json:"metadata_json"`
	VideoProfile   string    `json:"video_profile"` // 如 High / Main
	VideoLevel     int       `json:"video_level"`   // level_idc，如 31 表示 3.1
	NALUCount      int       `json:"nalu_count"`
	NALUTypes      string    `json:"nalu_types"` // 如 "SPS:1;PPS:1;IDR:1"
}

type TaskRequest struct {
	URL  string `json:"url" binding:"required"`
	Type string `json:"type" binding:"required"` // rtmp, http-flv, hls
	Dump bool   `json:"dump"`                    // dump raw received bytes (no demuxing)
}

// DumpFile 描述一个任务 dump 出的原始码流文件。
type DumpFile struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
}

// ChartHLSSegment 实时 HLS（TS）任务：按切片维度展示。
type ChartHLSSegment struct {
	TaskID string `json:"-"`

	StreamID    string  `json:"stream_id"`
	Seq         uint64  `json:"seq"`
	URI         string  `json:"uri"`
	DurationSec float64 `json:"duration_sec"`
	SizeBytes   int     `json:"size_bytes"`

	VideoPTSFirst90k int64 `json:"video_pts_first_90k"`
	VideoPTSLast90k  int64 `json:"video_pts_last_90k"`
	VideoDTSFirst90k int64 `json:"video_dts_first_90k"`
	VideoDTSLast90k  int64 `json:"video_dts_last_90k"`
	AudioPTSFirst90k int64 `json:"audio_pts_first_90k"`
	AudioPTSLast90k  int64 `json:"audio_pts_last_90k"`
	AudioDTSFirst90k int64 `json:"audio_dts_first_90k"`
	AudioDTSLast90k  int64 `json:"audio_dts_last_90k"`

	AVDiffPTS90k int64 `json:"av_diff_pts_90k"`
	AVDiffValid  bool  `json:"av_diff_valid"`

	AVDiffDTS90k      int64   `json:"av_diff_dts_90k"`
	AVDiffDTSValid    bool    `json:"av_diff_dts_valid"`
	IFrameIntervalsMs []int64 `json:"iframe_intervals_ms,omitempty"`

	PATCount int `json:"pat_count"`
	PMTCount int `json:"pmt_count"`
	PESCount int `json:"pes_count"`
	VideoPES int `json:"video_pes"`
	AudioPES int `json:"audio_pes"`
}

type ChartData struct {
	TaskID          string        `json:"task_id"`
	VideoWidth      int           `json:"video_width"`
	VideoHeight     int           `json:"video_height"`
	VideoCodec      string        `json:"video_codec"`
	AudioCodec      string        `json:"audio_codec"`
	SampleRate      int           `json:"sample_rate"`
	Channels        int           `json:"channels"`
	VideoDTS        []int64       `json:"video_dts"`
	VideoPTS        []int64       `json:"video_pts"`
	VideoCTS        []int64       `json:"video_cts"`
	AudioDTS        []int64       `json:"audio_dts"`
	VideoLens       []int64       `json:"video_lens"`
	AudioLens       []int64       `json:"audio_lens"`
	IFrameIntervals []int64       `json:"iframe_intervals"`
	FrameTypes      []string      `json:"frame_types"`
	GOPSizes        []int         `json:"gop_sizes"`
	SecondStats     []*SecondStat `json:"second_stats"`
	VideoFrameRate  float64       `json:"video_frame_rate"`
	MetadataJSON    string        `json:"metadata_json"`

	HLSSegments []ChartHLSSegment `json:"hls_segments,omitempty"`

	VideoProfile string `json:"video_profile,omitempty"`
	VideoLevel   int    `json:"video_level,omitempty"`

	// 分析增强：诊断结论、AV 同步曲线、抖动、帧类型/NALU 分布。
	Health       *StreamHealth   `json:"health,omitempty"`
	AVSyncPoints []AVSyncPoint   `json:"av_sync_points,omitempty"`
	JitterPoints []JitterPoint   `json:"jitter_points,omitempty"`
	FrameKinds   []FrameKindStat `json:"frame_kinds,omitempty"`
	NALUStats    []NALUStat      `json:"nalu_stats,omitempty"`
}

// StreamHealth 汇总一次分析的整体结论与告警明细。
type StreamHealth struct {
	Score   int     `json:"score"`   // 0-100
	Level   string  `json:"level"`   // good / warn / bad
	Summary string  `json:"summary"` // 一句话结论
	Alerts  []Alert `json:"alerts"`

	VideoFPSAvg      float64 `json:"video_fps_avg"`
	VideoBitrateAvg  float64 `json:"video_bitrate_avg_kbps"`
	AudioBitrateAvg  float64 `json:"audio_bitrate_avg_kbps"`
	BitratePeakRatio float64 `json:"bitrate_peak_ratio"` // 峰值/均值
	GOPAvgMs         float64 `json:"gop_avg_ms"`
	GOPJitterMs      float64 `json:"gop_jitter_ms"` // I 帧间隔标准差
	JitterAvgMs      float64 `json:"jitter_avg_ms"`
	JitterMaxMs      float64 `json:"jitter_max_ms"`
	AVSyncMaxMs      int64   `json:"av_sync_max_ms"`
}

// Alert 单条诊断结论。
type Alert struct {
	Severity string `json:"severity"` // error / warn / info
	Code     string `json:"code"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Count    int    `json:"count,omitempty"`
	AtDTS    int64  `json:"at_dts,omitempty"` // 首次出现位置
}

type AVSyncPoint struct {
	Second int   `json:"second"`
	DiffMs int64 `json:"diff_ms"` // video DTS - audio DTS
}

type JitterPoint struct {
	Index          int     `json:"index"`
	DTS            int64   `json:"dts"`
	JitterMs       float64 `json:"jitter_ms"`
	ArrivalDeltaMs float64 `json:"arrival_delta_ms"`
}

type FrameKindStat struct {
	Kind       string  `json:"kind"` // I / P / B
	Count      int     `json:"count"`
	AvgBytes   float64 `json:"avg_bytes"`
	MaxBytes   int64   `json:"max_bytes"`
	TotalBytes int64   `json:"total_bytes"`
}

type NALUStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type SecondStat struct {
	Second       int     `json:"second"`
	VideoBytes   int64   `json:"video_bytes"`
	AudioBytes   int64   `json:"audio_bytes"`
	VideoBitrate float64 `json:"video_bitrate"` // Mbps
	AudioBitrate float64 `json:"audio_bitrate"` // Mbps
	VideoFPS     int     `json:"video_fps"`
	AudioFPS     int     `json:"audio_fps"`
}

// =============================
// Offline file analysis models

type OfflineMode string

const (
	OfflineModeRaw  OfflineMode = "raw"
	OfflineModePCAP OfflineMode = "pcap"
	OfflineModeTS   OfflineMode = "ts"
	OfflineModeFLV  OfflineMode = "flv"
	OfflineModeMP4  OfflineMode = "mp4"
)

type OfflineStatus string

const (
	OfflineStatusPending OfflineStatus = "pending"
	OfflineStatusRunning OfflineStatus = "running"
	OfflineStatusDone    OfflineStatus = "done"
	OfflineStatusFailed  OfflineStatus = "failed"
)

type OfflineTaskRequest struct {
	Mode       OfflineMode `json:"mode" binding:"required"` // raw or pcap
	ServerPort uint16      `json:"server_port"`             // only for pcap
	SkipBytes  int         `json:"skip_bytes"`              // handshake bytes
}

type OfflineTask struct {
	ID     string        `json:"id"`
	Mode   OfflineMode   `json:"mode"`
	Status OfflineStatus `json:"status"`

	ServerPort uint16 `json:"server_port"`
	SkipBytes  int    `json:"skip_bytes"`

	InputName string `json:"input_name"`
	InputPath string `json:"input_path"`

	SummaryPath string `json:"summary_path"`
	Error       string `json:"error,omitempty"`

	CreatedAt  time.Time `json:"created_at"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

type OfflineSummary struct {
	TaskID string              `json:"task_id"`
	Mode   OfflineMode         `json:"mode"`
	Flows  []OfflineFlowResult `json:"flows"`
}

type OfflineFlowResult struct {
	FlowID int `json:"flow_id"`

	HasSYN       bool `json:"has_syn"`
	SYNCount     int  `json:"syn_count"`
	TCPPktCount  int  `json:"tcp_pkt_count"`
	PayloadBytes int  `json:"payload_bytes"`

	ClientIP   string `json:"client_ip"`
	ClientPort uint16 `json:"client_port"`
	ServerIP   string `json:"server_ip"`
	ServerPort uint16 `json:"server_port"`

	Direction  string `json:"direction"`    // pull/push/raw
	DumpRawDir string `json:"dump_raw_dir"` // server->client / client->server / single

	RawPath   string `json:"raw_path,omitempty"`
	VideoPath string `json:"video_path,omitempty"`
	AudioPath string `json:"audio_path,omitempty"`
	// ArtifactPaths 额外产物（如 MP4 的 mp4_report.txt），绝对路径。
	ArtifactPaths []string `json:"artifact_paths,omitempty"`

	ProgramCount  int `json:"program_count,omitempty"`
	PATCount      int `json:"pat_count,omitempty"`
	PMTCount      int `json:"pmt_count,omitempty"`
	PESCount      int `json:"pes_count,omitempty"`
	NALUCount     int `json:"nalu_count,omitempty"`
	VideoPIDCount int `json:"video_pid_count,omitempty"`
	AudioPIDCount int `json:"audio_pid_count,omitempty"`

	VideoCodec   string               `json:"video_codec,omitempty"`
	AudioCodec   string               `json:"audio_codec,omitempty"`
	Error        string               `json:"error,omitempty"`
	FLVMetadata  string               `json:"flv_metadata_json,omitempty"`
	PIDDetails   []OfflinePIDDetail   `json:"pid_details,omitempty"`
	FrameDetails []OfflineFrameDetail `json:"frame_details,omitempty"`

	PESDetails          []OfflinePESDetail `json:"pes_details,omitempty"`
	PESDetailTotal      int                `json:"pes_detail_total,omitempty"`
	PESDetailsTruncated bool               `json:"pes_details_truncated,omitempty"`
}

type OfflinePIDDetail struct {
	PID        uint16 `json:"pid"`
	StreamType string `json:"stream_type,omitempty"`
	Category   string `json:"category,omitempty"`
	PESCount   int    `json:"pes_count,omitempty"`
	NALUCount  int    `json:"nalu_count,omitempty"`
	Bytes      int64  `json:"bytes,omitempty"`
	OutputPath string `json:"output_path,omitempty"`
}

type OfflineFrameDetail struct {
	MediaType string `json:"media_type,omitempty"` // video/audio
	DTS       int64  `json:"dts,omitempty"`
	PTS       int64  `json:"pts,omitempty"`
	CTS       int64  `json:"cts,omitempty"`
	FrameLen  int    `json:"frame_len,omitempty"`
	FrameType string `json:"frame_type,omitempty"` // I/P/B/- for audio
}

// OfflinePESDetail 对应 TS 每个 PES 包一行（与 example/parse_ts 输出字段对齐）。
type OfflinePESDetail struct {
	Seq           int                `json:"seq"`
	PID           uint16             `json:"pid"`
	ProgramNumber uint16             `json:"program_number,omitempty"`
	StreamID      uint8              `json:"stream_id,omitempty"`
	StreamType    string             `json:"stream_type,omitempty"`
	Category      string             `json:"category,omitempty"`
	PTSBase       int64              `json:"pts_base,omitempty"` // 90kHz 基准值，无 PTS 时为 0 且 PTSValid=false
	PTSValid      bool               `json:"pts_valid,omitempty"`
	DTSBase       int64              `json:"dts_base,omitempty"`
	DTSValid      bool               `json:"dts_valid,omitempty"`
	PayloadLen    int                `json:"payload_len"`
	NALUCount     int                `json:"nalu_count,omitempty"`
	NALUs         []OfflineNALUBrief `json:"nalus,omitempty"`
}

type OfflineNALUBrief struct {
	Index    int    `json:"index"`
	Codec    string `json:"codec,omitempty"` // H264 / H265
	Type     int    `json:"type,omitempty"`
	TypeName string `json:"type_name,omitempty"`
	Len      int    `json:"len,omitempty"`
	Key      bool   `json:"key,omitempty"`
}
