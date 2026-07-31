package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/suyunjing-su/fpanel/backend/internal/nodeconfig"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
)

type configRefreshRequester interface {
	RequestNodes(context.Context, []int64) error
}

func getNodeFullConfig(w http.ResponseWriter, r *http.Request, nodeRepo *nodes.Repository, configs *nodeconfig.Repository, log *slog.Logger) {
	secret := nodeSecret(r)
	var nodeID int64
	if err := nodeRepo.LookupSecret(r.Context(), secret, &nodeID); err != nil {
		writePlain(w, http.StatusForbidden, "forbidden")
		return
	}
	document, err := configs.Build(r.Context(), nodeID)
	if err != nil {
		log.Error("failed to build node full configuration", "node_id", nodeID, "error", err)
		writePlain(w, http.StatusInternalServerError, "configuration unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(document); err != nil {
		log.Warn("failed to write node full configuration", "node_id", nodeID, "error", err)
	}
}

func reconcileNodeConfig(w http.ResponseWriter, r *http.Request, nodeRepo *nodes.Repository, configs *nodeconfig.Repository, refreshes configRefreshRequester, log *slog.Logger) {
	secret := nodeSecret(r)
	var nodeID int64
	if err := nodeRepo.LookupSecret(r.Context(), secret, &nodeID); err != nil {
		writePlain(w, http.StatusOK, "ok")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
	if err != nil {
		log.Warn("failed to read node configuration snapshot", "node_id", nodeID, "error", err)
		writePlain(w, http.StatusOK, "ok")
		return
	}
	actual, err := nodeconfig.DecodeSnapshot(body, secret)
	if err != nil {
		log.Warn("invalid node configuration snapshot", "node_id", nodeID, "error", err)
		writePlain(w, http.StatusOK, "ok")
		return
	}
	expected, err := configs.Build(r.Context(), nodeID)
	if err != nil {
		log.Warn("failed to build expected node configuration", "node_id", nodeID, "error", err)
		writePlain(w, http.StatusOK, "ok")
		return
	}
	if !nodeconfig.Equivalent(expected, actual) {
		if err := refreshes.RequestNodes(r.Context(), []int64{nodeID}); err != nil {
			log.Warn("failed to enqueue drifted node configuration", "node_id", nodeID, "error", err)
		} else {
			log.Info("node configuration drift detected", "node_id", nodeID)
		}
	}
	writePlain(w, http.StatusOK, "ok")
}
