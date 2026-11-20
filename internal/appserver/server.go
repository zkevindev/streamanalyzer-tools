package appserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"streamanalyzer/internal/diagnostic"
	"streamanalyzer/internal/handler"
	"streamanalyzer/internal/offline"
	"streamanalyzer/internal/onvifdiscovery"
	"streamanalyzer/internal/simulator/gb28181"
	"streamanalyzer/internal/simulator/hlsorigin"
	"streamanalyzer/internal/simulator/mediasource"
	"streamanalyzer/internal/simulator/onvif"
	"streamanalyzer/internal/simulator/sipphone"
	"streamanalyzer/internal/simulator/whep"

	"github.com/gin-gonic/gin"
)

type simulators struct {
	whep       whep.Manager
	rtmp       mediasource.RTMPManager
	media      mediasource.Manager
	gb28181    gb28181.Manager
	gbPlatform gb28181.PlatformManager
	onvif      onvif.Manager
	hls        hlsorigin.Manager
	sip        sipphone.Manager
	discovery  onvifdiscovery.Manager
}

func (s *simulators) Close() {
	s.whep.Close()
	s.rtmp.Close()
	s.media.Close()
	s.discovery.Close()
	s.hls.Close()
	s.sip.Close()
	s.onvif.Close()
	s.gb28181.Close()
	s.gbPlatform.Close()
}

type Server struct {
	httpServer  *http.Server
	diagnostics *diagnostic.Service
	simulators  *simulators
	addr        string
	close       sync.Once
	closeErr    error
}

func newEngine(cfg *Config, model diagnostic.Explainer, sim *simulators) (*gin.Engine, *diagnostic.Service, error) {
	ApplyDefaults(cfg)

	diagnostics, err := diagnostic.Open(cfg.Server.DataDir, model)
	if err != nil {
		return nil, nil, err
	}

	offlineMgr := offline.NewManager(cfg.Server.DataDir)
	offlineMgr.LoadExisting()
	h := handler.NewHandler(offlineMgr)

	r := gin.Default()
	h.RegisterRoutes(r)
	handler.RegisterDiagnosticRoutes(r, diagnostics)
	handler.RegisterONVIFDiscoveryRoutes(r, &sim.discovery)
	handler.RegisterSimulatorHistoryRoutes(r, cfg.Server.DataDir)
	handler.RegisterSimulatorRoutes(r, &sim.gb28181, &sim.onvif, &sim.sip, &sim.hls, &sim.gbPlatform, &sim.media, &sim.rtmp, &sim.whep)
	return r, diagnostics, nil
}

func Run(addr string, cfg *Config) error {
	model, err := diagnosticModel()
	if err != nil {
		return err
	}
	sim := &simulators{}
	engine, diagnostics, err := newEngine(cfg, model, sim)
	if err != nil {
		return err
	}
	defer diagnostics.Close()
	defer sim.Close()

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
	sim := &simulators{}
	engine, diagnostics, err := newEngine(cfg, model, sim)
	if err != nil {
		return nil, err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		diagnostics.Close()
		return nil, err
	}

	s := &Server{
		httpServer:  &http.Server{Handler: engine},
		diagnostics: diagnostics,
		simulators:  sim,
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
		s.diagnostics.StopFLVReplays()
		s.closeErr = s.httpServer.Shutdown(ctx)
		s.simulators.Close()
		_ = s.diagnostics.Close()
	})
	return s.closeErr
}
