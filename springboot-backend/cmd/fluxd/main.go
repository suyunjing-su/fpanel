package main

import (
	"context"
	"database/sql"
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
	"github.com/bqlpfy/flux-panel/backend/internal/nodehub"
	"github.com/bqlpfy/flux-panel/backend/internal/nodes"
	"github.com/bqlpfy/flux-panel/backend/internal/observability"
	"github.com/bqlpfy/flux-panel/backend/internal/siteconfig"
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
	authRepo := auth.NewRepository(db)
	nodeRepo := nodes.NewRepository(db)
	hub := nodehub.New(log, nodeRepo)
	configRepo := siteconfig.NewRepository(db)
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
	mux.Handle("GET /system-info", hub)
	mux.HandleFunc("GET /flow/test", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("test"))
	})
	mux.HandleFunc("GET /api/v1/config/get", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("name")
		if key == "" {
			var request struct {
				Name string `json:"name"`
			}
			if !httpapi.DecodeJSON(w, r, &request) {
				return
			}
			key = request.Name
		}
		value, err := configRepo.Get(r.Context(), key)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]string{"value": value}))
	})
	mux.HandleFunc("POST /api/v1/config/list", func(w http.ResponseWriter, r *http.Request) {
		values, err := configRepo.List(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "配置查询失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(values))
	})
	admin := func(r *http.Request) bool {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		return ok && identity.RoleID == 0
	}
	mux.HandleFunc("POST /api/v1/config/update", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		var values map[string]string
		if !httpapi.DecodeJSON(w, r, &values) {
			return
		}
		if err := configRepo.Update(r.Context(), values); err != nil {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/config/update-single", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		var request struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := configRepo.Update(r.Context(), map[string]string{request.Name: request.Value}); err != nil {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})

	mux.HandleFunc("POST /api/v1/user/login", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Username string `json:"username"`
			User     string `json:"user"`
			Password string `json:"password"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		username := request.Username
		if username == "" {
			username = request.User
		}
		identity, err := authRepo.Authenticate(r.Context(), auth.NormalizeUsername(username), request.Password)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusUnauthorized, httpapi.Failure(http.StatusUnauthorized, "用户名或密码错误"))
			return
		}
		token, err := jwtManager.Issue(identity)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "登录失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]any{
			"token": token, "role_id": identity.RoleID, "name": identity.Username,
		}))
	})
	mux.HandleFunc("POST /api/v1/node/list", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		result, err := nodeRepo.List(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "节点查询失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(result))
	})
	mux.HandleFunc("POST /api/v1/node/create", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		var request nodes.CreateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		id, err := nodeRepo.Create(r.Context(), request)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/node/update", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		var request nodes.UpdateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := nodeRepo.Update(r.Context(), request); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, err.Error()))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/node/delete", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		var request struct {
			ID int64 `json:"id"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := nodeRepo.Delete(r.Context(), request.ID); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, "节点删除失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})

	server := &http.Server{Addr: cfg.Address, Handler: httpapi.Middleware(log, metrics, jwtManager, cfg.AllowedOrigins, mux), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 120 * time.Second}
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
