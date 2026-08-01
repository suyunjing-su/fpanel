package main

import (
	"context"
	"net/http"

	"github.com/suyunjing-su/fpanel/backend/internal/batchdelete"
	"github.com/suyunjing-su/fpanel/backend/internal/forwards"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
	"github.com/suyunjing-su/fpanel/backend/internal/speedlimits"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnels"
	"github.com/suyunjing-su/fpanel/backend/internal/users"
)

func registerBatchDeleteRoutes(mux *http.ServeMux, userRepo *users.Repository, nodeRepo *nodes.Repository, tunnelRepo *tunnels.Repository, forwardRepo *forwards.Repository, refreshes refreshNotifier, isAdmin func(*http.Request) bool) {
	registerAdminBatchDelete(mux, "/api/v1/user/batch-delete", userRepo.DeleteWithName, refreshes, isAdmin)
	registerAdminBatchDelete(mux, "/api/v1/node/batch-delete", nodeRepo.DeleteWithName, refreshes, isAdmin)
	registerAdminBatchDelete(mux, "/api/v1/tunnel/batch-delete", tunnelRepo.DeleteWithName, refreshes, isAdmin)

	mux.HandleFunc("POST /api/v1/forward/batch-delete", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			httpapi.WriteJSON(w, http.StatusUnauthorized, httpapi.Failure(http.StatusUnauthorized, "未登录或token已过期"))
			return
		}
		ids, ok := decodeBatchIDs(w, r)
		if !ok {
			return
		}
		result := batchdelete.Execute(ids, func(id int64) (string, error) {
			return forwardRepo.DeleteWithName(r.Context(), id, identity.UserID, identity.RoleID == 0)
		})
		wakeAfterBatch(refreshes, result)
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(result))
	})
}

func registerAdminBatchDelete(mux *http.ServeMux, path string, remove func(context.Context, int64) (string, error), refreshes refreshNotifier, isAdmin func(*http.Request) bool) {
	registerBatchDeleteHandler(mux, path, true, isAdmin, refreshes, func(r *http.Request, id int64) (string, error) {
		return remove(r.Context(), id)
	})
}

func registerSpeedLimitBatchDelete(mux *http.ServeMux, limits *speedlimits.Repository, refreshes refreshNotifier, isAdmin func(*http.Request) bool) {
	registerBatchDeleteHandler(mux, "/api/v1/speed-limit/batch-delete", true, isAdmin, refreshes, func(r *http.Request, id int64) (string, error) {
		return limits.DeleteWithName(r.Context(), id)
	})
}

func registerBatchDeleteHandler(mux *http.ServeMux, path string, adminOnly bool, isAdmin func(*http.Request) bool, refreshes refreshNotifier, remove func(*http.Request, int64) (string, error)) {
	mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
		if adminOnly && !isAdmin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		ids, ok := decodeBatchIDs(w, r)
		if !ok {
			return
		}
		result := batchdelete.Execute(ids, func(id int64) (string, error) { return remove(r, id) })
		wakeAfterBatch(refreshes, result)
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(result))
	})
}

func decodeBatchIDs(w http.ResponseWriter, r *http.Request) ([]int64, bool) {
	var request struct {
		IDs []int64 `json:"ids"`
	}
	if !httpapi.DecodeJSON(w, r, &request) {
		return nil, false
	}
	if len(request.IDs) == 0 {
		httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, "请选择要删除的资源"))
		return nil, false
	}
	return request.IDs, true
}

func wakeAfterBatch(refreshes refreshNotifier, result batchdelete.Result) {
	if refreshes != nil && result.SuccessCount > 0 {
		refreshes.Wake()
	}
}
