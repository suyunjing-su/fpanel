package main

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/suyunjing-su/fpanel/backend/internal/auth"
	"github.com/suyunjing-su/fpanel/backend/internal/forwards"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnels"
	"github.com/suyunjing-su/fpanel/backend/internal/users"
)

func registerAccountRoutes(mux *http.ServeMux, userRepo *users.Repository, tunnelRepo *tunnels.Repository, forwardRepo *forwards.Repository, authRepo *auth.Repository, refreshes refreshNotifier, isAdmin func(*http.Request) bool) {
	mux.HandleFunc("POST /api/v1/user/package", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			httpapi.WriteJSON(w, http.StatusUnauthorized, httpapi.Failure(http.StatusUnauthorized, "未登录或token已过期"))
			return
		}
		permissions, err := tunnelRepo.UserTunnelList(r.Context(), identity.UserID)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "套餐信息查询失败"))
			return
		}
		forwardList, err := forwardRepo.List(r.Context(), identity.UserID, identity.RoleID == 0)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "套餐信息查询失败"))
			return
		}
		value, err := userRepo.Package(r.Context(), identity.UserID, userTunnelsAsMaps(permissions), forwardsAsMaps(forwardList))
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, "套餐信息查询失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(value))
	})

	mux.HandleFunc("POST /api/v1/user/reset", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		var request struct {
			ID   int64 `json:"id"`
			Type int   `json:"type"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := userRepo.ResetFlow(r.Context(), request.ID, request.Type); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, err.Error()))
			return
		}
		if refreshes != nil {
			refreshes.Wake()
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})

	mux.HandleFunc("POST /api/v1/user/updatePassword", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			httpapi.WriteJSON(w, http.StatusUnauthorized, httpapi.Failure(http.StatusUnauthorized, "未登录或token已过期"))
			return
		}
		var request struct {
			NewUsername     string `json:"newUsername"`
			CurrentPassword string `json:"currentPassword"`
			NewPassword     string `json:"newPassword"`
			ConfirmPassword string `json:"confirmPassword"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if request.NewPassword != request.ConfirmPassword {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, "两次输入密码不一致"))
			return
		}
		if request.NewUsername == "" {
			request.NewUsername = identity.Username
		}
		if err := authRepo.UpdatePassword(r.Context(), identity.UserID, request.NewUsername, request.CurrentPassword, request.NewPassword); err != nil {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
}

func userTunnelsAsMaps(items []tunnels.UserTunnel) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":             item.ID,
			"userId":         item.UserID,
			"tunnelId":       item.TunnelID,
			"tunnelName":     item.TunnelName,
			"status":         item.Status,
			"flow":           item.Flow,
			"inFlow":         item.InFlow,
			"outFlow":        item.OutFlow,
			"num":            item.Num,
			"expTime":        item.ExpTime,
			"flowResetTime":  item.FlowResetTime,
			"speedId":        item.SpeedID,
			"speedLimitName": item.SpeedLimitName,
			"tunnelFlow":     item.TunnelFlow,
		})
	}
	return out
}

func forwardsAsMaps(items []forwards.Forward) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":          item.ID,
			"userId":      item.UserID,
			"userName":    item.UserName,
			"name":        item.Name,
			"tunnelId":    item.TunnelID,
			"tunnelName":  item.TunnelName,
			"inIp":        item.InIP,
			"inPort":      item.InPort,
			"remoteAddr":  item.RemoteAddr,
			"inFlow":      item.InFlow,
			"outFlow":     item.OutFlow,
			"status":      item.Status,
			"createdTime": item.CreatedTime,
		})
	}
	return out
}
