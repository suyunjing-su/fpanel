package main

import (
	"net/http"

	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/maintenance"
)

func registerMaintenanceRoutes(mux *http.ServeMux, repository *maintenance.Repository, isAdmin func(*http.Request) bool) {
	mux.HandleFunc("POST /api/v1/maintenance/run/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		var request struct {
			Limit int `json:"limit"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		events, err := repository.ListEvents(r.Context(), request.Limit)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "维护运行历史查询失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(events))
	})
}
