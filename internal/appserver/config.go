package appserver

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Addr    string `yaml:"addr"`
		DataDir string `yaml:"data_dir"`
		// MaxDumpMB caps the raw dump of a single task. A live pull dumps
		// indefinitely otherwise (a 4 Mbps stream is ~1.8 GB/hour).
		// Omit for the default; set to 0 for unlimited. It is a pointer so an
		// explicit 0 is distinguishable from the field being absent.
		MaxDumpMB *int `yaml:"max_dump_mb"`
	} `yaml:"server"`
}

const defaultMaxDumpMB = 2048

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
	return &cfg, nil
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
	} else if *cfg.Server.MaxDumpMB < 0 {
		*cfg.Server.MaxDumpMB = 0
	}
}
