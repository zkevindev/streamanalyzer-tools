package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"streamanalyzer/internal/analyzer"
	"streamanalyzer/internal/handler"
	"streamanalyzer/internal/storage"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Addr    string `yaml:"addr"`
		DataDir string `yaml:"data_dir"`
	} `yaml:"server"`
}

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

	streamAnalyzer := analyzer.NewStreamAnalyzer(xlsxStorage)

	h := handler.NewHandler(streamAnalyzer, xlsxStorage)

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
