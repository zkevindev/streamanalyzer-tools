package diagnostic

const (
	rtspArchiveVersion   = 1
	rtspWireName         = "server-wire.bin"
	rtspSDPName          = "session.sdp"
	rtspMetadataName     = "session.json"
	maxRTSPRequests      = 64
	maxRTSPConnections   = 8
	maxRTSPTracks        = 32
	maxRTSPSDPBytes      = 256 << 10
	maxRTSPMetadataBytes = 256 << 10
	maxRTSPSSRCs         = 8
	rtspSequenceWindow   = 256
)

// RTSP reports contain only controlled protocol labels and numeric evidence.
// SDP, paths, credentials, authentication fields and error text stay out.
type RTSPRequestTiming struct {
	Number       int    `json:"number"`
	ConnectionID int    `json:"connection_id"`
	Method       string `json:"method"`
	CSeq         int    `json:"cseq"`
	StartMS      int64  `json:"start_ms"`
	EndMS        int64  `json:"end_ms"`
	StatusCode   int    `json:"status_code,omitempty"`
	Complete     bool   `json:"complete"`
	FailureCode  string `json:"failure_code,omitempty"`
}

type RTSPTrackMetrics struct {
	ID                     int    `json:"id"`
	MediaID                int    `json:"media_id"`
	Kind                   string `json:"kind"`
	Codec                  string `json:"codec"`
	PayloadType            int    `json:"payload_type"`
	ClockRate              int    `json:"clock_rate"`
	Channels               int    `json:"channels,omitempty"`
	Received               bool   `json:"received"`
	RTPPackets             int    `json:"rtp_packets"`
	RTPBytes               int64  `json:"rtp_bytes"`
	FirstRTPMS             *int64 `json:"first_rtp_ms,omitempty"`
	LastRTPMS              *int64 `json:"last_rtp_ms,omitempty"`
	MaxArrivalGapMS        int64  `json:"max_arrival_gap_ms"`
	SequenceGaps           int    `json:"sequence_gaps"`
	MissingSequencePackets int    `json:"missing_sequence_packets"`
	DuplicatePackets       int    `json:"duplicate_packets"`
	OutOfOrderPackets      int    `json:"out_of_order_packets"`
	SSRCChanges            int    `json:"ssrc_changes"`
	SequenceResets         int    `json:"sequence_resets"`
	TimestampRegressions   int    `json:"timestamp_regressions"`
	AnalysisLimited        bool   `json:"analysis_limited,omitempty"`
}

type RTSPMetrics struct {
	Boundary              string              `json:"boundary"`
	Transport             string              `json:"transport"`
	Secure                bool                `json:"secure"`
	SDPAvailable          bool                `json:"sdp_available"`
	Connections           int                 `json:"connections"`
	RTPPackets            int                 `json:"rtp_packets"`
	RTCPPackets           int                 `json:"rtcp_packets"`
	RTPBytes              int64               `json:"rtp_bytes"`
	MalformedPackets      int                 `json:"malformed_packets"`
	UnknownChannelPackets int                 `json:"unknown_channel_packets"`
	UnknownPayloadPackets int                 `json:"unknown_payload_packets"`
	TrailingBytes         int64               `json:"trailing_bytes"`
	FirstRTPMS            *int64              `json:"first_rtp_ms,omitempty"`
	FirstVideoRTPMS       *int64              `json:"first_video_rtp_ms,omitempty"`
	FirstAudioRTPMS       *int64              `json:"first_audio_rtp_ms,omitempty"`
	Requests              []RTSPRequestTiming `json:"requests"`
	RequestsTruncated     int                 `json:"requests_truncated"`
	Tracks                []RTSPTrackMetrics  `json:"tracks"`
	AnalysisLimited       bool                `json:"analysis_limited,omitempty"`
}

type rtspWireSpan struct {
	Offset       int64 `json:"offset"`
	Size         int   `json:"size"`
	ArrivalMS    int64 `json:"arrival_ms"`
	ConnectionID int   `json:"connection_id"`
}

type rtspChannel struct {
	ConnectionID int `json:"connection_id"`
	MediaID      int `json:"media_id"`
	RTPChannel   int `json:"rtp_channel"`
	RTCPChannel  int `json:"rtcp_channel"`
}

type rtspArchiveMetadata struct {
	Version           int                 `json:"version"`
	Connections       int                 `json:"connections"`
	SDPConnectionID   int                 `json:"sdp_connection_id"`
	Channels          []rtspChannel       `json:"channels"`
	Requests          []RTSPRequestTiming `json:"requests"`
	RequestsTruncated int                 `json:"requests_truncated"`
}
