package appserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"streamanalyzer/internal/analyzer"
	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/handler"
	"streamanalyzer/internal/offline"
	"streamanalyzer/internal/simulator/gb28181"
	"streamanalyzer/internal/storage"

	"github.com/gin-gonic/gin"
)

type Server struct {
	httpServer  *http.Server
	storage     *storage.CSVStorage
	diagnostics *diagnostic.Service
	simulator   *gb28181.Manager
	addr        string
	close       sync.Once
	closeErr    error
}

func newEngine(cfg *Config, model diagnostic.Explainer, simulator *gb28181.Manager) (*gin.Engine, *storage.CSVStorage, *diagnostic.Service, error) {
	ApplyDefaults(cfg)

	xlsxStorage, err := storage.NewCSVStorage(cfg.Server.DataDir)
	if err != nil {
		return nil, nil, nil, err
	}

	diagnostics, err := diagnostic.Open(cfg.Server.DataDir, model)
	if err != nil {
		xlsxStorage.Close()
		return nil, nil, nil, err
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
	handler.RegisterDiagnosticRoutes(r, diagnostics)
	handler.RegisterSimulatorRoutes(r, simulator)
	return r, xlsxStorage, diagnostics, nil
}

func Run(addr string, cfg *Config) error {
	model, err := diagnosticModel()
	if err != nil {
		return err
	}
	simulator := &gb28181.Manager{}
	engine, xlsxStorage, diagnostics, err := newEngine(cfg, model, simulator)
	if err != nil {
		return err
	}
	defer xlsxStorage.Close()
	defer diagnostics.Close()
	defer simulator.Close()

	log.Printf("Server starting on %s", addr)
	return engine.Run(addr)
}

func Start(ctx context.Context, addr string, cfg *Config) (*Server, error) {
	model, err := diagnosticModel()
	if err != nil {
		return nil, err
	}
	return StartWithModel(ctx, addr, cfg, model)
}

func StartWithModel(ctx context.Context, addr string, cfg *Config, model diagnostic.Explainer) (*Server, error) {
	simulator := &gb28181.Manager{}
	engine, xlsxStorage, diagnostics, err := newEngine(cfg, model, simulator)
	if err != nil {
		return nil, err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		diagnostics.Close()
		xlsxStorage.Close()
		return nil, err
	}

	s := &Server{
		httpServer:  &http.Server{Handler: engine},
		storage:     xlsxStorage,
		diagnostics: diagnostics,
		simulator:   simulator,
		addr:        ln.Addr().String(),
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

func (s *Server) SetDiagnosticModel(model diagnostic.Explainer) {
	s.diagnostics.SetModel(model)
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
	s.close.Do(func() {
		s.closeErr = s.httpServer.Shutdown(ctx)
		s.simulator.Close()
		_ = s.diagnostics.Close()
		_ = s.storage.Close()
	})
	return s.closeErr
}
