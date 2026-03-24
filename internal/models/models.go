package models

import "time"

type Task struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Type      string    `json:"type"`   // rtmp or http-flv
	Status    string    `json:"status"` // running, stopped, error
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
	FrameType      string    `json:"frame_type"`      // I/P/B frame
	IsKeyFrame     bool      `json:"is_key_frame"`    // is I frame
	IFrameInterval int64     `json:"iframe_interval"` // I帧间隔(ms)
	GOPSize        int       `json:"gop_size"`        // 当前GOP中的帧数
	RecordedAt     time.Time `json:"recorded_at"`
	MetadataJSON   string    `json:"metadata_json"`
}

type TaskRequest struct {
	URL  string `json:"url" binding:"required"`
	Type string `json:"type" binding:"required"` // rtmp or http-flv
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
	AudioDTS        []int64       `json:"audio_dts"`
	VideoLens       []int64       `json:"video_lens"`
	AudioLens       []int64       `json:"audio_lens"`
	IFrameIntervals []int64       `json:"iframe_intervals"`
	FrameTypes      []string      `json:"frame_types"`
	GOPSizes        []int         `json:"gop_sizes"`
	SecondStats     []*SecondStat `json:"second_stats"`
	VideoFrameRate  float64       `json:"video_frame_rate"`
	MetadataJSON    string        `json:"metadata_json"`
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
