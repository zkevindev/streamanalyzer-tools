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
	OfflineModeAuto   OfflineMode = "auto"
	OfflineModeRaw    OfflineMode = "raw"
	OfflineModePCAP   OfflineMode = "pcap"
	OfflineModeTS     OfflineMode = "ts"
	OfflineModeFLV    OfflineMode = "flv"
	OfflineModeMP4    OfflineMode = "mp4"
	OfflineModeAnnexB OfflineMode = "annexb"
	OfflineModePS     OfflineMode = "ps"
	OfflineModeWebM   OfflineMode = "webm"
)

type OfflineStatus string

const (
	OfflineStatusPending  OfflineStatus = "pending"
	OfflineStatusRunning  OfflineStatus = "running"
	OfflineStatusDone     OfflineStatus = "done"
	OfflineStatusFailed   OfflineStatus = "failed"
	OfflineStatusCanceled OfflineStatus = "canceled"
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

	SourceType string `json:"source_type,omitempty"` // "upload" | "local"
	InputName  string `json:"input_name"`
	InputPath  string `json:"input_path"`
	InputSize  int64  `json:"input_size,omitempty"`
	DetectedBy string `json:"detected_by,omitempty"`

	ProgressPct    float64 `json:"progress_pct,omitempty"`
	ProgressStage  string  `json:"progress_stage,omitempty"`
	BytesProcessed int64   `json:"bytes_processed,omitempty"`

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
	FlowID   int    `json:"flow_id"`
	Protocol string `json:"protocol,omitempty"` // rtmp / rtp / http-flv / mpeg-ts / ps / webm

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

	RTPStats *OfflineRTPStats `json:"rtp_stats,omitempty"`

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
	Profile      *OfflineMediaProfile `json:"profile,omitempty"`
	PIDDetails   []OfflinePIDDetail   `json:"pid_details,omitempty"`
	FrameDetails []OfflineFrameDetail `json:"frame_details,omitempty"`

	PESDetails          []OfflinePESDetail `json:"pes_details,omitempty"`
	PESDetailTotal      int                `json:"pes_detail_total,omitempty"`
	PESDetailsTruncated bool               `json:"pes_details_truncated,omitempty"`
}

type OfflinePIDDetail struct {
	PID         uint16 `json:"pid"`
	StreamType  string `json:"stream_type,omitempty"`
	Codec       string `json:"codec,omitempty"`
	Category    string `json:"category,omitempty"`
	PacketCount int    `json:"packet_count,omitempty"`
	PESCount    int    `json:"pes_count,omitempty"`
	NALUCount   int    `json:"nalu_count,omitempty"`
	CCErrors    int    `json:"cc_errors,omitempty"`
	PCRCount    int    `json:"pcr_count,omitempty"`
	Bytes       int64  `json:"bytes,omitempty"`
	OutputPath  string `json:"output_path,omitempty"`
}

type OfflineBitratePoint struct {
	Sec       int     `json:"sec"`
	VideoKbps float64 `json:"video_kbps"`
	AudioKbps float64 `json:"audio_kbps"`
}

type OfflineFrameSizePoint struct {
	Index int   `json:"index"`
	DTS   int64 `json:"dts"`
	Bytes int   `json:"bytes"`
	Key   bool  `json:"key,omitempty"`
}

type OfflineAVSyncPoint struct {
	Sec    int   `json:"sec"`
	DiffMS int64 `json:"diff_ms"` // audio DTS - video DTS
}

