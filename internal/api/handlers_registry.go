package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/registry"
)

// isLocalhost returns true when the request originates from the local
// machine (loopback interface or unix socket). Used to gate destructive
// operations like Leave that should only be triggered by the user
// physically at this Mac (or SSH'd in), not by anyone on the LAN who
// happens to load the dashboard URL.
func isLocalhost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// propagateAdd best-effort POSTs the new node entry to every other
// peer in the group so their local rosters stay in sync. Failures
// are logged at warn level and ignored — eventual consistency.
func propagateAdd(reg *registry.Registry, n registry.Node) {
	key := reg.GroupKey()
	if key == "" {
		return
	}
	body, _ := json.Marshal(n)
	for _, peer := range reg.Nodes() {
		if peer.Name == n.Name {
			continue // don't loop back to ourselves or the new node
		}
		go func(peerURL string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "POST",
				strings.TrimRight(peerURL, "/")+"/api/v1/nodes",
				bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Group-Key", key)
			req.Header.Set("X-Drizz-Propagate", "1") // peers won't re-propagate
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Warn().Err(err).Str("peer", peerURL).Msg("propagate add: peer unreachable")
				return
			}
			resp.Body.Close()
		}(peer.URL)
	}
}

// propagateRemove best-effort DELETEs the node from every peer in
// the group. Same eventual-consistency semantics as propagateAdd.
func propagateRemove(reg *registry.Registry, name string) {
	key := reg.GroupKey()
	if key == "" {
		return
	}
	for _, peer := range reg.Nodes() {
		if peer.Name == name {
			continue
		}
		go func(peerURL string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "DELETE",
				strings.TrimRight(peerURL, "/")+"/api/v1/nodes/"+name, nil)
			req.Header.Set("X-Group-Key", key)
			req.Header.Set("X-Drizz-Propagate", "1")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Warn().Err(err).Str("peer", peerURL).Msg("propagate remove: peer unreachable")
				return
			}
			resp.Body.Close()
		}(peer.URL)
	}
}

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
		"group_key":  h.reg.GroupKey(), // dashboard shows masked; eye toggle reveals
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
//
// Localhost-only: only the user physically at this Mac (or SSH'd in)
// can leave. Anyone else opening the dashboard URL gets 403 — they
// can't yank our machine out of the group from across the LAN.
//
// Best-effort propagation: tells every peer to remove us from their
// rosters too, so the leave is reflected group-wide.
func (h *registryHandlers) LeaveGroup(w http.ResponseWriter, r *http.Request) {
	if !isLocalhost(r) {
		JSON(w, http.StatusForbidden, ErrorResponse{
			Error:   "forbidden",
			Message: "leave can only be triggered from the local machine",
			Code:    403,
		})
		return
	}

	// Capture peer list + own name BEFORE we wipe the registry.
	peers := h.reg.Nodes()
	key := h.reg.GroupKey()
	selfName := h.selfName()

	if err := h.reg.LeaveGroup(); err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "leave_failed", Message: err.Error(), Code: 500})
		return
	}

	// Tell every peer to drop us. We're already out locally; this is
	// just so other dashboards don't show us as "offline" forever.
	for _, peer := range peers {
		if peer.Name == selfName {
			continue
		}
		go func(peerURL string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "DELETE",
				strings.TrimRight(peerURL, "/")+"/api/v1/nodes/"+selfName, nil)
			req.Header.Set("X-Group-Key", key)
			req.Header.Set("X-Drizz-Propagate", "1")
			if resp, err := http.DefaultClient.Do(req); err == nil {
				resp.Body.Close()
			}
		}(peer.URL)
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
//
// Two-factor admission to stop phantom-node injection:
//
//   1. X-Group-Key header must match — proves the caller already
//      knows the shared secret.
//   2. Callback verification — we open a connection back to the URL
//      being added and check it responds to GET /api/v1/group with
//      the SAME group_key. That proves the URL actually hosts a
//      drizz-farm node that's already provisioned with our group,
//      not just an arbitrary endpoint someone with the key picked.
//
// After admission we add locally and propagate to every other peer
// (best-effort, so all rosters converge). X-Drizz-Propagate: 1 marks
// a hop that's already a propagation, preventing O(n²) fan-out.
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

	// Callback verification — only enforced for the original add. A
	// propagation hop already came from another peer that did the
	// verification once; re-verifying on every fan-out would multiply
	// the round-trips and could fail spuriously if the joining node's
	// dashboard is briefly busy.
	if r.Header.Get("X-Drizz-Propagate") != "1" {
		if err := verifyJoiningNode(n.URL, key); err != nil {
			JSON(w, http.StatusForbidden, ErrorResponse{
				Error:   "verification_failed",
				Message: "could not verify the URL hosts a drizz-farm node in this group: " + err.Error(),
				Code:    403,
			})
			return
		}
	}

	if err := h.reg.AddNode(n); err != nil {
		JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "add_failed", Message: err.Error(), Code: 500})
		return
	}
	if r.Header.Get("X-Drizz-Propagate") != "1" {
		propagateAdd(h.reg, n)
	}
	JSON(w, http.StatusOK, map[string]any{"status": "ok", "node": n})
}

// verifyJoiningNode opens a connection back to nodeURL and confirms
// it responds to GET /api/v1/group with the same group_key. Returns
// nil if verification passes. Failures here come back to the caller
// as "verification_failed" so they know to fix their URL or check
// their key — not as a generic 500.
func verifyJoiningNode(nodeURL, expectedKey string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET",
		strings.TrimRight(nodeURL, "/")+"/api/v1/group", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	var body struct {
		GroupKey string `json:"group_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if body.GroupKey == "" {
		return fmt.Errorf("not in any group")
	}
	if body.GroupKey != expectedKey {
		return fmt.Errorf("group key mismatch")
	}
	return nil
}

// RemoveNode → DELETE /api/v1/nodes/:name (header: X-Group-Key)
// Authenticated. Removes a node entry from the group, then propagates
// the removal to every other peer so the whole group converges on
// the same view. The X-Drizz-Propagate hop guard prevents fan-out
// loops the same way it does for AddNode.
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
	if r.Header.Get("X-Drizz-Propagate") != "1" {
		propagateRemove(h.reg, name)
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
