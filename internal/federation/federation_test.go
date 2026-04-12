package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Leader Election Tests ---

func TestSingleNode_BecomesLeader(t *testing.T) {
	// Node A boots alone — no peers. Should be leader.
	reg := NewRegistry("10.0.0.1", 9401, "")
	reg.UpdateSelf("MacMini-A", 3, 3, 10, 16000)

	if !reg.IsLeader() {
		t.Error("single node should be leader")
	}
	if name := reg.LeaderName(); name != "MacMini-A" {
		t.Errorf("leader should be MacMini-A, got %s", name)
	}
}

func TestTwoNodes_HigherScoreLeads(t *testing.T) {
	// Node A (10 CPU, 16GB) vs Node B (4 CPU, 8GB).
	// A should lead because higher score.
	reg := NewRegistry("10.0.0.1", 9401, "")
	reg.UpdateSelf("MacMini-A", 3, 3, 10, 16000) // score = 3*10 + 10*5 + 16*3 + 3*2 = 30+50+48+6 = 134

	reg.AddPeer("MacMini-B", "10.0.0.2", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.2:9401"].Capacity = 2
	reg.peers["10.0.0.2:9401"].Available = 2
	reg.peers["10.0.0.2:9401"].NumCPU = 4
	reg.peers["10.0.0.2:9401"].MemoryMB = 8000
	reg.peers["10.0.0.2:9401"].Healthy = true
	reg.mu.Unlock()
	// B score = 2*10 + 4*5 + 8*3 + 2*2 = 20+20+24+4 = 68

	if !reg.IsLeader() {
		t.Error("MacMini-A (score 134) should lead over MacMini-B (score 68)")
	}

	// Now make B stronger — 16 CPU, 32GB, 5 capacity
	reg.mu.Lock()
	reg.peers["10.0.0.2:9401"].Capacity = 5
	reg.peers["10.0.0.2:9401"].Available = 5
	reg.peers["10.0.0.2:9401"].NumCPU = 16
	reg.peers["10.0.0.2:9401"].MemoryMB = 32000
	reg.mu.Unlock()
	// B score = 5*10 + 16*5 + 32*3 + 5*2 = 50+80+96+10 = 236

	if reg.IsLeader() {
		t.Error("MacMini-A should NOT lead when MacMini-B has higher score")
	}
	if name := reg.LeaderName(); name != "MacMini-B" {
		t.Errorf("leader should be MacMini-B, got %s", name)
	}
}

func TestThreeNodes_LeaderElection(t *testing.T) {
	// Three Mac Minis boot up. The one with the highest score leads.
	reg := NewRegistry("10.0.0.1", 9401, "")
	reg.UpdateSelf("MacMini-A", 3, 3, 8, 16000) // score = 30+40+48+6 = 124

	// B: weaker
	reg.AddPeer("MacMini-B", "10.0.0.2", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.2:9401"].Capacity = 2
	reg.peers["10.0.0.2:9401"].Available = 2
	reg.peers["10.0.0.2:9401"].NumCPU = 4
	reg.peers["10.0.0.2:9401"].MemoryMB = 8000
	reg.peers["10.0.0.2:9401"].Healthy = true
	reg.mu.Unlock()
	// B score = 20+20+24+4 = 68

	// C: strongest
	reg.AddPeer("MacMini-C", "10.0.0.3", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.3:9401"].Capacity = 5
	reg.peers["10.0.0.3:9401"].Available = 5
	reg.peers["10.0.0.3:9401"].NumCPU = 12
	reg.peers["10.0.0.3:9401"].MemoryMB = 32000
	reg.peers["10.0.0.3:9401"].Healthy = true
	reg.mu.Unlock()
	// C score = 50+60+96+10 = 216

	if reg.IsLeader() {
		t.Error("MacMini-A should not be leader when C has highest score")
	}
	if name := reg.LeaderName(); name != "MacMini-C" {
		t.Errorf("leader should be MacMini-C, got %s", name)
	}

	// Verify federated status shows correct roles
	status := reg.GetFederatedStatus()
	nodes := status["nodes"].([]map[string]any)
	roleMap := make(map[string]string)
	for _, n := range nodes {
		roleMap[n["name"].(string)] = n["role"].(string)
	}
	if roleMap["MacMini-C"] != "leader" {
		t.Errorf("C should be leader in status, got %s", roleMap["MacMini-C"])
	}
	if roleMap["MacMini-A"] != "node" {
		t.Errorf("A should be node in status, got %s", roleMap["MacMini-A"])
	}
	if roleMap["MacMini-B"] != "node" {
		t.Errorf("B should be node in status, got %s", roleMap["MacMini-B"])
	}
}

func TestTiebreaker_LowestHostWins(t *testing.T) {
	// Same score — lowest host:port should win.
	reg := NewRegistry("10.0.0.2", 9401, "") // self = 10.0.0.2:9401
	reg.UpdateSelf("MacMini-B", 3, 3, 8, 16000) // score = 124

	reg.AddPeer("MacMini-A", "10.0.0.1", 9401) // lower host
	reg.mu.Lock()
	reg.peers["10.0.0.1:9401"].Capacity = 3
	reg.peers["10.0.0.1:9401"].Available = 3
	reg.peers["10.0.0.1:9401"].NumCPU = 8
	reg.peers["10.0.0.1:9401"].MemoryMB = 16000
	reg.peers["10.0.0.1:9401"].Healthy = true
	reg.mu.Unlock()
	// Same score = 124, but A has lower host → A wins

	if reg.IsLeader() {
		t.Error("B should lose tiebreaker to A (lower host)")
	}
	if name := reg.LeaderName(); name != "MacMini-A" {
		t.Errorf("A should win tiebreaker, got %s", name)
	}
}

// --- Leader Failover Tests ---

func TestLeaderFailover_PeerDies(t *testing.T) {
	// C is leader. C goes unhealthy. A should become leader.
	reg := NewRegistry("10.0.0.1", 9401, "")
	reg.UpdateSelf("MacMini-A", 3, 3, 8, 16000) // score 124

	reg.AddPeer("MacMini-C", "10.0.0.3", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.3:9401"].Capacity = 5
	reg.peers["10.0.0.3:9401"].Available = 5
	reg.peers["10.0.0.3:9401"].NumCPU = 12
	reg.peers["10.0.0.3:9401"].MemoryMB = 32000
	reg.peers["10.0.0.3:9401"].Healthy = true
	reg.mu.Unlock()

	// C is leader
	if reg.IsLeader() {
		t.Fatal("A should not be leader when C is healthy")
	}

	// C dies — mark unhealthy
	reg.mu.Lock()
	reg.peers["10.0.0.3:9401"].Healthy = false
	reg.mu.Unlock()

	// A should now be leader (unhealthy peers are excluded from election)
	if !reg.IsLeader() {
		t.Error("A should become leader after C goes unhealthy")
	}
	if name := reg.LeaderName(); name != "MacMini-A" {
		t.Errorf("leader should be MacMini-A after failover, got %s", name)
	}
}

func TestLeaderFailover_PeerPruned(t *testing.T) {
	// Simulate the stale peer pruning that happens in StartRefreshLoop.
	reg := NewRegistry("10.0.0.1", 9401, "")
	reg.UpdateSelf("MacMini-A", 3, 3, 8, 16000)

	reg.AddPeer("MacMini-C", "10.0.0.3", 9401)
	reg.mu.Lock()
	p := reg.peers["10.0.0.3:9401"]
	p.Capacity = 5
	p.Available = 5
	p.NumCPU = 12
	p.MemoryMB = 32000
	p.Healthy = true
	p.LastSeen = time.Now().Add(-90 * time.Second) // stale (>60s)
	reg.mu.Unlock()

	// Simulate pruning logic
	reg.mu.Lock()
	for k, peer := range reg.peers {
		if time.Since(peer.LastSeen) > 60*time.Second {
			delete(reg.peers, k)
		}
	}
	reg.mu.Unlock()

	if reg.PeerCount() != 0 {
		t.Errorf("stale peer should be pruned, got %d peers", reg.PeerCount())
	}
	if !reg.IsLeader() {
		t.Error("A should be leader after C is pruned")
	}
}

func TestLeaderReElection_ScoreChanges(t *testing.T) {
	// A starts as leader. B's capacity increases (devices freed). B becomes leader.
	reg := NewRegistry("10.0.0.1", 9401, "")
	reg.UpdateSelf("MacMini-A", 3, 1, 8, 16000) // only 1 available → score = 10+40+48+6 = 104

	reg.AddPeer("MacMini-B", "10.0.0.2", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.2:9401"].Capacity = 3
	reg.peers["10.0.0.2:9401"].Available = 0
	reg.peers["10.0.0.2:9401"].NumCPU = 8
	reg.peers["10.0.0.2:9401"].MemoryMB = 16000
	reg.peers["10.0.0.2:9401"].Healthy = true
	reg.mu.Unlock()
	// B score = 0+40+48+6 = 94

	if !reg.IsLeader() {
		t.Fatal("A (score 104) should lead over B (score 94)")
	}

	// B frees all devices
	reg.mu.Lock()
	reg.peers["10.0.0.2:9401"].Available = 3
	reg.mu.Unlock()
	// B score = 30+40+48+6 = 124

	// A still has 1 available → score 104
	// B now 124 > 104 → B leads
	if reg.IsLeader() {
		t.Error("B should now lead after freeing devices (score 124 > 104)")
	}
}

// --- Pool Semaphore Tests ---

func TestPoolSemaphore_EnforcesCapacity(t *testing.T) {
	// Simulate a buffered channel semaphore (same pattern as pool.go)
	sem := make(chan struct{}, 3)

	// Acquire 3 slots
	for i := 0; i < 3; i++ {
		select {
		case sem <- struct{}{}:
			// acquired
		default:
			t.Fatalf("should acquire slot %d", i)
		}
	}

	// 4th should fail (non-blocking)
	select {
	case sem <- struct{}{}:
		t.Error("4th acquire should fail — pool is full")
	default:
		// correct: pool exhausted
	}

	// Release one
	<-sem

	// Now 4th should succeed
	select {
	case sem <- struct{}{}:
		// correct
	default:
		t.Error("4th should succeed after release")
	}
}

func TestPoolSemaphore_ConcurrentAccess(t *testing.T) {
	// 10 goroutines fight for 3 slots. Exactly 3 should win, 7 should fail.
	sem := make(chan struct{}, 3)
	var acquired int32
	var rejected int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				atomic.AddInt32(&acquired, 1)
				time.Sleep(50 * time.Millisecond) // hold slot briefly
				<-sem
			default:
				atomic.AddInt32(&rejected, 1)
			}
		}()
	}

	wg.Wait()

	// At least 3 should have acquired (could be more since we release quickly)
	if acquired < 3 {
		t.Errorf("expected at least 3 acquired, got %d", acquired)
	}
	if acquired+rejected != 10 {
		t.Errorf("expected 10 total, got %d acquired + %d rejected", acquired, rejected)
	}
}

// --- Federation Remote Session Routing ---

func TestFederationRouting_PeerWithCapacity(t *testing.T) {
	reg := NewRegistry("10.0.0.1", 9401, "")

	// Add peer with capacity
	reg.AddPeer("MacMini-B", "10.0.0.2", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.2:9401"].Capacity = 3
	reg.peers["10.0.0.2:9401"].Allocated = 1
	reg.peers["10.0.0.2:9401"].Available = 2
	reg.peers["10.0.0.2:9401"].Healthy = true
	reg.mu.Unlock()

	peer := reg.FindPeerWithCapacity()
	if peer == nil {
		t.Fatal("should find peer with capacity")
	}
	if peer.Name != "MacMini-B" {
		t.Errorf("expected MacMini-B, got %s", peer.Name)
	}
}

func TestFederationRouting_NoPeerCapacity(t *testing.T) {
	reg := NewRegistry("10.0.0.1", 9401, "")

	// Peer exists but no capacity
	reg.AddPeer("MacMini-B", "10.0.0.2", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.2:9401"].Available = 0
	reg.peers["10.0.0.2:9401"].Healthy = true
	reg.mu.Unlock()

	peer := reg.FindPeerWithCapacity()
	if peer != nil {
		t.Error("should not find peer when all at capacity")
	}
}

func TestFederationRouting_UnhealthyPeerSkipped(t *testing.T) {
	reg := NewRegistry("10.0.0.1", 9401, "")

	reg.AddPeer("MacMini-B", "10.0.0.2", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.2:9401"].Available = 5
	reg.peers["10.0.0.2:9401"].Healthy = false // down
	reg.mu.Unlock()

	peer := reg.FindPeerWithCapacity()
	if peer != nil {
		t.Error("should not route to unhealthy peer")
	}
}

func TestFederationRouting_BestPeerSelected(t *testing.T) {
	reg := NewRegistry("10.0.0.1", 9401, "")

	reg.AddPeer("MacMini-B", "10.0.0.2", 9401)
	reg.AddPeer("MacMini-C", "10.0.0.3", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.2:9401"].Capacity = 3
	reg.peers["10.0.0.2:9401"].Allocated = 2
	reg.peers["10.0.0.2:9401"].Available = 1
	reg.peers["10.0.0.2:9401"].Healthy = true
	reg.peers["10.0.0.3:9401"].Capacity = 5
	reg.peers["10.0.0.3:9401"].Allocated = 0
	reg.peers["10.0.0.3:9401"].Available = 5
	reg.peers["10.0.0.3:9401"].Healthy = true
	reg.mu.Unlock()

	peer := reg.FindPeerWithCapacity()
	if peer == nil || peer.Name != "MacMini-C" {
		t.Errorf("should select MacMini-C (most available), got %v", peer)
	}
}

// --- Mock Peer HTTP Server for Remote Sessions ---

func newMockPeerServer(t *testing.T, poolResponse map[string]any, sessionResponse map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/pool", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(poolResponse)
	})

	mux.HandleFunc("/api/v1/node/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"resources": map[string]any{
				"num_cpu":      8,
				"total_memory": 16000 * 1024 * 1024,
			},
		})
	})

	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201) // broker returns 201 on session create
			json.NewEncoder(w).Encode(sessionResponse)
		} else if r.Method == "DELETE" {
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]string{"status": "released"})
		}
	})

	return httptest.NewServer(mux)
}

