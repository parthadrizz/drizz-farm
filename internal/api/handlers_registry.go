package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/registry"
)

type registryHandlers struct {
	reg *registry.Registry
	cfg *config.Config
}

// selfURL returns how other browsers should reach THIS node.
// Uses cfg.Node.ExternalURL if set; otherwise defaults to hostname.local:port.
func (h *registryHandlers) selfURL() string {
	if h.cfg.Node.ExternalURL != "" {
		return h.cfg.Node.ExternalURL
	}
	host, _ := os.Hostname()
	if !strings.HasSuffix(host, ".local") {
		host += ".local"
	}
	return fmt.Sprintf("http://%s:%d", host, h.cfg.API.Port)
}

// selfName returns this node's name.
func (h *registryHandlers) selfName() string {
	if h.cfg.Node.Name != "" {
		return h.cfg.Node.Name
	}
	host, _ := os.Hostname()
	return host
}

// GroupInfo → GET /api/v1/group
// Public — no auth. Returns group name and this node's identity.
// Dashboard uses this to decide "am I standalone or in a group?".
func (h *registryHandlers) GroupInfo(w http.ResponseWriter, r *http.Request) {
	snap := h.reg.Snapshot()
	JSON(w, http.StatusOK, map[string]any{
		"group_name": snap.GroupName,
		"has_group":  snap.GroupName != "",
		"self": map[string]string{
			"name": h.selfName(),
			"url":  h.selfURL(),
		},
	})
}

// List → GET /api/v1/nodes
// Public — no auth. Returns the list of nodes in this group.
// If no group, returns just this node as a single-member list.
func (h *registryHandlers) List(w http.ResponseWriter, r *http.Request) {
	if !h.reg.HasGroup() {
		JSON(w, http.StatusOK, map[string]any{
			"group_name": "",
			"nodes": []registry.Node{
				{Name: h.selfName(), URL: h.selfURL()},
			},
		})
		return
	}
	snap := h.reg.Snapshot()
	JSON(w, http.StatusOK, map[string]any{
		"group_name": snap.GroupName,
		"nodes":      snap.Nodes,
	})
}

// CreateGroup → POST /api/v1/group  body: {"name": "my-lab"}
// No existing group required; fails if group already exists.
// Returns the generated group key ONCE. Shown to the user; they share it with peers who join.
func (h *registryHandlers) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		JSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid", Message: "name required", Code: 400})
		return
	}

	key, err := h.reg.CreateGroup(req.Name, h.selfName(), h.selfURL())
	if err != nil {
		JSON(w, http.StatusConflict, ErrorResponse{Error: "conflict", Message: err.Error(), Code: 409})
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"group_name": req.Name,
		"group_key":  key, // shown to user ONCE — they must copy it
		"self":       registry.Node{Name: h.selfName(), URL: h.selfURL()},
	})
}

// JoinGroup → POST /api/v1/group/join  body: {"peer_url": "...", "group_key": "..."}
// Fetches the group config from the peer, saves it locally, and tells the peer about us.
func (h *registryHandlers) JoinGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PeerURL  string `json:"peer_url"`
		GroupKey string `json:"group_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PeerURL == "" || req.GroupKey == "" {
		JSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid", Message: "peer_url and group_key required", Code: 400})
		return
	}

	groupName, err := h.reg.JoinGroup(r.Context(), req.PeerURL, req.GroupKey, h.selfName(), h.selfURL())
	if err != nil {
		code := http.StatusInternalServerError
		if strings.Contains(err.Error(), "invalid group key") {
			code = http.StatusForbidden
		} else if strings.Contains(err.Error(), "already in group") {
			code = http.StatusConflict
		}
		JSON(w, code, ErrorResponse{Error: "join_failed", Message: err.Error(), Code: code})
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"status":     "joined",
		"group_name": groupName,
	})
}

// LeaveGroup → DELETE /api/v1/group
// Wipes this node's group membership. Goes back to standalone mode.
func (h *registryHandlers) LeaveGroup(w http.ResponseWriter, r *http.Request) {
	if err := h.reg.LeaveGroup(); err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "leave_failed", Message: err.Error(), Code: 500})
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "left"})
}

// ExportGroup → GET /api/v1/group/export (header: X-Group-Key)
// Returns the full group config including the group key and node list.
// ONLY called by a node that's about to join — they authenticate with the key they already have.
func (h *registryHandlers) ExportGroup(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-Group-Key")
	if !h.reg.VerifyKey(key) {
		JSON(w, http.StatusForbidden, ErrorResponse{Error: "forbidden", Message: "invalid group key", Code: 403})
		return
	}
	data, err := h.reg.MarshalForExport()
	if err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "export_failed", Message: err.Error(), Code: 500})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// AddNode → POST /api/v1/nodes (header: X-Group-Key)  body: {"name":"", "url":""}
// Authenticated. Idempotent by name. Used during join flow.
func (h *registryHandlers) AddNode(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-Group-Key")
	if !h.reg.VerifyKey(key) {
		JSON(w, http.StatusForbidden, ErrorResponse{Error: "forbidden", Message: "invalid group key", Code: 403})
		return
	}
	var n registry.Node
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil || n.Name == "" || n.URL == "" {
		JSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid", Message: "name and url required", Code: 400})
		return
	}
	if err := h.reg.AddNode(n); err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "add_failed", Message: err.Error(), Code: 500})
		return
	}
	JSON(w, http.StatusOK, map[string]any{"status": "ok", "node": n})
}

// RemoveNode → DELETE /api/v1/nodes/:name (header: X-Group-Key)
// Authenticated. Removes a node entry from the group.
func (h *registryHandlers) RemoveNode(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-Group-Key")
	if !h.reg.VerifyKey(key) {
		JSON(w, http.StatusForbidden, ErrorResponse{Error: "forbidden", Message: "invalid group key", Code: 403})
		return
	}
	name := chi.URLParam(r, "name")
	removed, err := h.reg.RemoveNode(name)
	if err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "remove_failed", Message: err.Error(), Code: 500})
		return
	}
	if !removed {
		JSON(w, http.StatusNotFound, ErrorResponse{Error: "not_found", Message: "node not in group", Code: 404})
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// --- helpers ---

// lanIP finds a non-loopback IPv4 for the current machine (used in default URL derivation).
// Unused here but kept as a utility since other handlers may want it later.
func lanIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "127.0.0.1"
}

var _ = lanIP // keep the helper available
