package appserver

import (
	"testing"

	"streamanalyzer/internal/diagnostic"
)

func TestModelImageInputRetainsLegacyEnvironmentCredentials(t *testing.T) {
	clearDiagnosticModelEnv(t)
	t.Setenv("STREAMANALYZER_MODEL_IMAGE_INPUT", diagnostic.ImageInputText)
	if cfg := ModelConfigFromEnv(); cfg != (diagnostic.ModelConfig{}) {
		t.Fatal("an image policy alone configured an empty model")
	}
	t.Setenv("STREAMANALYZER_OPENAI_BASE_URL", "http://127.0.0.1:11434/v1")
	t.Setenv("STREAMANALYZER_OPENAI_MODEL", "legacy-model")
	cfg := ModelConfigFromEnv()
	model, err := cfg.NewExplainer()
	if err != nil || model == nil || model.Identity().Model != "legacy-model" || model.(diagnostic.ImageInputPolicy).ImageInputMode() != diagnostic.ImageInputText {
		t.Fatalf("new mode lost legacy credentials: %+v %v", cfg, err)
	}
}
