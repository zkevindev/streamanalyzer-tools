package diagnostic

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicExplainUsesSanitizedMessages(t *testing.T) {
	report := Report{
		Manifest: Manifest{
			Protocol: ProtocolHTTPFLV,
			Observation: Observation{
				Origin:    "https://private-origin.example/live?token=private-playback-token",
				ElapsedMS: 1000,
			},
			Stream: Artifact{Bytes: 4096, SHA256: "private-artifact-hash"},
		},
		Findings: []Finding{{
			ID: "F001", Code: "test_gap", Severity: "warn", Message: "时间戳间断",
			Evidence: []Event{{Artifact: "private-media-file.flv", Offset: 100}},
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/v1/messages" ||
			req.Header.Get("x-api-key") != "test-anthropic-key" ||
			req.Header.Get("anthropic-version") != anthropicVersion {
			t.Error("unexpected Anthropic Messages endpoint or headers")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(req.Body)
		var input struct {
			Model     string `json:"model"`
			System    string `json:"system"`
			MaxTokens int    `json:"max_tokens"`
			Messages  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err != nil || json.Unmarshal(body, &input) != nil || input.Model != "test-claude" ||
			input.System != explanationPrompt || input.MaxTokens <= 0 ||
			len(input.Messages) != 1 || input.Messages[0].Role != "user" ||
			!strings.Contains(input.Messages[0].Content, "F001") {
			t.Error("unexpected Anthropic Messages payload")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, private := range []string{"private-origin.example", "private-playback-token",
			"private-artifact-hash", "private-media-file.flv"} {
			if strings.Contains(input.Messages[0].Content, private) {
				t.Error("diagnostic payload included private stream metadata")
			}
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{
				{"type": "thinking", "thinking": "private model reasoning"},
				{"type": "text", "text": `{"summary":"时间戳间断",`},
				{"type": "text", "text": `"hypotheses":[{"claim":"检查编码端","evidence_ids":["F001"],"next_check":"比对源流"}]}`},
			},
		}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	provider, err := NewAnthropic(server.URL+"/v1", "test-claude", "test-anthropic-key")
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := provider.Explain(t.Context(), report)
	if err != nil {
		t.Fatal(err)
	}
	if explanation.Model != "test-claude" || explanation.Summary != "时间戳间断" ||
		len(explanation.Hypotheses) != 1 || len(explanation.Hypotheses[0].EvidenceIDs) != 1 ||
		explanation.Hypotheses[0].EvidenceIDs[0] != "F001" {
		t.Fatalf("unexpected explanation: %+v", explanation)
	}
}
