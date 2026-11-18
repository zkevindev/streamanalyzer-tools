package diagnostic

import (
	"fmt"
	"math"
	"time"
)

const (
	maxPlayerBytes         = 512 << 10
	maxPlayerEvents        = 3000
	maxPlayerSamples       = 1401
	maxPlayerElapsedMS     = (maxSeconds + 30) * 1000
	playerStallThresholdMS = 500
)

type PlayerStart struct {
	Token     string `json:"token"`
	Transport string `json:"transport"`
	Source    string `json:"source"`
	URL       string `json:"url"`
}

type PlayerEvent struct {
	AtMS   int64  `json:"at_ms"`
	Kind   string `json:"kind"`
	Method string `json:"method,omitempty"`
	Code   string `json:"code,omitempty"`
}

type PlayerSample struct {
	AtMS       int64 `json:"at_ms"`
	CurrentMS  int64 `json:"current_ms"`
	BufferMS   int64 `json:"buffer_ms"`
	ReadyState int   `json:"ready_state"`
	Paused     bool  `json:"paused"`
	Seeking    bool  `json:"seeking"`
	Hidden     bool  `json:"hidden"`
}

type PlayerRecording struct {
	Version   int            `json:"version"`
	Revision  int            `json:"revision"`
	StartedAt time.Time      `json:"started_at"`
	ElapsedMS int64          `json:"elapsed_ms"`
	Complete  bool           `json:"complete"`
	Engine    string         `json:"engine"`
	Source    string         `json:"source"`
	Events    []PlayerEvent  `json:"events"`
	Samples   []PlayerSample `json:"samples"`
}

type PlayerBufferSample struct {
	OffsetMS   int64 `json:"offset_ms"`
	CurrentMS  int64 `json:"current_ms"`
	BufferMS   int64 `json:"buffer_ms"`
	ReadyState int   `json:"ready_state"`
	Paused     bool  `json:"paused"`
	Seeking    bool  `json:"seeking"`
	Hidden     bool  `json:"hidden"`
}

type PlayerStall struct {
	StartMS    int64 `json:"start_ms"`
	EndMS      int64 `json:"end_ms"`
	DurationMS int64 `json:"duration_ms"`
}

type PlayerReport struct {
	State             string               `json:"state"`
	Revision          int                  `json:"revision"`
	Engine            string               `json:"engine"`
	Source            string               `json:"source"`
	StartOffsetMS     int64                `json:"start_offset_ms"`
	ElapsedMS         int64                `json:"elapsed_ms"`
	FirstScreenMS     *int64               `json:"first_screen_ms,omitempty"`
	FirstPlayingMS    *int64               `json:"first_playing_ms,omitempty"`
	FirstScreenMethod string               `json:"first_screen_method,omitempty"`
	StallCount        int                  `json:"stall_count"`
	TotalStallMS      int64                `json:"total_stall_ms"`
	MaxStallMS        int64                `json:"max_stall_ms"`
	ActiveMS          int64                `json:"active_ms"`
	StallRatio        float64              `json:"stall_ratio"`
	MinBufferMS       *int64               `json:"min_buffer_ms,omitempty"`
	MaxBufferMS       int64                `json:"max_buffer_ms"`
	StallThresholdMS  int64                `json:"stall_threshold_ms"`
	BufferSamples     []PlayerBufferSample `json:"buffer_samples"`
	Stalls            []PlayerStall        `json:"stalls"`
	Events            []PlayerEvent        `json:"events"`
}

func validatePlayerRecording(record PlayerRecording, reference time.Time) error {
	invalid := func() error {
		return fmt.Errorf("%w: 播放器记录的事件、时钟或缓冲区数据无效", ErrInvalid)
	}
	if record.Version != 1 || record.Revision < 0 || record.StartedAt.IsZero() ||
		record.ElapsedMS < 0 || record.ElapsedMS > maxPlayerElapsedMS ||
		len(record.Events) > maxPlayerEvents || len(record.Samples) > maxPlayerSamples {
		return invalid()
	}
	if !reference.IsZero() && math.Abs(float64(record.StartedAt.Sub(reference).Milliseconds())) > 60_000 {
		return invalid()
	}
	switch record.Source {
	case "same_capture", "independent_pull":
	default:
		return invalid()
	}
	switch record.Engine {
	case "", "mpegtsjs", "native-hls", "hlsjs":
	default:
		return invalid()
	}
	last := int64(0)
	for _, event := range record.Events {
		if event.AtMS < last || event.AtMS > record.ElapsedMS {
			return invalid()
		}
		last = event.AtMS
		switch event.Kind {
		case "start", "playing", "waiting", "pause", "seeking", "seeked", "ended", "error", "hidden", "visible", "first_frame", "autoplay_blocked", "unsupported", "stop":
		default:
			return invalid()
		}
		switch event.Method {
		case "", "video_frame_callback", "loadeddata_estimate":
		default:
			return invalid()
		}
		if event.Kind == "first_frame" && event.Method == "" {
			return invalid()
		}
		switch event.Code {
		case "", "blocked", "unsupported", "media_error", "network_error", "no_stream", "stopped":
		default:
			return invalid()
		}
	}
	last = 0
	for _, sample := range record.Samples {
		if sample.AtMS < last || sample.AtMS > record.ElapsedMS || sample.CurrentMS < 0 ||
			sample.CurrentMS > 1_000_000_000_000 || sample.BufferMS < 0 || sample.BufferMS > maxPlayerElapsedMS ||
			sample.ReadyState < 0 || sample.ReadyState > 4 {
			return invalid()
		}
		last = sample.AtMS
	}
	return nil
}

