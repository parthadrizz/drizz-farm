package api

// Unified per-session artifact API.
//
//   GET  /api/v1/sessions/{id}/artifacts
//     → list every file saved for this session (video, logcat,
//       screenshots, …) with type + filename + size + download URL.
//
//   GET  /api/v1/sessions/{id}/artifacts/{filename}
//     → download a single file. Filename is a plain basename; no
//       path traversal allowed.
//
// This lives alongside the older /recording/download and /logcat/download
// routes (kept for backwards compat) — the unified endpoint is the one
// new clients and the dashboard use.

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/drizz-dev/drizz-farm/internal/capture"
	"github.com/drizz-dev/drizz-farm/internal/session"
)

type artifactHandlers struct {
	capture *capture.Service
	broker  *session.Broker
}

// List → GET /api/v1/sessions/{id}/artifacts
func (h *artifactHandlers) List(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Surface capabilities so the caller can tell "nothing captured
	// because nothing was requested" apart from "capture was requested
	// but the files are missing."
	var caps *session.SessionCapabilities
	if sess, err := h.broker.Get(id); err == nil {
		caps = sess.Capabilities
	}

	files := h.capture.List(id)
	if files == nil {
		files = []capture.ArtifactFile{}
	}
	JSON(w, http.StatusOK, map[string]any{
		"session_id":   id,
		"capabilities": caps,
		"artifacts":    files,
		"total":        len(files),
	})
}

// Serve → GET /api/v1/sessions/{id}/artifacts/{filename}
// Streams the named artifact file back. 404 if the file doesn't exist.
func (h *artifactHandlers) Serve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	name := chi.URLParam(r, "filename")
	path := h.capture.ArtifactPath(id, name)
	if path == "" {
		JSON(w, http.StatusNotFound, ErrorResponse{Error: "not_found", Message: "artifact not found", Code: 404})
		return
	}

	// Infer content type from extension. Keeps downloads working in
	// browsers that honor Content-Type; for unknown extensions we
	// default to octet-stream so the user gets a download prompt
	// instead of a rendered blob.
	switch filepath.Ext(name) {
	case ".mp4":
		w.Header().Set("Content-Type", "video/mp4")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".txt", ".log":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	case ".har":
		w.Header().Set("Content-Type", "application/json")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	f, err := os.Open(path)
	if err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "open_failed", Message: err.Error(), Code: 500})
		return
	}
	defer f.Close()
	info, _ := f.Stat()
	http.ServeContent(w, r, name, info.ModTime(), f)
}
