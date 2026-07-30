package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bqlpfy/flux-panel/backend/internal/auth"
	"github.com/bqlpfy/flux-panel/backend/internal/config"
	"github.com/bqlpfy/flux-panel/backend/internal/database"
	"github.com/bqlpfy/flux-panel/backend/internal/httpapi"
	"github.com/bqlpfy/flux-panel/backend/internal/observability"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	log := observability.NewLogger(cfg.LogLevel)
	ctx := context.Background()
	db, err := database.Open(ctx, cfg.DatabasePath, log)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	if err := auth.EnsureBootstrapAdmin(ctx, db, cfg.BootstrapUsername, cfg.BootstrapPassword); err != nil {
		return fmt.Errorf("ensure bootstrap admin: %w", err)
	}

	jwtManager := auth.New(cfg.JWTSecret, cfg.TokenTTL)
	metrics := observability.NewMetrics()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]string{"status": "ok"}))
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			httpapi.WriteJSON(w, http.StatusServiceUnavailable, httpapi.Failure(http.StatusServiceUnavailable, "数据库不可用"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]string{"status": "ready"}))
	})
	mux.Handle("GET /metrics", metrics)
	mux.HandleFunc("GET /flow/test", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("test"))
	})
	mux.HandleFunc("GET /api/v1/config/get", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]string{"value": ""}))
	})
	mux.HandleFunc("POST /api/v1/user/login", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteJSON(w, http.StatusNotImplemented, httpapi.Failure(http.StatusNotImplemented, "接口正在迁移"))
	})
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           httpapi.Middleware(log, metrics, jwtManager, cfg.AllowedOrigins, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("flux control plane started", "address", cfg.Address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-serverErr:
		return fmt.Errorf("serve HTTP: %w", err)
	case signal := <-stop:
		log.Info("shutdown signal received", "signal", signal.String())
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	log.Info("flux control plane stopped")
	return nil
}
