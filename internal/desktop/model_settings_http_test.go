package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"streamanalyzer/internal/appserver"
	"streamanalyzer/internal/diagnostic"
)

type testDesktopCredentials map[string]string

func (c testDesktopCredentials) Get(account string) (string, error) {
	record, ok := c[account]
	if !ok {
		return "", errModelCredentialNotFound
	}
	return record, nil
}

func (c testDesktopCredentials) Set(account, record string) error {
	c[account] = record
	return nil
}

func TestDesktopModelSettingsApplyWithoutRestart(t *testing.T) {
	dir := t.TempDir()
	credentials := make(testDesktopCredentials)
	settings, model, err := openModelSettingsWithStore(dir, diagnostic.ModelConfig{}, credentials)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &appserver.Config{}
	cfg.Server.DataDir = dir
	server, err := appserver.StartWithModel(t.Context(), "127.0.0.1:0", cfg, model)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
	})
	shell := Shell{App: &App{server: server, modelSettings: settings, modelSettingsToken: "test-shell-only"}}

	const key = "settings-fixture-key"
	const input = `{"provider":"deepseek","base_url":"https://api.deepseek.com","model":"deepseek-flash","api_key":"` + key + `"}`
	for _, test := range []struct {
		method, token, body string
		status              int
	}{
		{http.MethodGet, "", "", http.StatusForbidden},
		{http.MethodPost, "wrong-token", input, http.StatusForbidden},
		{http.MethodGet, "test-shell-only", "", http.StatusOK},
		{http.MethodPost, "test-shell-only", input + "{}", http.StatusBadRequest},
		{http.MethodPost, "test-shell-only", input, http.StatusOK},
		{http.MethodGet, "test-shell-only", "", http.StatusOK},
	} {
		req := httptest.NewRequest(test.method, "/model-settings", strings.NewReader(test.body))
		req.Header.Set(settingsHeader, test.token)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		shell.ServeHTTP(res, req)
		if res.Code != test.status || strings.Contains(res.Body.String(), key) {
			t.Fatalf("%s /model-settings returned %d: %s", test.method, res.Code, res.Body.String())
		}
	}

	res, err := http.Get(server.URL() + "/api/diagnostics/config")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var state map[string]any
	if err := json.NewDecoder(res.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state["model_configured"] != true || state["model_provider"] != "deepseek" || state["model_name"] != "deepseek-flash" {
		t.Fatalf("new model did not take effect on the running server: %+v", state)
	}

	res, err = http.Post(server.URL()+"/model-settings", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("desktop settings exposed on the loopback server: HTTP %d", res.StatusCode)
	}

	res, err = http.Post(server.URL()+"/model-settings/test", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("desktop model connection test exposed on the loopback server: HTTP %d", res.StatusCode)
	}

	reopened, model, err := openModelSettingsWithStore(dir, diagnostic.ModelConfig{}, credentials)
	if err != nil || model == nil || !reopened.Current().HasAPIKey || model.Identity().Provider != "deepseek" {
		t.Fatalf("settings did not survive reinitialization: model=%v error=%v", model != nil, err)
	}
}

func TestDesktopModelConnectionTestsDraftWithoutSaving(t *testing.T) {
	const savedKey = "saved-fixture-key"
	const draftKey = "draft-fixture-key"
	const badKey = "rejected-fixture-key"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("invalid provider request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wantPath := "/v1/chat/completions"
		var wantAuth string
		switch request.Model {
		case "new-draft":
			wantAuth = "Bearer " + draftKey
		case "same-endpoint":
			wantAuth = "Bearer " + savedKey
		case "other-endpoint":
			wantPath = "/alt/v1/chat/completions"
		case "clear-key":
		case "bad-key":
			wantAuth = "Bearer " + badKey
		default:
			t.Errorf("unexpected provider model %q", request.Model)
		}
		if r.Method != http.MethodPost || r.URL.Path != wantPath || r.Header.Get("Authorization") != wantAuth {
			t.Errorf("incorrect endpoint or authentication for %q", request.Model)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if request.Model == "bad-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_api_key","message":"rejected `+badKey+`"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
	}))
	defer upstream.Close()

	store := &testModelCredentialStore{}
	startingConfig := diagnostic.ModelConfig{
		Provider: diagnostic.ProviderOpenAICompatible, BaseURL: upstream.URL + "/v1", Model: "saved", APIKey: savedKey,
	}
	settings, _, err := openModelSettingsWithStore(t.TempDir(), startingConfig, store)
	if err != nil {
		t.Fatal(err)
	}
	const token = "test-shell-only"
	shell := Shell{App: &App{modelSettings: settings, modelSettingsToken: token}}

	for _, test := range []struct {
		name, method, token, target, key string
		status                           int
		clear                            bool
		reason                           string
	}{
		{name: "new-draft", method: http.MethodPost, token: token, target: "/v1", key: draftKey, status: http.StatusOK},
		{name: "same-endpoint", method: http.MethodPost, token: token, target: "/v1/", status: http.StatusOK},
		{name: "other-endpoint", method: http.MethodPost, token: token, target: "/alt/v1", status: http.StatusOK},
		{name: "clear-key", method: http.MethodPost, token: token, target: "/v1", clear: true, status: http.StatusOK},
		{name: "bad-key", method: http.MethodPost, token: token, target: "/v1", key: badKey, status: http.StatusBadGateway, reason: "API Key"},
		{name: "blocked", method: http.MethodPost, key: draftKey, status: http.StatusForbidden},
		{name: "get", method: http.MethodGet, token: token, status: http.StatusMethodNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(modelSettingsRequest{
				Provider: startingConfig.Provider, BaseURL: upstream.URL + test.target, Model: test.name,
				APIKey: test.key, ClearAPIKey: test.clear,
			})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(test.method, "/model-settings/test", strings.NewReader(string(body)))
			req.Header.Set(settingsHeader, test.token)
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			shell.ServeHTTP(res, req)
			if res.Code != test.status || res.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("connection test returned HTTP %d, want %d", res.Code, test.status)
			}
			result := res.Body.String()
			for _, key := range []string{savedKey, draftKey, badKey} {
				if strings.Contains(result, key) {
					t.Fatal("the connection test response contained an API key")
				}
			}
			if test.status == http.StatusOK {
				var result struct {
					ElapsedMS *int64 `json:"elapsed_ms"`
				}
				if err := json.NewDecoder(res.Body).Decode(&result); err != nil || result.ElapsedMS == nil || *result.ElapsedMS < 0 {
					t.Fatalf("connection test did not return a duration: %v", err)
				}
			}
			if !strings.Contains(result, test.reason) {
				t.Fatalf("connection test did not explain the failure: %s", result)
			}
			if len(store.records) != 0 || settings.current != startingConfig {
				t.Fatal("connection test changed desktop settings")
			}
		})
	}
}
