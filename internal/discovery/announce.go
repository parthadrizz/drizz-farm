package discovery

import (
	"context"
	"fmt"
	"os"

	"github.com/grandcat/zeroconf"
	"github.com/rs/zerolog/log"
)

// Announcer registers the drizz-farm service on the local network via mDNS.
type Announcer struct {
	server *zeroconf.Server
}

// AnnounceConfig holds the parameters for mDNS service registration.
type AnnounceConfig struct {
	NodeName      string
	Port          int
	Version       string
	Tier          string
	MeshID        string // unique mesh identifier for discovery filtering
	MeshName      string // display label
	AndroidAvail  int
	IOSAvail      int
	TotalCapacity int
}

// NewAnnouncer creates and registers an mDNS service.
func NewAnnouncer(ctx context.Context, cfg AnnounceConfig) (*Announcer, error) {
	hostname, _ := os.Hostname()

	txt := []string{
		fmt.Sprintf("version=%s", cfg.Version),
		fmt.Sprintf("node=%s", cfg.NodeName),
		fmt.Sprintf("mesh_id=%s", cfg.MeshID),
		fmt.Sprintf("mesh=%s", cfg.MeshName),
		fmt.Sprintf("tier=%s", cfg.Tier),
		fmt.Sprintf("android_available=%d", cfg.AndroidAvail),
		fmt.Sprintf("ios_available=%d", cfg.IOSAvail),
		fmt.Sprintf("total_capacity=%d", cfg.TotalCapacity),
	}

	// All drizz-farm nodes use the same service type for discovery.
	// Mesh name is in the TXT record — filtering happens at the application layer.
	serviceType := "_drizz-farm._tcp"

	server, err := zeroconf.Register(
		cfg.NodeName, // Instance name
		serviceType,  // Service type (environment-scoped)
		"local.",     // Domain
		cfg.Port,           // Port
		txt,                // TXT records
		nil,                // Interfaces (nil = all)
	)
	if err != nil {
		return nil, fmt.Errorf("mdns register: %w", err)
	}

	log.Info().
		Str("node", cfg.NodeName).
		Str("hostname", hostname).
		Int("port", cfg.Port).
		Msg("discovery: mDNS service registered")

	return &Announcer{server: server}, nil
}

// Shutdown deregisters the mDNS service.
func (a *Announcer) Shutdown() {
	if a.server != nil {
		a.server.Shutdown()
		log.Info().Msg("discovery: mDNS service deregistered")
	}
}
