// Command server runs the loker-id API.
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ZoOwen/loker-id/internal/config"
	"github.com/ZoOwen/loker-id/internal/database"
	"github.com/ZoOwen/loker-id/internal/pipeline"
	"github.com/ZoOwen/loker-id/internal/scraper"
	"github.com/ZoOwen/loker-id/internal/server"
	"github.com/ZoOwen/loker-id/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	st := store.New(pool)

	scrapers := map[string]scraper.Scraper{
		"kalibrr": scraper.NewKalibrrScraper(),
	}
	pl := pipeline.New(st, scrapers, pipeline.Config{})

	srv := server.New(pool, st, pl, cfg.InternalToken, slog.Default())

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server starting", "port", cfg.Port, "env", cfg.AppEnv)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}
