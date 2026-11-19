package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"streamanalyzer/internal/simulator/gb28181"
	"streamanalyzer/internal/simulator/onvif"
	"streamanalyzer/internal/simulator/sipphone"

	"github.com/gin-gonic/gin"
)

func RegisterSimulatorRoutes(r *gin.Engine, simulator *gb28181.Manager, onvifSimulator *onvif.Manager, sipSimulator *sipphone.Manager) {
	r.GET("/simulators", localDiagnostics, func(c *gin.Context) {
		serveEmbeddedStaticFile(c, "simulators.html")
	})
	registerONVIFRoutes(r, onvifSimulator)
	registerSIPRoutes(r, sipSimulator)
	api := r.Group("/api/simulators/gb28181", localDiagnostics)
	api.GET("", func(c *gin.Context) {
		status, ok := simulator.StatusFor(c.Query("device_id"), c.Query("channel_id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "当前批次中没有该设备或通道"})
			return
		}
		c.JSON(http.StatusOK, status)
	})
	api.GET("/trace.json", func(c *gin.Context) {
		snapshot, ok := simulator.StatusFor(c.Query("device_id"), c.Query("channel_id"))
		if !ok || len(snapshot.Events) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "暂无可导出的 SIP 信令"})
			return
		}
		c.Header("Content-Disposition", `attachment; filename="gb28181-trace.json"`)
		c.IndentedJSON(http.StatusOK, snapshot)
	})
	api.GET("/sessions/:id/evidence.zip", func(c *gin.Context) {
		var output bytes.Buffer
		if err := simulator.SessionArchive(c.Param("id"), &output); err != nil {
			if errors.Is(err, gb28181.ErrSessionNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "无法生成 GB28181 证据包"})
			}
			return
		}
		c.Header("Content-Disposition", "attachment; filename=gb28181-evidence.zip")
		c.Data(http.StatusOK, "application/zip", output.Bytes())
	})
	api.POST("/evidence/import", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gb28181.MaxEvidenceBytes)
		report, err := gb28181.ImportEvidence(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, report)
	})
	api.POST("/start", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var cfg gb28181.Config
		if err := c.ShouldBindJSON(&cfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "模拟器配置格式无效"})
			return
		}
		if err := simulator.Start(cfg); err != nil {
			simulatorStartError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, simulator.Status())
	})
	api.POST("/start-file", func(c *gin.Context) {
		startSimulatorFile(c, simulator)
	})
	api.POST("/stop", func(c *gin.Context) {
		simulator.Stop()
		status, ok := simulator.StatusFor(c.Query("device_id"), c.Query("channel_id"))
		if !ok {
			status = simulator.Status()
		}
		c.JSON(http.StatusAccepted, status)
	})
}

func startSimulatorFile(c *gin.Context, simulator *gb28181.Manager) {
	const requestLimit = gb28181.MaxLocalVideoBytes + (64 << 10)
	if c.Request.ContentLength > requestLimit {
		simulatorStartError(c, gb28181.ErrVideoTooLarge)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, requestLimit)
	form, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择本地视频文件"})
		return
	}
	config, err := form.NextPart()
	if err != nil || config.FormName() != "config" || config.FileName() != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "模拟器配置格式无效"})
		return
	}
	const configLimit = 16 << 10
	body, err := io.ReadAll(io.LimitReader(config, configLimit+1))
	var cfg gb28181.Config
	if err != nil || len(body) > configLimit || json.Unmarshal(body, &cfg) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "模拟器配置格式无效"})
		return
	}
	file, err := form.NextPart()
	if err != nil || file.FormName() != "video" || file.FileName() == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先选择本地视频文件"})
		return
	}
	if err := simulator.StartFile(c.Request.Context(), cfg, file.FileName(), file); err != nil {
		simulatorStartError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, simulator.Status())
}

func simulatorStartError(c *gin.Context, err error) {
	status, message := http.StatusBadRequest, err.Error()
	var maxBytes *http.MaxBytesError
	switch {
	case errors.Is(err, gb28181.ErrVideoTooLarge), errors.As(err, &maxBytes):
		status, message = http.StatusRequestEntityTooLarge, gb28181.ErrVideoTooLarge.Error()
	case errors.Is(err, gb28181.ErrActive):
		status = http.StatusConflict
	case errors.Is(err, gb28181.ErrClosed):
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"error": message})
}