func TestCreateRemoteSession_Success(t *testing.T) {
	srv := newMockPeerServer(t,
		map[string]any{"total_capacity": 3, "warm": 2, "allocated": 1, "booting": 0},
		map[string]any{
			"id":      "remote-sess-123",
			"profile": "api34",
			"connection": map[string]any{
				"host":       "10.0.0.2",
				"adb_serial": "emulator-5554",
				"adb_port":   5555,
				"appium_url": "http://10.0.0.2:4723",
			},
		},
	)
	defer srv.Close()

	// Extract host:port from test server URL
	addr := strings.TrimPrefix(srv.URL, "http://")
	parts := strings.SplitN(addr, ":", 2)
	host := parts[0]
	port := 0
	fmt.Sscanf(parts[1], "%d", &port)

	reg := NewRegistry("10.0.0.1", 9401, "")
	reg.AddPeer("MockPeer", host, port)

	peer := &Peer{Name: "MockPeer", Host: host, Port: port, Healthy: true, Available: 2}
	result, err := reg.CreateRemoteSession(peer, "api34")
	if err != nil {
		t.Fatalf("CreateRemoteSession failed: %v", err)
	}
	if result["id"] != "remote-sess-123" {
		t.Errorf("expected session id 'remote-sess-123', got %v", result["id"])
	}
}