// OfflineMediaProfile is the structured readout for one parsed flow: codec
// parameters, timeline health and downsampled series for charts.
type OfflineMediaProfile struct {
	VideoCodec string `json:"video_codec,omitempty"`
	AudioCodec string `json:"audio_codec,omitempty"`

	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	Profile   string  `json:"profile,omitempty"`
	Level     int     `json:"level,omitempty"`
	LevelName string  `json:"level_name,omitempty"`
	FrameRate float64 `json:"frame_rate,omitempty"`

	AudioSampleRate int `json:"audio_sample_rate,omitempty"`
	AudioChannels   int `json:"audio_channels,omitempty"`

	DurationMS  int64 `json:"duration_ms,omitempty"`
	VideoFrames int   `json:"video_frames,omitempty"`
	AudioFrames int   `json:"audio_frames,omitempty"`
	Keyframes   int   `json:"keyframes,omitempty"`
	BFrames     int   `json:"b_frames,omitempty"`
	HasBFrames  bool  `json:"has_b_frames,omitempty"`

	VideoBytes       int64   `json:"video_bytes,omitempty"`
	AudioBytes       int64   `json:"audio_bytes,omitempty"`
	VideoBitrateKbps float64 `json:"video_bitrate_kbps,omitempty"`
	AudioBitrateKbps float64 `json:"audio_bitrate_kbps,omitempty"`

	VideoDeltaMinMS int64 `json:"video_delta_min_ms,omitempty"`
	VideoDeltaAvgMS int64 `json:"video_delta_avg_ms,omitempty"`
	VideoDeltaMaxMS int64 `json:"video_delta_max_ms,omitempty"`

	GOPMinMS       int64   `json:"gop_min_ms,omitempty"`
	GOPAvgMS       int64   `json:"gop_avg_ms,omitempty"`
	GOPMaxMS       int64   `json:"gop_max_ms,omitempty"`
	GOPIntervalsMS []int64 `json:"gop_intervals_ms,omitempty"`

	DTSRegressions int   `json:"dts_regressions,omitempty"`
	LargeGaps      int   `json:"large_gaps,omitempty"`
	MaxGapMS       int64 `json:"max_gap_ms,omitempty"`
	AudioLargeGaps int   `json:"audio_large_gaps,omitempty"`

	HasAVOffset     bool  `json:"has_av_offset,omitempty"`
	AVStartOffsetMS int64 `json:"av_start_offset_ms,omitempty"`
	AVDriftMaxMS    int64 `json:"av_drift_max_ms,omitempty"`

	MP4FastStart  *bool `json:"mp4_fast_start,omitempty"`
	MP4Fragmented bool  `json:"mp4_fragmented,omitempty"`

	TSPacketCount int   `json:"ts_packet_count,omitempty"`
	TSSyncErrors  int   `json:"ts_sync_errors,omitempty"`
	TSTEIErrors   int   `json:"ts_tei_errors,omitempty"`
	TSCCErrors    int   `json:"ts_cc_errors,omitempty"`
	PCRCount      int   `json:"pcr_count,omitempty"`
	PCRMaxGapMS   int64 `json:"pcr_max_gap_ms,omitempty"`

	BitrateTimeline  []OfflineBitratePoint   `json:"bitrate_timeline,omitempty"`
	VideoDeltaSeries []int64                 `json:"video_delta_series,omitempty"`
	FrameSizeSeries  []OfflineFrameSizePoint `json:"frame_size_series,omitempty"`
	AVSyncTimeline   []OfflineAVSyncPoint    `json:"av_sync_timeline,omitempty"`
	NALUHistogram    map[string]int          `json:"nalu_histogram,omitempty"`

	FrameTotal      int  `json:"frame_total,omitempty"`
	FramesTruncated bool `json:"frames_truncated,omitempty"`

	Peak200msKbps  float64              `json:"peak_200ms_kbps,omitempty"`
	PeakToAvgRatio float64              `json:"peak_to_avg_ratio,omitempty"`
	HealthChecks   []OfflineHealthCheck `json:"health_checks,omitempty"`

	Warnings []string `json:"warnings,omitempty"`
}

// OfflineHealthCheck represents one structured rule result in the deep media health report.
type OfflineHealthCheck struct {
	Code            string `json:"code"`
	Category        string `json:"category"` // codec / timeline / bitrate / audio / container / transport
	Severity        string `json:"severity"` // ok / warn / error
	Title           string `json:"title"`
	Detail          string `json:"detail"`
	Count           int    `json:"count,omitempty"`
	FirstFrameIndex *int   `json:"first_frame_index,omitempty"` // 0-based index into FrameDetails
	FirstDTSMS      int64  `json:"first_dts_ms,omitempty"`
}

type OfflineFrameDetail struct {
	MediaType    string `json:"media_type,omitempty"`     // video/audio
	DTS          int64  `json:"dts,omitempty"`
	PTS          int64  `json:"pts,omitempty"`
	CTS          int64  `json:"cts,omitempty"`
	FrameLen     int    `json:"frame_len,omitempty"`
	FrameType    string `json:"frame_type,omitempty"`     // I/P/B/- for audio
	StreamSource string `json:"stream_source,omitempty"`  // "video" | "audio" | "input" | "pid:<n>"
	StreamOffset int64  `json:"stream_offset,omitempty"`
	StreamLen    int    `json:"stream_len,omitempty"`
	NALULenSize  int    `json:"nalu_len_size,omitempty"`  // 0 = Annex B / raw audio; 1..4 = AVCC/HVCC
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
	StreamOffset  int64              `json:"stream_offset,omitempty"`
	StreamLen     int                `json:"stream_len,omitempty"`
	FrameIndex    *int               `json:"frame_index,omitempty"`
}

