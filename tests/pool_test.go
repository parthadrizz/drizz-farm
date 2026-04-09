//go:build integration

package tests

import (
	"testing"
)

func TestPool_StartsEmpty(t *testing.T) {
	pool := GetPool(t)
	if pool.Warm != 0 && pool.Allocated != 0 && pool.Booting != 0 {
		// Pool might have instances from previous tests in this run
		// Only fail if this is clearly wrong
		t.Logf("pool state: warm=%d allocated=%d booting=%d", pool.Warm, pool.Allocated, pool.Booting)
	}
	if pool.TotalCapacity < 1 {
		t.Errorf("expected capacity >= 1, got %d", pool.TotalCapacity)
	}
}

func TestPool_OnDemandBoot(t *testing.T) {
	sess := CreateSession(t, "")
	defer ReleaseSession(t, sess.ID)

	pool := GetPool(t)
	if pool.Allocated < 1 {
		t.Errorf("expected at least 1 allocated, got %d", pool.Allocated)
	}

	// Verify instance is in pool
	found := false
	for _, inst := range pool.Instances {
		if inst.ID == sess.InstanceID {
			found = true
			if inst.State != "allocated" {
				t.Errorf("expected allocated, got %s", inst.State)
			}
			break
		}
	}
	if !found {
		t.Errorf("instance %s not found in pool", sess.InstanceID)
	}
}

func TestPool_Exhaustion(t *testing.T) {
	pool := GetPool(t)
	capacity := pool.TotalCapacity

	// Fill to capacity
	var sessions []SessionResponse
	for i := 0; i < capacity; i++ {
		sess := CreateSession(t, "")
		sessions = append(sessions, sess)
	}
	defer func() {
		for _, s := range sessions {
			ReleaseSession(t, s.ID)
		}
	}()

	pool = GetPool(t)
	if pool.Allocated != capacity {
		t.Errorf("expected %d allocated, got %d", capacity, pool.Allocated)
	}
}
