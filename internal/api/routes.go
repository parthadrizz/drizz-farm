package api

import (
	"github.com/go-chi/chi/v5"

	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/license"
	"github.com/drizz-dev/drizz-farm/internal/pool"
	"github.com/drizz-dev/drizz-farm/internal/session"
)

// RegisterRoutes sets up all API routes on the router.
func RegisterRoutes(r chi.Router, cfg *config.Config, p *pool.Pool, b *session.Broker, lic *license.Validator, deps ServerDeps) {
	sessH := &sessionHandlers{broker: b}
	poolH := &poolHandlers{pool: p}
	nodeH := &nodeHandlers{
		cfg:       cfg,
		pool:      p,
		broker:    b,
		license:   lic,
		startedAt: deps.StartedAt,
	}

	r.Route("/api/v1", func(r chi.Router) {
		// Sessions
		r.Post("/sessions", sessH.Create)
		r.Get("/sessions", sessH.List)
		r.Get("/sessions/{id}", sessH.Get)
		r.Delete("/sessions/{id}", sessH.Release)

		// Pool
		r.Get("/pool", poolH.Status)
		r.Get("/pool/available", poolH.Available)

		// Node
		r.Get("/node/health", nodeH.Health)
	})
}
