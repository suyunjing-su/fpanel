package httpapi

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/audit"
	"github.com/suyunjing-su/fpanel/backend/internal/auth"
	"github.com/suyunjing-su/fpanel/backend/internal/observability"
)

type contextKey string

const identityKey contextKey = "identity"

func IdentityFromContext(ctx context.Context) (auth.Identity, bool) {
	identity, ok := ctx.Value(identityKey).(auth.Identity)
	return identity, ok
}

type APIResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	TS   int64  `json:"ts"`
	Data any    `json:"data"`
}

func WriteJSON(w http.ResponseWriter, status int, response APIResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func Success(data any) APIResponse {
	return APIResponse{Code: 0, Msg: "操作成功", TS: time.Now().UnixMilli(), Data: data}
}

func Failure(code int, message string) APIResponse {
	return APIResponse{Code: code, Msg: message, TS: time.Now().UnixMilli(), Data: nil}
}

func Middleware(log *slog.Logger, metrics *observability.Metrics, manager *auth.Manager, allowedOrigins []string, handler http.Handler, identityStore IdentityStore, auditRecorders ...audit.Recorder) http.Handler {
	var auditRecorder audit.Recorder
	if len(auditRecorders) > 0 {
		auditRecorder = auditRecorders[0]
	}
	handler = auditRequests(log, auditRecorder, handler)
	handler = authenticatePublicRoutes(manager, handler, identityStore)
	return requestID(log, metrics, cors(allowedOrigins, securityHeaders(recoverPanic(log, handler))))
}

type IdentityStore interface {
	FindIdentity(context.Context, int64) (auth.Identity, error)
}

func authenticatePublicRoutes(manager *auth.Manager, next http.Handler, identityStores ...IdentityStore) http.Handler {
	var identityStore IdentityStore
	if len(identityStores) > 0 {
		identityStore = identityStores[0]
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" || r.URL.Path == "/metrics" || r.URL.Path == "/system-info" ||
			r.URL.Path == "/flow/test" || r.URL.Path == "/flow/upload" || r.URL.Path == "/flow/config" || r.URL.Path == "/flow/config/all" || r.URL.Path == "/api/v1/user/login" || r.URL.Path == "/api/v1/config/get" ||
			strings.HasPrefix(r.URL.Path, "/api/v1/captcha/") || strings.HasPrefix(r.URL.Path, "/api/v1/open_api/") {
			next.ServeHTTP(w, r)
			return
		}
		authenticateWithStore(manager, next, identityStore).ServeHTTP(w, r)
	})
}

func auditRequests(log *slog.Logger, recorder audit.Recorder, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if recorder == nil || !isAuditedMutation(r) {
			next.ServeHTTP(w, r)
			return
		}
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		var recovered any
		defer func() {
			if value := recover(); value != nil {
				recovered = value
				rw.status = http.StatusInternalServerError
			}
			var actorID *int64
			if identity, ok := IdentityFromContext(r.Context()); ok {
				value := identity.UserID
				actorID = &value
			}
			remoteAddr := r.RemoteAddr
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				remoteAddr = host
			}
			outcome := "success"
			if rw.status >= http.StatusBadRequest {
				outcome = "failure"
			}
			event := audit.Event{
				ActorID:      actorID,
				Action:       strings.ToLower(r.Method),
				ResourceType: strings.TrimPrefix(r.URL.Path, "/api/v1/"),
				Outcome:      outcome,
				RequestID:    requestIDFromContext(r.Context()),
				RemoteAddr:   remoteAddr,
				Detail:       fmt.Sprintf("status=%d", rw.status),
				CreatedAt:    time.Now().UnixMilli(),
			}
			if err := recorder.Record(context.WithoutCancel(r.Context()), event); err != nil {
				log.Warn("failed to record audit event", "error", err, "path", r.URL.Path)
			}
			if recovered != nil {
				panic(recovered)
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

func isAuditedMutation(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodOptions || !strings.HasPrefix(r.URL.Path, "/api/v1/") || strings.HasPrefix(r.URL.Path, "/api/v1/captcha/") || strings.HasPrefix(r.URL.Path, "/api/v1/open_api/") {
		return false
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	action := path[strings.LastIndex(path, "/")+1:]
	switch action {
	case "create", "update", "delete", "update-single", "reset", "restore", "backup", "support-bundle", "updatePassword", "batch-delete", "rotate-secret", "assign", "remove", "force-delete", "pause", "resume", "update-order":
		return true
	default:
		return false
	}
}

func requestID(log *slog.Logger, metrics *observability.Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = randomRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		metrics.TrackActive(1)
		defer metrics.TrackActive(-1)
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(context.WithValue(r.Context(), contextKey("request_id"), requestID)))
		metrics.Observe(r.Method, rw.status, time.Since(started))
		log.Info("http request", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "status", rw.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func Authenticate(manager *auth.Manager, next http.Handler) http.Handler {
	return authenticateWithStore(manager, next, nil)
}

func authenticateWithStore(manager *auth.Manager, next http.Handler, identityStore IdentityStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(raw), "bearer ") {
			raw = strings.TrimSpace(raw[7:])
		}
		identity, err := manager.Parse(raw)
		if err != nil {
			WriteJSON(w, http.StatusUnauthorized, Failure(http.StatusUnauthorized, "未登录或token已过期"))
			return
		}
		if identityStore != nil {
			current, lookupErr := identityStore.FindIdentity(r.Context(), identity.UserID)
			if lookupErr != nil || current.TokenVersion != identity.TokenVersion || current.Role != identity.Role || current.Username != identity.Username {
				WriteJSON(w, http.StatusUnauthorized, Failure(http.StatusUnauthorized, "未登录或token已过期"))
				return
			}
			identity = current
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey, identity)))
	})
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := IdentityFromContext(r.Context())
		if !ok {
			WriteJSON(w, http.StatusUnauthorized, Failure(http.StatusUnauthorized, "未登录或token已过期"))
			return
		}
		if identity.RoleID != 0 && identity.Role != "admin" {
			WriteJSON(w, http.StatusForbidden, Failure(http.StatusForbidden, "无权限"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(contextKey("request_id")).(string)
	return value
}

func randomRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func recoverPanic(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Error("panic recovered", "error", recovered, "stack", string(debug.Stack()))
				WriteJSON(w, http.StatusInternalServerError, Failure(http.StatusInternalServerError, "服务器内部错误"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func cors(allowedOrigins []string, next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
