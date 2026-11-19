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
	Status    string            `json:"status"` // ready, partial, unavailable, failed
	Reason    string            `json:"reason,omitempty"`
	Alignment string            `json:"alignment,omitempty"`
	StartMS   *int64            `json:"start_ms,omitempty"`
	EndMS     *int64            `json:"end_ms,omitempty"`
	Assets    []DerivedArtifact `json:"assets"`
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
