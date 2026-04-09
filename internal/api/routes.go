package api

import (
	"github.com/go-chi/chi/v5"

	"github.com/drizz-dev/drizz-farm/internal/android"
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
	discH := &discoveryHandlers{
		sdk:    deps.SDK,
		runner: deps.Runner,
	}
	devH := &deviceHandlers{
		pool: p,
		adb:  android.NewADBClient(deps.SDK, deps.Runner),
	}
	cfgH := &configHandlers{cfg: cfg}
	histH := &historyHandlers{store: deps.Store}
	snapH := &snapshotHandlers{
		pool: p,
		adb:  android.NewADBClient(deps.SDK, deps.Runner),
	}
	screenH := &screenHandlers{
		pool: p,
		adb:  android.NewADBClient(deps.SDK, deps.Runner),
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
		r.Post("/pool/boot", poolH.Boot)
		r.Post("/pool/shutdown", poolH.Shutdown)

		// Node
		r.Get("/node/health", nodeH.Health)

		// Screen streaming + input (WebSocket)
		r.Get("/sessions/{id}/screen", screenH.StreamScreen)
		r.Get("/sessions/{id}/input", screenH.SendInput)

		// Device simulation
		r.Post("/sessions/{id}/gps", devH.SetGPS)
		r.Post("/sessions/{id}/network", devH.SetNetwork)
		r.Post("/sessions/{id}/battery", devH.SetBattery)
		r.Post("/sessions/{id}/orientation", devH.SetOrientation)
		r.Post("/sessions/{id}/locale", devH.SetLocale)
		r.Post("/sessions/{id}/appearance", devH.SetDarkMode)
		r.Post("/sessions/{id}/install", devH.InstallAPK)
		r.Post("/sessions/{id}/deeplink", devH.OpenDeeplink)
		r.Post("/sessions/{id}/adb", devH.ExecADB)

		// Snapshots
		r.Post("/sessions/{id}/snapshot/save", snapH.Save)
		r.Post("/sessions/{id}/snapshot/restore", snapH.Restore)
		r.Get("/sessions/{id}/snapshots", snapH.List)
		r.Delete("/sessions/{id}/snapshot/{name}", snapH.Delete)

		// Config
		r.Get("/config", cfgH.GetConfig)
		r.Put("/config", cfgH.UpdateConfig)
		r.Get("/config/raw", cfgH.GetConfigRaw)
		r.Put("/config/raw", cfgH.SaveConfigRaw)

		// History
		r.Get("/history/sessions", histH.SessionHistory)
		r.Get("/history/events", histH.Events)

		// Discovery (for Create Wizard)
		r.Route("/discovery", func(r chi.Router) {
			r.Get("/system-images", discH.SystemImages)
			r.Get("/available-images", discH.AvailableImages)
			r.Post("/install-image", discH.InstallImage)
			r.Get("/devices", discH.Devices)
			r.Get("/avds", discH.AVDs)
			r.Post("/create-avds", discH.CreateAVDs)
		})
	})
}
