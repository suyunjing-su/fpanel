package siteconfig

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
)

func TestRepositoryProtectsSecrets(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository := NewRepository(db)
	if err := repository.Update(context.Background(), map[string]string{
		"app_name":                     "panel",
		"captcha_recaptcha_secret_key": "server-secret",
	}); err != nil {
		t.Fatal(err)
	}
	if value, err := repository.GetPublic(context.Background(), "app_name"); err != nil || value != "panel" {
		t.Fatalf("unexpected public value %q: %v", value, err)
	}
	if _, err := repository.GetPublic(context.Background(), "captcha_recaptcha_secret_key"); err == nil {
		t.Fatal("secret was available through the public reader")
	}
	values, err := repository.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if values["captcha_recaptcha_secret_key"] != "" {
		t.Fatalf("secret leaked through administrator list: %q", values["captcha_recaptcha_secret_key"])
	}
	if configured, err := repository.SecretConfigured(context.Background(), "captcha_recaptcha_secret_key"); err != nil || !configured {
		t.Fatalf("secret configured state is invalid: %v, %v", configured, err)
	}
	if err := repository.Update(context.Background(), map[string]string{"captcha_recaptcha_secret_key": ""}); err != nil {
		t.Fatal(err)
	}
	if value, err := repository.Get(context.Background(), "captcha_recaptcha_secret_key"); err != nil || value != "server-secret" {
		t.Fatalf("blank update erased secret: %q, %v", value, err)
	}
	var secret bool
	if err := db.QueryRow("SELECT secret FROM site_config WHERE key = 'captcha_recaptcha_secret_key'").Scan(&secret); err != nil || !secret {
		t.Fatalf("secret marker is missing: %v, %v", secret, err)
	}
}
