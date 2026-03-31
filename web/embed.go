package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

func serveIndex(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.FileServer(http.FS(sub)).ServeHTTP(w, r)
}
