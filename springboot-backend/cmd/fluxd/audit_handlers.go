package main

import (
	"net/http"

	"github.com/suyunjing-su/fpanel/backend/internal/audit"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
)

func registerAuditRoutes(mux *http.ServeMux, repository *audit.Repository, isAdmin func(*http.Request) bool) {
	mux.HandleFunc("POST /api/v1/audit/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		var request struct {
			Page     int `json:"page"`
			PageSize int `json:"pageSize"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if request.Page < 1 {
			request.Page = 1
		}
		if request.PageSize == 0 {
			request.PageSize = 50
		}
		if request.PageSize < 1 || request.PageSize > 200 || (request.Page > 1 && request.Page-1 > int(^uint(0)>>1)/request.PageSize) {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, "invalid audit pagination"))
			return
		}
		result, err := repository.List(r.Context(), (request.Page-1)*request.PageSize, request.PageSize)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(result))
	})
}
