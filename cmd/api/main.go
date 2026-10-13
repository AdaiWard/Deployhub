package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/deployhub-io/deployhub/internal/api"
	"github.com/deployhub-io/deployhub/internal/config"
	"github.com/deployhub-io/deployhub/internal/database"
	"github.com/deployhub-io/deployhub/internal/queue"
	"github.com/deployhub-io/deployhub/internal/secretbox"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		logger.Error("database migration failed", "error", err)
		os.Exit(1)
	}

	jobs := queue.New(cfg.RedisAddr, cfg.RedisPassword)
	defer jobs.Close()
	if err := jobs.Ping(ctx); err != nil {
		logger.Error("redis unavailable", "error", err)
		os.Exit(1)
	}

	box, err := secretbox.New(cfg.EncryptionKey)
	if err != nil {
		logger.Error("encryption unavailable", "error", err)
		os.Exit(1)
	}

	server, err := api.New(cfg, pool, jobs, box, logger)
	if err != nil {
		logger.Error("create api", "error", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("api listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api stopped", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("api shutdown failed", "error", err)
	}
}
