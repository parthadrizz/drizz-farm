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
	// Name is the human-readable node name.
	Name string `json:"name"`
	// Host is the IP or hostname of the peer.
	Host string `json:"host"`
	// Port is the HTTP API port of the peer.
	Port int `json:"port"`
	// Capacity is the maximum number of emulator instances the peer can run.
	Capacity int `json:"capacity"`
	// Warm is the number of pre-booted idle instances on the peer.
	Warm int `json:"warm"`
	// Allocated is the number of instances currently assigned to sessions.
	Allocated int `json:"allocated"`
	// Available is the number of additional instances the peer can accept.
	Available int `json:"available"`
	// NumCPU is the number of CPU cores on the peer machine.
	NumCPU int `json:"num_cpu"`
	// MemoryMB is the total system memory in megabytes.
	MemoryMB int `json:"memory_mb"`
	// Healthy indicates whether the peer responded to the last health check.
	Healthy bool `json:"healthy"`
	// LastSeen is the timestamp of the last successful health check.
	LastSeen time.Time `json:"last_seen"`
}

// PeerPool is the pool status returned by a remote peer's /api/v1/pool endpoint.
type PeerPool struct {
	// TotalCapacity is the maximum number of emulator instances the peer supports.
	TotalCapacity int `json:"total_capacity"`
	// Warm is the count of pre-booted idle instances.
	Warm int `json:"warm"`
	// Allocated is the count of instances currently in use by sessions.
	Allocated int `json:"allocated"`
	// Booting is the count of instances currently starting up.
	Booting int `json:"booting"`
}

// Registry tracks all known peers in the cluster.
type Registry struct {
	mu       sync.RWMutex
	peers    map[string]*Peer // keyed by host:port
	self     string           // this node's host:port
	selfPeer Peer             // this node's stats for leader election
	selfUpdateFn func()       // called each refresh to update self stats
	clusterKey   string       // shared secret for peer authentication
	client   *http.Client
}

// NewRegistry creates a federation registry with a cluster key for peer authentication.
func NewRegistry(selfHost string, selfPort int, clusterKey string) *Registry {
	return &Registry{
		peers:  make(map[string]*Peer),
		self:   fmt.Sprintf("%s:%d", selfHost, selfPort),
		selfPeer: Peer{
			Host:    selfHost,
			Port:    selfPort,
			Healthy: true,
		},
		clusterKey: clusterKey,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// VerifyHandshake checks if a peer's cluster key matches ours.
func (r *Registry) VerifyHandshake(peerKey string) bool {
	return r.clusterKey != "" && peerKey == r.clusterKey
}

// ClusterKey returns this node's cluster key (used by API handlers).
func (r *Registry) ClusterKey() string {
	return r.clusterKey
}

// SetSelfUpdateFn sets a callback invoked every refresh cycle to update self stats.
func (r *Registry) SetSelfUpdateFn(fn func()) {
	r.selfUpdateFn = fn
}

// UpdateSelf updates this node's stats for leader election.
func (r *Registry) UpdateSelf(name string, capacity, available, numCPU, memoryMB int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selfPeer.Name = name
	r.selfPeer.Capacity = capacity
	r.selfPeer.Available = available
	r.selfPeer.NumCPU = numCPU
	r.selfPeer.MemoryMB = memoryMB
	r.selfPeer.Healthy = true
	r.selfPeer.LastSeen = time.Now()
}

// leaderScore computes a score for leader election.
// Higher score = more suitable as leader.
func leaderScore(p *Peer) int {
	return p.Available*10 + p.NumCPU*5 + (p.MemoryMB/1024)*3 + p.Capacity*2
}

// IsLeader returns true if this node is the current cluster leader.
func (r *Registry) IsLeader() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	selfScore := leaderScore(&r.selfPeer)
	for _, p := range r.peers {
		if !p.Healthy {
			continue
		}
		peerScore := leaderScore(p)
		if peerScore > selfScore {
			return false
		}
		if peerScore == selfScore {
			// Tiebreaker: lowest host:port
			peerAddr := fmt.Sprintf("%s:%d", p.Host, p.Port)
			if peerAddr < r.self {
				return false
			}
		}
	}
	return true
}

// LeaderName returns the name of the current leader.
func (r *Registry) LeaderName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	best := &r.selfPeer
	bestScore := leaderScore(best)
	bestAddr := r.self

	for _, p := range r.peers {
		if !p.Healthy {
			continue
		}
		s := leaderScore(p)
		addr := fmt.Sprintf("%s:%d", p.Host, p.Port)
		if s > bestScore || (s == bestScore && addr < bestAddr) {
			best = p
			bestScore = s
			bestAddr = addr
		}
	}
	return best.Name
}

