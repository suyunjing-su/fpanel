package main

import (
	"io"
	"net/http"
	"strings"

	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
	"github.com/suyunjing-su/fpanel/backend/internal/traffic"
)

func uploadTraffic(w http.ResponseWriter, r *http.Request, nodeRepo *nodes.Repository, trafficRepo *traffic.Repository) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	secret := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(secret), "bearer ") {
		secret = strings.TrimSpace(secret[7:])
	}
	var nodeID int64
	if err := nodeRepo.LookupSecret(r.Context(), secret, &nodeID); err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
	if err != nil {
		http.Error(w, "invalid traffic report", http.StatusBadRequest)
		return
	}
	items, err := traffic.DecodeReport(body, secret)
	if err != nil {
		http.Error(w, "invalid traffic report", http.StatusBadRequest)
		return
	}
	if err := trafficRepo.Record(r.Context(), nodeID, items); err != nil {
		httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "流量上报失败"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
