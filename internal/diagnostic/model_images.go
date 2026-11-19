package diagnostic

import (
	"context"
	"fmt"
	"path"
)

const (
	ImageInputAuto     = "auto"
	ImageInputText     = "text"
	MaxModelImages     = 3
	MaxModelImageBytes = 3 * maxDerivedImageBytes
)

var ErrModelImagesUnsupported = fmt.Errorf("%w: image input unsupported", ErrProvider)

// ImageExplainer is optional. Existing text explainers remain valid.
type ImageExplainer interface {
	ExplainWithImages(context.Context, Report, []ModelImage) (Explanation, error)
}

type ImageInputPolicy interface{ ImageInputMode() string }
type ImageCapability interface{ SupportsImages() bool }

type ModelImageReference struct {
	ID     string `json:"id"`
	CaseID string `json:"case_id"`
	Name   string `json:"name"`
	Artifact
	MediaTimeMS *int64 `json:"media_time_ms,omitempty"`
}

// JPEG is transient input. It is never serialized into a report or bundle.
type ModelImage struct {
	Reference ModelImageReference `json:"reference"`
	JPEG      []byte              `json:"-"`
}

// Images names the final successful vision request's inputs. AttemptedImages
// records a rejected image request followed by a successful text request.
type ModelInputEvidence struct {
	Version         int                   `json:"version"`
	CaseID          string                `json:"case_id"`
	PlayerRevision  int                   `json:"player_revision"`
	RequestedMode   string                `json:"requested_mode"`
	Mode            string                `json:"mode"`
	FallbackCode    string                `json:"fallback_code,omitempty"`
	Images          []ModelImageReference `json:"images"`
	AttemptedImages []ModelImageReference `json:"attempted_images,omitempty"`
}

func modelImageInput(model Explainer) string {
	if policy, ok := model.(ImageInputPolicy); ok && policy.ImageInputMode() == ImageInputText {
		return ImageInputText
	}
	return ImageInputAuto
}

func (c modelClient) ImageInputMode() string {
	if c.imageInput == ImageInputText {
		return ImageInputText
	}
	return ImageInputAuto
}

func (c *modelClient) setImageInput(mode string) { c.imageInput = mode }

func (s *Service) ModelImageInput() string {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.model == nil {
		return ImageInputAuto
	}
	return modelImageInput(s.model.explainer)
}

func cloneModelImageReferences(source []ModelImageReference) []ModelImageReference {
	if source == nil {
		return nil
	}
	result := append([]ModelImageReference{}, source...)
	for i := range result {
		if result[i].MediaTimeMS != nil {
			value := *result[i].MediaTimeMS
			result[i].MediaTimeMS = &value
		}
	}
	return result
}

// These IDs are trusted file evidence, separate from receive-side findings.
func modelImageFindings(images []ModelImageReference) []Finding {
	result := make([]Finding, 0, len(images))
	for _, image := range images {
		at := "媒体时间未记录"
		if image.MediaTimeMS != nil {
			at = fmt.Sprintf("媒体时间 %d ms", *image.MediaTimeMS)
		}
		result = append(result, Finding{ID: image.ID, Code: "model_screenshot", Severity: "info", Message: fmt.Sprintf("源流截图 %s（%s），用于核对画面内容。", path.Base(image.Name), at), NextCheck: "结合对应的收流、缓冲与呈帧记录验证；静态源流截图不能证明浏览器当时的画面、卡顿或根因。", Occurrences: 1})
	}
	return result
}
