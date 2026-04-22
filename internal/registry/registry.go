// Package registry maintains the list of nodes in a drizz-farm group.
//
// Design: dumb list. No leader, no consensus, no sync protocol.
// Any node serves the list at GET /nodes. Dashboard fetches, then talks
// to each node directly from the browser. Nodes never call each other.
//
// The registry file is shared by convention (git, dotfiles sync, scp, or
// HTTP push with group_key auth). When you want to add a node, you edit
// the file on any one node and optionally push to the others.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Node describes a single drizz-farm node that belongs to a group.
type Node struct {
	Name        string    `yaml:"name" json:"name"`
	URL         string    `yaml:"url" json:"url"` // how browsers reach it, e.g. http://mac-mini-1.local:9401
	AddedAt     time.Time `yaml:"added_at" json:"added_at"`
	Description string    `yaml:"description,omitempty" json:"description,omitempty"`
}

// File is the on-disk format for nodes.yaml.
// GroupKey is a shared secret required to add/remove nodes via API.
// Not used for browser → node calls; those are fine over LAN/VPN/hub.
type File struct {
	GroupName string `yaml:"group_name" json:"group_name"`
	GroupKey  string `yaml:"group_key" json:"-"` // never serialized to clients
	Nodes     []Node `yaml:"nodes" json:"nodes"`
}

// Registry is an in-memory, file-backed list of nodes.
type Registry struct {
	mu   sync.RWMutex
	path string
	file File
}

// New loads the registry from disk. If the file doesn't exist, returns
// an empty registry with that path — will be created on first Save().
func New(path string) (*Registry, error) {
	r := &Registry{path: path}
	if err := r.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return r, nil
}

// load reads the YAML file into memory. Safe to call repeatedly.
func (r *Registry) load() error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.file = File{}
	return yaml.Unmarshal(data, &r.file)
}

// save writes the file atomically (tmp + rename) so a crash mid-write
// doesn't corrupt the registry.
func (r *Registry) save() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
		return fmt.Errorf("create registry dir: %w", err)
	}
	r.mu.RLock()
	data, err := yaml.Marshal(&r.file)
	r.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// GroupName returns the configured group name (empty if no group yet).
func (r *Registry) GroupName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.file.GroupName
}

// HasGroup returns true when a group has been configured.
func (r *Registry) HasGroup() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.file.GroupName != "" && r.file.GroupKey != ""
}

// VerifyKey returns true if the given key matches the group key.
// Used to authorize POST /nodes and DELETE /nodes/:name.
func (r *Registry) VerifyKey(key string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.file.GroupKey != "" && r.file.GroupKey == key
}

// GroupKey returns the raw group key. Exposed via GET /group so the
// dashboard can render it (masked + copyable). Security boundary is
// "who can reach the dashboard," not "who knows the key" — the key
// only authorizes adding/removing nodes in this group's roster, it
// doesn't grant control of any node's emulators or data.
func (r *Registry) GroupKey() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.file.GroupKey
}

// Nodes returns a copy of the node list.
func (r *Registry) Nodes() []Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Node, len(r.file.Nodes))
	copy(out, r.file.Nodes)
	return out
}

// Snapshot returns a sanitized copy of the registry file for the API.
// GroupKey is cleared — it never leaves the backend.
func (r *Registry) Snapshot() File {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := r.file
	cp.GroupKey = ""
	cp.Nodes = append([]Node(nil), r.file.Nodes...)
	return cp
}

// CreateGroup initializes a new group with the given name and generated key.
// Returns the generated key so the caller can show it to the user once.
// No-op error if a group already exists.
func (r *Registry) CreateGroup(name, selfNodeName, selfURL string) (string, error) {
	r.mu.Lock()
	if r.file.GroupName != "" {
		r.mu.Unlock()
		return "", fmt.Errorf("group %q already exists", r.file.GroupName)
	}
	key := generateKey()
	r.file.GroupName = name
	r.file.GroupKey = key
	r.file.Nodes = []Node{{
		Name:    selfNodeName,
		URL:     selfURL,
		AddedAt: time.Now(),
	}}
	r.mu.Unlock()
	return key, r.save()
}

