//nolint:revive // "api" is a meaningful package name, not a stub like "common" or "util"
package api

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
)

//go:embed ui/*
var embeddedUI embed.FS

func uiFileSystem() fs.FS {
	sub, err := fs.Sub(embeddedUI, "ui")
	if err != nil {
		panic(err)
	}

	return sub
}

func serveEmbeddedFile(w http.ResponseWriter, name string) {
	data, err := fs.ReadFile(uiFileSystem(), name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
