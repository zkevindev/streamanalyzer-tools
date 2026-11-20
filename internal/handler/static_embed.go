package handler

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed static/*
var embeddedStaticFiles embed.FS

var embeddedStaticRoot = mustStaticRoot()
var embeddedWorkbenchPages = renderWorkbenchPages(embeddedStaticRoot)

func mustStaticRoot() fs.FS {
	root, err := fs.Sub(embeddedStaticFiles, "static")
	if err != nil {
		panic(err)
	}
	return root
}

func embeddedStaticHTTPFS() http.FileSystem {
	return http.FS(embeddedStaticRoot)
}

func serveEmbeddedStaticFile(c *gin.Context, name string) {
	if page, ok := embeddedWorkbenchPages[name]; ok {
		http.ServeContent(c.Writer, c.Request, name, time.Time{}, bytes.NewReader(page))
		return
	}
	http.ServeFileFS(c.Writer, c.Request, embeddedStaticRoot, name)
}
