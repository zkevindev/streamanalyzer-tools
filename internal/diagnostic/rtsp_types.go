package diagnostic

const (
	rtspArchiveVersion   = 1
	rtspWireName         = "server-wire.bin"
	rtspSDPName          = "session.sdp"
	rtspMetadataName     = "session.json"
	rtspUDPDataName      = "udp-datagrams.bin"
	rtspUDPIndexName     = "udp-index.ndjson"
	maxRTSPUDPSockets    = 128
	maxRTSPUDPIndexBytes = 32 << 20
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

type RTSPUDPSourceEvidence struct {
	MediaID         int    `json:"media_id"`
	NegotiatedPort  int    `json:"negotiated_port"`
	ObservedPort    int    `json:"observed_port"`
	SameIP          bool   `json:"same_ip"`
	FilteredPackets int    `json:"filtered_packets"`
	FirstMS         int64  `json:"first_ms"`
	LastMS          int64  `json:"last_ms"`
	RestoredMS      *int64 `json:"restored_ms,omitempty"`
}

type RTSPMetrics struct {
	UDPSourceEvidence     []RTSPUDPSourceEvidence `json:"udp_source_evidence,omitempty"`
	SenderClocks          []RTSPSenderClock       `json:"sender_clocks,omitempty"`
	Preview               *RTSPPreviewReport      `json:"preview,omitempty"`
	Boundary              string                  `json:"boundary"`
	Transport             string                  `json:"transport"`
	Secure                bool                    `json:"secure"`
	SDPAvailable          bool                    `json:"sdp_available"`
	Connections           int                     `json:"connections"`
	RTPPackets            int                     `json:"rtp_packets"`
	RTCPPackets           int                     `json:"rtcp_packets"`
	RTPBytes              int64                   `json:"rtp_bytes"`
	MalformedPackets      int                     `json:"malformed_packets"`
	UnknownChannelPackets int                     `json:"unknown_channel_packets"`
	UnknownPayloadPackets int                     `json:"unknown_payload_packets"`
	TrailingBytes         int64                   `json:"trailing_bytes"`
	FirstRTPMS            *int64                  `json:"first_rtp_ms,omitempty"`
	FirstVideoRTPMS       *int64                  `json:"first_video_rtp_ms,omitempty"`
	FirstAudioRTPMS       *int64                  `json:"first_audio_rtp_ms,omitempty"`
	Requests              []RTSPRequestTiming     `json:"requests"`
	RequestsTruncated     int                     `json:"requests_truncated"`
	Tracks                []RTSPTrackMetrics      `json:"tracks"`
	AnalysisLimited       bool                    `json:"analysis_limited,omitempty"`
	RequestedTransport    string                  `json:"requested_transport,omitempty"`
	UDPDatagrams          int                     `json:"udp_datagrams,omitempty"`
	UDPBytes              int64                   `json:"udp_bytes,omitempty"`
	AcceptedDatagrams     int                     `json:"accepted_datagrams,omitempty"`
	FilteredDatagrams     int                     `json:"filtered_datagrams,omitempty"`
	UDPOversizeDatagrams  int                     `json:"udp_oversize_datagrams,omitempty"`
	FirstRTCPMS           *int64                  `json:"first_rtcp_ms,omitempty"`
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
	Preview           *RTSPPreviewReport  `json:"preview,omitempty"`
	Version           int                 `json:"version"`
	Connections       int                 `json:"connections"`
	SDPConnectionID   int                 `json:"sdp_connection_id"`
	Channels          []rtspChannel       `json:"channels"`
	Requests          []RTSPRequestTiming `json:"requests"`
	RequestsTruncated int                 `json:"requests_truncated"`
	Transport         string              `json:"transport,omitempty"`
	ControlPeerIP     string              `json:"control_peer_ip,omitempty"`
	UDPSockets        []rtspUDPSocket     `json:"udp_sockets,omitempty"`
	UDPTransports     []rtspUDPTransport  `json:"udp_transports,omitempty"`
}

type rtspUDPSocket struct {
	ID                int `json:"id"`
	LocalPort         int `json:"local_port"`
	ReaderBufferBytes int `json:"reader_buffer_bytes"`
}

type rtspUDPTransport struct {
	MediaID        int    `json:"media_id"`
	RTPSocketID    int    `json:"rtp_socket_id"`
	RTCPSocketID   int    `json:"rtcp_socket_id"`
	SourceIP       string `json:"source_ip"`
	RTPServerPort  int    `json:"rtp_server_port"`
	RTCPServerPort int    `json:"rtcp_server_port"`
	CSeq           int    `json:"cseq"`
}

type rtspUDPSpan struct {
	Offset     int64  `json:"offset"`
	Size       int    `json:"size"`
	ArrivalMS  int64  `json:"arrival_ms"`
	SocketID   int    `json:"socket_id"`
	SourceIP   string `json:"source_ip"`
	SourcePort int    `json:"source_port"`
}
