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
	"github.com/bqlpfy/flux-panel/backend/internal/users"
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
	db, err := database.Open(context.Background(), cfg.DatabasePath, log)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	if err := auth.EnsureBootstrapAdmin(context.Background(), db, cfg.BootstrapUsername, cfg.BootstrapPassword); err != nil {
		return fmt.Errorf("ensure bootstrap admin: %w", err)
	}
	jwtManager := auth.New(cfg.JWTSecret, cfg.TokenTTL)
	authRepo := auth.NewRepository(db)
	nodeRepo := nodes.NewRepository(db)
	hub := nodehub.New(log, nodeRepo)
	configRepo := siteconfig.NewRepository(db)
	userRepo := users.NewRepository(db)
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
	admin := func(r *http.Request) bool {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		return ok && identity.RoleID == 0
	}
	mux.HandleFunc("GET /api/v1/config/get", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("name")
		if key == "" {
			var q struct {
				Name string `json:"name"`
			}
			if !httpapi.DecodeJSON(w, r, &q) {
				return
			}
			key = q.Name
		}
		v, err := configRepo.Get(r.Context(), key)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]string{"value": v}))
	})
	mux.HandleFunc("POST /api/v1/config/list", func(w http.ResponseWriter, r *http.Request) {
		v, err := configRepo.List(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, 500, httpapi.Failure(500, "配置查询失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(v))
	})
	mux.HandleFunc("POST /api/v1/config/update", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, 403, httpapi.Failure(403, "无权限"))
			return
		}
		var v map[string]string
		if !httpapi.DecodeJSON(w, r, &v) {
			return
		}
		if err := configRepo.Update(r.Context(), v); err != nil {
			httpapi.WriteJSON(w, 400, httpapi.Failure(400, err.Error()))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/config/update-single", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, 403, httpapi.Failure(403, "无权限"))
			return
		}
		var q struct {
			Name, Value string `json:"name"`
		}
		if !httpapi.DecodeJSON(w, r, &q) {
			return
		}
		if err := configRepo.Update(r.Context(), map[string]string{q.Name: q.Value}); err != nil {
			httpapi.WriteJSON(w, 400, httpapi.Failure(400, err.Error()))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/user/login", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Username string `json:"username"`
			User     string `json:"user"`
			Password string `json:"password"`
		}
		if !httpapi.DecodeJSON(w, r, &q) {
			return
		}
		if q.Username == "" {
			q.Username = q.User
		}
		identity, err := authRepo.Authenticate(r.Context(), auth.NormalizeUsername(q.Username), q.Password)
		if err != nil {
			httpapi.WriteJSON(w, 401, httpapi.Failure(401, "用户名或密码错误"))
			return
		}
		token, err := jwtManager.Issue(identity)
		if err != nil {
			httpapi.WriteJSON(w, 500, httpapi.Failure(500, "登录失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(map[string]any{"token": token, "role_id": identity.RoleID, "name": identity.Username}))
	})
	mux.HandleFunc("POST /api/v1/user/list", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, 403, httpapi.Failure(403, "无权限"))
			return
		}
		v, err := userRepo.List(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, 500, httpapi.Failure(500, "用户查询失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(v))
	})
	mux.HandleFunc("POST /api/v1/user/create", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, 403, httpapi.Failure(403, "无权限"))
			return
		}
		var q users.CreateRequest
		if !httpapi.DecodeJSON(w, r, &q) {
			return
		}
		id, err := userRepo.Create(r.Context(), q)
		if err != nil {
			httpapi.WriteJSON(w, 400, httpapi.Failure(400, err.Error()))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/user/update", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, 403, httpapi.Failure(403, "无权限"))
			return
		}
		var q users.UpdateRequest
		if !httpapi.DecodeJSON(w, r, &q) {
			return
		}
		if err := userRepo.Update(r.Context(), q); err != nil {
			httpapi.WriteJSON(w, 400, httpapi.Failure(400, err.Error()))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/user/delete", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, 403, httpapi.Failure(403, "无权限"))
			return
		}
		var q struct {
			ID int64 `json:"id"`
		}
		if !httpapi.DecodeJSON(w, r, &q) {
			return
		}
		if err := userRepo.Delete(r.Context(), q.ID); err != nil {
			status := 500
			if errors.Is(err, sql.ErrNoRows) {
				status = 404
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, "用户删除失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/node/list", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, 403, httpapi.Failure(403, "无权限"))
			return
		}
		v, err := nodeRepo.List(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, 500, httpapi.Failure(500, "节点查询失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(v))
	})
	mux.HandleFunc("POST /api/v1/node/create", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, 403, httpapi.Failure(403, "无权限"))
			return
		}
		var q nodes.CreateRequest
		if !httpapi.DecodeJSON(w, r, &q) {
			return
		}
		id, err := nodeRepo.Create(r.Context(), q)
		if err != nil {
			httpapi.WriteJSON(w, 400, httpapi.Failure(400, err.Error()))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/node/update", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, 403, httpapi.Failure(403, "无权限"))
			return
		}
		var q nodes.UpdateRequest
		if !httpapi.DecodeJSON(w, r, &q) {
			return
		}
		if err := nodeRepo.Update(r.Context(), q); err != nil {
			status := 400
			if errors.Is(err, sql.ErrNoRows) {
				status = 404
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, err.Error()))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/node/delete", func(w http.ResponseWriter, r *http.Request) {
		if !admin(r) {
			httpapi.WriteJSON(w, 403, httpapi.Failure(403, "无权限"))
			return
		}
		var q struct {
			ID int64 `json:"id"`
		}
		if !httpapi.DecodeJSON(w, r, &q) {
			return
		}
		if err := nodeRepo.Delete(r.Context(), q.ID); err != nil {
			status := 500
			if errors.Is(err, sql.ErrNoRows) {
				status = 404
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, "节点删除失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
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
	case sig := <-stop:
		log.Info("shutdown signal received", "signal", sig.String())
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	log.Info("flux control plane stopped")
	return nil
}
