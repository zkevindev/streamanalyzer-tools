package appserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"

	"streamanalyzer/internal/analyzer"
	"streamanalyzer/internal/handler"
	"streamanalyzer/internal/offline"
	"streamanalyzer/internal/storage"

	"github.com/gin-gonic/gin"
)

type Server struct {
	httpServer *http.Server
	storage    *storage.CSVStorage
	addr       string
}

func NewEngine(cfg *Config) (*gin.Engine, *storage.CSVStorage, error) {
	ApplyDefaults(cfg)

	xlsxStorage, err := storage.NewCSVStorage(cfg.Server.DataDir)
	if err != nil {
		return nil, nil, err
	}

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
	return r, xlsxStorage, nil
}

func Run(addr string, cfg *Config) error {
	engine, xlsxStorage, err := NewEngine(cfg)
	if err != nil {
		return err
	}
	defer xlsxStorage.Close()

	log.Printf("Server starting on %s", addr)
	return engine.Run(addr)
}

func Start(ctx context.Context, addr string, cfg *Config) (*Server, error) {
	engine, xlsxStorage, err := NewEngine(cfg)
	if err != nil {
		return nil, err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		xlsxStorage.Close()
		return nil, err
	}

	s := &Server{
		httpServer: &http.Server{Handler: engine},
		storage:    xlsxStorage,
		addr:       ln.Addr().String(),
	}

	go func() {
		<-ctx.Done()
		_ = s.Shutdown(context.Background())
	}()

	go func() {
		log.Printf("Server starting on %s", s.addr)
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("Server failed: %v", err)
		}
	}()

	return s, nil
}

func (s *Server) URL() string {
	if s == nil || s.addr == "" {
		return ""
	}
	return fmt.Sprintf("http://%s", s.addr)
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if s.storage != nil {
		s.storage.Close()
	}
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}
