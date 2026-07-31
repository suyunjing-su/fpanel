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

	"github.com/suyunjing-su/fpanel/backend/internal/auth"
	"github.com/suyunjing-su/fpanel/backend/internal/config"
	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/forwards"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/nodehub"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
	"github.com/suyunjing-su/fpanel/backend/internal/observability"
	"github.com/suyunjing-su/fpanel/backend/internal/siteconfig"
	"github.com/suyunjing-su/fpanel/backend/internal/traffic"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnels"
	"github.com/suyunjing-su/fpanel/backend/internal/users"
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
	tunnelRepo := tunnels.NewRepository(db, nodeRepo)
	forwardRepo := forwards.NewRepository(db, nodeRepo, tunnelRepo)
	trafficRepo := traffic.NewRepository(db, nodeRepo)
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

	mux.HandleFunc("POST /flow/upload", func(w http.ResponseWriter, r *http.Request) {
		uploadTraffic(w, r, nodeRepo, trafficRepo)
	})

	isAdmin := func(r *http.Request) bool {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		return ok && identity.RoleID == 0
	}
	forbidden := func(w http.ResponseWriter) {
		httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
	}
	badRequest := func(w http.ResponseWriter, err error) {
		httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
	}

	configHandler := func(w http.ResponseWriter, r *http.Request) {
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
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]string{"value": value}))
	}
	mux.HandleFunc("GET /api/v1/config/get", configHandler)
	mux.HandleFunc("POST /api/v1/config/get", configHandler)
	mux.HandleFunc("POST /api/v1/config/list", func(w http.ResponseWriter, r *http.Request) {
		values, err := configRepo.List(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, 500, httpapi.Failure(500, "配置查询失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(values))
	})
	mux.HandleFunc("POST /api/v1/config/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var values map[string]string
		if !httpapi.DecodeJSON(w, r, &values) {
			return
		}
		if err := configRepo.Update(r.Context(), values); err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/config/update-single", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
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
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
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
		if request.Username == "" {
			request.Username = request.User
		}
		identity, err := authRepo.Authenticate(r.Context(), auth.NormalizeUsername(request.Username), request.Password)
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
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		value, err := userRepo.List(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, 500, httpapi.Failure(500, "用户查询失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/user/create", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request users.CreateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		id, err := userRepo.Create(r.Context(), request)
		if err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/user/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request users.UpdateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := userRepo.Update(r.Context(), request); err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/user/delete", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request struct {
			ID int64 `json:"id"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := userRepo.Delete(r.Context(), request.ID); err != nil {
			status := 500
			if errors.Is(err, sql.ErrNoRows) {
				status = 404
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, "用户删除失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})

	mux.HandleFunc("POST /api/v1/tunnel/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		value, err := tunnelRepo.List(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, 500, httpapi.Failure(500, "隧道查询失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/tunnel/create", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request tunnels.CreateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		id, err := tunnelRepo.Create(r.Context(), request)
		if err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/tunnel/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request tunnels.UpdateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := tunnelRepo.Update(r.Context(), request); err != nil {
			status := 400
			if errors.Is(err, sql.ErrNoRows) {
				status = 404
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, err.Error()))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/tunnel/delete", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request struct {
			ID int64 `json:"id"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := tunnelRepo.Delete(r.Context(), request.ID); err != nil {
			status := 500
			if errors.Is(err, sql.ErrNoRows) {
				status = 404
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, "隧道删除失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/tunnel/user/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request struct {
			UserID int64 `json:"userId"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		value, err := tunnelRepo.UserTunnelList(r.Context(), request.UserID)
		if err != nil {
			httpapi.WriteJSON(w, 500, httpapi.Failure(500, "用户隧道权限查询失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/tunnel/user/assign", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request tunnels.AssignRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		id, err := tunnelRepo.Assign(r.Context(), request)
		if err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/tunnel/user/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request tunnels.UpdateUserTunnelRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := tunnelRepo.UpdateUserTunnel(r.Context(), request); err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/tunnel/user/remove", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request struct {
			ID int64 `json:"id"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := tunnelRepo.Remove(r.Context(), request.ID); err != nil {
			status := 500
			if errors.Is(err, sql.ErrNoRows) {
				status = 404
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, "用户隧道权限删除失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/tunnel/user/tunnel", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			httpapi.WriteJSON(w, 401, httpapi.Failure(401, "未登录或token已过期"))
			return
		}
		value, err := tunnelRepo.UserChoices(r.Context(), identity.UserID)
		if err != nil {
			httpapi.WriteJSON(w, 500, httpapi.Failure(500, "可用隧道查询失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(value))
	})

	mux.HandleFunc("POST /api/v1/forward/list", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			httpapi.WriteJSON(w, 401, httpapi.Failure(401, "未登录或token已过期"))
			return
		}
		value, err := forwardRepo.List(r.Context(), identity.UserID, identity.RoleID == 0)
		if err != nil {
			httpapi.WriteJSON(w, 500, httpapi.Failure(500, "转发查询失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/forward/create", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			httpapi.WriteJSON(w, 401, httpapi.Failure(401, "未登录或token已过期"))
			return
		}
		var request forwards.CreateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		id, err := forwardRepo.Create(r.Context(), request, identity.UserID, identity.RoleID == 0)
		if err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/forward/update", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			httpapi.WriteJSON(w, 401, httpapi.Failure(401, "未登录或token已过期"))
			return
		}
		var request forwards.UpdateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := forwardRepo.Update(r.Context(), request, identity.UserID, identity.RoleID == 0); err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/forward/delete", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			httpapi.WriteJSON(w, 401, httpapi.Failure(401, "未登录或token已过期"))
			return
		}
		var request struct {
			ID int64 `json:"id"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := forwardRepo.Delete(r.Context(), request.ID, identity.UserID, identity.RoleID == 0); err != nil {
			status := 500
			if errors.Is(err, sql.ErrNoRows) {
				status = 404
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, "转发删除失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/forward/force-delete", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			httpapi.WriteJSON(w, 401, httpapi.Failure(401, "未登录或token已过期"))
			return
		}
		var request struct {
			ID int64 `json:"id"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := forwardRepo.Delete(r.Context(), request.ID, identity.UserID, identity.RoleID == 0); err != nil {
			status := 500
			if errors.Is(err, sql.ErrNoRows) {
				status = 404
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, "转发删除失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/forward/pause", func(w http.ResponseWriter, r *http.Request) { setForwardStatus(w, r, forwardRepo, 0) })
	mux.HandleFunc("POST /api/v1/forward/resume", func(w http.ResponseWriter, r *http.Request) { setForwardStatus(w, r, forwardRepo, 1) })
	mux.HandleFunc("POST /api/v1/forward/update-order", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			httpapi.WriteJSON(w, 401, httpapi.Failure(401, "未登录或token已过期"))
			return
		}
		var request struct {
			Forwards []struct {
				ID    int64 `json:"id"`
				Index int   `json:"inx"`
			} `json:"forwards"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := forwardRepo.Reorder(r.Context(), identity.UserID, identity.RoleID == 0, request.Forwards); err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/node/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		value, err := nodeRepo.List(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, 500, httpapi.Failure(500, "节点查询失败"))
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/node/create", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request nodes.CreateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		id, err := nodeRepo.Create(r.Context(), request)
		if err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, 200, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/node/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request nodes.UpdateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := nodeRepo.Update(r.Context(), request); err != nil {
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
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request struct {
			ID int64 `json:"id"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := nodeRepo.Delete(r.Context(), request.ID); err != nil {
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
