package handler

import (
	"errors"
	"net/http"

	"streamanalyzer/internal/simulator/gb28181"

	"github.com/gin-gonic/gin"
)

func registerGBPlatformRoutes(r *gin.Engine, platform *gb28181.PlatformManager, devices *gb28181.Manager) {
	r.GET("/simulators/gb28181/platform", localDiagnostics, func(c *gin.Context) {
		serveEmbeddedStaticFile(c, "simulator-gb-platform.html")
	})
	api := r.Group("/api/simulators/gb28181/platform", localDiagnostics)
	status := func() gb28181.PlatformStatus {
		result := platform.Status()
		if result.Preview != nil {
			if device, ok := devices.StatusFor(result.DeviceID, result.Preview.ChannelID); ok {
				for _, session := range device.Sessions {
					if session.CallID == result.Preview.CallID {
						result.DeviceSession = &session
					}
					if result.Recovery != nil && session.CallID == result.Recovery.Before.CallID {
						result.Recovery.PreviousSession = &session
					}
				}
			}
		}
		return result
	}
	api.GET("", func(c *gin.Context) { c.JSON(http.StatusOK, status()) })
	api.GET("/trace.json", func(c *gin.Context) {
		result := status()
		if result.Preview == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "暂无平台点播证据"})
			return
		}
		c.Header("Content-Disposition", `attachment; filename="gb28181-platform-preview.json"`)
		c.IndentedJSON(http.StatusOK, result)
	})
	api.POST("/start", func(c *gin.Context) {
		var config gb28181.PlatformConfig
		if !bindGBPlatform(c, &config) {
			return
		}
		if err := platform.Start(config); err != nil {
			gbPlatformError(c, err)
			return
		}
		c.JSON(http.StatusCreated, status())
	})
	api.POST("/preview", func(c *gin.Context) {
		var config gb28181.PreviewConfig
		if !bindGBPlatform(c, &config) {
			return
		}
		if err := platform.Preview(config); err != nil {
			gbPlatformError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, status())
	})
	api.POST("/playback-control", func(c *gin.Context) {
		var action gb28181.PlaybackControl
		if !bindGBPlatform(c, &action) {
			return
		}
		if err := platform.ControlPlayback(action); err != nil {
			gbPlatformError(c, err)
			return
		}
		c.JSON(http.StatusOK, status())
	})
	api.POST("/stop", func(c *gin.Context) {
		platform.Stop()
		c.JSON(http.StatusOK, status())
	})
}

func bindGBPlatform(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "平台侧收流配置格式无效"})
		return false
	}
	return true
}

func gbPlatformError(c *gin.Context, err error) {
	code := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, gb28181.ErrPlatformConfig):
		code = http.StatusBadRequest
	case errors.Is(err, gb28181.ErrActive), errors.Is(err, gb28181.ErrPlatformPeer):
		code = http.StatusConflict
	}
	c.JSON(code, gin.H{"error": err.Error()})
}
