package main

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
)

const maxRestoreBytes = 2 << 30

func registerOperationsRoutes(mux *http.ServeMux, db *sql.DB, databasePath string, restart chan<- struct{}, isAdmin func(*http.Request) bool) {
	mux.HandleFunc("POST /api/v1/operations/backup", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		directory, err := os.MkdirTemp("", "flux-download-")
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "备份临时目录创建失败"))
			return
		}
		defer os.RemoveAll(directory)
		path := filepath.Join(directory, "flux-backup.db")
		if err := database.BackupToPath(r.Context(), db, path); err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "数据库备份失败"))
			return
		}
		file, err := os.Open(path)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "数据库备份读取失败"))
			return
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "数据库备份读取失败"))
			return
		}
		filename := "flux-backup-" + strconv.FormatInt(time.Now().Unix(), 10) + ".db"
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, filename, stat.ModTime(), file)
	})
	mux.HandleFunc("POST /api/v1/operations/restore", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRestoreBytes+(1<<20))
		file, _, err := r.FormFile("backup")
		if err != nil {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, "请选择有效的数据库备份文件"))
			return
		}
		defer file.Close()
		if err := database.StageRestore(r.Context(), databasePath, file, maxRestoreBytes); err != nil {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(map[string]any{"restart": true}))
		time.AfterFunc(250*time.Millisecond, func() {
			select {
			case restart <- struct{}{}:
			default:
			}
		})
	})
	mux.HandleFunc("POST /api/v1/operations/support-bundle", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		info, err := database.CollectSupportInfo(r.Context(), db)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusInternalServerError, httpapi.Failure(http.StatusInternalServerError, "支持信息收集失败"))
			return
		}
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		payload := struct {
			Database   database.SupportInfo `json:"database"`
			GoVersion  string               `json:"goVersion"`
			Goroutines int                  `json:"goroutines"`
			Memory     map[string]uint64    `json:"memory"`
		}{
			Database:   info,
			GoVersion:  runtime.Version(),
			Goroutines: runtime.NumGoroutine(),
			Memory: map[string]uint64{
				"allocatedBytes": memory.Alloc,
				"heapInUseBytes": memory.HeapInuse,
				"systemBytes":    memory.Sys,
			},
		}
		filename := "flux-support-" + strconv.FormatInt(time.Now().Unix(), 10) + ".zip"
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		archive := zip.NewWriter(w)
		entry, err := archive.Create("support.json")
		if err == nil {
			encoder := json.NewEncoder(entry)
			encoder.SetIndent("", "  ")
			err = encoder.Encode(payload)
		}
		if err != nil {
			_ = archive.Close()
			return
		}
		_ = archive.Close()
	})
}
