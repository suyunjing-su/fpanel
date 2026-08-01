package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-gost/core/auth"
	"github.com/go-gost/x/config"
)

type testAuther struct{}

func (testAuther) Authenticate(_ context.Context, user, password string, _ ...auth.Option) (string, bool) {
	return user, user == "admin" && password == "secret"
}

func TestRegisterRequiresValidCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Register(engine, &Options{Auther: testAuther{}})

	for _, credentials := range [][2]string{{"", ""}, {"admin", "wrong"}} {
		request := httptest.NewRequest(http.MethodGet, "/docs/swagger.yaml", nil)
		if credentials[0] != "" {
			request.SetBasicAuth(credentials[0], credentials[1])
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("credentials %q returned status %d, want %d", credentials[0], response.Code, http.StatusUnauthorized)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/docs/swagger.yaml", nil)
	request.SetBasicAuth("admin", "secret")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid credentials returned status %d, want %d", response.Code, http.StatusOK)
	}
}

func TestSaveConfigUsesFixedAtomicDestination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.Set(&config.Config{})
	engine := gin.New()
	Register(engine, &Options{Auther: testAuther{}})

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(workingDirectory) })

	escapedPath := filepath.Join(t.TempDir(), "escaped.json")
	request := httptest.NewRequest(http.MethodPost, "/config?format=json&path="+escapedPath, nil)
	request.SetBasicAuth("admin", "secret")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := os.Stat("gost.json"); err != nil {
		t.Fatalf("fixed config destination was not written: %v", err)
	}
	if _, err := os.Stat(escapedPath); !os.IsNotExist(err) {
		t.Fatalf("request-controlled path was written: %v", err)
	}
}
