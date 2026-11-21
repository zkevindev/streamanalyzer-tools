package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"streamanalyzer/internal/simulator/pusher"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var pusherWSUpgrader = websocket.Upgrader{
	ReadBufferSize:  64 * 1024,
	WriteBufferSize: 16 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		return err == nil && strings.EqualFold(u.Host, r.Host)
	},
}

func registerPusherSimulatorRoutes(r *gin.Engine, mgr *pusher.Manager) {
	r.GET("/simulators/pusher", localDiagnostics, func(c *gin.Context) {
		serveEmbeddedStaticFile(c, "simulator-pusher.html")
	})

	api := r.Group("/api/simulators/pusher", localDiagnostics)

	api.GET("/tasks", func(c *gin.Context) {
		tasks := mgr.List()
		c.JSON(http.StatusOK, gin.H{
			"tasks":     tasks,
			"max_tasks": pusher.MaxActiveTasks,
		})
	})

	api.GET("/tasks/:id", func(c *gin.Context) {
		task, ok := mgr.Get(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": pusher.ErrNotFound.Error()})
			return
		}
		c.JSON(http.StatusOK, task)
	})

	api.POST("/tasks", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		var cfg pusher.Config
		if err := c.ShouldBindJSON(&cfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "推流配置 JSON 无效"})
			return
		}
		task, err := mgr.Start(cfg)
		if err != nil {
			writePusherError(c, err)
			return
		}
		c.JSON(http.StatusCreated, task)
	})

	api.POST("/tasks/upload", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, pusher.MaxUploadBytes+(2<<20))
		mr, err := c.Request.MultipartReader()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请使用 multipart/form-data 上传录像文件"})
			return
		}

		var cfg pusher.Config
		var hasConfig bool
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "未找到上传的录像文件字段 file"})
				return
			}
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "读取上传表单失败"})
				return
			}
			name := part.FormName()
			if name == "config" {
				raw, readErr := io.ReadAll(io.LimitReader(part, 64<<10))
				_ = part.Close()
				if readErr != nil || json.Unmarshal(raw, &cfg) != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "推流配置 JSON 无效"})
					return
				}
				hasConfig = true
				continue
			}
			if name == "file" {
				if !hasConfig {
					_ = part.Close()
					c.JSON(http.StatusBadRequest, gin.H{"error": "请先传入 config 字段再上传 file"})
					return
				}
				task, startErr := mgr.StartUpload(cfg, part.FileName(), part)
				_ = part.Close()
				if startErr != nil {
					writePusherError(c, startErr)
					return
				}
				c.JSON(http.StatusCreated, task)
				return
			}
			_ = part.Close()
		}
	})

	api.POST("/tasks/:id/stop", func(c *gin.Context) {
		task, err := mgr.Stop(c.Param("id"))
		if err != nil {
			writePusherError(c, err)
			return
		}
		c.JSON(http.StatusOK, task)
	})

	api.DELETE("/tasks/:id", func(c *gin.Context) {
		if err := mgr.Delete(c.Param("id")); err != nil {
			writePusherError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": true})
	})

	api.POST("/stop-all", func(c *gin.Context) {
		stopped := mgr.StopAll()
		c.JSON(http.StatusOK, gin.H{"stopped": stopped})
	})

	// WebSocket endpoint for browser camera/screen capture -> FFmpeg -> RTMP/RTSP
	api.GET("/tasks/:id/ingest", func(c *gin.Context) {
		taskID := c.Param("id")
		if _, ok := mgr.Get(taskID); !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": pusher.ErrNotFound.Error()})
			return
		}
		conn, err := pusherWSUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		if err := mgr.AttachIngest(taskID, conn); err != nil {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, err.Error()), time.Now().Add(time.Second))
			_ = conn.Close()
		}
	})

	// Local WHIP signaling proxy for browser WebRTC (webcam/screen) direct WHIP publishing.
	api.POST("/whip-proxy", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
		var req struct {
			TargetURL   string `json:"target_url"`
			BearerToken string `json:"bearer_token"`
			OfferSDP    string `json:"offer_sdp"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "WHIP 代理请求格式无效"})
			return
		}
		answerSDP, resourceURL, statusCode, err := pusher.ProxyWHIPOffer(c.Request.Context(), req.TargetURL, req.BearerToken, req.OfferSDP)
		if err != nil {
			writePusherError(c, err)
			return
		}
		if statusCode < 200 || statusCode >= 300 {
			c.JSON(http.StatusBadGateway, gin.H{
				"error":       strings.TrimSpace(answerSDP),
				"status_code": statusCode,
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"answer_sdp":   answerSDP,
			"resource_url": resourceURL,
			"status_code":  statusCode,
		})
	})

	api.DELETE("/whip-proxy", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var req struct {
			ResourceURL string `json:"resource_url"`
			BearerToken string `json:"bearer_token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "WHIP 释放请求格式无效"})
			return
		}
		if err := pusher.ProxyWHIPDelete(c.Request.Context(), req.ResourceURL, req.BearerToken); err != nil {
			writePusherError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": true})
	})
}

func writePusherError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, pusher.ErrInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, pusher.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, pusher.ErrLimitReached), errors.Is(err, pusher.ErrTaskRunning):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, pusher.ErrUploadTooBig):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": err.Error()})
	case errors.Is(err, pusher.ErrClosed):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
