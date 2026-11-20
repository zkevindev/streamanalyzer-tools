package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"streamanalyzer/internal/diagnostic"

	"github.com/gin-gonic/gin"
)

func RegisterDiagnosticRoutes(r *gin.Engine, service *diagnostic.Service) {
	workbench := func(c *gin.Context) { serveEmbeddedStaticFile(c, "diagnostics.html") }
	r.GET("/diagnostics", localDiagnostics, workbench)
	r.GET("/diagnostics/onvif", localDiagnostics, workbench)
	r.GET("/diagnostics/review", localDiagnostics, workbench)
	api := r.Group("/api/diagnostics", localDiagnostics)
	RegisterDiagnosticPlayerMedia(api, service)
	RegisterStallInvestigationRoutes(api, service)
	RegisterAutomaticInvestigationRoutes(api, service)
	api.POST("/runs/:id/player", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		var record diagnostic.PlayerRecording
		if err := decoder.Decode(&record); err != nil || decoder.Decode(new(any)) != io.EOF {
			diagnosticError(c, diagnostic.ErrInvalid)
			return
		}
		report, err := service.SavePlayerRecording(c.Param("id"), c.GetHeader("X-StreamAnalyzer-Player"), record)
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusOK, report)
	})
	api.GET("/config", func(c *gin.Context) {
		identity := service.ModelIdentity()
		configured := identity != (diagnostic.ModelIdentity{})
		config := gin.H{"model_configured": configured, "capture_limit_bytes": service.CaptureLimitBytes()}
		if configured {
			config["model_provider"] = identity.Provider
			config["model_name"] = identity.Model
			config["model_image_input"] = service.ModelImageInput()
		}
		c.JSON(http.StatusOK, config)
	})
	api.GET("/storage", func(c *gin.Context) {
		storage, err := service.Storage()
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusOK, storage)
	})
	api.POST("/storage/prune", func(c *gin.Context) {
		var req diagnostic.PruneRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			diagnosticError(c, diagnostic.ErrInvalid)
			return
		}
		result, err := service.Prune(req)
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusOK, result)
	})
	api.GET("/runs", func(c *gin.Context) {
		runs, err := service.List()
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusOK, runs)
	})
	api.POST("/runs", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var req diagnostic.SampleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			diagnosticError(c, diagnostic.ErrInvalid)
			return
		}
		run, err := service.Start(req)
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, run)
	})
	api.POST("/runs/delete", func(c *gin.Context) {
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			diagnosticError(c, diagnostic.ErrInvalid)
			return
		}
		if err := service.DeleteMany(req.IDs); err != nil {
			diagnosticError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	api.POST("/import", func(c *gin.Context) {
		if c.Request.ContentLength > diagnostic.MaxBundleBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "证据包超出大小上限"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, diagnostic.MaxBundleBytes)
		run, err := service.Import(c.Request.Body)
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusCreated, run)
	})
	api.GET("/runs/:id", func(c *gin.Context) {
		detail, err := service.Detail(c.Param("id"))
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusOK, detail)
	})
	api.DELETE("/runs/:id", func(c *gin.Context) {
		if err := service.Delete(c.Param("id")); err != nil {
			diagnosticError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	api.POST("/runs/:id/protection", func(c *gin.Context) {
		var req struct {
			Protected bool `json:"protected"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			diagnosticError(c, diagnostic.ErrInvalid)
			return
		}
		run, err := service.SetProtected(c.Param("id"), req.Protected)
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusOK, run)
	})
	api.POST("/runs/:id/stop", func(c *gin.Context) {
		run, err := service.Stop(c.Param("id"))
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, run)
	})
	api.POST("/runs/:id/explain", func(c *gin.Context) {
		explain := service.Explain
		if c.Query("refresh") == "1" {
			explain = service.RefreshExplanation
		}
		explanation, err := explain(c.Request.Context(), c.Param("id"))
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusOK, explanation)
	})
	api.GET("/runs/:id/report.json", func(c *gin.Context) {
		detail, err := diagnosticReport(c, service)
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.Header("Content-Disposition", `attachment; filename="report.json"`)
		c.JSON(http.StatusOK, detail.Report)
	})
	api.GET("/runs/:id/report.html", func(c *gin.Context) {
		detail, err := diagnosticReport(c, service)
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:")
		if err := diagnostic.RenderHTMLWithAssets(c.Writer, *detail.Report, detail.Explanation, "/diagnostics#"+detail.ID, service.StallImages(detail.ID, *detail.Report)); err != nil {
			diagnosticError(c, err)
		}
	})
	api.GET("/runs/:id/dump", diagnosticDump(service, ""))
	api.GET("/runs/:id/stream.flv", diagnosticDump(service, "stream.flv"))
	api.GET("/runs/:id/rtsp-replay", func(c *gin.Context) {
		c.JSON(http.StatusOK, service.StatusRTSPReplay(c.Param("id")))
	})
	api.POST("/runs/:id/rtsp-replay/start", func(c *gin.Context) {
		source, err := service.StartRTSPReplay(c.Request.Context(), c.Param("id"))
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusCreated, source)
	})
	api.POST("/runs/:id/rtsp-replay/stop", func(c *gin.Context) {
		service.StopRTSPReplay(c.Param("id"))
		c.JSON(http.StatusOK, service.StatusRTSPReplay(c.Param("id")))
	})
	api.GET("/runs/:id/replay.flv", func(c *gin.Context) {
		if err := service.ServeFLVReplay(c.Writer, c.Request, c.Param("id")); err != nil && !errors.Is(err, context.Canceled) && c.Request.Context().Err() == nil {
			diagnosticError(c, err)
		}
	})
	api.GET("/runs/:id/evidence.zip", func(c *gin.Context) {
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", `attachment; filename="stream-evidence.zip"`)
		if err := service.WriteBundle(c.Param("id"), c.Writer); err != nil {
			diagnosticError(c, err)
		}
	})
}

func diagnosticDump(service *diagnostic.Service, filename string) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, err := service.OpenDump(c.Param("id"))
		if err != nil {
			diagnosticError(c, err)
			return
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil {
			diagnosticError(c, err)
			return
		}
		name := stat.Name()
		if filename != "" && filename != name {
			diagnosticError(c, diagnostic.ErrNotFound)
			return
		}
		contentType := "application/octet-stream"
		if strings.HasSuffix(name, ".zip") {
			contentType = "application/zip"
		}
		c.Header("Content-Type", contentType)
		c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
		http.ServeContent(c.Writer, c.Request, name, stat.ModTime(), file)
	}
}

func diagnosticReport(c *gin.Context, service *diagnostic.Service) (diagnostic.Detail, error) {
	detail, err := service.Detail(c.Param("id"))
	if err == nil && detail.Report == nil {
		err = diagnostic.ErrPending
	}
	return detail, err
}

func localDiagnostics(c *gin.Context) {
	remote, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	host := c.Request.Host
	if value, _, err := net.SplitHostPort(host); err == nil {
		host = value
	}
	host = strings.Trim(host, "[]")
	if err != nil || !net.ParseIP(remote).IsLoopback() || !(strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "排障工作台仅在本机使用"})
		return
	}
	if c.Request.Method == http.MethodPost {
		if value := c.GetHeader("Origin"); value != "" {
			origin, err := url.Parse(value)
			scheme := "http"
			if c.Request.TLS != nil {
				scheme = "https"
			}
			if err != nil || origin.Scheme != scheme || !strings.EqualFold(origin.Host, c.Request.Host) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "请求来源与排障工作台不一致"})
				return
			}
		}
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Next()
}

func diagnosticError(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, "排障服务内部错误"
	switch {
	case errors.Is(err, diagnostic.ErrInvalid):
		status, message = http.StatusBadRequest, "请输入有效的播放地址、采样时长或证据包"
		if detail, ok := strings.CutPrefix(err.Error(), diagnostic.ErrInvalid.Error()+": "); ok {
			message = detail
		}
	case errors.Is(err, diagnostic.ErrNotFound):
		status, message = http.StatusNotFound, "找不到排障记录"
	case errors.Is(err, diagnostic.ErrPending):
		status, message = http.StatusConflict, "该记录尚无可用报告"
	case errors.Is(err, diagnostic.ErrModelChanged):
		status, message = http.StatusConflict, "模型配置已更新，请重新生成解读"
	case errors.Is(err, diagnostic.ErrReportChanged):
		status, message = http.StatusConflict, "排障记录已更新，请刷新报告后重试"
	case errors.Is(err, diagnostic.ErrBusy):
		status, message = http.StatusServiceUnavailable, "当前任务较多，请稍后重试"
	case errors.Is(err, diagnostic.ErrUnconfigured):
		status, message = http.StatusServiceUnavailable, "尚未配置模型服务"
	case errors.Is(err, diagnostic.ErrProvider):
		status, message = http.StatusBadGateway, "模型服务未返回可用解读"
		if detail, ok := strings.CutPrefix(err.Error(), diagnostic.ErrProvider.Error()+": "); ok {
			message += "（" + detail + "）"
		}
	default:
		slog.Error("diagnostic request", "error", err)
	}
	if !c.Writer.Written() {
		c.Header("Content-Disposition", "")
		c.JSON(status, gin.H{"error": message})
	}
}
