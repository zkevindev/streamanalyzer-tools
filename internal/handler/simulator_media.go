package handler

import (
	"errors"
	"net/http"

	"streamanalyzer/internal/simulator/mediasource"

	"github.com/gin-gonic/gin"
)

func registerMediaSimulatorRoutes(r *gin.Engine, sim *mediasource.Manager) {
	r.GET("/simulators/media", localDiagnostics, func(c *gin.Context) {
		serveEmbeddedStaticFile(c, "simulator-media.html")
	})
	api := r.Group("/api/simulators/media", localDiagnostics)
	api.GET("", func(c *gin.Context) { c.JSON(http.StatusOK, sim.Status()) })
	api.POST("/start", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		var config mediasource.Config
		if err := c.ShouldBindJSON(&config); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": mediasource.ErrInvalid.Error()})
			return
		}
		status, err := sim.Start(config)
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
	api.POST("/send", func(c *gin.Context) {
		status, err := sim.Send()
		if err != nil {
			code := http.StatusServiceUnavailable
			if errors.Is(err, mediasource.ErrActive) || errors.Is(err, mediasource.ErrSenderInactive) {
				code = http.StatusConflict
			}
			c.JSON(code, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, status)
	})
	api.POST("/stop", func(c *gin.Context) { c.JSON(http.StatusOK, sim.Stop()) })
	serve := func(c *gin.Context) { sim.Serve(c.Writer, c.Request, c.Param("id")) }
	api.GET("/stream/:id", serve)
	api.HEAD("/stream/:id", serve)
}
