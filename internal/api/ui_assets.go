//nolint:revive // "api" is a meaningful package name, not a stub like "common" or "util"
package api

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed ui/*
var embeddedUI embed.FS

// embeddedReactUI holds the compiled React bundle copied by "make embed-web".
// When the placeholder index.html is the only file present (i.e. "make web"
// has not been run), reactUIFileSystem returns (nil, false) and the server
// falls back to the vanilla embedded UI.
//
//go:embed reactui
var embeddedReactUI embed.FS

// reactUIFileSystem returns the embedded React UI as an fs.FS together with a
// boolean that reports whether the real compiled bundle is present.
// It returns (nil, false) when only the placeholder index.html exists.
func reactUIFileSystem() (fs.FS, bool) {
	sub, err := fs.Sub(embeddedReactUI, "reactui")
	if err != nil {
		return nil, false
	}

	// The real React build places JS chunks under assets/.
	// If that directory is absent or empty the placeholder is still in place.
	entries, err := embeddedReactUI.ReadDir("reactui/assets")
	if err != nil || len(entries) == 0 {
		return nil, false
	}
	built := false
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
			built = true
			break
		}
	}
	if !built {
		return nil, false
	}
	return sub, true
}

// serveEmbeddedReactSPA returns an http.Handler that serves a React SPA from
// the given fs.FS.  Unknown paths fall back to index.html (client-side routing).
func serveEmbeddedReactSPA(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			// SPA fallback: serve index.html for unknown paths.
			data, err = fs.ReadFile(fsys, "index.html")
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}
		if ct := mime.TypeByExtension(path.Ext(p)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}

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
