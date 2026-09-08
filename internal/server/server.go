// Package server exposes the HTTP API: job search/detail, aggregate
// stats, and an internal scrape trigger.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ZoOwen/loker-id/internal/pipeline"
	"github.com/ZoOwen/loker-id/internal/store"
)

// healthzDBTimeout bounds how long /healthz waits on the DB ping. Neon's
// free tier scales to zero when idle, so the first query after a while
// can take ~1s to wake it back up — well within this — while a genuinely
// unreachable DB still fails the check in bounded time instead of hanging
// on the incoming request's own (often unbounded) context.
const healthzDBTimeout = 5 * time.Second

type Server struct {
	router        *chi.Mux
	db            *pgxpool.Pool
	store         *store.Store
	pipeline      *pipeline.Pipeline
	internalToken string
	logger        *slog.Logger
}

// New builds a Server. internalToken gates POST /internal/scrape (via the
// X-Internal-Token header) — an empty value makes that endpoint
// permanently unreachable rather than open.
func New(db *pgxpool.Pool, st *store.Store, pl *pipeline.Pipeline, internalToken string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		router:        chi.NewRouter(),
		db:            db,
		store:         st,
		pipeline:      pl,
		internalToken: internalToken,
		logger:        logger,
	}

	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)

	s.routes()

	return s
}

func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) routes() {
	s.router.Get("/healthz", s.handleHealthz)

	s.router.Route("/api", func(r chi.Router) {
		r.Get("/jobs", s.handleListJobs)
		r.Get("/jobs/{id}", s.handleGetJob)
		r.Get("/stats", s.handleStats)
	})

	s.router.Route("/internal", func(r chi.Router) {
		r.Post("/scrape", s.handleTriggerScrape)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	body := map[string]string{"status": "ok"}

	ctx, cancel := context.WithTimeout(r.Context(), healthzDBTimeout)
	defer cancel()

	if err := s.db.Ping(ctx); err != nil {
		status = http.StatusServiceUnavailable
		body = map[string]string{"status": "db unreachable"}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
