package android

import (
	"fmt"
	"sync"
)

// PortPair holds the console and ADB ports for an emulator.
// Android emulators use consecutive port pairs: even=console, odd=ADB.
type PortPair struct {
	Console int // Even port (e.g., 5554)
	ADB     int // Odd port (e.g., 5555)
}

// PortAllocator manages port allocation for emulator instances.
// Ports are allocated in pairs from the configured range.
type PortAllocator struct {
	mu       sync.Mutex
	minPort  int
	maxPort  int
	inUse    map[int]bool // keyed by console port (even)
}

// NewPortAllocator creates a port allocator for the given range.
// minPort should be even; maxPort is exclusive.
func NewPortAllocator(minPort, maxPort int) *PortAllocator {
	// Ensure minPort is even
	if minPort%2 != 0 {
		minPort++
	}
	return &PortAllocator{
		minPort: minPort,
		maxPort: maxPort,
		inUse:   make(map[int]bool),
	}
}

// Allocate reserves the next available port pair.
func (pa *PortAllocator) Allocate() (PortPair, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	for port := pa.minPort; port < pa.maxPort; port += 2 {
		if !pa.inUse[port] {
			pa.inUse[port] = true
			return PortPair{
				Console: port,
				ADB:     port + 1,
			}, nil
		}
	}
	return PortPair{}, fmt.Errorf("no ports available in range %d-%d (%d/%d in use)",
		pa.minPort, pa.maxPort, len(pa.inUse), (pa.maxPort-pa.minPort)/2)
}

// Release frees a previously allocated port pair.
func (pa *PortAllocator) Release(ports PortPair) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	delete(pa.inUse, ports.Console)
}

// InUseCount returns the number of port pairs currently allocated.
func (pa *PortAllocator) InUseCount() int {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	return len(pa.inUse)
}

// Capacity returns the total number of port pairs available.
func (pa *PortAllocator) Capacity() int {
	return (pa.maxPort - pa.minPort) / 2
}
