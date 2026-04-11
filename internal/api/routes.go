package api

import (
	"encoding/json"
	"io"
	"net/http"

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
		store:  deps.Store,
	}
	devH := &deviceHandlers{
		pool: p,
		adb:  android.NewADBClient(deps.SDK, deps.Runner),
	}
	recH := newRecordingHandlers(p, android.NewADBClient(deps.SDK, deps.Runner), deps.SDK, cfg.DataDir())
	cfgH := &configHandlers{cfg: cfg}
	histH := &historyHandlers{store: deps.Store}
	snapH := &snapshotHandlers{
		pool: p,
		adb:  android.NewADBClient(deps.SDK, deps.Runner),
	}
	screenH := &screenHandlers{
		pool: p,
		adb:  android.NewADBClient(deps.SDK, deps.Runner),
		sdk:  deps.SDK,
	}
	_ = newScreenV2Handlers
	webrtcH := &webrtcHandlers{pool: p, sdk: deps.SDK}

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
		r.Get("/sessions/{id}/screen", screenH.StreamScreen) // PNG WebSocket fallback
		r.Post("/sessions/{id}/webrtc/offer", webrtcH.Offer) // WebRTC H.264 (primary)
		r.Get("/sessions/{id}/input", screenH.SendInput)
		r.Get("/sessions/{id}/logcat", screenH.StreamLogcat)

		// Device simulation
		r.Post("/sessions/{id}/gps", devH.SetGPS)
		r.Post("/sessions/{id}/network", devH.SetNetwork)
		r.Post("/sessions/{id}/battery", devH.SetBattery)
		r.Post("/sessions/{id}/orientation", devH.SetOrientation)
		r.Post("/sessions/{id}/locale", devH.SetLocale)
		r.Post("/sessions/{id}/appearance", devH.SetDarkMode)
		r.Post("/sessions/{id}/install", devH.InstallAPK)
		r.Post("/sessions/{id}/uninstall", devH.UninstallApp)
		r.Post("/sessions/{id}/clear-data", devH.ClearData)
		r.Post("/sessions/{id}/deeplink", devH.OpenDeeplink)
		r.Post("/sessions/{id}/permissions", devH.Permissions)
		r.Post("/sessions/{id}/file/push", devH.PushFile)
		r.Post("/sessions/{id}/file/pull", devH.PullFile)
		r.Post("/sessions/{id}/biometric", devH.Biometric)
		r.Post("/sessions/{id}/camera", devH.CameraInject)
		r.Post("/sessions/{id}/timezone", devH.SetTimezone)
		r.Post("/sessions/{id}/push-notification", devH.PushNotification)
		r.Post("/sessions/{id}/clipboard", devH.Clipboard)
		r.Post("/sessions/{id}/font-scale", devH.FontScale)
		r.Post("/sessions/{id}/shake", devH.Shake)
		r.Post("/sessions/{id}/sensor", devH.Sensor)
		r.Post("/sessions/{id}/audio", devH.AudioInject)
		r.Post("/sessions/{id}/volume", devH.Volume)
		r.Post("/sessions/{id}/lock", devH.LockUnlock)
		r.Post("/sessions/{id}/animations", devH.Animations)
		r.Post("/sessions/{id}/gps-route", devH.GPSRoute)
		r.Post("/sessions/{id}/accessibility", devH.Accessibility)
		r.Post("/sessions/{id}/brightness", devH.Brightness)
		r.Post("/sessions/{id}/wifi", devH.WifiToggle)
		r.Post("/sessions/{id}/launch", devH.LaunchApp)
		r.Post("/sessions/{id}/force-stop", devH.ForceStop)
		r.Get("/sessions/{id}/ui-tree", devH.GetUITree)
		r.Get("/sessions/{id}/activity", devH.GetActivity)
		r.Get("/sessions/{id}/device-info", devH.GetDeviceInfo)
		r.Get("/sessions/{id}/notifications", devH.GetNotifications)
		r.Get("/sessions/{id}/clipboard/get", devH.GetClipboard)
		r.Get("/sessions/{id}/keyboard", devH.IsKeyboardShown)
		r.Get("/sessions/{id}/package-info", devH.GetPackageInfo)
		r.Post("/sessions/{id}/key", devH.PressKey)
		r.Post("/sessions/{id}/adb", devH.ExecADB)

		// Recording + Artifacts
		r.Post("/sessions/{id}/recording/start", recH.Start)
		r.Post("/sessions/{id}/recording/stop", recH.Stop)
		r.Get("/sessions/{id}/recording/download", recH.Download)
		r.Get("/sessions/{id}/recordings", recH.List)
		r.Post("/sessions/{id}/screenshot", recH.Screenshot)
		r.Get("/sessions/{id}/logcat/download", recH.GetLogcat)
		r.Post("/sessions/{id}/har/start", recH.StartHAR)
		r.Post("/sessions/{id}/har/stop", recH.StopHAR)
		r.Get("/sessions/{id}/har/download", recH.DownloadHAR)

		// Permanent artifact URLs (work after session ends)
		r.Get("/artifacts/{id}/*", recH.ServeArtifact)

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

		// Federation — manage all nodes from orchestrator
		r.Route("/federation", func(r chi.Router) {
			// Handshake — peer sends cluster key, we verify before accepting
			r.Post("/handshake", func(w http.ResponseWriter, r *http.Request) {
				if deps.Federation == nil {
					JSON(w, 200, map[string]string{"status": "ok"})
					return
				}
				var req struct {
					ClusterKey string `json:"cluster_key"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					JSON(w, 400, ErrorResponse{Error: "invalid", Message: "bad request", Code: 400})
					return
				}
				if !deps.Federation.VerifyHandshake(req.ClusterKey) {
					JSON(w, 403, ErrorResponse{Error: "forbidden", Message: "invalid cluster key", Code: 403})
					return
				}
				JSON(w, 200, map[string]string{"status": "ok"})
			})

			r.Get("/peers", func(w http.ResponseWriter, r *http.Request) {
				if deps.Federation == nil {
					JSON(w, 200, map[string]any{"peers": []any{}, "count": 0})
					return
				}
				peers := deps.Federation.Peers()
				JSON(w, 200, map[string]any{"peers": peers, "count": len(peers)})
			})

			r.Get("/status", func(w http.ResponseWriter, r *http.Request) {
				if deps.Federation == nil {
					JSON(w, 200, map[string]any{"nodes": []any{}, "total_nodes": 1})
					return
				}
				JSON(w, 200, deps.Federation.GetFederatedStatus())
			})

			// Proxy management to a specific node
			r.Get("/nodes/{node}/pool", func(w http.ResponseWriter, r *http.Request) {
				node := chi.URLParam(r, "node")
				data, err := deps.Federation.GetRemotePool(node)
				if err != nil { Error(w, err); return }
				w.Header().Set("Content-Type", "application/json")
				w.Write(data)
			})

			r.Get("/nodes/{node}/avds", func(w http.ResponseWriter, r *http.Request) {
				node := chi.URLParam(r, "node")
				data, err := deps.Federation.GetRemoteAVDs(node)
				if err != nil { Error(w, err); return }
				w.Header().Set("Content-Type", "application/json")
				w.Write(data)
			})

			r.Get("/nodes/{node}/system-images", func(w http.ResponseWriter, r *http.Request) {
				node := chi.URLParam(r, "node")
				data, err := deps.Federation.GetRemoteSystemImages(node)
				if err != nil { Error(w, err); return }
				w.Header().Set("Content-Type", "application/json")
				w.Write(data)
			})

			r.Post("/nodes/{node}/create-avds", func(w http.ResponseWriter, r *http.Request) {
				node := chi.URLParam(r, "node")
				body, _ := io.ReadAll(r.Body)
				data, err := deps.Federation.CreateRemoteAVDs(node, string(body))
				if err != nil { Error(w, err); return }
				w.Header().Set("Content-Type", "application/json")
				w.Write(data)
			})

			r.Post("/nodes/{node}/boot", func(w http.ResponseWriter, r *http.Request) {
				node := chi.URLParam(r, "node")
				var req struct { AVDName string `json:"avd_name"` }
				json.NewDecoder(r.Body).Decode(&req)
				data, err := deps.Federation.BootRemoteAVD(node, req.AVDName)
				if err != nil { Error(w, err); return }
				w.Header().Set("Content-Type", "application/json")
				w.Write(data)
			})

			r.Post("/nodes/{node}/shutdown", func(w http.ResponseWriter, r *http.Request) {
				node := chi.URLParam(r, "node")
				var req struct { InstanceID string `json:"instance_id"` }
				json.NewDecoder(r.Body).Decode(&req)
				data, err := deps.Federation.ShutdownRemoteInstance(node, req.InstanceID)
				if err != nil { Error(w, err); return }
				w.Header().Set("Content-Type", "application/json")
				w.Write(data)
			})
		})

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
