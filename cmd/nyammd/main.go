package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"NyaMediaMetadataTool/internal/api"
	"NyaMediaMetadataTool/internal/bootstrap"
	"NyaMediaMetadataTool/internal/config"
	"NyaMediaMetadataTool/internal/runner"
	"NyaMediaMetadataTool/internal/store"
	"NyaMediaMetadataTool/internal/watcher"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := store.Open(cfg.Database.Path)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		logger.Error("migrate database", "error", err)
		os.Exit(1)
	}

	if err := bootstrap.SyncAndScan(context.Background(), cfg, db, logger); err != nil {
		logger.Error("bootstrap sync and scan", "error", err)
		os.Exit(1)
	}

	serviceCtx, serviceCancel := context.WithCancel(context.Background())
	defer serviceCancel()

	go func() {
		if err := watcher.New(cfg, db, logger).Run(serviceCtx); err != nil {
			logger.Error("watcher stopped", "error", err)
		}
	}()

	go func() {
		if err := runner.New(cfg, db, logger).Run(serviceCtx); err != nil {
			logger.Error("runner stopped", "error", err)
		}
	}()

	server := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           api.NewServer(cfg, *configPath, db, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("server started", "addr", cfg.Server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	serviceCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown server", "error", err)
		os.Exit(1)
	}
}
