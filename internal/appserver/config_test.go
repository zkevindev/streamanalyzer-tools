package appserver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureLimitConfigDefaultsAndValidation(t *testing.T) {
	cfg := &Config{}
	ApplyDefaults(cfg)
	if cfg.Server.MaxDumpMB == nil || *cfg.Server.MaxDumpMB != defaultMaxDumpMB || defaultMaxDumpMB != 128 {
		t.Fatalf("unexpected default capture limit: %+v", cfg.Server.MaxDumpMB)
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	for _, value := range []int{-1, 0, 129} {
		cfg.Server.MaxDumpMB = &value
		if err := ValidateConfig(cfg); err == nil {
			t.Fatalf("accepted invalid capture limit %d", value)
		}
	}
}

func TestLoadConfigRejectsUnexportableCaptureLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  max_dump_mb: 2048\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("accepted a capture limit that offline evidence import cannot support")
	}
}
