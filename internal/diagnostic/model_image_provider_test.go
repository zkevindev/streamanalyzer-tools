package diagnostic

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type testImageProvider interface {
	Explainer
	ImageExplainer
	SupportsImages() bool
}

func newTestImageProvider(t *testing.T, name, endpoint string) testImageProvider {
	t.Helper()
	var provider testImageProvider
	var err error
	switch name {
	case "openai":
		provider, err = NewOpenAI(endpoint, "image-model", "private-api-key")
	case "anthropic":
		provider, err = NewAnthropic(endpoint, "image-model", "private-api-key")
	case "gemini":
		provider, err = NewGemini(endpoint, "image-model", "private-api-key")
	default:
		t.Fatal("unknown provider fixture")
	}
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func providerImageFixture(t *testing.T) []ModelImage {
	t.Helper()
	var images []ModelImage
	for index, name := range []string{"before.jpg", "during.jpg", "after.jpg"} {
		picture := image.NewRGBA(image.Rect(0, 0, 12, 8))
		for y := range 8 {
			for x := range 12 {
				picture.Set(x, y, color.RGBA{R: uint8(index * 60), G: uint8(x * 10), B: uint8(y * 20), A: 255})
			}
		}
		var jpegBytes bytes.Buffer
		if err := jpeg.Encode(&jpegBytes, picture, nil); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(jpegBytes.Bytes())
		at := int64(index * 1200)
		images = append(images, ModelImage{Reference: ModelImageReference{ID: "S001-I00" + string(rune('1'+index)), CaseID: "S001", Name: "stalls/S001/" + name,
			Artifact: Artifact{Bytes: int64(jpegBytes.Len()), SHA256: hex.EncodeToString(hash[:])}, MediaTimeMS: &at}, JPEG: append([]byte(nil), jpegBytes.Bytes()...)})
	}
	return images
}

func providerImageResponse(w http.ResponseWriter, name string) {
	// Provider-controlled audit fields must never become application evidence.
	answer := `{"summary":"画面内容需结合缓冲时间线核对","hypotheses":[{"claim":"核对源媒体是否变化","evidence_ids":["F001"],"next_check":"比对同一媒体时钟"}],"input_evidence":{"version":1,"mode":"vision","images":[{"id":"forged"}]}}`
	switch name {
	case "openai":
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": answer}}}})
	case "anthropic":
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "thinking", "thinking": "private reasoning"}, map[string]any{"type": "text", "text": answer}}})
	case "gemini":
		_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"parts": []any{map[string]any{"text": "private reasoning", "thought": true}, map[string]any{"text": answer}}}}}})
	}
}

