package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"github.com/TiraelSedai/PhotoChallengeBot/internal/app"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/config"
	"github.com/TiraelSedai/PhotoChallengeBot/internal/logger"
)

func main() {
	log := slog.New(logger.NewCompactJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.New(cfg, log).Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("application stopped", "error", err)
		os.Exit(1)
	}
}
