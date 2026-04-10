package api

import (
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/license"
	"github.com/drizz-dev/drizz-farm/internal/pool"
	"github.com/drizz-dev/drizz-farm/internal/session"

	"github.com/drizz-dev/drizz-farm/internal/buildinfo"
)

type nodeHandlers struct {
	cfg     *config.Config
	pool    *pool.Pool
	broker  *session.Broker
	license *license.Validator
	startedAt time.Time
}

// Health handles GET /api/v1/node/health
func (h *nodeHandlers) Health(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	poolStatus := h.pool.Status()
	lic := h.license.Current()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	JSON(w, http.StatusOK, map[string]any{
		"status":   "healthy",
		"node":     h.cfg.Node.Name,
		"hostname": hostname,
		"version":  buildinfo.Version,
		"uptime":   time.Since(h.startedAt).String(),
		"platform": runtime.GOOS + "/" + runtime.GOARCH,
		"go":       runtime.Version(),
		"license": map[string]any{
			"tier":    lic.Tier,
			"expires": lic.ExpiresAt,
		},
		"pool": map[string]any{
			"capacity":  poolStatus.TotalCapacity,
			"warm":      poolStatus.Warm,
			"allocated": poolStatus.Allocated,
			"booting":   poolStatus.Booting,
			"error":     poolStatus.Error,
		},
		"sessions": map[string]any{
			"active": h.broker.ActiveCount(),
			"queued": h.broker.QueueDepth(),
		},
		"resources": map[string]any{
			"goroutines":    runtime.NumGoroutine(),
			"heap_alloc":    memStats.HeapAlloc,
			"sys":           memStats.Sys,
			"num_cpu":       runtime.NumCPU(),
			"total_memory":  memStats.Sys,
		},
	})
}
