package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/suyunjing-su/fpanel/backend/internal/auth"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
)

func registerOpenAPIRoutes(mux *http.ServeMux, repository *auth.Repository) {
	mux.HandleFunc("GET /api/v1/open_api/sub_store", func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			username = r.URL.Query().Get("user")
			password = r.URL.Query().Get("pwd")
		}
		tunnelID := int64(0)
		tunnel := strings.TrimSpace(r.URL.Query().Get("tunnel"))
		if tunnel != "" && tunnel != "-1" {
			var err error
			tunnelID, err = strconv.ParseInt(tunnel, 10, 64)
			if err != nil || tunnelID <= 0 {
				httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, "隧道参数错误"))
				return
			}
		}
		usage, err := repository.SubscriptionUsage(r.Context(), username, password, tunnelID)
		if err != nil {
			status := http.StatusUnauthorized
			message := "鉴权失败"
			if errors.Is(err, auth.ErrSubscriptionTunnelNotFound) {
				status = http.StatusNotFound
				message = "隧道不存在"
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, message))
			return
		}
		header := fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", usage.Upload, usage.Download, usage.Total, usage.Expire)
		w.Header().Set("Subscription-Userinfo", header)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(header))
	})
}