func TestModelImageProvidersSendExactJPEGAndTimeline(t *testing.T) {
	images := providerImageFixture(t)
	report := Report{Manifest: Manifest{Protocol: ProtocolRTSP, Observation: Observation{Origin: "rtsp://secret-user:secret-password@private-camera/secret-path?token=private-source-token"}},
		Findings: []Finding{{ID: "F001", Code: "test_buffer", Message: "保存的缓冲时间线", Evidence: []Event{{Artifact: "private-source-artifact"}}}}}
	for _, name := range []string{"openai", "anthropic", "gemini"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				var envelope struct {
					Messages []struct {
						Role    string          `json:"role"`
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
					Contents []struct {
						Role  string            `json:"role"`
						Parts []json.RawMessage `json:"parts"`
					} `json:"contents"`
				}
				if json.Unmarshal(body, &envelope) != nil {
					t.Error("invalid provider image JSON")
					return
				}
				var blocks []json.RawMessage
				switch name {
				case "openai":
					if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer private-api-key" || len(envelope.Messages) != 2 || envelope.Messages[1].Role != "user" {
						t.Error("wrong OpenAI image endpoint/role/authentication")
						return
					}
					_ = json.Unmarshal(envelope.Messages[1].Content, &blocks)
				case "anthropic":
					if r.URL.Path != "/messages" || r.Header.Get("x-api-key") != "private-api-key" || len(envelope.Messages) != 1 || envelope.Messages[0].Role != "user" {
						t.Error("wrong Anthropic image endpoint/role/authentication")
						return
					}
					_ = json.Unmarshal(envelope.Messages[0].Content, &blocks)
				case "gemini":
					if r.URL.Path != "/models/image-model:generateContent" || r.Header.Get("x-goog-api-key") != "private-api-key" || len(envelope.Contents) != 1 || envelope.Contents[0].Role != "user" {
						t.Error("wrong Gemini image endpoint/role/authentication")
						return
					}
					blocks = envelope.Contents[0].Parts
				}
				if len(blocks) != 7 {
					t.Errorf("want report plus3image labels/blocks, got%d", len(blocks))
					return
				}
				var summary struct {
					Text string `json:"text"`
				}
				_ = json.Unmarshal(blocks[0], &summary)
				var input struct {
					Pixels   bool                  `json:"image_pixels_provided"`
					Images   []ModelImageReference `json:"image_evidence"`
					Findings []modelFinding        `json:"findings"`
				}
				if json.Unmarshal([]byte(summary.Text), &input) != nil || !input.Pixels || len(input.Images) != 3 || len(input.Findings) == 0 {
					t.Error("image request lost actual pixel manifest or numeric timeline")
				}
				for index, image := range images {
					var label struct {
						Text string `json:"text"`
					}
					_ = json.Unmarshal(blocks[1+index*2], &label)
					if !strings.Contains(label.Text, image.Reference.ID) || !strings.Contains(label.Text, image.Reference.Name) || !strings.Contains(label.Text, "ms") {
						t.Error("image has no actual evidence ID/media time label")
					}
					var encoded struct {
						Type     string `json:"type"`
						Text     string `json:"text"`
						ImageURL *struct {
							URL string `json:"url"`
						} `json:"image_url"`
						Source     *struct{ Type, MediaType, Data string } `json:"source"`
						InlineData *geminiImageData                        `json:"inlineData"`
					}
					// media_type needs its native wire spelling rather than Go's field name.
					if json.Unmarshal(blocks[2+index*2], &encoded) != nil || encoded.Text != "" {
						t.Error("image block contains arbitrary text")
					}
					var data string
					switch name {
					case "openai":
						if encoded.Type != "image_url" || encoded.ImageURL == nil || !strings.HasPrefix(encoded.ImageURL.URL, "data:image/jpeg;base64,") {
							t.Error("not an inline OpenAI JPEG")
							return
						}
						data = strings.TrimPrefix(encoded.ImageURL.URL, "data:image/jpeg;base64,")
					case "anthropic":
						var source struct {
							Source struct {
								Type      string `json:"type"`
								MediaType string `json:"media_type"`
								Data      string `json:"data"`
							} `json:"source"`
						}
						_ = json.Unmarshal(blocks[2+index*2], &source)
						if encoded.Type != "image" || source.Source.Type != "base64" || source.Source.MediaType != "image/jpeg" {
							t.Error("not a native Anthropic JPEG source")
							return
						}
						data = source.Source.Data
					case "gemini":
						if encoded.InlineData == nil || encoded.InlineData.MimeType != "image/jpeg" {
							t.Error("not native Gemini inlineData JPEG")
							return
						}
						data = encoded.InlineData.Data
					}
					decoded, err := base64.StdEncoding.DecodeString(data)
					if err != nil || !bytes.Equal(decoded, image.JPEG) {
						t.Error("provider did not receive the exact validated JPEG")
					}
				}
				for _, secret := range []string{"secret-user", "secret-password", "private-camera", "private-source-token", "private-source-artifact", "private-api-key"} {
					if bytes.Contains(body, []byte(secret)) {
						t.Errorf("request leaked private metadata %s", secret)
					}
				}
				providerImageResponse(w, name)
			}))
			defer server.Close()
			provider := newTestImageProvider(t, name, server.URL)
			if !provider.SupportsImages() {
				t.Fatal("native image provider declared unsupported")
			}
			result, err := provider.ExplainWithImages(t.Context(), report, images)
			if err != nil || result.Summary == "" || result.InputEvidence != nil || calls.Load() != 1 {
				t.Fatalf("image response failed or trusted forged audit: %+v %v calls%d", result, err, calls.Load())
			}
		})
	}
}

func TestModelImageProvidersRejectUnverifiedJPEGWithoutHTTP(t *testing.T) {
	for _, name := range []string{"openai", "anthropic", "gemini"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); providerImageResponse(w, name) }))
			defer server.Close()
			provider := newTestImageProvider(t, name, server.URL)
			for _, mutate := range []func([]ModelImage){
				func(images []ModelImage) { images[0].JPEG[0] ^= 1 },
				func(images []ModelImage) { images[0].Reference.Name = "https://private.invalid/photo.jpg" },
				func(images []ModelImage) { images[1].Reference.CaseID = "S002" },
			} {
				images := providerImageFixture(t)
				mutate(images)
				if _, err := provider.ExplainWithImages(t.Context(), Report{}, images); !errors.Is(err, ErrInvalid) {
					t.Fatalf("invalid image accepted: %v", err)
				}
			}
			if calls.Load() != 0 {
				t.Fatal("unverified image left the process")
			}
		})
	}
}

func TestDeepSeekImagesReturnUnsupportedWithoutSendingPixels(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	provider, err := NewDeepSeek(server.URL, "deepseek-chat", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if provider.SupportsImages() {
		t.Fatal("DeepSeek declared image input support")
	}
	if _, err := provider.ExplainWithImages(t.Context(), Report{}, providerImageFixture(t)); !errors.Is(err, ErrModelImagesUnsupported) || calls.Load() != 0 {
		t.Fatalf("DeepSeek sent unsupported pixels: %v calls%d", err, calls.Load())
	}
}
