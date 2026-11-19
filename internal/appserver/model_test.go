package appserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"streamanalyzer/internal/diagnostic"
)

func clearDiagnosticModelEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"STREAMANALYZER_MODEL_PROVIDER", "STREAMANALYZER_MODEL_BASE_URL", "STREAMANALYZER_MODEL_NAME", "STREAMANALYZER_MODEL_API_KEY",
		"STREAMANALYZER_MODEL_IMAGE_INPUT",
		"STREAMANALYZER_OPENAI_BASE_URL", "STREAMANALYZER_OPENAI_MODEL", "STREAMANALYZER_OPENAI_API_KEY",
	} {
		t.Setenv(name, "")
	}
}

func TestDiagnosticSwitchesToDeepSeekWithoutRestart(t *testing.T) {
	clearDiagnosticModelEnv(t)
	const modelName = "deepseek-flash"
	const original = "旧模型解读"
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/live.flv":
			_, _ = w.Write(testFLV())
		case "/deepseek/chat/completions":
			data, _ := io.ReadAll(r.Body)
			var request struct {
				Model          string `json:"model"`
				MaxTokens      int    `json:"max_tokens"`
				ResponseFormat struct {
					Type string `json:"type"`
				} `json:"response_format"`
				Thinking struct {
					Type string `json:"type"`
				} `json:"thinking"`
			}
			if r.Header.Get("Authorization") != "Bearer deepseek-test-key" ||
				json.Unmarshal(data, &request) != nil || request.Model != modelName ||
				request.ResponseFormat.Type != "json_object" || request.Thinking.Type != "disabled" || request.MaxTokens == 0 ||
				bytes.Contains(data, []byte("source-test-token")) || bytes.Contains(data, []byte("127.0.0.1")) ||
				!bytes.Contains(data, []byte("video_dts_gap")) {
				t.Error("incorrect DeepSeek API request or raw source leaked")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			evidence := "F001"
			if calls.Add(1) == 1 {
				evidence = "MISSING-FINDING"
			}
			content := fmt.Sprintf(`{"summary":"DeepSeek 的解读","hypotheses":[{"claim":"排查时间戳","evidence_ids":[%q],"next_check":"检查源端 DTS"}]}`, evidence)
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{
				map[string]any{"message": map[string]string{"content": content, "reasoning_content": "PROVIDER-INTERNAL"}},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	dir := t.TempDir()
	const api = "/api/diagnostics"
	server := testDiagnosticServer(t, dir)
	run := decodeDiagnostic[diagnostic.Run](t, diagnosticRequest(t, server, http.MethodPost, api+"/runs", http.StatusAccepted,
		[]byte(fmt.Sprintf(`{"url":%q,"seconds":1}`, upstream.URL+"/live.flv?token=source-test-token"))))
	runPath := api + "/runs/" + run.ID
	waitDiagnostic(t, server, runPath)

	// Older installs stored a model name with no vendor identifier. Keeping
	// the same name must not hide the option to switch to a native DeepSeek API.
	legacy := fmt.Sprintf(`{"model":%q,"summary":%q,"hypotheses":[]}`, modelName, original)
	file := filepath.Join(dir, "diagnostics", "runs", run.ID, "explanation.json")
	if err := os.WriteFile(file, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STREAMANALYZER_OPENAI_BASE_URL", upstream.URL+"/legacy")
	t.Setenv("STREAMANALYZER_OPENAI_MODEL", "old-model")
	t.Setenv("STREAMANALYZER_OPENAI_API_KEY", "legacy-test-key")
	t.Setenv("STREAMANALYZER_MODEL_PROVIDER", "deepseek")
	t.Setenv("STREAMANALYZER_MODEL_BASE_URL", upstream.URL+"/deepseek")
	t.Setenv("STREAMANALYZER_MODEL_NAME", modelName)
	t.Setenv("STREAMANALYZER_MODEL_API_KEY", "deepseek-test-key")

	model, err := diagnosticModel()
	if err != nil {
		t.Fatal(err)
	}
	server.SetDiagnosticModel(model)
	config := diagnosticRequest(t, server, http.MethodGet, api+"/config", http.StatusOK, nil)
	settings := decodeDiagnostic[map[string]any](t, config)
	if settings["model_provider"] != "deepseek" || settings["model_name"] != modelName ||
		bytes.Contains(config, []byte("test-key")) || bytes.Contains(config, []byte(upstream.URL)) {
		t.Fatalf("unexpected publicly visible provider configuration: %s", config)
	}
	if got := waitDiagnostic(t, server, runPath).Explanation; got == nil || got.Summary != original {
		t.Fatalf("previous explanation was unavailable: %+v", got)
	}
	diagnosticRequest(t, server, http.MethodPost, runPath+"/explain", http.StatusBadGateway, nil)
	if got := waitDiagnostic(t, server, runPath).Explanation; got == nil || got.Summary != original {
		t.Fatalf("bad new explanation replaced stored output: %+v", got)
	}

	data := diagnosticRequest(t, server, http.MethodPost, runPath+"/explain", http.StatusOK, nil)
	result := decodeDiagnostic[diagnostic.Explanation](t, data)
	if result.Provider != "deepseek" || result.Model != modelName || result.Summary == original || bytes.Contains(data, []byte("PROVIDER-INTERNAL")) {
		t.Fatalf("unexpected DeepSeek explanation: %+v", result)
	}
	diagnosticRequest(t, server, http.MethodPost, runPath+"/explain", http.StatusOK, nil)
	if calls.Load() != 2 {
		t.Fatal("explanation was not reused for the same provider and model")
	}
}
