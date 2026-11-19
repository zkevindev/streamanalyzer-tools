package diagnostic

import "time"

const (
	automaticInvestigationName = "automatic-investigation.json"
	automaticRequestName       = "automatic-request.json"
	automaticPolicy            = "critical_intervals_v1"
	maxAutomaticInvestigations = 3
	maxAutomaticBytes          = 64 << 10
)

// AutomaticSelection identifies an observed interval, not a new stall count.
// CaseID remains empty until its evidence job has actually been created.
type AutomaticSelection struct {
	IntervalKind  string      `json:"interval_kind"`
	IntervalIndex int         `json:"interval_index"`
	Interval      PlayerStall `json:"interval"`
	CaseID        string      `json:"case_id,omitempty"`
}

type AutomaticInvestigation struct {
	Version             int                  `json:"version"`
	Policy              string               `json:"policy"`
	State               string               `json:"state"`
	OriginalState       string               `json:"original_state,omitempty"`
	SourceArtifact      Artifact             `json:"source_artifact"`
	TimelineArtifact    Artifact             `json:"timeline_artifact"`
	PlayerArtifact      *Artifact            `json:"player_artifact,omitempty"`
	PlayerRevision      int                  `json:"player_revision"`
	CandidateCount      int                  `json:"candidate_count"`
	Selected            []AutomaticSelection `json:"selected"`
	ModelState          string               `json:"model_state"`
	SummaryReportSHA256 string               `json:"summary_report_sha256,omitempty"`
	ReasonCode          string               `json:"reason_code,omitempty"`
	Reason              string               `json:"reason,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
	CompletedAt         *time.Time           `json:"completed_at,omitempty"`
}

type automaticPlan struct {
	CandidateCount int
	Selected       []AutomaticSelection
}
