package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"streamanalyzer/internal/onvifdiscovery"
)

func RegisterONVIFDiscoveryRoutes(r *gin.Engine, manager *onvifdiscovery.Manager) {
	api := r.Group("/api/diagnostics/onvif", localDiagnostics)
	api.GET("/interfaces", func(c *gin.Context) {
		value, err := onvifdiscovery.Interfaces()
		if err != nil {
			onvifDiscoveryError(c, err)
			return
		}
		c.JSON(http.StatusOK, value)
	})
	api.POST("/discoveries", func(c *gin.Context) {
		value, ok := onvifDiscoveryRequest[onvifdiscovery.DiscoverRequest](c)
		if !ok {
			return
		}
		result, err := manager.Start(value)
		if err != nil {
			onvifDiscoveryError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, result)
	})
	api.GET("/discoveries/:id", func(c *gin.Context) {
		value, err := manager.Get(c.Param("id"))
		if err != nil {
			onvifDiscoveryError(c, err)
			return
		}
		c.JSON(http.StatusOK, value)
	})
	api.POST("/discoveries/:id/stop", func(c *gin.Context) {
		value, err := manager.Stop(c.Param("id"))
		if err != nil {
			onvifDiscoveryError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, value)
	})
	api.POST("/profiles", func(c *gin.Context) {
		value, ok := onvifDiscoveryRequest[onvifdiscovery.ProfilesRequest](c)
		if !ok {
			return
		}
		result, err := manager.Profiles(c.Request.Context(), value)
		if err != nil {
			onvifDiscoveryError(c, err)
			return
		}
		c.JSON(http.StatusOK, result)
	})
	api.POST("/stream", func(c *gin.Context) {
		value, ok := onvifDiscoveryRequest[onvifdiscovery.StreamRequest](c)
		if !ok {
			return
		}
		result, err := manager.Stream(c.Request.Context(), value)
		if err != nil {
			onvifDiscoveryError(c, err)
			return
		}
		c.JSON(http.StatusOK, result)
	})
}

func onvifDiscoveryRequest[T any](c *gin.Context) (T, bool) {
	var value T
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ONVIF 请求格式无效", "code": "invalid_input"})
		return value, false
	}
	return value, true
}

func onvifDiscoveryError(c *gin.Context, err error) {
	var known *onvifdiscovery.Error
	if !errors.As(err, &known) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ONVIF 服务内部错误", "code": "internal_error"})
		return
	}
	status := http.StatusBadGateway
	switch known.Code {
	case "invalid_input", "endpoint_scope", "stream_url_invalid":
		status = http.StatusBadRequest
	case "not_found":
		status = http.StatusNotFound
	case "busy", "closed":
		status = http.StatusConflict
	case "auth_failed":
		status = http.StatusUnauthorized
	case "canceled":
		status = http.StatusRequestTimeout
	case "timeout":
		status = http.StatusGatewayTimeout
	}
	c.JSON(status, known)
}
