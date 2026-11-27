package handler

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"streamanalyzer/internal/simulator/whep"

	"github.com/gin-gonic/gin"
)

func registerWHEPSimulatorRoutes(r *gin.Engine, source *whep.Manager) {
	r.GET("/simulators/whep", localDiagnostics, func(c *gin.Context) {
		serveEmbeddedStaticFile(c, "simulator-whep.html")
	})
	api := r.Group("/api/simulators/whep", localDiagnostics)
	api.GET("", func(c *gin.Context) { c.JSON(http.StatusOK, source.Status()) })
	api.POST("/start", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512)
		var input struct {
			Mode string `json:"mode"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			whepError(c, whep.ErrMode)
			return
		}
		status, err := source.Start(input.Mode)
		if err != nil {
			whepError(c, err)
			return
		}
		c.JSON(http.StatusCreated, status)
	})
	api.POST("/stop", func(c *gin.Context) { c.JSON(http.StatusOK, source.Stop()) })
	api.POST("/stream/:source", func(c *gin.Context) {
		format, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || format != "application/sdp" {
			c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "WHEP Offer 需使用 application/sdp"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
		offer, err := io.ReadAll(c.Request.Body)
		if err != nil {
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				c.Status(http.StatusRequestEntityTooLarge)
			} else {
				whepError(c, whep.ErrOffer)
			}
			return
		}
		answer, path, err := source.Offer(c.Request.Context(), c.Param("source"), string(offer))
		if err != nil {
			whepError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("Location", path)
		c.Data(http.StatusCreated, "application/sdp", []byte(answer))
	})
	api.GET("/sessions/:id", func(c *gin.Context) {
		status, ok := source.Session(c.Param("id"))
		if !ok {
			whepError(c, whep.ErrMissing)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, status)
	})
	api.DELETE("/sessions/:id", func(c *gin.Context) {
		if err := source.Delete(c.Param("id")); err != nil {
			whepError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}

func whepError(c *gin.Context, err error) {
	code := http.StatusServiceUnavailable
	switch err {
	case whep.ErrMode, whep.ErrOffer:
		code = http.StatusBadRequest
	case whep.ErrUnauthorized:
		c.Header("WWW-Authenticate", `Bearer realm="whep-simulator"`)
		code = http.StatusUnauthorized
	case whep.ErrSDPReject:
		code = http.StatusNotAcceptable
	case whep.ErrActive:
		code = http.StatusConflict
	case whep.ErrMissing:
		code = http.StatusNotFound
	case whep.ErrLimit:
		code = http.StatusTooManyRequests
	}
	c.JSON(code, gin.H{"error": err.Error()})
}