func TestReleaseRemoteSession_Success(t *testing.T) {
	var released bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" && strings.Contains(r.URL.Path, "/sessions/") {
			released = true
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	reg := NewRegistry("10.0.0.1", 9401, "")
	err := reg.ReleaseRemoteSession(addr, "remote-sess-123")
	if err != nil {
		t.Fatalf("ReleaseRemoteSession failed: %v", err)
	}
	if !released {
		t.Error("peer should have received DELETE request")
	}
}

// --- RefreshPeers with Mock HTTP Server ---

func TestRefreshPeers_UpdatesCapacity(t *testing.T) {
	srv := newMockPeerServer(t,
		map[string]any{"total_capacity": 5, "warm": 3, "allocated": 1, "booting": 0},
		nil,
	)
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	parts := strings.SplitN(addr, ":", 2)
	host := parts[0]
	port := 0
	fmt.Sscanf(parts[1], "%d", &port)

	reg := NewRegistry("10.0.0.1", 9401, "")
	reg.AddPeer("MockPeer", host, port)

	reg.RefreshPeers(context.Background())
	time.Sleep(500 * time.Millisecond) // wait for goroutine

	reg.mu.RLock()
	peer := reg.peers[addr]
	reg.mu.RUnlock()

	if peer == nil {
		t.Fatal("peer should exist after refresh")
	}
	if peer.Capacity != 5 {
		t.Errorf("expected capacity 5, got %d", peer.Capacity)
	}
	if !peer.Healthy {
		t.Error("peer should be healthy after successful refresh")
	}
	if peer.Available != 4 { // 5 - 1 - 0
		t.Errorf("expected 4 available, got %d", peer.Available)
	}
}

