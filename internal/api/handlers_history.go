package api

import (
	"net/http"
	"strconv"

	"github.com/drizz-dev/drizz-farm/internal/store"
)

type historyHandlers struct {
	store *store.Store
}

// SessionHistory handles GET /api/v1/history/sessions
func (h *historyHandlers) SessionHistory(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		JSON(w, http.StatusOK, map[string]any{"sessions": []any{}, "message": "store not available"})
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	sessions, err := h.store.SessionHistory(limit)
	if err != nil {
		Error(w, err)
		return
	}
	if sessions == nil {
		sessions = []store.SessionRecord{}
	}
	JSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// Events handles GET /api/v1/history/events
func (h *historyHandlers) Events(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		JSON(w, http.StatusOK, map[string]any{"events": []any{}})
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	events, err := h.store.RecentEvents(limit)
	if err != nil {
		Error(w, err)
		return
	}
	if events == nil {
		events = []store.EventRecord{}
	}
	JSON(w, http.StatusOK, map[string]any{"events": events})
}
