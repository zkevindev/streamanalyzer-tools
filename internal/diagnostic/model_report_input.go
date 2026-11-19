package diagnostic

// ReportModelInputEvidence binds the overall conclusion to the numeric input
// and the exact local pictures selected for this request. It is service-owned,
// never accepted from a provider's generated JSON.
type ReportModelInputEvidence struct {
	Version           int                        `json:"version"`
	ReportSHA256      string                     `json:"report_sha256"`
	PlayerRevision    int                        `json:"player_revision"`
	RequestedMode     string                     `json:"requested_mode"`
	Mode              string                     `json:"mode"`
	FallbackCode      string                     `json:"fallback_code,omitempty"`
	Selection         string                     `json:"selection"`
	Cases             []ReportModelCaseReference `json:"cases"`
	Images            []ModelImageReference      `json:"images"`
	AttemptedImages   []ModelImageReference      `json:"attempted_images,omitempty"`
	OmittedImageCases int                        `json:"omitted_image_cases,omitempty"`
}

type ReportModelCaseReference struct {
	CaseID         string `json:"case_id"`
	PlayerRevision int    `json:"player_revision"`
}
