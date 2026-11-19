package handler

import (
	"encoding/json"
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
		respondONVIFStart(c, status, err)
	})
	api.POST("/start-file", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*onvif.MaxVideoBytes+(64<<10))
		if err := c.Request.ParseMultipartForm(256 << 10); err != nil {
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "每路本地视频最多 64 MiB"})
			} else {
				c.JSON(http.StatusBadRequest, gin.H{"error": "无法读取 ONVIF 本地视频"})
			}
			return
		}
		form := c.Request.MultipartForm
		defer form.RemoveAll()
		var cfg onvif.Config
		if values := form.Value["config"]; len(values) != 1 || len(values[0]) > 4096 || json.Unmarshal([]byte(values[0]), &cfg) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ONVIF 配置格式无效"})
			return
		}
		var files [2]onvif.LocalVideo
		for i, name := range [2]string{"main", "sub"} {
			parts := form.File[name]
			if len(parts) == 0 {
				continue
			}
			if len(parts) > 1 || parts[0].Size > onvif.MaxVideoBytes {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "每路最多选择一个不超过 64 MiB 的视频"})
				return
			}
			file, err := parts[0].Open()
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "无法读取 ONVIF 本地视频"})
				return
			}
			defer file.Close()
			files[i] = onvif.LocalVideo{Name: parts[0].Filename, Reader: file}
		}
		status, err := simulator.StartFiles(c.Request.Context(), cfg, files)
		respondONVIFStart(c, status, err)
	})
	api.POST("/stop", func(c *gin.Context) {
		c.JSON(http.StatusOK, simulator.Stop())
	})
}

func respondONVIFStart(c *gin.Context, status onvif.Status, err error) {
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
}
