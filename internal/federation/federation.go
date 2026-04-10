// Package federation manages multi-node drizz-farm clusters.
// One node acts as orchestrator, routing session requests to peers with capacity.
package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Peer represents a discovered drizz-farm node.
type Peer struct {
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Capacity  int       `json:"capacity"`
	Warm      int       `json:"warm"`
	Allocated int       `json:"allocated"`
	Available int       `json:"available"`
	Healthy   bool      `json:"healthy"`
	LastSeen  time.Time `json:"last_seen"`
}

// PeerPool is the pool status from a remote peer.
type PeerPool struct {
	TotalCapacity int `json:"total_capacity"`
	Warm          int `json:"warm"`
	Allocated     int `json:"allocated"`
	Booting       int `json:"booting"`
}

// Registry tracks all known peers in the cluster.
type Registry struct {
	mu     sync.RWMutex
	peers  map[string]*Peer // keyed by host:port
	self   string           // this node's host:port
	client *http.Client
}

// NewRegistry creates a federation registry.
func NewRegistry(selfHost string, selfPort int) *Registry {
	return &Registry{
		peers:  make(map[string]*Peer),
		self:   fmt.Sprintf("%s:%d", selfHost, selfPort),
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// AddPeer registers a discovered peer.
func (r *Registry) AddPeer(name, host string, port int) {
	key := fmt.Sprintf("%s:%d", host, port)
	if key == r.self {
		return // don't add ourselves
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.peers[key] = &Peer{
		Name:     name,
		Host:     host,
		Port:     port,
		Healthy:  true,
		LastSeen: time.Now(),
	}
	log.Info().Str("peer", key).Str("name", name).Msg("federation: peer added")
}

// RemovePeer removes a peer.
func (r *Registry) RemovePeer(host string, port int) {
	key := fmt.Sprintf("%s:%d", host, port)
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers, key)
}

// Peers returns all known peers.
func (r *Registry) Peers() []Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Peer, 0, len(r.peers))
	for _, p := range r.peers {
		result = append(result, *p)
	}
	return result
}

// PeerCount returns number of peers (excluding self).
func (r *Registry) PeerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.peers)
}

// RefreshPeers polls all peers for their current pool status.
func (r *Registry) RefreshPeers(ctx context.Context) {
	r.mu.RLock()
	keys := make([]string, 0, len(r.peers))
	for k := range r.peers {
		keys = append(keys, k)
	}
	r.mu.RUnlock()

	for _, key := range keys {
		go func(k string) {
			r.mu.RLock()
			peer, ok := r.peers[k]
			r.mu.RUnlock()
			if !ok {
				return
			}

			url := fmt.Sprintf("http://%s/api/v1/pool", k)
			resp, err := r.client.Get(url)
			if err != nil {
				r.mu.Lock()
				if p, ok := r.peers[k]; ok {
					p.Healthy = false
				}
				r.mu.Unlock()
				return
			}
			defer resp.Body.Close()

			var pool PeerPool
			body, _ := io.ReadAll(resp.Body)
			json.Unmarshal(body, &pool)

			r.mu.Lock()
			if p, ok := r.peers[k]; ok {
				p.Capacity = pool.TotalCapacity
				p.Warm = pool.Warm
				p.Allocated = pool.Allocated
				p.Available = pool.TotalCapacity - pool.Allocated - pool.Booting
				p.Healthy = true
				p.LastSeen = time.Now()
			}
			r.mu.Unlock()

			_ = peer // suppress unused
		}(key)
	}
}

// FindPeerWithCapacity returns the best peer that has available capacity.
// Returns nil if no peer has capacity.
func (r *Registry) FindPeerWithCapacity() *Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var best *Peer
	bestAvail := 0

	for _, p := range r.peers {
		if !p.Healthy {
			continue
		}
		avail := p.Capacity - p.Allocated
		if avail > bestAvail {
			bestAvail = avail
			pCopy := *p
			best = &pCopy
		}
	}

	return best
}

// CreateRemoteSession creates a session on a remote peer.
func (r *Registry) CreateRemoteSession(peer *Peer, profile string) (map[string]any, error) {
	url := fmt.Sprintf("http://%s:%d/api/v1/sessions", peer.Host, peer.Port)
	body := fmt.Sprintf(`{"profile":"%s","source":"federation"}`, profile)

	resp, err := r.client.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("federation: remote session create failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		return nil, fmt.Errorf("federation: remote returned %d: %s", resp.StatusCode, string(data))
	}

	var result map[string]any
	json.Unmarshal(data, &result)

	// Tag which node owns this session
	result["node"] = fmt.Sprintf("%s:%d", peer.Host, peer.Port)
	result["node_name"] = peer.Name

	return result, nil
}

// ReleaseRemoteSession releases a session on a remote peer.
func (r *Registry) ReleaseRemoteSession(nodeAddr string, sessionID string) error {
	url := fmt.Sprintf("http://%s/api/v1/sessions/%s", nodeAddr, sessionID)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// GetRemoteArtifacts gets artifact list from a remote peer.
func (r *Registry) GetRemoteArtifacts(nodeAddr string, instanceID string) ([]byte, error) {
	url := fmt.Sprintf("http://%s/api/v1/artifacts/%s", nodeAddr, instanceID)
	resp, err := r.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// StartRefreshLoop periodically refreshes peer status.
func (r *Registry) StartRefreshLoop(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.RefreshPeers(ctx)
				// Prune stale peers (not seen in 60s)
				r.mu.Lock()
				for k, p := range r.peers {
					if time.Since(p.LastSeen) > 60*time.Second {
						delete(r.peers, k)
						log.Warn().Str("peer", k).Msg("federation: peer removed (stale)")
					}
				}
				r.mu.Unlock()
			}
		}
	}()
}
