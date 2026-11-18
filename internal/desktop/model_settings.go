package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"streamanalyzer/internal/diagnostic"
)

const modelCredentialService = "Stream Analyzer Model Settings"

var (
	errModelCredentialNotFound     = errors.New("model settings credential not found")
	errModelCredentialsUnavailable = errors.New("无法访问系统凭据库，请检查凭据库是否已解锁")
	errModelCredentialTooLarge     = errors.New("模型配置超出系统凭据库的长度限制")
	errModelSettingsCorrupt        = errors.New("已保存的模型配置无法加载，请重新保存设置")
	errModelSettingsFallback       = errors.New("环境变量中的模型配置无效，请在桌面端重新填写")
)

type modelCredentialStore interface {
	Get(account string) (string, error)
	Set(account, value string) error
}

type modelSettingsRequest struct {
	Provider    string `json:"provider"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	APIKey      string `json:"api_key,omitempty"`
	ClearAPIKey bool   `json:"clear_api_key,omitempty"`
}

type modelSettingsView struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	HasAPIKey bool   `json:"has_api_key"`
	Warning   string `json:"warning,omitempty"`
}

type modelCredential struct {
	diagnostic.ModelConfig
	Key string `json:"api_key,omitempty"`
}

type modelSettings struct {
	mu      sync.Mutex
	account string
	store   modelCredentialStore
	current diagnostic.ModelConfig
	warning string
}

func openModelSettings(dataDir string, fallback diagnostic.ModelConfig) (*modelSettings, diagnostic.Explainer, error) {
	return openModelSettingsWithStore(dataDir, fallback, systemModelCredentialStore{})
}

func openModelSettingsWithStore(dataDir string, fallback diagnostic.ModelConfig, store modelCredentialStore) (*modelSettings, diagnostic.Explainer, error) {
	settings := &modelSettings{account: modelCredentialAccount(dataDir), store: store}

	var warning error
	encoded, err := store.Get(settings.account)
	switch {
	case err == nil:
		var record modelCredential
		if json.Unmarshal([]byte(encoded), &record) == nil {
			record.APIKey = record.Key
			cfg := record.ModelConfig.Normalize()
			if explainer, err := cfg.NewExplainer(); err == nil && explainer != nil {
				settings.current = cfg
				return settings, explainer, nil
			}
		}
		warning = errModelSettingsCorrupt
	case !errors.Is(err, errModelCredentialNotFound):
		warning = errModelCredentialsUnavailable
	}

	fallback = fallback.Normalize()
	explainer, err := fallback.NewExplainer()
	if err != nil {
		warning = errors.Join(warning, errModelSettingsFallback)
	} else if explainer != nil {
		settings.current = fallback
	}
	if warning != nil {
		settings.warning = warning.Error()
	}
	return settings, explainer, warning
}

func modelCredentialAccount(dataDir string) string {
	if absolute, err := filepath.Abs(dataDir); err == nil {
		dataDir = absolute
	}
	if canonical, err := filepath.EvalSymlinks(dataDir); err == nil {
		dataDir = canonical
	}
	hash := sha256.Sum256([]byte(dataDir))
	return hex.EncodeToString(hash[:])
}

func (s *modelSettings) Current() modelSettingsView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view()
}

func (s *modelSettings) Save(input modelSettingsRequest, apply func(diagnostic.Explainer)) (modelSettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := s.resolveDraft(input)
	if err != nil {
		return modelSettingsView{}, err
	}
	explainer, err := next.NewExplainer()
	if err != nil {
		return modelSettingsView{}, err
	}
	// A single credential store update commits the endpoint and key together.
	encoded, err := json.Marshal(modelCredential{ModelConfig: next, Key: next.APIKey})
	if err != nil {
		return modelSettingsView{}, errModelSettingsCorrupt
	}
	if err := s.store.Set(s.account, string(encoded)); err != nil {
		if errors.Is(err, errModelCredentialTooLarge) {
			return modelSettingsView{}, errModelCredentialTooLarge
		}
		return modelSettingsView{}, errModelCredentialsUnavailable
	}

	s.current = next
	s.warning = ""
	apply(explainer)
	return s.view(), nil
}

func (s *modelSettings) TestConnection(ctx context.Context, input modelSettingsRequest) error {
	s.mu.Lock()
	next, err := s.resolveDraft(input)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return next.TestConnection(ctx)
}

// resolveDraft applies the same credential rules for saving and testing.
// The caller must hold s.mu.
func (s *modelSettings) resolveDraft(input modelSettingsRequest) (diagnostic.ModelConfig, error) {
	if strings.TrimSpace(input.Provider) == "" || strings.TrimSpace(input.Model) == "" {
		return diagnostic.ModelConfig{}, fmt.Errorf("%w: 请选择厂商并填写模型名", diagnostic.ErrInvalid)
	}
	next := diagnostic.ModelConfig{Provider: input.Provider, BaseURL: input.BaseURL, Model: input.Model, APIKey: input.APIKey}.Normalize()
	if input.ClearAPIKey && next.APIKey != "" {
		return diagnostic.ModelConfig{}, fmt.Errorf("%w: 清除 API Key 时请留空密钥输入框", diagnostic.ErrInvalid)
	}
	if !input.ClearAPIKey && next.APIKey == "" &&
		next.Provider == s.current.Provider && next.BaseURL == s.current.BaseURL {
		next.APIKey = s.current.APIKey
	}
	return next, nil
}

func (s *modelSettings) view() modelSettingsView {
	return modelSettingsView{
		Provider: s.current.Provider, BaseURL: s.current.BaseURL, Model: s.current.Model,
		HasAPIKey: s.current.APIKey != "", Warning: s.warning,
	}
}
