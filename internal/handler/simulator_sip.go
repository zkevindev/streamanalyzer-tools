package handler

import (
	"errors"
	"net/http"

	"streamanalyzer/internal/simulator/sipphone"

	"github.com/gin-gonic/gin"
)

func registerSIPRoutes(r *gin.Engine, simulator *sipphone.Manager) {
	r.GET("/simulators/sip", localDiagnostics, func(c *gin.Context) {
		serveEmbeddedStaticFile(c, "simulator-sip.html")
	})
	api := r.Group("/api/simulators/sip", localDiagnostics)
	api.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, simulator.Status())
	})
	api.POST("/start", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		var cfg sipphone.Config
		if err := c.ShouldBindJSON(&cfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "SIP 配置格式无效"})
			return
		}
		status, err := simulator.Start(cfg)
		if err != nil {
			sipActionError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, status)
	})
	api.POST("/stop", func(c *gin.Context) {
		c.JSON(http.StatusAccepted, simulator.Stop())
	})
	api.POST("/dial", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		var input struct {
			Target string `json:"target"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "被叫号码格式无效"})
			return
		}
		if err := simulator.Dial(input.Target); err != nil {
			sipActionError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, simulator.Status())
	})
	api.POST("/answer", sipCallAction(simulator, simulator.Answer))
	api.POST("/hangup", sipCallAction(simulator, simulator.Hangup))
	api.GET("/received.wav", func(c *gin.Context) {
		wav, err := simulator.ReceivedAudio()
		if err != nil {
			sipActionError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "audio/wav", wav)
	})
}

func sipCallAction(simulator *sipphone.Manager, action func() error) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := action(); err != nil {
			sipActionError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, simulator.Status())
	}
}

func sipActionError(c *gin.Context, err error) {
	code := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, sipphone.ErrConfig):
		code = http.StatusBadRequest
	case errors.Is(err, sipphone.ErrConflict):
		code = http.StatusConflict
	case errors.Is(err, sipphone.ErrNoAudio):
		code = http.StatusNotFound
	}
	c.JSON(code, gin.H{"error": err.Error()})
}
