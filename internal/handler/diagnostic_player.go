package handler

import (
	"time"

	"streamanalyzer/internal/diagnostic"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var diagnosticPlayerUpgrader = websocket.Upgrader{HandshakeTimeout: 2 * time.Second}

// RegisterDiagnosticPlayerMedia keeps playback on the diagnostic workbench's
// loopback origin and requires the per-run grant for each media request.
func RegisterDiagnosticPlayerMedia(api *gin.RouterGroup, service *diagnostic.Service) {
	api.GET("/runs/:id/player/stream", func(c *gin.Context) {
		player, err := service.PlayerSession(c.Param("id"), c.Query("token"))
		if err != nil {
			diagnosticError(c, err)
			return
		}
		if player.Transport() == "hls" {
			diagnosticError(c, diagnostic.ErrNotFound)
			return
		}
		serveDiagnosticPlayerSocket(c, player)
	})
	proxy := func(c *gin.Context) {
		player, err := service.PlayerSession(c.Param("id"), c.Query("token"))
		if err != nil {
			diagnosticError(c, err)
			return
		}
		player.ServeHLS(c.Writer, c.Request, c.Param("resource"))
	}
	api.GET("/runs/:id/player/hls/:resource", proxy)
	api.HEAD("/runs/:id/player/hls/:resource", proxy)
}

func serveDiagnosticPlayerSocket(c *gin.Context, player *diagnostic.PlayerSession) {
	ws, err := diagnosticPlayerUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	closeSocket := func(code int, reason string) {
		_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(250*time.Millisecond))
	}
	previewClose := func() bool {
		if reason := player.PreviewFailure(); reason != "" {
			code := websocket.CloseInternalServerErr
			if reason == "codec_unsupported" {
				code = websocket.CloseUnsupportedData
			}
			if reason == "queue_limit" {
				code = websocket.CloseTryAgainLater
			}
			closeSocket(code, "rtsp_preview:"+reason)
			return true
		}
		return false
	}
	bootstrap, live, unsubscribe, err := player.Subscribe()
	if err != nil {
		if !previewClose() {
			closeSocket(websocket.ClosePolicyViolation, "playback stream unavailable")
		}
		return
	}
	defer unsubscribe()

	disconnected := make(chan struct{})
	ws.SetReadLimit(1024)
	go func() {
		defer close(disconnected)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	write := func(data []byte) bool {
		if err := ws.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return false
		}
		return ws.WriteMessage(websocket.BinaryMessage, data) == nil
	}
	for _, data := range bootstrap {
		select {
		case <-disconnected:
			return
		case <-player.Done():
			return
		default:
		}
		if !write(data) {
			return
		}
	}
	for {
		select {
		case <-disconnected:
			return
		case <-player.Done():
			return
		case data, more := <-live:
			if !more {
				if previewClose() {
					return
				}
				if player.StreamDropped() {
					closeSocket(websocket.CloseTryAgainLater, "playback buffer limit")
				} else {
					closeSocket(websocket.CloseNormalClosure, "capture finished")
				}
				return
			}
			if !write(data) {
				return
			}
		}
	}
}
