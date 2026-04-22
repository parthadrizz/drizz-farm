package registry

import (
	"path/filepath"
	"testing"
)

func TestEmptyRegistry(t *testing.T) {
	dir := t.TempDir()
	r, err := New(filepath.Join(dir, "nodes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if r.HasGroup() {
		t.Error("empty registry should not have group")
	}
	if len(r.Nodes()) != 0 {
		t.Error("empty registry should have no nodes")
	}
}

func TestCreateGroup(t *testing.T) {
	dir := t.TempDir()
	r, _ := New(filepath.Join(dir, "nodes.yaml"))

	key, err := r.CreateGroup("my-lab", "mac-1", "http://mac-1.local:9401")
	if err != nil {
		t.Fatal(err)
	}
	if key == "" {
		t.Error("CreateGroup should return a key")
	}
	if r.GroupName() != "my-lab" {
		t.Errorf("GroupName = %q", r.GroupName())
	}
	if !r.VerifyKey(key) {
		t.Error("VerifyKey should accept the generated key")
	}
	if r.VerifyKey("wrong") {
		t.Error("VerifyKey should reject wrong key")
	}
	nodes := r.Nodes()
	if len(nodes) != 1 || nodes[0].Name != "mac-1" {
		t.Errorf("expected 1 node (mac-1), got %v", nodes)
	}
}

func TestCreateGroup_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	r, _ := New(filepath.Join(dir, "nodes.yaml"))
	r.CreateGroup("my-lab", "mac-1", "http://mac-1.local:9401")

	_, err := r.CreateGroup("another-lab", "mac-1", "http://mac-1.local:9401")
	if err == nil {
		t.Error("should error when group already exists")
	}
}

func TestAddRemoveNode(t *testing.T) {
	dir := t.TempDir()
	r, _ := New(filepath.Join(dir, "nodes.yaml"))
	r.CreateGroup("lab", "mac-1", "http://mac-1:9401")

	if err := r.AddNode(Node{Name: "mac-2", URL: "http://mac-2:9401"}); err != nil {
		t.Fatal(err)
	}
	if len(r.Nodes()) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(r.Nodes()))
	}

	// Idempotent: adding same name replaces
	if err := r.AddNode(Node{Name: "mac-2", URL: "http://mac-2-new:9401"}); err != nil {
		t.Fatal(err)
	}
	nodes := r.Nodes()
	if len(nodes) != 2 {
		t.Errorf("re-add should not duplicate; got %d nodes", len(nodes))
	}
	var mac2URL string
	for _, n := range nodes {
		if n.Name == "mac-2" {
			mac2URL = n.URL
		}
	}
	if mac2URL != "http://mac-2-new:9401" {
		t.Errorf("URL should be updated, got %q", mac2URL)
	}

	removed, err := r.RemoveNode("mac-2")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Error("RemoveNode should return true for existing node")
	}
	if len(r.Nodes()) != 1 {
		t.Errorf("expected 1 node after remove, got %d", len(r.Nodes()))
	}

	removed, _ = r.RemoveNode("nonexistent")
	if removed {
		t.Error("RemoveNode should return false for missing node")
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.yaml")

	r1, _ := New(path)
	key, _ := r1.CreateGroup("lab", "mac-1", "http://mac-1:9401")
	r1.AddNode(Node{Name: "mac-2", URL: "http://mac-2:9401"})

	// Reload from disk
	r2, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if r2.GroupName() != "lab" {
		t.Errorf("GroupName not persisted: %q", r2.GroupName())
	}
	if !r2.VerifyKey(key) {
		t.Error("key should survive reload")
	}
	if len(r2.Nodes()) != 2 {
		t.Errorf("nodes not persisted: got %d", len(r2.Nodes()))
	}
}

func TestSnapshot_HidesKey(t *testing.T) {
	dir := t.TempDir()
	r, _ := New(filepath.Join(dir, "nodes.yaml"))
	r.CreateGroup("lab", "mac-1", "http://mac-1:9401")

	snap := r.Snapshot()
	if snap.GroupKey != "" {
		t.Error("Snapshot must not leak group key")
	}
	if snap.GroupName != "lab" {
		t.Error("Snapshot should include group name")
	}
}

func TestImportFromJSON(t *testing.T) {
	// Simulate joining: source node exports, target imports
	dirA := t.TempDir()
	rA, _ := New(filepath.Join(dirA, "nodes.yaml"))
	key, _ := rA.CreateGroup("shared", "mac-a", "http://mac-a:9401")
	rA.AddNode(Node{Name: "mac-b", URL: "http://mac-b:9401"})

	data, err := rA.MarshalForExport()
	if err != nil {
		t.Fatal(err)
	}

	dirB := t.TempDir()
	rB, _ := New(filepath.Join(dirB, "nodes.yaml"))
	if err := rB.ImportFromJSON(data); err != nil {
		t.Fatal(err)
	}
	if rB.GroupName() != "shared" {
		t.Errorf("imported group name = %q", rB.GroupName())
	}
	if !rB.VerifyKey(key) {
		t.Error("imported registry should have same key")
	}
	if len(rB.Nodes()) != 2 {
		t.Errorf("imported registry should have 2 nodes, got %d", len(rB.Nodes()))
	}
}

func TestLeaveGroup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.yaml")
	r, _ := New(path)
	r.CreateGroup("lab", "mac-1", "http://mac-1:9401")

	if err := r.LeaveGroup(); err != nil {
		t.Fatal(err)
	}
	if r.HasGroup() {
		t.Error("HasGroup should be false after leave")
	}
	// File should be deleted; reload from disk confirms empty state
	r2, _ := New(path)
	if r2.HasGroup() {
		t.Error("reloaded registry after leave should be empty")
	}
}