// AddNode adds or replaces a node entry. Idempotent by name.
func (r *Registry) AddNode(n Node) error {
	if n.Name == "" || n.URL == "" {
		return fmt.Errorf("node name and url are required")
	}
	r.mu.Lock()
	found := false
	for i, existing := range r.file.Nodes {
		if existing.Name == n.Name {
			if n.AddedAt.IsZero() {
				n.AddedAt = existing.AddedAt
			}
			r.file.Nodes[i] = n
			found = true
			break
		}
	}
	if !found {
		if n.AddedAt.IsZero() {
			n.AddedAt = time.Now()
		}
		r.file.Nodes = append(r.file.Nodes, n)
	}
	r.mu.Unlock()
	return r.save()
}

// RemoveNode removes a node by name. Returns true if it existed.
func (r *Registry) RemoveNode(name string) (bool, error) {
	r.mu.Lock()
	removed := false
	filtered := r.file.Nodes[:0]
	for _, existing := range r.file.Nodes {
		if existing.Name == name {
			removed = true
			continue
		}
		filtered = append(filtered, existing)
	}
	r.file.Nodes = filtered
	r.mu.Unlock()
	if !removed {
		return false, nil
	}
	return true, r.save()
}

// LeaveGroup clears the group info and nodes list. Used when this node
// wants to stop being part of the group entirely.
func (r *Registry) LeaveGroup() error {
	r.mu.Lock()
	r.file = File{}
	r.mu.Unlock()
	// Delete file — empty state means "standalone"
	if err := os.Remove(r.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete registry file: %w", err)
	}
	return nil
}

// JoinGroup joins an existing group via a peer node.
//   1. Fetch peer's full group config (validates that our key is correct)
//   2. Save it locally as our nodes.yaml
//   3. Register ourselves with the peer so other members see us too
// Returns the group name on success.
func (r *Registry) JoinGroup(ctx context.Context, peerURL, groupKey, selfName, selfURL string) (string, error) {
	if r.HasGroup() {
		return "", fmt.Errorf("already in group %q — leave first", r.GroupName())
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: pull the group config from the peer
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(peerURL, "/")+"/api/v1/group/export", nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Group-Key", groupKey)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("reach peer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 {
		return "", fmt.Errorf("invalid group key — peer rejected")
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("peer returned status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read peer response: %w", err)
	}

	// Step 2: save the group config locally
	if err := r.ImportFromJSON(data); err != nil {
		return "", fmt.Errorf("import group config: %w", err)
	}

	// Step 3: tell the peer about us so other members see us on their next refresh.
	// Best-effort — if this fails, we're still in the group (we have the config),
	// we just need someone else to add us later. Log but don't fail.
	selfEntry := Node{Name: selfName, URL: selfURL, AddedAt: time.Now()}
	if err := r.AddNode(selfEntry); err != nil {
		return r.GroupName(), fmt.Errorf("save self entry: %w", err)
	}
	body, _ := json.Marshal(selfEntry)
	req2, _ := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(peerURL, "/")+"/api/v1/nodes", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Group-Key", groupKey)
	if resp2, err := client.Do(req2); err == nil {
		resp2.Body.Close()
	}
	// If the push failed, that's fine — peer will see us via periodic re-reads of nodes.yaml
	// when an admin does a manual sync. For LAN case, they can also re-add via the UI.

	return r.GroupName(), nil
}

// ImportFromJSON is called when joining an existing group: the joining
// node receives the full export (with group key) from a peer and writes
// it to local disk.
func (r *Registry) ImportFromJSON(data []byte) error {
	var f exportFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse group config: %w", err)
	}
	if f.GroupName == "" || f.GroupKey == "" {
		return fmt.Errorf("group config missing name or key")
	}
	r.mu.Lock()
	r.file = File{GroupName: f.GroupName, GroupKey: f.GroupKey, Nodes: f.Nodes}
	r.mu.Unlock()
	return r.save()
}

// exportFile is an export-time mirror of File that includes the group key.
// The main File struct intentionally omits the key from JSON (never leaked
// to dashboard / public). This type is only used for node-to-node join.
type exportFile struct {
	GroupName string `json:"group_name"`
	GroupKey  string `json:"group_key"`
	Nodes     []Node `json:"nodes"`
}

// MarshalForExport returns the full registry (including group key) as JSON.
// ONLY used when another node is joining and has authenticated with the group key.
// Never return this to unauthenticated clients.
func (r *Registry) MarshalForExport() ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return json.Marshal(exportFile{
		GroupName: r.file.GroupName,
		GroupKey:  r.file.GroupKey,
		Nodes:     r.file.Nodes,
	})
}
