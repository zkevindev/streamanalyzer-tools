package diagnostic

import "time"

const (
	investigationSchemaVersion = 4
	maxStallInvestigations     = 8
	maxDerivedClipBytes        = 12 << 20
	maxDerivedImageBytes       = 512 << 10
	maxInvestigationBytes      = 2 << 20
	investigationName          = "stall-evidence.json"
)

type StallInvestigationRequest struct {
	StallIndex    int    `json:"stall_index"` // One-based index in the finalized player report.
	Retry         bool   `json:"retry,omitempty"`
	IntervalKind  string `json:"interval_kind,omitempty"`
	IntervalIndex int    `json:"interval_index,omitempty"`
}

type ReceptionGap struct {
	Kind       string `json:"kind"`
	StartMS    int64  `json:"start_ms"`
	EndMS      int64  `json:"end_ms"`
	DurationMS int64  `json:"duration_ms"`
	Before     *Event `json:"before,omitempty"`
	After      *Event `json:"after,omitempty"`
}

type StallCorrelation struct {
	Source                string                    `json:"source"`
	WindowStartMS         int64                     `json:"window_start_ms"`
	WindowEndMS           int64                     `json:"window_end_ms"`
	BufferSamples         []PlayerBufferSample      `json:"buffer_samples"`
	Gaps                  []ReceptionGap            `json:"gaps"`
	Keyframes             []Event                   `json:"keyframes"`
	Requests              []HTTPRequestTiming       `json:"requests,omitempty"`
	PlayerRequests        []PlayerNetworkRequest    `json:"player_requests,omitempty"`
	PlayerNetworkRecorded bool                      `json:"player_network_recorded,omitempty"`
	Scheduling            *PlayerSchedulingReport   `json:"scheduling,omitempty"`
	Presentation          *PlayerPresentationReport `json:"presentation,omitempty"`
	Findings              []Finding                 `json:"findings"`
	Notes                 []string                  `json:"notes"`
}

type DerivedArtifact struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // clip or screenshot, decoded from the captured source.
	Artifact
	MediaTimeMS *int64 `json:"media_time_ms,omitempty"`
	DurationMS  *int64 `json:"duration_ms,omitempty"`
}

type InvestigationMedia struct {
	Status    string             `json:"status"` // ready, partial, unavailable, failed
	Reason    string             `json:"reason,omitempty"`
	Alignment string             `json:"alignment,omitempty"`
	StartMS   *int64             `json:"start_ms,omitempty"`
	EndMS     *int64             `json:"end_ms,omitempty"`
	Assets    []DerivedArtifact  `json:"assets"`
	RTSP      *RTSPMediaEvidence `json:"rtsp,omitempty"`
}

// RTSPMediaEvidence records the deterministic offline reconstruction recipe.
// It binds source identities and a saved player sample, not decoded pixels or
// source A/V synchronisation. The two origins use the receiver RTP clock;
// AnchorCurrentMS + PreviewOriginMS - MediaOriginMS locates the sample in the
// reconstruction-relative media timeline.
type RTSPMediaEvidence struct {
	Version               int      `json:"version"`
	SourceArtifact        Artifact `json:"source_artifact"`
	TimelineArtifact      Artifact `json:"timeline_artifact"`
	PlayerArtifact        Artifact `json:"player_artifact"`
	PlayerRevision        int      `json:"player_revision"`
	VideoTrackID          int      `json:"video_track_id,omitempty"`
	AudioTrackID          int      `json:"audio_track_id,omitempty"`
	VideoCodec            string   `json:"video_codec,omitempty"`
	AudioCodec            string   `json:"audio_codec,omitempty"`
	AudioTransform        string   `json:"audio_transform,omitempty"` // native_remux or g711_to_pcm before final AAC encoding.
	AnchorSampleOffsetMS  int64    `json:"anchor_sample_offset_ms"`   // Saved sample's capture-relative offset.
	AnchorCurrentMS       int64    `json:"anchor_current_ms"`         // Saved browser media currentTime, in ms.
	MediaOriginMS         int64    `json:"media_origin_ms"`           // Reconstruction seek origin: earliest presentation PTS / format.start_time.
	PreviewOriginMS       int64    `json:"preview_origin_ms"`         // Live preview's earliest media DTS, before browser normalisation.
	DurationMS            int64    `json:"duration_ms"`               // Deterministic reconstruction extent, in ms.
	PacketCount           int      `json:"packet_count"`              // Selected RTP packets examined, not decoded frames.
	VideoAccessUnits      int      `json:"video_access_units"`        // Complete video AUs written into the reconstruction.
	AudioAccessUnits      int      `json:"audio_access_units"`        // Complete audio AUs, or G.711 PCM blocks, written into it.
	DroppedPackets        int      `json:"dropped_packets"`           // Selected RTP packets not used by the reconstruction.
	IncompleteAccessUnits int      `json:"incomplete_access_units"`   // Partial media AUs discarded rather than repaired.
	Keyframes             []Event  `json:"keyframes"`                 // At most 16 actual random-access AUs; raw ref identifies their completion RTP packet.
	RemuxArtifact         Artifact `json:"remux_artifact"`            // Hash/bytes of deterministic temporary fMP4, not an exported raw stream.
}

type StallInvestigation struct {
	ID             string             `json:"id"`
	StallIndex     int                `json:"stall_index"`
	IntervalKind   string             `json:"interval_kind,omitempty"`
	PlayerRevision int                `json:"player_revision"`
	Status         string             `json:"status"` // queued, running, complete, partial, failed, canceled
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
	Stall          PlayerStall        `json:"stall"`
	Correlation    StallCorrelation   `json:"correlation"`
	Media          InvestigationMedia `json:"media"`
	ModelState     string             `json:"model_state"` // pending, complete, unconfigured, failed, changed
	ModelError     string             `json:"model_error,omitempty"`
	Explanation    *Explanation       `json:"explanation,omitempty"`
	Assessment     *StallAssessment   `json:"assessment,omitempty"`
}

type investigationLedger struct {
	Version int                  `json:"version"`
	Items   []StallInvestigation `json:"items"`
}
