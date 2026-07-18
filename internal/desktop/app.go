package desktop

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"streamanalyzer/internal/appserver"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx          context.Context
	cancel       context.CancelFunc
	server       *appserver.Server
	serverURL    string
	startupError string
	mu           sync.RWMutex
}

func NewApp() *App {
	return &App{}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx, a.cancel = context.WithCancel(ctx)

	defaultDirectory, _ := os.UserHomeDir()
	dataDirectory, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
		Title:                "选择 Stream Analyzer 数据目录",
		DefaultDirectory:     defaultDirectory,
		CanCreateDirectories: true,
	})
	if err != nil {
		a.setStartupError(fmt.Sprintf("选择数据目录失败：%v", err))
		return
	}
	if dataDirectory == "" {
		a.setStartupError("未选择数据目录，请重新启动应用后选择一个可写目录。")
		return
	}

	cfg := &appserver.Config{}
	appserver.ApplyDefaults(cfg)
	cfg.Server.DataDir = dataDirectory

	server, err := appserver.Start(a.ctx, "127.0.0.1:0", cfg)
	if err != nil {
		log.Printf("Failed to start embedded server: %v", err)
		a.setStartupError(fmt.Sprintf("启动内置服务失败：%v", err))
		return
	}

	a.mu.Lock()
	a.server = server
	a.serverURL = server.URL()
	a.mu.Unlock()
}

func (a *App) setStartupError(message string) {
	a.mu.Lock()
	a.startupError = message
	a.mu.Unlock()
}

func (a *App) Shutdown(ctx context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = a.server.Shutdown(shutdownCtx)
}

func (a *App) startupState() (string, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.serverURL, a.startupError
}

type Shell struct {
	App *App
}

func (s Shell) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	serverURL, startupError := s.App.startupState()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if startupError != "" {
		_, _ = w.Write([]byte(errorHTML(startupError)))
		return
	}
	if serverURL == "" {
		_, _ = w.Write([]byte(waitingHTML()))
		return
	}
	_, _ = w.Write([]byte(shellHTML(serverURL)))
}

func errorHTML(message string) string {
	return fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Stream Analyzer</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;font:14px system-ui,sans-serif;background:#0f172a;color:#e5e7eb}.box{max-width:680px;padding:28px;border:1px solid #334155;border-radius:12px;background:#1e293b}h1{margin:0 0 12px;font-size:18px;color:#fca5a5}p{margin:0;line-height:1.7;white-space:pre-wrap}</style></head><body><div class="box"><h1>Stream Analyzer 启动失败</h1><p>%s</p></div></body></html>`, html.EscapeString(message))
}

func waitingHTML() string {
	return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Stream Analyzer</title><style>body{margin:0;height:100vh;display:grid;place-items:center;font:14px system-ui,sans-serif;background:#0f172a;color:#e5e7eb}</style></head><body>正在启动 Stream Analyzer...</body><script>setTimeout(function(){location.reload()},500)</script></html>`
}

func shellHTML(serverURL string) string {
	escapedURL := html.EscapeString(serverURL)
	return fmt.Sprintf(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Stream Analyzer</title>
  <style>
    html, body, iframe { width: 100%%; height: 100%%; margin: 0; border: 0; }
    body { overflow: hidden; background: #f8fafc; }
  </style>
</head>
<body>
  <iframe src="%s" title="Stream Analyzer"></iframe>
</body>
</html>`, escapedURL)
}
