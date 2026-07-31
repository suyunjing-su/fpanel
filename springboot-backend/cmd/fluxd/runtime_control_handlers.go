package main

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/runtimecontrols"
)

func registerRuntimeControlRoutes(mux *http.ServeMux, repository *runtimecontrols.Repository, refreshes refreshNotifier, isAdmin func(*http.Request) bool) {
	forbidden := func(w http.ResponseWriter) {
		httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
	}
	writeError := func(w http.ResponseWriter, err error) {
		if errors.Is(err, sql.ErrNoRows) {
			httpapi.WriteJSON(w, http.StatusNotFound, httpapi.Failure(http.StatusNotFound, "资源不存在"))
			return
		}
		httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
	}
	wake := func() {
		if refreshes != nil {
			refreshes.Wake()
		}
	}
	decodeID := func(w http.ResponseWriter, r *http.Request) (int64, bool) {
		var request struct {
			ID int64 `json:"id"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return 0, false
		}
		return request.ID, true
	}

	mux.HandleFunc("POST /api/v1/endpoint-group/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		value, err := repository.ListEndpointGroups(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "端点组查询失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/endpoint-group/create", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request runtimecontrols.EndpointGroupRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		id, err := repository.CreateEndpointGroup(r.Context(), request)
		if err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/endpoint-group/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request runtimecontrols.UpdateEndpointGroupRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := repository.UpdateEndpointGroup(r.Context(), request); err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/endpoint-group/delete", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		id, ok := decodeID(w, r)
		if !ok {
			return
		}
		if err := repository.DeleteEndpointGroup(r.Context(), id); err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})

	mux.HandleFunc("POST /api/v1/route-rule-set/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		value, err := repository.ListRouteRuleSets(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "路由规则集查询失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/route-rule-set/create", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request runtimecontrols.RouteRuleSetRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		id, err := repository.CreateRouteRuleSet(r.Context(), request)
		if err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/route-rule-set/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request runtimecontrols.UpdateRouteRuleSetRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := repository.UpdateRouteRuleSet(r.Context(), request); err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/route-rule-set/delete", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		id, ok := decodeID(w, r)
		if !ok {
			return
		}
		if err := repository.DeleteRouteRuleSet(r.Context(), id); err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})

	mux.HandleFunc("POST /api/v1/node-group/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		value, err := repository.ListNodeGroups(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "节点组查询失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/node-group/create", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request runtimecontrols.NodeGroupRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		id, err := repository.CreateNodeGroup(r.Context(), request)
		if err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/node-group/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request runtimecontrols.UpdateNodeGroupRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := repository.UpdateNodeGroup(r.Context(), request); err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/node-group/delete", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		id, ok := decodeID(w, r)
		if !ok {
			return
		}
		if err := repository.DeleteNodeGroup(r.Context(), id); err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})

	mux.HandleFunc("POST /api/v1/tunnel-node-group-binding/list", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		value, err := repository.ListTunnelNodeGroupBindings(r.Context())
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "隧道节点组绑定查询失败"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(value))
	})
	mux.HandleFunc("POST /api/v1/tunnel-node-group-binding/create", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request runtimecontrols.TunnelNodeGroupBindingRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		id, err := repository.CreateTunnelNodeGroupBinding(r.Context(), request)
		if err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]any{"id": id}))
	})
	mux.HandleFunc("POST /api/v1/tunnel-node-group-binding/update", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		var request runtimecontrols.UpdateTunnelNodeGroupBindingRequest
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		if err := repository.UpdateTunnelNodeGroupBinding(r.Context(), request); err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
	mux.HandleFunc("POST /api/v1/tunnel-node-group-binding/delete", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			forbidden(w)
			return
		}
		id, ok := decodeID(w, r)
		if !ok {
			return
		}
		if err := repository.DeleteTunnelNodeGroupBinding(r.Context(), id); err != nil {
			writeError(w, err)
			return
		}
		wake()
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
	})
}