type OfflineNALUBrief struct {
	Index    int    `json:"index"`
	Codec    string `json:"codec,omitempty"` // H264 / H265
	Type     int    `json:"type,omitempty"`
	TypeName string `json:"type_name,omitempty"`
	Len      int    `json:"len,omitempty"`
	Key      bool   `json:"key,omitempty"`
}

// OfflineRTPStats holds RFC 3550 RTP/RTCP statistics for one SSRC flow extracted from PCAP.
type OfflineRTPStats struct {
	SSRC           uint32  `json:"ssrc"`
	PayloadType    uint8   `json:"payload_type"`
	PayloadName    string  `json:"payload_name,omitempty"`
	ClockRate      uint32  `json:"clock_rate,omitempty"`
	Packets        int     `json:"packets"`
	Expected       int     `json:"expected"`
	Lost           int     `json:"lost"`
	LossRatePct    float64 `json:"loss_rate_pct"`
	OutOfOrder     int     `json:"out_of_order"`
	Duplicates     int     `json:"duplicates"`
	JitterMS       float64 `json:"jitter_ms"`
	MaxJitterMS    float64 `json:"max_jitter_ms"`
	Bytes          int64   `json:"bytes"`
	FirstSeq       uint16  `json:"first_seq"`
	LastSeq        uint16  `json:"last_seq"`
	MarkerCount    int     `json:"marker_count"`
	RTCPCount      int     `json:"rtcp_count,omitempty"`
	RTCPSenderPkts uint32  `json:"rtcp_sender_packets,omitempty"`
	RTCPFraction   float64 `json:"rtcp_fraction_lost_pct,omitempty"`
}

// OfflineTimeRange describes one detected event interval in seconds.
type OfflineTimeRange struct {
	StartSec    float64 `json:"start_sec"`
	EndSec      float64 `json:"end_sec"`
	DurationSec float64 `json:"duration_sec"`
	Detail      string  `json:"detail,omitempty"`
}

// OfflineDecodeAudit holds FFmpeg decode/black/freeze/silence audit output.
type OfflineDecodeAudit struct {
	TaskID         string             `json:"task_id"`
	FlowIndex      int                `json:"flow_index"`
	InputPath      string             `json:"input_path"`
	DurationMS     int64              `json:"duration_ms"`
	DecodeErrors   int                `json:"decode_errors"`
	ErrorSamples   []string           `json:"error_samples,omitempty"`
	BlackFrames    []OfflineTimeRange `json:"black_frames,omitempty"`
	FreezeFrames   []OfflineTimeRange `json:"freeze_frames,omitempty"`
	SilenceRanges  []OfflineTimeRange `json:"silence_ranges,omitempty"`
	Warnings       []string           `json:"warnings,omitempty"`
	GeneratedAt    time.Time          `json:"generated_at"`
}

// OfflineDiffRow is one field-level comparison entry between Task A and Task B.
type OfflineDiffRow struct {
	Category string `json:"category"`
	Label    string `json:"label"`
	ValueA   string `json:"value_a"`
	ValueB   string `json:"value_b"`
	Changed  bool   `json:"changed"`
	Severity string `json:"severity,omitempty"` // "" | "warn" | "error"
}

// OfflineCompareSide summarizes one side of an A/B offline comparison.
type OfflineCompareSide struct {
	TaskID     string               `json:"task_id"`
	InputName  string               `json:"input_name"`
	InputSize  int64                `json:"input_size"`
	Mode       OfflineMode          `json:"mode"`
	FlowIndex  int                  `json:"flow_index"`
	FlowLabel  string               `json:"flow_label"`
	VideoCodec string               `json:"video_codec,omitempty"`
	AudioCodec string               `json:"audio_codec,omitempty"`
	Profile    *OfflineMediaProfile `json:"profile,omitempty"`
}

// OfflineCompareResult holds the full side-by-side comparison of two offline tasks.
type OfflineCompareResult struct {
	SideA         OfflineCompareSide `json:"side_a"`
	SideB         OfflineCompareSide `json:"side_b"`
	Rows          []OfflineDiffRow   `json:"rows"`
	SummaryNotes  []string           `json:"summary_notes,omitempty"`
	ChangedCount  int                `json:"changed_count"`
}

