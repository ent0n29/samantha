package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var embeddedStatic embed.FS

func newStaticHandler() http.Handler {
	sub, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(sub))
}
