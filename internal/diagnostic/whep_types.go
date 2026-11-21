package diagnostic

// WHEPICEServer describes an optional STUN/TURN server configured for WHEP sampling.
type WHEPICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// WHEPSDPDigest captures bounded, credential-free SDP negotiation metadata.
type WHEPSDPDigest struct {
	VideoCodecs    []string `json:"video_codecs,omitempty"`
	AudioCodecs    []string `json:"audio_codecs,omitempty"`
	ICELite        bool     `json:"ice_lite,omitempty"`
	SetupRole      string   `json:"setup_role,omitempty"`
	CandidateCount int      `json:"candidate_count,omitempty"`
	HasVideo       bool     `json:"has_video,omitempty"`
	HasAudio       bool     `json:"has_audio,omitempty"`
}

// WHEPCandidatePair describes the active ICE candidate pair selected for media.
type WHEPCandidatePair struct {
	LocalType      string   `json:"local_type,omitempty"`      // host | srflx | prflx | relay
	LocalProtocol  string   `json:"local_protocol,omitempty"`  // udp | tcp
	RemoteType     string   `json:"remote_type,omitempty"`     // host | srflx | prflx | relay
	RemoteProtocol string   `json:"remote_protocol,omitempty"` // udp | tcp
	State          string   `json:"state,omitempty"`
	CurrentRTTMS   *float64 `json:"current_rtt_ms,omitempty"`
}

// WHEPTrackMetrics holds per-track RTP/RTCP and media timing measurements.
type WHEPTrackMetrics struct {
	ID                     int      `json:"id"`
	Kind                   string   `json:"kind"` // video | audio | other
	Codec                  string   `json:"codec,omitempty"`
	MimeType               string   `json:"mime_type,omitempty"`
	PayloadType            uint8    `json:"payload_type"`
	ClockRate              uint32   `json:"clock_rate,omitempty"`
	SSRC                   uint32   `json:"ssrc"`
	Received               bool     `json:"received"`
	Packets                int      `json:"packets"`
	Bytes                  int64    `json:"bytes"`
	BitrateKbps            float64  `json:"bitrate_kbps,omitempty"`
	FirstRTPMS             *int64   `json:"first_rtp_ms,omitempty"`
	LastRTPMS              *int64   `json:"last_rtp_ms,omitempty"`
	FirstKeyframeMS        *int64   `json:"first_keyframe_ms,omitempty"`
	Frames                 int      `json:"frames"`
	Keyframes              int      `json:"keyframes,omitempty"`
	MaxFrameGapMS          int64    `json:"max_frame_gap_ms,omitempty"`
	MaxGOPMS               int64    `json:"max_gop_ms,omitempty"`
	MaxArrivalGapMS        int64    `json:"max_arrival_gap_ms,omitempty"`
	SequenceGaps           int      `json:"sequence_gaps,omitempty"`
	MissingSequencePackets int      `json:"missing_sequence_packets,omitempty"`
	OutOfOrderPackets      int      `json:"out_of_order_packets,omitempty"`
	DuplicatePackets       int      `json:"duplicate_packets,omitempty"`
	LossRatePct            float64  `json:"loss_rate_pct,omitempty"`
	JitterMS               float64  `json:"jitter_ms,omitempty"`
	MaxJitterMS            float64  `json:"max_jitter_ms,omitempty"`
	PLICount               int      `json:"pli_count,omitempty"`
	PLIFirstRecoveryMS     *int64   `json:"pli_first_recovery_ms,omitempty"`
	RTCPSRCount            int      `json:"rtcp_sr_count,omitempty"`
	RTCPRRCount            int      `json:"rtcp_rr_count,omitempty"`
}

// WHEPMetrics summarizes the WHEP HTTP signaling, ICE/DTLS setup, and RTP/RTCP reception.
type WHEPMetrics struct {
	Boundary            string              `json:"boundary"`
	SignalingMode       string              `json:"signaling_mode,omitempty"` // client_offer | server_offer
	HTTPStatus          int                 `json:"http_status,omitempty"`
	LocationRecorded    bool                `json:"location_recorded,omitempty"`
	LocationPath        string              `json:"location_path,omitempty"`
	ETag                string              `json:"etag,omitempty"`
	LinkICEServers      []string            `json:"link_ice_servers,omitempty"`
	DeleteStatus        int                 `json:"delete_status,omitempty"`
	ICETransportPolicy  string              `json:"ice_transport_policy,omitempty"`
	ICEState            string              `json:"ice_state,omitempty"`
	DTLSState           string              `json:"dtls_state,omitempty"`
	PeerState           string              `json:"peer_state,omitempty"`
	GatherMS            *int64              `json:"gather_ms,omitempty"`
	ICEConnectMS        *int64              `json:"ice_connect_ms,omitempty"`
	DTLSConnectMS       *int64              `json:"dtls_connect_ms,omitempty"`
	FirstRTPMS          *int64              `json:"first_rtp_ms,omitempty"`
	FirstRTCPMS         *int64              `json:"first_rtcp_ms,omitempty"`
	SelectedPair        *WHEPCandidatePair  `json:"selected_pair,omitempty"`
	LocalCandidates     int                 `json:"local_candidates,omitempty"`
	RemoteCandidates    int                 `json:"remote_candidates,omitempty"`
	OfferSDP            *WHEPSDPDigest      `json:"offer_sdp,omitempty"`
	AnswerSDP           *WHEPSDPDigest      `json:"answer_sdp,omitempty"`
	Tracks              []WHEPTrackMetrics  `json:"tracks,omitempty"`
	TotalPackets        int                 `json:"total_packets"`
	TotalBytes          int64               `json:"total_bytes"`
	TruncatedPackets    int                 `json:"truncated_packets,omitempty"`
	MediaStoppedGapMS   int64               `json:"media_stopped_gap_ms,omitempty"`
}

