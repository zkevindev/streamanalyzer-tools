package desktop

import (
	"context"
	"crypto/rand"
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"
	"time"

	"streamanalyzer/internal/appserver"
)

type App struct {
	ctx                context.Context
	cancel             context.CancelFunc
	server             *appserver.Server
	startupError       string
	modelSettings      *modelSettings
	modelSettingsToken string
	updates            *desktopUpdates
	updateToken        string
}

func NewApp() *App {
	ctx, cancel := context.WithCancel(context.Background())
	return &App{ctx: ctx, cancel: cancel}
}

func (a *App) Startup(dataDirectory string) error {
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
		return err
	}
	cfg := &appserver.Config{}
	cfg.Server.DataDir = dataDirectory
	settings, model, _ := openModelSettings(dataDirectory, appserver.ModelConfigFromEnv())
	server, err := appserver.StartWithModel(a.ctx, "127.0.0.1:0", cfg, model)
	if err == nil {
		a.server = server
		a.modelSettings = settings
		if settings != nil {
			a.modelSettingsToken = rand.Text()
		}
	}
	return err
}

func (a *App) Shutdown() {
	if a.updates != nil {
		a.updates.close()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = a.server.Shutdown(shutdownCtx)
	a.cancel()
}

type Shell struct {
	App      *App
	SaveFile func(string) (string, error)
}

func (s Shell) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/desktop-update.js" {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write([]byte(updateScript))
		return
	}
	if r.URL.Path == "/desktop-update" || strings.HasPrefix(r.URL.Path, "/desktop-update/") {
		s.serveUpdates(w, r)
		return
	}
	if r.URL.Path == "/model-settings" || r.URL.Path == "/model-settings/test" {
		s.serveModelSettings(w, r)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/save-artifact" && s.SaveFile != nil {
		s.download(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if s.App.startupError != "" {
		_, _ = w.Write([]byte(errorHTML(s.App.startupError)))
		return
	}
	_, _ = w.Write([]byte(shellHTML(s.App.server.URL(), s.App.modelSettingsToken, s.App.updateToken)))
}

func errorHTML(message string) string {
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Stream Analyzer</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;font:14px system-ui,sans-serif;background:#0f172a;color:#e5e7eb}.box{max-width:680px;padding:28px;border:1px solid #334155;border-radius:12px;background:#1e293b}h1{margin:0 0 12px;font-size:18px;color:#fca5a5}p{margin:0;line-height:1.7;white-space:pre-wrap}</style></head><body><div class="box"><h1>Stream Analyzer 启动失败</h1><p>%s</p></div></body></html>`, html.EscapeString(message))
}
