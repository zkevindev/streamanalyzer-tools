package diagnostic

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestModelImageUnsupportedRequiresExplicitInputCapability(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"openai content capability", 400, `{"error":{"type":"invalid_request_error","message":"Invalid content type. image_url is only supported by certain models."}}`, true},
		{"anthropic model capability", 400, `{"error":{"type":"invalid_request_error","message":"This model does not support images."}}`, true},
		{"gemini numeric status", 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"Image input is not supported by this model."}}`, true},
		{"unsupported media input", 415, `{"error":{"code":"unsupported_image_input"}}`, true},
		{"unprocessable vision input", 422, `{"error":{"type":"vision_not_supported"}}`, true},
		{"top level capability", 400, `{"code":"image_input_not_supported"}`, true},
		{"quoted image url", 400, `{"error":{"message":"The content type 'image_url' is not supported by this model."}}`, true},
		{"unsupported image modality", 400, `{"error":{"message":"Unsupported modality: image"}}`, true},
		{"explicit text only model", 400, `{"error":{"message":"This model only supports text input."}}`, true},
		{"generic unsupported model with vision proof", 400, `{"error":{"code":"model_not_supported","message":"This model does not support image input."}}`, true},
		{"generic unsupported model without proof", 400, `{"error":{"code":"model_not_supported","message":"The requested model is unavailable."}}`, false},
		{"missing endpoint with vision proof", 404, `{"error":{"code":"model_not_supported","message":"This model does not support image input."}}`, false},
		{"generic bad request", 400, `{"error":{"type":"invalid_request_error","message":"Invalid request."}}`, false},
		{"unsupported json schema", 400, `{"error":{"message":"This model does not support response_format."}}`, false},
		{"unsupported image format", 400, `{"error":{"message":"This model does not support image format webp."}}`, false},
		{"invalid image", 400, `{"error":{"code":"invalid_image","message":"Unable to decode image."}}`, false},
		{"corrupt image", 400, `{"error":{"message":"Corrupt image input is not supported."}}`, false},
		{"image too large", 413, `{"error":{"message":"Images are not supported at this image size."}}`, false},
		{"unsupported image size", 400, `{"error":{"message":"Images are not supported at this resolution."}}`, false},
		{"auth declared despite400", 400, `{"error":{"type":"authentication_error","message":"This model does not support images."}}`, false},
		{"quota declared despite400", 400, `{"error":{"code":"insufficient_quota","message":"This model does not support images."}}`, false},
		{"permission in message", 400, `{"error":{"message":"This API key does not support images; permission denied."}}`, false},
		{"rate limit in message", 400, `{"error":{"message":"Images are not supported during rate limit."}}`, false},
		{"safety rejection", 400, `{"error":{"message":"Image content is not supported by safety policy."}}`, false},
		{"authentication", 401, `{"error":{"code":"unsupported_image_input"}}`, false},
		{"permission", 403, `{"error":{"code":"unsupported_image_input"}}`, false},
		{"model not found", 404, `{"error":{"code":"unsupported_image_input"}}`, false},
		{"request timeout", 408, `{"error":{"code":"unsupported_image_input"}}`, false},
		{"rate limit", 429, `{"error":{"code":"unsupported_image_input"}}`, false},
		{"server failure", 500, `{"error":{"code":"unsupported_image_input"}}`, false},
		{"gateway failure", 502, `{"error":{"code":"unsupported_image_input"}}`, false},
		{"successful invalid response", 200, `{"error":{"code":"unsupported_image_input"}}`, false},
		{"html response", 400, `<h1>This model does not support images</h1>`, false},
		{"invalid json", 400, `{"error":{"message":"This model does not support images."}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := modelImageUnsupported(test.status, []byte(test.body)); got != test.want {
				t.Fatalf("capability classification=%v want%v", got, test.want)
			}
		})
	}
}

func TestModelImageUnsupportedOnlyAffectsActualImageRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"unsupported_image_input","message":"This model does not support images: https://secret-user:private-key@private.invalid/request?token=private-token"}}`)
	}))
	defer server.Close()
	client, err := newModelClient(ProviderOpenAICompatible, server.URL, "test", "private-key")
	if err != nil {
		t.Fatal(err)
	}
	var output any
	err = client.postImages(t.Context(), "/image", nil, map[string]string{"image": "inline bytes"}, &output)
	if !errors.Is(err, ErrModelImagesUnsupported) || !errors.Is(err, ErrProvider) {
		t.Fatalf("explicit image rejection not classified: %v", err)
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret-user") {
		t.Fatal("raw image failure leaked private response")
	}
	err = client.post(t.Context(), "/text", nil, map[string]string{"text": "report"}, &output)
	var classified *ModelConnectionError
	if errors.Is(err, ErrModelImagesUnsupported) || !errors.As(err, &classified) || classified.Code != modelRejected || classified.StatusCode != 400 {
		t.Fatalf("text request acquired image fallback semantics: %v", err)
	}
}

func TestModelImageProvidersKeepOtherFailuresVisibleWithoutRetry(t *testing.T) {
	images := providerImageFixture(t)
	for _, name := range []string{"openai", "anthropic", "gemini"} {
		for _, test := range []struct {
			status int
			body   string
		}{
			{401, `{"error":{"code":"invalid_api_key","message":"This model does not support images."}}`},
			{429, `{"error":{"code":"rate_limit_exceeded","message":"This model does not support images."}}`},
			{500, `{"error":{"code":"unsupported_image_input"}}`},
			{400, `{"error":{"code":"invalid_image","message":"Unable to decode image."}}`},
			{200, `{"unexpected":"invalid response"}`},
		} {
			t.Run(name+"/"+http.StatusText(test.status), func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.WriteHeader(test.status)
					_, _ = io.WriteString(w, test.body)
				}))
				defer server.Close()
				provider := newTestImageProvider(t, name, server.URL)
				_, err := provider.ExplainWithImages(t.Context(), Report{}, images)
				if err == nil || errors.Is(err, ErrModelImagesUnsupported) || !errors.Is(err, ErrProvider) || calls.Load() != 1 {
					t.Fatalf("noncapability failure hidden/retried: %v calls%d", err, calls.Load())
				}
			})
		}
	}
}