func analyzePlayer(record PlayerRecording, observation Observation) *PlayerReport {
	offset := record.StartedAt.Sub(observation.StartedAt).Milliseconds()
	result := &PlayerReport{
		State: "recording", Revision: record.Revision, Engine: record.Engine, Source: record.Source,
		StartOffsetMS: offset, ElapsedMS: record.ElapsedMS, StallThresholdMS: playerStallThresholdMS,
		BufferSamples: []PlayerBufferSample{}, Stalls: []PlayerStall{}, Events: record.Events,
	}
	if record.Complete {
		result.State = "complete"
	}
	if record.Revision == 0 {
		result.State = "awaiting"
	}
	activeStart, stallStart := int64(-1), int64(-1)
	type activeSpan struct{ start, end int64 }
	var activeSpans []activeSpan
	paused, seeking, hidden, stopped := false, false, false, false
	closeStall := func(end int64) {
		if stallStart < 0 {
			return
		}
		duration := end - stallStart
		if duration >= playerStallThresholdMS {
			result.Stalls = append(result.Stalls, PlayerStall{StartMS: offset + stallStart, EndMS: offset + end, DurationMS: duration})
			result.StallCount++
			result.TotalStallMS += duration
			result.MaxStallMS = max(result.MaxStallMS, duration)
		}
		stallStart = -1
	}
	closeActive := func(end int64) {
		closeStall(end)
		if activeStart >= 0 {
			result.ActiveMS += end - activeStart
			activeSpans = append(activeSpans, activeSpan{activeStart, end})
			activeStart = -1
		}
	}
	for _, event := range record.Events {
		switch event.Kind {
		case "first_frame":
			if result.FirstScreenMS == nil {
				ms := event.AtMS
				result.FirstScreenMS, result.FirstScreenMethod = &ms, event.Method
			}
		case "playing":
			paused, seeking = false, false
			if result.State == "autoplay_blocked" || result.State == "unsupported" || result.State == "error" {
				result.State = "recording"
				if record.Complete {
					result.State = "complete"
				}
			}
			if result.FirstPlayingMS == nil {
				ms := event.AtMS
				result.FirstPlayingMS = &ms
			}
			closeStall(event.AtMS)
			if activeStart < 0 && !hidden && !stopped {
				activeStart = event.AtMS
			}
		case "waiting":
			if activeStart >= 0 && !paused && !seeking && !hidden && !stopped && stallStart < 0 {
				stallStart = event.AtMS
			}
		case "pause":
			paused = true
			closeActive(event.AtMS)
		case "seeking":
			seeking = true
			closeActive(event.AtMS)
		case "seeked":
			// A following playing event establishes resumed playback.
			seeking = false
		case "hidden":
			hidden = true
			closeActive(event.AtMS)
		case "visible":
			hidden = false
		case "autoplay_blocked", "unsupported":
			result.State = event.Kind
			closeActive(event.AtMS)
		case "error":
			result.State = "error"
			closeActive(event.AtMS)
		case "ended", "stop":
			stopped = true
			closeActive(event.AtMS)
		}
	}
	closeActive(record.ElapsedMS)
	if result.ActiveMS > 0 {
		result.StallRatio = float64(result.TotalStallMS) / float64(result.ActiveMS)
	}
	for _, sample := range record.Samples {
		result.BufferSamples = append(result.BufferSamples, PlayerBufferSample{
			OffsetMS: offset + sample.AtMS, CurrentMS: sample.CurrentMS, BufferMS: sample.BufferMS,
			ReadyState: sample.ReadyState, Paused: sample.Paused, Seeking: sample.Seeking, Hidden: sample.Hidden,
		})
		active := false
		for _, span := range activeSpans {
			if sample.AtMS >= span.start && sample.AtMS < span.end {
				active = true
				break
			}
		}
		if active && !sample.Paused && !sample.Seeking && !sample.Hidden {
			if result.MinBufferMS == nil || sample.BufferMS < *result.MinBufferMS {
				value := sample.BufferMS
				result.MinBufferMS = &value
			}
			result.MaxBufferMS = max(result.MaxBufferMS, sample.BufferMS)
		}
	}
	return result
}
