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

func TestGeminiExplain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1beta/models/gemini-test:generateContent" || r.URL.RawQuery != "" {
			t.Errorf("unexpected Gemini endpoint: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("x-goog-api-key") != "test-gemini-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("Gemini authentication or JSON content type missing")
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "could not read request", http.StatusBadRequest)
			return
		}
		var request struct {
			SystemInstruction geminiContent   `json:"systemInstruction"`
			Contents          []geminiContent `json:"contents"`
			GenerationConfig  struct {
				ResponseMimeType string `json:"responseMimeType"`
			} `json:"generationConfig"`
		}
		if err := json.Unmarshal(data, &request); err != nil || len(request.SystemInstruction.Parts) != 1 ||
			request.SystemInstruction.Parts[0].Text != explanationPrompt || request.GenerationConfig.ResponseMimeType != "application/json" ||
			len(request.Contents) != 1 || request.Contents[0].Role != "user" || len(request.Contents[0].Parts) != 1 {
			t.Error("Gemini request is missing the system prompt, report or JSON output settings")
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		summary := request.Contents[0].Parts[0].Text
		if !strings.Contains(summary, "F001") || !strings.Contains(summary, "bad timing") ||
			strings.Contains(summary, "private.invalid") || strings.Contains(summary, "signed-secret") || strings.Contains(summary, "private-dump.ts") {
			t.Error("Gemini received unexpected report fields")
		}
		const response = `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"internal thought","thought":true},{"text":"{\"summary\":\"observed issue\","},{"text":"\"hypotheses\":[{\"claim\":\"timing\",\"evidence_ids\":[\"F001\"],\"next_check\":\"measure PTS\"}]}"}]}}]}`
		_, _ = io.WriteString(w, response)
	}))
	defer server.Close()

	provider, err := NewGemini(server.URL+"/v1beta", "models/gemini-test", "test-gemini-key")
	if err != nil {
		t.Fatal(err)
	}
	report := Report{
		Manifest: Manifest{Protocol: ProtocolHLS, Observation: Observation{Origin: "https://private.invalid/live.m3u8?token=signed-secret"}},
		Findings: []Finding{{ID: "F001", Message: "bad timing", Evidence: []Event{{Artifact: "private-dump.ts"}}}},
	}
	result, err := provider.Explain(t.Context(), report)
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != ProviderGemini || result.Model != "gemini-test" || result.Summary != "observed issue" ||
		len(result.Hypotheses) != 1 || len(result.Hypotheses[0].EvidenceIDs) != 1 || result.Hypotheses[0].EvidenceIDs[0] != "F001" {
		t.Fatalf("unexpected explanation: %+v", result)
	}
}

func TestGeminiRejectsUnavailableResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"blocked prompt", `{"promptFeedback":{"blockReason":"SAFETY"}}`},
		{"blocked candidate", `{"candidates":[{"finishReason":"SAFETY","content":{"parts":[{"text":"{\"summary\":\"filtered\"}"}]}}]}`},
		{"thought only", `{"candidates":[{"content":{"parts":[{"text":"{\"summary\":\"thinking\"}","thought":true}]}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			provider, err := NewGemini(server.URL, "gemini-test", "test-gemini-key")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.Explain(t.Context(), Report{}); !errors.Is(err, ErrProvider) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGeminiValidatesModelPath(t *testing.T) {
	for _, model := range []string{"", "models/", "../gemini-test", "models/gemini-test:streamGenerateContent", "gemini-test%2fother"} {
		if _, err := NewGemini("", model, "test-gemini-key"); !errors.Is(err, ErrInvalid) {
			t.Errorf("model %q returned unexpected error: %v", model, err)
		}
	}
	if _, err := NewGemini("", "gemini-test", " "); !errors.Is(err, ErrInvalid) {
		t.Errorf("missing key returned unexpected error: %v", err)
	}
}
