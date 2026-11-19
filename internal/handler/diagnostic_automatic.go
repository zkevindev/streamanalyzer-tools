package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"streamanalyzer/internal/diagnostic"
)

func automaticEmptyRequest(c *gin.Context) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var body *struct{}
	err := decoder.Decode(&body)
	return err == io.EOF || err == nil && body != nil && decoder.Decode(new(any)) == io.EOF
}

func RegisterAutomaticInvestigationRoutes(api *gin.RouterGroup, service *diagnostic.Service) {
	api.POST("/runs/:id/automatic/retry", func(c *gin.Context) {
		if !automaticEmptyRequest(c) {
			diagnosticError(c, diagnostic.ErrInvalid)
			return
		}
		result, err := service.RetryAutomaticInvestigation(c.Param("id"))
		if err != nil {
			diagnosticError(c, err)
			return
		}
		status := http.StatusOK
		switch result.State {
		case "waiting_player", "queued", "investigating", "summarizing":
			status = http.StatusAccepted
		}
		c.JSON(status, result)
	})
	api.POST("/runs/:id/automatic/stop", func(c *gin.Context) {
		if !automaticEmptyRequest(c) {
			diagnosticError(c, diagnostic.ErrInvalid)
			return
		}
		if err := service.StopAutomaticInvestigation(c.Param("id")); err != nil {
			diagnosticError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "stopping"})
	})
}
