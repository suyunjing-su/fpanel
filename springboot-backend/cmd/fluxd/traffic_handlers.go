package main

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
	"github.com/suyunjing-su/fpanel/backend/internal/traffic"
)

func nodeSecret(r *http.Request) string {
	secret := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(secret), "bearer ") {
		secret = strings.TrimSpace(secret[7:])
	}
	return secret
}

func writePlain(w http.ResponseWriter, status int, value string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(value))
}

func uploadTraffic(w http.ResponseWriter, r *http.Request, nodeRepo *nodes.Repository, trafficRepo *traffic.Repository, refreshes refreshNotifier, log *slog.Logger) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	secret := nodeSecret(r)
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
	entryNodeIDs, err := trafficRepo.Record(r.Context(), nodeID, items)
	if err != nil {
		httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "流量上报失败"))
		return
	}
	if len(entryNodeIDs) > 0 && refreshes != nil {
		refreshes.Wake()
	}
	log.Debug("traffic report recorded", "node_id", nodeID, "items", len(items), "refresh_nodes", len(entryNodeIDs))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
