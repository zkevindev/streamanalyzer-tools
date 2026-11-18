package diagnostic

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelConnectionUsesNativeGenerationEndpoints(t *testing.T) {
	for _, tc := range []struct {
		provider    string
		path        string
		keyHeader   string
		keyValue    string
		budgetField string
		response    string
	}{
		{
			ProviderOpenAICompatible, "/v1/chat/completions", "Authorization", "Bearer probe-key", "",
			`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`,
		},
		{
			ProviderDeepSeek, "/v1/chat/completions", "Authorization", "Bearer probe-key", "max_tokens",
			`{"choices":[{"message":{"role":"assistant","content":null,"reasoning_content":"thinking"},"finish_reason":"length"}]}`,
		},
		{
			ProviderAnthropic, "/v1/messages", "x-api-key", "probe-key", "max_tokens",
			`{"content":[],"stop_reason":"max_tokens"}`,
		},
		{
			ProviderGemini, "/v1/models/probe-model:generateContent", "x-goog-api-key", "probe-key", "maxOutputTokens",
			`{"candidates":[{"finishReason":"MAX_TOKENS"}]}`,
		},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			prompt, err := json.Marshal(connectionProbeInput)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != tc.path || r.URL.RawQuery != "" ||
					r.Header.Get(tc.keyHeader) != tc.keyValue {
					t.Error("connection probe did not use the configured generation endpoint and authentication")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				data, err := io.ReadAll(r.Body)
				body := string(data)
				if err != nil || !strings.Contains(body, string(prompt)) ||
					!strings.Contains(body, connectionProbeSystem) || strings.Contains(body, "probe-key") ||
					strings.Contains(body, `"findings"`) {
					t.Error("connection probe did not send only its fixed prompt")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if tc.budgetField != "" && !strings.Contains(body, `"`+tc.budgetField+`":`) {
					t.Error("connection probe did not set a small generation budget")
				}
				if tc.provider == ProviderDeepSeek && (!strings.Contains(body, `"type":"json_object"`) ||
					!strings.Contains(body, `"type":"disabled"`)) {
					t.Error("DeepSeek connection probe did not use the explanation request options")
				}
				if !strings.Contains(body, "probe-model") && tc.provider != ProviderGemini {
					t.Error("connection probe did not use the configured model")
				}
				io.WriteString(w, tc.response)
			}))
			defer server.Close()
			config := ModelConfig{Provider: tc.provider, BaseURL: server.URL + "/v1", Model: "probe-model", APIKey: "probe-key"}
			if err := config.TestConnection(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestModelConnectionRejectsMissingGenerationEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"choices":[{}],"content":[],"candidates":[{}]}`)
	}))
	defer server.Close()
	for _, provider := range []string{ProviderOpenAICompatible, ProviderDeepSeek, ProviderAnthropic, ProviderGemini} {
		t.Run(provider, func(t *testing.T) {
			config := ModelConfig{Provider: provider, BaseURL: server.URL, Model: "probe-model", APIKey: "probe-key"}
			if err := config.TestConnection(t.Context()); !errors.Is(err, ErrProvider) {
				t.Fatalf("expected provider error for missing generation envelope, got %v", err)
			}
		})
	}
	if err := (ModelConfig{}).TestConnection(t.Context()); !errors.Is(err, ErrUnconfigured) {
		t.Fatalf("expected unconfigured error, got %v", err)
	}
}
