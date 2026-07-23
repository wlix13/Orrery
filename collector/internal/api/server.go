// Package api serves the JSON API, the embedded dashboard, and /metrics.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wlix13/orrery/collector/internal/config"
	"github.com/wlix13/orrery/collector/internal/promexp"
	"github.com/wlix13/orrery/collector/internal/store"
)

type Server struct {
	store   store.Store
	cfg     *config.Config
	log     *slog.Logger
	startAt time.Time
	version string
}

func New(st store.Store, cfg *config.Config, version string, log *slog.Logger) *Server {
	return &Server{store: st, cfg: cfg, log: log, startAt: time.Now(), version: version}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "ok",
			"version":  s.version,
			"uptime_s": int64(time.Since(s.startAt).Seconds()),
		})
	})

	api := http.NewServeMux()
	api.HandleFunc("GET /api/me", s.handleMe)
	api.HandleFunc("GET /api/overview", s.handleOverview)
	api.HandleFunc("GET /api/nodes", s.handleNodes)
	api.HandleFunc("GET /api/nodes/{fleet}/{id}", s.handleNodeDetail)
	api.HandleFunc("GET /api/series", s.handleSeries)
	api.HandleFunc("GET /api/users", s.handleUsers)
	api.HandleFunc("GET /api/users/{email}", s.handleUserDetail)
	api.HandleFunc("GET /api/online", s.handleOnline)
	mux.Handle("/api/", s.auth(api))

	// Same auth as /api: metrics expose per-node and per-user traffic volumes.
	if s.cfg.Metrics.Enabled {
		mux.Handle("GET /metrics", s.auth(promexp.Handler(s.store, s.log, s.nodeStatus, func(r *http.Request) store.Scope {
			return principalOf(r.Context()).Scope
		})))
	}

	mux.Handle("/", s.rootHandler())

	return s.logRequests(mux)
}

// rootHandler serves the dashboard, or a JSON 404 when there isn't one.
func (s *Server) rootHandler() http.Handler {
	switch {
	case !s.cfg.DashboardRequested():
		return notFound()
	case !DashboardEmbedded:
		if s.cfg.DashboardExplicitlyEnabled() {
			s.log.Warn("dashboard.enabled is set but this binary was built without the dashboard (-tags nodashboard)")
		}

		return notFound()
	case !dashboardBuilt():
		s.log.Warn("dashboard.enabled is set but no dashboard build is embedded; run `task dashboard`")

		return notFound()
	default:
		return spaHandler()
	}
}

func notFound() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeErr(w, http.StatusNotFound, "not_found", "no such path")
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		next.ServeHTTP(w, r)

		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start).Round(time.Millisecond))
		}
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": msg}})
}
