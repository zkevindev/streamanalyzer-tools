package diagnostic

import (
	"errors"
	"time"
)

const (
	legacyFLVSchemaVersion     = 1
	multiProtocolSchemaVersion = 2
	playerSchemaVersion        = 3
	defaultSeconds             = 60
	maxSeconds                 = 300
	maxStreamBytes             = 128 << 20
	maxTimeline                = 32 << 20
	maxTags                    = 100_000
	MaxBundleBytes             = maxStreamBytes + maxTimeline + 160<<20 + maxPlayerNetworkBytes
)

const (
	ProtocolHTTPFLV = "http-flv"
	ProtocolRTMP    = "rtmp"
	ProtocolHLS     = "hls"
	ProtocolHTTPTS  = "http-ts"
	ProtocolUDPTS   = "udp-ts"
	ProtocolRTPTS   = "rtp-ts"
	ProtocolRTSP    = "rtsp"
	ProtocolRTSPS   = "rtsps"
)

const (
	streamName     = "stream.flv"
	rtmpStreamName = "stream.rtmp"
	hlsStreamName  = "stream.hls.zip"
	tsStreamName   = "stream.ts"
	udpStreamName  = "stream.udp"
	rtpStreamName  = "stream.rtp"
	rtspStreamName = "stream.rtsp.zip"
	timelineName   = "timeline.ndjson"
	manifestName   = "manifest.json"
	reportName     = "report.json"
	htmlName       = "report.html"
	playerName     = "player.json"
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
	AutoInvestigate   bool                `json:"auto_investigate,omitempty"`
	URL               string              `json:"url"`
	Seconds           int                 `json:"seconds"`
	Protocol          string              `json:"protocol,omitempty"`
	Transport         string              `json:"transport,omitempty"`
	Playback          bool                `json:"playback,omitempty"`
	Headers           map[string]string   `json:"headers,omitempty"`
	ProxyURL          string              `json:"proxy_url,omitempty"`
	CACertificate     string              `json:"ca_certificate,omitempty"`
	ReconnectAttempts int                 `json:"reconnect_attempts,omitempty"`
	ReconnectDelayMS  int                 `json:"reconnect_delay_ms,omitempty"`
	HLSVariant        *int                `json:"hls_variant,omitempty"`
	Profile           string              `json:"profile,omitempty"`
	CustomThresholds  *AnalysisThresholds `json:"custom_thresholds,omitempty"`
	SegmentSeconds    int                 `json:"segment_seconds,omitempty"`
	RollingSegments   int                 `json:"rolling_segments,omitempty"`
	SessionGroupID    string              `json:"-"`
	SessionAttempt    int                 `json:"-"`
}

type Run struct {
	ID             string       `json:"id"`
	Mode           string       `json:"mode"`
	Protocol       string       `json:"protocol"`
	Status         string       `json:"status"`
	Origin         string       `json:"origin,omitempty"`
	CreatedAt      time.Time    `json:"created_at"`
	Error          string       `json:"error,omitempty"`
	ReportReady    bool         `json:"report_ready"`
	Protected      bool         `json:"protected"`
	SessionGroupID string       `json:"session_group_id,omitempty"`
	Player         *PlayerStart `json:"player,omitempty"`
}

type Artifact struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type Observation struct {
	Origin           string              `json:"origin,omitempty"`
	StartedAt        time.Time           `json:"started_at"`
	RequestedSeconds int                 `json:"requested_seconds"`
	ElapsedMS        int64               `json:"elapsed_ms"`
	HTTPStatus       int                 `json:"http_status,omitempty"`
	TTFBMS           *int64              `json:"ttfb_ms,omitempty"`
	EndReason        string              `json:"end_reason"`
	FirstPayloadMS   *int64              `json:"first_payload_ms,omitempty"`
	Stages           []CaptureStage      `json:"stages,omitempty"`
	Requests         []HTTPRequestTiming `json:"requests,omitempty"`
	FailureStage     string              `json:"failure_stage,omitempty"`
	FailureCode      string              `json:"failure_code,omitempty"`
}

