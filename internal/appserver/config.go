package appserver

import (
	"fmt"
	"os"
	"strings"

	"streamanalyzer/internal/diagnostic"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Addr    string `yaml:"addr"`
		DataDir string `yaml:"data_dir"`
		// MaxDumpMB caps the portable raw evidence for one task. Capture and
		// offline bundle validation share the same upper bound.
		MaxDumpMB *int `yaml:"max_dump_mb"`
	} `yaml:"server"`
}

const defaultMaxDumpMB = 128

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	ApplyDefaults(&cfg)
	if err := ValidateConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func ValidateConfig(cfg *Config) error {
	limit := cfg.Server.MaxDumpMB
	if limit == nil || *limit < 1 || int64(*limit)<<20 > diagnostic.MaxCaptureLimitBytes {
		return fmt.Errorf("server.max_dump_mb 须为 1 至 %d MiB", diagnostic.MaxCaptureLimitBytes>>20)
	}
	return nil
}

func ApplyDefaults(cfg *Config) {
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8087"
	}
	if cfg.Server.DataDir == "" {
		cfg.Server.DataDir = "./data"
	}
	if cfg.Server.MaxDumpMB == nil {
		def := defaultMaxDumpMB
		cfg.Server.MaxDumpMB = &def
	}
}

func diagnosticModel() (diagnostic.Explainer, error) {
	return ModelConfigFromEnv().NewExplainer()
}

// ModelConfigFromEnv preserves the legacy environment as a fallback for both
// entry points. Desktop preferences take precedence when available.
func ModelConfigFromEnv() diagnostic.ModelConfig {
	cfg := diagnostic.ModelConfig{
		Provider: strings.TrimSpace(os.Getenv("STREAMANALYZER_MODEL_PROVIDER")),
		BaseURL:  strings.TrimSpace(os.Getenv("STREAMANALYZER_MODEL_BASE_URL")),
		Model:    strings.TrimSpace(os.Getenv("STREAMANALYZER_MODEL_NAME")),
		APIKey:   strings.TrimSpace(os.Getenv("STREAMANALYZER_MODEL_API_KEY")),
	}
	if cfg == (diagnostic.ModelConfig{}) {
		cfg.BaseURL = strings.TrimSpace(os.Getenv("STREAMANALYZER_OPENAI_BASE_URL"))
		cfg.Model = strings.TrimSpace(os.Getenv("STREAMANALYZER_OPENAI_MODEL"))
		cfg.APIKey = strings.TrimSpace(os.Getenv("STREAMANALYZER_OPENAI_API_KEY"))
	}
	if cfg != (diagnostic.ModelConfig{}) {
		cfg.ImageInput = strings.TrimSpace(os.Getenv("STREAMANALYZER_MODEL_IMAGE_INPUT"))
	}
	return cfg
}
