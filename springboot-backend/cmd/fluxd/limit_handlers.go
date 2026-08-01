package main

import (
	"net/http"

	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/speedlimits"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnelpolicies"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnels"
)

type refreshNotifier interface {
	Wake()
}

func registerLimitRoutes(mux *http.ServeMux, limits *speedlimits.Repository, policies *tunnelpolicies.Repository, tunnelRepo *tunnels.Repository, refreshes refreshNotifier, isAdmin func(*http.Request) bool) {
	forbidden := func(w http.ResponseWriter) {
		httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
	}
	badRequest := func(w http.ResponseWriter, err error) {
		httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
	}
	wake := func() {
		if refreshes != nil {
			refreshes.Wake()
		}
	}

	mux.HandleFunc("POST /api/v1/speed-limit/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		value, err := limits.List(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "限速规则查询失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/speed-limit/create", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request speedlimits.CreateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		id, err := limits.Create(r.Context(), request)
		if err != nil {
			badRequest(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/speed-limit/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request speedlimits.UpdateRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := limits.Update(r.Context(), request); err != nil {
			badRequest(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/speed-limit/delete", func(w http.ResponseWriter, r *http.Request) {
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
		if err := limits.Delete(r.Context(), request.ID); err != nil {
			badRequest(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
	registerSpeedLimitBatchDelete(mux, limits, refreshes, isAdmin)
	mux.HandleFunc("POST /api/v1/speed-limit/user-tunnel/list", func(w http.ResponseWriter, r *http.Request) {
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
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/speed-limit/user-tunnel/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request tunnels.UpdateUserTunnelRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if _, err := tunnelRepo.UpdateUserTunnel(r.Context(), request); err != nil {
			badRequest(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/speed-limit/user-tunnel/entry-policy/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request struct {
			UserTunnelID int64 `json:"userTunnelId"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		value, err := policies.ListEntry(r.Context(), request.UserTunnelID)
		if err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/speed-limit/user-tunnel/entry-policy/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request tunnelpolicies.UpdateEntryRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if _, err := policies.UpdateEntry(r.Context(), request); err != nil {
			badRequest(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/speed-limit/user-tunnel/exit-policy/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request struct {
			UserTunnelID int64 `json:"userTunnelId"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		value, err := policies.ListExit(r.Context(), request.UserTunnelID)
		if err != nil {
			badRequest(w, err)
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/speed-limit/user-tunnel/exit-policy/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request tunnelpolicies.UpdateExitRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if _, err := policies.UpdateExit(r.Context(), request); err != nil {
			badRequest(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
}