type CaptureStage struct {
	Name    string `json:"name"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
}

// HTTPRequestTiming describes one request through its response headers, with
// optional body completion on the terminal hop. Stage offsets are relative
// to the sampling start. TTFBMS is relative to this request's own start.
type HTTPRequestTiming struct {
	Number                   int            `json:"number"`
	Kind                     string         `json:"kind,omitempty"`
	StartMS                  int64          `json:"start_ms"`
	EndMS                    int64          `json:"end_ms"`
	TotalMS                  int64          `json:"total_ms"`
	BodyEndMS                *int64         `json:"body_end_ms,omitempty"`
	HTTPStatus               int            `json:"http_status,omitempty"`
	TTFBMS                   *int64         `json:"ttfb_ms,omitempty"`
	Stages                   []CaptureStage `json:"stages,omitempty"`
	FailureStage             string         `json:"failure_stage,omitempty"`
	FailureCode              string         `json:"failure_code,omitempty"`
	Method                   string         `json:"method,omitempty"`
	HTTPProtocol             string         `json:"http_protocol,omitempty"`
	HeadersRecorded          bool           `json:"headers_recorded,omitempty"`
	RequestHeaders           []HTTPHeader   `json:"request_headers,omitempty"`
	ResponseHeaders          []HTTPHeader   `json:"response_headers,omitempty"`
	RequestHeadersTruncated  bool           `json:"request_headers_truncated,omitempty"`
	ResponseHeadersTruncated bool           `json:"response_headers_truncated,omitempty"`
}

// HTTPHeader is a normalized field, with credentials and URL query data
// removed before it is persisted. Repeated fields retain separate entries.
type HTTPHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Manifest struct {
	AutomaticInvestigation *Artifact          `json:"automatic_investigation,omitempty"`
	Version                int                `json:"version"`
	Protocol               string             `json:"protocol"`
	StreamLimitBytes       int64              `json:"stream_limit_bytes,omitempty"`
	HLSVariant             *int               `json:"hls_variant,omitempty"`
	HLSVariantBandwidth    int                `json:"hls_variant_bandwidth,omitempty"`
	AnalysisProfile        string             `json:"analysis_profile,omitempty"`
	AnalysisThresholds     AnalysisThresholds `json:"analysis_thresholds,omitempty"`
	Observation            Observation        `json:"observation"`
	Stream                 Artifact           `json:"stream"`
	Timeline               Artifact           `json:"timeline"`
	Player                 *Artifact          `json:"player,omitempty"`
	StallEvidence          *Artifact          `json:"stall_evidence,omitempty"`
	PlayerNetwork          *Artifact          `json:"player_network,omitempty"`
}

// Event locates protocol evidence in the dump. RTMP and FLV events use byte
// offsets; HLS events identify a snapshot, init segment or media object.
// ArrivalMS is relative to the start of sampling.
type Event struct {
	Offset      int64   `json:"offset"`
	Size        int     `json:"size"`
	ArrivalMS   int64   `json:"arrival_ms"`
	Kind        string  `json:"kind"`
	DTS         int64   `json:"dts_ms"`
	PTS         int64   `json:"pts_ms"`
	Codec       string  `json:"codec,omitempty"`
	Track       uint8   `json:"track,omitempty"`
	Keyframe    bool    `json:"keyframe,omitempty"`
	Artifact    string  `json:"artifact,omitempty"`
	Rendition   string  `json:"rendition,omitempty"`
	Sequence    *int    `json:"sequence,omitempty"`
	HTTPStatus  int     `json:"http_status,omitempty"`
	DurationMS  int64   `json:"duration_ms,omitempty"`
	PID         *uint16 `json:"pid,omitempty"`
	RTPSequence *uint16 `json:"rtp_sequence,omitempty"`
}

type Metrics struct {
	MediaTimingUnit string   `json:"media_timing_unit,omitempty"`
	VideoCodecs     []string `json:"video_codecs,omitempty"`
	AudioCodecs     []string `json:"audio_codecs,omitempty"`
	VideoFrames     int      `json:"video_frames"`
	AudioFrames     int      `json:"audio_frames"`
	FirstVideoMS    *int64   `json:"first_video_ms,omitempty"`
	FirstAudioMS    *int64   `json:"first_audio_ms,omitempty"`
	MaxVideoGapMS   int64    `json:"max_video_gap_ms"`
	MaxGOPMS        int64    `json:"max_gop_ms"`
	FirstKeyframeMS *int64   `json:"first_keyframe_ms,omitempty"`
	MaxAudioGapMS   int64    `json:"max_audio_gap_ms"`
}

type Finding struct {
	ID          string  `json:"id"`
	Code        string  `json:"code"`
	Severity    string  `json:"severity"`
	Message     string  `json:"message"`
	StartMS     int64   `json:"start_ms"`
	EndMS       int64   `json:"end_ms"`
	Evidence    []Event `json:"evidence,omitempty"`
	Occurrences int     `json:"occurrences,omitempty"`
	NextCheck   string  `json:"next_check,omitempty"`
}

type Report struct {
	AutomaticInvestigation *AutomaticInvestigation `json:"automatic_investigation,omitempty"`
	Assessment             *ReportAssessment       `json:"assessment,omitempty"`
	ContentQuality         *ContentQualityReport   `json:"content_quality,omitempty"`
	Throughput             *ThroughputReport       `json:"throughput,omitempty"`
	RTMPControl            *RTMPControlReport      `json:"rtmp_control,omitempty"`
	HLSABR                 *HLSABRReport           `json:"hls_abr,omitempty"`
	modelScope             string
	Requests               []HTTPRequestTiming  `json:"requests,omitempty"`
	RequestsTruncated      int                  `json:"requests_truncated,omitempty"`
	RuleVersion            string               `json:"rule_version"`
	Manifest               Manifest             `json:"manifest"`
	Metrics                Metrics              `json:"metrics"`
	HLS                    *HLSMetrics          `json:"hls,omitempty"`
	MPEGTS                 *MPEGTSMetrics       `json:"mpegts,omitempty"`
	RTSP                   *RTSPMetrics         `json:"rtsp,omitempty"`
	Findings               []Finding            `json:"findings"`
	Player                 *PlayerReport        `json:"player,omitempty"`
	PlayerNetwork          *PlayerNetworkReport `json:"player_network,omitempty"`
	StallInvestigations    []StallInvestigation `json:"stall_investigations,omitempty"`
}

type Detail struct {
	Run
	AutomaticRunning    bool         `json:"automatic_running,omitempty"`
	RTSPReplayAvailable bool         `json:"rtsp_replay_available,omitempty"`
	FLVReplayAvailable  bool         `json:"flv_replay_available,omitempty"`
	DumpName            string       `json:"dump_name,omitempty"`
	Report              *Report      `json:"report,omitempty"`
	Explanation         *Explanation `json:"explanation,omitempty"`
}
