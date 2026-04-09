package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dashboard/*
var dashboardFS embed.FS

// ServeDashboard returns an HTTP handler that serves the embedded React dashboard.
// Falls back to index.html for client-side routing (SPA).
func ServeDashboard() http.Handler {
	// Strip the "dashboard/" prefix so files are served from root
	sub, err := fs.Sub(dashboardFS, "dashboard")
	if err != nil {
		panic("embedded dashboard not found: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// API requests should not be handled here
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws/") {
			http.NotFound(w, r)
			return
		}

		// Try to serve the file directly
		// If it's a file with extension (css, js, svg, etc), serve it
		if strings.Contains(path, ".") {
			fileServer.ServeHTTP(w, r)
			return
		}

		// For all other paths (/, /create, /sessions, /settings),
		// serve index.html for client-side routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
