package handler

import (
	"errors"
	"net/http"

	"streamanalyzer/internal/simulator/mediasource"

	"github.com/gin-gonic/gin"
)

func registerRTMPSimulatorRoutes(r *gin.Engine, sim *mediasource.RTMPManager) {
	r.GET("/simulators/rtmp", localDiagnostics, func(c *gin.Context) {
		serveEmbeddedStaticFile(c, "simulator-rtmp.html")
	})
	api := r.Group("/api/simulators/rtmp", localDiagnostics)
	api.GET("", func(c *gin.Context) { c.JSON(http.StatusOK, sim.Status()) })
	api.POST("/start", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		var cfg mediasource.RTMPConfig
		if err := c.ShouldBindJSON(&cfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": mediasource.ErrInvalid.Error()})
			return
		}
		status, err := sim.Start(cfg)
		if err != nil {
			code := http.StatusServiceUnavailable
			switch {
			case errors.Is(err, mediasource.ErrInvalid):
				code = http.StatusBadRequest
			case errors.Is(err, mediasource.ErrActive):
				code = http.StatusConflict
			}
			c.JSON(code, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, status)
	})
	api.POST("/stop", func(c *gin.Context) { c.JSON(http.StatusOK, sim.Stop()) })
}
