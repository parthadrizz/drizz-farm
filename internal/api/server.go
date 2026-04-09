package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/drizz-dev/drizz-farm/internal/android"
	"github.com/drizz-dev/drizz-farm/internal/config"
	"github.com/drizz-dev/drizz-farm/internal/license"
	"github.com/drizz-dev/drizz-farm/internal/pool"
	"github.com/drizz-dev/drizz-farm/internal/session"
)

// ServerDeps holds shared dependencies for the API server.
type ServerDeps struct {
	StartedAt time.Time
	SDK       *android.SDK
	Runner    android.CommandRunner
}

// Server is the HTTP API server.
type Server struct {
	httpServer *http.Server
	router     chi.Router
}

// NewServer creates and configures the API server.
func NewServer(cfg *config.Config, p *pool.Pool, b *session.Broker, lic *license.Validator, deps ServerDeps) *Server {
	r := chi.NewRouter()

	// Middleware stack
	r.Use(Recovery)
	r.Use(RequestID)
	r.Use(Logger)
	r.Use(CORS)

	// Register API routes
	RegisterRoutes(r, cfg, p, b, lic, deps)

	// Root handler
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]string{
			"service": "drizz-farm",
			"version": "dev",
			"docs":    fmt.Sprintf("http://%s:%d/api/v1", cfg.API.Host, cfg.API.Port),
		})
	})

	addr := fmt.Sprintf("%s:%d", cfg.API.Host, cfg.API.Port)
	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      r,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		router: r,
	}
}

// Start begins serving HTTP requests.
func (s *Server) Start() error {
	log.Info().Str("addr", s.httpServer.Addr).Msg("api: server starting")
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("api server: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info().Msg("api: server shutting down")
	return s.httpServer.Shutdown(ctx)
}

// Router returns the chi router (useful for testing).
func (s *Server) Router() chi.Router {
	return s.router
}
