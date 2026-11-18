package diagnostic

import (
	"errors"
	"time"
)

const (
	legacyFLVSchemaVersion     = 1
	multiProtocolSchemaVersion = 2
	defaultSeconds             = 60
	maxSeconds                 = 300
	maxStreamBytes             = 128 << 20
	maxTimeline                = 32 << 20
	maxTags                    = 100_000
	MaxBundleBytes             = maxStreamBytes + maxTimeline + 2<<20
)

const (
	ProtocolHTTPFLV = "http-flv"
	ProtocolRTMP    = "rtmp"
	ProtocolHLS     = "hls"
)

const (
	streamName     = "stream.flv"
	rtmpStreamName = "stream.rtmp"
	hlsStreamName  = "stream.hls.zip"
	timelineName   = "timeline.ndjson"
	manifestName   = "manifest.json"
	reportName     = "report.json"
	htmlName       = "report.html"
)

var (
	ErrInvalid      = errors.New("invalid diagnostic input")
	ErrNotFound     = errors.New("diagnostic run not found")
	ErrPending      = errors.New("diagnostic report is not available")
	ErrBusy         = errors.New("diagnostic worker limit reached")
	ErrUnconfigured = errors.New("model provider is not configured")
	ErrProvider     = errors.New("model provider request failed")
)

type SampleRequest struct {
	URL      string `json:"url"`
	Seconds  int    `json:"seconds"`
	Protocol string `json:"protocol,omitempty"`
}

type Run struct {
	ID          string    `json:"id"`
	Mode        string    `json:"mode"`
	Protocol    string    `json:"protocol"`
	Status      string    `json:"status"`
	Origin      string    `json:"origin,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	Error       string    `json:"error,omitempty"`
	ReportReady bool      `json:"report_ready"`
}

type Artifact struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type Observation struct {
	Origin           string    `json:"origin,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	RequestedSeconds int       `json:"requested_seconds"`
	ElapsedMS        int64     `json:"elapsed_ms"`
	HTTPStatus       int       `json:"http_status,omitempty"`
	TTFBMS           *int64    `json:"ttfb_ms,omitempty"`
	EndReason        string    `json:"end_reason"`
}

type Manifest struct {
	Version     int         `json:"version"`
	Protocol    string      `json:"protocol"`
	Observation Observation `json:"observation"`
	Stream      Artifact    `json:"stream"`
	Timeline    Artifact    `json:"timeline"`
}

// Event locates protocol evidence in the dump. RTMP and FLV events use byte
// offsets; HLS events identify a snapshot, init segment or media object.
// ArrivalMS is relative to the start of sampling.
type Event struct {
	Offset     int64  `json:"offset"`
	Size       int    `json:"size"`
	ArrivalMS  int64  `json:"arrival_ms"`
	Kind       string `json:"kind"`
	DTS        int64  `json:"dts_ms"`
	PTS        int64  `json:"pts_ms"`
	Codec      string `json:"codec,omitempty"`
	Track      uint8  `json:"track,omitempty"`
	Keyframe   bool   `json:"keyframe,omitempty"`
	Artifact   string `json:"artifact,omitempty"`
	Rendition  string `json:"rendition,omitempty"`
	Sequence   *int   `json:"sequence,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

type Metrics struct {
	VideoCodecs   []string `json:"video_codecs,omitempty"`
	AudioCodecs   []string `json:"audio_codecs,omitempty"`
	VideoFrames   int      `json:"video_frames"`
	AudioFrames   int      `json:"audio_frames"`
	FirstVideoMS  *int64   `json:"first_video_ms,omitempty"`
	FirstAudioMS  *int64   `json:"first_audio_ms,omitempty"`
	MaxVideoGapMS int64    `json:"max_video_gap_ms"`
	MaxGOPMS      int64    `json:"max_gop_ms"`
}

type Finding struct {
	ID       string  `json:"id"`
	Code     string  `json:"code"`
	Severity string  `json:"severity"`
	Message  string  `json:"message"`
	StartMS  int64   `json:"start_ms"`
	EndMS    int64   `json:"end_ms"`
	Evidence []Event `json:"evidence,omitempty"`
}

type Report struct {
	RuleVersion string      `json:"rule_version"`
	Manifest    Manifest    `json:"manifest"`
	Metrics     Metrics     `json:"metrics"`
	HLS         *HLSMetrics `json:"hls,omitempty"`
	Findings    []Finding   `json:"findings"`
}

type Detail struct {
	Run
	DumpName    string       `json:"dump_name,omitempty"`
	Report      *Report      `json:"report,omitempty"`
	Explanation *Explanation `json:"explanation,omitempty"`
}
