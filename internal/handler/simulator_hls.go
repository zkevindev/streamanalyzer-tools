package handler

import (
	"errors"
	"net/http"

	"streamanalyzer/internal/simulator/hlsorigin"

	"github.com/gin-gonic/gin"
)

func registerHLSRoutes(r *gin.Engine, source *hlsorigin.Manager) {
	r.GET("/simulators/hls", localDiagnostics, func(c *gin.Context) {
		serveEmbeddedStaticFile(c, "simulator-hls.html")
	})
	api := r.Group("/api/simulators/hls", localDiagnostics)
	api.GET("", func(c *gin.Context) { c.JSON(http.StatusOK, source.Status()) })
	api.POST("/start", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		var cfg hlsorigin.Config
		if err := c.ShouldBindJSON(&cfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": hlsorigin.ErrConfig.Error()})
			return
		}
		status, err := source.Start(cfg)
		if err != nil {
			code := http.StatusServiceUnavailable
			if errors.Is(err, hlsorigin.ErrActive) {
				code = http.StatusConflict
			} else if errors.Is(err, hlsorigin.ErrConfig) {
				code = http.StatusBadRequest
			}
			c.JSON(code, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, status)
	})
	api.POST("/freeze", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		var input struct {
			Frozen *bool `json:"frozen"`
		}
		if err := c.ShouldBindJSON(&input); err != nil || input.Frozen == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "冻结参数无效"})
			return
		}
		status, err := source.Freeze(*input.Frozen)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, status)
	})
	api.POST("/live-fault", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		var config hlsorigin.LiveFaultConfig
		if err := c.ShouldBindJSON(&config); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": hlsorigin.ErrConfig.Error()})
			return
		}
		status, err := source.ArmLiveFault(config)
		if err != nil {
			code := http.StatusConflict
			if errors.Is(err, hlsorigin.ErrConfig) {
				code = http.StatusBadRequest
			}
			c.JSON(code, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, status)
	})
	api.POST("/part-delay", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		var config struct {
			DelayMS int `json:"delay_ms"`
		}
		if err := c.ShouldBindJSON(&config); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": hlsorigin.ErrConfig.Error()})
			return
		}
		status, err := source.ArmPartDelay(config.DelayMS)
		if err != nil {
			code := http.StatusConflict
			if errors.Is(err, hlsorigin.ErrConfig) {
				code = http.StatusBadRequest
			}
			c.JSON(code, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, status)
	})
	api.POST("/key-restore/:id", func(c *gin.Context) {
		status, err := source.RestoreKey(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, status)
	})
	api.POST("/stop", func(c *gin.Context) { c.JSON(http.StatusOK, source.Stop()) })
	serve := func(c *gin.Context) { source.Serve(c.Writer, c.Request, c.Param("id"), c.Param("file")) }
	api.GET("/stream/:id/:file", serve)
	api.HEAD("/stream/:id/:file", serve)
}