// PlayerWebRTCStats captures browser RTCPeerConnection.getStats() metrics for WHEP playback.
type PlayerWebRTCStats struct {
	ICEState               string   `json:"ice_state,omitempty"`
	ConnectionState        string   `json:"connection_state,omitempty"`
	SelectedCandidateType  string   `json:"selected_candidate_type,omitempty"`
	SelectedRemoteType     string   `json:"selected_remote_type,omitempty"`
	SelectedProtocol       string   `json:"selected_protocol,omitempty"`
	CurrentRTTMS           *float64 `json:"current_rtt_ms,omitempty"`
	VideoCodec             string   `json:"video_codec,omitempty"`
	AudioCodec             string   `json:"audio_codec,omitempty"`
	PacketsReceived        int64    `json:"packets_received,omitempty"`
	PacketsLost            int64    `json:"packets_lost,omitempty"`
	JitterMS               float64  `json:"jitter_ms,omitempty"`
	JitterBufferAvgMS      float64  `json:"jitter_buffer_avg_ms,omitempty"`
	FramesDecoded          int64    `json:"frames_decoded,omitempty"`
	FramesDropped          int64    `json:"frames_dropped,omitempty"`
	FreezeCount            int64    `json:"freeze_count,omitempty"`
	TotalFreezesDurationMS int64    `json:"total_freezes_duration_ms,omitempty"`
	PLICount               int64    `json:"pli_count,omitempty"`
	NACKCount              int64    `json:"nack_count,omitempty"`
	FIRCount               int64    `json:"fir_count,omitempty"`
	AudioConcealedSamples  int64    `json:"audio_concealed_samples,omitempty"`
	BitrateKbps            float64  `json:"bitrate_kbps,omitempty"`
	StatsSamples           int      `json:"stats_samples,omitempty"`
}

// whepArchiveMetadata is stored as metadata.json inside stream.whep.zip for deterministic replay.
type whepArchiveMetadata struct {
	Version            int                `json:"version"`
	SignalingMode      string             `json:"signaling_mode,omitempty"`
	HTTPStatus         int                `json:"http_status,omitempty"`
	LocationRecorded   bool               `json:"location_recorded,omitempty"`
	LocationPath       string             `json:"location_path,omitempty"`
	ETag               string             `json:"etag,omitempty"`
	LinkICEServers     []string           `json:"link_ice_servers,omitempty"`
	DeleteStatus       int                `json:"delete_status,omitempty"`
	ICETransportPolicy string             `json:"ice_transport_policy,omitempty"`
	ICEState           string             `json:"ice_state,omitempty"`
	DTLSState          string             `json:"dtls_state,omitempty"`
	PeerState          string             `json:"peer_state,omitempty"`
	GatherMS           *int64             `json:"gather_ms,omitempty"`
	ICEConnectMS       *int64             `json:"ice_connect_ms,omitempty"`
	DTLSConnectMS      *int64             `json:"dtls_connect_ms,omitempty"`
	FirstRTCPMS        *int64             `json:"first_rtcp_ms,omitempty"`
	SelectedPair       *WHEPCandidatePair `json:"selected_pair,omitempty"`
	LocalCandidates    int                `json:"local_candidates,omitempty"`
	RemoteCandidates   int                `json:"remote_candidates,omitempty"`
	TruncatedPackets   int                `json:"truncated_packets,omitempty"`
	Tracks             []whepTrackMeta    `json:"tracks,omitempty"`
}

type whepTrackMeta struct {
	ID                 int    `json:"id"`
	Kind               string `json:"kind"`
	Codec              string `json:"codec"`
	MimeType           string `json:"mime_type"`
	PayloadType        uint8  `json:"payload_type"`
	ClockRate          uint32 `json:"clock_rate"`
	SSRC               uint32 `json:"ssrc"`
	PLICount           int    `json:"pli_count,omitempty"`
	PLIFirstSentMS     *int64 `json:"pli_first_sent_ms,omitempty"`
	PLIFirstRecoveryMS *int64 `json:"pli_first_recovery_ms,omitempty"`
	RTCPSRCount        int    `json:"rtcp_sr_count,omitempty"`
	RTCPRRCount        int    `json:"rtcp_rr_count,omitempty"`
}

type whepRTPIndexEntry struct {
	TrackID   int    `json:"track_id"`
	SSRC      uint32 `json:"ssrc"`
	ArrivalMS int64  `json:"arrival_ms"`
	Offset    int64  `json:"offset"`
	Size      int    `json:"size"`
}
