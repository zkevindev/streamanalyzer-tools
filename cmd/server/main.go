package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"streamanalyzer/internal/analyzer"
	"streamanalyzer/internal/handler"
	"streamanalyzer/internal/offline"
	"streamanalyzer/internal/storage"

	"github.com/gin-gonic/gin"
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

// defaultMaxDumpMB applies when max_dump_mb is absent from the config file.
const defaultMaxDumpMB = 2048

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

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
		*cfg.Server.MaxDumpMB = 0 // negative is meaningless; treat as unlimited
	}

	return &cfg, nil
}

func main() {
	configPath := flag.String("config", "config.yaml", "config file path")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	xlsxStorage, err := storage.NewCSVStorage(cfg.Server.DataDir)
	if err != nil {
		log.Fatalf("Failed to create XLSX storage: %v", err)
	}
	defer xlsxStorage.Close()

	streamAnalyzer := analyzer.NewStreamAnalyzer(xlsxStorage, *cfg.Server.MaxDumpMB)
	if *cfg.Server.MaxDumpMB > 0 {
		log.Printf("Dump limit: %d MB per task", *cfg.Server.MaxDumpMB)
	} else {
		log.Printf("Dump limit: unlimited")
	}

	offlineMgr := offline.NewManager(cfg.Server.DataDir)
	offlineMgr.LoadExisting()
	h := handler.NewHandler(streamAnalyzer, xlsxStorage, offlineMgr)

	r := gin.Default()
	h.RegisterRoutes(r)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		xlsxStorage.Close()
		os.Exit(0)
	}()

	log.Printf("Server starting on %s", cfg.Server.Addr)
	if err := r.Run(cfg.Server.Addr); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
