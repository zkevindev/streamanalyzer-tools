package handler

import (
	"errors"
	"net/http"

	"streamanalyzer/internal/simulator/onvif"

	"github.com/gin-gonic/gin"
)

func registerONVIFRoutes(r *gin.Engine, simulator *onvif.Manager) {
	r.GET("/simulators/onvif", localDiagnostics, func(c *gin.Context) {
		serveEmbeddedStaticFile(c, "simulator-onvif.html")
	})
	api := r.Group("/api/simulators/onvif", localDiagnostics)
	api.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, simulator.Status())
	})
	api.GET("/interfaces", func(c *gin.Context) {
		addresses, err := onvif.Addresses()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法读取本机 IPv4 网卡"})
			return
		}
		c.JSON(http.StatusOK, addresses)
	})
	api.POST("/start", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		var cfg onvif.Config
		if err := c.ShouldBindJSON(&cfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ONVIF 配置格式无效"})
			return
		}
		status, err := simulator.Start(cfg)
		if err != nil {
			code := http.StatusServiceUnavailable
			switch {
			case errors.Is(err, onvif.ErrActive):
				code = http.StatusConflict
			case errors.Is(err, onvif.ErrConfig):
				code = http.StatusBadRequest
			}
			c.JSON(code, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, status)
	})
	api.POST("/stop", func(c *gin.Context) {
		c.JSON(http.StatusOK, simulator.Stop())
	})
}
