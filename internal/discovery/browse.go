package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/rs/zerolog/log"
)

// Node represents a discovered drizz-farm node on the LAN.
type Node struct {
	Name          string `json:"name"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	MeshID        string `json:"mesh_id"`
	MeshName      string `json:"mesh_name"`
	Version       string `json:"version"`
	Tier          string `json:"tier"`
	TotalCapacity int    `json:"total_capacity"`
	AndroidAvail  int    `json:"android_available"`
	IOSAvail      int    `json:"ios_available"`
}

// Browse discovers drizz-farm nodes on the local network in the default mesh.
func Browse(ctx context.Context, timeout time.Duration) ([]Node, error) {
	return BrowseMesh(ctx, timeout, "default")
}

// BrowseMesh discovers drizz-farm nodes filtered by mesh name.
// Nodes in different meshes use different mDNS service types
// and never see each other.
func BrowseMesh(ctx context.Context, timeout time.Duration, meshName string) ([]Node, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, fmt.Errorf("mdns resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	var nodes []Node

	go func() {
		for entry := range entries {
			node := Node{
				Name: entry.Instance,
				Port: entry.Port,
			}

			// Pick first IPv4
			for _, ip := range entry.AddrIPv4 {
				node.Host = ip.String()
				break
			}
			if node.Host == "" {
				for _, ip := range entry.AddrIPv6 {
					node.Host = ip.String()
					break
				}
			}

			// Parse TXT records
			for _, txt := range entry.Text {
				parseTXTRecord(txt, &node)
			}

			// Filter by mesh ID (empty = return all, for setup scanning)
			if meshName == "" || node.MeshID == meshName || node.MeshName == meshName {
				nodes = append(nodes, node)
				log.Debug().Str("node", node.Name).Str("host", node.Host).Str("mesh", node.MeshName).Msg("discovery: found node")
			}
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	serviceType := "_drizz-farm._tcp"
	if err := resolver.Browse(ctx, serviceType, "local.", entries); err != nil {
		return nil, fmt.Errorf("mdns browse: %w", err)
	}

	<-ctx.Done()
	return nodes, nil
}

func parseTXTRecord(txt string, node *Node) {
	for i := 0; i < len(txt); i++ {
		if txt[i] == '=' {
			key := txt[:i]
			val := txt[i+1:]
			switch key {
			case "version":
				node.Version = val
			case "mesh_id":
				node.MeshID = val
			case "mesh":
				node.MeshName = val
			case "tier":
				node.Tier = val
			case "total_capacity":
				fmt.Sscanf(val, "%d", &node.TotalCapacity)
			case "android_available":
				fmt.Sscanf(val, "%d", &node.AndroidAvail)
			case "ios_available":
				fmt.Sscanf(val, "%d", &node.IOSAvail)
			case "node":
				if node.Name == "" {
					node.Name = val
				}
			}
			return
		}
	}
}
