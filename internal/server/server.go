// Package server wires the HTTP router and its middleware.
//
// Domain routes (jobs, alerts, ...) are mounted from routes() as they are
// built; nothing beyond a health check lives here yet.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	router *chi.Mux
	db     *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Server {
	s := &Server{
		router: chi.NewRouter(),
		db:     db,
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
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	body := map[string]string{"status": "ok"}

	if err := s.db.Ping(r.Context()); err != nil {
		status = http.StatusServiceUnavailable
		body = map[string]string{"status": "db unreachable"}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
