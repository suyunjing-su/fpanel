package main

import (
	"net/http"

	"github.com/suyunjing-su/fpanel/backend/internal/forwards"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
)

func setForwardStatus(w http.ResponseWriter, r *http.Request, repository *forwards.Repository, status int) {
	identity, ok := httpapi.IdentityFromContext(r.Context())
	if !ok {
		httpapi.WriteJSON(w, http.StatusUnauthorized, httpapi.Failure(http.StatusUnauthorized, "未登录或token已过期"))
		return
	}
	var request struct {
		ID int64 `json:"id"`
	}
	if !httpapi.DecodeJSON(w, r, &request) {
		return
	}
	if err := repository.SetStatus(r.Context(), request.ID, identity.UserID, status, identity.RoleID == 0); err != nil {
		httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(nil))
}
