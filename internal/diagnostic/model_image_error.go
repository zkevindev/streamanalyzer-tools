package diagnostic

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Only a rejected image request with explicit input-capability evidence may
// be retried as text. Provider messages are inspected transiently and never
// retained in an error, report or fallback reason.
func modelImageUnsupported(status int, payload []byte) bool {
	if status != http.StatusBadRequest && status != http.StatusUnsupportedMediaType && status != http.StatusUnprocessableEntity {
		return false
	}
	// Some compatible gateways use model_not_supported for a model that
	// accepts text but rejects image input. Still require explicit image
	// capability proof below; a missing model alone never triggers fallback.
	if code := modelDeclaredError(payload); code != "" && code != modelNotFound {
		return false
	}
	type fields struct {
		Code    json.RawMessage `json:"code"`
		Type    string          `json:"type"`
		Message string          `json:"message"`
	}
	var response struct {
		fields
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(payload, &response) != nil {
		return false
	}
	values := []fields{response.fields}
	if len(response.Error) > 0 {
		var nested fields
		if json.Unmarshal(response.Error, &nested) == nil {
			values = append(values, nested)
		}
	}
	for _, value := range values {
		message := strings.Join(strings.Fields(strings.ToLower(value.Message)), " ")
		// An invalid/oversized image or unsupported file encoding is not proof
		// that the model lacks image input. Keep these failures visible.
		blocked := false
		for _, detail := range []string{"image format", "image file", "mime", "jpeg", "png", "webp", "corrupt", "too large", "image size", "resolution", "safety", "moderation", "api key", "api_key", "authentication", "unauthorized", "permission", "quota", "rate limit", "rate_limit", "billing", "balance", "credit", "payment", "context length"} {
			if strings.Contains(message, detail) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		var declared string
		_ = json.Unmarshal(value.Code, &declared)
		for _, code := range []string{declared, value.Type} {
			switch strings.ToLower(strings.TrimSpace(code)) {
			case "image_input_not_supported", "unsupported_image_input", "vision_not_supported", "multimodal_not_supported", "images_not_supported", "image_not_supported":
				return true
			}
		}
		for _, proof := range []string{
			"does not support image", "doesn't support image", "do not support image",
			"does not support vision", "doesn't support vision", "do not support vision",
			"image input is not supported", "image inputs are not supported", "images are not supported",
			"image content is not supported", "image blocks are not supported", "image content blocks are not supported",
			"image_url is only supported by certain models", "image_url is not supported by this model",
			"content blocks of type image are not supported",
			"'image_url' is not supported", `"image_url" is not supported`,
			"unsupported content type: image", "unsupported input type: image", "unsupported modality: image",
			"model only supports text input", "only text input is supported",
		} {
			if strings.Contains(message, proof) {
				return true
			}
		}
	}
	return false
}
