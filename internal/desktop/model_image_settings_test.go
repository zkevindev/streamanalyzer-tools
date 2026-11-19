package desktop

import (
	"errors"
	"strings"
	"testing"

	"streamanalyzer/internal/diagnostic"
)

func TestModelImagePolicyPersistsAndAppliesWithoutChangingIdentity(t *testing.T) {
	store := &testModelCredentialStore{}
	dir := t.TempDir()
	settings, _, err := openModelSettingsWithStore(dir, diagnostic.ModelConfig{}, store)
	if err != nil {
		t.Fatal(err)
	}
	text := diagnostic.ImageInputText
	request := modelSettingsRequest{Provider: diagnostic.ProviderOpenAICompatible, BaseURL: "https://model.example/v1", Model: "same-model", APIKey: "private-policy-key", ImageInput: &text}
	var current diagnostic.Explainer
	apply := func(model diagnostic.Explainer) { current = model }
	view, err := settings.Save(request, apply)
	if err != nil || view.ImageInput != text || current.(diagnostic.ImageInputPolicy).ImageInputMode() != text {
		t.Fatalf("text policy not applied: %+v %v", view, err)
	}
	identity := current.Identity()
	// An older client omitting the new option must preserve a deliberate opt-out.
	request.ImageInput, request.APIKey = nil, ""
	view, err = settings.Save(request, apply)
	if err != nil || view.ImageInput != text || !view.HasAPIKey {
		t.Fatalf("omitted option reset policy or key: %+v %v", view, err)
	}
	restored, reloaded, err := openModelSettingsWithStore(dir, diagnostic.ModelConfig{}, store)
	if err != nil || restored.Current().ImageInput != text || reloaded.(diagnostic.ImageInputPolicy).ImageInputMode() != text {
		t.Fatalf("stored policy lost: %v", err)
	}
	auto := diagnostic.ImageInputAuto
	request.ImageInput = &auto
	view, err = settings.Save(request, apply)
	if err != nil || current.Identity() != identity || current.(diagnostic.ImageInputPolicy).ImageInputMode() != auto || view.ImageInput != auto {
		t.Fatalf("same-model policy not hot applied: %+v %v", view, err)
	}
	before := store.records[settings.account]
	bad := "unsupported-setting"
	request.ImageInput = &bad
	if _, err := settings.Save(request, apply); !errors.Is(err, diagnostic.ErrInvalid) || store.records[settings.account] != before || settings.Current().ImageInput != auto {
		t.Fatal("invalid policy altered persisted settings")
	}
	if !strings.Contains(before, "private-policy-key") {
		t.Fatal("policy switch lost saved credential")
	}
}
