package handler

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed static/*
var embeddedStaticFiles embed.FS

var embeddedStaticRoot = mustStaticRoot()

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
	http.ServeFileFS(c.Writer, c.Request, embeddedStaticRoot, name)
}
