package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-gost/x/controller"
)

func TestSyncFullConfigFallsBackAndPromotesController(t *testing.T) {
	primary := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	backup := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"services":[],"chains":[]}`))
	}))
	defer backup.Close()
	originalTransport := http.DefaultTransport
	http.DefaultTransport = backup.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	pool, err := controller.New([]string{primary.URL, backup.URL})
	if err != nil {
		t.Fatal(err)
	}
	oldDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDirectory)
	if err := syncFullConfigFromDashboard(pool, "secret"); err != nil {
		t.Fatal(err)
	}
	if got := pool.Candidates(); !reflect.DeepEqual(got, []string{backup.URL, primary.URL}) {
		t.Fatalf("controllers were not promoted: %#v", got)
	}
	if _, err := os.Stat(filepath.Join("gost.json")); err != nil {
		t.Fatal(err)
	}
}

func TestSyncFullConfigUsesValidCacheWhenControllersFail(t *testing.T) {
	controlServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer controlServer.Close()
	originalTransport := http.DefaultTransport
	http.DefaultTransport = controlServer.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	pool, err := controller.New([]string{controlServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "gost.json")
	if err := os.WriteFile(path, []byte(`{"services":[],"chains":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	usingCache, err := syncFullConfigOrUseCache(pool, "secret", path)
	if err != nil {
		t.Fatal(err)
	}
	if !usingCache {
		t.Fatal("valid cache was not selected")
	}
}

func TestSyncFullConfigRejectsInvalidCache(t *testing.T) {
	controlServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer controlServer.Close()
	originalTransport := http.DefaultTransport
	http.DefaultTransport = controlServer.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	pool, err := controller.New([]string{controlServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "gost.json")
	if err := os.WriteFile(path, []byte(`{"services":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := syncFullConfigOrUseCache(pool, "secret", path); err == nil {
		t.Fatal("invalid cache was accepted")
	}
}

func TestLoadConfigRejectsInsecureController(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"addr":"http://primary/","controllers":["http://primary","https://backup"],"secret":"secret"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("insecure controller was accepted")
	}
}
