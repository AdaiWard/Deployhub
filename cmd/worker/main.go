package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/deployhub-io/deployhub/internal/config"
	"github.com/deployhub-io/deployhub/internal/database"
	"github.com/deployhub-io/deployhub/internal/queue"
	"github.com/deployhub-io/deployhub/internal/secretbox"
	"github.com/deployhub-io/deployhub/internal/worker"
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

	service := worker.New(cfg, pool, jobs, box, logger)
	if err := service.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}
