package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"streamanalyzer/internal/diagnostic"
)

func RegisterStallInvestigationRoutes(api *gin.RouterGroup, service *diagnostic.Service) {
	api.POST("/runs/:id/stalls", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		var request diagnostic.StallInvestigationRequest
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
			diagnosticError(c, diagnostic.ErrInvalid)
			return
		}
		item, err := service.InvestigateStall(c.Param("id"), request)
		if err != nil {
			diagnosticError(c, err)
			return
		}
		status := http.StatusOK
		if item.Status == "queued" || item.Status == "running" {
			status = http.StatusAccepted
		}
		c.JSON(status, item)
	})
	api.POST("/runs/:id/stalls/:caseID/explain", func(c *gin.Context) {
		item, err := service.ExplainStall(c.Request.Context(), c.Param("id"), c.Param("caseID"))
		if err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusOK, item)
	})
	api.POST("/runs/:id/stalls/:caseID/stop", func(c *gin.Context) {
		if err := service.StopStall(c.Param("id"), c.Param("caseID")); err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "stopping"})
	})
	api.GET("/runs/:id/stall-assets/:caseID/:filename", func(c *gin.Context) {
		file, err := service.OpenStallAsset(c.Param("id"), c.Param("caseID"), c.Param("filename"))
		if err != nil {
			diagnosticError(c, err)
			return
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil || !stat.Mode().IsRegular() {
			diagnosticError(c, diagnostic.ErrNotFound)
			return
		}
		c.Header("Content-Type", "image/jpeg")
		if strings.HasSuffix(stat.Name(), ".mp4") {
			c.Header("Content-Type", "video/mp4")
		}
		c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
		http.ServeContent(c.Writer, c.Request, stat.Name(), stat.ModTime(), file)
	})
}