func TestRefreshPeers_MarksUnhealthyOnFailure(t *testing.T) {
	// Server that immediately closes
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	srv.Close() // close immediately

	addr := strings.TrimPrefix(srv.URL, "http://")
	parts := strings.SplitN(addr, ":", 2)
	host := parts[0]
	port := 0
	fmt.Sscanf(parts[1], "%d", &port)

	reg := NewRegistry("10.0.0.1", 9401, "")
	reg.AddPeer("DeadPeer", host, port)
	reg.mu.Lock()
	reg.peers[addr] = &Peer{Name: "DeadPeer", Host: host, Port: port, Healthy: true, LastSeen: time.Now()}
	reg.mu.Unlock()

	reg.RefreshPeers(context.Background())
	time.Sleep(500 * time.Millisecond)

	reg.mu.RLock()
	peer := reg.peers[addr]
	reg.mu.RUnlock()

	if peer != nil && peer.Healthy {
		t.Error("peer should be marked unhealthy after failed refresh")
	}
}

// --- Full Cluster Lifecycle Simulation ---

func TestClusterLifecycle_BootSequence(t *testing.T) {
	// Simulates: A boots → leader. B boots → A still leads. C boots → C leads.
	// C dies → A leads again.

	reg := NewRegistry("10.0.0.1", 9401, "")

	// Step 1: A boots alone
	reg.UpdateSelf("MacMini-A", 3, 3, 8, 16000) // score 124
	if !reg.IsLeader() {
		t.Fatal("Step 1: A should be leader when alone")
	}
	t.Log("Step 1: A boots → LEADER ✓")

	// Step 2: B boots (weaker)
	reg.AddPeer("MacMini-B", "10.0.0.2", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.2:9401"].Capacity = 2
	reg.peers["10.0.0.2:9401"].Available = 2
	reg.peers["10.0.0.2:9401"].NumCPU = 4
	reg.peers["10.0.0.2:9401"].MemoryMB = 8000
	reg.peers["10.0.0.2:9401"].Healthy = true
	reg.peers["10.0.0.2:9401"].LastSeen = time.Now()
	reg.mu.Unlock()

	if !reg.IsLeader() {
		t.Fatal("Step 2: A should still lead over weaker B")
	}
	t.Log("Step 2: B boots → A still LEADER ✓")

	// Step 3: C boots (strongest — 16 CPU, 32GB, 5 capacity)
	reg.AddPeer("MacMini-C", "10.0.0.3", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.3:9401"].Capacity = 5
	reg.peers["10.0.0.3:9401"].Available = 5
	reg.peers["10.0.0.3:9401"].NumCPU = 16
	reg.peers["10.0.0.3:9401"].MemoryMB = 32000
	reg.peers["10.0.0.3:9401"].Healthy = true
	reg.peers["10.0.0.3:9401"].LastSeen = time.Now()
	reg.mu.Unlock()

	if reg.IsLeader() {
		t.Fatal("Step 3: A should NOT lead when C is stronger")
	}
	if name := reg.LeaderName(); name != "MacMini-C" {
		t.Fatalf("Step 3: leader should be C, got %s", name)
	}
	t.Log("Step 3: C boots → C becomes LEADER ✓")

	// Step 4: C crashes (goes unhealthy)
	reg.mu.Lock()
	reg.peers["10.0.0.3:9401"].Healthy = false
	reg.mu.Unlock()

	if !reg.IsLeader() {
		t.Fatal("Step 4: A should become leader after C crashes")
	}
	t.Log("Step 4: C dies → A becomes LEADER ✓")

	// Step 5: C comes back
	reg.mu.Lock()
	reg.peers["10.0.0.3:9401"].Healthy = true
	reg.peers["10.0.0.3:9401"].LastSeen = time.Now()
	reg.mu.Unlock()

	if reg.IsLeader() {
		t.Fatal("Step 5: C should reclaim leadership")
	}
	t.Log("Step 5: C recovers → C reclaims LEADER ✓")

	// Step 6: C gets pruned (stale >60s)
	reg.mu.Lock()
	reg.peers["10.0.0.3:9401"].LastSeen = time.Now().Add(-90 * time.Second)
	reg.mu.Unlock()

	// Simulate prune
	reg.mu.Lock()
	for k, p := range reg.peers {
		if time.Since(p.LastSeen) > 60*time.Second {
			delete(reg.peers, k)
		}
	}
	reg.mu.Unlock()

	if !reg.IsLeader() {
		t.Fatal("Step 6: A should be leader after C is pruned")
	}
	if reg.PeerCount() != 1 { // only B left
		t.Fatalf("Step 6: should have 1 peer (B), got %d", reg.PeerCount())
	}
	t.Log("Step 6: C pruned → A LEADER, 1 peer remaining ✓")
}

// --- Federated Status Output ---

func TestFederatedStatus_IncludesAllNodes(t *testing.T) {
	reg := NewRegistry("10.0.0.1", 9401, "")
	reg.UpdateSelf("MacMini-A", 3, 2, 8, 16000)

	reg.AddPeer("MacMini-B", "10.0.0.2", 9401)
	reg.mu.Lock()
	reg.peers["10.0.0.2:9401"].Capacity = 3
	reg.peers["10.0.0.2:9401"].Available = 3
	reg.peers["10.0.0.2:9401"].NumCPU = 8
	reg.peers["10.0.0.2:9401"].MemoryMB = 16000
	reg.peers["10.0.0.2:9401"].Healthy = true
	reg.mu.Unlock()

	status := reg.GetFederatedStatus()

	if status["total_nodes"].(int) != 2 {
		t.Errorf("expected 2 nodes, got %v", status["total_nodes"])
	}

	nodes := status["nodes"].([]map[string]any)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 node entries, got %d", len(nodes))
	}

	// Check aggregates include self
	totalCap := status["total_capacity"].(int)
	if totalCap != 6 { // 3 + 3
		t.Errorf("expected total capacity 6, got %d", totalCap)
	}

	// Every node should have a score
	for _, n := range nodes {
		if _, ok := n["score"]; !ok {
			t.Errorf("node %s missing score", n["name"])
		}
	}

	// Should have a leader field
	if _, ok := status["leader"]; !ok {
		t.Error("status should include leader field")
	}
}

// --- Score Calculation ---

func TestLeaderScore_Calculation(t *testing.T) {
	p := &Peer{
		Available: 3,
		NumCPU:    8,
		MemoryMB:  16000,
		Capacity:  3,
	}
	// score = 3*10 + 8*5 + (16000/1024)*3 + 3*2 = 30 + 40 + 46 + 6 = 122
	// Note: 16000/1024 = 15 (int division) → 15*3 = 45
	score := leaderScore(p)
	expected := 3*10 + 8*5 + (16000/1024)*3 + 3*2
	if score != expected {
		t.Errorf("expected score %d, got %d", expected, score)
	}
}

func TestLeaderScore_ZeroValues(t *testing.T) {
	p := &Peer{}
	score := leaderScore(p)
	if score != 0 {
		t.Errorf("empty peer should have score 0, got %d", score)
	}
}

// --- Cluster Auth Tests ---

func TestHandshake_CorrectKey(t *testing.T) {
	reg := NewRegistry("10.0.0.1", 9401, "my-secret-key")
	if !reg.VerifyHandshake("my-secret-key") {
		t.Error("correct key should pass handshake")
	}
}

func TestHandshake_WrongKey(t *testing.T) {
	reg := NewRegistry("10.0.0.1", 9401, "my-secret-key")
	if reg.VerifyHandshake("wrong-key") {
		t.Error("wrong key should fail handshake")
	}
}

func TestHandshake_EmptyKey(t *testing.T) {
	reg := NewRegistry("10.0.0.1", 9401, "")
	// Empty mesh key = no auth required
	if reg.VerifyHandshake("anything") {
		t.Error("empty mesh key means auth is disabled, VerifyHandshake should return false")
	}
}

func TestAddPeer_WithAuth(t *testing.T) {
	// Start a mock peer that accepts handshakes with the right key
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/federation/handshake", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MeshKey string `json:"mesh_key"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.MeshKey == "shared-secret" {
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		} else {
			w.WriteHeader(403)
			json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	parts := strings.SplitN(addr, ":", 2)
	host := parts[0]
	port := 0
	fmt.Sscanf(parts[1], "%d", &port)

	// Registry with correct key — should accept peer
	reg := NewRegistry("10.0.0.1", 9401, "shared-secret")
	reg.AddPeer("GoodPeer", host, port)

	if reg.PeerCount() != 1 {
		t.Errorf("peer with correct key should be accepted, got %d peers", reg.PeerCount())
	}
}

func TestAddPeer_WrongKey_Rejected(t *testing.T) {
	// Mock peer that only accepts "correct-key"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/federation/handshake", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	parts := strings.SplitN(addr, ":", 2)
	host := parts[0]
	port := 0
	fmt.Sscanf(parts[1], "%d", &port)

	reg := NewRegistry("10.0.0.1", 9401, "wrong-key")
	reg.AddPeer("BadPeer", host, port)

	if reg.PeerCount() != 0 {
		t.Errorf("peer with wrong key should be rejected, got %d peers", reg.PeerCount())
	}
}
