package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"streamanalyzer/internal/diagnostic"
)

type testModelCredentialStore struct {
	records map[string]string
	readErr error
	saveErr error
}

func (s *testModelCredentialStore) Get(account string) (string, error) {
	if s.readErr != nil {
		return "", s.readErr
	}
	record, ok := s.records[account]
	if !ok {
		return "", errModelCredentialNotFound
	}
	return record, nil
}

func (s *testModelCredentialStore) Set(account, record string) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	if s.records == nil {
		s.records = make(map[string]string)
	}
	s.records[account] = record
	return nil
}

func TestModelSettingsCommitRestoreAndRedact(t *testing.T) {
	fallback := diagnostic.ModelConfig{Provider: diagnostic.ProviderOpenAICompatible, BaseURL: "https://legacy.example/v1", Model: "old", APIKey: "legacy-secret"}
	store := &testModelCredentialStore{}
	dir := t.TempDir()
	settings, model, err := openModelSettingsWithStore(dir, fallback, store)
	if err != nil || model == nil || model.Identity().Model != "old" {
		t.Fatalf("environment fallback failed: %v", err)
	}

	var applied diagnostic.Explainer
	view, err := settings.Save(modelSettingsRequest{Provider: diagnostic.ProviderDeepSeek, Model: "current", APIKey: "user-secret"},
		func(model diagnostic.Explainer) { applied = model })
	if err != nil || applied == nil || applied.Identity().Model != "current" {
		t.Fatalf("saved model was not applied: %v", err)
	}
	if view.Provider != diagnostic.ProviderDeepSeek || view.BaseURL != "https://api.deepseek.com" || !view.HasAPIKey {
		t.Fatalf("incorrect visible settings: %+v", view)
	}
	encodedView, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedView), "user-secret") || strings.Contains(string(encodedView), `"api_key"`) {
		t.Fatalf("secret appeared in settings view")
	}

	_, reloaded, err := openModelSettingsWithStore(dir, diagnostic.ModelConfig{Provider: "invalid-env"}, store)
	if err != nil || reloaded == nil || reloaded.Identity() != applied.Identity() {
		t.Fatalf("saved preferences did not override environment: %v", err)
	}
	_, unrelated, err := openModelSettingsWithStore(t.TempDir(), fallback, store)
	if err != nil || unrelated == nil || unrelated.Identity().Model != "old" {
		t.Fatalf("preferences leaked across data directories: %v", err)
	}
}

func TestModelSettingsReuseKeysOnlyForSameEndpoint(t *testing.T) {
	store := &testModelCredentialStore{}
	fallback := diagnostic.ModelConfig{
		Provider: diagnostic.ProviderDeepSeek, Model: "old", APIKey: "retained-secret",
	}
	settings, _, err := openModelSettingsWithStore(t.TempDir(), fallback, store)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(diagnostic.Explainer) {}
	view, err := settings.Save(modelSettingsRequest{
		Provider: diagnostic.ProviderDeepSeek, BaseURL: "https://api.deepseek.com/", Model: "updated",
	}, apply)
	if err != nil || !view.HasAPIKey || !strings.Contains(store.records[settings.account], "retained-secret") {
		t.Fatalf("existing key was not retained: %v", err)
	}
	for _, attempt := range []modelSettingsRequest{
		{Provider: diagnostic.ProviderDeepSeek, BaseURL: "https://another.example/v1", Model: "redirected"},
		{Provider: diagnostic.ProviderAnthropic, BaseURL: "https://api.deepseek.com", Model: "new-provider"},
	} {
		if _, err := settings.Save(attempt, apply); !errors.Is(err, diagnostic.ErrInvalid) {
			t.Fatalf("switching base URL or provider reused a native key: %v", err)
		}
	}
	view, err = settings.Save(modelSettingsRequest{
		Provider: diagnostic.ProviderOpenAICompatible, BaseURL: "http://127.0.0.1:11434/v1", Model: "local",
	}, apply)
	if err != nil || view.HasAPIKey || strings.Contains(store.records[settings.account], "retained-secret") {
		t.Fatalf("the previous key followed the switch to a keyless endpoint: %v", err)
	}
	view, err = settings.Save(modelSettingsRequest{
		Provider: diagnostic.ProviderOpenAICompatible, BaseURL: view.BaseURL, Model: view.Model, APIKey: "local-secret",
	}, apply)
	if err != nil || !view.HasAPIKey {
		t.Fatalf("key was not stored for compatible provider: %v", err)
	}
	view, err = settings.Save(modelSettingsRequest{
		Provider: diagnostic.ProviderOpenAICompatible, BaseURL: view.BaseURL, Model: view.Model, ClearAPIKey: true,
	}, apply)
	if err != nil || view.HasAPIKey || strings.Contains(store.records[settings.account], "local-secret") {
		t.Fatalf("explicit key clearing failed: %v", err)
	}
}

func TestModelSettingsStoreFailureLeavesPreviousModel(t *testing.T) {
	errCanary := errors.New("upstream error may include confidential details")
	store := &testModelCredentialStore{readErr: errCanary, saveErr: errCanary}
	fallback := diagnostic.ModelConfig{Provider: diagnostic.ProviderOpenAICompatible, BaseURL: "http://127.0.0.1:11434/v1", Model: "local"}
	settings, model, err := openModelSettingsWithStore(t.TempDir(), fallback, store)
	if !errors.Is(err, errModelCredentialsUnavailable) || settings.Current().Warning != errModelCredentialsUnavailable.Error() || model == nil {
		t.Fatalf("credential error did not preserve the available fallback: %v", err)
	}
	_, err = settings.Save(modelSettingsRequest{Provider: diagnostic.ProviderDeepSeek, Model: "new", APIKey: "private"}, func(diagnostic.Explainer) {
		t.Error("failed store operation still installed a different provider")
	})
	if !errors.Is(err, errModelCredentialsUnavailable) || settings.Current().Model != fallback.Model {
		t.Fatalf("failed store operation changed visible model settings: %v", err)
	}
}

func TestModelCredentialAccountUsesCanonicalDirectory(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("cannot create directory alias: %v", err)
	}
	if modelCredentialAccount(dir) != modelCredentialAccount(link) {
		t.Fatal("aliases of one data directory used different credential accounts")
	}
}