// AddPeer registers a discovered peer after verifying cluster key via handshake.
// If the peer doesn't respond to the handshake or has a wrong key, it's rejected.
func (r *Registry) AddPeer(name, host string, port int) {
	key := fmt.Sprintf("%s:%d", host, port)
	if key == r.self {
		return // don't add ourselves
	}

	// Already known — skip handshake
	r.mu.RLock()
	_, exists := r.peers[key]
	r.mu.RUnlock()
	if exists {
		return
	}

	// Verify cluster key via handshake
	if r.clusterKey != "" {
		handshakeURL := fmt.Sprintf("http://%s/api/v1/federation/handshake", key)
		body := fmt.Sprintf(`{"cluster_key":"%s"}`, r.clusterKey)
		resp, err := r.client.Post(handshakeURL, "application/json", strings.NewReader(body))
		if err != nil {
			log.Debug().Str("peer", key).Err(err).Msg("federation: handshake failed (unreachable)")
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == 403 {
			log.Warn().Str("peer", key).Msg("federation: peer rejected (wrong cluster key)")
			return
		}
		if resp.StatusCode != 200 {
			log.Debug().Str("peer", key).Int("status", resp.StatusCode).Msg("federation: handshake unexpected status")
			return
		}
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

			// Also fetch health for CPU/memory stats
			var numCPU, memMB int
			healthURL := fmt.Sprintf("http://%s/api/v1/node/health", k)
			if hResp, hErr := r.client.Get(healthURL); hErr == nil {
				defer hResp.Body.Close()
				var health map[string]any
				hBody, _ := io.ReadAll(hResp.Body)
				json.Unmarshal(hBody, &health)
				if res, ok := health["resources"].(map[string]any); ok {
					if v, ok := res["num_cpu"].(float64); ok {
						numCPU = int(v)
					}
					if v, ok := res["total_memory"].(float64); ok {
						memMB = int(v / 1024 / 1024)
					}
				}
			}

			r.mu.Lock()
			if p, ok := r.peers[k]; ok {
				p.Capacity = pool.TotalCapacity
				p.Warm = pool.Warm
				p.Allocated = pool.Allocated
				p.Available = pool.TotalCapacity - pool.Allocated - pool.Booting
				p.NumCPU = numCPU
				p.MemoryMB = memMB
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

// --- Remote Node Management ---

// GetRemotePool gets full pool status from a peer.
func (r *Registry) GetRemotePool(nodeAddr string) ([]byte, error) {
	return r.remoteGet(nodeAddr, "/api/v1/pool")
}

// GetRemoteHealth gets health from a peer.
func (r *Registry) GetRemoteHealth(nodeAddr string) ([]byte, error) {
	return r.remoteGet(nodeAddr, "/api/v1/node/health")
}

// GetRemoteAVDs lists AVDs on a peer.
func (r *Registry) GetRemoteAVDs(nodeAddr string) ([]byte, error) {
	return r.remoteGet(nodeAddr, "/api/v1/discovery/avds")
}

// GetRemoteSystemImages lists system images on a peer.
func (r *Registry) GetRemoteSystemImages(nodeAddr string) ([]byte, error) {
	return r.remoteGet(nodeAddr, "/api/v1/discovery/system-images")
}

// CreateRemoteAVDs creates AVDs on a peer.
func (r *Registry) CreateRemoteAVDs(nodeAddr string, body string) ([]byte, error) {
	return r.remotePost(nodeAddr, "/api/v1/discovery/create-avds", body)
}

// BootRemoteAVD boots an AVD on a peer.
func (r *Registry) BootRemoteAVD(nodeAddr string, avdName string) ([]byte, error) {
	return r.remotePost(nodeAddr, "/api/v1/pool/boot", fmt.Sprintf(`{"avd_name":"%s"}`, avdName))
}

// ShutdownRemoteInstance shuts down an instance on a peer.
func (r *Registry) ShutdownRemoteInstance(nodeAddr string, instanceID string) ([]byte, error) {
	return r.remotePost(nodeAddr, "/api/v1/pool/shutdown", fmt.Sprintf(`{"instance_id":"%s"}`, instanceID))
}

// GetFederatedStatus returns combined status of all nodes including self.
// Uses dynamic leader election — role is "leader" or "node" based on score.
func (r *Registry) GetFederatedStatus() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	isLeader := true // assume leader unless beaten
	selfScore := leaderScore(&r.selfPeer)

	nodes := make([]map[string]any, 0, len(r.peers)+1)

	// Determine leader among all nodes
	leaderAddr := r.self
	bestScore := selfScore
	for _, p := range r.peers {
		if !p.Healthy {
			continue
		}
		s := leaderScore(p)
		addr := fmt.Sprintf("%s:%d", p.Host, p.Port)
		if s > bestScore || (s == bestScore && addr < leaderAddr) {
			leaderAddr = addr
			bestScore = s
			isLeader = false
		}
	}

	// Add self
	selfRole := "node"
	if isLeader {
		selfRole = "leader"
	}
	nodes = append(nodes, map[string]any{
		"name":      r.selfPeer.Name,
		"host":      r.self,
		"role":      selfRole,
		"capacity":  r.selfPeer.Capacity,
		"available": r.selfPeer.Available,
		"num_cpu":   r.selfPeer.NumCPU,
		"memory_mb": r.selfPeer.MemoryMB,
		"score":     selfScore,
		"healthy":   true,
	})

	// Add peers
	totalCapacity := r.selfPeer.Capacity
	totalAllocated := r.selfPeer.Capacity - r.selfPeer.Available
	totalAvailable := r.selfPeer.Available

	for _, p := range r.peers {
		pAddr := fmt.Sprintf("%s:%d", p.Host, p.Port)
		role := "node"
		if pAddr == leaderAddr {
			role = "leader"
		}
		nodes = append(nodes, map[string]any{
			"name":      p.Name,
			"host":      pAddr,
			"role":      role,
			"capacity":  p.Capacity,
			"warm":      p.Warm,
			"allocated": p.Allocated,
			"available": p.Available,
			"num_cpu":   p.NumCPU,
			"memory_mb": p.MemoryMB,
			"score":     leaderScore(p),
			"healthy":   p.Healthy,
			"last_seen": p.LastSeen.Format("2006-01-02T15:04:05Z"),
		})
		totalCapacity += p.Capacity
		totalAllocated += p.Allocated
		totalAvailable += p.Available
	}

	return map[string]any{
		"leader":          leaderAddr,
		"nodes":           nodes,
		"total_nodes":     len(r.peers) + 1,
		"total_capacity":  totalCapacity,
		"total_allocated": totalAllocated,
		"total_available": totalAvailable,
	}
}

// --- HTTP helpers ---

// remoteGet performs an HTTP GET to the given path on nodeAddr and returns the response body.
func (r *Registry) remoteGet(nodeAddr, path string) ([]byte, error) {
	resp, err := r.client.Get(fmt.Sprintf("http://%s%s", nodeAddr, path))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// remotePost performs an HTTP POST with a JSON body to the given path on nodeAddr and returns the response body.
func (r *Registry) remotePost(nodeAddr, path, body string) ([]byte, error) {
	resp, err := r.client.Post(fmt.Sprintf("http://%s%s", nodeAddr, path), "application/json", strings.NewReader(body))
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
				// Update self stats if callback set
				if r.selfUpdateFn != nil {
					r.selfUpdateFn()
				}
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
